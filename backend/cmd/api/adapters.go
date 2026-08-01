package main

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/apperr"
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
	}, nil
}

func (a eventProviderAdapter) CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, id uuid.UUID, qty int32) error {
	err := a.events.CheckAndDeductQuota(ctx, tx, id, qty)
	if errors.Is(err, event.ErrInsufficientQuota) {
		return order.ErrInsufficientQuota
	}
	return err
}

func (a eventProviderAdapter) RestoreQuota(ctx context.Context, tx pgx.Tx, id uuid.UUID, qty int32) error {
	return a.events.RestoreQuota(ctx, tx, id, qty)
}

// --- order.PaymentGateway: checkout's view of the payment provider ---------

type gatewayAdapter struct{ gateway payment.Gateway }

func (a gatewayAdapter) Name() string { return a.gateway.Name() }

func (a gatewayAdapter) CreateTransaction(ctx context.Context, req order.PaymentRequest) (order.PaymentSession, error) {
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
	if err != nil {
		return order.PaymentSession{}, err
	}

	return order.PaymentSession{
		ProviderRef: session.ProviderRef,
		QRString:    session.QRString,
		QRImageURL:  session.QRImageURL,
		ExpiresAt:   session.ExpiresAt,
		RedirectURL: session.RedirectURL,
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

func (a paymentOrderAdapter) LineItems(ctx context.Context, orderID uuid.UUID) ([]payment.LineItem, error) {
	items, err := a.orders.ListOrderItemsByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}

	out := make([]payment.LineItem, 0, len(items))
	for _, item := range items {
		out = append(out, payment.LineItem{TicketTypeID: item.TicketTypeID, Quantity: item.Quantity})
	}
	return out, nil
}

func (a paymentOrderAdapter) UpdateStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, status string) (bool, error) {
	return a.orders.UpdateOrderStatusIfPending(ctx, tx, orderID, status)
}

// --- notification.OrderProvider: delivery's view of the order domain -------

type notificationOrderAdapter struct{ orders *order.Repository }

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

	return notification.OrderDelivery{
		ID:          rec.ID,
		OrderNumber: rec.OrderNumber,
		BuyerName:   rec.BuyerName,
		BuyerEmail:  rec.BuyerEmail,
		Status:      rec.Status,
	}, nil
}

func (a notificationOrderAdapter) MarkEmailSent(ctx context.Context, orderID uuid.UUID) error {
	return a.orders.SetEmailSent(ctx, orderID)
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
			TicketTypeName: detail.TicketTypeName,
			EventName:      detail.EventName,
			Venue:          detail.Venue,
			StartDate:      detail.StartDate,
		})
	}
	return out, nil
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
		}
	}
	return displays, nil
}
