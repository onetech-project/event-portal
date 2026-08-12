package notification

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// OrderDelivery is the slice of an order this domain needs to address and
// itemize the receipt emails.
//
// BuyerName/BuyerEmail are the PRIMARY CONTACT — the topmost holder form's
// snapshot (spec 011). Delivery goes to each holder's own attendee email;
// BuyerEmail is only the defensive fallback for a slot with no address.
type OrderDelivery struct {
	ID          uuid.UUID
	OrderNumber string
	BuyerName   string
	BuyerEmail  string
	Status      string
	// TotalAmount is the charged amount, printed on every receipt (Figma 251-2).
	TotalAmount decimal.Decimal
	// Subtotal is the pre-fee sum of the lines; nil on pre-fee orders, which
	// collapses the receipt's breakdown to the total only.
	Subtotal *decimal.Decimal
	// Items and Fees itemize the FULL order on every recipient's receipt
	// (spec 011 FR-012: each holder receives the order receipt, complete and
	// self-consistent, not a per-holder subset).
	Items []ReceiptLine
	Fees  []ReceiptFee
}

// ReceiptLine is one purchased order line as the receipt prints it.
type ReceiptLine struct {
	Name      string
	Quantity  int32
	UnitPrice decimal.Decimal
	Subtotal  decimal.Decimal
}

// ReceiptFee is one frozen fee line, exactly as booking computed it.
type ReceiptFee struct {
	Name   string
	Amount decimal.Decimal
}

