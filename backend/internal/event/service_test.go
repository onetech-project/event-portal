package event_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

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

	// Ticket data lives behind its own read since 008 (clarification):
	types, err := svc.TicketTypesForEventSlug(context.Background(), "jazz-fest")
	require.NoError(t, err)
	require.Len(t, types, 2)

	assert.Equal(t, "Regular", types[0].Name)
	assert.Equal(t, "150000.00", types[0].Price.String())
	// quota_remaining maps 1:1 onto ticket_types.quota — 0 means sold out, and no
	// arithmetic against a stored total is performed because none exists.
	assert.Equal(t, int32(0), types[0].QuotaRemaining)
	assert.Equal(t, int32(5), types[1].QuotaRemaining)
}

func TestServiceEventDetailHasEmptyTicketTypesWhenNoneConfigured(t *testing.T) {
	svc, pool := newService(t)
	testsupport.SeedEvent(t, pool, "bare-event", "PUBLISHED")

	types, err := svc.TicketTypesForEventSlug(context.Background(), "bare-event")

	require.NoError(t, err)
	assert.NotNil(t, types, "an empty list must serialize as [] not null")
	assert.Empty(t, types)
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

// --- T023: package with constituent outside its sales window ----------------

func TestPackageNotPurchasableWhenConstituentWindowClosed(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "constituent-window", "PUBLISHED")
	// Ticket whose sales window already closed.
	closed := testsupport.SeedTicketTypeWindow(t, pool, ev.ID, "Early Bird", 10,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	// Package is within its own window (default: now-1d to now+29d).
	pkg := testsupport.SeedPackage(t, pool, ev.ID, "Early Bundle", "25000.00", true)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, closed.ID, ev.ID, 1)

	packages, err := svc.PackagesForEventSlug(ctx, "constituent-window")

	require.NoError(t, err)
	require.Len(t, packages, 1)
	assert.False(t, packages[0].Purchasable,
		"package whose constituent is outside its window must not be purchasable")
	assert.Equal(t, int32(10), packages[0].AvailableUnits,
		"quota exists but the closed window makes it unpurchasable")
}

// --- T024: INACTIVE packages absent from public payload ---------------------

func TestInactivePackageAbsentFromPublicPayload(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "inactive-filter", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "General", "10000.00", 10)

	active := testsupport.SeedPackage(t, pool, ev.ID, "Active Bundle", "15000.00", true)
	testsupport.SeedPackageTicket(t, pool, active.ID, tt.ID, ev.ID, 1)

	inactive := testsupport.SeedPackage(t, pool, ev.ID, "Hidden Bundle", "10000.00", false)
	testsupport.SeedPackageTicket(t, pool, inactive.ID, tt.ID, ev.ID, 1)

	packages, err := svc.PackagesForEventSlug(ctx, "inactive-filter")

	require.NoError(t, err)
	require.Len(t, packages, 1, "only active packages appear to guests")
	assert.Equal(t, "Active Bundle", packages[0].Name)
	_ = inactive // seeded but intentionally excluded
}
