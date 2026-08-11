package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/db"
)

// Recovering a payment whose notification never arrived (User Story 6).
//
// The gateway's automatic retry budget is finite — three attempts, ten seconds
// apart, with no dead-letter — so a deploy, a restart, or a brief database
// outage inside that window strands a guest who genuinely paid.
//
// The recovery is not a human path back into this system. It is asking the
// gateway to redeliver that transaction's notification, which then settles the
// order through settleExpiredOrder below — reaching the same fulfilment every
// ordinary purchase reaches. Nothing here records that a payment happened
// (FR-022d): the gateway stays the single source of payment truth, so there is
// exactly one code path that turns a payment into tickets, and it is the one
// exercised on every purchase rather than one that only runs on the worst day of
// the month.
//
// What this system contributes to the rescue is narrow: two read views an
// operator consults first, and a settle that runs when the gateway says so.

// ErrQuotaUnavailable reports that an order's released seats have been resold,
// so settling it would oversell the event.
var ErrQuotaUnavailable = errors.New("payment: the order's seats are no longer available")

// QuotaShortfall is one ticket type that cannot cover an order's hold, and by
// how much. It is the number an operator needs to size a top-up before asking
// the gateway to resend (FR-019c).
type QuotaShortfall struct {
	TicketTypeID   uuid.UUID
	TicketTypeName string
	Required       int32
	Remaining      int32
}

// SettleRefusedError reports that a redelivered notification could not settle an
// order because its seats have been resold.
//
// It is an error because the settle genuinely did not happen — but the handler
// MUST NOT turn it into a non-200. The gateway would spend its whole retry
// budget on an attempt guaranteed to fail identically, and the notification
// would be lost at the end of it. It is answered 200 with a body naming the
// shortfall (FR-019c); the gateway does not read response bodies, so that body
// is for our own record and for the operator reading it.
type SettleRefusedError struct {
	OrderNumber string
	Shortfalls  []QuotaShortfall
}

func (e *SettleRefusedError) Error() string {
	return fmt.Sprintf("payment: order %s cannot be settled; %d ticket type(s) short of quota",
		e.OrderNumber, len(e.Shortfalls))
}

// TicketTypeQuota is one ticket type's name beside how many seats it has left.
// Remaining is the live counter, never the original allocation.
type TicketTypeQuota struct {
	Name      string
	Remaining int32
}

// QuotaReserver is the settle path's whole view of the event domain: taking
// seats back, and reading how many are left.
//
// Every other path in this domain releases quota; the settle is the one that
// takes it. The read sits on the same interface because it exists only to
// explain a failed take — sizing the shortfall a refusal reports, and answering
// the holds view an operator reads before requesting a resend.
type QuotaReserver interface {
	// ReserveQuota deducts qty seats inside the caller's transaction, returning
	// ErrQuotaUnavailable when it cannot.
	ReserveQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
	// RemainingQuota reports what each ticket type has left. Read-only and
	// outside any transaction: it informs a person, it does not guard a
	// deduction. ReserveQuota is what makes overselling impossible, and it does
	// so under a row lock this must not hold.
	RemainingQuota(ctx context.Context, ticketTypeIDs []uuid.UUID) (map[uuid.UUID]TicketTypeQuota, error)
}

// OrderReleaser is the one transition the settle needs and nothing else does:
// moving an order back out of the state its own deadline put it in.
//
// It is deliberately not a general "move from A to B". FR-019d forbids a
// completion reviving an order the *gateway* rejected or cancelled — expiry is
// this system's own verdict, reached because a notification never came, so
// reversing it is honest; reversing the gateway's would contradict the source of
// truth this whole design rests on. A method that cannot express the forbidden
// transition enforces that better than a caller who has to remember not to ask.
type OrderReleaser interface {
	// SettleExpired moves an order from EXPIRED to PAID, reporting whether it
	// applied. Guarded on the order still being EXPIRED, so two deliveries of the
	// same resend produce one transition, not two — and the re-deduction that
	// accompanies it runs once rather than twice.
	SettleExpired(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error)
}

// NotificationRecord is one row of an order's payment history as staff read it.
type NotificationRecord struct {
	ID            uuid.UUID
	Provider      string
	TransactionID string
	// Status is the gateway's raw status, or a marker. IsMarker says which.
	Status   string
	IsMarker bool
	// RawPayload is the body exactly as it arrived, so staff can match it against
	// the gateway's own dashboard.
	RawPayload  json.RawMessage
	ReceivedAt  time.Time
	PaymentType string
}

