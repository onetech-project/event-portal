package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/logger"
)

// newTestCache returns a Redis cache backed by an in-process miniredis, plus the
// server so tests can inspect the keyspace or take it down.
func newTestCache(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	c, err := NewRedis("redis://"+srv.Addr()+"/0", time.Minute, logger.New(logger.LevelError), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, srv
}

// firstPage is what every pre-paging test implicitly asked for.
var firstPage = Paging{Page: 1, Size: 20}

func TestKeyGrammar(t *testing.T) {
	eventID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	status := "PAID"

	tests := []struct {
		name       string
		key        Key
		wantPrefix string
		wantGenKey string
	}{
		{
			name:       "global scope, no parameters",
			key:        EventsPublicKey(),
			wantPrefix: "list:events_public:-:g",
			wantGenKey: "gen:events",
		},
		{
			name:       "per-event scope embeds the id in both keys",
			key:        TicketTypesPublicKey(eventID),
			wantPrefix: "list:ticket_types_public:" + eventID.String() + ":-:g",
			wantGenKey: "gen:event:" + eventID.String(),
		},
		{
			name:       "filtered admin list renders its fingerprint",
			key:        OrdersAdminKey(&status, &eventID, firstPage),
			wantPrefix: "list:orders_admin:st=PAID:ev=" + eventID.String() + ":p=1:n=20:g",
			wantGenKey: "gen:orders",
		},
		{
			name:       "absent filters render as _, never as empty",
			key:        OrdersAdminKey(nil, nil, firstPage),
			wantPrefix: "list:orders_admin:st=_:ev=_:p=1:n=20:g",
			wantGenKey: "gen:orders",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantPrefix, tc.key.EntryPrefix())
			require.Equal(t, tc.wantGenKey, tc.key.GenerationKey())
		})
	}
}

// Fingerprints must be injective: two different filter combinations must never
// collide, or one admin's filtered view would serve another's rows.
func TestFingerprintsAreInjective(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	paid := "PAID"
	pending := "PENDING"

	seen := map[string]bool{}
	for _, k := range []Key{
		OrdersAdminKey(nil, nil, firstPage),
		OrdersAdminKey(&paid, nil, firstPage),
		OrdersAdminKey(&pending, nil, firstPage),
		OrdersAdminKey(nil, &a, firstPage),
		OrdersAdminKey(&paid, &a, firstPage),
		OrdersAdminKey(&paid, &b, firstPage),
		AttendeesAdminKey(nil, nil, firstPage),
		AttendeesAdminKey(&a, nil, firstPage),
		AttendeesAdminKey(nil, &a, firstPage),
		AttendeesAdminKey(&a, &b, firstPage),
	} {
		p := k.EntryPrefix()
		require.False(t, seen[p], "collision on %s", p)
		seen[p] = true
	}
}

// Fingerprints must be stable: the same parameters must always produce the same
// key, or every request would miss.
func TestFingerprintsAreStable(t *testing.T) {
	id := uuid.New()
	status := "PAID"
	for i := 0; i < 50; i++ {
		require.Equal(t,
			OrdersAdminKey(&status, &id, firstPage).EntryPrefix(),
			OrdersAdminKey(&status, &id, firstPage).EntryPrefix())
	}
}

func TestMissingGenerationReadsAsZero(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	// Nothing has ever been written, so there is no counter at all.
	require.NoError(t, c.Set(ctx, k, []byte(`["a"]`)))

	// It landed under generation 0, not under an error or a blank suffix.
	require.True(t, srv.Exists("list:events_public:-:g0"))

	raw, ok, err := c.Get(ctx, k)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, `["a"]`, string(raw))
}

