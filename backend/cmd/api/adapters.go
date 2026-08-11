package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// This file is the only place where two domains meet.
//
// Each domain declares the contract it needs from its neighbours (ARCHITECTURE.md
// §3.2), and the adapters below translate one domain's types into another's. No
// internal/<domain> package imports another, so any of them can be lifted into its
// own service later without untangling an import graph (Constitution Principle II).

// --- order.EventProvider: checkout's view of the event domain --------------

type eventProviderAdapter struct{ events *event.Service }

func (a eventProviderAdapter) TicketTypeForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (order.TicketTypeInfo, error) {
	row, err := a.events.GetTicketTypeForCheckout(ctx, tx, id)
	if err != nil {
		return order.TicketTypeInfo{}, err
	}
	return order.TicketTypeInfo{
		ID:         row.ID,
		EventID:    row.EventID,
		Name:       row.Name,
		Price:      row.Price,
		SalesStart: row.SalesStart,
		SalesEnd:   row.SalesEnd,
		// Already on the row — GetTicketTypeByID selects quota — so carrying it
		// across costs no query, no round trip and no lock. See the field's own
		// doc comment for why booking must not read it.
		QuotaRemaining: row.Quota,
	}, nil
}

func (a eventProviderAdapter) CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, id uuid.UUID, qty int32) error {
	err := a.events.CheckAndDeductQuota(ctx, tx, id, qty)
	if errors.Is(err, event.ErrInsufficientQuota) {
		return order.ErrInsufficientQuota
	}
	return err
}

func (a eventProviderAdapter) PackageForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (order.PackageInfo, error) {
	pkg, err := a.events.GetPackageForCheckout(ctx, tx, id)
	if err != nil {
		return order.PackageInfo{}, err
	}
	info := order.PackageInfo{
		ID:         pkg.ID,
		EventID:    pkg.EventID,
		Name:       pkg.Name,
		Price:      pkg.Price,
		SalesStart: pkg.SalesStart,
		SalesEnd:   pkg.SalesEnd,
		Components: make([]order.PackageComponentInfo, 0, len(pkg.Components)),
	}
	for _, c := range pkg.Components {
		info.Components = append(info.Components, order.PackageComponentInfo{
			TicketTypeID: c.TicketTypeID,
			Quantity:     c.PerUnit,
			SalesStart:   c.SalesStart,
			SalesEnd:     c.SalesEnd,
		})
	}
	return info, nil
}

func (a eventProviderAdapter) RestoreQuota(ctx context.Context, tx pgx.Tx, id uuid.UUID, qty int32) error {
	return a.events.RestoreQuota(ctx, tx, id, qty)
}

func (a eventProviderAdapter) CurrentTerms(ctx context.Context, eventID uuid.UUID) (order.EventTermsInfo, error) {
	row, err := a.events.CurrentTermsForEvent(ctx, eventID)
	if errors.Is(err, event.ErrNotFound) {
		return order.EventTermsInfo{}, order.ErrNoTerms
	}
	if err != nil {
		return order.EventTermsInfo{}, err
	}
	return order.EventTermsInfo{ID: row.ID, EventID: eventID}, nil
}

// --- order.PaymentGateway: checkout's view of the payment provider ---------

type gatewayAdapter struct {
	gateway payment.Gateway
	// payments is set after the payment service exists, because the two are
	// mutually dependent: checkout needs the gateway, and a duplicate-reference
	// refusal needs the payment service to release the order and record it.
	//
	// The back-reference lives here rather than inside either domain precisely
	// because this file is where domains are allowed to know about each other. A
	// nil value degrades to "translate the error but release nothing", which is
	// wrong but not silent — the log line below says so.
	payments *payment.Service
	log      *logger.Logger
}

func (a *gatewayAdapter) Name() string { return a.gateway.Name() }

