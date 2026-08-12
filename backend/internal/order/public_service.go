package order

import (
	"context"
	"errors"
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

// TicketOrderByNumber assembles the 008 guest order read
// (GET /ticket/order/:order_id): status, live deadline, agreement stamp,
// whether payment has started, and the attendee slots with details or nulls.
//
// Like OrderByNumber it stays a handful of indexed reads — this is the polling
// fallback while SSE is down, so no provider call and no writes.
func (s *PublicService) TicketOrderByNumber(ctx context.Context, orderNumber string) (TicketOrderDetail, error) {
	record, err := s.repo.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return TicketOrderDetail{}, apperr.NotFound(apperr.CodeOrderNotFound, orderNotFoundMessage)
	}
	if err != nil {
		return TicketOrderDetail{}, err
	}

	items, err := s.repo.ListOrderItemsByOrderID(ctx, record.ID)
	if err != nil {
		return TicketOrderDetail{}, err
	}
	displays, err := s.lineDisplays(ctx, items)
	if err != nil {
		return TicketOrderDetail{}, err
	}
	slots, err := s.repo.ListAttendeeSlotsByOrderID(ctx, record.ID)
	if err != nil {
		return TicketOrderDetail{}, err
	}
	feeRecords, err := s.repo.ListOrderFeesByOrderID(ctx, record.ID)
	if err != nil {
		return TicketOrderDetail{}, err
	}
	fees := make([]PublicOrderFee, 0, len(feeRecords))
	for _, fee := range feeRecords {
		fees = append(fees, PublicOrderFee{Name: fee.Name, Amount: money.From(fee.Amount)})
	}
	var subtotal *money.Money
	if record.Subtotal.Valid {
		v := money.From(record.Subtotal.Decimal)
		subtotal = &v
	}

	slotDTOs := make([]TicketOrderSlot, 0, len(slots))
	for _, slot := range slots {
		dto := TicketOrderSlot{
			ID:             slot.ID,
			TicketTypeName: slot.TicketTypeName,
			Name:           slot.Name,
			Email:          slot.Email,
			Phone:          slot.Phone,
			Gender:         slot.Gender,
		}
		if slot.PackageID.Valid {
			pkgID := slot.PackageID.UUID
			dto.PackageID = &pkgID
			if display, ok := displays.packages[pkgID]; ok {
				name := display.PackageName
				dto.PackageName = &name
			}
		}
		if slot.PackageUnit != nil {
			unit := int(*slot.PackageUnit)
			dto.PackageUnit = &unit
		}
		if slot.Dob != nil {
			dob := slot.Dob.Format("2006-01-02")
			dto.Dob = &dob
		}
		slotDTOs = append(slotDTOs, dto)
	}

	now := s.now()
	return TicketOrderDetail{
		OrderID:        record.OrderNumber,
		Status:         record.Status,
		TotalAmount:    money.From(record.TotalAmount),
		Subtotal:       subtotal,
		Fees:           fees,
		ExpiresAt:      record.PaymentExpiresAt,
		TermsAgreedAt:  record.TermsAgreedAt,
		PaymentStarted: record.PaymentQRString != nil && *record.PaymentQRString != "",
		Event:          eventOf(items, displays),
		Items:          publicItems(items, displays),
		Slots:          slotDTOs,
		ServerTime:     now.UTC(),
		Payment:        s.paymentInstruction(record, now),
	}, nil
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

// QRImagePath is where the order's QR code is rendered on demand — the 008
// /ticket/order namespace (one definition, shared with the checkout response).
func QRImagePath(orderNumber string) string {
	return TicketQRImagePath(orderNumber)
}

// lineDisplays is every label an order's lines need, resolved through the event
// domain in two batched lookups — never one per row, and never a JOIN across the
// domain boundary (Constitution Principle II).
//
// Both maps are needed because a line is a ticket type XOR a package: an order
// made entirely of bundles populates only the second.
type lineDisplays struct {
	ticketTypes map[uuid.UUID]TicketTypeDisplay
	packages    map[uuid.UUID]PackageDisplay
}

func (s *PublicService) lineDisplays(ctx context.Context, items []OrderItemRecord) (lineDisplays, error) {
	out := lineDisplays{
		ticketTypes: map[uuid.UUID]TicketTypeDisplay{},
		packages:    map[uuid.UUID]PackageDisplay{},
	}
	if len(items) == 0 {
		return out, nil
	}

	ticketTypeIDs := distinctRefs(items, func(r LineRef) uuid.NullUUID { return r.TicketTypeID })
	packageIDs := distinctRefs(items, func(r LineRef) uuid.NullUUID { return r.PackageID })

	if len(ticketTypeIDs) > 0 {
		displays, err := s.events.TicketTypeDisplays(ctx, ticketTypeIDs)
		if err != nil {
			return lineDisplays{}, err
		}
		out.ticketTypes = displays
	}
	if len(packageIDs) > 0 {
		displays, err := s.events.PackageDisplays(ctx, packageIDs)
		if err != nil {
			return lineDisplays{}, err
		}
		out.packages = displays
	}
	return out, nil
}

// distinctRefs collects the set ids one side of the XOR contributes, skipping the
// lines of the other kind.
func distinctRefs(items []OrderItemRecord, pick func(LineRef) uuid.NullUUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ref := pick(item.Ref)
		if !ref.Valid {
			continue
		}
		if _, ok := seen[ref.UUID]; ok {
			continue
		}
		seen[ref.UUID] = struct{}{}
		ids = append(ids, ref.UUID)
	}
	return ids
}

