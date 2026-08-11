package payment

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// maxWebhookBody caps how much of a notification body is read. Gateway
// notifications are a few hundred bytes; the cap stops a malformed or hostile
// request from consuming unbounded memory.
const maxWebhookBody = 1 << 20

// CallbackPath is where the gateway delivers notifications. The path is fixed by
// the gateway's own dispatch code and is not ours to choose — which is also why
// it is mounted at the root rather than under /api/v1.
const CallbackPath = "/v1.0/callback/exec"

// Handler exposes the payment callback over HTTP.
type Handler struct {
	svc *Service
	log *logger.Logger

	// SSE stream plumbing (stream.go). Intervals are fields so tests can run
	// the loop in milliseconds.
	streams           *streamLimiter
	keepAliveInterval time.Duration
	driftReadInterval time.Duration
}

// NewHandler builds the payment HTTP handler.
func NewHandler(svc *Service, log *logger.Logger) *Handler {
	return &Handler{
		svc: svc, log: log,
		streams:           newStreamLimiter(defaultStreamCap),
		keepAliveInterval: defaultKeepAliveInterval,
		driftReadInterval: defaultDriftReadInterval,
	}
}

// WithStreamIntervals overrides the SSE keep-alive and drift re-read timers —
// tests run the loop in milliseconds instead of tens of seconds.
func (h *Handler) WithStreamIntervals(keepAlive, driftRead time.Duration) *Handler {
	h.keepAliveInterval = keepAlive
	h.driftReadInterval = driftRead
	return h
}

// RegisterCallbackRoute mounts the gateway notification endpoint.
//
// It takes the Echo instance rather than a group because the path is absolute:
// the gateway posts to /v1.0/callback/exec, not to anything under /api/v1, and
// versioning it ourselves would simply mean the gateway could not reach it.
func (h *Handler) RegisterCallbackRoute(e *echo.Echo) {
	e.POST(CallbackPath, h.callback)
}

// callback receives one gateway notification.
//
// The response rule is narrow on purpose (FR-012c). The gateway retries any
// non-200 three times, ten seconds apart, with a five-second timeout and no
// dead-letter — so a refusal it cannot act on costs four deliveries and still
// ends in the notification being lost. Non-200 is therefore reserved for exactly
// two cases: authentication failed, or an internal fault where a retry genuinely
// helps. Everything else — unknown reference, withdrawal, unrecognised status,
// already-final order — is durably recorded and answered 200.
func (h *Handler) callback(c echo.Context) error {
	ctx := c.Request().Context()

	payload, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBody))
	if err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The notification body could not be read.")
	}

	err = h.svc.HandleNotification(ctx, h.svc.gateway.Name(), payload, bearerToken(c.Request()))
	if errors.Is(err, ErrInvalidSignature) {
		return apperr.Wrap(err, http.StatusUnauthorized, apperr.CodeInvalidSignature,
			"The notification could not be authenticated.")
	}

	// A redelivered notification whose order can no longer be covered. The settle
	// did not happen and never will under these conditions, so this is 200 with a
	// body rather than a refusal (FR-019c): a non-200 would spend the gateway's
	// three retries on an attempt guaranteed to fail identically, and the
	// notification would be lost at the end of it. The body is for our record and
	// for the operator who asked for the resend — the gateway does not read it.
	//
	// Because the status line is 200 here and 200 on every acknowledgement below,
	// the status cannot be what distinguishes them. The envelope's numeric code is
	// (FR-019e): 200001 here, 200000 there. Rendering it through apperr rather than
	// writing the envelope directly keeps apperr.Numeric the single place a numeric
	// code is derived, and earns the shared handler's "request rejected" log line —
	// wanted, because a refused settle is exactly what an operator goes looking for.
	var refused *SettleRefusedError
	if errors.As(err, &refused) {
		return apperr.New(http.StatusOK, apperr.CodeTicketsUnavailable, SettleRefusedMessage).
			WithData(toSettleRefusedResponse(refused))
	}

	// The other outcomes this system records but deliberately does not act on.
	// Each is answered 200 for the same reason as the refusal above — a retry
	// cannot change any of them — and each names itself in the envelope's code so
	// the caller learns what the operational signal already tells an operator
	// (FR-012g). The set is exactly the set that raises a signal; anything merely
	// uneventful falls through to the plain acknowledgement below.
	if notice, ok := notificationNotice(err); ok {
		return apperr.Wrap(err, http.StatusOK, notice.code, notice.message)
	}

	if err != nil {
		// A genuine internal fault: the one case besides auth where a retry is
		// worth the gateway's while.
		return err
	}

	// Acknowledged immediately, before any post-payment work runs
	// (ARCHITECTURE.md §3.4). Ticket generation and the email happen in a
	// goroutine precisely so they cannot spend the five-second budget.
	//
	// The body carries no data (FR-012f). Every outcome that reaches this line
	// answers identically — the payment applied, a pending notification, a
	// duplicate retry repeating an outcome already recorded — because what a
	// notification did is recorded (FR-018) and read back per order (FR-022c). A
	// second account of it here could drift from the durable one, and the gateway
	// would not read it either way.
	return httpx.Respond(c, http.StatusOK, nil)
}

