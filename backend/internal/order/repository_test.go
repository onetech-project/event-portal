package order_test

import (
	"context"
	"testing"

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

func TestCreateOrderPersistsAPendingOrderWithoutAPaymentURL(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	var created order.OrderRecord
	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		created, err = repo.CreateOrder(ctx, tx, order.CreateOrderParams{
			OrderNumber: "ORD-20260731-ABCDEF",
			BuyerName:   "Budi",
			BuyerEmail:  "budi@example.com",
			BuyerPhone:  "+628123456789",
			TotalAmount: decimal.RequireFromString("300000.00"),
		})
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, "PENDING", created.Status)
	assert.Nil(t, created.PaymentURL, "the payment URL is only stamped after the gateway call")
	assert.Nil(t, created.PaymentProvider)
	assert.Equal(t, "300000.00", created.TotalAmount.StringFixed(2))
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, pool, created.ID))
}

func TestCreateOrderRejectsADuplicateOrderNumber(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	testsupport.SeedOrder(t, pool, "ORD-DUP", "PENDING")

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := repo.CreateOrder(ctx, tx, order.CreateOrderParams{
			OrderNumber: "ORD-DUP",
			BuyerName:   "Budi",
			BuyerEmail:  "budi@example.com",
			BuyerPhone:  "+62812",
			TotalAmount: decimal.NewFromInt(1),
		})
		return err
	})

	assert.ErrorIs(t, err, order.ErrOrderNumberTaken)
}

func TestCreateOrderItemsAndAttendeesShareTheOrdersTransaction(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "tx-shape", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		created, err := repo.CreateOrder(ctx, tx, order.CreateOrderParams{
			OrderNumber: "ORD-ROLLBACK",
			BuyerName:   "Budi",
			BuyerEmail:  "budi@example.com",
			BuyerPhone:  "+62812",
			TotalAmount: decimal.NewFromInt(300000),
		})
		if err != nil {
			return err
		}
		if err := repo.CreateOrderItem(ctx, tx, created.ID, tt.ID, 2, tt.Price); err != nil {
			return err
		}
		if _, err := repo.CreateAttendee(ctx, tx, created.ID, tt.ID, "A", "a@example.com"); err != nil {
			return err
		}
		return assert.AnError // abort the whole checkout
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

func TestUpdatePaymentDetailsStampsTheURLAndProvider(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-PAYURL", "PENDING")

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdatePaymentDetails(ctx, tx, seeded.ID, "https://pay.example.com/x", "midtrans")
	})

	require.NoError(t, err)
	got, err := repo.GetOrderByID(ctx, seeded.ID)
	require.NoError(t, err)
	require.NotNil(t, got.PaymentURL)
	assert.Equal(t, "https://pay.example.com/x", *got.PaymentURL)
	require.NotNil(t, got.PaymentProvider)
	assert.Equal(t, "midtrans", *got.PaymentProvider)
}

func TestUpdateOrderStatusIfPendingAppliesTheTransitionOnce(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	seeded := testsupport.SeedOrder(t, pool, "ORD-ONCE", "PENDING")

	var first, second bool
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		first, err = repo.UpdateOrderStatusIfPending(ctx, tx, seeded.ID, "CANCELLED")
		return err
	}))
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
	assert.Equal(t, tt.ID, items[0].TicketTypeID)

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
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
	require.NoError(t, db.InTx(ctx, pool, func(tx pgx.Tx) error {
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