// OrderHold is one ticket type's seats on an order beside what that type has
// left — the pair an operator needs to size a top-up (FR-022e).
type OrderHold struct {
	TicketTypeID   uuid.UUID
	TicketTypeName string
	Held           int32
	Remaining      int32
}

// OrderNotifications returns every notification recorded against an order,
// accepted or refused, newest first (FR-022c).
//
// This is the whole of this system's part in the rescue: it lets an operator
// match the order against the gateway's own dashboard before asking for a
// resend. It reads nothing but `payments`, so no cross-domain JOIN appears
// (Principle II).
func (s *Service) OrderNotifications(ctx context.Context, orderID uuid.UUID) ([]NotificationRecord, error) {
	rows, err := s.repo.ListByOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	out := make([]NotificationRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, NotificationRecord{
			ID:            row.ID,
			Provider:      row.Provider,
			TransactionID: row.TransactionID,
			Status:        row.Status,
			IsMarker:      isMarker(row.Status),
			RawPayload:    json.RawMessage(validJSONOrNull(row.RawResponse)),
			ReceivedAt:    row.CreatedAt,
			PaymentType:   row.PaymentType,
		})
	}
	return out, nil
}

// OrderHolds reports what an order holds per ticket type against what remains
// (FR-022e).
//
// This figure exists nowhere else in the admin surface. The sold count on the
// ticket-type editor counts released orders too, so it overstates what has been
// sold — an operator sizing a top-up from it would add too few seats and the
// resend would be refused a second time on the same shortfall.
func (s *Service) OrderHolds(ctx context.Context, orderID uuid.UUID) ([]OrderHold, error) {
	var holds []QuotaHold
	err := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		holds, err = s.orders.QuotaHolds(ctx, tx, orderID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(holds) == 0 {
		return []OrderHold{}, nil
	}

	quotas, err := s.reserver.RemainingQuota(ctx, ticketTypeIDs(holds))
	if err != nil {
		return nil, err
	}

	out := make([]OrderHold, 0, len(holds))
	for _, hold := range holds {
		quota := quotas[hold.TicketTypeID]
		out = append(out, OrderHold{
			TicketTypeID:   hold.TicketTypeID,
			TicketTypeName: quota.Name,
			Held:           hold.Quantity,
			Remaining:      quota.Remaining,
		})
	}
	return out, nil
}

// settleExpiredOrder settles an order this system had already expired, on the
// strength of a redelivered notification saying it was paid (FR-019).
//
// The transaction is the whole guarantee. The transition goes first, guarded on
// the order still being EXPIRED: reserving before the guard would deduct seats
// for a transition that then loses a race, removing quota for an order nobody is
// paying for. The seats are then taken back in the same transaction, in full or
// not at all (FR-019b) — a partial settle is never an outcome, because half an
// order is not something this system can issue tickets for.
func (s *Service) settleExpiredOrder(ctx context.Context, ord OrderRef, provider string, result *WebhookResult) error {
	log := s.log.With("order_number", ord.OrderNumber, "provider", provider,
		"transaction_id", result.TransactionID)

	if s.reserver == nil || s.releaser == nil {
		// A deployment wired without the settle capability. Loud and retryable on
		// purpose: this refuses a payment the gateway confirmed, and a redeploy
		// inside the retry window would genuinely fix it.
		log.ErrorContext(ctx, "cannot settle an expired order: the settle capability is not wired")
		return errors.New("payment: settle capability unavailable")
	}

	var (
		applied bool
		holds   []QuotaHold
	)
	err := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		applied, err = s.releaser.SettleExpired(ctx, tx, ord.ID)
		if err != nil || !applied {
			return err
		}

		holds, err = s.orders.QuotaHolds(ctx, tx, ord.ID)
		if err != nil {
			return err
		}
		// The same deterministic ticket-type order checkout deducts in, which keeps
		// this out of deadlock range with a concurrent purchase.
		for _, hold := range holds {
			if err := s.reserver.ReserveQuota(ctx, tx, hold.TicketTypeID, hold.Quantity); err != nil {
				// Rolls the transition back too — the settle happens whole or not at all.
				return err
			}
		}
		return nil
	})

	if errors.Is(err, ErrQuotaUnavailable) {
		return s.refuseSettle(ctx, ord, provider, result, holds)
	}
	if err != nil {
		return err
	}
	if !applied {
		// Another delivery of the same resend already settled it, or the order moved
		// on between the read and the write. Either way this delivery has nothing to
		// do, and the winner owns the quota accounting (US6 scenario 5).
		log.InfoContext(ctx, "order was no longer expired; settle skipped")
		return nil
	}

	// Not routine traffic: this order's deadline had already released its seats,
	// and someone should be able to see that it came back (FR-019).
	log.ErrorContext(ctx, "expired order settled by a redelivered payment notification",
		"marker", MarkerSettledAfterExpiry, "holds_retaken", len(holds))
	s.writeMarker(ctx, ord, provider, MarkerSettledAfterExpiry, result, map[string]any{
		"order_status_at_arrival": ord.Status,
		"quota_lines_retaken":     holds,
	})

	s.hub.Publish(ord.OrderNumber, StatusEvent{
		OrderID: ord.OrderNumber, Status: OrderStatusPaid, ExpiresAt: ord.PaymentExpiresAt,
	})

	// The same fulfilment a first-time notification reaches. Using one path is the
	// point: a second, recovery-only issuance route would be a second place for the
	// exactly-once guarantee to be wrong, and the one nobody exercises.
	s.fulfillAsync(ctx, ord)
	return nil
}

