package notification_test

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

type fakeOrders struct {
	order       notification.OrderDelivery
	getErr      error
	markCalled  int
	markErr     error
	byNumberErr error
}

func (f *fakeOrders) OrderForDelivery(context.Context, uuid.UUID) (notification.OrderDelivery, error) {
	if f.getErr != nil {
		return notification.OrderDelivery{}, f.getErr
	}
	return f.order, nil
}

func (f *fakeOrders) MarkEmailSent(context.Context, uuid.UUID) error {
	f.markCalled++
	return f.markErr
}

func (f *fakeOrders) OrderIDByNumber(context.Context, string) (uuid.UUID, error) {
	if f.byNumberErr != nil {
		return uuid.Nil, f.byNumberErr
	}
	return f.order.ID, nil
}

type fakeTickets struct {
	tickets []notification.TicketDetail
	err     error
}

func (f *fakeTickets) TicketDetailsForOrder(context.Context, uuid.UUID) ([]notification.TicketDetail, error) {
	return f.tickets, f.err
}

// fakeMailer records every accepted message. err fails every send; errFor fails
// only the given recipients (keyed by lowercased address), which is how the
// partial-failure tests break one holder's mailbox and not the other's.
type fakeMailer struct {
	sent   []notification.Message
	err    error
	errFor map[string]error
}

func (f *fakeMailer) Send(msg notification.Message) error {
	if f.err != nil {
		return f.err
	}
	if err, ok := f.errFor[strings.ToLower(msg.To)]; ok {
		return err
	}
	f.sent = append(f.sent, msg)
	return nil
}

// toAddresses lists the delivered recipients in send order.
func (f *fakeMailer) toAddresses() []string {
	out := make([]string, 0, len(f.sent))
	for _, msg := range f.sent {
		out = append(out, msg.To)
	}
	return out
}

// holderTicket builds one ticket row the way checkout stores it since spec 011:
// the holder's own name and delivery address travel on the row itself.
func holderTicket(code, name, email string) notification.TicketDetail {
	return notification.TicketDetail{
		TicketCode:     code,
		AttendeeName:   name,
		AttendeeEmail:  email,
		TicketTypeName: "Regular",
		EventName:      "Jazz Night 2026",
		Venue:          "Balai Sarbini",
		EventStart:     time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC),
		EventEnd:       time.Date(2026, 9, 1, 23, 0, 0, 0, time.UTC),
	}
}

type deliveryFixture struct {
	svc     *notification.Service
	orders  *fakeOrders
	tickets *fakeTickets
	mailer  *fakeMailer
	orderID uuid.UUID
}

// newDeliveryFixture is a paid order whose two tickets belong to two DIFFERENT
// holders, each with their own address — and NEITHER is the buyer's. That is
// load-bearing: it is what proves delivery goes to the buyer alone (spec 011
// FR-012, constitution v3.0.0) rather than to the addresses on the tickets. A
// fixture whose holders shared the buyer's address would pass either way.
func newDeliveryFixture(t *testing.T) deliveryFixture {
	t.Helper()
	orderID := uuid.New()

	subtotal := decimal.NewFromInt(500000)
	orders := &fakeOrders{order: notification.OrderDelivery{
		ID:          orderID,
		OrderNumber: "ORD-20260731-ABCDEF",
		BuyerName:   "Budi Santoso",
		BuyerEmail:  "budi@example.com",
		// Realistic, not zero: a fake that returns empty strings hides exactly
		// the omit-the-row and masking bugs these tests exist to catch.
		BuyerPhone:  "081234567890",
		Status:      "PAID",
		CreatedAt:   time.Date(2026, 7, 31, 10, 15, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 7, 31, 10, 20, 0, 0, time.UTC),
		TotalAmount: decimal.NewFromInt(550000),
		Subtotal:    &subtotal,
		Items: []notification.ReceiptLine{{
			Name:            "Regular",
			Quantity:        2,
			UnitPrice:       decimal.NewFromInt(250000),
			Subtotal:        decimal.NewFromInt(500000),
			AdmissionStarts: []time.Time{time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC)},
			Descriptor:      "Expo Entrance Ticket",
		}},
		Fees: []notification.ReceiptFee{{
			Name: "PPN (11%)", Amount: decimal.NewFromInt(50000),
		}},
		Event: notification.DeliveryEvent{
			Name:    "Jazz Night 2026",
			Venue:   "Balai Sarbini",
			Address: "Jl. Jend. Sudirman Kav. 50, Jakarta Selatan 12190",
		},
		Payment: notification.PaymentSummary{
			Method: "QRIS",
			Status: "Paid",
			PaidAt: time.Date(2026, 7, 31, 10, 18, 0, 0, time.UTC),
		},
	}}
	tickets := &fakeTickets{tickets: []notification.TicketDetail{
		holderTicket("ABC234DEFG", "Ani Lestari", "ani@example.com"),
		holderTicket("HJK567LMNP", "Bayu Wijaya", "bayu@example.com"),
	}}
	mailer := &fakeMailer{}

	return deliveryFixture{
		svc:     notification.NewService(orders, tickets, mailer, testBrand(), testsupport.DiscardLogger()),
		orders:  orders,
		tickets: tickets,
		mailer:  mailer,
		orderID: orderID,
	}
}

