package event_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/cache"
)

// These tests assert two things about every invalidation, and the second one is
// the one that matters:
//
//  1. the post-write read returns the new content, and
//  2. the post-write read was a cache MISS.
//
// Asserting only (1) would pass even if the cache were bypassed entirely — which
// is exactly the regression these tests exist to catch. Miss-detection works by
// counting loader invocations: the loader runs on a miss and does not run on a
// hit.

func newEventTestRig(t *testing.T) (*pgxpool.Pool, *event.Service, *cache.Redis, *miniredis.Miniredis) {
	t.Helper()

	pool := testsupport.RequirePool(t)
	srv := miniredis.RunT(t)

	c, err := cache.NewRedis("redis://"+srv.Addr()+"/0", time.Minute,
		testsupport.DiscardLogger(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	log := testsupport.DiscardLogger()
	svc := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool), log).
		WithCache(c)

	return pool, svc, c, srv
}

// loads counts how many times the catalogue actually hit Postgres, by reading the
// hit/miss behaviour through the service. Because the service owns the loader we
// cannot instrument it directly, so we detect a miss the honest way: a miss
// repopulates the key, so the key's absence beforehand proves the read was served
// from the database.
func catalogueCached(t *testing.T, srv *miniredis.Miniredis, c *cache.Redis) bool {
	t.Helper()
	_, ok, err := c.Get(context.Background(), cache.EventsPublicKey())
	require.NoError(t, err)
	return ok
}

func TestCatalogueIsCachedAndIdenticalToUncached(t *testing.T) {
	pool, svc, c, srv := newEventTestRig(t)
	ctx := context.Background()

	testsupport.SeedEvent(t, pool, "concert-a", "PUBLISHED")
	testsupport.SeedEvent(t, pool, "concert-b", "PUBLISHED")
	testsupport.SeedEvent(t, pool, "draft-c", "DRAFT")

	require.False(t, catalogueCached(t, srv, c), "cold: nothing cached yet")

	first, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, first, 2, "drafts stay invisible")

	require.True(t, catalogueCached(t, srv, c), "the miss must have populated the cache")

	second, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Equal(t, first, second, "the cached read must be identical to the uncached one")

	// And identical to what a cache-free service returns — FR-005's real claim.
	uncached := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool),
		testsupport.DiscardLogger())
	direct, err := uncached.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Equal(t, direct, second)
}

func TestPublishingAnEventInvalidatesTheCatalogue(t *testing.T) {
	pool, svc, c, srv := newEventTestRig(t)
	ctx := context.Background()

	testsupport.SeedEvent(t, pool, "existing", "PUBLISHED")

	warm, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, warm, 1)
	require.True(t, catalogueCached(t, srv, c), "precondition: catalogue is warm")

	_, err = svc.CreateEvent(ctx, publishedEventRequest("brand-new"))
	require.NoError(t, err)

	// (2) the write invalidated: the warm entry is unreachable.
	require.False(t, catalogueCached(t, srv, c),
		"the create must have invalidated the catalogue, not left it to expire")

	// (1) and the very next read shows the new event, with no waiting period.
	after, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, after, 2)
}

func TestUpdatingAnEventInvalidatesTheCatalogue(t *testing.T) {
	pool, svc, c, srv := newEventTestRig(t)
	ctx := context.Background()

	seeded := testsupport.SeedEvent(t, pool, "renameable", "PUBLISHED")

	warm, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, warm, 1)

	req := publishedEventRequest("renameable")
	req.Name = "Renamed Event"
	_, err = svc.UpdateEvent(ctx, seeded.ID, req)
	require.NoError(t, err)

	require.False(t, catalogueCached(t, srv, c))

	after, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, "Renamed Event", after[0].Name)
}

func TestUnpublishingRemovesAnEventFromTheCatalogueImmediately(t *testing.T) {
	pool, svc, c, srv := newEventTestRig(t)
	ctx := context.Background()

	seeded := testsupport.SeedEvent(t, pool, "going-dark", "PUBLISHED")

	warm, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, warm, 1)

	req := publishedEventRequest("going-dark")
	req.Status = "DRAFT"
	_, err = svc.UpdateEvent(ctx, seeded.ID, req)
	require.NoError(t, err)

	require.False(t, catalogueCached(t, srv, c))

	after, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Empty(t, after, "an unpublished event must vanish on the next request")
}