// refuseSettle records a settle that could not be honoured and reports the
// shortfall back to the caller (FR-019c).
//
// Nothing has moved by the time this runs: the transaction rolled back, so the
// order is still expired, no tickets exist, and no quota changed. What is left is
// to say why, in a form an operator can act on.
func (s *Service) refuseSettle(
	ctx context.Context,
	ord OrderRef,
	provider string,
	result *WebhookResult,
	holds []QuotaHold,
) error {
	shortfalls := s.shortfalls(ctx, holds)

	s.log.ErrorContext(ctx, "redelivered payment notification cannot settle the order: its seats have been resold",
		"order_number", ord.OrderNumber, "provider", provider,
		"transaction_id", result.TransactionID,
		"marker", MarkerSettleRefusedNoQuota, "short_ticket_types", len(shortfalls))

	s.writeMarker(ctx, ord, provider, MarkerSettleRefusedNoQuota, result, map[string]any{
		"order_status_at_arrival": ord.Status,
		"shortfall":               shortfalls,
	})

	return &SettleRefusedError{OrderNumber: ord.OrderNumber, Shortfalls: shortfalls}
}

// shortfalls names which of an order's holds the ticket types can no longer
// cover, and by how much.
//
// A failure to read the remaining counts costs the operator a number, not the
// refusal itself: the settle is refused either way, and turning a diagnostic read
// into a callback failure would trade a missing figure for a lost notification.
func (s *Service) shortfalls(ctx context.Context, holds []QuotaHold) []QuotaShortfall {
	short := make([]QuotaShortfall, 0, len(holds))
	if len(holds) == 0 {
		return short
	}

	quotas, err := s.reserver.RemainingQuota(ctx, ticketTypeIDs(holds))
	if err != nil {
		s.log.ErrorContext(ctx, "could not read remaining quota to size the shortfall; refusing without it",
			"error", err.Error())
		return short
	}

	for _, hold := range holds {
		quota := quotas[hold.TicketTypeID]
		if quota.Remaining >= hold.Quantity {
			continue
		}
		short = append(short, QuotaShortfall{
			TicketTypeID:   hold.TicketTypeID,
			TicketTypeName: quota.Name,
			Required:       hold.Quantity,
			Remaining:      quota.Remaining,
		})
	}
	return short
}

func ticketTypeIDs(holds []QuotaHold) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(holds))
	for _, hold := range holds {
		ids = append(ids, hold.TicketTypeID)
	}
	return ids
}

func isMarker(status string) bool {
	switch status {
	case MarkerDisputed, MarkerSettledAfterExpiry, MarkerSettleRefusedNoQuota, MarkerSessionDuplicate:
		return true
	default:
		return false
	}
}

// envelopeString reads one string field out of a marker's envelope, tolerating
// anything unexpected: a display value is never worth failing a read for.
func envelopeString(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	if s, ok := fields[key].(string); ok {
		return s
	}
	return fmt.Sprintf("%v", fields[key])
}
