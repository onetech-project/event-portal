package cache

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Paging joins the fingerprint, not the family and not the scope. That split is
// what keeps spec 021 inside Constitution Principle VII without an amendment: a
// page is a filtered variant of a list already on the closed surface list, so
// the surface count does not change and no page number can reach a metric label.

func TestDifferentPagesTakeDifferentKeys(t *testing.T) {
	// The failure this pins is invisible without a cache: identical keys would
	// serve page 1's rows for every page, and only when caching is enabled.
	id := uuid.New()
	paid := "PAID"

	seen := map[string]bool{}
	for _, k := range []Key{
		OrdersAdminKey(&paid, &id, Paging{Page: 1, Size: 20}),
		OrdersAdminKey(&paid, &id, Paging{Page: 2, Size: 20}),
		OrdersAdminKey(&paid, &id, Paging{Page: 1, Size: 50}),
		OrdersAdminKey(&paid, &id, Paging{Page: 2, Size: 50}),
		AttendeesAdminKey(&id, nil, Paging{Page: 1, Size: 20}),
		AttendeesAdminKey(&id, nil, Paging{Page: 2, Size: 20}),
		EventsAdminKey(Paging{Page: 1, Size: 20}),
		EventsAdminKey(Paging{Page: 2, Size: 20}),
		EventsAdminKey(Paging{Page: 1, Size: 100}),
	} {
		p := k.EntryPrefix()
		require.False(t, seen[p], "collision on %s", p)
		seen[p] = true
	}
}

func TestPagingRendersInAFixedOrderAfterTheFilters(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	paid := "PAID"

	require.Equal(t,
		"list:orders_admin:st=PAID:ev="+id.String()+":p=3:n=50:g",
		OrdersAdminKey(&paid, &id, Paging{Page: 3, Size: 50}).EntryPrefix())

	require.Equal(t,
		"list:attendees_admin:or="+id.String()+":ev=_:p=2:n=20:g",
		AttendeesAdminKey(&id, nil, Paging{Page: 2, Size: 20}).EntryPrefix())

	require.Equal(t,
		"list:events_admin:p=4:n=100:g",
		EventsAdminKey(Paging{Page: 4, Size: 100}).EntryPrefix())
}

func TestPagedKeysAreStable(t *testing.T) {
	id := uuid.New()
	paid := "PAID"
	pg := Paging{Page: 7, Size: 50}
	for i := 0; i < 50; i++ {
		require.Equal(t,
			OrdersAdminKey(&paid, &id, pg).EntryPrefix(),
			OrdersAdminKey(&paid, &id, pg).EntryPrefix())
	}
}

func TestPagingDoesNotChangeTheScope(t *testing.T) {
	// If paging reached the scope, one write would have to know which pages were
	// warm in order to invalidate them. It must not.
	id := uuid.New()
	require.Equal(t, Orders(), OrdersAdminKey(nil, nil, Paging{Page: 9, Size: 50}).Scope)
	require.Equal(t, Orders(), AttendeesAdminKey(nil, &id, Paging{Page: 9, Size: 50}).Scope)
	require.Equal(t, Events(), EventsAdminKey(Paging{Page: 9, Size: 50}).Scope)
}

func TestPagingDoesNotChangeTheFamily(t *testing.T) {
	// Family is the Prometheus label. A page number reaching it would make the
	// label set unbounded.
	require.Equal(t, FamilyOrdersAdmin, OrdersAdminKey(nil, nil, Paging{Page: 9, Size: 50}).Family)
	require.Equal(t, FamilyAttendeesAdmin, AttendeesAdminKey(nil, nil, Paging{Page: 9, Size: 50}).Family)
	require.Equal(t, FamilyEventsAdmin, EventsAdminKey(Paging{Page: 9, Size: 50}).Family)
}

func TestOneBumpInvalidatesEveryPageOfEveryVariant(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	id := uuid.New()
	paid := "PAID"
	variants := []Key{
		OrdersAdminKey(nil, nil, Paging{Page: 1, Size: 20}),
		OrdersAdminKey(nil, nil, Paging{Page: 2, Size: 20}),
		OrdersAdminKey(&paid, &id, Paging{Page: 3, Size: 50}),
		OrdersAdminKey(&paid, &id, Paging{Page: 1, Size: 100}),
		AttendeesAdminKey(nil, &id, Paging{Page: 4, Size: 20}),
	}
	for _, k := range variants {
		require.NoError(t, c.Set(ctx, k, []byte(`{"items":[]}`)))
	}
	for _, k := range variants {
		_, ok, _ := c.Get(ctx, k)
		require.True(t, ok, "precondition: every page warm")
	}

	// One INCR, whatever the page count. This is the property that lets paging
	// multiply the entry count without multiplying the invalidation work.
	require.NoError(t, c.Invalidate(ctx, Orders()))

	for _, k := range variants {
		_, ok, _ := c.Get(ctx, k)
		require.False(t, ok, "page %s must be invalidated", k.EntryPrefix())
	}
}

func TestEventPagesAreInvalidatedByTheEventsScope(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	pages := []Key{
		EventsAdminKey(Paging{Page: 1, Size: 20}),
		EventsAdminKey(Paging{Page: 2, Size: 20}),
		EventsAdminKey(Paging{Page: 1, Size: 100}),
	}
	for _, k := range pages {
		require.NoError(t, c.Set(ctx, k, []byte(`{"items":[]}`)))
	}

	require.NoError(t, c.Invalidate(ctx, Events()))

	for _, k := range pages {
		_, ok, _ := c.Get(ctx, k)
		require.False(t, ok, "page %s must be invalidated", k.EntryPrefix())
	}
}
