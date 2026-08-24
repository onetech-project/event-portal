package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/internal/order/ordersql"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// Sentinel errors this domain reports to its service layer.
var (
	// ErrNotFound reports that no row matched.
	ErrNotFound = errors.New("order: not found")
	// ErrOrderNumberTaken reports a unique-violation on orders.order_number, which
	// the service answers by drawing a fresh number rather than failing checkout.
	ErrOrderNumberTaken = errors.New("order: order number already taken")
)

const uniqueViolation = "23505"

// OrderRecord is the internal view of an orders row.
//
// Buyer fields are pointers since 008: booking creates the order before any
// buyer identity exists, and nil is how a reader tells "not collected yet"
// from an empty submission.
type OrderRecord struct {
	ID              uuid.UUID
	OrderNumber     string
	BuyerName       *string
	BuyerEmail      *string
	BuyerPhone      *string
	TotalAmount     decimal.Decimal
	Status          string
	PaymentProvider *string
	PaymentURL      *string
	EmailSent       *bool
	CreatedAt       *time.Time
	// UpdatedAt is the receipt's "Last updated" stamp (spec 016 FR-008). The
	// column was always selected and always discarded here; nothing new is read.
	//
	// It moves on any write to the row, email_sent included, so a RESENT
	// receipt legitimately carries a later stamp than the original while every
	// monetary figure is identical. That is correct — the two are different
	// facts — and it is why the receipt's transaction date comes from the
	// payment row instead.
	UpdatedAt *time.Time
	// PaymentQRString and PaymentExpiresAt are nil for orders created before the
	// in-app QRIS flow, and for any order whose charge never completed. Readers
	// must treat nil as "no payment instruction" rather than rendering an empty
	// code.
	PaymentQRString  *string
	PaymentExpiresAt *time.Time
	// TermsAgreedAt is the durable T&C agreement record (FR-008); nil until the
	// record-agreement call lands. EventTermsID names the agreed document.
	TermsAgreedAt *time.Time
	EventTermsID  uuid.NullUUID
	// Subtotal is the pre-fee sum of the lines; invalid on orders that predate
	// fees (migration 0010). TotalAmount stays the single charged amount.
	Subtotal decimal.NullDecimal
	// IsRegistration reports a free registration (spec 022): PAID without a
	// payment. DERIVED by the query, not a stored column (Principle IV v6.0.0) —
	// true when the order carries a line for a ticket type that is not
	// guest-visible. Consequently it is not stable across time: an admin making
	// that type purchasable again flips it for orders already delivered.
	IsRegistration bool
}

// OrderItemRecord is the internal view of an order_items row.
//
// Ref carries the line's XOR: a ticket-type line or a package line, never both.
// Readers must branch on it rather than assuming a ticket type is present — an
// order made entirely of bundles has no ticket_type_id on any of its lines.
type OrderItemRecord struct {
	ID       uuid.UUID
	OrderID  uuid.UUID
	Ref      LineRef
	Quantity int32
	Price    decimal.Decimal
}

// AttendeeRecord is the internal view of an attendees row.
//
// TicketTypeID is always present, including for bundle-derived registrants;
// PackageID records only which bundle the slot originated in.
// Identity fields are pointers since 008: a slot is created empty at booking
// and filled at checkout, so nil means "not yet registered".
type AttendeeRecord struct {
	ID           uuid.UUID
	OrderID      uuid.UUID
	TicketTypeID uuid.UUID
	PackageID    uuid.NullUUID
	Name         *string
	Email        *string
}

// QuotaHold is one ticket type's total hold for an order, with package lines
// already expanded through the junction. Restoring these is the exact inverse of
// what checkout deducted.
type QuotaHold struct {
	TicketTypeID uuid.UUID
	Quantity     int32
}

// Repository is the only place in the codebase that talks to orders, order_items,
// and attendees.
type Repository struct {
	queries *ordersql.Queries
}

// NewRepository builds a repository over a pool or any other DBTX.
func NewRepository(dbtx ordersql.DBTX) *Repository {
	return &Repository{queries: ordersql.New(dbtx)}
}

// --- Checkout writes (all take the caller's TX1) --------------------------

