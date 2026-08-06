package order_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// eventProviderAdapter is the same shape cmd/api wires in production: it adapts the
// event domain's service onto the contract the order domain declares, so neither
// package imports the other.
type eventProviderAdapter struct{ svc *event.Service }

func (a eventProviderAdapter) TicketTypeForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (order.TicketTypeInfo, error) {
	row, err := a.svc.GetTicketTypeForCheckout(ctx, tx, id)
	if err != nil {
		return order.TicketTypeInfo{}, err
	}
	return order.TicketTypeInfo{
		ID:         row.ID,
		EventID:    row.EventID,
		Name:       row.Name,
		Price:      row.Price,
		SalesStart: row.SalesStart,
		SalesEnd:   row.SalesEnd,
	}, nil
}

func (a eventProviderAdapter) CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, id uuid.UUID, qty int32) error {
	err := a.svc.CheckAndDeductQuota(ctx, tx, id, qty)
	if errors.Is(err, event.ErrInsufficientQuota) {
		return order.ErrInsufficientQuota
	}
	return err
}

func (a eventProviderAdapter) RestoreQuota(ctx context.Context, tx pgx.Tx, id uuid.UUID, qty int32) error {
	return a.svc.RestoreQuota(ctx, tx, id, qty)
}

func (a eventProviderAdapter) CurrentTerms(ctx context.Context, eventID uuid.UUID) (order.EventTermsInfo, error) {
	row, err := a.svc.CurrentTermsForEvent(ctx, eventID)
	if errors.Is(err, event.ErrNotFound) {
		return order.EventTermsInfo{}, order.ErrNoTerms
	}
	if err != nil {
		return order.EventTermsInfo{}, err
	}
	return order.EventTermsInfo{ID: row.ID, EventID: eventID}, nil
}

func (a eventProviderAdapter) PackageForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (order.PackageInfo, error) {
	row, err := a.svc.GetPackageForCheckout(ctx, tx, id)
	if err != nil {
		return order.PackageInfo{}, err
	}
	components := make([]order.PackageComponentInfo, len(row.Components))
	for i, c := range row.Components {
		components[i] = order.PackageComponentInfo{
			TicketTypeID: c.TicketTypeID,
			Quantity:     c.PerUnit,
			SalesStart:   c.SalesStart,
			SalesEnd:     c.SalesEnd,
		}
	}
	return order.PackageInfo{
		ID:         row.ID,
		EventID:    row.EventID,
		Name:       row.Name,
		Price:      row.Price,
		SalesStart: row.SalesStart,
		SalesEnd:   row.SalesEnd,
		Components: components,
	}, nil
}

// fakeGateway stands in for the payment provider so checkout can be driven through
// both its success and its failure path without a network.
type fakeGateway struct {
	mu        sync.Mutex
	url       string
	qrString  string
	expiresAt time.Time
	err       error
	calls     []order.PaymentRequest
	// onCall runs inside CreateTransaction, which is where a test can observe
	// what the database looks like at the moment the gateway is called.
	onCall func(order.PaymentRequest)
}

func (g *fakeGateway) Name() string { return "fakegw" }

func (g *fakeGateway) CreateTransaction(_ context.Context, req order.PaymentRequest) (order.PaymentSession, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, req)
	if g.onCall != nil {
		g.onCall(req)
	}
	if g.err != nil {
		return order.PaymentSession{}, g.err
	}
	return order.PaymentSession{
		ProviderRef: "txn-" + req.OrderNumber,
		QRString:    g.qrString,
		QRImageURL:  g.url,
		ExpiresAt:   g.expiresAt,
	}, nil
}

func (g *fakeGateway) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

func (g *fakeGateway) lastCall() order.PaymentRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls[len(g.calls)-1]
}

type checkoutFixture struct {
	svc     *order.Service
	public  *order.PublicService
	pool    *testsupport.Pool
	gateway *fakeGateway
	repo    *order.Repository
}

func newCheckoutFixture(t *testing.T) checkoutFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	repo := order.NewRepository(pool)
	gw := &fakeGateway{
		url:       "https://pay.example.com/session",
		qrString:  "00020101021226620014COM.EXAMPLE.QRIS",
		expiresAt: time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second),
	}
	events := event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())
	provider := eventProviderAdapter{svc: events}

	return checkoutFixture{
		svc:     order.NewService(pool, repo, provider, gw, testsupport.DiscardLogger()),
		public:  order.NewPublicService(repo, eventLookupAdapter{svc: events}),
		pool:    pool,
		gateway: gw,
		repo:    repo,
	}
}

