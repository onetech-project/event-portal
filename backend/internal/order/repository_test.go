package order_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/db"
)

func newRepo(t *testing.T) (*order.Repository, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)
	return order.NewRepository(pool), pool
}

func TestCreateBookedOrderPersistsAPendingOrderWithoutBuyerOrPayment(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	var created order.OrderRecord
	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		created, err = repo.CreateBookedOrder(ctx, tx, "ORD-20260731-ABCDEF",
			decimal.RequireFromString("300000.00"), decimal.RequireFromString("300000.00"),
			time.Now().Add(time.Hour))
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, "PENDING", created.Status)
	assert.Nil(t, created.BuyerName, "buyer identity arrives only at checkout (Option B)")
	assert.Nil(t, created.PaymentURL, "the payment URL is only stamped after the gateway call")
	assert.Nil(t, created.PaymentProvider)
	assert.Equal(t, "300000.00", created.TotalAmount.StringFixed(2))
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, pool, created.ID))
}

// Migration 0013 moved orders.status to a status_id reference while keeping the
// NAME as the only value anything above storage sees (spec 011 FR-026). This
// walks the whole loop — create, read by id, read by number, transition, read
// again — asserting a NAME every time, so a future change that lets the numeric
// id surface on a record or a DTO fails here rather than in a client.
func TestOrderStatusIsExchangedByNameThroughEveryReadPath(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	var created order.OrderRecord
	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		created, err = repo.CreateBookedOrder(ctx, tx, "ORD-STATUS-NAME",
			decimal.RequireFromString("100000.00"), decimal.RequireFromString("100000.00"),
			time.Now().Add(time.Hour))
		return err
	})
	require.NoError(t, err)
	require.Equal(t, "PENDING", created.Status, "the insert returns the name, not the id")

	byID, err := repo.GetOrderByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "PENDING", byID.Status)

	byNumber, err := repo.GetOrderByNumber(ctx, "ORD-STATUS-NAME")
	require.NoError(t, err)
	assert.Equal(t, "PENDING", byNumber.Status)

	// The transition takes a name too — the caller never learns an id exists.
	var moved bool
	err = db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		moved, err = repo.UpdateOrderStatusIfPending(ctx, tx, created.ID, "PAID")
		return err
	})
	require.NoError(t, err)
	require.True(t, moved, "a PENDING order transitions on the first attempt")

	after, err := repo.GetOrderByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "PAID", after.Status)

	// And the guard still holds: a second transition finds nothing PENDING,
	// which is what makes a replayed webhook a no-op.
	var again bool
	err = db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		again, err = repo.UpdateOrderStatusIfPending(ctx, tx, created.ID, "CANCELLED")
		return err
	})
	require.NoError(t, err)
	assert.False(t, again, "the PENDING guard survived the move to a reference")
	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, pool, created.ID))
}

func TestCreateBookedOrderRejectsADuplicateOrderNumber(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	testsupport.SeedOrder(t, pool, "ORD-DUP", "PENDING")

	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := repo.CreateBookedOrder(ctx, tx, "ORD-DUP",
			decimal.NewFromInt(1), decimal.NewFromInt(1), time.Now().Add(time.Hour))
		return err
	})

	assert.ErrorIs(t, err, order.ErrOrderNumberTaken)
}

func TestBookedOrderItemsAndSlotsShareTheOrdersTransaction(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "tx-shape", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		created, err := repo.CreateBookedOrder(ctx, tx, "ORD-ROLLBACK",
			decimal.NewFromInt(300000), decimal.NewFromInt(300000), time.Now().Add(time.Hour))
		if err != nil {
			return err
		}
		if err := repo.CreateOrderItem(ctx, tx, created.ID, order.TicketLine(tt.ID), 2, tt.Price); err != nil {
			return err
		}
		if _, err := repo.CreateAttendeeSlot(ctx, tx, created.ID, order.AttendeeRef{TicketTypeID: tt.ID}); err != nil {
			return err
		}
		return assert.AnError // abort the whole booking
	})

	require.Error(t, err)

	// Nothing may survive: orders, order_items, and attendees are one unit of work.
	_, err = repo.GetOrderByNumber(ctx, "ORD-ROLLBACK")
	assert.ErrorIs(t, err, order.ErrNotFound)
}