// CreateOrderItem inserts one line item, priced from current server-side data.
//
// ref decides what the line sells. A package line is written once at the package's
// own price rather than expanded into per-constituent rows, so total_amount stays
// exactly SUM(quantity * price).
func (r *Repository) CreateOrderItem(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, ref LineRef, quantity int32, price decimal.Decimal) error {
	if !ref.Valid() {
		return fmt.Errorf("create order item: line must reference exactly one of ticket type or package")
	}
	_, err := r.queries.WithTx(tx).CreateOrderItem(ctx, ordersql.CreateOrderItemParams{
		OrderID:      orderID,
		TicketTypeID: ref.TicketTypeID,
		PackageID:    ref.PackageID,
		Quantity:     quantity,
		Price:        price,
	})
	if err != nil {
		return fmt.Errorf("create order item: %w", err)
	}
	return nil
}

// OrderNumberExists reports whether a generated order number is already in use.
func (r *Repository) OrderNumberExists(ctx context.Context, orderNumber string) (bool, error) {
	taken, err := r.queries.OrderNumberExists(ctx, orderNumber)
	if err != nil {
		return false, fmt.Errorf("check order number: %w", err)
	}
	return taken, nil
}

// --- Payment lifecycle ----------------------------------------------------

// PaymentDetails is what TX2 stamps onto an order once the gateway has opened a
// payment session: where the provider hosts the QR image, the payload the image
// is rendered from, and when the whole thing stops being payable.
type PaymentDetails struct {
	PaymentURL string
	Provider   string
	QRString   string
	ExpiresAt  time.Time
}

