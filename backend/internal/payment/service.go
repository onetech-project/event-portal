package payment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// ErrOrderNotFound reports that a notification names an order we do not have.
var ErrOrderNotFound = errors.New("payment: order not found")

// ErrProviderHasNoRecord reports that the provider has never heard of an order
// we do hold. It is deliberately distinct from ErrOrderNotFound: the guest is
// looking at an order that exists, so answering "not found" would be a lie. The
// honest answer is that we could not confirm anything.
var ErrProviderHasNoRecord = errors.New("payment: provider has no record of this order")

// fulfillmentTimeout bounds the post-payment goroutine. Ticket generation, PDF
// rendering, and an SMTP round-trip should take well under this; the bound stops a
// hung mail server from leaking a goroutine for the process's lifetime.
const fulfillmentTimeout = 2 * time.Minute

// OrderRef is the slice of an order this domain needs to decide what to do.
type OrderRef struct {
	ID          uuid.UUID
	OrderNumber string
	Status      string
	// PaymentExpiresAt is the deadline the payment stops being accepted at, or
	// nil for an order that never had one. Past it, the order is expired no
	// matter what the provider says.
	PaymentExpiresAt *time.Time
	// TotalAmount and the buyer fields feed the provider request when a QR is
	// re-issued (spec 008 FR-015); buyer fields are nil before checkout.
	TotalAmount decimal.Decimal
	BuyerName   *string
	BuyerEmail  *string
	BuyerPhone  *string
	// PaymentStarted reports whether a QR payload is stamped on the order.
	PaymentStarted bool
}

// QuotaHold is one ticket type's total reserved quantity for an order — how much
// quota to restore when the order is released.
//
// It is a hold, not a line: a package line holds quota in every ticket type it
// contains, so one order line can produce several holds. Reading order lines
// directly would restore nothing for a bundle, since a package line carries no
// ticket type of its own.
type QuotaHold struct {
	TicketTypeID uuid.UUID
	Quantity     int32
}

// OrderProvider is the contract this domain needs from the order domain, declared
// here by its consumer (ARCHITECTURE.md §3.2).
type OrderProvider interface {
	// OrderByNumber resolves the provider's order_id echo, returning
	// ErrOrderNotFound when there is no such order.
	OrderByNumber(ctx context.Context, orderNumber string) (OrderRef, error)
	// QuotaHolds returns what the order actually holds per ticket type, with any
	// package lines already expanded through their composition.
	//
	// It takes the caller's transaction rather than borrowing its own connection:
	// restoration runs inside the transaction that guarded the status change, and
	// asking the pool for a second connection while holding one can exhaust the
	// pool and deadlock under load.
	QuotaHolds(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]QuotaHold, error)
	// UpdateStatusIfPending applies a transition only while the order is still
	// PENDING, reporting whether it actually applied.
	UpdateStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, status string) (bool, error)
	// DueForExpiry returns PENDING orders whose payment deadline has passed,
	// oldest first, capped at limit. It is what lets abandoned orders release
	// their seats without anyone opening the page.
	DueForExpiry(ctx context.Context, now time.Time, limit int32) ([]OrderRef, error)
	// UpdatePaymentQR swaps the order's QR payload WITHOUT touching its
	// deadline (spec 008 FR-015), guarded on the order still being PENDING.
	UpdatePaymentQR(ctx context.Context, orderID uuid.UUID, url, qrString string) (bool, error)
}

// QuotaRestorer is the contract this domain needs from the event domain.
type QuotaRestorer interface {
	RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
}

// EventScopeLookup resolves ticket types back to the events that own them.
//
// It exists because `orders` has no event_id column — an order reaches its event
// only through order_items → ticket_types — while this domain holds nothing but
// []QuotaHold{TicketTypeID, Quantity}. Restoring quota changes what the guest's
// ticket list should say, so the event has to be named somehow, and reaching into
// the event domain's tables to do it would violate Principle II.
//
// Declared here by its consumer (ARCHITECTURE.md §3.2) and satisfied by
// event.Service. Called only AFTER the transaction commits, so the extra indexed
// read costs the webhook nothing while a row lock is held.
type EventScopeLookup interface {
	EventIDsForTicketTypes(ctx context.Context, ticketTypeIDs []uuid.UUID) ([]uuid.UUID, error)
}

// TicketIssuer generates one ticket per attendee once an order is paid.
type TicketIssuer interface {
	IssueTicketsForOrder(ctx context.Context, orderID uuid.UUID) error
}

