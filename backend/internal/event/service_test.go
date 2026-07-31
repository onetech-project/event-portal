package event_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

func newService(t *testing.T) (*event.Service, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)
	return event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger()), pool
}

func TestServiceListsOnlyPublishedEvents(t *testing.T) {
	svc, pool := newService(t)

	testsupport.SeedEvent(t, pool, "live-show", "PUBLISHED")
	testsupport.SeedEvent(t, pool, "hidden-show", "DRAFT")

	events, err := svc.ListPublishedEvents(context.Background())

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "live-show", events[0].Slug)
}

func TestServiceReturnsEmptyListNotNilWhenNoEventsExist(t *testing.T) {
	svc, _ := newService(t)

	events, err := svc.ListPublishedEvents(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, events, "an empty catalog must serialize as [] not null")
	assert.Empty(t, events)
}

func TestServiceReturnsEventDetailWithItsTicketTypes(t *testing.T) {
	svc, pool := newService(t)

	ev := testsupport.SeedEvent(t, pool, "jazz-fest", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "VIP", "500000.00", 5)
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 0)

	detail, err := svc.GetPublishedEventBySlug(context.Background(), "jazz-fest")

	require.NoError(t, err)
	assert.Equal(t, ev.ID, detail.ID)
	require.Len(t, detail.TicketTypes, 2)

	assert.Equal(t, "Regular", detail.TicketTypes[0].Name)
	assert.Equal(t, "150000.00", detail.TicketTypes[0].Price.String())
	// quota_remaining maps 1:1 onto ticket_types.quota — 0 means sold out, and no
	// arithmetic against a stored total is performed because none exists.
	assert.Equal(t, int32(0), detail.TicketTypes[0].QuotaRemaining)
	assert.Equal(t, int32(5), detail.TicketTypes[1].QuotaRemaining)
}

func TestServiceEventDetailHasEmptyTicketTypesWhenNoneConfigured(t *testing.T) {
	svc, pool := newService(t)
	testsupport.SeedEvent(t, pool, "bare-event", "PUBLISHED")

	detail, err := svc.GetPublishedEventBySlug(context.Background(), "bare-event")

	require.NoError(t, err)
	assert.NotNil(t, detail.TicketTypes)
	assert.Empty(t, detail.TicketTypes)
}

func TestServiceMapsMissingEventToA404AppError(t *testing.T) {
	svc, _ := newService(t)

	_, err := svc.GetPublishedEventBySlug(context.Background(), "nope")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeEventNotFound, appErr.Code)
}

func TestServiceMapsUnpublishedEventToA404AppError(t *testing.T) {
	svc, pool := newService(t)
	testsupport.SeedEvent(t, pool, "draft-only", "DRAFT")

	_, err := svc.GetPublishedEventBySlug(context.Background(), "draft-only")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}