// UpdatePaymentDetails stamps the gateway's payment details onto the order (TX2).
func (r *Repository) UpdatePaymentDetails(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, details PaymentDetails) error {
	// Zero values are stored as NULL rather than as an empty string or the zero
	// time: a reader must be able to tell "no payment instruction" from one that
	// happens to be blank, because it decides whether a QR is shown at all.
	var qrString *string
	if details.QRString != "" {
		qrString = &details.QRString
	}
	var expiresAt *time.Time
	if !details.ExpiresAt.IsZero() {
		expiresAt = &details.ExpiresAt
	}

	affected, err := r.queries.WithTx(tx).UpdatePaymentDetails(ctx, ordersql.UpdatePaymentDetailsParams{
		ID:               orderID,
		PaymentUrl:       &details.PaymentURL,
		PaymentProvider:  &details.Provider,
		PaymentQrString:  qrString,
		PaymentExpiresAt: expiresAt,
	})
	if err != nil {
		return fmt.Errorf("update payment details: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ExpiryCandidate is a PENDING order whose payment deadline has passed.
type ExpiryCandidate struct {
	ID               uuid.UUID
	OrderNumber      string
	Status           string
	PaymentExpiresAt *time.Time
}

// ListOrdersDueForExpiry returns orders past their payment deadline that nothing
// has moved yet, oldest first, capped at limit.
//
// It is deliberately a read: the transition itself runs through the same guarded
// update every other path uses, so a sweep that races a webhook cannot restore
// the same quota twice.
func (r *Repository) ListOrdersDueForExpiry(ctx context.Context, now time.Time, limit int32) ([]ExpiryCandidate, error) {
	rows, err := r.queries.ListOrdersDueForExpiry(ctx, ordersql.ListOrdersDueForExpiryParams{
		PaymentExpiresAt: &now,
		Limit:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list orders due for expiry: %w", err)
	}

	out := make([]ExpiryCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpiryCandidate{
			ID:               row.ID,
			OrderNumber:      row.OrderNumber,
			Status:           row.Status,
			PaymentExpiresAt: row.PaymentExpiresAt,
		})
	}
	return out, nil
}

// UpdateOrderStatusIfPending applies a status transition only while the order is
// still PENDING, reporting whether it actually applied. Callers use that answer to
// decide whether to restore quota, so a replayed webhook cannot restore twice
// (Constitution Principle IV).
func (r *Repository) UpdateOrderStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, status string) (bool, error) {
	affected, err := r.queries.WithTx(tx).UpdateOrderStatusIfPending(ctx, ordersql.UpdateOrderStatusIfPendingParams{
		ID:     orderID,
		Status: status,
	})
	if err != nil {
		return false, fmt.Errorf("update order status: %w", err)
	}
	return affected > 0, nil
}

// UpdateOrderStatusFrom applies a transition only while the order is still in
// the `from` status, reporting whether it actually applied.
//
// It exists for the settle-after-expiry path (spec 012 FR-019), which is the one
// path that moves an order OUT of a released state — a gateway notification
// redelivered after the order's deadline already released its seats.
//
// It is deliberately not a generalisation of UpdateOrderStatusIfPending. This one
// can express any from→to move, so the payment domain is never handed it
// directly: its adapter binds both statuses to EXPIRED → PAID at the call site,
// which is what makes reviving a gateway-cancelled order (FR-019d) unreachable by
// construction rather than by a caller remembering the rule.
func (r *Repository) UpdateOrderStatusFrom(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, from, to string) (bool, error) {
	affected, err := r.queries.WithTx(tx).UpdateOrderStatusFromReleased(ctx, ordersql.UpdateOrderStatusFromReleasedParams{
		ID:         orderID,
		Status:     to,
		FromStatus: from,
	})
	if err != nil {
		return false, fmt.Errorf("update order status from %s: %w", from, err)
	}
	return affected > 0, nil
}

// SetEmailSent records that the ticket email was delivered. It is only called
// after a successful send (Constitution Principle IV).
func (r *Repository) SetEmailSent(ctx context.Context, orderID uuid.UUID) error {
	affected, err := r.queries.SetEmailSent(ctx, orderID)
	if err != nil {
		return fmt.Errorf("set email_sent: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Reads ----------------------------------------------------------------

// GetOrderByID loads one order.
func (r *Repository) GetOrderByID(ctx context.Context, id uuid.UUID) (OrderRecord, error) {
	row, err := r.queries.GetOrderByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderRecord{}, ErrNotFound
	}
	if err != nil {
		return OrderRecord{}, fmt.Errorf("get order: %w", err)
	}
	return toOrderRecord(row), nil
}

// GetOrderByNumber loads one order by its public order number, which is what the
// payment provider echoes back in webhook notifications.
func (r *Repository) GetOrderByNumber(ctx context.Context, orderNumber string) (OrderRecord, error) {
	row, err := r.queries.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderRecord{}, ErrNotFound
	}
	if err != nil {
		return OrderRecord{}, fmt.Errorf("get order by number: %w", err)
	}
	return toOrderRecord(ordersql.GetOrderByIDRow(row)), nil
}

// ListOrderItemsByOrderID returns an order's line items for display. Restoring
// quota uses ListQuotaHoldsByOrderID instead, which expands package lines through
// the junction rather than leaving that arithmetic to callers.
func (r *Repository) ListOrderItemsByOrderID(ctx context.Context, orderID uuid.UUID) ([]OrderItemRecord, error) {
	rows, err := r.queries.ListOrderItemsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}

	out := make([]OrderItemRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, OrderItemRecord{
			ID:       row.ID,
			OrderID:  row.OrderID,
			Ref:      LineRef{TicketTypeID: row.TicketTypeID, PackageID: row.PackageID},
			Quantity: row.Quantity,
			Price:    row.Price,
		})
	}
	return out, nil
}

// ListQuotaHoldsByOrderID returns what an order actually holds per ticket type,
// with package lines already expanded through package_tickets. Rows arrive sorted
// by ticket type, matching deduction order so restore stays on the same
// deterministic lock sequence.
func (r *Repository) ListQuotaHoldsByOrderID(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]QuotaHold, error) {
	rows, err := r.queries.WithTx(tx).ListQuotaHoldsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list quota holds: %w", err)
	}

	out := make([]QuotaHold, 0, len(rows))
	for _, row := range rows {
		// The column reads as nullable because it is projected through a UNION, but
		// both arms filter nulls out. Skipping rather than dereferencing keeps a
		// surprising row from restoring quota to the nil ticket type.
		if !row.TicketTypeID.Valid {
			continue
		}
		out = append(out, QuotaHold{TicketTypeID: row.TicketTypeID.UUID, Quantity: row.Qty})
	}
	return out, nil
}

// HasOrdersForPackage reports whether a package is referenced by an order_items or
// attendees row. Both FKs are ON DELETE RESTRICT, so the guard must run to produce
// a 400 rather than letting a raw constraint violation surface.
func (r *Repository) HasOrdersForPackage(ctx context.Context, tx pgx.Tx, packageID uuid.UUID) (bool, error) {
	has, err := r.queries.WithTx(tx).HasOrdersForPackage(ctx, uuid.NullUUID{UUID: packageID, Valid: true})
	if err != nil {
		return false, fmt.Errorf("check orders for package: %w", err)
	}
	return has != nil && *has, nil
}