// TicketDeliverer emails the buyer their tickets.
type TicketDeliverer interface {
	SendTicketEmail(ctx context.Context, orderID uuid.UUID) error
}

// Service handles inbound payment notifications.
type Service struct {
	pool      db.Beginner
	repo      *Repository
	gateway   Gateway
	orders    OrderProvider
	quota     QuotaRestorer
	issuer    TicketIssuer
	deliverer TicketDeliverer
	log       *logger.Logger
	// now is injectable so expiry can be exercised without waiting out a
	// deadline.
	now func() time.Time

	// fulfillment tracks the post-payment goroutines so tests and graceful
	// shutdown can wait for them.
	fulfillment sync.WaitGroup

	// hub fans status transitions out to open SSE streams (spec 008). Always
	// non-nil; publishing with no subscribers is a cheap no-op.
	hub *StreamHub

	// cache is the list cache. Quota restored by a webhook or the sweeper must
	// reach the guest-facing ticket lists, so this domain invalidates too.
	cache cache.Lists
	// eventScopes resolves ticket types to their events, because orders carry no
	// event_id and this domain must not reach into the event domain's tables.
	eventScopes EventScopeLookup
}

// NewService builds the payment service.
func NewService(
	pool db.Beginner,
	repo *Repository,
	gateway Gateway,
	orders OrderProvider,
	quota QuotaRestorer,
	issuer TicketIssuer,
	deliverer TicketDeliverer,
	log *logger.Logger,
) *Service {
	return &Service{
		pool: pool, repo: repo, gateway: gateway, orders: orders,
		quota: quota, issuer: issuer, deliverer: deliverer, log: log,
		now:   time.Now,
		hub:   NewStreamHub(),
		cache: cache.NoOp{},
	}
}

// WithCache installs the list cache and the lookup that resolves a restored
// quota hold's ticket types back to their event (Constitution Principle VII).
//
// Both are optional: without them this service simply invalidates nothing, which
// is what every existing test does.
func (s *Service) WithCache(c cache.Lists, scopes EventScopeLookup) *Service {
	if c != nil {
		s.cache = c
	}
	s.eventScopes = scopes
	return s
}

// Hub exposes the status stream hub (used by tests and diagnostics).
func (s *Service) Hub() *StreamHub { return s.hub }

// WaitForFulfillment blocks until every in-flight post-payment goroutine finishes.
// Used by graceful shutdown and by tests that assert on their effects.
func (s *Service) WaitForFulfillment() {
	s.fulfillment.Wait()
}

// HandleNotification processes one provider notification.
//
// Only a signature failure is reported as an error (the handler turns it into a
// 401). Every authenticated notification is acknowledged with 200 — including ones
// for unknown orders and unrecognized statuses — because a non-2xx answer makes
// the provider retry a notification we can never process. Anything anomalous is
// logged loudly instead.
func (s *Service) HandleNotification(ctx context.Context, provider string, payload []byte, signature string) error {
	result, err := s.gateway.VerifyWebhook(payload, signature)
	if err != nil {
		s.log.WarnContext(ctx, "rejected unverified payment notification", "provider", provider, "error", err.Error())
		return err
	}

	// A re-issued QR lives under a suffixed provider reference
	// ({orderNumber}-R{n}); strip it so every session of the same order feeds
	// the identical idempotent settlement path — first settlement wins.
	orderNumber := stripReissueSuffix(result.OrderNumber)

	ord, err := s.orders.OrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrOrderNotFound) {
		s.log.ErrorContext(ctx, "notification names an unknown order; acknowledging without processing",
			"provider", provider, "order_number", result.OrderNumber)
		return nil
	}
	if err != nil {
		return err
	}

	_, err = s.applyProviderResult(ctx, ord, provider, "webhook", result)
	return err
}