func TestOrderNumberExists(t *testing.T) {
	repo, pool := newRepo(t)
	testsupport.SeedOrder(t, pool, "ORD-TAKEN", "PENDING")

	taken, err := repo.OrderNumberExists(context.Background(), "ORD-TAKEN")
	require.NoError(t, err)
	assert.True(t, taken)

	free, err := repo.OrderNumberExists(context.Background(), "ORD-FREE")
	require.NoError(t, err)
	assert.False(t, free)
}

func TestUpdatePaymentDetailsStampsTheWholePaymentInstruction(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-PAYURL", "PENDING")
	expiresAt := time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second)

	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		return repo.UpdatePaymentDetails(ctx, tx, seeded.ID, order.PaymentDetails{
			PaymentURL: "https://pay.example.com/x",
			Provider:   "midtrans",
			QRString:   "00020101021226620014COM.EXAMPLE",
			ExpiresAt:  expiresAt,
		})
	})

	require.NoError(t, err)
	got, err := repo.GetOrderByID(ctx, seeded.ID)
	require.NoError(t, err)
	require.NotNil(t, got.PaymentURL)
	assert.Equal(t, "https://pay.example.com/x", *got.PaymentURL)
	require.NotNil(t, got.PaymentProvider)
	assert.Equal(t, "midtrans", *got.PaymentProvider)
	require.NotNil(t, got.PaymentQRString)
	assert.Equal(t, "00020101021226620014COM.EXAMPLE", *got.PaymentQRString)
	require.NotNil(t, got.PaymentExpiresAt)
	assert.WithinDuration(t, expiresAt, *got.PaymentExpiresAt, time.Second)
}

// An order whose charge never produced an instruction must read back as "no
// instruction", not as an empty code with a zero deadline — the read model keys
// off nil to decide whether to show a QR at all.
func TestUpdatePaymentDetailsLeavesEmptyInstructionFieldsNull(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-NOQR", "PENDING")

	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		return repo.UpdatePaymentDetails(ctx, tx, seeded.ID, order.PaymentDetails{
			PaymentURL: "https://pay.example.com/x",
			Provider:   "midtrans",
		})
	})

	require.NoError(t, err)
	got, err := repo.GetOrderByID(ctx, seeded.ID)
	require.NoError(t, err)
	assert.Nil(t, got.PaymentQRString)
	assert.Nil(t, got.PaymentExpiresAt)
}

func TestListOrdersDueForExpiryReturnsOnlyLapsedPendingOrders(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	lapsed := testsupport.SeedOrder(t, pool, "ORD-LAPSED", "PENDING")
	live := testsupport.SeedOrder(t, pool, "ORD-LIVE", "PENDING")
	settled := testsupport.SeedOrder(t, pool, "ORD-SETTLED", "PAID")
	noDeadline := testsupport.SeedOrder(t, pool, "ORD-NODEADLINE", "PENDING")

	setExpiry(t, pool, lapsed.ID, -2*time.Minute)
	setExpiry(t, pool, live.ID, 5*time.Minute)
	setExpiry(t, pool, settled.ID, -2*time.Minute)

	due, err := repo.ListOrdersDueForExpiry(ctx, time.Now(), 10)

	require.NoError(t, err)
	require.Len(t, due, 1, "only a PENDING order past its deadline is due")
	assert.Equal(t, "ORD-LAPSED", due[0].OrderNumber)

	// Named so the intent of seeding it is not lost: an order with no deadline
	// (one created before this feature) must never be swept.
	_ = noDeadline
}

func TestListOrdersDueForExpiryReturnsOldestFirstAndHonoursTheLimit(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	newest := testsupport.SeedOrder(t, pool, "ORD-NEWEST", "PENDING")
	oldest := testsupport.SeedOrder(t, pool, "ORD-OLDEST", "PENDING")
	middle := testsupport.SeedOrder(t, pool, "ORD-MIDDLE", "PENDING")

	setExpiry(t, pool, newest.ID, -1*time.Minute)
	setExpiry(t, pool, oldest.ID, -30*time.Minute)
	setExpiry(t, pool, middle.ID, -10*time.Minute)

	// The longest-held quota is released first, so a batch limit cannot starve
	// the orders that have been sitting on seats the longest.
	due, err := repo.ListOrdersDueForExpiry(ctx, time.Now(), 2)

	require.NoError(t, err)
	require.Len(t, due, 2)
	assert.Equal(t, "ORD-OLDEST", due[0].OrderNumber)
	assert.Equal(t, "ORD-MIDDLE", due[1].OrderNumber)
}