func TestInvalidateOrphansPriorEntries(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	require.NoError(t, c.Set(ctx, k, []byte(`["before"]`)))
	_, ok, _ := c.Get(ctx, k)
	require.True(t, ok, "precondition: the entry is readable")

	require.NoError(t, c.Invalidate(ctx, Events()))

	_, ok, err := c.Get(ctx, k)
	require.NoError(t, err)
	require.False(t, ok, "the pre-invalidation entry must be unreachable")

	// The old key still physically exists — it is orphaned, not deleted, and TTL
	// plus allkeys-lru reclaim it. That is the whole point of the design: one
	// INCR invalidates an unbounded number of derived entries.
	require.True(t, srv.Exists("list:events_public:-:g0"))

	// A fresh write after the bump lands under the new generation.
	require.NoError(t, c.Set(ctx, k, []byte(`["after"]`)))
	require.True(t, srv.Exists("list:events_public:-:g1"))
	raw, ok, _ := c.Get(ctx, k)
	require.True(t, ok)
	require.Equal(t, `["after"]`, string(raw))
}

// Scoping: one event's writes must not disturb another event's entries (FR-009).
func TestInvalidationIsScoped(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	eventA, eventB := uuid.New(), uuid.New()
	keyA := TicketTypesPublicKey(eventA)
	keyB := TicketTypesPublicKey(eventB)

	require.NoError(t, c.Set(ctx, keyA, []byte(`["a"]`)))
	require.NoError(t, c.Set(ctx, keyB, []byte(`["b"]`)))

	require.NoError(t, c.Invalidate(ctx, Event(eventA)))

	_, okA, _ := c.Get(ctx, keyA)
	require.False(t, okA, "event A's entry must be gone")

	rawB, okB, _ := c.Get(ctx, keyB)
	require.True(t, okB, "event B's entry must survive")
	require.Equal(t, `["b"]`, string(rawB))
}

// A single orders-scope bump must reach every filter variant at once (FR-010) —
// this is the property that makes generation counters worth the indirection.
func TestOneBumpInvalidatesEveryFilterVariant(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	id := uuid.New()
	paid := "PAID"
	variants := []Key{
		OrdersAdminKey(nil, nil, firstPage),
		OrdersAdminKey(&paid, nil, firstPage),
		OrdersAdminKey(nil, &id, firstPage),
		OrdersAdminKey(&paid, &id, firstPage),
		AttendeesAdminKey(nil, &id, firstPage),
	}
	for _, k := range variants {
		require.NoError(t, c.Set(ctx, k, []byte(`["warm"]`)))
	}
	for _, k := range variants {
		_, ok, _ := c.Get(ctx, k)
		require.True(t, ok, "precondition: all variants warm")
	}

	require.NoError(t, c.Invalidate(ctx, Orders()))

	for _, k := range variants {
		_, ok, _ := c.Get(ctx, k)
		require.False(t, ok, "variant %s must be invalidated", k.EntryPrefix())
	}
}

// An empty list is a legitimate cached value — an event with no packages must not
// re-query on every request. "Key absent" and "key holds []" are different states.
func TestEmptyListIsCachedNotTreatedAsMiss(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	k := PackagesPublicKey(uuid.New())

	_, ok, _ := c.Get(ctx, k)
	require.False(t, ok, "nothing stored yet")

	require.NoError(t, c.Set(ctx, k, []byte(`[]`)))

	raw, ok, err := c.Get(ctx, k)
	require.NoError(t, err)
	require.True(t, ok, "an empty list must read back as a HIT, not a miss")
	require.Equal(t, `[]`, string(raw))
}

func TestScopeSetDeduplicates(t *testing.T) {
	id := uuid.New()
	s := NewScopeSet()
	s.Add(Events(), Events(), Event(id), Event(id), Orders())
	require.Equal(t, 3, s.Len())
	require.Len(t, s.Slice(), 3)
}

func TestFlushAllClearsEntriesAndCounters(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	require.NoError(t, c.Set(ctx, k, []byte(`["a"]`)))
	require.NoError(t, c.Invalidate(ctx, Events()))
	require.NoError(t, c.Set(ctx, k, []byte(`["b"]`)))

	n, err := c.FlushAll(ctx)
	require.NoError(t, err)
	require.Positive(t, n)

	require.Empty(t, srv.Keys(), "entries and generation counters go together")

	// And the cache is immediately usable again from a clean slate.
	_, ok, err := c.Get(ctx, k)
	require.NoError(t, err)
	require.False(t, ok)
}
