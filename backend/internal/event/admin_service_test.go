package event_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// newAdminService wires the real order repository as the OrderChecker, so the
// delete guards and sold counts are exercised against real order data rather than
// a stub that could agree with a wrong implementation.
func newAdminService(t *testing.T) (*event.Service, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)
	svc := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool), testsupport.DiscardLogger())
	return svc, pool
}

func appErrOf(t *testing.T, err error) *apperr.Error {
	t.Helper()
	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr), "expected an *apperr.Error, got %v", err)
	return appErr
}

// --- Events ---------------------------------------------------------------

func TestAdminListEventsIncludesEveryStatus(t *testing.T) {
	svc, pool := newAdminService(t)
	testsupport.SeedEvent(t, pool, "published-one", "PUBLISHED")
	testsupport.SeedEvent(t, pool, "draft-one", "DRAFT")
	testsupport.SeedEvent(t, pool, "done-one", "COMPLETED")

	page, err := svc.ListEvents(context.Background(), httpx.NewPageRequest(1, 20))

	require.NoError(t, err)
	assert.Len(t, page.Items, 3, "the admin list is not filtered by status, unlike the guest one")
	assert.EqualValues(t, 3, page.Total)
}

func TestCreateEventPersistsTheSubmittedFields(t *testing.T) {
	svc, _ := newAdminService(t)
	req := validEventRequest()
	banner := "https://cdn.example.com/jazz.png"
	req.BannerURL = &banner

	created, err := svc.CreateEvent(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "jazz-night-2026", created.Slug)
	assert.Equal(t, event.StatusDraft, created.Status)
	require.NotNil(t, created.BannerURL)
	assert.Equal(t, banner, *created.BannerURL,
		"banner_url is a plain admin-supplied URL, stored verbatim")
}

func TestCreateEventRejectsADuplicateSlug(t *testing.T) {
	svc, _ := newAdminService(t)
	ctx := context.Background()
	_, err := svc.CreateEvent(ctx, validEventRequest())
	require.NoError(t, err)

	_, err = svc.CreateEvent(ctx, validEventRequest())

	appErr := appErrOf(t, err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeSlugNotUnique, appErr.Code)
}

func TestUpdateEventReplacesItsFields(t *testing.T) {
	svc, _ := newAdminService(t)
	ctx := context.Background()
	created, err := svc.CreateEvent(ctx, validEventRequest())
	require.NoError(t, err)

	req := validEventRequest()
	req.Name = "Jazz Night 2026 (Rescheduled)"
	req.Status = event.StatusPublished

	updated, err := svc.UpdateEvent(ctx, created.ID, req)

	require.NoError(t, err)
	assert.Equal(t, "Jazz Night 2026 (Rescheduled)", updated.Name)
	assert.Equal(t, event.StatusPublished, updated.Status)
}

func TestUpdateEventReportsAMissingEvent(t *testing.T) {
	svc, pool := newAdminService(t)
	ghost := testsupport.SeedEvent(t, pool, "ghost", "DRAFT")
	require.NoError(t, svc.DeleteEvent(context.Background(), ghost.ID))

	_, err := svc.UpdateEvent(context.Background(), ghost.ID, validEventRequest())

	assert.Equal(t, http.StatusNotFound, appErrOf(t, err).HTTPStatus)
}

func TestUpdateEventRejectsASlugTakenByAnotherEvent(t *testing.T) {
	svc, _ := newAdminService(t)
	ctx := context.Background()
	first, err := svc.CreateEvent(ctx, validEventRequest())
	require.NoError(t, err)

	second := validEventRequest()
	second.Slug = "rock-fest-2026"
	_, err = svc.CreateEvent(ctx, second)
	require.NoError(t, err)

	// Try to move the second event onto the first one's slug.
	second.Slug = first.Slug
	_, err = svc.UpdateEvent(ctx, first.ID, second)
	require.NoError(t, err, "an event keeping its own slug is fine")

	conflicting := validEventRequest()
	conflicting.Slug = first.Slug
	_, err = svc.CreateEvent(ctx, conflicting)
	assert.Equal(t, apperr.CodeSlugNotUnique, appErrOf(t, err).Code)
}

// --- Event delete ---------------------------------------------------------

