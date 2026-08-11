package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// ErrOrderNotFound reports that a notification names an order we do not have.
var ErrOrderNotFound = errors.New("payment: order not found")

// Notifications this system records but deliberately does not act on, in a way a
// person should know about.
//
// Each is an error because something is wrong — but none may become a non-200.
// The gateway retries any other answer three times, ten seconds apart, with no
// dead-letter, so refusing one of these costs four deliveries and still ends in
// the notification being lost (FR-012c). They are answered 200 carrying their own
// envelope code, which is what tells them apart from an acknowledgement on an
// identical status line (FR-012g).
//
// The set is exactly the set that raises an operational signal. That is the rule
// rather than a coincidence: if an outcome is worth telling a human about, the
// caller is told too. Outcomes that are merely uneventful — a payment applied, a
// pending notification, a duplicate retry repeating an outcome already
// recorded — stay plain successes, because at-least-once delivery makes them the
// normal case and flagging them would bury these five in routine noise.
var (
	// ErrNotificationUnknownOrder: the reference matches no order of ours. Either
	// the gateway is misconfigured against another tenant or an order vanished;
	// both need a human and neither is fixed by a retry (FR-013).
	ErrNotificationUnknownOrder = errors.New("payment: notification names no order of ours")

	// ErrNotificationNotDeposit: a withdrawal arrived on an endpoint that only
	// ever expects deposits (FR-020).
	ErrNotificationNotDeposit = errors.New("payment: notification is not a deposit")

	// ErrNotificationUnknownStatus: Obscure, or a value this build has never seen.
	// Either may be a status that should have released quota (FR-014).
	ErrNotificationUnknownStatus = errors.New("payment: gateway status not recognised")

	// ErrNotificationContradiction: the gateway now reports a failure for an order
	// already paid, whose tickets are already issued. Nothing is reversed
	// automatically; a person chooses between refunding and honouring the ticket
	// (FR-016b, FR-016d).
	ErrNotificationContradiction = errors.New("payment: notification contradicts a settled payment")

	// ErrNotificationOrderCancelled: a completed payment for an order the gateway
	// itself rejected or cancelled. Not the lost-notification case, so it is never
	// revived — the money is real and needs a person (FR-019d, FR-007f).
	ErrNotificationOrderCancelled = errors.New("payment: completed payment for a cancelled order")
)

// Marker statuses written into payments.status alongside — never instead of —
// the audit row for the payload that triggered them (FR-018).
//
// A marker records what was true at the moment a decision was taken: the order's
// status when a contradiction arrived, the lines re-taken when a settle
// succeeded, the shortfall when one was refused. None of that is recoverable
// afterwards — quota moves on, statuses move on — so it is written here, where
// the order's status is already loaded, rather than re-derived later by a query
// that would have to JOIN payments to orders across a domain boundary
// (Principle II).
const (
	// MarkerDisputed records a terminal-failure notification arriving for an
	// order already PAID. The order is NOT moved (FR-016a) — this is a statement
	// that two sources disagree, for a human to settle.
	MarkerDisputed = "DISPUTED"
	// MarkerSettledAfterExpiry records a redelivered Completed notification
	// reviving an order this system had already expired (FR-019). It answers the
	// question an oversold event provokes: why does this ticket type's issued
	// count exceed the allocation it was given?
	MarkerSettledAfterExpiry = "SETTLED_AFTER_EXPIRY"
	// MarkerSettleRefusedNoQuota records a redelivered Completed notification
	// that could not be honoured because the seats have been resold (FR-019c). It
	// carries the shortfall per ticket type, which is the number an operator needs
	// to size the top-up before asking for another resend.
	MarkerSettleRefusedNoQuota = "SETTLE_REFUSED_NO_QUOTA"
	// MarkerSessionDuplicate records a session-open refused as a duplicate
	// reference — a code exists that this system will never hold.
	MarkerSessionDuplicate = "SESSION_DUPLICATE"
)

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
	// nil for an order that never had one. It is now the gateway's own expiry
	// rather than a figure this system computed. Past it, the order is expired no
	// matter what the gateway later says.
	PaymentExpiresAt *time.Time
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
	// OrderByNumber resolves the gateway's reference echo, returning
	// ErrOrderNotFound when there is no such order.
	OrderByNumber(ctx context.Context, orderNumber string) (OrderRef, error)
	// OrderByID resolves an order the payments log already points at. The admin
	// read views work from payment rows, which carry order_id and not the order
	// number.
	OrderByID(ctx context.Context, orderID uuid.UUID) (OrderRef, error)
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
	// reserver and releaser back the settle path: a redelivered Completed
	// notification reviving an expired order has to take its seats back
	// (FR-019b). They are constructor arguments rather than optional extras
	// because the callback path now depends on them — a deployment missing them
	// would silently refuse a payment the gateway confirmed, on the path least
	// likely to be exercised before it matters.
	//
	// Safety comes from what releaser CAN express, not from who holds it:
	// SettleExpired moves an order out of EXPIRED and nowhere else, so no caller
	// can revive one the gateway itself cancelled (FR-019d).
	reserver QuotaReserver
	releaser OrderReleaser
	// now is injectable so expiry can be exercised without waiting out a
	// deadline.
	now func() time.Time

	// fulfillment tracks the post-payment goroutines so tests and graceful
	// shutdown can wait for them.
	fulfillment sync.WaitGroup

	// hub fans status transitions out to open SSE streams (spec 008). Always
	// non-nil; publishing with no subscribers is a cheap no-op.
	hub *StreamHub
}

