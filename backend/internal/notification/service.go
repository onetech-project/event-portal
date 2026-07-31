package notification

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// OrderDelivery is the slice of an order this domain needs to address an email.
type OrderDelivery struct {
	ID          uuid.UUID
	OrderNumber string
	BuyerName   string
	BuyerEmail  string
	Status      string
}

// OrderProvider is the contract this domain needs from the order domain, declared
// here by its consumer (ARCHITECTURE.md §3.2). Nothing here imports
// internal/order — the composition root adapts one onto the other.
type OrderProvider interface {
	// OrderForDelivery returns the buyer's contact details and the order status.
	OrderForDelivery(ctx context.Context, orderID uuid.UUID) (OrderDelivery, error)
	// MarkEmailSent records a successful delivery.
	MarkEmailSent(ctx context.Context, orderID uuid.UUID) error
}

// TicketProvider is the contract this domain needs from the ticket domain: the
// already-issued codes and the event details printed alongside them.
type TicketProvider interface {
	TicketDetailsForOrder(ctx context.Context, orderID uuid.UUID) ([]TicketDetail, error)
}

// Service renders and delivers ticket emails.
type Service struct {
	orders  OrderProvider
	tickets TicketProvider
	mailer  Mailer
	log     *logger.Logger
}

// NewService builds the notification service.
func NewService(orders OrderProvider, tickets TicketProvider, mailer Mailer, log *logger.Logger) *Service {
	return &Service{orders: orders, tickets: tickets, mailer: mailer, log: log}
}

// SendTicketEmail delivers an order's tickets to its buyer as one PDF.
//
// This single path serves both the automatic post-payment send and the admin
// resend, so the two can never drift apart. It is safely re-runnable: the existing
// ticket_code values are reused verbatim and never regenerated, while each QR image
// is re-rendered from its code at render time — a guest's original ticket stays
// valid after any number of resends (specs/003 FR-011).
func (s *Service) SendTicketEmail(ctx context.Context, orderID uuid.UUID) error {
	order, err := s.orders.OrderForDelivery(ctx, orderID)
	if err != nil {
		return err
	}

	// Only PAID orders have generated tickets. PENDING, CANCELLED, and EXPIRED all
	// fail here rather than sending an empty or misleading email.
	if order.Status != "PAID" {
		return apperr.BadRequest(apperr.CodeOrderNotPaid,
			fmt.Sprintf("Tickets can only be sent for a paid order; this order is %s.", order.Status))
	}

	tickets, err := s.tickets.TicketDetailsForOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if len(tickets) == 0 {
		return errors.New("notification: order is paid but has no tickets to deliver")
	}

	pdf, err := RenderTicketsPDF(order, tickets)
	if err != nil {
		return err
	}

	if err := s.mailer.Send(Message{
		To:       order.BuyerEmail,
		Subject:  fmt.Sprintf("Your tickets for %s", tickets[0].EventName),
		HTMLBody: buildEmailBody(order, tickets),
		Attachments: []Attachment{{
			Filename:    fmt.Sprintf("tickets-%s.pdf", order.OrderNumber),
			ContentType: "application/pdf",
			Content:     pdf,
		}},
	}); err != nil {
		// Deliberately not marking email_sent: the flag means "the buyer has it".
		return err
	}

	if err := s.orders.MarkEmailSent(ctx, orderID); err != nil {
		// The email is genuinely out. Failing here would invite a retry that
		// double-sends to the buyer, so the flag miss is logged instead.
		s.log.ErrorContext(ctx, "ticket email delivered but email_sent could not be recorded",
			"order_number", order.OrderNumber, "order_id", orderID.String(), "error", err.Error())
	}

	s.log.InfoContext(ctx, "ticket email delivered",
		"order_number", order.OrderNumber, "ticket_count", len(tickets))
	return nil
}

func buildEmailBody(order OrderDelivery, tickets []TicketDetail) string {
	var sb strings.Builder

	sb.WriteString(`<div style="font-family:Helvetica,Arial,sans-serif;font-size:15px;color:#111">`)
	fmt.Fprintf(&sb, "<p>Hi %s,</p>", html.EscapeString(order.BuyerName))
	fmt.Fprintf(&sb,
		"<p>Your payment for order <strong>%s</strong> is confirmed. "+
			"Your %d ticket(s) for <strong>%s</strong> are attached as a PDF.</p>",
		html.EscapeString(order.OrderNumber), len(tickets), html.EscapeString(tickets[0].EventName))

	sb.WriteString("<ul>")
	for _, ticket := range tickets {
		fmt.Fprintf(&sb, "<li>%s — %s (<code>%s</code>)</li>",
			html.EscapeString(ticket.AttendeeName),
			html.EscapeString(ticket.TicketTypeName),
			html.EscapeString(ticket.TicketCode))
	}
	sb.WriteString("</ul>")

	sb.WriteString("<p>Show the QR code or read out the ticket code at the entrance. " +
		"Each ticket admits one person and can only be used once.</p>")
	sb.WriteString("<p>See you there!</p></div>")

	return sb.String()
}