// notice is the code and message a deliberate no-op answers with.
type notice struct {
	code    string
	message string
}

// notificationNotice maps a deliberate no-op onto the code and message the
// callback answers with.
//
// The messages are short and carry no order-specific detail. Not for secrecy —
// the endpoint is authenticated, so the enumeration concern shaping the resend
// endpoint does not apply here — but because the durable record is the account
// of what happened (FR-018, FR-022c), and a response restating it could drift
// from it. The caller gets the *kind* of problem, which is enough to tell a
// misconfigured gateway from a vanished order without a log search.
//
// Nothing says "recorded and acknowledged" either: that is true of every one of
// them, and it is what the 200 already means.
func notificationNotice(err error) (notice, bool) {
	switch {
	case errors.Is(err, ErrNotificationUnknownOrder):
		return notice{apperr.CodeNotificationOrderUnknown, "No order matches this reference."}, true
	case errors.Is(err, ErrNotificationNotDeposit):
		return notice{apperr.CodeNotificationNotDeposit, "Not a deposit."}, true
	case errors.Is(err, ErrNotificationUnknownStatus):
		return notice{apperr.CodeNotificationStatusUnknown, "Unrecognised gateway status."}, true
	case errors.Is(err, ErrNotificationContradiction):
		return notice{apperr.CodeNotificationContradiction, "Order is already paid; nothing reversed."}, true
	case errors.Is(err, ErrNotificationOrderCancelled):
		return notice{apperr.CodeNotificationOrderCancelled, "Order was cancelled; not revived."}, true
	}
	return notice{}, false
}

// RegisterAdminRoutes mounts the two order-scoped read views on the
// JWT-protected admin group.
//
// Both are reads, and that is the design rather than an omission: an operator
// looks an order up, checks it against the gateway's own dashboard, tops up
// quota if the seats were resold, and asks the gateway to resend. Nothing here
// records that a payment happened — ticket issuance has exactly one trigger, a
// notification from the gateway (FR-022d).
func (h *Handler) RegisterAdminRoutes(g *echo.Group) {
	g.GET("/admin/payment/order/:order_id/notifications", h.orderNotifications)
	g.GET("/admin/payment/order/:order_id/holds", h.orderHolds)
}

func (h *Handler) orderNotifications(c echo.Context) error {
	orderID, err := uuid.Parse(c.Param("order_id"))
	if err != nil {
		return apperr.BadRequest(apperr.CodeValidation, "The order id is not a valid identifier.")
	}

	records, err := h.svc.OrderNotifications(c.Request().Context(), orderID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, toNotificationResponse(records))
}

func (h *Handler) orderHolds(c echo.Context) error {
	orderID, err := uuid.Parse(c.Param("order_id"))
	if err != nil {
		return apperr.BadRequest(apperr.CodeValidation, "The order id is not a valid identifier.")
	}

	holds, err := h.svc.OrderHolds(c.Request().Context(), orderID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, toOrderHoldResponse(holds))
}

// bearerToken extracts the presented credential from the Authorization header.
//
// It returns the raw value with the scheme stripped, and an empty string when
// the header is absent or not a bearer token. Comparing it is VerifyWebhook's
// job — doing it here would put the constant-time comparison outside the gateway
// boundary the constitution puts it behind.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	header := r.Header.Get(echo.HeaderAuthorization)
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
