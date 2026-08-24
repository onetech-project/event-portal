package order_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/db"
)

// --- spec 023: the gender master list is served from the cache ---------------
//
// The trap these tests exist to catch: a cache that never populates and a cache
// that works perfectly return the SAME gender list. Asserting on the returned
// names proves nothing. Every test below therefore asserts on the mechanism —
// how many times the store was written, what the store was asked for, and
// whether a value only the store could have produced comes back.

// fakeLists is a map-backed cache.Lists that counts what it was asked to do.
// Deliberately not a mock of Redis: the point is to observe the domain's use of
// the interface, not to re-test pkg/cache.
type fakeLists struct {
	mu      sync.Mutex
	entries map[string][]byte
	gets    int
	sets    int
	setErr  error
	// invalidated records any scope the domain invalidated. It must stay empty
	// on every read path (FR-015c).
	invalidated []cache.Scope
}

func newFakeLists() *fakeLists {
	return &fakeLists{entries: map[string][]byte{}}
}

func (f *fakeLists) Get(_ context.Context, k cache.Key) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	v, ok := f.entries[k.EntryPrefix()]
	return v, ok, nil
}

func (f *fakeLists) Set(_ context.Context, k cache.Key, v []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	f.entries[k.EntryPrefix()] = v
	return nil
}

func (f *fakeLists) Invalidate(_ context.Context, scopes ...cache.Scope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidated = append(f.invalidated, scopes...)
	return nil
}

func (f *fakeLists) FlushAll(context.Context) (int64, error) { return 0, nil }
func (f *fakeLists) Ping(context.Context) error              { return nil }
func (f *fakeLists) Enabled() bool                           { return true }
func (f *fakeLists) Trusted() bool                           { return true }

func (f *fakeLists) counts() (gets, sets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.sets
}

// seed writes a value directly, bypassing the counters, so a test can plant
// something the database could never return.
func (f *fakeLists) seed(k cache.Key, v []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[k.EntryPrefix()] = v
}

// unreachableLists fails every operation the way a store that is down does:
// a miss plus an error, which Through must swallow.
type unreachableLists struct{ fakeLists }

func (u *unreachableLists) Get(context.Context, cache.Key) ([]byte, bool, error) {
	return nil, false, errors.New("store unreachable")
}

func (u *unreachableLists) Set(context.Context, cache.Key, []byte) error {
	return errors.New("store unreachable")
}

// cachedFixture is a registration fixture whose service reads through fc.
func cachedFixture(t *testing.T, fc cache.Lists) registrationFixture {
	t.Helper()
	f := newRegistrationFixture(t, 5)
	f.svc.WithCache(fc)
	return f
}

// FR-015a / SC-010: a miss must POPULATE, not merely read past the store.
//
// The second read is served a sentinel that no database row could produce, so a
// pass means the value provably came from the store. An implementation that
// reads through on every miss without storing satisfies every other test in this
// package and fails this one.
func TestGendersServedFromTheCacheOnceWarm(t *testing.T) {
	fc := newFakeLists()
	f := cachedFixture(t, fc)
	ctx := context.Background()

	first, err := f.svc.Genders(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, first, "the master list is seeded by migration")

	gets, sets := fc.counts()
	assert.Equal(t, 1, gets, "the first read consults the store")
	assert.Equal(t, 1, sets, "the miss populates it")

	// Plant a value the database cannot return.
	fc.seed(cache.GendersActiveKey(), []byte(`[{"id":99,"name":"SENTINEL"}]`))

	second, err := f.svc.Genders(ctx)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "SENTINEL", second[0].Name,
		"the second read must come from the store, not the database")

	_, sets = fc.counts()
	assert.Equal(t, 1, sets, "a hit must not write again")
}

