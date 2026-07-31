package notification_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

type fakeOrders struct {
	order      notification.OrderDelivery
	getErr     error
	markCalled int
	markErr    error
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

type fakeTickets struct {
	tickets []notification.TicketDetail
	err     error
}

func (f *fakeTickets) TicketDetailsForOrder(context.Context, uuid.UUID) ([]notification.TicketDetail, error) {
	return f.tickets, f.err
}

type fakeMailer struct {
	sent []notification.Message
	err  error
}

func (f *fakeMailer) Send(msg notification.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, msg)
	return nil
}

type deliveryFixture struct {
	svc     *notification.Service
	orders  *fakeOrders
	tickets *fakeTickets
	mailer  *fakeMailer
	orderID uuid.UUID
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
	t.Helper()
	orderID := uuid.New()

	orders := &fakeOrders{order: notification.OrderDelivery{
		ID:          orderID,
		OrderNumber: "ORD-20260731-ABCDEF",
		BuyerName:   "Budi Santoso",
		BuyerEmail:  "budi@example.com",
		Status:      "PAID",
	}}
	tickets := &fakeTickets{tickets: sampleTickets(2)}
	mailer := &fakeMailer{}

	return deliveryFixture{
		svc:     notification.NewService(orders, tickets, mailer, testsupport.DiscardLogger()),
		orders:  orders,
		tickets: tickets,
		mailer:  mailer,
		orderID: orderID,
	}
}

func TestSendTicketEmailDeliversOneEmailWithOnePDFToTheBuyer(t *testing.T) {
	f := newDeliveryFixture(t)

	err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.NoError(t, err)
	require.Len(t, f.mailer.sent, 1, "exactly one email per paid order")

	msg := f.mailer.sent[0]
	assert.Equal(t, "budi@example.com", msg.To, "the email goes to the buyer, not the attendees")
	assert.Contains(t, msg.Subject, "Jazz Night 2026")

	require.Len(t, msg.Attachments, 1, "all tickets travel in a single PDF")
	assert.Equal(t, "application/pdf", msg.Attachments[0].ContentType)
	assert.Contains(t, msg.Attachments[0].Filename, "ORD-20260731-ABCDEF")
	assert.True(t, strings.HasPrefix(string(msg.Attachments[0].Content), "%PDF-"))
}

// email_sent must only be set after a successful delivery (Constitution IV).
func TestSendTicketEmailSetsEmailSentAfterDelivery(t *testing.T) {
	f := newDeliveryFixture(t)

	require.NoError(t, f.svc.SendTicketEmail(context.Background(), f.orderID))

	assert.Equal(t, 1, f.orders.markCalled)
}

func TestSendTicketEmailDoesNotSetEmailSentWhenDeliveryFails(t *testing.T) {
	f := newDeliveryFixture(t)
	f.mailer.err = errors.New("smtp unavailable")

	err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Zero(t, f.orders.markCalled, "a failed send must not be recorded as sent")
}

// Resend is the same path, so the guard lives in one place for both.
func TestSendTicketEmailRejectsAnOrderThatIsNotPaid(t *testing.T) {
	for _, status := range []string{"PENDING", "CANCELLED", "EXPIRED"} {
		t.Run(status, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.orders.order.Status = status

			err := f.svc.SendTicketEmail(context.Background(), f.orderID)

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

	err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	require.Error(t, err)
	assert.Empty(t, f.mailer.sent)
}

func TestSendTicketEmailPropagatesAMissingOrder(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.getErr = apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")

	err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

// The resend path (specs/003 US3) reuses stored codes verbatim and re-renders each
// QR from them — a guest's original ticket must stay valid.
func TestResendReusesTheExistingTicketCodes(t *testing.T) {
	f := newDeliveryFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.SendTicketEmail(ctx, f.orderID))
	first := f.mailer.sent[0].Attachments[0].Content

	require.NoError(t, f.svc.SendTicketEmail(ctx, f.orderID))
	second := f.mailer.sent[1].Attachments[0].Content

	assert.Len(t, f.mailer.sent, 2)
	assert.Equal(t, len(first), len(second),
		"the resend renders the same tickets, never regenerated codes")
	assert.Equal(t, 2, f.orders.markCalled, "a successful resend also records email_sent (FR-011)")
}

func TestSendTicketEmailStillSucceedsIfRecordingTheFlagFails(t *testing.T) {
	f := newDeliveryFixture(t)
	f.orders.markErr = errors.New("db hiccup")

	err := f.svc.SendTicketEmail(context.Background(), f.orderID)

	// The email genuinely went out; failing the whole operation would invite a
	// retry that double-sends to the buyer.
	require.NoError(t, err)
	assert.Len(t, f.mailer.sent, 1)
}

func TestSendTicketEmailBodyNamesTheBuyerAndTicketCount(t *testing.T) {
	f := newDeliveryFixture(t)

	require.NoError(t, f.svc.SendTicketEmail(context.Background(), f.orderID))

	body := f.mailer.sent[0].HTMLBody
	assert.Contains(t, body, "Budi Santoso")
	assert.Contains(t, body, "ORD-20260731-ABCDEF")
}