func (a *gatewayAdapter) CreateTransaction(ctx context.Context, req order.PaymentRequest) (order.PaymentSession, error) {
	items := make([]payment.TransactionItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, payment.TransactionItem{
			ID: item.ID, Name: item.Name, Price: item.Price, Quantity: item.Quantity,
		})
	}

	session, err := a.gateway.CreateTransaction(ctx, payment.TransactionRequest{
		OrderNumber:   req.OrderNumber,
		GrossAmount:   req.GrossAmount,
		CustomerName:  req.CustomerName,
		CustomerEmail: req.CustomerEmail,
		CustomerPhone: req.CustomerPhone,
		Items:         items,
	})
	if errors.Is(err, payment.ErrDuplicateReference) {
		// Release the seats now and record why on the order's own history, then
		// translate onto the order domain's own sentinel so checkout can tell the
		// guest to start again rather than offering a retry that cannot work.
		if a.payments != nil {
			if releaseErr := a.payments.ReleaseDuplicateSession(ctx, req.OrderNumber, err); releaseErr != nil {
				a.log.ErrorContext(ctx, "could not release an order refused as a duplicate reference; its seats stay held until the sweeper",
					"order_number", req.OrderNumber, "error", releaseErr.Error())
			}
		} else {
			a.log.ErrorContext(ctx, "duplicate reference with no payment service wired; seats stay held until the sweeper",
				"order_number", req.OrderNumber)
		}
		return order.PaymentSession{}, fmt.Errorf("%w: %w", order.ErrGatewaySessionDuplicate, err)
	}
	if err != nil {
		return order.PaymentSession{}, err
	}

	return order.PaymentSession{
		ProviderRef:       session.ProviderRef,
		QRString:          session.QRString,
		QRImageURL:        session.QRImageURL,
		ExpiresAt:         session.ExpiresAt,
		ExpiryFromGateway: session.ExpiryFromGateway,
		RedirectURL:       session.RedirectURL,
	}, nil
}

// --- payment.OrderProvider: the webhook's view of the order domain ---------

type paymentOrderAdapter struct{ orders *order.Repository }

func (a paymentOrderAdapter) OrderByNumber(ctx context.Context, orderNumber string) (payment.OrderRef, error) {
	rec, err := a.orders.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, order.ErrNotFound) {
		return payment.OrderRef{}, payment.ErrOrderNotFound
	}
	if err != nil {
		return payment.OrderRef{}, err
	}
	return payment.OrderRef{
		ID:               rec.ID,
		OrderNumber:      rec.OrderNumber,
		Status:           rec.Status,
		PaymentExpiresAt: rec.PaymentExpiresAt,
	}, nil
}

func (a paymentOrderAdapter) OrderByID(ctx context.Context, orderID uuid.UUID) (payment.OrderRef, error) {
	rec, err := a.orders.GetOrderByID(ctx, orderID)
	if errors.Is(err, order.ErrNotFound) {
		return payment.OrderRef{}, payment.ErrOrderNotFound
	}
	if err != nil {
		return payment.OrderRef{}, err
	}
	return payment.OrderRef{
		ID:               rec.ID,
		OrderNumber:      rec.OrderNumber,
		Status:           rec.Status,
		PaymentExpiresAt: rec.PaymentExpiresAt,
	}, nil
}

// SettleExpired backs the settle path: a redelivered notification reviving an
// order this system had already expired (FR-019).
//
// Both statuses are fixed here rather than passed in. The underlying query can
// express any from→to move, and binding it at the composition root is what makes
// FR-019d unreachable by construction: no caller in the payment domain can ask
// for a CANCELLED order to be revived, because the method has nowhere to say it.
func (a paymentOrderAdapter) SettleExpired(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error) {
	return a.orders.UpdateOrderStatusFrom(ctx, tx, orderID, payment.OrderStatusExpired, payment.OrderStatusPaid)
}

func (a paymentOrderAdapter) DueForExpiry(ctx context.Context, now time.Time, limit int32) ([]payment.OrderRef, error) {
	candidates, err := a.orders.ListOrdersDueForExpiry(ctx, now, limit)
	if err != nil {
		return nil, err
	}

	out := make([]payment.OrderRef, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, payment.OrderRef{
			ID:               candidate.ID,
			OrderNumber:      candidate.OrderNumber,
			Status:           candidate.Status,
			PaymentExpiresAt: candidate.PaymentExpiresAt,
		})
	}
	return out, nil
}