// HasPendingOrdersForPackage reports whether an open PENDING order holds this
// package. The event domain uses it to lock a package's composition (R-004): once
// a hold exists, editing the composition would drift what expiry restores, so
// those edits are rejected with 409 until the order resolves.
func (r *Repository) HasPendingOrdersForPackage(ctx context.Context, tx pgx.Tx, packageID uuid.UUID) (bool, error) {
	has, err := r.queries.WithTx(tx).HasPendingOrdersForPackage(ctx, uuid.NullUUID{UUID: packageID, Valid: true})
	if err != nil {
		return false, fmt.Errorf("check pending orders for package: %w", err)
	}
	return has, nil
}

// ListAttendeesByOrderID returns an order's attendees — one ticket is generated
// per row once the order is paid.
func (r *Repository) ListAttendeesByOrderID(ctx context.Context, orderID uuid.UUID) ([]AttendeeRecord, error) {
	rows, err := r.queries.ListAttendeesByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list attendees: %w", err)
	}

	out := make([]AttendeeRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, AttendeeRecord{
			ID:           row.ID,
			OrderID:      row.OrderID,
			TicketTypeID: row.TicketTypeID,
			Name:         row.Name,
			Email:        row.Email,
		})
	}
	return out, nil
}

// --- Spec 008: two-phase booking -------------------------------------------

// CreateBookedOrder inserts the TX-B order: PENDING, buyer columns NULL,
// payment fields NULL, payment_expires_at carrying the 1-hour hold.
func (r *Repository) CreateBookedOrder(ctx context.Context, tx pgx.Tx, orderNumber string, total, subtotal decimal.Decimal, expiresAt time.Time) (OrderRecord, error) {
	row, err := r.queries.WithTx(tx).CreateBookedOrder(ctx, ordersql.CreateBookedOrderParams{
		OrderNumber:      orderNumber,
		TotalAmount:      total,
		Subtotal:         decimal.NullDecimal{Decimal: subtotal, Valid: true},
		PaymentExpiresAt: &expiresAt,
	})
	if isUniqueViolation(err) {
		return OrderRecord{}, ErrOrderNumberTaken
	}
	if err != nil {
		return OrderRecord{}, fmt.Errorf("create booked order: %w", err)
	}
	return toOrderRecord(ordersql.GetOrderByIDRow(row)), nil
}

// CreateAttendeeSlot inserts one EMPTY attendee slot: bound to its ticket type
// (and package origin) at booking, identity filled at checkout (Option B).
func (r *Repository) CreateAttendeeSlot(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, ref AttendeeRef) (uuid.UUID, error) {
	row, err := r.queries.WithTx(tx).CreateAttendeeSlot(ctx, ordersql.CreateAttendeeSlotParams{
		OrderID:      orderID,
		TicketTypeID: ref.TicketTypeID,
		PackageID:    ref.PackageID,
		PackageUnit:  ref.PackageUnit,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create attendee slot: %w", err)
	}
	return row.ID, nil
}

// RecordTermsAgreement stamps terms_agreed_at + event_terms_id on a PENDING
// order. Returns false when no row matched — the order moved on (expired,
// cancelled, paid) between the caller's guard and this write.
func (r *Repository) RecordTermsAgreement(ctx context.Context, orderID, termsID uuid.UUID) (bool, error) {
	rows, err := r.queries.RecordTermsAgreement(ctx, ordersql.RecordTermsAgreementParams{
		ID:           orderID,
		EventTermsID: uuid.NullUUID{UUID: termsID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("record terms agreement: %w", err)
	}
	return rows > 0, nil
}

// EventIDForOrder resolves the event an order belongs to through its attendee
// slots. ErrNotFound for an order with no slots (never true of a booked order).
func (r *Repository) EventIDForOrder(ctx context.Context, orderID uuid.UUID) (uuid.UUID, error) {
	eventID, err := r.queries.GetOrderEventID(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve order event: %w", err)
	}
	return eventID, nil
}

// UpdateOrderBuyer stamps the primary contact onto a PENDING order (TX-D) —
// a snapshot of the topmost holder form (spec 011). Returns false when the
// order is no longer PENDING.
func (r *Repository) UpdateOrderBuyer(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, buyer BuyerDetails) (bool, error) {
	rows, err := r.queries.WithTx(tx).UpdateOrderBuyer(ctx, ordersql.UpdateOrderBuyerParams{
		ID:         orderID,
		BuyerName:  &buyer.Name,
		BuyerEmail: &buyer.Email,
		BuyerPhone: &buyer.Phone,
	})
	if err != nil {
		return false, fmt.Errorf("update order buyer: %w", err)
	}
	return rows > 0, nil
}

// BuyerDetails is the primary contact checkout stamps onto the order (TX-D):
// the topmost holder form's name/email/phone — exactly what downstream reads
// (gateway customer details, admin list, delivery fallback). The holder's dob
// and gender live only on their attendee row (spec 011).
type BuyerDetails struct {
	Name  string
	Email string
	Phone string
}

// ListActiveGenders returns the gender master list as the forms' OPTIONS
// (clarified 2026-08-05). Active entries only, and deliberately so — spec 011
// FR-031 widens one card's option list, never this one.
func (r *Repository) ListActiveGenders(ctx context.Context) ([]GenderRecord, error) {
	rows, err := r.queries.ListActiveGenders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list genders: %w", err)
	}
	out := make([]GenderRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, GenderRecord{ID: row.ID, Name: row.Name})
	}
	return out, nil
}

// ListGenders returns every gender, active or not, as checkout needs them
// (spec 011 FR-031, clarified 2026-08-19). A slot keeps whatever gender it was
// saved with, so a restored form can submit one this list still knows and
// ListActiveGenders no longer offers.
func (r *Repository) ListGenders(ctx context.Context) ([]GenderRecord, error) {
	rows, err := r.queries.ListGenders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all genders: %w", err)
	}
	out := make([]GenderRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, GenderRecord{ID: row.ID, Name: row.Name, IsActive: row.IsActive})
	}
	return out, nil
}