// Spec 011 FR-012 / constitution v3.0.0: ONE email, to the buyer, carrying every
// ticket in the order. Three tickets across two holders — Ani holds a two-ticket
// bundle unit, Bayu one ticket, neither of them the buyer — still produce a
// single message to the buyer's address with all three pages.
func TestSendTicketEmailSendsOneEmailToTheBuyerWithEveryTicket(t *testing.T) {
	f := newDeliveryFixture(t)
	f.tickets.tickets = []notification.TicketDetail{
		holderTicket("ABC234DEFG", "Ani Lestari", "ani@example.com"),
		holderTicket("QRS789TUVW", "Ani Lestari", "ani@example.com"),
		holderTicket("HJK567LMNP", "Bayu Wijaya", "bayu@example.com"),
	}

	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.NoError(t, err)
	assert.Equal(t, "budi@example.com", recipient, "the buyer's snapshot address")
	require.Len(t, f.mailer.sent, 1, "exactly one email for the whole order")

	msg := f.mailer.sent[0]
	assert.Equal(t, "budi@example.com", msg.To)
	assert.NotContains(t, f.mailer.toAddresses(), "ani@example.com",
		"holder addresses are identity, never delivery targets")
	assert.NotContains(t, f.mailer.toAddresses(), "bayu@example.com")
	// Spec 016 FR-005a / SC-022: the whole subject, not just the event name.
	// The previous Contains("Jazz Night 2026") passed unchanged under the new
	// subject — a test that survives a deliberate behaviour change was not
	// asserting that behaviour.
	assert.Equal(t, "[ORD-20260731-ABCDEF] E-receipt & E-Ticket for Jazz Night 2026", msg.Subject)

	// Spec 016 FR-001: two attachments now — the receipt and the merged tickets.
	require.Len(t, msg.Attachments, 2, "a receipt and one PDF holding every ticket")
	assert.Equal(t, "application/pdf", msg.Attachments[1].ContentType)
	assert.Contains(t, msg.Attachments[1].Filename, "ORD-20260731-ABCDEF")
	assert.True(t, strings.HasPrefix(string(msg.Attachments[1].Content), "%PDF-"),
		"the attachment must be a real, non-empty PDF")

	// Ticket codes live in the ATTACHMENT now, never in the body (FR-030).
	for _, ticket := range f.tickets.tickets {
		assert.NotContains(t, msg.HTMLBody, ticket.TicketCode)
	}

	onePage, err := notification.RenderTicketsPDF(f.orders.order, f.tickets.tickets[:1], testBrand())
	require.NoError(t, err)
	assert.Greater(t, len(msg.Attachments[1].Content), len(onePage),
		"the PDF must carry all three pages, not just the first ticket's")

	assert.Equal(t, 1, f.orders.markCalled, "email_sent recorded once, after the send succeeded")
}

// email_sent must only be set after the buyer's delivery succeeded.
func TestSendTicketEmailSetsEmailSentAfterDelivery(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.NoError(t, err)
	assert.Equal(t, 1, f.orders.markCalled)
}

func TestSendTicketEmailDoesNotSetEmailSentWhenDeliveryFails(t *testing.T) {
	f := newDeliveryFixture(t)
	f.mailer.err = errors.New("smtp unavailable")

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Zero(t, f.orders.markCalled, "a failed send must not be recorded as sent")
}

// A refused mailbox leaves email_sent FALSE so resend stays armed, and reports
// no recipient — nothing was delivered.
func TestSendTicketEmailReportsNoRecipientWhenTheSendFails(t *testing.T) {
	f := newDeliveryFixture(t)
	f.mailer.errFor = map[string]error{"budi@example.com": errors.New("mailbox refused the message")}

	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Empty(t, recipient)
	assert.Empty(t, f.mailer.toAddresses())
	assert.Zero(t, f.orders.markCalled, "a failed delivery must not be recorded as sent")
}

