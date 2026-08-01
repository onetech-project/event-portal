package payment

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
}

// LineItem is one reserved quantity, used to know how much quota to restore.
type LineItem struct {
	TicketTypeID uuid.UUID
	Quantity     int32
}

// OrderProvider is the contract this domain needs from the order domain, declared
// here by its consumer (ARCHITECTURE.md §3.2).
type OrderProvider interface {
	// OrderByNumber resolves the provider's order_id echo, returning
	// ErrOrderNotFound when there is no such order.
	OrderByNumber(ctx context.Context, orderNumber string) (OrderRef, error)
	// LineItems returns the order's reserved quantities.
	LineItems(ctx context.Context, orderID uuid.UUID) ([]LineItem, error)
	// UpdateStatusIfPending applies a transition only while the order is still
	// PENDING, reporting whether it actually applied.
	UpdateStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, status string) (bool, error)
	// DueForExpiry returns PENDING orders whose payment deadline has passed,
	// oldest first, capped at limit. It is what lets abandoned orders release
	// their seats without anyone opening the page.
	DueForExpiry(ctx context.Context, now time.Time, limit int32) ([]OrderRef, error)
}

// QuotaRestorer is the contract this domain needs from the event domain.
type QuotaRestorer interface {
	RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
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
		now: time.Now,
	}
}

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

	ord, err := s.orders.OrderByNumber(ctx, result.OrderNumber)
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

	applied, err := s.applyOutcome(ctx, ord, outcome)
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

	applied, err := s.applyOutcome(ctx, ord, expiredOutcome)
	if err != nil {
		return false, err
	}
	if applied {
		s.log.InfoContext(ctx, "order expired at its payment deadline; quota restored",
			"order_number", ord.OrderNumber, "expired_at", ord.PaymentExpiresAt.UTC())
	}
	return applied, nil
}

// applyOutcome moves the order and, when the outcome calls for it, restores its
// quota — in one transaction guarded on the order still being PENDING, so a
// replayed notification can never restore quota twice.
func (s *Service) applyOutcome(ctx context.Context, ord OrderRef, outcome Outcome) (bool, error) {
	var applied bool

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		applied, err = s.orders.UpdateStatusIfPending(ctx, tx, ord.ID, outcome.OrderStatus)
		if err != nil || !applied || !outcome.RestoreQuota {
			return err
		}

		items, err := s.orders.LineItems(ctx, ord.ID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := s.quota.RestoreQuota(ctx, tx, item.TicketTypeID, item.Quantity); err != nil {
				return err
			}
		}
		return nil
	})

	return applied, err
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