// applyProviderResult is the single path every provider answer travels, whether
// it arrived as a notification or was fetched by a reconciliation.
//
// Sharing it is what makes reconciliation exactly as safe as a webhook: the same
// audit row, the same already-paid short-circuit, the same status mapping, and
// the same guarded transition — so two paths arriving at once still move the
// order (and its quota) exactly once. It reports whether the order actually
// changed.
func (s *Service) applyProviderResult(
	ctx context.Context,
	ord OrderRef,
	provider, source string,
	result *WebhookResult,
) (bool, error) {
	log := s.log.With(
		"provider", provider,
		"source", source,
		"order_number", ord.OrderNumber,
		"provider_status", result.TransactionStatus,
		"fraud_status", result.FraudStatus,
	)

	// Log first: the payments row is the audit trail, and it must exist even for
	// answers that change nothing.
	if err := s.repo.CreatePayment(ctx, PaymentLog{
		OrderID:       ord.ID,
		Provider:      provider,
		TransactionID: result.TransactionID,
		PaymentType:   result.PaymentType,
		Status:        result.TransactionStatus,
		RawResponse:   result.RawPayload,
	}); err != nil {
		return false, err
	}

	// Idempotency short-circuit: a paid order is final, so a replay does no work
	// and cannot re-trigger ticket generation or a second email.
	if ord.Status == OrderStatusPaid {
		log.InfoContext(ctx, "order is already paid; provider result acknowledged as a no-op")
		return false, nil
	}

	outcome := MapProviderStatus(result.TransactionStatus, result.FraudStatus)
	if !outcome.Handled {
		log.ErrorContext(ctx, "unrecognized provider status; order left unchanged and acknowledged")
		return false, nil
	}
	if !outcome.ChangesOrder() {
		log.InfoContext(ctx, "provider result is a legitimate no-op; order left pending")
		return false, nil
	}

	// A success for an order that has already been expired or cancelled is a real
	// discrepancy: the guest may have been charged for seats we have released.
	// The order is deliberately NOT flipped back — that would resurrect quota we
	// no longer hold — but it must be loud enough for an admin to find.
	if outcome.OrderStatus == OrderStatusPaid && ord.Status != OrderStatusPending {
		log.ErrorContext(ctx, "provider reports a successful payment for an order that is no longer pending; needs manual reconciliation",
			"order_status", ord.Status)
		return false, nil
	}

	applied, _, err := s.applyOutcome(ctx, ord, outcome)
	if err != nil {
		return false, err
	}
	if !applied {
		// Another path won the race and already moved the order on. Its quota
		// accounting is that path's responsibility.
		log.InfoContext(ctx, "order was no longer pending; transition skipped")
		return false, nil
	}

	log.InfoContext(ctx, "order status updated", "new_status", outcome.OrderStatus, "quota_restored", outcome.RestoreQuota)

	if outcome.OrderStatus == OrderStatusPaid {
		s.fulfillAsync(ctx, ord)
	}
	return true, nil
}

// expiryBatchSize bounds one sweep. A backlog is drained over successive ticks
// rather than in one transaction-heavy pass that would hold connections the
// request path needs.
const expiryBatchSize = 100

// ExpireDueOrders expires every order whose payment deadline has passed and
// returns their reserved seats to the pool, reporting how many it moved.
//
// The provider does send an `expire` notification, but relying on it alone
// leaves quota held whenever that notification is late or undeliverable — and an
// abandoned cart nobody reopens would hold its seats forever. Each order goes
// through the same guarded transition every other path uses, so a sweep racing a
// webhook still restores quota exactly once.
func (s *Service) ExpireDueOrders(ctx context.Context) (int, error) {
	due, err := s.orders.DueForExpiry(ctx, s.now(), expiryBatchSize)
	if err != nil {
		return 0, err
	}

	expired := 0
	for _, ord := range due {
		applied, err := s.expireIfDue(ctx, ord)
		if err != nil {
			// One bad order must not strand the rest of the batch; the next tick
			// retries it.
			s.log.ErrorContext(ctx, "could not expire an order past its deadline",
				"order_number", ord.OrderNumber, "error", err.Error())
			continue
		}
		if applied {
			expired++
		}
	}
	return expired, nil
}

// reissueSuffix matches the -R{n} tail a re-issued QR's provider reference
// carries (contracts/booking-flow.md §4).
var reissueSuffix = regexp.MustCompile(`-R\d+$`)

// stripReissueSuffix maps any session reference back onto its order number.
func stripReissueSuffix(reference string) string {
	return reissueSuffix.ReplaceAllString(reference, "")
}

// ReissuedQR is what a 7-minute refresh hands back: a fresh payload under the
// same, untouched deadline.
type ReissuedQR struct {
	OrderNumber string
	QRString    string
	// ExpiresAt is the order's existing payment deadline — re-issuing never
	// extends the 14-minute window (FR-015).
	ExpiresAt time.Time
}