// Delivery reads the buyer snapshot, never the ticket rows, so blank holder
// emails change nothing — they are identity fields the send does not consult.
func TestSendTicketEmailIgnoresBlankHolderEmails(t *testing.T) {
	f := newDeliveryFixture(t)
	f.tickets.tickets = []notification.TicketDetail{
		holderTicket("ABC234DEFG", "Ani Lestari", ""),
		holderTicket("HJK567LMNP", "Bayu Wijaya", ""),
	}

	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.NoError(t, err)
	assert.Equal(t, "budi@example.com", recipient)
	require.Len(t, f.mailer.sent, 1)
	assert.Equal(t, "budi@example.com", f.mailer.sent[0].To)
	assert.Equal(t, 1, f.orders.markCalled)
}

// Spec 011 edge case: a holder form repeating the buyer's address is not a
// second delivery — the recipient comes from the order, not from the rows.
func TestSendTicketEmailSendsOnceWhenAHolderRepeatsTheBuyerAddress(t *testing.T) {
	f := newDeliveryFixture(t)
	f.tickets.tickets = []notification.TicketDetail{
		holderTicket("ABC234DEFG", "Budi Santoso", "budi@example.com"),
		holderTicket("HJK567LMNP", "Bayu Wijaya", "bayu@example.com"),
	}

	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.NoError(t, err)
	assert.Equal(t, "budi@example.com", recipient)
	require.Len(t, f.mailer.sent, 1, "still exactly one email")
	// The buyer is named in the greeting. The OTHER holder is not: since spec
	// 016 the body carries no per-ticket card, so holder identity lives on the
	// e-ticket pages (FR-030). Both holders' tickets are still in the attachment.
	assert.Contains(t, f.mailer.sent[0].HTMLBody, "Budi Santoso")
	require.Len(t, f.mailer.sent[0].Attachments, 2)
	assert.Contains(t, f.mailer.sent[0].Attachments[1].Filename, "tickets-")
}

// An order with no buyer snapshot never went through checkout. Refuse rather
// than mail nobody and then mark the order delivered.
func TestSendTicketEmailFailsWithoutABuyerAddress(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.order.BuyerEmail = "   "

	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Empty(t, recipient)
	assert.Empty(t, f.mailer.sent)
	assert.Zero(t, f.orders.markCalled)
}

// Resend is the same path, so the guard lives in one place for both.
func TestSendTicketEmailRejectsAnOrderThatIsNotPaid(t *testing.T) {
	for _, status := range []string{"PENDING", "CANCELLED", "EXPIRED"} {
		t.Run(status, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.orders.order.Status = status

			_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

			var appErr *apperr.Error
			require.True(t, errors.As(err, &appErr))
			assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
			assert.Equal(t, apperr.CodeOrderNotPaid, appErr.Code)
			assert.Empty(t, f.mailer.sent)
		})
	}
}

func TestSendTicketEmailFailsWhenTheOrderHasNoTickets(t *testing.T) {
	f := newDeliveryFixture(t)
	f.tickets.tickets = nil

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Empty(t, f.mailer.sent)
}

func TestSendTicketEmailPropagatesAMissingOrder(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.getErr = apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

// The resend path (specs/003 US3) reuses stored codes verbatim and re-renders each
// QR from them — a guest's original ticket must stay valid.
func TestResendReusesTheExistingTicketCodes(t *testing.T) {
	f := newDeliveryFixture(t)
	ctx := context.Background()

	_, err := f.svc.SendTicketEmail(ctx, f.orderID)
	require.NoError(t, err)
	_, err = f.svc.SendTicketEmail(ctx, f.orderID)
	require.NoError(t, err)

	require.Len(t, f.mailer.sent, 2, "one email per round")
	first, second := f.mailer.sent[0], f.mailer.sent[1]
	assert.Equal(t, first.To, second.To, "the resend reaches the same buyer")
	assert.Equal(t, len(first.Attachments[0].Content), len(second.Attachments[0].Content),
		"the resend renders the same tickets, never regenerated codes")
	assert.Equal(t, 2, f.orders.markCalled, "a successful resend also records email_sent (FR-011)")
}

func TestSendTicketEmailStillSucceedsIfRecordingTheFlagFails(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.markErr = errors.New("db hiccup")

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	// The email genuinely went out; failing the whole operation would invite a
	// retry that double-sends.
	require.NoError(t, err)
	assert.Len(t, f.mailer.sent, 1)
}

// Spec 016 FR-023..FR-029: the body is the Figma 741-5105 confirmation, in
// order. Each section is asserted by its own heading rather than by a blob of
// HTML, so a failure names which section regressed.
func TestSendTicketEmailBodyHasEverySectionOfTheDesign(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1)

	body := f.mailer.sent[0].HTMLBody

	sections := []string{
		"cid:jive-logo-white.png",         // FR-023a real logo, referenced by content ID
		"Hooray, you've got your ticket!", // FR-023 headline
		"Budi Santoso",                    // FR-023 greeting
		"Order Status",                    // FR-024
		"PAID",
		"Order Number",
		"ORD-20260731-ABCDEF",
		"Order Date",
		"Payment Method",
		"QRIS",
		"Jazz Night 2026", // FR-025 event block
		"Balai Sarbini",
		"Jakarta Selatan 12190",
		"Ticket Details", // FR-026
		"Total Payment",
		"IDR 550.000",
		"Buyer Information",     // FR-027
		"Important Information", // FR-028
		"valid ID card",
		"non-refundable",
		"do not reply", // FR-029
		"© 2026 manjo",
		"cid:location-pin.png", // FR-025 the venue's pin icon
	}
	for _, want := range sections {
		assert.Contains(t, body, want)
	}

	// Order matters: the design reads top to bottom and a section rendered out of
	// sequence is a regression a Contains-only assertion would miss.
	assertInOrder(t, body,
		"Hooray", "Order Status", "Jazz Night 2026", "Ticket Details",
		"Buyer Information", "Important Information", "© 2026 manjo")

	// FR-029: the receipt's attribution must not appear in the email body.
	assert.NotContains(t, body, "Powered By Manjo")
	// FR-031a: nested tables only. Outlook on Windows implements neither.
	assert.NotContains(t, body, "display:flex")
	assert.NotContains(t, body, "display:grid")
}