func TestDeletingAnEventInvalidatesTheCatalogue(t *testing.T) {
	pool, svc, c, srv := newEventTestRig(t)
	ctx := context.Background()

	seeded := testsupport.SeedEvent(t, pool, "deletable", "PUBLISHED")

	_, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.True(t, catalogueCached(t, srv, c))

	require.NoError(t, svc.DeleteEvent(ctx, seeded.ID))

	require.False(t, catalogueCached(t, srv, c))

	after, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Empty(t, after)
}

// A write that rolls back must leave the cache alone — invalidating there would
// discard a warm, still-correct entry for no reason, and worse, it would mean the
// invalidation is not actually tied to commit (FR-007).
func TestRolledBackDeleteDoesNotInvalidate(t *testing.T) {
	pool, svc, c, srv := newEventTestRig(t)
	ctx := context.Background()

	seeded := testsupport.SeedEvent(t, pool, "has-orders", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, seeded.ID, "Regular", "100000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-CACHE-1", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 1)

	_, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.True(t, catalogueCached(t, srv, c), "precondition: warm")

	// Rejected by the has-orders guard, so the transaction rolls back.
	err = svc.DeleteEvent(ctx, seeded.ID)
	require.Error(t, err)

	require.True(t, catalogueCached(t, srv, c),
		"a rolled-back write must not invalidate: nothing changed")
}

// Two service instances sharing one Redis stand in for two API containers. A
// write through one must be visible through the other on the next read (FR-011).
// No pub/sub is involved: the shared generation counter does it.
func TestInvalidationCrossesInstances(t *testing.T) {
	pool, instanceA, c, _ := newEventTestRig(t)
	ctx := context.Background()

	instanceB := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool),
		testsupport.DiscardLogger()).WithCache(c)

	testsupport.SeedEvent(t, pool, "shared", "PUBLISHED")

	warmA, err := instanceA.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, warmA, 1)

	warmB, err := instanceB.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Equal(t, warmA, warmB)

	// Instance A processes the write.
	_, err = instanceA.CreateEvent(ctx, publishedEventRequest("added-by-a"))
	require.NoError(t, err)

	// Instance B, which never saw the write, must still serve the new state.
	afterB, err := instanceB.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, afterB, 2, "the other instance must not serve its own stale copy")
}

// With Redis gone the catalogue must still answer, correctly, from Postgres
// (FR-013, SC-005). A cache outage is a performance event, never an error.
func TestCatalogueSurvivesCacheOutage(t *testing.T) {
	pool, svc, _, srv := newEventTestRig(t)
	ctx := context.Background()

	testsupport.SeedEvent(t, pool, "resilient", "PUBLISHED")

	warm, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, warm, 1)

	srv.Close() // Redis is gone

	after, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err, "a cache outage must not fail the request")
	require.Equal(t, warm, after)

	// Writes must commit too.
	_, err = svc.CreateEvent(ctx, publishedEventRequest("written-while-down"))
	require.NoError(t, err, "a cache outage must not block writes")

	afterWrite, err := svc.ListPublishedEvents(ctx)
	require.NoError(t, err)
	require.Len(t, afterWrite, 2, "reads go straight to Postgres, so they stay correct")
}

// publishedEventRequest builds a valid EventRequest for a published event.
func publishedEventRequest(slug string) event.EventRequest {
	start := time.Now().Add(30 * 24 * time.Hour)
	description := "A test event"
	banner := "https://cdn.example.com/banner.png"
	return event.EventRequest{
		Name:        "Event " + slug,
		Slug:        slug,
		Description: &description,
		Venue:       "Test Venue",
		Address:     "Test Address",
		StartDate:   start,
		EndDate:     start.Add(24 * time.Hour),
		BannerURL:   &banner,
		Status:      "PUBLISHED",
	}
}

// --- Ticket-type CRUD invalidation ----------------------------------------
//
// This file previously covered only the event catalogue: nothing anywhere
// asserted that creating, editing or deleting a TICKET TYPE invalidates the
// per-event ticket list. The only things standing between a ticket-type change
// and a stale guest-facing list were the generation bump on three admin paths
// and a 10-minute TTL, neither of which was pinned by a test.
//
// Spec 015 makes that gap sharper by adding two guest-visible fields to exactly
// that cached DTO, so it is closed here rather than widened.