// FR-002: the two projections are separate entries and must not be collapsed.
// Checkout resolves through the all-known list; the forms offer the active one.
func TestGenderProjectionsUseSeparateEntries(t *testing.T) {
	fc := newFakeLists()
	f := cachedFixture(t, fc)
	ctx := context.Background()

	_, err := f.svc.Genders(ctx) // active projection
	require.NoError(t, err)

	// Reach the all-known projection the way checkout does.
	require.NoError(t, f.svc.RegisterFree(ctx, f.ticket.ID, f.request()))

	fc.mu.Lock()
	defer fc.mu.Unlock()
	_, hasActive := fc.entries[cache.GendersActiveKey().EntryPrefix()]
	assert.True(t, hasActive, "the active projection is stored under its own key")
	assert.NotEqual(t,
		cache.GendersActiveKey().EntryPrefix(),
		cache.GendersAllKey().EntryPrefix(),
		"the two projections must never share an entry")
}

// FR-019 / SC-002: the cached and uncached paths must agree exactly.
func TestGendersAreIdenticalCachedAndUncached(t *testing.T) {
	uncached := newRegistrationFixture(t, 5)
	fromDB, err := uncached.svc.Genders(context.Background())
	require.NoError(t, err)

	fc := newFakeLists()
	cached := cachedFixture(t, fc)
	warm, err := cached.svc.Genders(context.Background())
	require.NoError(t, err)
	again, err := cached.svc.Genders(context.Background())
	require.NoError(t, err)

	assert.Equal(t, fromDB, warm, "a cold cached read equals the database read")
	assert.Equal(t, fromDB, again, "and so does a warm one, in the same order")
}

// FR-015b / SC-011: a store that rejects writes costs the next reader a query
// and costs this one nothing.
func TestGendersSurviveAStoreThatRejectsWrites(t *testing.T) {
	fc := newFakeLists()
	fc.setErr = errors.New("OOM command not allowed when used memory > maxmemory")
	f := cachedFixture(t, fc)
	ctx := context.Background()

	first, err := f.svc.Genders(ctx)
	require.NoError(t, err, "a failed store-back must never reach the caller")
	require.NotEmpty(t, first)

	second, err := f.svc.Genders(ctx)
	require.NoError(t, err)
	assert.Equal(t, first, second, "still correct, just not accelerated")

	_, sets := fc.counts()
	assert.Equal(t, 2, sets, "each miss retried the write; neither failed the request")
}

// FR-013 / SC-004: an unreachable store degrades latency and nothing else.
func TestGendersFallBackWhenTheStoreIsUnreachable(t *testing.T) {
	f := cachedFixture(t, &unreachableLists{})

	got, err := f.svc.Genders(context.Background())
	require.NoError(t, err, "no endpoint may fail because the cache is down")
	assert.NotEmpty(t, got)
}

// FR-015c: reading must never invalidate. Both projections share gen:master, so
// invalidating on a miss would orphan the sibling and the two would evict each
// other forever — while every functional assertion above still passed.
func TestGenderReadsNeverInvalidate(t *testing.T) {
	fc := newFakeLists()
	f := cachedFixture(t, fc)
	ctx := context.Background()

	_, err := f.svc.Genders(ctx)
	require.NoError(t, err)
	_, err = f.svc.Genders(ctx)
	require.NoError(t, err)

	fc.mu.Lock()
	defer fc.mu.Unlock()
	assert.Empty(t, fc.invalidated, "a read must not invalidate anything")
}

// FR-015: concurrent first readers collapse into one load.
func TestConcurrentColdGenderReadsCollapse(t *testing.T) {
	fc := newFakeLists()
	f := cachedFixture(t, fc)

	const readers = 8
	var wg sync.WaitGroup
	errs := make([]error, readers)
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Genders(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	_, sets := fc.counts()
	assert.LessOrEqual(t, sets, 2,
		"a cold burst must collapse into ~one load, not one per reader")
}

// Principle VII: a cache call inside an order-writing transaction would hold a
// row lock across a network round trip. None of the gender consumers does this
// today; this test is what stops a later refactor from inlining one into
// checkout's transaction and serialising every concurrent buyer.
func TestGenderReadInsideATransactionIsRefused(t *testing.T) {
	fc := newFakeLists()
	f := cachedFixture(t, fc)

	err := db.InTx(context.Background(), f.pool, func(ctx context.Context, _ pgx.Tx) error {
		_, gErr := f.svc.Genders(ctx)
		return gErr
	})

	require.Error(t, err, "a gender read inside a transaction must fail loudly")
	assert.ErrorIs(t, err, cache.ErrInTransaction)
}
