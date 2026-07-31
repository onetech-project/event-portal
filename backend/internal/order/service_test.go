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

// fakeGateway stands in for the payment provider so checkout can be driven through
// both its success and its failure path without a network.
type fakeGateway struct {
	mu    sync.Mutex
	url   string
	err   error
	calls []order.PaymentRequest
}

func (g *fakeGateway) Name() string { return "fakegw" }

func (g *fakeGateway) CreateTransaction(_ context.Context, req order.PaymentRequest) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, req)
	if g.err != nil {
		return "", g.err
	}
	return g.url, nil
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
	pool    *testsupport.Pool
	gateway *fakeGateway
	repo    *order.Repository
}

func newCheckoutFixture(t *testing.T) checkoutFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	repo := order.NewRepository(pool)
	gw := &fakeGateway{url: "https://pay.example.com/session"}
	provider := eventProviderAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())}

	return checkoutFixture{
		svc:     order.NewService(pool, repo, provider, gw, testsupport.DiscardLogger()),
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
		Items:      []order.CheckoutItem{{TicketTypeID: tt.ID, Quantity: quantity}},
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
			{TicketTypeID: regular.ID, Quantity: 2},
			{TicketTypeID: vip.ID, Quantity: 1},
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
			{TicketTypeID: plenty.ID, Quantity: 2},
			{TicketTypeID: scarce.ID, Quantity: 2},
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
	req.Items[0].TicketTypeID = ghost
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