// FR-030 / SC-006. The per-ticket cards moved to the attachment. Leaving a copy
// in the body would give a buyer two places to read a ticket code, one of them
// unscannable.
func TestSendTicketEmailBodyCarriesNoTicketCodeOrQR(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[0].HTMLBody
	for _, ticket := range f.tickets.tickets {
		assert.NotContains(t, body, ticket.TicketCode)
		assert.NotContains(t, body, ticket.AttendeeName,
			"holder names belong on the e-ticket pages, not in the body")
	}
	assert.NotContains(t, body, "Show at entry")
	// The body DOES carry images now (FR-023a, FR-025) — but only inline ones.
	// A data: URI is stripped by Gmail and a remote URL is blocked by default in
	// Outlook and Gmail, so either would render as nothing for most recipients.
	assert.NotContains(t, body, "data:image")
	assert.NotContains(t, body, `src="http`)
	assert.NotContains(t, body, `src='http`)
}

// Spec 011 FR-016 carried forward: the itemization lives in this email — each
// purchased line, the frozen fees, and the Total Payment.
func TestSendTicketEmailBodyItemizesTheFullOrderSummary(t *testing.T) {
	f := newDeliveryFixture(t)
	subtotal := decimal.NewFromInt(500000)
	f.orders.order.Subtotal = &subtotal
	f.orders.order.Items = []notification.ReceiptLine{
		{Name: "Early Bird", Quantity: 2, UnitPrice: decimal.NewFromInt(150000), Subtotal: decimal.NewFromInt(300000)},
		{Name: "VIP Duo Bundle", Quantity: 1, UnitPrice: decimal.NewFromInt(200000), Subtotal: decimal.NewFromInt(200000)},
	}
	f.orders.order.Fees = []notification.ReceiptFee{
		{Name: "Platform fee", Amount: decimal.NewFromInt(50000)},
	}

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1)

	body := f.mailer.sent[0].HTMLBody
	assert.Contains(t, body, "Early Bird")
	assert.Contains(t, body, "2 x IDR 150.000")
	assert.Contains(t, body, "IDR 300.000")
	assert.Contains(t, body, "VIP Duo Bundle")
	assert.Contains(t, body, "Platform fee")
	assert.Contains(t, body, "IDR 50.000")
	assert.Contains(t, body, "Total Payment")
	assert.Contains(t, body, "IDR 550.000")
}

// FR-013. A pre-fee order records no subtotal. It shows the total alone — no
// subtotal row, and no fee rows invented to fill the gap.
func TestSendTicketEmailBodyCollapsesAPreFeeOrderToTheTotal(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.order.Subtotal = nil
	f.orders.order.Fees = nil

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[0].HTMLBody
	assert.Contains(t, body, "Total Payment")
	assert.Contains(t, body, "IDR 550.000")
	assert.NotContains(t, body, "PPN")
	assert.NotContains(t, body, "IDR 500.000", "no subtotal row on a pre-fee order")
}

// FR-027, FR-032, FR-033 / SC-008. Both surfaces render the SAME buyer contact
// values, masked identically — two shapes for one value read as two buyers.
func TestSendTicketEmailMasksBuyerContactIdenticallyInBodyAndReceipt(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	msg := f.mailer.sent[0]
	maskedEmail := notification.MaskEmail("budi@example.com")
	maskedPhone := notification.MaskPhone("081234567890")

	assert.Contains(t, msg.HTMLBody, maskedEmail)
	assert.Contains(t, msg.HTMLBody, maskedPhone)
	assert.NotContains(t, msg.HTMLBody, "budi@example.com",
		"the body shows the masked address, not the raw one")

	receipt, err := notification.RenderReceiptPDFPlain(f.orders.order, testBrand())
	require.NoError(t, err)
	assert.Contains(t, string(receipt), maskedEmail)
	assert.Contains(t, string(receipt), maskedPhone)
}

