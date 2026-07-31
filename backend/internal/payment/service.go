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

// fulfillmentTimeout bounds the post-payment goroutine. Ticket generation, PDF
// rendering, and an SMTP round-trip should take well under this; the bound stops a
// hung mail server from leaking a goroutine for the process's lifetime.
const fulfillmentTimeout = 2 * time.Minute

// OrderRef is the slice of an order the webhook needs to decide what to do.
type OrderRef struct {
	ID          uuid.UUID
	OrderNumber string
	Status      string
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

	log := s.log.With(
		"provider", provider,
		"order_number", result.OrderNumber,
		"provider_status", result.TransactionStatus,
		"fraud_status", result.FraudStatus,
	)

	ord, err := s.orders.OrderByNumber(ctx, result.OrderNumber)
	if errors.Is(err, ErrOrderNotFound) {
		log.ErrorContext(ctx, "notification names an unknown order; acknowledging without processing")
		return nil
	}
	if err != nil {
		return err
	}

	// Log first: the payments row is the audit trail, and it must exist even for
	// notifications that change nothing.
	if err := s.repo.CreatePayment(ctx, PaymentLog{
		OrderID:       ord.ID,
		Provider:      provider,
		TransactionID: result.TransactionID,
		PaymentType:   result.PaymentType,
		Status:        result.TransactionStatus,
		RawResponse:   result.RawPayload,
	}); err != nil {
		return err
	}

	// Idempotency short-circuit: a paid order is final, so a replay does no work
	// and cannot re-trigger ticket generation or a second email.
	if ord.Status == OrderStatusPaid {
		log.InfoContext(ctx, "order is already paid; notification acknowledged as a no-op")
		return nil
	}

	outcome := MapProviderStatus(result.TransactionStatus, result.FraudStatus)
	if !outcome.Handled {
		log.ErrorContext(ctx, "unrecognized provider status; order left unchanged and acknowledged")
		return nil
	}
	if !outcome.ChangesOrder() {
		log.InfoContext(ctx, "notification is a legitimate no-op; order left pending")
		return nil
	}

	applied, err := s.applyOutcome(ctx, ord, outcome)
	if err != nil {
		return err
	}
	if !applied {
		// Another notification won the race and already moved the order on. Its
		// quota accounting is that path's responsibility.
		log.InfoContext(ctx, "order was no longer pending; transition skipped")
		return nil
	}

	log.InfoContext(ctx, "order status updated", "new_status", outcome.OrderStatus, "quota_restored", outcome.RestoreQuota)

	if outcome.OrderStatus == OrderStatusPaid {
		s.fulfillAsync(ctx, ord)
	}
	return nil
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