// GenderRecord is one row of the gender master list.
//
// ID narrowed from uuid to a small integer in migration 0013 (spec 011 FR-025).
// It stays storage-internal either way: the NAME is what the wire carries.
type GenderRecord struct {
	ID   int16
	Name string
	// Zero value on rows read through ListActiveGenders, which selects only
	// active ones; meaningful only on ListGenders.
	IsActive bool
}

// SlotDetails is one visitor form's content, written into an attendee slot.
// GenderID is the master row resolved from the NAME the request carried
// (spec 011, FR-018).
type SlotDetails struct {
	Name     string
	Email    string
	Phone    string
	Dob      time.Time
	GenderID int16
}

// UpdateAttendeeDetails fills one slot (TX-D). The orderID predicate stops a
// forged slot id from writing into another order's attendee; false reports
// that no slot matched.
func (r *Repository) UpdateAttendeeDetails(ctx context.Context, tx pgx.Tx, slotID, orderID uuid.UUID, d SlotDetails) (bool, error) {
	rows, err := r.queries.WithTx(tx).UpdateAttendeeDetails(ctx, ordersql.UpdateAttendeeDetailsParams{
		ID:       slotID,
		OrderID:  orderID,
		Name:     &d.Name,
		Email:    &d.Email,
		Phone:    &d.Phone,
		Dob:      pgtype.Date{Time: d.Dob, Valid: true},
		GenderID: &d.GenderID,
	})
	if err != nil {
		return false, fmt.Errorf("update attendee details: %w", err)
	}
	return rows > 0, nil
}

// UpdatePaymentDetailsIfUnstarted stamps the gateway session and the payment
// window (TX-P), guarded on `status='PENDING' AND payment_qr_string IS NULL`
// so a concurrent checkout cannot overwrite a live QR. False = guard matched
// no row and the caller should re-read.
func (r *Repository) UpdatePaymentDetailsIfUnstarted(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, details PaymentDetails) (bool, error) {
	rows, err := r.queries.WithTx(tx).UpdatePaymentDetailsIfUnstarted(ctx, ordersql.UpdatePaymentDetailsIfUnstartedParams{
		ID:               orderID,
		PaymentUrl:       &details.PaymentURL,
		PaymentProvider:  &details.Provider,
		PaymentQrString:  &details.QRString,
		PaymentExpiresAt: &details.ExpiresAt,
	})
	if err != nil {
		return false, fmt.Errorf("stamp payment details: %w", err)
	}
	return rows > 0, nil
}