func (f checkoutFixture) seedSellableEvent(t *testing.T, quota int32) testsupport.TicketType {
	t.Helper()
	ev := testsupport.SeedEvent(t, f.pool, "sellable", "PUBLISHED")
	return testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", quota)
}

// seedBundleEvent seeds an event with two ticket types and a bundle over both
// (Day 1 + Day 2, quantity_per_unit 1 each). Returns the event, both ticket
// types and the package.
func (f checkoutFixture) seedBundleEvent(t *testing.T, day1Quota, day2Quota int32) (testsupport.Event, testsupport.TicketType, testsupport.TicketType, testsupport.Package) {
	t.Helper()
	ev := testsupport.SeedEvent(t, f.pool, "bundle-event", "PUBLISHED")
	day1 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 1", "30000.00", day1Quota)
	day2 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 2", "20000.00", day2Quota)
	pkg := testsupport.SeedPackage(t, f.pool, ev.ID, "Day 1+2 Bundle", "50000.00", "ACTIVE")
	testsupport.SeedPackageTicket(t, f.pool, pkg.ID, day1.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, f.pool, pkg.ID, day2.ID, ev.ID, 1)
	return ev, day1, day2, pkg
}

func checkoutFor(tt testsupport.TicketType, quantity int32) order.CheckoutRequest {
	attendees := make([]order.CheckoutAttendee, 0, quantity)
	for range int(quantity) {
		attendees = append(attendees, order.CheckoutAttendee{
			TicketTypeID: tt.ID,
			Name:         "Attendee",
			Email:        "attendee@example.com",
		})
	}
	return order.CheckoutRequest{
		BuyerName:  "Budi Santoso",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+628123456789",
		Items:      []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: quantity}},
		Attendees:  attendees,
	}
}

// bundleCheckoutFor builds a request buying quantity units of a package with
// constituents day1 and day2, one attendee per constituent unit.
func bundleCheckoutFor(pkg testsupport.Package, day1, day2 testsupport.TicketType, quantity int32) order.CheckoutRequest {
	var attendees []order.CheckoutAttendee
	for range int(quantity) {
		attendees = append(attendees,
			order.CheckoutAttendee{TicketTypeID: day1.ID, PackageID: &pkg.ID, Name: "Budi", Email: "budi@example.com"},
			order.CheckoutAttendee{TicketTypeID: day2.ID, PackageID: &pkg.ID, Name: "Siti", Email: "siti@example.com"},
		)
	}
	return order.CheckoutRequest{
		BuyerName:  "Budi Santoso",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+628123456789",
		Items:      []order.CheckoutItem{{PackageID: &pkg.ID, Quantity: quantity}},
		Attendees:  attendees,
	}
}

// --- Happy path -----------------------------------------------------------

func TestCheckoutCreatesAnOrderDeductsQuotaAndReturnsAPaymentURL(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)
	ctx := context.Background()

	resp, err := f.svc.Checkout(ctx, checkoutFor(tt, 2))

	require.NoError(t, err)
	assert.NotEmpty(t, resp.OrderNumber)
	assert.Equal(t, "PENDING", resp.Status)
	assert.Equal(t, "https://pay.example.com/session", resp.PaymentURL)
	// The total is recomputed server-side: 2 x 150000.
	assert.Equal(t, "300000.00", resp.TotalAmount.String())

	assert.Equal(t, int32(8), testsupport.QuotaOf(t, f.pool, tt.ID))

	stored, err := f.repo.GetOrderByNumber(ctx, resp.OrderNumber)
	require.NoError(t, err)
	assert.Equal(t, "PENDING", stored.Status)
	require.NotNil(t, stored.PaymentURL)
	assert.Equal(t, "https://pay.example.com/session", *stored.PaymentURL)
	require.NotNil(t, stored.PaymentProvider)
	assert.Equal(t, "fakegw", *stored.PaymentProvider)

	// The payment instruction the guest's page renders: the payload it scans and
	// the deadline it counts down to. Without both, the order page has nothing to
	// show (spec FR-011, FR-012).
	require.NotNil(t, stored.PaymentQRString)
	assert.Equal(t, "00020101021226620014COM.EXAMPLE.QRIS", *stored.PaymentQRString)
	require.NotNil(t, stored.PaymentExpiresAt)
	assert.WithinDuration(t, f.gateway.expiresAt, *stored.PaymentExpiresAt, time.Second)

	items, err := f.repo.ListOrderItemsByOrderID(ctx, stored.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int32(2), items[0].Quantity)
	assert.Equal(t, "150000.00", items[0].Price.StringFixed(2), "the line is priced from server-side data")

	attendees, err := f.repo.ListAttendeesByOrderID(ctx, stored.ID)
	require.NoError(t, err)
	assert.Len(t, attendees, 2, "one attendee row per ticket, so one ticket can be issued per person")
}