func TestDeleteEventRemovesTheEventAndItsTicketTypes(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "deletable", "DRAFT")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	testsupport.SeedTicketType(t, pool, ev.ID, "VIP", "500000.00", 5)

	require.NoError(t, svc.DeleteEvent(ctx, ev.ID))

	_, err := svc.GetEventDetail(ctx, ev.ID)
	assert.Equal(t, http.StatusNotFound, appErrOf(t, err).HTTPStatus)

	var remaining int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM ticket_types WHERE event_id = $1`, ev.ID).Scan(&remaining))
	assert.Zero(t, remaining, "ticket_types.event_id is ON DELETE RESTRICT, so they must go first")
}

func TestDeleteEventReportsAMissingEvent(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()
	ev := testsupport.SeedEvent(t, pool, "gone", "DRAFT")
	require.NoError(t, svc.DeleteEvent(ctx, ev.ID))

	err := svc.DeleteEvent(ctx, ev.ID)

	assert.Equal(t, http.StatusNotFound, appErrOf(t, err).HTTPStatus)
}

func TestDeleteEventIsBlockedByAnOrderItemReference(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "sold-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-EV1", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 1)

	err := svc.DeleteEvent(ctx, ev.ID)

	appErr := appErrOf(t, err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeEventHasOrders, appErr.Code)

	// Nothing was deleted — the guard and the deletes share one transaction.
	_, err = svc.GetEventDetail(ctx, ev.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, pool, tt.ID))
}

// Both FKs onto ticket_types are ON DELETE RESTRICT, so an attendee-only
// reference must block the delete too (Constitution Principle VI).
func TestDeleteEventIsBlockedByAnAttendeeOnlyReference(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "attendee-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-EV2", "PAID")
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Andi", "andi@example.com")

	err := svc.DeleteEvent(ctx, ev.ID)

	assert.Equal(t, apperr.CodeEventHasOrders, appErrOf(t, err).Code)

	_, err = svc.GetEventDetail(ctx, ev.ID)
	require.NoError(t, err, "the transaction rolled back, so the event survives")
}

func TestDeleteEventWithNoTicketTypesSucceeds(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "bare", "DRAFT")

	assert.NoError(t, svc.DeleteEvent(context.Background(), ev.ID))
}

// --- Ticket types ---------------------------------------------------------

func TestCreateTicketTypeStartsWithZeroSold(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "for-types", "DRAFT")

	req := validTicketTypeRequest()
	req.EventID = ev.ID

	created, err := svc.CreateTicketType(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, int32(100), created.Quota)
	assert.Zero(t, created.Sold)
	assert.Equal(t, "150000.00", created.Price.String())
}

func TestCreateTicketTypeRejectsAnUnknownEvent(t *testing.T) {
	svc, _ := newAdminService(t)

	_, err := svc.CreateTicketType(context.Background(), validTicketTypeRequest())

	appErr := appErrOf(t, err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus,
		"an unknown event_id must be a clear 400, not a raw foreign-key violation")
}

func TestListTicketTypesReportsDerivedSoldCounts(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "with-sales", "PUBLISHED")
	sold := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 7)
	unsold := testsupport.SeedTicketType(t, pool, ev.ID, "VIP", "500000.00", 5)

	first := testsupport.SeedOrder(t, pool, "ORD-S1", "PAID")
	second := testsupport.SeedOrder(t, pool, "ORD-S2", "PAID")
	testsupport.SeedOrderItem(t, pool, first.ID, sold.ID, 2)
	testsupport.SeedOrderItem(t, pool, second.ID, sold.ID, 1)

	types, err := svc.ListTicketTypes(ctx, ev.ID)

	require.NoError(t, err)
	require.Len(t, types, 2)

	byID := map[string]event.TicketTypeAdminView{}
	for _, tt := range types {
		byID[tt.ID.String()] = tt
	}

	assert.Equal(t, 3, byID[sold.ID.String()].Sold)
	assert.Equal(t, int32(7), byID[sold.ID.String()].Quota,
		"quota is the REMAINING quota and is independent of the derived sold count")
	assert.Zero(t, byID[unsold.ID.String()].Sold)
}

// The submitted quota replaces the remaining quota absolutely; the server must
// never re-subtract past sales from it.
func TestUpdateTicketTypeSetsRemainingQuotaAbsolutely(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "absolute-quota", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 7)
	ord := testsupport.SeedOrder(t, pool, "ORD-ABS", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 3)

	req := validTicketTypeRequest()
	req.Quota = 20

	updated, err := svc.UpdateTicketType(ctx, tt.ID, req)

	require.NoError(t, err)
	assert.Equal(t, int32(20), updated.Quota, "20 means 20 remaining, not 20 minus 3 sold")
	assert.Equal(t, 3, updated.Sold, "the sold count is reported but never subtracted")
	assert.Equal(t, int32(20), testsupport.QuotaOf(t, pool, tt.ID))
}

func TestUpdateTicketTypeIgnoresASubmittedEventID(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	original := testsupport.SeedEvent(t, pool, "original-event", "PUBLISHED")
	other := testsupport.SeedEvent(t, pool, "other-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, original.ID, "Regular", "150000.00", 10)

	req := validTicketTypeRequest()
	req.EventID = other.ID

	updated, err := svc.UpdateTicketType(ctx, tt.ID, req)

	require.NoError(t, err)
	assert.Equal(t, original.ID, updated.EventID, "event_id is immutable on update")
}

func TestUpdateTicketTypeReportsAMissingTicketType(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "missing-tt-event", "PUBLISHED")

	_, err := svc.UpdateTicketType(context.Background(), ev.ID, validTicketTypeRequest())

	assert.Equal(t, http.StatusNotFound, appErrOf(t, err).HTTPStatus)
}

func TestGetTicketTypeIncludesItsSoldCount(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "one-type", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-GET", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 4)

	got, err := svc.GetTicketType(ctx, tt.ID)

	require.NoError(t, err)
	assert.Equal(t, 4, got.Sold)
}

// --- Ticket-type delete ---------------------------------------------------

func TestDeleteTicketTypeSucceedsWhenUnsold(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "deletable-type", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	require.NoError(t, svc.DeleteTicketType(ctx, tt.ID))

	_, err := svc.GetTicketType(ctx, tt.ID)
	assert.Equal(t, http.StatusNotFound, appErrOf(t, err).HTTPStatus)
}

func TestDeleteTicketTypeIsBlockedByAnOrderItemReference(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "sold-type", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-TT1", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 1)

	err := svc.DeleteTicketType(ctx, tt.ID)

	appErr := appErrOf(t, err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeTicketTypeHasOrders, appErr.Code)

	_, err = svc.GetTicketType(ctx, tt.ID)
	require.NoError(t, err, "nothing was deleted")
}

func TestDeleteTicketTypeIsBlockedByAnAttendeeOnlyReference(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "attendee-type", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-TT2", "PAID")
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Sari", "sari@example.com")

	err := svc.DeleteTicketType(ctx, tt.ID)

	assert.Equal(t, apperr.CodeTicketTypeHasOrders, appErrOf(t, err).Code,
		"checking order_items alone would leak a raw FK violation instead of a 400")
}

func TestDeleteTicketTypeReportsAMissingTicketType(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "no-such-type", "PUBLISHED")

	err := svc.DeleteTicketType(context.Background(), ev.ID)

	assert.Equal(t, http.StatusNotFound, appErrOf(t, err).HTTPStatus)
}

// --- Interaction with the guest catalog -----------------------------------

// An admin-set remaining quota is the same counter checkout decrements.
func TestAdminQuotaEditIsVisibleToTheGuestCatalog(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "shared-counter", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	req := validTicketTypeRequest()
	req.Quota = 3
	req.Price = money.From(decimal.RequireFromString("175000.00"))
	req.SalesStart = time.Now().Add(-time.Hour)
	req.SalesEnd = time.Now().Add(24 * time.Hour)
	_, err := svc.UpdateTicketType(ctx, tt.ID, req)
	require.NoError(t, err)

	// Ticket data moved off the detail response (clarification 2026-08-05);
	// the split read is now the surface that must reflect the admin edit.
	types, err := svc.TicketTypesForEventSlug(ctx, "shared-counter")

	require.NoError(t, err)
	require.Len(t, types, 1)
	assert.Equal(t, int32(3), types[0].QuotaRemaining)
	assert.Equal(t, "175000.00", types[0].Price.String())
}

// --- Ticket type event window containment (spec 015) ----------------------

func TestCreateTicketTypeRejectsAWindowOutsideItsEvent(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "containment-create", "PUBLISHED")

	for name, mutate := range map[string]func(*event.TicketTypeRequest){
		"starts before the event opens": func(r *event.TicketTypeRequest) {
			r.EventStart = time.Now().Add(29 * 24 * time.Hour)
		},
		"ends after the event closes": func(r *event.TicketTypeRequest) {
			r.EventEnd = time.Now().Add(32 * 24 * time.Hour)
		},
		"entirely elsewhere": func(r *event.TicketTypeRequest) {
			r.EventStart = time.Now().Add(60 * 24 * time.Hour)
			r.EventEnd = time.Now().Add(61 * 24 * time.Hour)
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := validTicketTypeRequest()
			req.EventID = ev.ID
			mutate(&req)

			_, err := svc.CreateTicketType(context.Background(), req)

			appErr := appErrOf(t, err)
			assert.Equal(t, apperr.CodeInvalidDateRange, appErr.Code)
			assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
			assert.NotNil(t, appErr.Data, "the refusal names the event's own bounds so the form can point at them")
		})
	}
}

func TestUpdateTicketTypeRejectsAWindowOutsideItsEvent(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "containment-update", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	req := validTicketTypeRequest()
	req.EventStart = time.Now().Add(60 * 24 * time.Hour)
	req.EventEnd = time.Now().Add(61 * 24 * time.Hour)

	_, err := svc.UpdateTicketType(context.Background(), tt.ID, req)

	assert.Equal(t, apperr.CodeInvalidDateRange, appErrOf(t, err).Code,
		"update resolves the owning event from the stored row, since event_id is ignored on update")
}

func TestCreateTicketTypeAcceptsAWindowEqualToItsEvent(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "containment-exact", "PUBLISHED")

	stored, err := svc.GetEventDetail(context.Background(), ev.ID)
	require.NoError(t, err)

	req := validTicketTypeRequest()
	req.EventID = ev.ID
	req.EventStart = stored.StartDate
	req.EventEnd = stored.EndDate

	created, err := svc.CreateTicketType(context.Background(), req)

	require.NoError(t, err, "the boundaries are inclusive — this is exactly what migration 0014 backfilled")
	assert.WithinDuration(t, stored.StartDate, created.EventStart, time.Millisecond)
	assert.WithinDuration(t, stored.EndDate, created.EventEnd, time.Millisecond)
}

func TestCreateTicketTypePersistsItsEventWindow(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "window-persists", "PUBLISHED")

	req := validTicketTypeRequest()
	req.EventID = ev.ID

	created, err := svc.CreateTicketType(context.Background(), req)

	require.NoError(t, err)
	// Postgres stores timestamptz to microsecond precision, so a round-tripped
	// time.Time is never bit-identical to the one sent.
	assert.WithinDuration(t, req.EventStart, created.EventStart, time.Millisecond)
	assert.WithinDuration(t, req.EventEnd, created.EventEnd, time.Millisecond)
	assert.False(t, created.EventStart.Equal(created.SalesStart), "the two windows are stored separately")
}

// FR-005a: the mirror-image guard is deliberately absent. Enforcing containment
// on the event edit too would deadlock a reschedule — neither the event nor its
// tickets could move first.
func TestUpdateEventSucceedsEvenWhenItStrandsATicketWindow(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "reschedule-me", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	before, err := svc.GetTicketType(ctx, tt.ID)
	require.NoError(t, err)

	req := validEventRequest()
	req.Slug = "reschedule-me"
	req.Status = event.StatusPublished
	req.StartDate = time.Now().Add(60 * 24 * time.Hour)
	req.EndDate = time.Now().Add(61 * 24 * time.Hour)

	_, err = svc.UpdateEvent(ctx, ev.ID, req)
	require.NoError(t, err, "the event must be movable first; its ticket types follow")

	after, err := svc.GetTicketType(ctx, tt.ID)
	require.NoError(t, err)
	assert.True(t, after.EventStart.Equal(before.EventStart), "an event edit never cascades onto a ticket window (FR-005c)")
	assert.True(t, after.EventEnd.Equal(before.EventEnd))

	// And the recovery direction now works: the ticket can move into the new range.
	ttReq := validTicketTypeRequest()
	ttReq.EventStart = req.StartDate.Add(time.Hour)
	ttReq.EventEnd = req.EndDate.Add(-time.Hour)

	moved, err := svc.UpdateTicketType(ctx, tt.ID, ttReq)
	require.NoError(t, err, "move the event, then its ticket types — the one order that works")
	assert.WithinDuration(t, ttReq.EventStart, moved.EventStart, time.Millisecond)
}
