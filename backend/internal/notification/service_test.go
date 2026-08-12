package notification_test

import (
	"context"
	"errors"
	"net/http"
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

	orders := &fakeOrders{order: notification.OrderDelivery{
		ID:          orderID,
		OrderNumber: "ORD-20260731-ABCDEF",
		BuyerName:   "Budi Santoso",
		BuyerEmail:  "budi@example.com",
		Status:      "PAID",
		TotalAmount: decimal.NewFromInt(550000),
	}}
	tickets := &fakeTickets{tickets: []notification.TicketDetail{
		holderTicket("ABC234DEFG", "Ani Lestari", "ani@example.com"),
		holderTicket("HJK567LMNP", "Bayu Wijaya", "bayu@example.com"),
	}}
	mailer := &fakeMailer{}

	return deliveryFixture{
		svc:     notification.NewService(orders, tickets, mailer, testsupport.DiscardLogger()),
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
	assert.Contains(t, msg.Subject, "Jazz Night 2026")

	require.Len(t, msg.Attachments, 1, "every ticket travels in a single PDF")
	assert.Equal(t, "application/pdf", msg.Attachments[0].ContentType)
	assert.Contains(t, msg.Attachments[0].Filename, "ORD-20260731-ABCDEF")
	assert.True(t, strings.HasPrefix(string(msg.Attachments[0].Content), "%PDF-"),
		"the attachment must be a real, non-empty PDF")

	// Every code is in the one PDF's body counterpart, so no holder is dropped.
	for _, ticket := range f.tickets.tickets {
		assert.Contains(t, msg.HTMLBody, ticket.TicketCode)
	}

	onePage, err := notification.RenderTicketsPDF(f.orders.order, f.tickets.tickets[:1])
	require.NoError(t, err)
	assert.Greater(t, len(msg.Attachments[0].Content), len(onePage),
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
	assert.Contains(t, f.mailer.sent[0].HTMLBody, "Budi Santoso")
	assert.Contains(t, f.mailer.sent[0].HTMLBody, "Bayu Wijaya")
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

// The buyer's one email names every holder in the order and carries every
// ticket code, so the buyer can hand the right pass to the right person.
func TestSendTicketEmailBodyNamesEveryHolderAndTicket(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1)

	body := f.mailer.sent[0].HTMLBody

	assert.Contains(t, body, "TICKET HOLDER")
	assert.Contains(t, body, "Ani Lestari")
	assert.Contains(t, body, "Bayu Wijaya")
	assert.Contains(t, body, "ABC234DEFG")
	assert.Contains(t, body, "HJK567LMNP")
	// Addressed to the buyer, so the buyer's own address appears on the copy.
	assert.Contains(t, body, "budi@example.com")
}

// Figma 251-2: the email is a receipt — PAID badge, invoice number, the
// e-ticket cards with their codes, and the amount paid.
func TestSendTicketEmailBodyIsTheReceiptLayout(t *testing.T) {
	f := newDeliveryFixture(t)

	_, err := f.svc.SendTicketEmail(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1)

	body := f.mailer.sent[0].HTMLBody
	assert.Contains(t, body, "PAID")
	assert.Contains(t, body, "E-Ticket")
	assert.Contains(t, body, "Total Payment")
	assert.Contains(t, body, "Rp 550.000")
	assert.Contains(t, body, "ORD-20260731-ABCDEF")
	for _, ticket := range f.tickets.tickets {
		assert.Contains(t, body, ticket.TicketCode)
	}
}

// Spec 011 FR-016: the receipt email is where the itemization lives — each
// purchased line, the pre-fee Ticket Total, the frozen fees, and the Total
// Payment (the order page's form step deliberately shows none of it).
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

	{
		body := f.mailer.sent[0].HTMLBody
		assert.Contains(t, body, "Early Bird")
		assert.Contains(t, body, "x2")
		assert.Contains(t, body, "Rp 300.000")
		assert.Contains(t, body, "VIP Duo Bundle")
		assert.Contains(t, body, "Ticket Total")
		assert.Contains(t, body, "Rp 500.000")
		assert.Contains(t, body, "Platform fee")
		assert.Contains(t, body, "Rp 50.000")
		assert.Contains(t, body, "Total Payment")
		assert.Contains(t, body, "Rp 550.000")
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