// An unrecorded phone omits its row rather than rendering a blank or a
// fully-masked placeholder (FR-027).
func TestSendTicketEmailBodyOmitsThePhoneRowWhenNoneWasRecorded(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.order.BuyerPhone = ""

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[0].HTMLBody
	assert.Contains(t, body, "Buyer Information")
	assert.NotContains(t, body, "Phone Number")
}

// assertInOrder checks that each needle appears after the previous one.
func assertInOrder(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	at := 0
	for _, needle := range needles {
		i := strings.Index(haystack[at:], needle)
		if !assert.GreaterOrEqual(t, i, 0, "%q must appear after %d", needle, at) {
			return
		}
		at += i + len(needle)
	}
}

// A pre-fee order (nil Subtotal) collapses the breakdown to the Total Payment
// row alone.
func TestSendTicketEmailBodyCollapsesTheSummaryWithoutASubtotal(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1)

	assert.Contains(t, f.mailer.sent[0].HTMLBody, "Total Payment")
	assert.NotContains(t, f.mailer.sent[0].HTMLBody, "Ticket Total")
}

// testBrand is the platform branding every delivery test renders with. Values
// are distinctive so an assertion cannot pass on a coincidence.
func testBrand() notification.Branding {
	return notification.Branding{
		SiteName:     "JIVE",
		SiteURL:      "https://www.jive.co.id",
		SupportEmail: "help@manjo.com",
		LegalEntity:  "PT Manjo Teknologi Indonesia",
		Attribution:  "Powered By Manjo",
		Copyright:    "© 2026 manjo",
	}
}

// --- Spec 016: two attachments, and what happens when one cannot be built ---

// FR-001 / SC-001. Exactly two attachments, receipt first, both real PDFs, both
// named for the order and distinguishable from each other (FR-004).
func TestSendTicketEmailCarriesTheReceiptAndTheTicketsAsTwoAttachments(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1)

	attachments := f.mailer.sent[0].Attachments
	require.Len(t, attachments, 2, "exactly two: never one, never three")

	assert.Equal(t, "receipt-ORD-20260731-ABCDEF.pdf", attachments[0].Filename)
	assert.Equal(t, "tickets-ORD-20260731-ABCDEF.pdf", attachments[1].Filename)
	assert.NotEqual(t, attachments[0].Filename, attachments[1].Filename,
		"two files saved to one folder must not collide")

	for _, a := range attachments {
		assert.Equal(t, "application/pdf", a.ContentType)
		assert.True(t, strings.HasPrefix(string(a.Content), "%PDF-"),
			"%s must be a real, non-empty PDF", a.Filename)
		assert.Contains(t, a.Filename, "ORD-20260731-ABCDEF")
	}

	assert.NotEqual(t, attachments[0].Content, attachments[1].Content,
		"the receipt and the tickets are different documents")
}

// FR-006. If the RECEIPT cannot be built, nothing is sent and the order is not
// marked delivered — a half-delivered email carrying one of the two documents
// would leave email_sent TRUE and the resend disarmed.
func TestSendTicketEmailSendsNothingWhenTheReceiptCannotBeBuilt(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.order.OrderNumber = "" // a receipt with nothing to identify it

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Empty(t, f.mailer.sent, "no partial email may go out")
	assert.Zero(t, f.orders.markCalled, "email_sent stays FALSE so resend stays armed")
}

// FR-006, the other document. An unrenderable ticket aborts the same way.
func TestSendTicketEmailSendsNothingWhenTheTicketsCannotBeBuilt(t *testing.T) {
	f := newDeliveryFixture(t)
	f.tickets.tickets[1].TicketCode = "" // no code, so no QR, so no page

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Empty(t, f.mailer.sent)
	assert.Zero(t, f.orders.markCalled)
}