// AttendeeSlotRecord is one slot row with its human ticket-type name, as the
// order page renders it (details or nulls — Option B).
type AttendeeSlotRecord struct {
	ID             uuid.UUID
	TicketTypeID   uuid.UUID
	PackageID      uuid.NullUUID
	PackageUnit    *int16
	TicketTypeName string
	Name           *string
	Email          *string
	Phone          *string
	Dob            *time.Time
	Gender         *string
}

// ListAttendeeSlotsByOrderID returns an order's slots in stable insertion
// order, with the 008 detail columns.
func (r *Repository) ListAttendeeSlotsByOrderID(ctx context.Context, orderID uuid.UUID) ([]AttendeeSlotRecord, error) {
	rows, err := r.queries.ListAttendeeSlotsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list attendee slots: %w", err)
	}
	out := make([]AttendeeSlotRecord, 0, len(rows))
	for _, row := range rows {
		var dob *time.Time
		if row.Dob.Valid {
			d := row.Dob.Time
			dob = &d
		}
		out = append(out, AttendeeSlotRecord{
			ID:             row.ID,
			TicketTypeID:   row.TicketTypeID,
			PackageID:      row.PackageID,
			PackageUnit:    row.PackageUnit,
			TicketTypeName: row.TicketTypeName,
			Name:           row.Name,
			Email:          row.Email,
			Phone:          row.Phone,
			Dob:            dob,
			Gender:         row.Gender,
		})
	}
	return out, nil
}

// AttendeeIDsForOrder returns just the attendee ids, which is all the ticket
// domain needs to issue one ticket per attendee.
func (r *Repository) AttendeeIDsForOrder(ctx context.Context, orderID uuid.UUID) ([]uuid.UUID, error) {
	attendees, err := r.ListAttendeesByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, 0, len(attendees))
	for _, attendee := range attendees {
		ids = append(ids, attendee.ID)
	}
	return ids, nil
}

// --- OrderChecker: the contract the event domain consumes ------------------
//
// Every method below touches only order-domain tables. The event domain resolves
// its own ticket-type ids first and passes them in, so neither domain reads the
// other's tables (Constitution Principle II).

// HasOrdersForTicketType reports whether a ticket type is referenced by an
// order_items row OR an attendees row. Both foreign keys are ON DELETE RESTRICT,
// so checking only one would let a delete through and surface a raw constraint
// violation instead of the required 400 (Constitution Principle VI).
//
// It runs inside the caller's delete transaction, closing the TOCTOU window in
// which an order could land between the guard and the DELETE.
//
// Since order_items.ticket_type_id became nullable, the attendees half of this
// check stopped being a redundant safety net: an order made only of bundles has no
// ticket_type_id on any order_items row, and attendees.ticket_type_id is the only
// place that reference survives.
func (r *Repository) HasOrdersForTicketType(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID) (bool, error) {
	has, err := r.queries.WithTx(tx).HasOrdersForTicketType(ctx, uuid.NullUUID{UUID: ticketTypeID, Valid: true})
	if err != nil {
		return false, fmt.Errorf("check orders for ticket type: %w", err)
	}
	return has != nil && *has, nil
}

// HasOrdersForTicketTypes is the batched form used when deleting an event, which
// must consider every one of its ticket types.
func (r *Repository) HasOrdersForTicketTypes(ctx context.Context, tx pgx.Tx, ticketTypeIDs []uuid.UUID) (bool, error) {
	if len(ticketTypeIDs) == 0 {
		return false, nil
	}
	has, err := r.queries.WithTx(tx).HasOrdersForTicketTypes(ctx, ticketTypeIDs)
	if err != nil {
		return false, fmt.Errorf("check orders for ticket types: %w", err)
	}
	return has != nil && *has, nil
}

// SoldCountByTicketType returns units sold per ticket type in one query for a
// whole page. The count is always derived, never stored: a stored counter could
// drift from the truth, and SCHEMA.md defines no column for it (constitution,
// Critical Data Flow Rules).
//
// The underlying query counts standalone lines AND package lines expanded through
// package_tickets, so a bundled sale is attributed to every ticket type it
// consumed rather than disappearing from the figure.
func (r *Repository) SoldCountByTicketType(ctx context.Context, ticketTypeIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(ticketTypeIDs))
	if len(ticketTypeIDs) == 0 {
		return counts, nil
	}

	rows, err := r.queries.SoldCountByTicketTypes(ctx, ticketTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("count sold tickets: %w", err)
	}
	for _, row := range rows {
		// Nullable only because the column is projected through a UNION; both arms
		// filter nulls out.
		if !row.TicketTypeID.Valid {
			continue
		}
		counts[row.TicketTypeID.UUID] = int(row.Sold)
	}
	return counts, nil
}

