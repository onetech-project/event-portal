package order_test

import (
	"context"
	"errors"
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
		// Mirrors cmd/api: dropping it here would leave every availability check
		// in these tests judging demand against a remaining quota of zero.
		QuotaRemaining: row.Quota,
		// Also mirrors cmd/api (spec 022). Dropping it would silently disable the
		// FR-008 refusal in every test that goes through this adapter, so the
		// seam would look guarded while being wide open.
		IsVisible: row.IsVisible,
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
	info := order.EventTermsInfo{ID: row.ID, EventID: eventID}
	if row.UpdatedAt != nil {
		info.UpdatedAt = *row.UpdatedAt
	}
	return info, nil
}

// Mirrors the production adapter (cmd/api/adapters.go) exactly, including the
// collapse of every event-domain "not found" onto ONE sentinel — spec 022 FR-012
// forbids the wire distinguishing them, and a test adapter that distinguished
// them would let a regression through.
func (a eventProviderAdapter) RegistrationTargetBySlug(ctx context.Context, slug string, ticketTypeID uuid.UUID) (order.RegistrationTarget, error) {
	target, err := a.svc.RegistrationTargetBySlug(ctx, slug, ticketTypeID)
	if errors.Is(err, event.ErrNotFound) {
		return order.RegistrationTarget{}, order.ErrRegistrationTargetMissing
	}
	if err != nil {
		return order.RegistrationTarget{}, err
	}
	return order.RegistrationTarget{
		TicketTypeID:   target.TicketTypeID,
		TicketTypeName: target.TicketTypeName,
		EventID:        target.EventID,
		EventName:      target.EventName,
		EventSlug:      target.EventSlug,
		IsVisible:      target.IsVisible,
		SalesStart:     target.SalesStart,
		SalesEnd:       target.SalesEnd,
		QuotaRemaining: target.QuotaRemaining,
	}, nil
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
	// providerRef overrides the reference this gateway issues. Empty string means
	// "issue the default"; use noProviderRef to model a gateway that opens a
	// session without supplying one at all.
	providerRef string
	// records and orders let the fake do what the composition root does for real:
	// write the reference down the moment the session opens, so the branches that
	// rebuild a response from storage have something to read (spec 017).
	records *fakePaymentRecords
	orders  *order.Repository
}

// noProviderRef models a gateway that opened a session but supplied no external
// reference. Distinct from the zero value, which means "use the default".
const noProviderRef = "\x00none"

// fakePaymentRecords stands in for the payment domain's record of what a session
// was opened under — the read half of what cmd/api wires for real.
type fakePaymentRecords struct {
	mu   sync.Mutex
	refs map[uuid.UUID]string
	err  error
}

func (r *fakePaymentRecords) ExternalRefForOrder(_ context.Context, orderID uuid.UUID) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return "", r.err
	}
	return r.refs[orderID], nil
}

func (r *fakePaymentRecords) record(orderID uuid.UUID, ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs[orderID] = ref
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

	ref := "txn-" + req.OrderNumber
	switch g.providerRef {
	case noProviderRef:
		ref = ""
	case "":
		// default
	default:
		ref = g.providerRef
	}

	// What the composition root does the moment the gateway agrees: write the
	// reference down, keyed by the order, so the answers rebuilt from storage can
	// find it. Resolving the id from the number is the same step
	// RecordSessionOpened takes.
	if g.records != nil && g.orders != nil {
		if ord, err := g.orders.GetOrderByNumber(context.Background(), req.OrderNumber); err == nil {
			g.records.record(ord.ID, ref)
		}
	}

	return order.PaymentSession{
		ProviderRef: ref,
		QRString:    g.qrString,
		QRImageURL:  g.url,
		// The deadline is the gateway's, adopted verbatim. The adapter guarantees
		// it is always set, substituting a fallback when the gateway returned
		// nothing usable — so a stub that returns a zero time here would be
		// modelling something the real adapter cannot produce.
		ExpiresAt:         g.expiresAt,
		ExpiryFromGateway: true,
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
	records *fakePaymentRecords
}

func newCheckoutFixture(t *testing.T) checkoutFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	repo := order.NewRepository(pool)
	records := &fakePaymentRecords{refs: map[uuid.UUID]string{}}
	gw := &fakeGateway{
		url:       "https://pay.example.com/session",
		qrString:  "00020101021226620014COM.EXAMPLE.QRIS",
		expiresAt: time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second),
		records:   records,
		orders:    repo,
	}
	events := event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())
	provider := eventProviderAdapter{svc: events}

	return checkoutFixture{
		svc: order.NewService(pool, repo, provider, gw, testsupport.DiscardLogger()).
			WithPaymentRecords(records),
		public:  order.NewPublicService(repo, eventLookupAdapter{svc: events}),
		pool:    pool,
		gateway: gw,
		repo:    repo,
		records: records,
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
	pkg := testsupport.SeedPackage(t, f.pool, ev.ID, "Day 1+2 Bundle", "50000.00", true)
	testsupport.SeedPackageTicket(t, f.pool, pkg.ID, day1.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, f.pool, pkg.ID, day2.ID, ev.ID, 1)
	return ev, day1, day2, pkg
}

// bundleBookFor builds a live booking request (spec 008) buying quantity units
// of a package — items only, no visitor identity yet (Option B).
func bundleBookFor(eventID, packageID uuid.UUID, quantity int32) order.BookRequest {
	pkgID := packageID
	return order.BookRequest{
		EventID: eventID,
		Items:   []order.CheckoutItem{{PackageID: &pkgID, Quantity: quantity}},
	}
}