// NewService builds the payment service.
func NewService(
	pool db.Beginner,
	repo *Repository,
	gateway Gateway,
	orders OrderProvider,
	quota QuotaRestorer,
	reserver QuotaReserver,
	releaser OrderReleaser,
	issuer TicketIssuer,
	deliverer TicketDeliverer,
	log *logger.Logger,
) *Service {
	return &Service{
		pool: pool, repo: repo, gateway: gateway, orders: orders,
		quota: quota, reserver: reserver, releaser: releaser,
		issuer: issuer, deliverer: deliverer, log: log,
		now: time.Now,
		hub: NewStreamHub(),
	}
}

// Hub exposes the status stream hub (used by tests and diagnostics).
func (s *Service) Hub() *StreamHub { return s.hub }

// WaitForFulfillment blocks until every in-flight post-payment goroutine finishes.
// Used by graceful shutdown and by tests that assert on their effects.
func (s *Service) WaitForFulfillment() {
	s.fulfillment.Wait()
}

// HandleNotification processes one gateway notification.
//
// Only an authentication failure is reported as an error (the handler turns it
// into a non-200). Every authenticated notification is acknowledged with 200 —
// including ones for unknown orders, non-deposit transaction types, and
// unrecognised statuses — because the gateway retries any other answer three
// times, ten seconds apart, with no dead-letter (FR-012c). Retrying cannot change
// any of those outcomes, so a refusal would buy nothing and cost four deliveries.
// Anything anomalous is signalled loudly instead.
func (s *Service) HandleNotification(ctx context.Context, provider string, payload []byte, token string) error {
	result, err := s.gateway.VerifyWebhook(payload, token)
	if err != nil {
		s.log.WarnContext(ctx, "rejected unauthenticated payment notification", "provider", provider, "error", err.Error())
		return err
	}

	// A withdrawal is not a payment for one of our orders. Record it, signal it,
	// change nothing, and still answer 200 — a retry would deliver the same
	// irrelevant fact again (FR-020).
	if !result.IsDeposit {
		s.log.ErrorContext(ctx, "notification is not a deposit; recorded without changing any order",
			"provider", provider, "reference", result.OrderNumber, "trx_type", result.TransactionType)
		s.recordOrphanNotification(ctx, provider, result, "non-deposit transaction type")
		return ErrNotificationNotDeposit
	}

	// The reference is the order number, unmodified. There is no suffix to strip
	// and no normalisation between the two: the re-issue machinery that made the
	// reference differ from the order number is withdrawn, and one order now has
	// exactly one session (FR-010b).
	ord, err := s.orders.OrderByNumber(ctx, result.OrderNumber)
	if errors.Is(err, ErrOrderNotFound) {
		// Loud rather than quiet: a reference we do not recognise means either the
		// gateway is misconfigured against another tenant, or an order vanished.
		// Both need a human, and neither is fixed by a retry (FR-013).
		s.log.ErrorContext(ctx, "notification names an unknown order; acknowledging without processing",
			"provider", provider, "reference", result.OrderNumber)
		s.recordOrphanNotification(ctx, provider, result, "unknown order reference")
		return ErrNotificationUnknownOrder
	}
	if err != nil {
		return err
	}

	_, err = s.applyProviderResult(ctx, ord, provider, "callback", result)
	return err
}

// recordOrphanNotification preserves a notification that names no order of ours,
// or names a transaction type we do not process.
//
// It cannot go in `payments` — that table's order_id is NOT NULL and there is no
// order to point it at — so the audit record here is the log line plus the raw
// payload. FR-018's "record every notification" is met for everything that
// resolves to an order; this is the honest boundary of that promise, stated
// rather than hidden.
func (s *Service) recordOrphanNotification(ctx context.Context, provider string, result *WebhookResult, reason string) {
	s.log.ErrorContext(ctx, "unattributable payment notification",
		"provider", provider,
		"reason", reason,
		"reference", result.OrderNumber,
		"transaction_id", result.TransactionID,
		"provider_status", result.TransactionStatus,
		"raw_payload", string(result.RawPayload))
}