// ReissueQR opens a fresh provider session for an order mid-payment and swaps
// the stored QR payload, leaving the deadline untouched (FR-015; spec 008
// research R3 — a QRIS payload's practical scan-life is shorter than the
// payment window, so the screen refreshes it at the 7-minute mark).
//
//	guards  PENDING ∧ payment started (else 409005) ∧ unexpired (else 410001)
//	network Gateway.CreateTransaction("{orderNumber}-R{n}", same total) — no TX
//	write   swap payment_url + payment_qr_string; deadline NOT touched
//
// n is derived from the payments log, where every re-issue is recorded — the
// audit trail that also lets support match a provider reference back to its
// order. A gateway failure leaves the old QR in place (client keeps showing it).
func (s *Service) ReissueQR(ctx context.Context, orderNumber string) (ReissuedQR, error) {
	ord, err := s.orders.OrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrOrderNotFound) {
		return ReissuedQR{}, apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}
	if err != nil {
		return ReissuedQR{}, err
	}

	if ord.Status != OrderStatusPending ||
		(ord.PaymentExpiresAt != nil && s.now().After(*ord.PaymentExpiresAt)) {
		return ReissuedQR{}, apperr.New(http.StatusGone, apperr.CodeOrderExpired,
			"This order can no longer be paid. Please book again.")
	}
	if !ord.PaymentStarted {
		return ReissuedQR{}, apperr.Conflict(apperr.CodePaymentNotStarted,
			"Payment for this order has not started yet.")
	}

	prior, err := s.repo.CountReissuedQRs(ctx, ord.ID)
	if err != nil {
		return ReissuedQR{}, err
	}
	reference := fmt.Sprintf("%s-R%d", ord.OrderNumber, prior+1)

	req := TransactionRequest{
		OrderNumber: reference,
		GrossAmount: ord.TotalAmount,
		Items: []TransactionItem{{
			ID: ord.OrderNumber, Name: "Order " + ord.OrderNumber,
			Price: ord.TotalAmount, Quantity: 1,
		}},
	}
	if ord.BuyerName != nil {
		req.CustomerName = *ord.BuyerName
	}
	if ord.BuyerEmail != nil {
		req.CustomerEmail = *ord.BuyerEmail
	}
	if ord.BuyerPhone != nil {
		req.CustomerPhone = *ord.BuyerPhone
	}

	session, err := s.gateway.CreateTransaction(ctx, req)
	if err != nil {
		s.log.ErrorContext(ctx, "QR re-issue failed; old QR remains live",
			"order_number", ord.OrderNumber, "reference", reference, "error", err.Error())
		return ReissuedQR{}, apperr.Wrap(err, http.StatusBadGateway, apperr.CodePaymentInitiationFailed,
			"We could not refresh the payment code. The previous code may still work.")
	}

	swapped, err := s.orders.UpdatePaymentQR(ctx, ord.ID, session.QRImageURL, session.QRString)
	if err != nil {
		return ReissuedQR{}, err
	}
	if !swapped {
		// The order settled or expired between the guard and the swap.
		return ReissuedQR{}, apperr.New(http.StatusGone, apperr.CodeOrderExpired,
			"This order can no longer be paid.")
	}

	// The audit row is what makes n monotonic and the suffixed reference
	// traceable back to its order.
	if err := s.repo.CreatePayment(ctx, PaymentLog{
		OrderID:       ord.ID,
		Provider:      s.gateway.Name(),
		TransactionID: reference,
		PaymentType:   "qris",
		Status:        "QR_REISSUED",
		RawResponse:   []byte(fmt.Sprintf(`{"reference":%q,"provider_ref":%q}`, reference, session.ProviderRef)),
	}); err != nil {
		// The swap already happened; a lost audit row must not fail the guest.
		s.log.ErrorContext(ctx, "could not record QR re-issue",
			"order_number", ord.OrderNumber, "reference", reference, "error", err.Error())
	}

	s.log.InfoContext(ctx, "payment QR re-issued",
		"order_number", ord.OrderNumber, "reference", reference)

	expiresAt := time.Time{}
	if ord.PaymentExpiresAt != nil {
		expiresAt = ord.PaymentExpiresAt.UTC()
	}
	return ReissuedQR{OrderNumber: ord.OrderNumber, QRString: session.QRString, ExpiresAt: expiresAt}, nil
}

// RefreshResult reports what a reconciliation found.
type RefreshResult struct {
	OrderNumber string
	Status      string
	// Changed reports whether this call actually moved the order, so the guest
	// can be told "confirmed" versus "still waiting" rather than nothing at all.
	Changed bool
}