// SoldCountByPackage returns units sold per package in one query, used by the
// admin package dashboard. A package line is stored once at the package's own
// price, so this is a straight SUM(quantity) over order_items.
func (r *Repository) SoldCountByPackage(ctx context.Context, packageIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(packageIDs))
	if len(packageIDs) == 0 {
		return counts, nil
	}

	rows, err := r.queries.SoldCountByPackage(ctx, packageIDs)
	if err != nil {
		return nil, fmt.Errorf("count sold packages: %w", err)
	}
	for _, row := range rows {
		if !row.PackageID.Valid {
			continue
		}
		counts[row.PackageID.UUID] = int(row.Sold)
	}
	return counts, nil
}

// Migration 0013 made every order read join the status master list to alias the
// NAME back to `status`, so sqlc no longer recognises these queries as returning
// the bare `orders` row and emits a per-query struct instead of the shared
// `ordersql.Order` model. The structs are field-for-field identical, so the call
// sites convert rather than each growing its own mapper.
func toOrderRecord(row ordersql.GetOrderByIDRow) OrderRecord {
	return OrderRecord{
		ID:              row.ID,
		OrderNumber:     row.OrderNumber,
		BuyerName:       row.BuyerName,
		BuyerEmail:      row.BuyerEmail,
		BuyerPhone:      row.BuyerPhone,
		TotalAmount:     row.TotalAmount,
		Status:          row.Status,
		PaymentProvider: row.PaymentProvider,
		PaymentURL:      row.PaymentUrl,
		EmailSent:       row.EmailSent,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,

		Subtotal:         row.Subtotal,
		PaymentQRString:  row.PaymentQrString,
		PaymentExpiresAt: row.PaymentExpiresAt,
		TermsAgreedAt:    row.TermsAgreedAt,
		EventTermsID:     row.EventTermsID,
		IsRegistration:   row.IsRegistration,
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// strv unwraps a nullable text column for display. Buyer and attendee identity
// became nullable in 008 (booking precedes the forms); nil renders as empty.
func strv(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// --- Fees (master + per-order snapshot; clarified 2026-08-05) ---------------

// FeeRecord is one active fee master row as booking applies it.
type FeeRecord struct {
	ID      uuid.UUID
	Name    string
	FeeType string
	Value   decimal.Decimal
}

// FeeRow is one fee master row as the admin panel sees it.
type FeeRow struct {
	ID        uuid.UUID
	Name      string
	FeeType   string
	Value     decimal.Decimal
	Position  int32
	IsActive  bool
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// OrderFeeRecord is one frozen fee line on an order.
type OrderFeeRecord struct {
	Name   string
	Amount decimal.Decimal
}

// ListActiveFeesTx returns the fee master rows booking applies, in display
// order, on the CALLER'S transaction. Booking must use this form: reading via
// the pool from inside TX-B would grab a second connection while the first
// holds quota row locks — at MaxConns concurrent bookings that starves the
// pool into a deadlock.
func (r *Repository) ListActiveFeesTx(ctx context.Context, tx pgx.Tx) ([]FeeRecord, error) {
	rows, err := r.queries.WithTx(tx).ListActiveFees(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active fees: %w", err)
	}
	out := make([]FeeRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, FeeRecord{ID: row.ID, Name: row.Name, FeeType: row.FeeType, Value: row.Value})
	}
	return out, nil
}

// ListActiveFees returns the fee master rows booking applies, in display order.
func (r *Repository) ListActiveFees(ctx context.Context) ([]FeeRecord, error) {
	rows, err := r.queries.ListActiveFees(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active fees: %w", err)
	}
	out := make([]FeeRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, FeeRecord{ID: row.ID, Name: row.Name, FeeType: row.FeeType, Value: row.Value})
	}
	return out, nil
}

// CreateOrderFee freezes one computed fee line onto the order inside TX-B.
func (r *Repository) CreateOrderFee(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, name string, amount decimal.Decimal, position int32) error {
	if err := r.queries.WithTx(tx).CreateOrderFee(ctx, ordersql.CreateOrderFeeParams{
		OrderID:  orderID,
		Name:     name,
		Amount:   amount,
		Position: position,
	}); err != nil {
		return fmt.Errorf("create order fee: %w", err)
	}
	return nil
}

// ListOrderFeesByOrderID returns an order's frozen fee lines, in display order.
func (r *Repository) ListOrderFeesByOrderID(ctx context.Context, orderID uuid.UUID) ([]OrderFeeRecord, error) {
	rows, err := r.queries.ListOrderFeesByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order fees: %w", err)
	}
	out := make([]OrderFeeRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, OrderFeeRecord{Name: row.Name, Amount: row.Amount})
	}
	return out, nil
}