func (a paymentOrderAdapter) QuotaHolds(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]payment.QuotaHold, error) {
	// Not ListOrderItemsByOrderID: a package line carries no ticket type, so
	// restoring from raw lines would release nothing for a bundle. This query
	// expands package lines through their composition.
	holds, err := a.orders.ListQuotaHoldsByOrderID(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}

	out := make([]payment.QuotaHold, 0, len(holds))
	for _, hold := range holds {
		out = append(out, payment.QuotaHold{TicketTypeID: hold.TicketTypeID, Quantity: hold.Quantity})
	}
	return out, nil
}

func (a paymentOrderAdapter) UpdateStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, status string) (bool, error) {
	return a.orders.UpdateOrderStatusIfPending(ctx, tx, orderID, status)
}

// --- payment.QuotaReserver: the settle path's view of the event domain -------

// quotaReserverAdapter exists separately from the QuotaRestorer the event
// service already satisfies, because taking quota back is a capability only the
// settle path needs — every other path in the payment domain releases it. It
// translates the event domain's insufficient-quota error onto the payment
// domain's own, so the refusal reads as "these seats are gone" rather than as an
// internal fault.
type quotaReserverAdapter struct{ events *event.Service }

func (a quotaReserverAdapter) ReserveQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	err := a.events.CheckAndDeductQuota(ctx, tx, ticketTypeID, qty)
	if errors.Is(err, event.ErrInsufficientQuota) {
		return payment.ErrQuotaUnavailable
	}
	return err
}

// RemainingQuota reports what each ticket type has left, so a refused settle can
// name the shortfall an operator has to top up (FR-019c) and the holds view can
// show held against remaining (FR-022e).
//
// It reads through the event service rather than joining ticket_types from the
// payment domain, which is what keeps Principle II intact for a figure that
// belongs to another domain's tables.
func (a quotaReserverAdapter) RemainingQuota(ctx context.Context, ticketTypeIDs []uuid.UUID) (map[uuid.UUID]payment.TicketTypeQuota, error) {
	records, err := a.events.TicketTypeQuotas(ctx, ticketTypeIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID]payment.TicketTypeQuota, len(records))
	for id, rec := range records {
		out[id] = payment.TicketTypeQuota{Name: rec.Name, Remaining: rec.Remaining}
	}
	return out, nil
}

// --- notification.OrderProvider: delivery's view of the order domain -------

// guestReads supplies the itemized order summary (line names resolved through
// the event domain) that every recipient's receipt prints since spec 011 —
// reusing the guest order read rather than re-deriving displays here.
type notificationOrderAdapter struct {
	orders     *order.Repository
	guestReads *order.PublicService
}

func (a notificationOrderAdapter) OrderForDelivery(ctx context.Context, orderID uuid.UUID) (notification.OrderDelivery, error) {
	rec, err := a.orders.GetOrderByID(ctx, orderID)
	if errors.Is(err, order.ErrNotFound) {
		// Translated here rather than leaking a repository sentinel: the resend
		// endpoint must answer 404 for an order that does not exist.
		return notification.OrderDelivery{}, apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}
	if err != nil {
		return notification.OrderDelivery{}, err
	}

	// Contact identity is nullable since 008 (booking precedes the forms), but
	// delivery only ever runs for PAID orders, where checkout has snapshotted
	// the primary contact (spec 011).
	buyerName, buyerEmail := "", ""
	if rec.BuyerName != nil {
		buyerName = *rec.BuyerName
	}
	if rec.BuyerEmail != nil {
		buyerEmail = *rec.BuyerEmail
	}

	detail, err := a.guestReads.TicketOrderByNumber(ctx, rec.OrderNumber)
	if err != nil {
		return notification.OrderDelivery{}, err
	}
	items := make([]notification.ReceiptLine, 0, len(detail.Items))
	for _, it := range detail.Items {
		name := ""
		if it.PackageName != nil {
			name = *it.PackageName
		} else if it.TicketTypeName != nil {
			name = *it.TicketTypeName
		}
		items = append(items, notification.ReceiptLine{
			Name:      name,
			Quantity:  it.Quantity,
			UnitPrice: it.UnitPrice.Decimal(),
			Subtotal:  it.Subtotal.Decimal(),
		})
	}
	fees := make([]notification.ReceiptFee, 0, len(detail.Fees))
	for _, fee := range detail.Fees {
		fees = append(fees, notification.ReceiptFee{Name: fee.Name, Amount: fee.Amount.Decimal()})
	}
	var subtotal *decimal.Decimal
	if detail.Subtotal != nil {
		d := detail.Subtotal.Decimal()
		subtotal = &d
	}

	return notification.OrderDelivery{
		ID:          rec.ID,
		OrderNumber: rec.OrderNumber,
		BuyerName:   buyerName,
		BuyerEmail:  buyerEmail,
		Status:      rec.Status,
		TotalAmount: rec.TotalAmount,
		Subtotal:    subtotal,
		Items:       items,
		Fees:        fees,
	}, nil
}