// RefreshStatus reconciles one order against the payment provider.
//
// It backs the guest's "check payment status" button. A button that only re-read
// our own database would be useless in exactly the situation it exists for — a
// notification that is delayed, lost, or (locally) undeliverable — so this asks
// the provider directly and applies the answer through the same path a webhook
// takes.
func (s *Service) RefreshStatus(ctx context.Context, orderNumber string) (RefreshResult, error) {
	ord, err := s.orders.OrderByNumber(ctx, orderNumber)
	if err != nil {
		return RefreshResult{}, err
	}

	// A settled order cannot change again; do not spend a provider round-trip on
	// it, and do not let a repeated press cost anything.
	if ord.Status != OrderStatusPending {
		return RefreshResult{OrderNumber: ord.OrderNumber, Status: ord.Status}, nil
	}

	// Past its deadline the order is expired whatever the provider says, and the
	// guest deserves that answer now rather than at the next sweep.
	if expired, err := s.expireIfDue(ctx, ord); err != nil {
		return RefreshResult{}, err
	} else if expired {
		return RefreshResult{OrderNumber: ord.OrderNumber, Status: OrderStatusExpired, Changed: true}, nil
	}

	result, err := s.gateway.FetchStatus(ctx, orderNumber)
	if errors.Is(err, ErrOrderNotFound) {
		// We hold the order but the provider does not, so there is nothing to
		// reconcile against. Translated here so the handler cannot mistake it for
		// "no such order" and tell the guest their own order does not exist.
		s.log.ErrorContext(ctx, "provider has no record of an order we hold",
			"order_number", orderNumber)
		return RefreshResult{}, ErrProviderHasNoRecord
	}
	if err != nil {
		return RefreshResult{}, err
	}

	changed, err := s.applyProviderResult(ctx, ord, s.gateway.Name(), "refresh", result)
	if err != nil {
		return RefreshResult{}, err
	}

	// Re-read rather than infer: the winning path may have been a webhook that
	// landed a moment ago.
	current, err := s.orders.OrderByNumber(ctx, orderNumber)
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{OrderNumber: current.OrderNumber, Status: current.Status, Changed: changed}, nil
}

// expiredOutcome is the transition an order past its deadline takes: expired,
// with its reserved seats returned to the pool.
var expiredOutcome = Outcome{OrderStatus: OrderStatusExpired, RestoreQuota: true, Handled: true}

// expireIfDue expires an order whose payment deadline has passed, reporting
// whether it actually applied the transition.
//
// It runs the same guarded transaction every other path runs, so the sweeper,
// a reconciliation, and a provider `expire` notification racing each other still
// restore the quota exactly once.
func (s *Service) expireIfDue(ctx context.Context, ord OrderRef) (bool, error) {
	if ord.Status != OrderStatusPending {
		return false, nil
	}
	if ord.PaymentExpiresAt == nil || ord.PaymentExpiresAt.After(s.now()) {
		return false, nil
	}

	applied, restored, err := s.applyOutcome(ctx, ord, expiredOutcome)
	if err != nil {
		return false, err
	}
	if applied {
		// FR-009: every expired order is auditable from the logs alone — its
		// identity plus exactly which quota went back to the pool.
		lines := make([]string, 0, len(restored))
		for _, hold := range restored {
			lines = append(lines, fmt.Sprintf("%s:+%d", hold.TicketTypeID, hold.Quantity))
		}
		s.log.InfoContext(ctx, "order expired at its payment deadline; quota restored",
			"order_number", ord.OrderNumber,
			"order_id", ord.ID.String(),
			"expired_at", ord.PaymentExpiresAt.UTC(),
			"restored_quota", strings.Join(lines, ","))
	}
	return applied, nil
}