// FR-007 / SC-005. A resend reuses the already-issued codes verbatim and
// produces the same pair. Regenerating a code would invalidate the pass the
// guest already holds.
func TestResendProducesTheSamePairWithTheSameTicketCodes(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	_, err = f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	require.Len(t, f.mailer.sent, 2)
	first, second := f.mailer.sent[0], f.mailer.sent[1]

	require.Len(t, second.Attachments, 2, "a resend carries the same two attachments")
	assert.Equal(t, first.Attachments[0].Filename, second.Attachments[0].Filename)
	assert.Equal(t, first.Attachments[1].Filename, second.Attachments[1].Filename)
	assert.Equal(t, first.To, second.To)

	assert.Equal(t, len(first.Attachments[1].Content), len(second.Attachments[1].Content),
		"the same tickets render the same document")

	// And the codes really are the originals. The shipped attachment is
	// compressed, so the codes are read from an uncompressed render of the same
	// input rather than from the message bytes.
	plain, err := notification.RenderTicketsPDFPlain(f.orders.order, f.tickets.tickets, testBrand())
	require.NoError(t, err)
	for _, ticket := range f.tickets.tickets {
		require.NotEmpty(t, ticket.TicketCode)
		assert.Contains(t, string(plain), ticket.TicketCode,
			"a resend reuses the issued code; regenerating one would invalidate the pass the guest holds")
	}
}

// FR-038 / SC-009. The receipt prints the figures FROZEN onto the order. A fee
// master-data edit after the order must not change an already-placed order's
// documents — the frozen order_fees rows are what render.
func TestResendPrintsTheFrozenFiguresNotCurrentFeeMasterData(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	original := f.mailer.sent[0].Attachments[0].Content

	// The order's own frozen lines are untouched; only "master data" changed,
	// which this domain never reads. Rendering again must be identical.
	_, err = f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	resent := f.mailer.sent[1].Attachments[0].Content

	assert.Equal(t, len(original), len(resent))

	receipt, err := notification.RenderReceiptPDFPlain(f.orders.order, testBrand())
	require.NoError(t, err)
	assert.Contains(t, string(receipt), "IDR 550.000", "the charged total, unchanged")
	assert.Contains(t, string(receipt), "PPN \\(11%\\)", "the frozen fee name, unchanged")
}

// FR-023a, FR-025 / T104. Every cid: the body references must have a matching
// inline part, and every inline part must be referenced.
//
// This is the assertion that makes a broken-image regression impossible to ship
// silently: a cid: whose part is missing renders as a broken image and raises no
// error anywhere — not in the send, not in the SMTP exchange, and not in any
// assertion that only counts attachments.
func TestEveryCIDReferenceHasAMatchingInlinePart(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	msg := f.mailer.sent[0]

	referenced := map[string]bool{}
	for _, m := range regexp.MustCompile(`src="cid:([^"]+)"`).FindAllStringSubmatch(msg.HTMLBody, -1) {
		referenced[m[1]] = true
	}
	require.NotEmpty(t, referenced, "the body is expected to reference at least the logo")

	carried := map[string]bool{}
	for _, part := range msg.Inline {
		carried[part.Filename] = true
		assert.NotEmpty(t, part.Content, "%s is referenced but carries no bytes", part.Filename)
		assert.Equal(t, "image/png", part.ContentType)
		assert.True(t, strings.HasSuffix(part.Filename, ".png"),
			"%s needs an image extension or the part becomes application/octet-stream", part.Filename)
	}

	for name := range referenced {
		assert.True(t, carried[name], "body references cid:%s with no matching inline part", name)
	}
	for name := range carried {
		assert.True(t, referenced[name], "inline part %s is carried but never referenced", name)
	}

	// And the documents are still exactly two — inline images are not documents.
	assert.Len(t, msg.Attachments, 2)
}

// FR-007 / T105. A resend carries the same inline parts as the original, not
// just the same two attachments.
func TestResendCarriesTheSameInlineParts(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	_, err = f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 2)

	first, second := f.mailer.sent[0], f.mailer.sent[1]
	require.Len(t, second.Inline, len(first.Inline))
	for i := range first.Inline {
		assert.Equal(t, first.Inline[i].Filename, second.Inline[i].Filename)
		// Byte-identical: go-mail's default CopyFunc drains its reader once, so a
		// second send that reused a drained reader would carry an empty image.
		assert.Equal(t, first.Inline[i].Content, second.Inline[i].Content,
			"%s must survive a second send with its bytes intact", first.Inline[i].Filename)
	}
}

// FR-026c / SC-015. A fee-free order drew a dashed separator between the ticket
// lines and nothing at all.
func TestSendTicketEmailOmitsTheDashedRuleWhenThereAreNoFees(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.order.Fees = nil

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[0].HTMLBody
	assert.NotContains(t, body, "dashed",
		"a separator with nothing on one side of it reads as a stray line")
	assert.Contains(t, body, "Total Payment", "the totals block itself still renders")
}

// ...and exactly one when there are fees, so the fix does not swing the other way.
func TestSendTicketEmailDrawsOneDashedRuleWhenThereAreFees(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[0].HTMLBody
	assert.Equal(t, 1, strings.Count(body, "dashed"))
}