// --- Concurrency (ported from the pre-008 checkout path to Book) ------------
//
// In-flight bookings are capped at 2 per test (the same shape as
// TestConcurrentBooksCannotOversellTheLastTicket): Book reads the fee master
// through the pool while its transaction already holds a connection and row
// locks, so flooding a MaxConns-sized pool with bookings would starve the fee
// read and hang on pool exhaustion rather than exercise the row-lock ordering
// under test.

// Buyers contend pairwise for one remaining complete set; exactly one wins,
// quotas land at 0, never negative (SC-003, SC-004). The whole-set guarantee is
// what stops a bundle from being split across two buyers.
func TestConcurrentBundleBooksCannotOversell(t *testing.T) {
	f := newCheckoutFixture(t)
	ev, day1, day2, pkg := f.seedBundleEvent(t, 1, 1)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	ctx := context.Background()

	const buyers = 8
	inFlight := make(chan struct{}, 2)
	var wg sync.WaitGroup
	errs := make([]error, buyers)
	for i := range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inFlight <- struct{}{}
			defer func() { <-inFlight }()
			_, errs[i] = f.svc.Book(ctx, bundleBookFor(ev.ID, pkg.ID, 1))
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

// Two overlapping bundles (Day1+Day2, Day2+Day3) booked concurrently in
// opposing order over many iterations must produce zero SQLSTATE 40P01 deadlock
// errors — the ascending ticket_type_id lock order makes the two transactions
// always touch the shared Day 2 row in the same direction. Every attempt either
// completes or fails cleanly (insufficient quota), never with a pgx deadlock.
func TestOverlappingBundleBooksNeverDeadlock(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "overlap", "PUBLISHED")
	day1 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 1", "30000.00", 100)
	day2 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 2", "20000.00", 100)
	day3 := testsupport.SeedTicketType(t, f.pool, ev.ID, "Day 3", "20000.00", 100)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	pkgA := testsupport.SeedPackage(t, f.pool, ev.ID, "Day 1+2", "50000.00", true)
	testsupport.SeedPackageTicket(t, f.pool, pkgA.ID, day1.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, f.pool, pkgA.ID, day2.ID, ev.ID, 1)

	pkgB := testsupport.SeedPackage(t, f.pool, ev.ID, "Day 2+3", "40000.00", true)
	testsupport.SeedPackageTicket(t, f.pool, pkgB.ID, day2.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, f.pool, pkgB.ID, day3.ID, ev.ID, 1)

	ctx := context.Background()
	const iterations = 25
	var deadlockErrors int

	// Each iteration races the two opposing bundles head-to-head, so their
	// transactions overlap on the shared Day 2 row in the same instant.
	for range iterations {
		var wg sync.WaitGroup
		results := make([]error, 2)
		for i, pkgID := range []uuid.UUID{pkgA.ID, pkgB.ID} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, results[i] = f.svc.Book(ctx, bundleBookFor(ev.ID, pkgID, 1))
			}()
		}
		wg.Wait()
		for _, err := range results {
			if isDeadlockError(err) {
				deadlockErrors++
			}
		}
	}

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

// --- External reference: the lost stamping race (spec 017 FR-011) ----------

// The branch that makes reading the reference necessary rather than merely
// convenient.
//
// A checkout can lose TX-P to a concurrent one and be served the payload the
// winner stored. Answering from this caller's own `session.ProviderRef` would
// then name a session the order did not keep — an identifier for a payment
// nobody is making. So the answer is read back from the record instead, and this
// asserts that it is.
//
// Only the winner's session is recorded here, which is what the live gateway
// guarantees: it refuses to open a second session under a reference it has
// already issued, so an order never has two. (A gateway that permitted duplicates
// could record two, and the newest-first lookup could then resolve the wrong one
// — the limitation research.md Decision 2 accepts and states rather than designs
// around.)
//
// onCall runs inside CreateTransaction, exactly the window a concurrent winner
// would land in, and the win goes through the same guarded UPDATE production
// uses rather than a hand-written one.
func TestCheckoutLosingTheStampRaceNamesTheStoredSessionNotItsOwn(t *testing.T) {
	f := newCheckoutFixture(t)
	ctx := context.Background()
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	// This caller's own session is deliberately NOT recorded: see above.
	f.gateway.records = nil

	f.gateway.onCall = func(req order.PaymentRequest) {
		stored, err := f.repo.GetOrderByNumber(ctx, req.OrderNumber)
		require.NoError(t, err)

		tx, err := f.pool.Begin(ctx)
		require.NoError(t, err)
		stamped, err := f.repo.UpdatePaymentDetailsIfUnstarted(ctx, tx, stored.ID, order.PaymentDetails{
			PaymentURL: "https://pay.example.com/winner",
			Provider:   "fakegw",
			QRString:   "WINNER-QR-PAYLOAD",
			ExpiresAt:  time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second),
		})
		require.NoError(t, err)
		require.True(t, stamped, "the concurrent checkout wins the stamp")
		require.NoError(t, tx.Commit(ctx))

		// The winner records its session, as the composition root would.
		f.records.record(stored.ID, "txn-winner")
	}

	resp, err := f.svc.CheckoutOrder(ctx, orderNumber, formsFor(t, f, slotIDs))
	require.NoError(t, err)

	assert.Equal(t, "WINNER-QR-PAYLOAD", resp.QRString,
		"the guest is served the code the order actually holds")
	assert.Equal(t, "txn-winner", resp.ExtRefID,
		"and the reference for THAT session, not the one this caller opened")
	assert.NotEqual(t, "txn-"+orderNumber, resp.ExtRefID,
		"answering from the loser's own session would name a payment nobody is making")
}