// eventOf names the event this order belongs to. An order is placed from a
// single event's page, so the first line that resolves settles it; a line whose
// ticket type or package has since been deleted simply leaves the name blank
// rather than failing the whole read.
func eventOf(items []OrderItemRecord, displays lineDisplays) PublicOrderEvent {
	for _, item := range items {
		if id := item.Ref.TicketTypeID; id.Valid {
			if display, ok := displays.ticketTypes[id.UUID]; ok {
				return PublicOrderEvent{
					Name:      display.EventName,
					Slug:      display.EventSlug,
					Venue:     display.EventVenue,
					Address:   display.EventAddress,
					StartDate: display.EventStartDate,
					EndDate:   display.EventEndDate,
				}
			}
		}
		if id := item.Ref.PackageID; id.Valid {
			if display, ok := displays.packages[id.UUID]; ok {
				return PublicOrderEvent{
					Name:      display.EventName,
					Slug:      display.EventSlug,
					Venue:     display.EventVenue,
					Address:   display.EventAddress,
					StartDate: display.EventStartDate,
					EndDate:   display.EventEndDate,
				}
			}
		}
	}
	return PublicOrderEvent{}
}

func publicItems(items []OrderItemRecord, displays lineDisplays) []PublicOrderItem {
	out := make([]PublicOrderItem, 0, len(items))
	for _, item := range items {
		line := PublicOrderItem{
			Kind:      LineKindTicket,
			Quantity:  item.Quantity,
			UnitPrice: money.From(item.Price),
			// Recomputed rather than stored: order_items holds the unit price it
			// was sold at, and the line total follows from it.
			Subtotal: money.From(item.Price.Mul(decimal.NewFromInt32(item.Quantity))),
		}

		if id := item.Ref.PackageID; id.Valid {
			// A package line is shown whole, under the bundle's own name and its
			// own price — never decomposed into its constituent ticket types.
			line.Kind = LineKindPackage
			display := displays.packages[id.UUID]
			name := display.PackageName
			line.PackageName = &name
			line.AdmissionStarts = display.AdmissionStarts
		} else if id := item.Ref.TicketTypeID; id.Valid {
			display := displays.ticketTypes[id.UUID]
			name := display.TicketTypeName
			line.TicketTypeName = &name
			line.AdmissionStarts = display.AdmissionStarts
		}

		out = append(out, line)
	}
	return out
}