// FR-031b / SC-018. The card must be able to shrink below the design's 600px
// frame, or a phone shows the left portion of it and clips the rest.
func TestSendTicketEmailBodyIsFluidBelowTheDesignWidth(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	body := f.mailer.sent[0].HTMLBody

	// The card sizes to its container and is merely CAPPED at the design frame.
	assert.Contains(t, body, "width:100%;max-width:600px",
		"the card must be fluid with a maximum, not pinned")
	// Every occurrence of "width:600px" must be part of "max-width:600px". A bare
	// one is the pinned form that cannot shrink, and is what clipped the body on
	// a phone. Substring-counting rather than NotContains, because the fluid form
	// legitimately contains the pinned form's text.
	assert.Equal(t,
		strings.Count(body, "max-width:600px"), strings.Count(body, "width:600px"),
		"a bare width:600px cannot shrink; only max-width:600px may appear")

	// Outlook still gets a fixed layout: it reads the width ATTRIBUTE and the
	// ghost table, and has no phone client to be responsive for.
	assert.Contains(t, body, `width="600"`)
	assert.Contains(t, body, "<!--[if mso]>")

	// device-width is what makes a phone honour the layout instead of rendering
	// a desktop-width page and zooming out.
	assert.Contains(t, body, "width=device-width")
}

// The mobile rules are progressive enhancement ONLY: a client that strips
// stylesheets must still get workable spacing from the inline styles (FR-031).
func TestSendTicketEmailBodyMobileRulesAreEnhancementOnly(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	body := f.mailer.sent[0].HTMLBody

	assert.Contains(t, body, "@media only screen and (max-width:600px)")
	// Every class the media query targets must also carry an inline style, so
	// stripping the <style> block degrades polish and nothing else.
	assert.Contains(t, body, `class="sec" style="padding:`)
	assert.Contains(t, body, `class="amt" align="right"`)
	assert.Contains(t, body, "width:130px")
}

// --- Spec 016 Revision 3: the brand refresh -------------------------------

// FR-023b. The header mark stays at its drawn size; the band does not grow to
// suit the asset. Enlarging it to 280px was tried on 2026-08-19 and reversed.
//
// The HTML ATTRIBUTES are the load-bearing half. Outlook on Windows renders
// through the Word engine, which ignores CSS dimensions on an <img> and draws the
// part's natural pixel size — so a change made only in the inline style would
// render the mark at its full asset width for every Outlook recipient while
// looking perfect everywhere the developer checked. The two must agree, and this
// asserts both.
func TestEmailHeaderKeepsTheMarkAtItsFixedSize(t *testing.T) {
	body, _ := notification.BuildEmailBody(sampleOrder(), sampleTickets(1), sampleBrand())

	assert.Contains(t, body, `width="108" height="61"`,
		"the attributes are what Outlook's Word engine obeys")
	assert.Contains(t, body, "width:108px",
		"the inline style must agree with the attributes")
	assert.NotContains(t, body, `width="280"`,
		"the reversed 280px enlargement must not return")
}

// SC-018 / FR-031b. The mark must always be allowed to scale down rather than
// force the body wider. At 108px nothing overflows a 320px viewport today, so
// this guards the next size or asset change rather than a present defect — which
// is the cheapest moment to pin it.
func TestEmailHeaderMarkScalesDownOnNarrowViewports(t *testing.T) {
	body, _ := notification.BuildEmailBody(sampleOrder(), sampleTickets(1), sampleBrand())

	assert.Contains(t, body, "max-width:100%")
	assert.Contains(t, body, "height:auto")
}

// --- spec 022: the registration delivery shape -------------------------------

// asRegistration turns the fixture's order into a free registration: zero total,
// no fees, marked registration-originated. It stays PAID, which is the point —
// constitution v5.0.0 admits a second origin for that status, and delivery must
// tell them apart by the MARKER, not by the status.
func asRegistration(f deliveryFixture) {
	f.orders.order.IsRegistration = true
	f.orders.order.TotalAmount = decimal.Zero
	f.orders.order.Subtotal = nil
	f.orders.order.Fees = nil
	f.orders.order.Items = []notification.ReceiptLine{{
		Name:            "Invitation Access",
		Quantity:        1,
		UnitPrice:       decimal.Zero,
		Subtotal:        decimal.Zero,
		AdmissionStarts: []time.Time{time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC)},
	}}
}