// applyProviderResult is the single path every gateway answer travels, whether
// it is a first delivery, one of the gateway's automatic retries, or a
// redelivery an operator asked for to rescue a lost notification.
//
// Sharing it is what makes recovery exactly as safe as an ordinary payment: the
// same audit row, the same already-paid short-circuit, the same status mapping,
// and the same guarded transition — so two deliveries arriving at once still
// move the order (and its quota) exactly once. There is no separate replay path
// to get right, which is the point. It reports whether the order actually
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
	)

	// Log first: the payments row is the audit trail, and it must exist even for
	// answers that change nothing — including ones this system refuses (FR-018).
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

	outcome := MapProviderStatus(result.Status)

	// Idempotency short-circuit: PAID is terminal for every automated path
	// (FR-016a). Nothing below moves a paid order — only the attributable staff
	// action does.
	if ord.Status == OrderStatusPaid {
		// A repeat of the recorded outcome passes quietly. At-least-once delivery
		// makes this the *normal* case, not an anomaly, so signalling it would bury
		// the contradiction below in routine noise (FR-016).
		if outcome.OrderStatus == OrderStatusPaid {
			log.InfoContext(ctx, "notification repeats the recorded outcome; acknowledged as a no-op")
			return false, nil
		}
		// A contradiction is different in kind: the gateway now says this order
		// failed, and we have already issued tickets against it. Nothing is
		// reversed automatically — the seats are gone and the buyer holds valid
		// tickets — but it raises a signal distinguishable from retry traffic and
		// leaves a marker on the order's own record, so anyone who looks the order
		// up sees it without being told to look (FR-016b, FR-016c, FR-016d).
		if outcome.IsTerminalFailure() {
			log.ErrorContext(ctx, "notification contradicts a settled payment; order left PAID for manual review",
				"marker", MarkerDisputed)
			s.writeMarker(ctx, ord, provider, MarkerDisputed, result, map[string]any{
				"order_status_at_arrival": ord.Status,
				"contradicting_status":    result.TransactionStatus,
			})
			return false, ErrNotificationContradiction
		}
		log.InfoContext(ctx, "order is already paid; notification acknowledged as a no-op")
		return false, nil
	}

	if !outcome.Handled {
		// Obscure, or a value this build has never seen. Either may be a status
		// that should have released quota, so it must not pass as a quiet no-op
		// (FR-014).
		log.ErrorContext(ctx, "unrecognised gateway status; order left unchanged and acknowledged")
		return false, ErrNotificationUnknownStatus
	}
	if !outcome.ChangesOrder() {
		log.InfoContext(ctx, "notification is a legitimate no-op; order left pending")
		return false, nil
	}

	// A completion for an order that is no longer pending splits in two, and the
	// split is the whole of FR-019/FR-019d. Both orders look identical here — a
	// released order with money against it — but only one of them holds a verdict
	// this system is entitled to reverse.
	if outcome.OrderStatus == OrderStatusPaid && ord.Status != OrderStatusPending {
		if ord.Status == OrderStatusExpired {
			// Expiry is OUR verdict, reached on the deadline alone because a
			// notification never came. A redelivered one says the verdict was wrong,
			// so it is reversed: seats taken back, tickets issued, through the
			// ordinary path (FR-019).
			//
			// This deliberately does NOT fall through to applyOutcome below. That
			// path's only primitive is UpdateStatusIfPending, which would match zero
			// rows for an expired order and log a skipped transition at info level —
			// turning a settle into a silent drop.
			if err := s.settleExpiredOrder(ctx, ord, provider, result); err != nil {
				return false, err
			}
			return true, nil
		}

		// Cancellation is the GATEWAY's verdict, or the consequence of a code that
		// never reached the guest (FR-007f). Reversing it would contradict the source
		// of payment truth this design rests on, so the order stands and a person
		// decides what to do about the money. No marker: applyProviderResult has
		// already written the audit row carrying this payload, and that row sits on
		// the order's own record, which is where anyone looking it up will find it.
		log.ErrorContext(ctx, "gateway reports a successful payment for a cancelled order; not revived, recorded for review",
			"order_status", ord.Status)
		return false, ErrNotificationOrderCancelled
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

// writeMarker records a second statement about a notification that has already
// been logged: what this system concluded about it, on the order's own record.
//
// It never replaces the audit row (FR-018) — the raw payload stays on its own
// row exactly as it arrived, and this one carries the interpretation. A failure
// to write it is logged and swallowed: the caller is on the callback path with a
// five-second budget, and losing an annotation must not turn a recorded outcome
// into a retried delivery.
func (s *Service) writeMarker(
	ctx context.Context,
	ord OrderRef,
	provider, marker string,
	result *WebhookResult,
	detail map[string]any,
) {
	envelope := map[string]any{
		"marker":         marker,
		"raised_at":      s.now().UTC().Format(time.RFC3339),
		"payload":        json.RawMessage(validJSONOrNull(result.RawPayload)),
		"transaction_id": result.TransactionID,
	}
	for k, v := range detail {
		envelope[k] = v
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		s.log.ErrorContext(ctx, "could not encode payment marker",
			"order_number", ord.OrderNumber, "marker", marker, "error", err.Error())
		return
	}

	// transaction_id is NOT NULL, so a marker with no gateway id of its own falls
	// back to the order's own reference rather than writing an empty string that
	// would read as a real, blank gateway id.
	transactionID := result.TransactionID
	if transactionID == "" {
		transactionID = ord.OrderNumber
	}

	if err := s.repo.CreatePayment(ctx, PaymentLog{
		OrderID:       ord.ID,
		Provider:      provider,
		TransactionID: transactionID,
		PaymentType:   result.PaymentType,
		Status:        marker,
		RawResponse:   encoded,
	}); err != nil {
		s.log.ErrorContext(ctx, "could not record payment marker",
			"order_number", ord.OrderNumber, "marker", marker, "error", err.Error())
	}
}

// validJSONOrNull keeps a marker envelope encodable when the payload that
// triggered it is not itself JSON. raw_response is JSONB, so embedding an
// unparseable body verbatim would fail the insert and lose the marker entirely —
// the one outcome worse than losing the body's exact bytes, which the audit row
// written alongside still holds.
func validJSONOrNull(raw []byte) []byte {
	if len(raw) > 0 && json.Valid(raw) {
		return raw
	}
	return []byte("null")
}

// ReleaseDuplicateSession handles a session-open the gateway refused because it
// had already issued a code for this reference.
//
// The seats are released now rather than held to the deadline (FR-007e). That is
// not impatience: no call returns an existing code, so the guest will never be
// shown anything to scan, and holding their seats for the full window cannot end
// in a payment — it only keeps them from anyone who could actually buy them.
func (s *Service) ReleaseDuplicateSession(ctx context.Context, orderNumber string, cause error) error {
	ord, err := s.orders.OrderByNumber(ctx, orderNumber)
	if err != nil {
		return err
	}

	applied, restored, err := s.applyOutcome(ctx, ord, Outcome{
		OrderStatus: OrderStatusCancelled, RestoreQuota: true, Handled: true,
	})
	if err != nil {
		return err
	}

	detail := map[string]any{"order_status_at_arrival": ord.Status, "quota_released": applied}
	if cause != nil {
		detail["gateway_error"] = cause.Error()
	}
	// FR-007f: a payment that somehow arrives for this order still has to reach a
	// person, so the marker goes on the order's record whether or not the release
	// applied.
	s.writeMarker(ctx, ord, s.gateway.Name(), MarkerSessionDuplicate, &WebhookResult{
		OrderNumber: ord.OrderNumber,
		PaymentType: "qris",
	}, detail)

	lines := make([]string, 0, len(restored))
	for _, hold := range restored {
		lines = append(lines, fmt.Sprintf("%s:+%d", hold.TicketTypeID, hold.Quantity))
	}
	s.log.ErrorContext(ctx, "gateway refused a duplicate reference; order released without a payable code",
		"order_number", ord.OrderNumber,
		"quota_released", applied,
		"restored_quota", strings.Join(lines, ","),
		"marker", MarkerSessionDuplicate)

	return nil
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

// expiredOutcome is the transition an order past its deadline takes: expired,
// with its reserved seats returned to the pool.
var expiredOutcome = Outcome{OrderStatus: OrderStatusExpired, RestoreQuota: true, Handled: true}

// expireIfDue expires an order whose payment deadline has passed, reporting
// whether it actually applied the transition.
//
// It runs the same guarded transaction every other path runs, so the sweeper and
// a gateway `expire` notification racing each other still restore the quota
// exactly once.
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

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
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
		// Open payment screens learn the transition live (SSE). The callback, the
		// sweeper, and the settle-after-expiry path all pass through here.
		s.hub.Publish(ord.OrderNumber, StatusEvent{
			OrderID: ord.OrderNumber, Status: outcome.OrderStatus,
			ExpiresAt: ord.PaymentExpiresAt,
		})
	}
	return applied, restored, err
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