func ticketListCached(t *testing.T, c *cache.Redis, eventID uuid.UUID) bool {
	t.Helper()
	_, ok, err := c.Get(context.Background(), cache.TicketTypesPublicKey(eventID))
	require.NoError(t, err)
	return ok
}

func TestCreatingATicketTypeInvalidatesTheEventTicketList(t *testing.T) {
	pool, svc, c, _ := newEventTestRig(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "cache-tt-create", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	warm, err := svc.TicketTypesForEventSlug(ctx, "cache-tt-create")
	require.NoError(t, err)
	require.Len(t, warm, 1)
	require.True(t, ticketListCached(t, c, ev.ID), "the read should have populated the key")

	req := validTicketTypeRequest()
	req.EventID = ev.ID
	req.Name = "VIP"
	_, err = svc.CreateTicketType(ctx, req)
	require.NoError(t, err)

	assert.False(t, ticketListCached(t, c, ev.ID),
		"the create must orphan the cached list, or the new type stays invisible")

	after, err := svc.TicketTypesForEventSlug(ctx, "cache-tt-create")
	require.NoError(t, err)
	assert.Len(t, after, 2)
}

func TestUpdatingATicketTypeEventWindowInvalidatesTheList(t *testing.T) {
	pool, svc, c, _ := newEventTestRig(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "cache-tt-window", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	warm, err := svc.TicketTypesForEventSlug(ctx, "cache-tt-window")
	require.NoError(t, err)
	require.Len(t, warm, 1)
	before := warm[0].EventStart

	req := validTicketTypeRequest()
	req.Name = "Regular"
	req.EventStart = before.Add(2 * time.Hour)
	req.EventEnd = before.Add(6 * time.Hour)
	_, err = svc.UpdateTicketType(ctx, tt.ID, req)
	require.NoError(t, err)

	assert.False(t, ticketListCached(t, c, ev.ID))

	after, err := svc.TicketTypesForEventSlug(ctx, "cache-tt-window")
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.False(t, after[0].EventStart.Equal(before),
		"an edited admission window must be visible on the very next guest read (spec 015 FR-008)")
}

func TestDeletingATicketTypeInvalidatesTheEventTicketList(t *testing.T) {
	pool, svc, c, _ := newEventTestRig(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "cache-tt-delete", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	warm, err := svc.TicketTypesForEventSlug(ctx, "cache-tt-delete")
	require.NoError(t, err)
	require.Len(t, warm, 1)
	require.True(t, ticketListCached(t, c, ev.ID))

	require.NoError(t, svc.DeleteTicketType(ctx, tt.ID))

	assert.False(t, ticketListCached(t, c, ev.ID))

	after, err := svc.TicketTypesForEventSlug(ctx, "cache-tt-delete")
	require.NoError(t, err)
	assert.Empty(t, after)
}

// One event's ticket-type write must leave another event's cached list alone.
func TestTicketTypeWritesAreScopedToTheirOwnEvent(t *testing.T) {
	pool, svc, c, _ := newEventTestRig(t)
	ctx := context.Background()

	a := testsupport.SeedEvent(t, pool, "cache-scope-a", "PUBLISHED")
	b := testsupport.SeedEvent(t, pool, "cache-scope-b", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, a.ID, "Regular", "150000.00", 10)
	testsupport.SeedTicketType(t, pool, b.ID, "Regular", "150000.00", 10)

	_, err := svc.TicketTypesForEventSlug(ctx, "cache-scope-a")
	require.NoError(t, err)
	_, err = svc.TicketTypesForEventSlug(ctx, "cache-scope-b")
	require.NoError(t, err)
	require.True(t, ticketListCached(t, c, a.ID))
	require.True(t, ticketListCached(t, c, b.ID))

	req := validTicketTypeRequest()
	req.EventID = a.ID
	req.Name = "VIP"
	_, err = svc.CreateTicketType(ctx, req)
	require.NoError(t, err)

	assert.False(t, ticketListCached(t, c, a.ID), "the written event's list is orphaned")
	assert.True(t, ticketListCached(t, c, b.ID), "the other event's list is untouched")
}