// FR-035/FR-036 and the constitution's amended ticket-generation rule: exactly
// ONE attachment, the e-ticket. A receipt for a zero-amount registration would be
// a proof of payment for a payment that never happened.
func TestRegistrationDeliverySendsTheETicketAlone(t *testing.T) {
	f := newDeliveryFixture(t)
	asRegistration(f)
	f.tickets.tickets = []notification.TicketDetail{
		holderTicket("REG234DEFG", "Halo Registrant", "halo@example.com"),
	}

	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	assert.Equal(t, "budi@example.com", recipient)

	require.Len(t, f.mailer.sent, 1)
	msg := f.mailer.sent[0]

	require.Len(t, msg.Attachments, 1, "a registration carries the e-ticket and nothing else")
	assert.Contains(t, msg.Attachments[0].Filename, "tickets-",
		"and the one document must be the e-ticket, not a receipt")
	assert.True(t, strings.HasPrefix(string(msg.Attachments[0].Content), "%PDF-"))
}

// The subject must not promise a receipt the message does not carry.
func TestRegistrationDeliverySubjectDoesNotPromiseAReceipt(t *testing.T) {
	f := newDeliveryFixture(t)
	asRegistration(f)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	subject := f.mailer.sent[0].Subject
	assert.Equal(t, "[ORD-20260731-ABCDEF] E-Ticket for Jazz Night 2026", subject)
	assert.NotContains(t, subject, "E-receipt")
}

// FR-036: no monetary amount ANYWHERE. The attachments are not the only place
// money appears — the body's Ticket Details block would otherwise render
// "Total Payment  IDR 0", which is exactly the claim the missing receipt exists
// to avoid making.
func TestRegistrationDeliveryBodyStatesNoMonetaryAmount(t *testing.T) {
	f := newDeliveryFixture(t)
	asRegistration(f)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[0].HTMLBody
	assert.NotContains(t, body, "Total Payment")
	assert.NotContains(t, body, "Ticket Details")
	assert.NotContains(t, body, "IDR", "no figure, not even a zero one")
	assert.NotContains(t, body, "Rp")
}

// The paid path must be untouched by the branch. If this regresses, a buyer who
// paid stops receiving their proof of payment — a worse failure than the one the
// branch was added to fix.
func TestPaidDeliveryStillCarriesBothDocuments(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	msg := f.mailer.sent[0]
	require.Len(t, msg.Attachments, 2)
	assert.Contains(t, msg.Attachments[0].Filename, "receipt-", "receipt first, still")
	assert.Contains(t, msg.Attachments[1].Filename, "tickets-")
	assert.Contains(t, msg.HTMLBody, "Total Payment")
}

// Spec 022 T065 / FR-038a. Admin resend is the SOLE recovery route for an
// undelivered registration: the confirmation page tells the guest their e-ticket
// was sent before delivery has actually completed (FR-039, decided deliberately),
// and shows no address, so nothing on the guest's screen will ever correct a
// failure. If this path assumes a payment, a receipt or a non-zero total, an
// undelivered registration is unrecoverable and the guest is never told.
//
// Exercised through SendTicketEmail because that is literally what the admin
// endpoint calls — the same function the first delivery used, which is the
// property being pinned. A separate resend code path would be free to drift.
func TestResendingARegistrationStillSendsTheETicketAlone(t *testing.T) {
	f := newDeliveryFixture(t)
	asRegistration(f)
	f.tickets.tickets = []notification.TicketDetail{
		holderTicket("REG234DEFG", "Halo Registrant", "halo@example.com"),
	}

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	recipient, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	assert.Equal(t, "budi@example.com", recipient,
		"the admin is shown the address that was actually mailed")

	require.Len(t, f.mailer.sent, 2)
	first, second := f.mailer.sent[0], f.mailer.sent[1]

	require.Len(t, second.Attachments, 1,
		"a resent registration carries the e-ticket and still no receipt")
	assert.Equal(t, first.Attachments[0].Filename, second.Attachments[0].Filename)
	assert.Equal(t, first.Subject, second.Subject)
	assert.Equal(t, first.To, second.To)

	// The code the guest may already be holding must survive the resend.
	plain, err := notification.RenderTicketsPDFPlain(f.orders.order, f.tickets.tickets, testBrand())
	require.NoError(t, err)
	assert.Contains(t, string(plain), "REG234DEFG",
		"a resend reuses the issued code; regenerating one would invalidate a pass already at the door")
}

// A registration has a zero total and no fees. Anything on the resend path that
// divides by, formats, or reasons about a total would fail here rather than in
// production, where it would fail after the guest was told the ticket was sent.
func TestResendingARegistrationNeverPrintsAMonetaryFigure(t *testing.T) {
	f := newDeliveryFixture(t)
	asRegistration(f)
	f.tickets.tickets = []notification.TicketDetail{
		holderTicket("REG234DEFG", "Halo Registrant", "halo@example.com"),
	}

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	_, err = f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)

	body := f.mailer.sent[1].HTMLBody
	for _, forbidden := range []string{"Rp", "IDR", "Total", "Receipt", "Subtotal"} {
		assert.NotContains(t, body, forbidden,
			"a resent registration email must carry no money and no receipt")
	}
}