func setExpiry(t *testing.T, pool *testsupport.Pool, orderID uuid.UUID, offset time.Duration) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE orders SET payment_expires_at = now() + $2::interval WHERE id = $1`,
		orderID, offset.String())
	require.NoError(t, err)
}

func TestUpdateOrderStatusIfPendingAppliesTheTransitionOnce(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-ONCE", "PENDING")

	var first, second bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		first, err = repo.UpdateOrderStatusIfPending(ctx, tx, seeded.ID, "CANCELLED")
		return err
	}))
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		second, err = repo.UpdateOrderStatusIfPending(ctx, tx, seeded.ID, "CANCELLED")
		return err
	}))

	assert.True(t, first, "the first transition applies")
	assert.False(t, second, "a replayed notification must be a no-op, or quota would be restored twice")
	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, pool, seeded.ID))
}

func TestUpdateOrderStatusIfPendingLeavesAPaidOrderAlone(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-PAID", "PAID")

	var applied bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		applied, err = repo.UpdateOrderStatusIfPending(ctx, tx, seeded.ID, "EXPIRED")
		return err
	}))

	assert.False(t, applied)
	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, pool, seeded.ID))
}

func TestSetEmailSentFlipsTheFlag(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-EMAIL", "PAID")

	require.NoError(t, repo.SetEmailSent(ctx, seeded.ID))

	got, err := repo.GetOrderByID(ctx, seeded.ID)
	require.NoError(t, err)
	require.NotNil(t, got.EmailSent)
	assert.True(t, *got.EmailSent)
}

func TestGetOrderByNumberReportsMissingOrders(t *testing.T) {
	repo, _ := newRepo(t)

	_, err := repo.GetOrderByNumber(context.Background(), "ORD-NOPE")

	assert.ErrorIs(t, err, order.ErrNotFound)
}

func TestListOrderItemsAndAttendees(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "listing", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-LIST", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 2)
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Andi", "andi@example.com")
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Sari", "sari@example.com")

	items, err := repo.ListOrderItemsByOrderID(ctx, ord.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int32(2), items[0].Quantity)
	assert.Equal(t, tt.ID, items[0].Ref.TicketTypeID.UUID)

	attendees, err := repo.ListAttendeesByOrderID(ctx, ord.ID)
	require.NoError(t, err)
	assert.Len(t, attendees, 2)
}

func TestAttendeeIDsForOrderReturnsOneIDPerAttendee(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "attendee-ids", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-IDS", "PAID")
	first := testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Andi", "andi@example.com")
	second := testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Sari", "sari@example.com")

	ids, err := repo.AttendeeIDsForOrder(ctx, ord.ID)

	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{first, second}, ids)
}

func TestAttendeeIDsForOrderIsEmptyForAnOrderWithNoAttendees(t *testing.T) {
	repo, pool := newRepo(t)
	ord := testsupport.SeedOrder(t, pool, "ORD-NOBODY", "PENDING")

	ids, err := repo.AttendeeIDsForOrder(context.Background(), ord.ID)

	require.NoError(t, err)
	assert.Empty(t, ids)
}

// --- OrderChecker (delete guards + derived sold counts) --------------------

func TestHasOrdersForTicketTypeDetectsAnOrderItemReference(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "guard-items", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-GUARD1", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 1)

	var has bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		has, err = repo.HasOrdersForTicketType(ctx, tx, tt.ID)
		return err
	}))

	assert.True(t, has)
}

// Both FKs onto ticket_types are ON DELETE RESTRICT, so a guard that only checked
// order_items would let the delete through and surface a raw constraint violation
// instead of the required 400 (Constitution Principle VI).
func TestHasOrdersForTicketTypeDetectsAnAttendeeOnlyReference(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "guard-attendees", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-GUARD2", "PAID")
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Andi", "andi@example.com")

	var has bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		has, err = repo.HasOrdersForTicketType(ctx, tx, tt.ID)
		return err
	}))

	assert.True(t, has, "an attendee reference alone must still block deletion")
}

func TestHasOrdersForTicketTypeIsFalseForAnUnsoldType(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "guard-clean", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	var has bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		has, err = repo.HasOrdersForTicketType(ctx, tx, tt.ID)
		return err
	}))

	assert.False(t, has)
}

func TestHasOrdersForTicketTypesChecksTheWholeSet(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "guard-set", "PUBLISHED")
	clean := testsupport.SeedTicketType(t, pool, ev.ID, "Clean", "100000.00", 10)
	sold := testsupport.SeedTicketType(t, pool, ev.ID, "Sold", "200000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-GUARD3", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, sold.ID, 1)

	var hasAll, hasCleanOnly bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if hasAll, err = repo.HasOrdersForTicketTypes(ctx, tx, []uuid.UUID{clean.ID, sold.ID}); err != nil {
			return err
		}
		hasCleanOnly, err = repo.HasOrdersForTicketTypes(ctx, tx, []uuid.UUID{clean.ID})
		return err
	}))

	assert.True(t, hasAll, "one sold type in the set blocks the whole event delete")
	assert.False(t, hasCleanOnly)
}

func TestHasOrdersForTicketTypesIsFalseForAnEmptySet(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	var has bool
	require.NoError(t, db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		has, err = repo.HasOrdersForTicketTypes(ctx, tx, nil)
		return err
	}))

	assert.False(t, has, "an event with no ticket types has nothing blocking its delete")
}

func TestSoldCountByTicketTypesIsDerivedAndBatched(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "sold-count", "PUBLISHED")
	a := testsupport.SeedTicketType(t, pool, ev.ID, "A", "100000.00", 10)
	b := testsupport.SeedTicketType(t, pool, ev.ID, "B", "200000.00", 10)
	unsold := testsupport.SeedTicketType(t, pool, ev.ID, "C", "300000.00", 10)

	first := testsupport.SeedOrder(t, pool, "ORD-SOLD1", "PAID")
	second := testsupport.SeedOrder(t, pool, "ORD-SOLD2", "PAID")
	testsupport.SeedOrderItem(t, pool, first.ID, a.ID, 2)
	testsupport.SeedOrderItem(t, pool, second.ID, a.ID, 3)
	testsupport.SeedOrderItem(t, pool, first.ID, b.ID, 1)

	counts, err := repo.SoldCountByTicketType(ctx, []uuid.UUID{a.ID, b.ID, unsold.ID})

	require.NoError(t, err)
	assert.Equal(t, 5, counts[a.ID])
	assert.Equal(t, 1, counts[b.ID])
	assert.Zero(t, counts[unsold.ID], "a ticket type with no sales reports zero, not a missing key")
}

func TestSoldCountByTicketTypesHandlesAnEmptyRequest(t *testing.T) {
	repo, _ := newRepo(t)

	counts, err := repo.SoldCountByTicketType(context.Background(), nil)

	require.NoError(t, err)
	assert.Empty(t, counts)
}

// T050: ListQuotaHoldsByOrderID expands package lines through the junction and
// returns the exact inverse of what checkout deducted — a package line of
// quantity q over components c1..cN holds q × ci.quantity of each ci.
func TestListQuotaHoldsByOrderIDExpandsPackageLines(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "quota-holds", "PUBLISHED")
	ttA := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 10)
	ttB := testsupport.SeedTicketType(t, pool, ev.ID, "Day 2", "20000.00", 10)
	ttC := testsupport.SeedTicketType(t, pool, ev.ID, "Standalone", "15000.00", 10)
	pkg := testsupport.SeedPackage(t, pool, ev.ID, "Day 1+2", "50000.00", true)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, ttA.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, ttB.ID, ev.ID, 2)

	ord := testsupport.SeedOrder(t, pool, "ORD-HOLDS", "PENDING")
	// 2 package units → 2 × ttA, 2 × 2 = 4 × ttB.
	testsupport.SeedOrderItemPackage(t, pool, ord.ID, pkg.ID, 2, decimal.NewFromInt(100000))
	// Standalone 3 × ttC.
	testsupport.SeedOrderItem(t, pool, ord.ID, ttC.ID, 3)

	var holds []order.QuotaHold
	err := db.InTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		holds, e = repo.ListQuotaHoldsByOrderID(ctx, tx, ord.ID)
		return e
	})
	require.NoError(t, err)

	got := map[uuid.UUID]int32{}
	for _, h := range holds {
		got[h.TicketTypeID] = h.Quantity
	}
	assert.Equal(t, int32(2), got[ttA.ID], "package quantity × quantity_per_unit (1)")
	assert.Equal(t, int32(4), got[ttB.ID], "package quantity × quantity_per_unit (2)")
	assert.Equal(t, int32(3), got[ttC.ID], "standalone line passes through unchanged")
	assert.Len(t, holds, 3, "one hold per touched ticket type, package expanded, never one per package")
}
