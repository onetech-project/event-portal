package cache

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// --- spec 023: the master-data surface --------------------------------------

// The two projections must render as two different entries.
//
// This is the assertion that matters most in the whole surface. A collision
// would serve the ACTIVE-only list where checkout expects the all-known one, and
// checkout would then refuse every retired gender a restored booking form
// carries (spec 011 FR-031) — a data-loss-shaped bug that no other test here
// would notice, because both keys would still return a perfectly valid list.
func TestGenderKeysAreDistinct(t *testing.T) {
	active, all := GendersActiveKey(), GendersAllKey()

	require.NotEqual(t, active.EntryPrefix(), all.EntryPrefix(),
		"the two projections must not share an entry")
	require.Equal(t, "list:genders_master:proj=active:g", active.EntryPrefix())
	require.Equal(t, "list:genders_master:proj=all:g", all.EntryPrefix())
}

// Both derive from one counter, so one bump orphans both — correct, because the
// migration that changes the table changes both projections.
func TestGenderKeysShareTheMasterGeneration(t *testing.T) {
	require.Equal(t, "gen:master", GendersActiveKey().GenerationKey())
	require.Equal(t, "gen:master", GendersAllKey().GenerationKey())
	require.Equal(t, "master", Master().String(),
		"the scope renders as a bounded constant; it is a metric label")
}

// The registry is the only way to build a Key, so a family absent from it cannot
// be cached at all. Missing this line would leave the surface undocumented to
// every consumer that reads Families.
func TestGendersMasterIsRegistered(t *testing.T) {
	require.Contains(t, Families, FamilyGendersMaster)
}

func TestInvalidatingMasterOrphansBothProjections(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	active, all := GendersActiveKey(), GendersAllKey()
	require.NoError(t, c.Set(ctx, active, []byte(`["FEMALE","MALE"]`)))
	require.NoError(t, c.Set(ctx, all, []byte(`["FEMALE","MALE","RETIRED"]`)))

	require.NoError(t, c.Invalidate(ctx, Master()))

	_, okActive, _ := c.Get(ctx, active)
	_, okAll, _ := c.Get(ctx, all)
	require.False(t, okActive, "the active projection must be orphaned")
	require.False(t, okAll, "the all-known projection must be orphaned too")
}

// The narrowest rule in spec 023 (FR-009), and the one a shortcut breaks.
//
// Reusing an existing scope instead of adding ScopeKindMaster would have been
// less code and would pass every other test in this file. It would also mean the
// startup invalidation — which runs on EVERY boot — discards the event catalogue
// and every admin order list, which are expensive to rebuild and were already
// correct. This test is what makes that shortcut fail.
func TestInvalidatingMasterLeavesEveryOtherScopeAlone(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	eventID := uuid.New()
	catalogue := EventsPublicKey()
	ticketTypes := TicketTypesPublicKey(eventID)
	orders := OrdersAdminKey(nil, nil, nil, firstPage)

	require.NoError(t, c.Set(ctx, catalogue, []byte(`["catalogue"]`)))
	require.NoError(t, c.Set(ctx, ticketTypes, []byte(`["types"]`)))
	require.NoError(t, c.Set(ctx, orders, []byte(`["orders"]`)))
	require.NoError(t, c.Set(ctx, GendersActiveKey(), []byte(`["FEMALE"]`)))

	require.NoError(t, c.Invalidate(ctx, Master()))

	for name, k := range map[string]Key{
		"events catalogue":   catalogue,
		"event ticket types": ticketTypes,
		"admin orders":       orders,
	} {
		_, ok, _ := c.Get(ctx, k)
		require.True(t, ok, "%s must survive a master-scope invalidation", name)
	}
}

// A miss must POPULATE, never invalidate (FR-015c).
//
// The two are opposite operations and conflating them is unusually damaging
// here: both projections share gen:master, so invalidating on a miss of one
// would orphan the other. The two would spend their lives evicting each other,
// the hit rate would sit near zero, and every functional test would still pass —
// a cache that looks correct and accelerates nothing.
func TestReadingDoesNotBumpTheMasterGeneration(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, GendersActiveKey(), []byte(`["FEMALE"]`)))

	// The counter is created lazily by Invalidate, never by Set: generation()
	// treats absence as zero. So it is legitimately ABSENT here, and "absent" is
	// the value this test has to be able to compare.
	generation := func() string {
		v, err := srv.Get("gen:master")
		if err != nil {
			return "<absent>"
		}
		return v
	}
	before := generation()
	require.Equal(t, "<absent>", before, "a write alone must not create the counter")

	// A hit, then a miss on the sibling projection: neither may bump.
	_, _, _ = c.Get(ctx, GendersActiveKey())
	_, ok, _ := c.Get(ctx, GendersAllKey())
	require.False(t, ok, "the sibling projection was never written")

	require.Equal(t, before, generation(), "reading must never move the generation")

	// And the counter DOES move when something legitimately invalidates, so the
	// assertion above is a real constraint rather than one nothing could break.
	require.NoError(t, c.Invalidate(ctx, Master()))
	require.NotEqual(t, before, generation(), "an invalidation must move it")
}

// FR-010: the operator refresh must clear this surface along with every other,
// so a direct database correction stays recoverable without a deployment.
//
// Verified rather than assumed. FlushAll uses FLUSHDB, which is surface-agnostic
// by construction — but "it obviously covers everything" is the kind of claim
// that stops being true the moment someone narrows it to a key pattern.
func TestOperatorFlushClearsTheMasterSurface(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, GendersActiveKey(), []byte(`["FEMALE"]`)))
	require.NoError(t, c.Set(ctx, GendersAllKey(), []byte(`["FEMALE","RETIRED"]`)))
	require.NoError(t, c.Invalidate(ctx, Master())) // create the counter too

	_, err := c.FlushAll(ctx)
	require.NoError(t, err)

	_, okActive, _ := c.Get(ctx, GendersActiveKey())
	_, okAll, _ := c.Get(ctx, GendersAllKey())
	require.False(t, okActive, "the flush must clear the active projection")
	require.False(t, okAll, "and the all-known projection")
}