func TestCheckoutPricesFromTheServerNotTheClient(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "multi", "PUBLISHED")
	regular := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	vip := testsupport.SeedTicketType(t, f.pool, ev.ID, "VIP", "500000.00", 10)

	req := order.CheckoutRequest{
		BuyerName:  "Budi",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+62812",
		Items: []order.CheckoutItem{
			{TicketTypeID: &regular.ID, Quantity: 2},
			{TicketTypeID: &vip.ID, Quantity: 1},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: regular.ID, Name: "A", Email: "a@example.com"},
			{TicketTypeID: regular.ID, Name: "B", Email: "b@example.com"},
			{TicketTypeID: vip.ID, Name: "C", Email: "c@example.com"},
		},
	}

	resp, err := f.svc.Checkout(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "800000.00", resp.TotalAmount.String())
	assert.Equal(t, int32(8), testsupport.QuotaOf(t, f.pool, regular.ID))
	assert.Equal(t, int32(9), testsupport.QuotaOf(t, f.pool, vip.ID))
}

func TestCheckoutSendsTheOrderNumberAndAmountToTheGateway(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)

	resp, err := f.svc.Checkout(context.Background(), checkoutFor(tt, 2))
	require.NoError(t, err)

	call := f.gateway.lastCall()
	assert.Equal(t, resp.OrderNumber, call.OrderNumber)
	assert.Equal(t, "300000.00", call.GrossAmount.StringFixed(2))
	assert.Equal(t, "budi@example.com", call.CustomerEmail)
	require.Len(t, call.Items, 1)
	assert.Equal(t, int32(2), call.Items[0].Quantity)
}

// --- Validation and quota -------------------------------------------------

func TestCheckoutRejectsAnInvalidRequestBeforeTouchingQuota(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)

	req := checkoutFor(tt, 2)
	req.Attendees = req.Attendees[:1]

	_, err := f.svc.Checkout(context.Background(), req)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeAttendeeCountMismatch, appErr.Code)
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, tt.ID))
	assert.Zero(t, f.gateway.callCount(), "an invalid request must never reach the gateway")
}

func TestCheckoutRejectsMoreTicketsThanRemainingQuota(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 2)

	_, err := f.svc.Checkout(context.Background(), checkoutFor(tt, 3))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeInsufficientQuota, appErr.Code)
	assert.Equal(t, int32(2), testsupport.QuotaOf(t, f.pool, tt.ID))
	assert.Zero(t, f.gateway.callCount())
}

// A partially-satisfiable multi-line order must not leave the satisfiable line
// deducted — TX1 is all or nothing.
func TestCheckoutRollsBackEveryDeductionWhenOneLineIsShort(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "partial", "PUBLISHED")
	plenty := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	scarce := testsupport.SeedTicketType(t, f.pool, ev.ID, "VIP", "500000.00", 1)

	req := order.CheckoutRequest{
		BuyerName: "Budi", BuyerEmail: "budi@example.com", BuyerPhone: "+62812",
		Items: []order.CheckoutItem{
			{TicketTypeID: &plenty.ID, Quantity: 2},
			{TicketTypeID: &scarce.ID, Quantity: 2},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: plenty.ID, Name: "A", Email: "a@example.com"},
			{TicketTypeID: plenty.ID, Name: "B", Email: "b@example.com"},
			{TicketTypeID: scarce.ID, Name: "C", Email: "c@example.com"},
			{TicketTypeID: scarce.ID, Name: "D", Email: "d@example.com"},
		},
	}

	_, err := f.svc.Checkout(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, plenty.ID))
	assert.Equal(t, int32(1), testsupport.QuotaOf(t, f.pool, scarce.ID))
}

