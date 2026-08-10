package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/db"
)

// --- The transaction-boundary invariant ------------------------------------
//
// The single most damaging mistake available in this area is issuing a cache
// command while an order-writing transaction holds the quota row lock: every
// concurrent buyer of that ticket type would serialise behind a network
// round-trip. Principle IV bans gateway calls inside the transaction for exactly
// this reason, and Principle VII extends it to the cache.
//
// A rule enforced only by code review erodes on the first hurried change, so it
// is enforced at runtime instead. These tests are what keep that true.

type fakeBeginner struct{ tx pgx.Tx }

func (f fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

type fakeTx struct {
	pgx.Tx
	committed  bool
	rolledBack bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTx) Rollback(context.Context) error { t.rolledBack = true; return nil }

func TestCacheRefusesToRunInsideATransaction(t *testing.T) {
	c, _ := newTestCache(t)
	tx := &fakeTx{}

	var getErr, setErr, invErr, flushErr error
	err := db.InTx(context.Background(), fakeBeginner{tx: tx}, func(ctx context.Context, _ pgx.Tx) error {
		_, _, getErr = c.Get(ctx, EventsPublicKey())
		setErr = c.Set(ctx, EventsPublicKey(), []byte(`[]`))
		invErr = c.Invalidate(ctx, Events())
		_, flushErr = c.FlushAll(ctx)
		return nil
	})
	require.NoError(t, err)

	require.ErrorIs(t, getErr, ErrInTransaction)
	require.ErrorIs(t, setErr, ErrInTransaction)
	require.ErrorIs(t, invErr, ErrInTransaction)
	require.ErrorIs(t, flushErr, ErrInTransaction)
}

func TestThroughRefusesToRunInsideATransaction(t *testing.T) {
	c, _ := newTestCache(t)
	tx := &fakeTx{}

	var loaded bool
	var throughErr error
	err := db.InTx(context.Background(), fakeBeginner{tx: tx}, func(ctx context.Context, _ pgx.Tx) error {
		_, throughErr = Through(ctx, c, EventsPublicKey(), func(context.Context) ([]string, error) {
			loaded = true
			return []string{"a"}, nil
		})
		return nil
	})
	require.NoError(t, err)
	require.ErrorIs(t, throughErr, ErrInTransaction)
	require.False(t, loaded, "it must fail before doing any work, not after")
}

func TestInTransactionIsFalseOutsideAndTrueInside(t *testing.T) {
	ctx := context.Background()
	require.False(t, db.InTransaction(ctx))

	require.NoError(t, db.InTx(ctx, fakeBeginner{tx: &fakeTx{}}, func(txCtx context.Context, _ pgx.Tx) error {
		require.True(t, db.InTransaction(txCtx))
		// The caller's own context is untouched, so nothing leaks outward.
		require.False(t, db.InTransaction(ctx))
		return nil
	}))
}

// --- After-commit hooks ------------------------------------------------------

func TestAfterCommitRunsOnlyOnCommit(t *testing.T) {
	ctx := context.Background()

	var ran int
	require.NoError(t, db.InTx(ctx, fakeBeginner{tx: &fakeTx{}}, func(txCtx context.Context, _ pgx.Tx) error {
		db.AfterCommit(txCtx, func(context.Context) { ran++ })
		require.Zero(t, ran, "the hook must not run while the transaction is open")
		return nil
	}))
	require.Equal(t, 1, ran)
}

func TestAfterCommitDoesNotRunOnRollback(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("write failed")

	var ran int
	err := db.InTx(ctx, fakeBeginner{tx: &fakeTx{}}, func(txCtx context.Context, _ pgx.Tx) error {
		db.AfterCommit(txCtx, func(context.Context) { ran++ })
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Zero(t, ran, "a rolled-back write must never invalidate: the data never changed")
}

func TestAfterCommitRunsImmediatelyOutsideATransaction(t *testing.T) {
	var ran int
	db.AfterCommit(context.Background(), func(context.Context) { ran++ })
	require.Equal(t, 1, ran, "callers write the same line whether or not they are in a transaction")
}

func TestInvalidateAfterCommitDefersUntilCommit(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()
	require.NoError(t, c.Set(ctx, k, []byte(`["warm"]`)))

	require.NoError(t, db.InTx(ctx, fakeBeginner{tx: &fakeTx{}}, func(txCtx context.Context, _ pgx.Tx) error {
		InvalidateAfterCommit(txCtx, c, Events())
		// Still readable: the write has not committed, so the cache is still right.
		_, ok, _ := c.Get(ctx, k)
		require.True(t, ok)
		return nil
	}))

	_, ok, _ := c.Get(ctx, k)
	require.False(t, ok, "invalidated once the transaction committed")
}

// --- Fail open ---------------------------------------------------------------

func TestReadsSucceedWhenTheStoreIsDown(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	require.NoError(t, c.Set(ctx, k, []byte(`["warm"]`)))
	srv.Close() // Redis is gone

	// The cache reports a miss and an error; it does NOT propagate a failure that
	// would surface to a guest as a 5xx.
	_, ok, err := c.Get(ctx, k)
	require.False(t, ok)
	require.Error(t, err, "the error is available for metrics")

	// Through absorbs it entirely and answers from the loader.
	got, err := Through(ctx, c, k, func(context.Context) ([]string, error) {
		return []string{"from the database"}, nil
	})
	require.NoError(t, err, "a cache outage must never fail a request")
	require.Equal(t, []string{"from the database"}, got)
}

func TestThroughReturnsLoaderErrorsUnchanged(t *testing.T) {
	c, _ := newTestCache(t)
	boom := errors.New("query failed")

	_, err := Through(context.Background(), c, EventsPublicKey(), func(context.Context) ([]string, error) {
		return nil, boom
	})
	require.ErrorIs(t, err, boom, "database failures are real failures and must surface")
}

// --- Distrust window ---------------------------------------------------------

func TestFailedInvalidationEntersDistrustAndBypassesReads(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	require.NoError(t, c.Set(ctx, k, []byte(`["stale"]`)))
	require.True(t, c.Trusted())

	// The write committed, then Redis vanished before the invalidation landed.
	// The cached entry is now provably wrong.
	srv.Close()
	require.Error(t, c.Invalidate(ctx, Events()))
	require.False(t, c.Trusted(), "entries are known-wrong; they must not be served")

	// Through therefore bypasses the cache entirely rather than risk serving them.
	got, err := Through(ctx, c, k, func(context.Context) ([]string, error) {
		return []string{"fresh"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"fresh"}, got)
}

func TestDistrustClearsItself(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	c.cooldown = 20 * time.Millisecond

	srv.Close()
	require.Error(t, c.Invalidate(ctx, Events()))
	require.False(t, c.Trusted())

	require.Eventually(t, c.Trusted, time.Second, 5*time.Millisecond,
		"recovery must need no operator action")
}

// The operator escape hatch doubles as the manual recovery from distrust: a
// successful flush proves Redis is reachable and leaves it empty, so there is
// nothing left to distrust.
func TestFlushAllClearsDistrust(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	c.distrustUntil.Store(time.Now().Add(time.Hour).UnixNano())
	require.False(t, c.Trusted())

	_, err := c.FlushAll(ctx)
	require.NoError(t, err)
	require.True(t, c.Trusted())
}

// --- Miss collapsing ---------------------------------------------------------

// A cold-cache burst must produce one database query, not one per waiting
// request (FR-017, SC-006).
func TestConcurrentMissesCollapseToOneLoad(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	k := TicketTypesPublicKey(uuid.New())

	var mu sync.Mutex
	loads := 0
	release := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = Through(ctx, c, k, func(context.Context) ([]string, error) {
				mu.Lock()
				loads++
				mu.Unlock()
				<-release // hold the flight open so the others pile up behind it
				return []string{"x"}, nil
			})
		}()
	}
	// Give the goroutines time to arrive before letting the first one finish.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	require.LessOrEqual(t, loads, 5, "100 concurrent misses must not become 100 queries")
}

// --- NoOp --------------------------------------------------------------------

func TestNoOpReproducesPreCacheBehaviour(t *testing.T) {
	ctx := context.Background()
	var c Lists = NoOp{}

	require.False(t, c.Enabled())
	require.True(t, c.Trusted())

	loads := 0
	for i := 0; i < 3; i++ {
		got, err := Through(ctx, c, EventsPublicKey(), func(context.Context) ([]string, error) {
			loads++
			return []string{"a"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"a"}, got)
	}
	require.Equal(t, 3, loads, "with caching off every call must reach the database")
}

// A cache outage must cost latency once, not on every request.
//
// This is a regression test for a real defect found during validation: with the
// default dial timeout and go-redis's 3 retries, every single request paid ~3.5s
// discovering Redis was down before falling through to Postgres. Failing open
// slowly is its own outage, so a connection-level failure opens a breaker and
// subsequent reads skip the store entirely until it closes.
func TestCacheOutageCostsLatencyOnceNotPerRequest(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	srv.Close()

	// First read discovers the outage and opens the breaker.
	_, _, _ = c.Get(ctx, k)

	// Subsequent reads must not touch the network at all.
	start := time.Now()
	for i := 0; i < 20; i++ {
		_, ok, err := c.Get(ctx, k)
		require.False(t, ok)
		require.NoError(t, err, "a short-circuited read is a plain miss, not an error")
	}
	elapsed := time.Since(start)

	require.Less(t, elapsed, 50*time.Millisecond,
		"20 reads during an outage must be effectively free, not 20 dial timeouts")
}

// Writes must be short-circuited too, for the same reason.
func TestCacheWritesAreSkippedWhileTheStoreIsUnreachable(t *testing.T) {
	c, srv := newTestCache(t)
	ctx := context.Background()
	k := EventsPublicKey()

	srv.Close()
	_ = c.Set(ctx, k, []byte(`["a"]`)) // opens the breaker

	start := time.Now()
	for i := 0; i < 20; i++ {
		require.NoError(t, c.Set(ctx, k, []byte(`["a"]`)),
			"a skipped write is not a failure the caller needs to hear about")
	}
	require.Less(t, time.Since(start), 50*time.Millisecond)
}
