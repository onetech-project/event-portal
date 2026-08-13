package notification

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

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
	// BuyerPhone is the primary contact's phone. Empty means unrecorded, and the
	// surfaces OMIT the row rather than printing a blank or a placeholder
	// (spec 016 FR-027).
	BuyerPhone string
	// CreatedAt is the email body's "Order Date"; UpdatedAt is the receipt's
	// "Last updated" (spec 016 FR-008, FR-024).
	//
	// These are two different facts and the design draws the distinction on
	// purpose. Note that UpdatedAt moves when email_sent is written, so a RESENT
	// receipt legitimately shows a later "Last updated" than the original while
	// every monetary figure is identical — that is correct, not a discrepancy.
	CreatedAt time.Time
	UpdatedAt time.Time
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
	// Event is the event's own identity, for the body's event block and the
	// e-ticket pages (spec 016 FR-025).
	Event DeliveryEvent
	// Payment is the receipt's Transaction Details block (spec 016 FR-010).
	Payment PaymentSummary
}

// DeliveryEvent is the event an order was placed against, as the documents name
// it.
//
// It carries NO dates, deliberately. Every date on these three surfaces is a
// ticket-type date or a per-line admission date — the constitution is explicit
// that a ticket names its ticket type's window and only an event surface names
// the event's. Omitting the fields makes reaching for the wrong one impossible
// rather than merely discouraged.
type DeliveryEvent struct {
	Name    string
	Venue   string
	Address string
}

// PaymentSummary is what the receipt's Transaction Details block prints.
//
// Method is resolved by the adapter, not invented here: the payment row's
// instrument, falling back to the order's provider and then to QRIS. A constant
// would be true today and false the first time Principle V is exercised again —
// which has already happened once (Midtrans -> Manjo, spec 012) — and a wrong
// instrument on a financial document is not a cosmetic defect.
type PaymentSummary struct {
	Method string
	Status string
	PaidAt time.Time
}

// ReceiptLine is one purchased order line as the receipt prints it.
type ReceiptLine struct {
	Name      string
	Quantity  int32
	UnitPrice decimal.Decimal
	Subtotal  decimal.Decimal
	// AdmissionStarts are the days THIS line admits on, ascending: one for a
	// ticket line, one per distinct constituent for a bundle.
	//
	// A list, not a single date, for the reason spec 015 introduced it: a Day 1 +
	// Day 2 bundle cannot be expressed by one value, and collapsing it to the
	// earliest is the exact defect that work removed. Re-collapsing it here would
	// reintroduce that defect in a new place.
	AdmissionStarts []time.Time
	// Descriptor is the line's own admin-authored description, printed after the
	// admission date on the receipt's product sub-line. Empty omits both the
	// separator and the descriptor (spec 016 FR-011).
	Descriptor string
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
	brand   Branding
	log     *logger.Logger
}