func TestCheckoutRejectsAnUnknownTicketType(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)

	req := checkoutFor(tt, 1)
	ghost := uuid.New()
	req.Items[0].TicketTypeID = &ghost
	req.Attendees[0].TicketTypeID = ghost

	_, err := f.svc.Checkout(context.Background(), req)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
}

func TestCheckoutRejectsATicketTypeWhoseSalesHaveNotOpened(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "not-yet", "PUBLISHED")
	tt := testsupport.SeedTicketTypeWindow(t, f.pool, ev.ID, "Early", 10,
		time.Now().Add(24*time.Hour), time.Now().Add(48*time.Hour))

	_, err := f.svc.Checkout(context.Background(), checkoutFor(tt, 1))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeTicketTypeNotOnSale, appErr.Code)
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, tt.ID))
}

func TestCheckoutRejectsATicketTypeWhoseSalesHaveClosed(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "too-late", "PUBLISHED")
	tt := testsupport.SeedTicketTypeWindow(t, f.pool, ev.ID, "Late", 10,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))

	_, err := f.svc.Checkout(context.Background(), checkoutFor(tt, 1))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeTicketTypeNotOnSale, appErr.Code)
}

// The quota-deducting UPDATE holds a row lock until commit, so a gateway
// round-trip inside TX1 would serialize every concurrent buyer of the same
// ticket type behind it (Constitution Principle IV). This asserts the shape that
// prevents it: by the time the gateway is called, TX1 has already committed and
// the order is readable on another connection.
func TestCheckoutCallsTheGatewayOutsideTheReservingTransaction(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)
	ctx := context.Background()

	var committedDuringCall bool
	f.gateway.onCall = func(req order.PaymentRequest) {
		// A separate connection: it can only see the row if TX1 committed.
		stored, err := f.repo.GetOrderByNumber(ctx, req.OrderNumber)
		committedDuringCall = err == nil && stored.Status == "PENDING"
	}

	_, err := f.svc.Checkout(ctx, checkoutFor(tt, 1))

	require.NoError(t, err)
	assert.True(t, committedDuringCall,
		"the reserving transaction must be committed before the provider is called")
}

// --- Gateway failure and compensation (FR-021) ----------------------------

func TestCheckoutCompensatesWhenTheGatewayFails(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)
	f.gateway.err = errors.New("gateway unreachable")
	ctx := context.Background()

	_, err := f.svc.Checkout(ctx, checkoutFor(tt, 3))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusBadGateway, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodePaymentInitiationFailed, appErr.Code)

	// The guest may safely retry: the reservation has been fully released.
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, tt.ID),
		"quota reserved in TX1 must be restored when payment initiation fails")
}

func TestCheckoutMarksTheOrderCancelledWhenTheGatewayFails(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 10)
	f.gateway.err = errors.New("gateway unreachable")
	ctx := context.Background()

	_, err := f.svc.Checkout(ctx, checkoutFor(tt, 1))
	require.Error(t, err)

	// The order row survives for audit, but as CANCELLED, not PENDING.
	orders := allOrders(t, f.pool)
	require.Len(t, orders, 1)
	assert.Equal(t, "CANCELLED", orders[0])
}

// --- Concurrency ----------------------------------------------------------

func TestConcurrentCheckoutsCannotOversell(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 5)
	ctx := context.Background()

	const buyers = 12
	var wg sync.WaitGroup
	errs := make([]error, buyers)
	for i := range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Checkout(ctx, checkoutFor(tt, 1))
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}

	assert.Equal(t, 5, succeeded, "exactly the available quota may be sold")
	assert.Equal(t, int32(0), testsupport.QuotaOf(t, f.pool, tt.ID))
}

func TestConcurrentCheckoutsGetDistinctOrderNumbers(t *testing.T) {
	f := newCheckoutFixture(t)
	tt := f.seedSellableEvent(t, 20)
	ctx := context.Background()

	const buyers = 20
	var wg sync.WaitGroup
	numbers := make([]string, buyers)
	for i := range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := f.svc.Checkout(ctx, checkoutFor(tt, 1))
			if err == nil {
				numbers[i] = resp.OrderNumber
			}
		}()
	}
	wg.Wait()

	seen := map[string]struct{}{}
	for _, n := range numbers {
		require.NotEmpty(t, n)
		_, dup := seen[n]
		assert.False(t, dup, "order numbers must be unique")
		seen[n] = struct{}{}
	}
}

