package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/money"
)

const (
	// paymentMethod is what the guest is looking at. It is the instrument, not
	// the provider: swapping acquirers does not change what the page says.
	paymentMethod = "QRIS"
	// statusPending is the only order status that can carry a live payment
	// instruction. It matches the CHECK constraint on orders.status.
	statusPending = "PENDING"
	// orderNotFoundMessage is identical for an order that never existed and one
	// that exists but cannot be shown, so the endpoint reveals nothing by
	// comparison (spec FR-022).
	orderNotFoundMessage = "Order not found."
)

// PublicService serves the guest's own view of an order — the page they land on
// after checkout and watch until their payment is confirmed.
//
// It is deliberately read-only and cheap: the page polls it every few seconds
// while an order is pending, so it must stay a couple of indexed reads with no
// outbound provider call and no writes. Reconciling with the provider is the
// payment domain's job, behind a rate-limited endpoint of its own.
type PublicService struct {
	repo   *Repository
	events EventLookup
	now    func() time.Time
}

// NewPublicService builds the guest-facing order read service.
func NewPublicService(repo *Repository, events EventLookup) *PublicService {
	return &PublicService{repo: repo, events: events, now: time.Now}
}

// OrderByNumber assembles the guest's view of one order.
//
// An unknown order number and an order that exists but cannot be shown produce
// the identical 404, so the endpoint reveals nothing by comparison.
func (s *PublicService) OrderByNumber(ctx context.Context, orderNumber string) (PublicOrderDetail, error) {
	record, err := s.repo.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return PublicOrderDetail{}, apperr.NotFound(apperr.CodeOrderNotFound, orderNotFoundMessage)
	}
	if err != nil {
		return PublicOrderDetail{}, err
	}

	items, err := s.repo.ListOrderItemsByOrderID(ctx, record.ID)
	if err != nil {
		return PublicOrderDetail{}, err
	}

	displays, err := s.ticketTypeDisplays(ctx, items)
	if err != nil {
		return PublicOrderDetail{}, err
	}

	now := s.now()
	detail := PublicOrderDetail{
		OrderNumber: record.OrderNumber,
		Status:      record.Status,
		TotalAmount: money.From(record.TotalAmount),
		BuyerName:   record.BuyerName,
		BuyerEmail:  record.BuyerEmail,
		CreatedAt:   record.CreatedAt,
		Event:       eventOf(items, displays),
		Items:       publicItems(items, displays),
		ServerTime:  now.UTC(),
		Payment:     s.paymentInstruction(record, now),
	}
	return detail, nil
}

// PaymentQRPayload returns the QRIS payload to render for an order.
//
// It answers 404 for an order that is not payable as well as for one that does
// not exist, so the image disappears at exactly the moment the payment
// instruction does — a page left open past the deadline cannot keep showing a
// live-looking code.
func (s *PublicService) PaymentQRPayload(ctx context.Context, orderNumber string) (string, error) {
	record, err := s.repo.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return "", apperr.NotFound(apperr.CodeOrderNotFound, orderNotFoundMessage)
	}
	if err != nil {
		return "", err
	}

	if s.paymentInstruction(record, s.now()) == nil {
		return "", apperr.NotFound(apperr.CodeOrderNotFound, orderNotFoundMessage)
	}
	return *record.PaymentQRString, nil
}

// paymentInstruction returns what the guest needs to pay, or nil when the order
// is not payable.
//
// Nil covers every case where showing a code would be wrong: an order that is
// already paid, one that was cancelled or expired, one whose charge never
// recorded an instruction, and one whose deadline has passed but whose status
// the sweeper has not caught up with yet (spec FR-014).
func (s *PublicService) paymentInstruction(record OrderRecord, now time.Time) *PaymentInstruction {
	if record.Status != statusPending {
		return nil
	}
	if record.PaymentQRString == nil || *record.PaymentQRString == "" {
		return nil
	}
	if record.PaymentExpiresAt == nil || !record.PaymentExpiresAt.After(now) {
		return nil
	}

	provider := ""
	if record.PaymentProvider != nil {
		provider = *record.PaymentProvider
	}

	return &PaymentInstruction{
		Method:      paymentMethod,
		Provider:    provider,
		Amount:      money.From(record.TotalAmount),
		ExpiresAt:   record.PaymentExpiresAt.UTC(),
		QRImagePath: QRImagePath(record.OrderNumber),
	}
}

// QRImagePath is where the order's QR code is rendered on demand. Exported so
// the handler and the DTO agree on one definition of the route.
func QRImagePath(orderNumber string) string {
	return fmt.Sprintf("/api/v1/orders/%s/qris.png", orderNumber)
}

// ticketTypeDisplays resolves every line's ticket type through the event domain
// in one batched lookup — never one per row, and never a JOIN across the domain
// boundary (Constitution Principle II).
func (s *PublicService) ticketTypeDisplays(ctx context.Context, items []OrderItemRecord) (map[uuid.UUID]TicketTypeDisplay, error) {
	if len(items) == 0 {
		return map[uuid.UUID]TicketTypeDisplay{}, nil
	}

	seen := make(map[uuid.UUID]struct{}, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.TicketTypeID]; ok {
			continue
		}
		seen[item.TicketTypeID] = struct{}{}
		ids = append(ids, item.TicketTypeID)
	}

	return s.events.TicketTypeDisplays(ctx, ids)
}

// eventOf names the event this order belongs to. An order is placed from a
// single event's page, so the first line settles it; a line whose ticket type
// has since been deleted simply leaves the name blank rather than failing the
// whole read.
func eventOf(items []OrderItemRecord, displays map[uuid.UUID]TicketTypeDisplay) PublicOrderEvent {
	for _, item := range items {
		if display, ok := displays[item.TicketTypeID]; ok {
			return PublicOrderEvent{Name: display.EventName, Slug: display.EventSlug}
		}
	}
	return PublicOrderEvent{}
}

func publicItems(items []OrderItemRecord, displays map[uuid.UUID]TicketTypeDisplay) []PublicOrderItem {
	out := make([]PublicOrderItem, 0, len(items))
	for _, item := range items {
		out = append(out, PublicOrderItem{
			TicketTypeName: displays[item.TicketTypeID].TicketTypeName,
			Quantity:       item.Quantity,
			UnitPrice:      money.From(item.Price),
			// Recomputed rather than stored: order_items holds the unit price it
			// was sold at, and the line total follows from it.
			Subtotal: money.From(item.Price.Mul(decimal.NewFromInt32(item.Quantity))),
		})
	}
	return out
}