// OrderProvider is the contract this domain needs from the order domain, declared
// here by its consumer (ARCHITECTURE.md §3.2). Nothing here imports
// internal/order — the composition root adapts one onto the other.
type OrderProvider interface {
	// OrderForDelivery returns the buyer's contact details and the order status.
	OrderForDelivery(ctx context.Context, orderID uuid.UUID) (OrderDelivery, error)
	// MarkEmailSent records a successful delivery.
	MarkEmailSent(ctx context.Context, orderID uuid.UUID) error
	// OrderIDByNumber resolves the public order number a guest holds to the
	// internal ID the rest of this domain works in. The guest-facing resend is
	// keyed by order number — it is what the confirmation screen has, and the
	// only identifier a buyer ever sees.
	OrderIDByNumber(ctx context.Context, orderNumber string) (uuid.UUID, error)
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

// SendTicketEmail delivers an order's tickets to the BUYER (spec 011 FR-012,
// constitution v3.0.0): exactly ONE email, addressed to the order's
// primary-contact snapshot — the first holder form's address, that form being
// ticket holder 1 and the buyer at once — carrying every ticket in the order as
// a single PDF plus the full receipt. It returns that address, which the admin
// resend surfaces as sent_to.
//
// The other attendees' email addresses are holder identity, not delivery
// addresses: spec 011 briefly fanned delivery out across them and the
// 2026-08-06 clarification reversed it. They are still collected and stored, so
// widening delivery again would be a change here alone.
//
// This single path serves the automatic post-payment send and both resends, so
// they can never drift apart. It is safely re-runnable: the existing
// ticket_code values are reused verbatim and never regenerated, while each QR
// image is re-rendered from its code at render time — a guest's original
// ticket stays valid after any number of resends (specs/003 FR-011).
func (s *Service) SendTicketEmail(ctx context.Context, orderID uuid.UUID) (string, error) {
	order, err := s.orders.OrderForDelivery(ctx, orderID)
	if err != nil {
		return "", err
	}

	// Only PAID orders have generated tickets. PENDING, CANCELLED, and EXPIRED all
	// fail here rather than sending an empty or misleading email.
	if order.Status != "PAID" {
		return "", apperr.BadRequest(apperr.CodeOrderNotPaid,
			fmt.Sprintf("Tickets can only be sent for a paid order; this order is %s.", order.Status))
	}

	tickets, err := s.tickets.TicketDetailsForOrder(ctx, orderID)
	if err != nil {
		return "", err
	}
	if len(tickets) == 0 {
		return "", errors.New("notification: order is paid but has no tickets to deliver")
	}

	// The buyer's snapshot address is the recipient. Checkout writes it from the
	// first canonical slot's form, so it is exactly what the guest typed into
	// the card carrying the "sent via email" notice. An empty snapshot would
	// mean an order that never went through checkout — refuse rather than mail
	// nobody and then mark the order delivered.
	recipient := strings.TrimSpace(order.BuyerEmail)
	if recipient == "" {
		return "", errors.New("notification: order has no buyer email to deliver to")
	}

	// One PDF holding every ticket in the order.
	pdf, err := RenderTicketsPDF(order, tickets)
	if err != nil {
		return "", fmt.Errorf("render tickets for %s: %w", order.OrderNumber, err)
	}

	if err := s.mailer.Send(Message{
		To:       recipient,
		Subject:  fmt.Sprintf("Your tickets for %s", tickets[0].EventName),
		HTMLBody: buildEmailBody(order, tickets, recipient),
		Attachments: []Attachment{{
			Filename:    fmt.Sprintf("tickets-%s.pdf", order.OrderNumber),
			ContentType: "application/pdf",
			Content:     pdf,
		}},
	}); err != nil {
		// email_sent stays FALSE, so resend remains armed.
		return "", fmt.Errorf("send to %s: %w", recipient, err)
	}

	if err := s.orders.MarkEmailSent(ctx, orderID); err != nil {
		// The email is genuinely out. Failing here would invite a retry that
		// double-sends, so the flag miss is logged instead.
		s.log.ErrorContext(ctx, "ticket email delivered but email_sent could not be recorded",
			"order_number", order.OrderNumber, "order_id", orderID.String(), "error", err.Error())
	}

	s.log.InfoContext(ctx, "ticket email delivered",
		"order_number", order.OrderNumber, "ticket_count", len(tickets))
	return recipient, nil
}

// Receipt palette, mirrored from the frontend brand tokens so the email and the
// site read as one product (Figma 251-2).
const (
	emailBrand   = "#cb1c4f"
	emailSurface = "#fbeff3"
	emailLine    = "#ecabbf"
)

// buildEmailBody renders the buyer's receipt + e-ticket email (Figma 251-2): a
// header with the PAID badge and invoice number, every ticket holder in the
// order, the transaction details, one e-ticket card per ticket, the full order
// summary (every line + the frozen fee breakdown), and a dark footer. Everything
// is inline-styled tables — email clients ignore stylesheets.
func buildEmailBody(order OrderDelivery, tickets []TicketDetail, recipient string) string {
	var sb strings.Builder
	event := tickets[0]

	sb.WriteString(`<div style="background:#f4f4f5;padding:24px 8px;font-family:Helvetica,Arial,sans-serif;color:#18181b">`)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:640px;margin:0 auto;background:#ffffff;border-radius:12px;overflow:hidden">`)

	// Header: brand + event on the left, PAID badge and invoice on the right.
	sb.WriteString(`<tr><td style="padding:24px 28px;border-bottom:1px solid #e4e4e7">`)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>`)
	fmt.Fprintf(&sb, `<td style="font-size:18px;font-weight:bold;color:%s">%s</td>`,
		emailBrand, html.EscapeString(event.EventName))
	sb.WriteString(`<td align="right" style="font-size:12px;color:#52525b">`)
	sb.WriteString(`<span style="display:inline-block;background:#dcfce7;color:#15803d;font-weight:bold;font-size:11px;letter-spacing:.5px;padding:4px 10px;border-radius:999px">PAID</span>`)
	fmt.Fprintf(&sb, `<br>Inv: #%s`, html.EscapeString(order.OrderNumber))
	sb.WriteString(`</td></tr></table></td></tr>`)

	// Ticket holder(s) + transaction details, two columns. One name per distinct
	// holder in this recipient's group (two holders may share one address —
	// spec 011 edge case), addressed to the email this copy goes to.
	sb.WriteString(`<tr><td style="padding:20px 28px;border-bottom:1px dashed #d4d4d8">`)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>`)
	sb.WriteString(`<td valign="top" style="font-size:13px;color:#3f3f46">`)
	sb.WriteString(`<div style="font-size:11px;letter-spacing:.5px;color:#a1a1aa;font-weight:bold">TICKET HOLDER</div>`)
	for _, name := range distinctHolderNames(tickets) {
		fmt.Fprintf(&sb, `<div style="font-size:15px;font-weight:bold;padding-top:4px">%s</div>`,
			html.EscapeString(name))
	}
	fmt.Fprintf(&sb, `<div>%s</div>`, html.EscapeString(recipient))
	sb.WriteString(`</td>`)
	sb.WriteString(`<td valign="top" style="font-size:13px;color:#3f3f46">`)
	sb.WriteString(`<div style="font-size:11px;letter-spacing:.5px;color:#a1a1aa;font-weight:bold">TRANSACTION</div>`)
	sb.WriteString(`<div style="padding-top:4px">Payment method: QRIS</div>`)
	sb.WriteString(`<div>Status: <span style="color:#15803d;font-weight:bold">Successful</span></div>`)
	sb.WriteString(`</td></tr></table></td></tr>`)

	// One e-ticket card per attendee.
	sb.WriteString(`<tr><td style="padding:20px 28px 8px">`)
	sb.WriteString(`<div style="font-size:16px;font-weight:bold">E-Ticket</div>`)
	for _, ticket := range tickets {
		fmt.Fprintf(&sb, `<div style="background:%s;border:1px solid %s;border-radius:12px;padding:16px 20px;margin-top:12px">`,
			emailSurface, emailLine)
		fmt.Fprintf(&sb, `<span style="display:inline-block;background:%s;color:#ffffff;font-size:11px;font-weight:bold;letter-spacing:.5px;padding:3px 10px;border-radius:6px">%s</span>`,
			emailBrand, html.EscapeString(strings.ToUpper(ticket.TicketTypeName)))
		fmt.Fprintf(&sb, `<div style="font-size:17px;font-weight:bold;padding-top:8px">%s</div>`,
			html.EscapeString(ticket.EventName))
		fmt.Fprintf(&sb, `<div style="font-size:13px;color:#52525b;padding-top:4px">%s · %s</div>`,
			html.EscapeString(FormatTicketWindow(ticket.EventStart, ticket.EventEnd)),
			html.EscapeString(ticket.Venue))
		sb.WriteString(`<div style="border-top:1px solid ` + emailLine + `;margin-top:12px;padding-top:10px;font-size:12px;color:#a1a1aa">Attendee</div>`)
		fmt.Fprintf(&sb, `<div style="font-size:15px;font-weight:bold">%s</div>`,
			html.EscapeString(ticket.AttendeeName))
		fmt.Fprintf(&sb, `<div style="font-size:12px;color:#52525b;padding-top:6px">Show at entry: <code style="font-size:13px;font-weight:bold">%s</code></div>`,
			html.EscapeString(ticket.TicketCode))
		sb.WriteString(`</div>`)
	}
	sb.WriteString(`<div style="font-size:12px;color:#52525b;padding-top:10px">Your QR codes are in the attached PDF. Each ticket admits one person and can only be used once.</div>`)
	sb.WriteString(`</td></tr>`)

	// Order summary: the FULL order in every copy (spec 011 — the receipt each
	// holder gets is the order receipt, not a per-holder subset). Pre-fee
	// orders (nil subtotal) collapse to the Total Payment row alone.
	sb.WriteString(`<tr><td style="padding:16px 28px">`)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-top:1px solid #e4e4e7">`)
	if order.Subtotal != nil {
		for _, line := range order.Items {
			fmt.Fprintf(&sb, `<tr><td style="font-size:13px;color:#3f3f46;padding-top:10px">%s <span style="color:#a1a1aa">x%d</span></td>`,
				html.EscapeString(line.Name), line.Quantity)
			fmt.Fprintf(&sb, `<td align="right" style="font-size:13px;color:#3f3f46;padding-top:10px">%s</td></tr>`,
				formatIDR(line.Subtotal))
		}
		sb.WriteString(`<tr><td style="font-size:13px;font-weight:bold;padding-top:10px">Ticket Total</td>`)
		fmt.Fprintf(&sb, `<td align="right" style="font-size:13px;font-weight:bold;padding-top:10px">%s</td></tr>`,
			formatIDR(*order.Subtotal))
		for _, fee := range order.Fees {
			fmt.Fprintf(&sb, `<tr><td style="font-size:13px;color:#3f3f46;padding-top:6px">%s</td>`,
				html.EscapeString(fee.Name))
			fmt.Fprintf(&sb, `<td align="right" style="font-size:13px;color:#3f3f46;padding-top:6px">%s</td></tr>`,
				formatIDR(fee.Amount))
		}
	}
	sb.WriteString(`<tr>`)
	sb.WriteString(`<td style="font-size:14px;font-weight:bold;padding-top:12px">Total Payment</td>`)
	fmt.Fprintf(&sb, `<td align="right" style="font-size:18px;font-weight:bold;color:%s;padding-top:12px">%s</td>`,
		emailBrand, formatIDR(order.TotalAmount))
	sb.WriteString(`</tr></table></td></tr>`)

	// Dark footer.
	sb.WriteString(`<tr><td style="background:#18181b;color:#a1a1aa;font-size:11px;text-align:center;padding:14px 28px;line-height:1.6">`)
	sb.WriteString(`This is a valid proof of payment and your official e-ticket. No signature is required.`)
	sb.WriteString(`</td></tr>`)

	sb.WriteString(`</table></div>`)
	return sb.String()
}

// distinctHolderNames returns the order's holder names in first-occurrence
// order — a bundle unit's N identical rows contribute one name, while distinct
// holders each contribute their own — so the buyer's email lists everyone the
// passes are for.
func distinctHolderNames(tickets []TicketDetail) []string {
	seen := make(map[string]bool, len(tickets))
	names := make([]string, 0, len(tickets))
	for _, t := range tickets {
		if seen[t.AttendeeName] {
			continue
		}
		seen[t.AttendeeName] = true
		names = append(names, t.AttendeeName)
	}
	return names
}

// formatIDR renders an amount the way the site does: "Rp 550.000". Rupiah has no
// cent display, so the decimal part is dropped.
func formatIDR(amount decimal.Decimal) string {
	digits := strconv.FormatInt(amount.IntPart(), 10)
	var grouped []byte
	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			grouped = append(grouped, '.')
		}
		grouped = append(grouped, digits[i])
	}
	return "Rp " + string(grouped)
}