// applyOutcome moves the order and, when the outcome calls for it, restores its
// quota — in one transaction guarded on the order still being PENDING, so a
// replayed notification can never restore quota twice. The holds it restored
// are returned so callers can log the quota lines (FR-009).
func (s *Service) applyOutcome(ctx context.Context, ord OrderRef, outcome Outcome) (bool, []QuotaHold, error) {
	var (
		applied  bool
		restored []QuotaHold
	)

	err := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		restored = nil // reset on a retried transaction
		var err error
		applied, err = s.orders.UpdateStatusIfPending(ctx, tx, ord.ID, outcome.OrderStatus)
		if err != nil || !applied || !outcome.RestoreQuota {
			return err
		}

		// Holds arrive with package lines already expanded and sorted by ticket
		// type — the same order checkout deducts in, which keeps restoration on
		// the same deterministic lock sequence and out of deadlock range.
		holds, err := s.orders.QuotaHolds(ctx, tx, ord.ID)
		if err != nil {
			return err
		}
		for _, hold := range holds {
			if err := s.quota.RestoreQuota(ctx, tx, hold.TicketTypeID, hold.Quantity); err != nil {
				return err
			}
		}
		restored = holds
		return nil
	})

	if err == nil && applied {
		// Open payment screens learn the transition live (SSE); the webhook,
		// the sweeper, and reconciliation all pass through here.
		s.hub.Publish(ord.OrderNumber, StatusEvent{
			OrderID: ord.OrderNumber, Status: outcome.OrderStatus,
			ExpiresAt: ord.PaymentExpiresAt,
		})
		s.invalidateAfterOutcome(ctx, ord, restored)
	}
	return applied, restored, err
}

// invalidateAfterOutcome refreshes the cached lists a status transition made
// wrong. Every path that moves an order — webhook, sweeper, reconciliation —
// funnels through applyOutcome, so this is the one place it has to happen.
//
// Guarded on `applied`, which is false for a replayed notification against an
// already-resolved order: an idempotent no-op wrote nothing and must invalidate
// nothing. Providers retry, and discarding a warm cache on every duplicate
// delivery would be a self-inflicted stampede.
//
// It runs after the transaction has committed and outside it. The event lookup
// below is a second database read: issuing it inside the transaction would take a
// second pool connection while quota row locks are held, which is the same
// starvation this domain avoids elsewhere.
//
// Cost on the webhook's response path is one INCR plus, on a restore, one indexed
// SELECT — small enough to keep here rather than defer, and worth it: a guest
// watching an event page should see returned tickets immediately, not after a PDF
// has been rendered and an email sent.
func (s *Service) invalidateAfterOutcome(ctx context.Context, ord OrderRef, restored []QuotaHold) {
	if s.cache == nil {
		return
	}

	scopes := []cache.Scope{cache.Orders()}

	// Quota went back to the pool, so the guest-facing ticket and package lists
	// for the affected events are now understating availability.
	if len(restored) > 0 && s.eventScopes != nil {
		ticketTypeIDs := make([]uuid.UUID, 0, len(restored))
		for _, hold := range restored {
			ticketTypeIDs = append(ticketTypeIDs, hold.TicketTypeID)
		}
		eventIDs, err := s.eventScopes.EventIDsForTicketTypes(ctx, ticketTypeIDs)
		if err != nil {
			// The order list still gets refreshed below. The ticket lists fall
			// back to their TTL backstop, which is exactly what it is for.
			s.log.ErrorContext(ctx, "could not resolve events for restored quota; their ticket lists may be briefly stale",
				"order_number", ord.OrderNumber, "error", err.Error())
		}
		for _, eventID := range eventIDs {
			scopes = append(scopes, cache.Event(eventID))
		}
	}

	_ = s.cache.Invalidate(ctx, scopes...)
}

// fulfillAsync runs post-payment work off the request path so the provider gets
// its 200 OK immediately (ARCHITECTURE.md §3.4, Constitution Principle IV).
func (s *Service) fulfillAsync(requestCtx context.Context, ord OrderRef) {
	s.fulfillment.Add(1)
	go func() {
		defer s.fulfillment.Done()

		// WithoutCancel keeps the request's values — crucially its trace context —
		// while dropping the cancellation that fires as soon as the 200 is written.
		// Deriving from context.Background() instead would orphan this work from
		// the webhook's trace, which is exactly where you look when a buyer says
		// the email never arrived.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(requestCtx), fulfillmentTimeout)
		defer cancel()

		log := s.log.With("order_number", ord.OrderNumber, "order_id", ord.ID.String())

		if err := s.issuer.IssueTicketsForOrder(ctx, ord.ID); err != nil {
			// Without tickets there is nothing to email, so stop here rather than
			// send the buyer an empty or partial delivery.
			log.ErrorContext(ctx, "ticket generation failed for a paid order", "error", err.Error())
			return
		}

		if err := s.deliverer.SendTicketEmail(ctx, ord.ID); err != nil {
			// The tickets exist and are valid; only delivery failed. email_sent
			// stays false, and an admin can resend from the dashboard.
			log.ErrorContext(ctx, "ticket email delivery failed for a paid order", "error", err.Error())
			return
		}
	}()
}
