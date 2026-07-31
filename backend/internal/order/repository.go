package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/internal/order/ordersql"
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
type OrderRecord struct {
	ID              uuid.UUID
	OrderNumber     string
	BuyerName       string
	BuyerEmail      string
	BuyerPhone      string
	TotalAmount     decimal.Decimal
	Status          string
	PaymentProvider *string
	PaymentURL      *string
	EmailSent       *bool
	CreatedAt       *time.Time
}

// OrderItemRecord is the internal view of an order_items row.
type OrderItemRecord struct {
	ID           uuid.UUID
	OrderID      uuid.UUID
	TicketTypeID uuid.UUID
	Quantity     int32
	Price        decimal.Decimal
}

// AttendeeRecord is the internal view of an attendees row.
type AttendeeRecord struct {
	ID           uuid.UUID
	OrderID      uuid.UUID
	TicketTypeID uuid.UUID
	Name         string
	Email        string
}

// CreateOrderParams carries the server-computed values for a new order. There is
// deliberately no status or total field taken from the client.
type CreateOrderParams struct {
	OrderNumber string
	BuyerName   string
	BuyerEmail  string
	BuyerPhone  string
	TotalAmount decimal.Decimal
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

// CreateOrder inserts a PENDING order with no payment URL yet.
func (r *Repository) CreateOrder(ctx context.Context, tx pgx.Tx, p CreateOrderParams) (OrderRecord, error) {
	row, err := r.queries.WithTx(tx).CreateOrder(ctx, ordersql.CreateOrderParams{
		OrderNumber: p.OrderNumber,
		BuyerName:   p.BuyerName,
		BuyerEmail:  p.BuyerEmail,
		BuyerPhone:  p.BuyerPhone,
		TotalAmount: p.TotalAmount,
	})
	if isUniqueViolation(err) {
		return OrderRecord{}, ErrOrderNumberTaken
	}
	if err != nil {
		return OrderRecord{}, fmt.Errorf("create order: %w", err)
	}
	return toOrderRecord(row), nil
}

// CreateOrderItem inserts one line item, priced from current server-side data.
func (r *Repository) CreateOrderItem(ctx context.Context, tx pgx.Tx, orderID, ticketTypeID uuid.UUID, quantity int32, price decimal.Decimal) error {
	_, err := r.queries.WithTx(tx).CreateOrderItem(ctx, ordersql.CreateOrderItemParams{
		OrderID:      orderID,
		TicketTypeID: ticketTypeID,
		Quantity:     quantity,
		Price:        price,
	})
	if err != nil {
		return fmt.Errorf("create order item: %w", err)
	}
	return nil
}

// CreateAttendee inserts one attendee, who will receive exactly one ticket once
// the order is paid.
func (r *Repository) CreateAttendee(ctx context.Context, tx pgx.Tx, orderID, ticketTypeID uuid.UUID, name, email string) (uuid.UUID, error) {
	row, err := r.queries.WithTx(tx).CreateAttendee(ctx, ordersql.CreateAttendeeParams{
		OrderID:      orderID,
		TicketTypeID: ticketTypeID,
		Name:         name,
		Email:        email,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create attendee: %w", err)
	}
	return row.ID, nil
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

// UpdatePaymentDetails stamps the gateway's payment URL and provider (TX2).
func (r *Repository) UpdatePaymentDetails(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, paymentURL, provider string) error {
	affected, err := r.queries.WithTx(tx).UpdatePaymentDetails(ctx, ordersql.UpdatePaymentDetailsParams{
		ID:              orderID,
		PaymentUrl:      &paymentURL,
		PaymentProvider: &provider,
	})
	if err != nil {
		return fmt.Errorf("update payment details: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
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
	return toOrderRecord(row), nil
}

// ListOrderItemsByOrderID returns an order's line items, used to know how much
// quota to restore when an order is cancelled or expires.
func (r *Repository) ListOrderItemsByOrderID(ctx context.Context, orderID uuid.UUID) ([]OrderItemRecord, error) {
	rows, err := r.queries.ListOrderItemsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}

	out := make([]OrderItemRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, OrderItemRecord{
			ID:           row.ID,
			OrderID:      row.OrderID,
			TicketTypeID: row.TicketTypeID,
			Quantity:     row.Quantity,
			Price:        row.Price,
		})
	}
	return out, nil
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
func (r *Repository) HasOrdersForTicketType(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID) (bool, error) {
	has, err := r.queries.WithTx(tx).HasOrdersForTicketType(ctx, ticketTypeID)
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

// SoldCountByTicketType returns SUM(order_items.quantity) per ticket type in one
// query for a whole page. The count is always derived, never stored: a stored
// counter could drift from the truth, and SCHEMA.md defines no column for it
// (constitution, Critical Data Flow Rules).
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
		counts[row.TicketTypeID] = int(row.Sold)
	}
	return counts, nil
}

func toOrderRecord(row ordersql.Order) OrderRecord {
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
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