// NewService builds the notification service.
func NewService(orders OrderProvider, tickets TicketProvider, mailer Mailer, brand Branding, log *logger.Logger) *Service {
	return &Service{orders: orders, tickets: tickets, mailer: mailer, brand: brand, log: log}
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

	// Both documents are built BEFORE the send, so a failure in either sends
	// nothing at all (spec 016 FR-006). A half-delivered email carrying only one
	// of the two would leave email_sent TRUE and the resend disarmed, which is
	// the one outcome worse than not sending.
	receiptPDF, err := RenderReceiptPDF(order, s.brand)
	if err != nil {
		return "", fmt.Errorf("render receipt for %s: %w", order.OrderNumber, err)
	}

	// One PDF holding every ticket in the order.
	ticketsPDF, err := RenderTicketsPDF(order, tickets, s.brand)
	if err != nil {
		return "", fmt.Errorf("render tickets for %s: %w", order.OrderNumber, err)
	}

	body, inline := buildEmailBody(order, tickets, s.brand)

	if err := s.mailer.Send(Message{
		To:       recipient,
		Subject:  fmt.Sprintf("Your tickets for %s", tickets[0].EventName),
		HTMLBody: body,
		// The brand mark and the location pin, referenced from the body by
		// content ID. Not attachments — a buyer still receives exactly two
		// documents (FR-001).
		Inline: inline,
		// Receipt first. Not required by the spec, but fixed here so two mail
		// clients cannot show the buyer a different first attachment, and so the
		// tests can assert positionally rather than order-tolerantly.
		Attachments: []Attachment{{
			Filename:    fmt.Sprintf("receipt-%s.pdf", order.OrderNumber),
			ContentType: "application/pdf",
			Content:     receiptPDF,
		}, {
			Filename:    fmt.Sprintf("tickets-%s.pdf", order.OrderNumber),
			ContentType: "application/pdf",
			Content:     ticketsPDF,
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

// Email palette, mirrored from the frontend brand tokens so the message and the
// site read as one product (Figma 741-5105).
const (
	emailBrand    = "#cb1c4f"
	emailBand     = "#151a26" // matches the logo's opaque background — see pdfBand
	emailInk      = "#18181b"
	emailMuted    = "#6b7280"
	emailRule     = "#e4e4e7"
	emailPanel    = "#f6f7f9"
	emailNotice   = "#fffbeb"
	emailAmber    = "#b45309"
	emailPaid     = "#15803d"
	emailPaidBg   = "#f0fdf4"
	emailPaidLine = "#bbf7d0"
)

// Content IDs for the images the body references. They are the embedded parts'
// filenames — go-mail derives Content-ID from the name — so these constants are
// the single place the two must agree.
const (
	cidLogo = "jive-logo.png"
	cidPin  = "location-pin.png"
)

// buildEmailBody renders the buyer's confirmation email (Figma 741-5105): a dark
// header band, the confirmation headline and greeting, the order panel, the event
// block, the itemized ticket details with the frozen fee breakdown and total, the
// buyer's own contact details, the entry rules, and the automated-email footer.
//
// It carries NO per-ticket card, NO QR and NO ticket code (spec 016 FR-030) —
// those moved to the attachment, and duplicating them here would give a buyer two
// places to read a ticket code and one of them no way to scan it.
//
// It returns the inline image parts alongside the HTML, and that pairing is the
// point: a cid: reference whose part is missing renders as a broken image and
// raises no error anywhere — not in the send, not in the SMTP exchange, not in
// any assertion that counts attachments. Returning them together makes that
// state unrepresentable.
//
// LAYOUT CONSTRAINT (FR-031a): inline-styled nested tables only. No flexbox, no
// grid. Outlook on Windows renders with the Word engine and implements neither,
// so a flex or grid layout collapses to stacked full-width blocks for a large
// share of recipients.
func buildEmailBody(order OrderDelivery, tickets []TicketDetail, brand Branding) (string, []Attachment) {
	var sb strings.Builder

	eventName := order.Event.Name
	if eventName == "" && len(tickets) > 0 {
		// Pre-016 orders reached this function with the event named only on the
		// tickets. Falling back keeps a resend of an old order readable.
		eventName = tickets[0].EventName
	}

	inline := []Attachment{
		{Filename: cidLogo, ContentType: "image/png", Content: logoPNG(brand)},
		{Filename: cidPin, ContentType: "image/png", Content: pinPNG(brand)},
	}

	// A full document, not a fragment. The <head> block is the only working fix
	// for Outlook's DPI scaling: the Word engine converts px to points for
	// everything except HTML width/height ATTRIBUTES, so on a 120/144 DPI Windows
	// box the CID logo renders 25-50% oversized without the PixelsPerInch
	// normaliser. Gmail discards <head> harmlessly.
	sb.WriteString(`<!DOCTYPE html><html xmlns:o="urn:schemas-microsoft-com:office:office">`)
	sb.WriteString(`<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	fmt.Fprintf(&sb, `<title>%s</title>`, html.EscapeString(eventName))
	sb.WriteString(`<!--[if mso]><xml><o:OfficeDocumentSettings><o:AllowPNG/><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml><![endif]-->`)
	// Progressive enhancement ONLY. Every rule here narrows padding that the
	// inline styles already set to a workable value, so a client that strips
	// stylesheets loses polish and nothing else (FR-031). Outlook ignores this
	// block entirely, which is fine — it has no phone.
	sb.WriteString(`<style>@media only screen and (max-width:600px){` +
		`.sec{padding-left:18px !important;padding-right:18px !important}` +
		`.amt{width:auto !important}` +
		`}</style>`)
	sb.WriteString(`</head>`)
	fmt.Fprintf(&sb, `<body style="margin:0;padding:0;background:#f4f4f5;font-family:Helvetica,Arial,sans-serif;color:%s">`, emailInk)

	// Outer wrapper table: the Word engine does not honour `margin:0 auto`
	// centring, so centring is an align attribute on a full-width table.
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#f4f4f5"><tr><td align="center" style="padding:24px 8px">`)
	// Ghost table: Outlook ignores max-width on a non-table and would otherwise
	// let the card run the full window width.
	sb.WriteString(`<!--[if mso]><table role="presentation" width="600" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->`)
	// Width as an ATTRIBUTE and a style, but they say different things on
	// purpose. Outlook's Word engine reads the attribute and lays the card out at
	// a fixed 600px, which is right — it has no mobile client. Everything else
	// reads the style, where `width:100%` lets the card SHRINK on a phone and
	// `max-width` stops it growing past the design's frame.
	//
	// A literal `width:600px` here is what made the body overflow a phone
	// viewport: the card could not shrink below 600px, so a 375px screen showed
	// roughly the left two-thirds of it and cut the rest off.
	sb.WriteString(`<table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:600px;background:#ffffff;border-radius:12px;overflow:hidden">`)

	emailHeader(&sb, brand)
	emailGreeting(&sb, order, eventName)
	emailOrderPanel(&sb, order)
	emailEventBlock(&sb, order, eventName)
	emailTicketDetails(&sb, order)
	emailBuyerInformation(&sb, order)
	emailImportantInformation(&sb)
	emailFooter(&sb, brand)

	sb.WriteString(`</table>`)
	sb.WriteString(`<!--[if mso]></td></tr></table><![endif]-->`)
	sb.WriteString(`</td></tr></table></body></html>`)
	return sb.String(), inline
}

// emailHeader draws the dark band carrying the real brand mark (FR-023, FR-023a).
//
// The image is referenced by content ID, never by URL: Outlook and Gmail block
// remote images by default, and Gmail strips data: URIs from img sources.
//
// width and height are HTML ATTRIBUTES, not CSS. Outlook's Word engine ignores
// CSS dimensions on an <img> and draws the natural pixel size, so the 2x asset
// would render at double size there and correctly everywhere else. The alt text
// is the site name, so a client that blocks even inline images still shows the
// brand rather than an empty band.
func emailHeader(sb *strings.Builder, brand Branding) {
	fmt.Fprintf(sb,
		`<tr><td align="center" style="background:%s;padding:28px 28px 24px">`+
			`<img src="cid:%s" width="108" height="61" alt="%s" `+
			`style="display:block;border:0;outline:none;text-decoration:none;width:108px;height:61px">`+
			`</td></tr>`,
		emailBand, cidLogo, html.EscapeString(brand.SiteName))
}

// emailGreeting draws the confirmation headline and the greeting naming the
// buyer (FR-023).
func emailGreeting(sb *strings.Builder, order OrderDelivery, eventName string) {
	sb.WriteString(`<tr><td class="sec" style="padding:28px 28px 4px">`)
	fmt.Fprintf(sb, `<div style="font-size:21px;font-weight:bold;color:%s">Hooray, you've got your ticket!</div>`, emailInk)
	fmt.Fprintf(sb,
		`<div style="font-size:14px;line-height:1.6;color:#3f3f46;padding-top:14px">Hello <strong>%s</strong>,</div>`,
		html.EscapeString(order.BuyerName))
	fmt.Fprintf(sb,
		`<div style="font-size:14px;line-height:1.6;color:#3f3f46;padding-top:6px">`+
			`Thank you for booking %s tickets.<br>`+
			`Your booking and payment process has been received! The ticket for this event has been issued.`+
			`</div>`,
		html.EscapeString(eventName))
	sb.WriteString(`</td></tr>`)
}

// emailOrderPanel draws the status/number/date/method panel (FR-024).
func emailOrderPanel(sb *strings.Builder, order OrderDelivery) {
	sb.WriteString(`<tr><td class="sec" style="padding:20px 28px 8px">`)
	fmt.Fprintf(sb, `<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:%s;border-radius:10px">`, emailPanel)

	sb.WriteString(`<tr><td style="padding:14px 18px 6px;font-size:13px;color:` + emailMuted + `">Order Status</td>`)
	// The badge is a nested one-cell table, not an inline-flex span: the Word
	// engine implements neither inline-flex nor border-radius. A table cell
	// shrinks to its content everywhere, which is the behaviour that matters;
	// the rounding is a progressive nicety that Outlook renders square. That
	// degradation is accepted and recorded in the plan rather than worked around.
	fmt.Fprintf(sb,
		`<td align="right" style="padding:14px 18px 6px">`+
			`<table role="presentation" cellpadding="0" cellspacing="0" border="0" align="right" style="border-collapse:separate">`+
			`<tr><td style="background:%s;border:1px solid %s;border-radius:6px;padding:4px 12px;`+
			`font-size:11px;font-weight:bold;letter-spacing:.5px;color:%s;white-space:nowrap">PAID</td></tr>`+
			`</table></td></tr>`,
		emailPaidBg, emailPaidLine, emailPaid)

	panelRow := func(label, value string) {
		fmt.Fprintf(sb,
			`<tr><td style="padding:6px 18px;font-size:13px;color:%s">%s</td>`+
				`<td align="right" style="padding:6px 18px;font-size:13px;font-weight:bold;color:%s">%s</td></tr>`,
			emailMuted, html.EscapeString(label), emailInk, html.EscapeString(value))
	}
	panelRow("Order Number", order.OrderNumber)
	// "04 Jul 2026 06:56 WIB" — no comma, unlike the receipt's transaction stamp.
	panelRow("Order Date", formatOrderStamp(order.CreatedAt))
	// Upper case whatever the payment record stores (FR-010a, FR-024c).
	panelRow("Payment Method", strings.ToUpper(order.Payment.Method))

	sb.WriteString(`<tr><td colspan="2" style="padding:6px"></td></tr>`)
	sb.WriteString(`</table></td></tr>`)
}

// emailEventBlock names the event with its venue and full address (FR-025).
func emailEventBlock(sb *strings.Builder, order OrderDelivery, eventName string) {
	sb.WriteString(`<tr><td class="sec" style="padding:16px 28px 8px">`)
	fmt.Fprintf(sb, `<div style="border-top:1px solid %s;padding-top:18px"></div>`, emailRule)
	fmt.Fprintf(sb, `<div style="font-size:17px;font-weight:bold;color:%s">%s</div>`,
		emailInk, html.EscapeString(eventName))

	if order.Event.Venue != "" || order.Event.Address != "" {
		// Icon and text in separate cells. vertical-align on an inline <img> is
		// unreliable in the Word engine, and the body is table-based anyway, so
		// the cell costs nothing. The pin is a CID image rather than inline SVG
		// (Outlook renders no SVG) or an emoji (each platform draws its own).
		sb.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="padding-top:10px"><tr>`)
		fmt.Fprintf(sb,
			`<td width="16" valign="top" style="width:16px;padding-top:2px">`+
				`<img src="cid:%s" width="16" height="16" alt="" style="display:block;border:0;width:16px;height:16px">`+
				`</td><td width="8" style="width:8px">&nbsp;</td><td valign="top">`,
			cidPin)
		if order.Event.Venue != "" {
			fmt.Fprintf(sb, `<div style="font-size:14px;font-weight:bold;color:%s">%s</div>`,
				emailInk, html.EscapeString(order.Event.Venue))
		}
		if order.Event.Address != "" {
			fmt.Fprintf(sb, `<div style="font-size:13px;line-height:1.6;color:#3f3f46">%s</div>`,
				html.EscapeString(order.Event.Address))
		}
		sb.WriteString(`</td></tr></table>`)
	}
	sb.WriteString(`</td></tr>`)
}

// emailTicketDetails itemizes the lines, the frozen fees and the total (FR-026).
//
// The FULL order appears here, not a per-holder subset: the receipt each buyer
// gets is the order receipt, complete and self-consistent (spec 011 FR-012).
//
// A pre-fee order — no recorded subtotal — collapses to the total alone, with no
// subtotal row and no fabricated fee rows (FR-013).
func emailTicketDetails(sb *strings.Builder, order OrderDelivery) {
	sb.WriteString(`<tr><td class="sec" style="padding:16px 28px 8px">`)
	fmt.Fprintf(sb, `<div style="border-top:1px solid %s;padding-top:18px"></div>`, emailRule)
	fmt.Fprintf(sb, `<div style="font-size:15px;font-weight:bold;color:%s;padding-bottom:6px">Ticket Details</div>`, emailInk)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0">`)

	// The amount cell carries a FIXED width, on every row. Without it each row's
	// column edge follows its own content and the amounts visibly stagger — the
	// defect the design review reported. amountWidth is wide enough for the
	// longest realistic figure so nothing wraps into a second line.
	const amountWidth = 130

	if order.Subtotal != nil {
		for _, line := range order.Items {
			fmt.Fprintf(sb,
				`<tr><td style="padding:10px 0 0;font-size:14px;font-weight:bold;color:%s">%s</td>`,
				emailInk, html.EscapeString(line.Name))
			fmt.Fprintf(sb,
				`<td width="%d" class="amt" align="right" valign="top" style="width:%dpx;padding:10px 0 0;font-size:14px;font-weight:bold;color:%s;white-space:nowrap">%s</td></tr>`,
				amountWidth, amountWidth, emailInk, formatIDR(line.Subtotal))

			// The per-line breakdown and the line's own admission day(s).
			detail := fmt.Sprintf("%d x %s", line.Quantity, formatIDR(line.UnitPrice))
			if sub := receiptSubLine(line); sub != "" {
				detail = sub + " · " + detail
			}
			fmt.Fprintf(sb,
				`<tr><td colspan="2" style="padding:2px 0 0;font-size:12px;color:%s">%s</td></tr>`,
				emailMuted, html.EscapeString(detail))
		}

		// Only when there is something on the other side of it (FR-026c). A
		// fee-free order was drawing a separator between the lines and nothing.
		if len(order.Fees) > 0 {
			fmt.Fprintf(sb, `<tr><td colspan="2" style="padding:10px 0 0;border-bottom:1px dashed %s;font-size:0;line-height:0">&nbsp;</td></tr>`, emailRule)
		}

		// Fee rows share the ticket lines' rhythm and, crucially, the same fixed
		// amount column — that is what puts every figure on one right edge.
		for _, fee := range order.Fees {
			fmt.Fprintf(sb,
				`<tr><td style="padding:8px 0 0;font-size:13px;color:#3f3f46">%s</td>`,
				html.EscapeString(fee.Name))
			fmt.Fprintf(sb,
				`<td width="%d" class="amt" align="right" style="width:%dpx;padding:8px 0 0;font-size:13px;color:#3f3f46;white-space:nowrap">%s</td></tr>`,
				amountWidth, amountWidth, formatIDR(fee.Amount))
		}
	}

	fmt.Fprintf(sb, `<tr><td colspan="2" style="padding:12px 0 0;border-bottom:1px solid %s;font-size:0;line-height:0">&nbsp;</td></tr>`, emailRule)
	fmt.Fprintf(sb,
		`<tr><td style="padding:12px 0 0;font-size:15px;font-weight:bold;color:%s">Total Payment</td>`+
			`<td width="%d" class="amt" align="right" style="width:%dpx;padding:12px 0 0;font-size:18px;font-weight:bold;color:%s;white-space:nowrap">%s</td></tr>`,
		emailInk, amountWidth, amountWidth, emailBrand, formatIDR(order.TotalAmount))

	sb.WriteString(`</table></td></tr>`)
}

// emailBuyerInformation shows the buyer their own contact details as recorded
// (FR-027), masked under the same rule the receipt uses so the two surfaces
// cannot disagree about who bought the order (FR-032, FR-033).
//
// An unrecorded phone omits its row rather than rendering a blank or a
// fully-masked placeholder.
func emailBuyerInformation(sb *strings.Builder, order OrderDelivery) {
	// No leading rule. The design closes Ticket Details with whitespace, and a
	// border here is what put a line directly under the Total Payment row
	// (FR-026b).
	sb.WriteString(`<tr><td class="sec" style="padding:26px 28px 8px">`)
	fmt.Fprintf(sb, `<div style="font-size:15px;font-weight:bold;color:%s;padding-bottom:4px">Buyer Information</div>`, emailInk)
	sb.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0">`)

	row := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(sb,
			`<tr><td style="padding-top:8px;font-size:13px;color:%s">%s</td>`+
				`<td align="right" style="padding-top:8px;font-size:13px;color:%s">%s</td></tr>`,
			emailMuted, html.EscapeString(label), emailInk, html.EscapeString(value))
	}
	row("Name", order.BuyerName)
	row("Email", MaskEmail(order.BuyerEmail))
	row("Phone Number", MaskPhone(order.BuyerPhone))

	sb.WriteString(`</table></td></tr>`)
}

// emailImportantInformation draws the entry, sharing and refund rules (FR-028).
func emailImportantInformation(sb *strings.Builder) {
	sb.WriteString(`<tr><td class="sec" style="padding:20px 28px 8px">`)
	fmt.Fprintf(sb, `<div style="border-top:1px solid %s;margin-bottom:18px"></div>`, emailRule)
	fmt.Fprintf(sb,
		`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:%s;border-left:4px solid #f59e0b;border-radius:6px">`,
		emailNotice)
	fmt.Fprintf(sb,
		`<tr><td style="padding:14px 18px"><div style="font-size:13px;font-weight:bold;color:%s">Important Information</div>`,
		emailAmber)
	fmt.Fprintf(sb, `<ul style="margin:8px 0 0;padding-left:18px;font-size:12px;line-height:1.7;color:%s">`, emailAmber)
	for _, rule := range []string{
		"Please prepare your E-Ticket barcode/QR code and a valid ID card before entering the venue.",
		"Do not share your E-Ticket with anyone to prevent duplication or misuse.",
		"Tickets are non-refundable unless the event is officially canceled by the organizer.",
	} {
		fmt.Fprintf(sb, `<li>%s</li>`, html.EscapeString(rule))
	}
	sb.WriteString(`</ul></td></tr></table></td></tr>`)
}

// emailFooter closes with the automated-email notice and the platform
// attribution (FR-029).
func emailFooter(sb *strings.Builder, brand Branding) {
	// Exact wording, per FR-029. "Powered By Manjo" belongs to the RECEIPT, where
	// the design uses it; it must not appear in the body.
	fmt.Fprintf(sb,
		`<tr><td class="sec" style="padding:24px 28px 28px;text-align:center;font-size:11px;line-height:1.7;color:#a1a1aa">`+
			`This email was generated automatically. Please do not reply to this email.<br>%s`+
			`</td></tr>`,
		html.EscapeString(brand.Copyright))
}

// formatIDR renders a money amount for the documents: "IDR 550.000",
// "IDR 15.000,92" (spec 016 FR-037, FR-037a, FR-037b).
//
// The IDR prefix with INDONESIAN separators — dot for thousands, comma for the
// decimal. The documents differ from the site (which writes "Rp") in the prefix
// only; the separators are identical on purpose, because comma-as-thousands
// reads as a decimal to these buyers and a receipt is where a misread total does
// the most damage. Do not "correct" the separators to match the email mock.
//
// The fraction is printed only when it is non-zero, and then always with two
// digits — "IDR 15.000,90", never "IDR 15.000,9", which reads as malformed.
//
// It never truncates. The previous implementation used IntPart(), which
// discarded the fraction, so on an order whose percentage fee computed to cents
// the printed rows did not sum to the printed total. Money columns are
// NUMERIC(12,2), so at most two fractional digits exist and none are invented.
func formatIDR(amount decimal.Decimal) string {
	// Two decimal places is the column's own precision, so this rounds nothing
	// that was not already rounded on the way in.
	fixed := amount.Round(2)
	whole := fixed.Truncate(0)

	digits := whole.String()
	var grouped []byte
	for i := range len(digits) {
		// The `digits[i-1] != '-'` guard is load-bearing: without it a negative
		// amount renders as "IDR -,550.000".
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			grouped = append(grouped, '.')
		}
		grouped = append(grouped, digits[i])
	}

	out := "IDR " + string(grouped)

	// Abs before extracting the fraction: the sign already rides on the whole
	// part, and Sub on a negative would otherwise yield "-0.92".
	fraction := fixed.Abs().Sub(fixed.Abs().Truncate(0))
	if fraction.IsZero() {
		return out
	}
	// Shift(2) turns 0.92 into 92 and 0.05 into 5; pad so both read as cents.
	return out + fmt.Sprintf(",%02d", fraction.Shift(2).IntPart())
}