// ListFees returns every fee master row for the admin panel.
func (r *Repository) ListFees(ctx context.Context, page httpx.PageRequest) ([]FeeRow, error) {
	rows, err := r.queries.ListFees(ctx, ordersql.ListFeesParams{
		RowLimit:  int32(page.Limit()),
		RowOffset: int32(page.Offset()),
	})
	if err != nil {
		return nil, fmt.Errorf("list fees: %w", err)
	}
	out := make([]FeeRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, toFeeRow(row.ID, row.Name, row.FeeType, row.Value, row.Position, row.IsActive, row.CreatedAt, row.UpdatedAt))
	}
	return out, nil
}

// CountFees reports how many fee master rows exist, for the paged admin read.
func (r *Repository) CountFees(ctx context.Context) (int64, error) {
	total, err := r.queries.CountFees(ctx)
	if err != nil {
		return 0, fmt.Errorf("count fees: %w", err)
	}
	return total, nil
}

// CreateFee inserts a fee master row. ErrFeeNameTaken on a duplicate name.
func (r *Repository) CreateFee(ctx context.Context, name, feeType string, value decimal.Decimal, position int32, isActive bool) (FeeRow, error) {
	row, err := r.queries.CreateFee(ctx, ordersql.CreateFeeParams{
		Name: name, FeeType: feeType, Value: value, Position: position, IsActive: isActive,
	})
	if isUniqueViolation(err) {
		return FeeRow{}, ErrFeeNameTaken
	}
	if err != nil {
		return FeeRow{}, fmt.Errorf("create fee: %w", err)
	}
	return toFeeRow(row.ID, row.Name, row.FeeType, row.Value, row.Position, row.IsActive, row.CreatedAt, row.UpdatedAt), nil
}

// UpdateFee rewrites a fee master row. ErrNotFound when the id has no row.
func (r *Repository) UpdateFee(ctx context.Context, id uuid.UUID, name, feeType string, value decimal.Decimal, position int32, isActive bool) (FeeRow, error) {
	row, err := r.queries.UpdateFee(ctx, ordersql.UpdateFeeParams{
		ID: id, Name: name, FeeType: feeType, Value: value, Position: position, IsActive: isActive,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return FeeRow{}, ErrNotFound
	}
	if isUniqueViolation(err) {
		return FeeRow{}, ErrFeeNameTaken
	}
	if err != nil {
		return FeeRow{}, fmt.Errorf("update fee: %w", err)
	}
	return toFeeRow(row.ID, row.Name, row.FeeType, row.Value, row.Position, row.IsActive, row.CreatedAt, row.UpdatedAt), nil
}

// DeleteFee removes a fee master row; existing orders keep their snapshots.
func (r *Repository) DeleteFee(ctx context.Context, id uuid.UUID) error {
	rows, err := r.queries.DeleteFee(ctx, id)
	if err != nil {
		return fmt.Errorf("delete fee: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func toFeeRow(id uuid.UUID, name, feeType string, value decimal.Decimal, position int32, isActive bool, createdAt, updatedAt pgtype.Timestamptz) FeeRow {
	return FeeRow{
		ID: id, Name: name, FeeType: feeType, Value: value,
		Position: position, IsActive: isActive,
		CreatedAt: tsPtr(createdAt), UpdatedAt: tsPtr(updatedAt),
	}
}

// tsPtr converts a pgtype timestamp into the *time.Time the DTO layer speaks.
func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// ErrFeeNameTaken reports a duplicate fee name.
var ErrFeeNameTaken = errors.New("order: fee name taken")