// T048: N goroutines contend for one remaining complete set; exactly one wins,
// quotas land at 0, never negative (SC-003, SC-004). The whole-set guarantee is
// what stops a bundle from being split across two buyers.
func TestConcurrentBundleCheckoutsCannotOversell(t *testing.T) {
	f := newCheckoutFixture(t)
	_, day1, day2, pkg := f.seedBundleEvent(t, 1, 1)
	ctx := context.Background()

	const buyers = 8
	var wg sync.WaitGroup
	errs := make([]error, buyers)
	for i := range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Checkout(ctx, bundleCheckoutFor(pkg, day1, day2, 1))
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}

	assert.Equal(t, 1, succeeded, "exactly one buyer may win the last complete set")
	assert.Equal(t, int32(0), testsupport.QuotaOf(t, f.pool, day1.ID), "never negative")
	assert.Equal(t, int32(0), testsupport.QuotaOf(t, f.pool, day2.ID), "never negative")
}

// T049: two overlapping bundles (Day1+Day2, Day2+Day3) bought concurrently over
// many iterations must produce zero SQLSTATE 40P01 deadlock errors — the
// ascending ticket_type_id lock order makes the two transactions always touch
// the shared Day 2 row in the same direction.
func TestOverlappingBundlesNeverDeadlock(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "overlap", "PUBLISHED")
	day1 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 1", "30000.00", 100)
	day2 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 2", "20000.00", 100)
	day3 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 3", "20000.00", 100)

	pkgA := testsupport.SeedPackage(t, f.pool, ev.ID, "Day 1+2", "50000.00", "ACTIVE")
	testsupport.SeedPackageTicket(t, f.pool, pkgA.ID, day1.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, f.pool, pkgA.ID, day2.ID, ev.ID, 1)

	pkgB := testsupport.SeedPackage(t, f.pool, ev.ID, "Day 2+3", "40000.00", "ACTIVE")
	testsupport.SeedPackageTicket(t, f.pool, pkgB.ID, day2.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, f.pool, pkgB.ID, day3.ID, ev.ID, 1)

	ctx := context.Background()
	const iterations = 25
	var wg sync.WaitGroup
	var mu sync.Mutex
	var deadlockErrors int

	for range iterations {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := f.svc.Checkout(ctx, bundleCheckoutFor(pkgA, day1, day2, 1))
			if isDeadlockError(err) {
				mu.Lock()
				deadlockErrors++
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			_, err := f.svc.Checkout(ctx, bundleCheckoutFor(pkgB, day2, day3, 1))
			if isDeadlockError(err) {
				mu.Lock()
				deadlockErrors++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Zero(t, deadlockErrors, "ascending lock order must prevent SQLSTATE 40P01")
	assert.GreaterOrEqual(t, testsupport.QuotaOf(t, f.pool, day2.ID), int32(0), "shared constituent never oversold")
}

// isDeadlockError reports whether err is a Postgres deadlock (SQLSTATE 40P01).
func isDeadlockError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.SQLState() == "40P01"
}

func allOrders(t *testing.T, pool *testsupport.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT status FROM orders ORDER BY created_at`)
	require.NoError(t, err)
	defer rows.Close()

	var out []string
	for rows.Next() {
		var status string
		require.NoError(t, rows.Scan(&status))
		out = append(out, status)
	}
	return out
}

// --- Bundle checkout (US3) --------------------------------------------------

// T045: a bundle checkout deducts each constituent and records ONE order_items
// row at the package's own price.
func TestCheckoutBundleDeductsConstituentsAndStoresOnePackageLine(t *testing.T) {
	f := newCheckoutFixture(t)
	_, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	ctx := context.Background()

	req := order.CheckoutRequest{
		BuyerName:  "Budi Santoso",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+628123456789",
		Items: []order.CheckoutItem{
			{PackageID: &pkg.ID, Quantity: 1},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: day1.ID, PackageID: &pkg.ID, Name: "Budi", Email: "budi@example.com"},
			{TicketTypeID: day2.ID, PackageID: &pkg.ID, Name: "Siti", Email: "siti@example.com"},
		},
	}

	resp, err := f.svc.Checkout(ctx, req)

	require.NoError(t, err)
	assert.Equal(t, "50000.00", resp.TotalAmount.String())
	assert.Equal(t, int32(4), testsupport.QuotaOf(t, f.pool, day1.ID), "Day 1 deducted")
	assert.Equal(t, int32(4), testsupport.QuotaOf(t, f.pool, day2.ID), "Day 2 deducted")

	stored, err := f.repo.GetOrderByNumber(ctx, resp.OrderNumber)
	require.NoError(t, err)
	items, err := f.repo.ListOrderItemsByOrderID(ctx, stored.ID)
	require.NoError(t, err)
	require.Len(t, items, 1, "one order_items row per selected line, not per constituent")
	assert.True(t, items[0].Ref.IsPackage())
	assert.Equal(t, pkg.ID, items[0].Ref.PackageID.UUID)
	assert.Equal(t, int32(1), items[0].Quantity)
	assert.Equal(t, "50000.00", items[0].Price.StringFixed(2))
}

// T046: attendee counts grouped by (ticket_type_id, package_id) must equal the
// server's expansion — one too few and one too many both rejected.
func TestCheckoutBundleRejectsWrongAttendeeCounts(t *testing.T) {
	f := newCheckoutFixture(t)
	_, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	ctx := context.Background()

	valid := order.CheckoutRequest{
		BuyerName:  "Budi",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+62812",
		Items: []order.CheckoutItem{
			{PackageID: &pkg.ID, Quantity: 1},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: day1.ID, PackageID: &pkg.ID, Name: "A", Email: "a@example.com"},
			{TicketTypeID: day2.ID, PackageID: &pkg.ID, Name: "B", Email: "b@example.com"},
		},
	}

	assertBundleMismatch := func(t *testing.T, req order.CheckoutRequest) {
		t.Helper()
		_, err := f.svc.Checkout(ctx, req)
		var appErr *apperr.Error
		require.True(t, errors.As(err, &appErr), "expected an *apperr.Error, got %v", err)
		assert.Equal(t, apperr.CodeAttendeeCountMismatch, appErr.Code)
		assert.Equal(t, int32(5), testsupport.QuotaOf(t, f.pool, day1.ID), "no quota consumed")
		assert.Equal(t, int32(5), testsupport.QuotaOf(t, f.pool, day2.ID), "no quota consumed")
		assert.Zero(t, f.gateway.callCount(), "no gateway call for a rejected bundle")
	}

	t.Run("one too few", func(t *testing.T) {
		req := valid
		req.Attendees = req.Attendees[:1]
		assertBundleMismatch(t, req)
	})

	t.Run("one too many", func(t *testing.T) {
		req := valid
		req.Attendees = append(req.Attendees,
			order.CheckoutAttendee{TicketTypeID: day1.ID, PackageID: &pkg.ID, Name: "C", Email: "c@example.com"})
		assertBundleMismatch(t, req)
	})

	t.Run("wrong constituent", func(t *testing.T) {
		req := valid
		req.Attendees[1].TicketTypeID = day1.ID // two for Day 1, none for Day 2
		assertBundleMismatch(t, req)
	})
}

// T047: a cart whose bundle is available but whose standalone ticket is not
// leaves no quota consumed anywhere — TX1 is all or nothing.
func TestCheckoutBundleAvailableButStandaloneUnavailableConsumesNothing(t *testing.T) {
	f := newCheckoutFixture(t)
	_, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	ctx := context.Background()

	closed := testsupport.SeedTicketTypeWindow(t, f.pool, day1.EventID, "Closed Early", 10,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))

	req := order.CheckoutRequest{
		BuyerName:  "Budi",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+62812",
		Items: []order.CheckoutItem{
			{PackageID: &pkg.ID, Quantity: 1},
			{TicketTypeID: &closed.ID, Quantity: 1},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: day1.ID, PackageID: &pkg.ID, Name: "A", Email: "a@example.com"},
			{TicketTypeID: day2.ID, PackageID: &pkg.ID, Name: "B", Email: "b@example.com"},
			{TicketTypeID: closed.ID, Name: "C", Email: "c@example.com"},
		},
	}

	_, err := f.svc.Checkout(ctx, req)

	require.Error(t, err)
	assert.Equal(t, int32(5), testsupport.QuotaOf(t, f.pool, day1.ID),
		"bundle constituent must not be consumed by a rejected cart")
	assert.Equal(t, int32(5), testsupport.QuotaOf(t, f.pool, day2.ID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, closed.ID))
	assert.Zero(t, f.gateway.callCount(), "no gateway call for a rejected cart")
}