func (a notificationOrderAdapter) MarkEmailSent(ctx context.Context, orderID uuid.UUID) error {
	return a.orders.SetEmailSent(ctx, orderID)
}

func (a notificationOrderAdapter) OrderIDByNumber(ctx context.Context, orderNumber string) (uuid.UUID, error) {
	rec, err := a.orders.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, order.ErrNotFound) {
		return uuid.Nil, apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}
	if err != nil {
		return uuid.Nil, err
	}

	return rec.ID, nil
}

// --- notification.TicketProvider: delivery's view of the ticket domain -----

type notificationTicketAdapter struct{ tickets *ticket.Repository }

func (a notificationTicketAdapter) TicketDetailsForOrder(ctx context.Context, orderID uuid.UUID) ([]notification.TicketDetail, error) {
	details, err := a.tickets.ListDetailsByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}

	out := make([]notification.TicketDetail, 0, len(details))
	for _, detail := range details {
		out = append(out, notification.TicketDetail{
			TicketCode:     detail.TicketCode,
			AttendeeName:   detail.AttendeeName,
			AttendeeEmail:  detail.AttendeeEmail,
			TicketTypeName: detail.TicketTypeName,
			EventName:      detail.EventName,
			Venue:          detail.Venue,
			StartDate:      detail.StartDate,
		})
	}
	return out, nil
}

// --- payment.TicketDeliverer: fulfilment's view of the notification domain --

// ticketDelivererAdapter narrows SendTicketEmail's signature (which returns the
// recipient for the admin resend) back to the error-only contract payment
// declares — fulfilment has no use for the address.
type ticketDelivererAdapter struct{ notifications *notification.Service }

func (a ticketDelivererAdapter) SendTicketEmail(ctx context.Context, orderID uuid.UUID) error {
	_, err := a.notifications.SendTicketEmail(ctx, orderID)
	return err
}

// --- order.EventLookup: the admin order views' view of the event domain ----

type orderEventLookupAdapter struct{ events *event.Service }

func (a orderEventLookupAdapter) TicketTypeNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	return a.events.TicketTypeNames(ctx, ids)
}

func (a orderEventLookupAdapter) TicketTypeIDsForEvent(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error) {
	return a.events.TicketTypeIDsForEvent(ctx, eventID)
}

func (a orderEventLookupAdapter) TicketTypeDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]order.TicketTypeDisplay, error) {
	records, err := a.events.TicketTypeDisplays(ctx, ids)
	if err != nil {
		return nil, err
	}

	displays := make(map[uuid.UUID]order.TicketTypeDisplay, len(records))
	for id, record := range records {
		displays[id] = order.TicketTypeDisplay{
			TicketTypeName: record.TicketTypeName,
			EventName:      record.EventName,
			EventSlug:      record.EventSlug,
			EventVenue:     record.EventVenue,
			EventAddress:   record.EventAddress,
			EventStartDate: record.EventStartDate,
			EventEndDate:   record.EventEndDate,
		}
	}
	return displays, nil
}

func (a orderEventLookupAdapter) PackageDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]order.PackageDisplay, error) {
	records, err := a.events.PackageDisplays(ctx, ids)
	if err != nil {
		return nil, err
	}

	displays := make(map[uuid.UUID]order.PackageDisplay, len(records))
	for id, record := range records {
		displays[id] = order.PackageDisplay{
			PackageName:    record.PackageName,
			EventName:      record.EventName,
			EventSlug:      record.EventSlug,
			EventVenue:     record.EventVenue,
			EventAddress:   record.EventAddress,
			EventStartDate: record.EventStartDate,
			EventEndDate:   record.EventEndDate,
		}
	}
	return displays, nil
}
