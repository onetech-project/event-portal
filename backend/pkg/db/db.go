// Package db owns the PostgreSQL connection pool and the transaction helper every
// domain uses to satisfy Constitution Principle IV.
package db

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/exaring/otelpgx"
	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool builds the shared pgx pool and registers the shopspring/decimal codec on
// every connection, so NUMERIC columns (prices, totals) round-trip as exact decimals
// rather than floats.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, errors.New("database DSN is empty")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database DSN: %w", err)
	}

	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		pgxdecimal.Register(conn.TypeMap())
		return nil
	}

	// Every query becomes a span under the request that issued it, so a slow
	// checkout can be attributed to a specific statement rather than guessed at.
	// This is a no-op when tracing is disabled: the global provider is then a
	// no-op provider.
	cfg.ConnConfig.Tracer = otelpgx.NewTracer(
		otelpgx.WithTrimSQLInSpanName(),
	)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Beginner is the subset of the pool used to open transactions. Services depend on
// this rather than *pgxpool.Pool so they can be exercised with a fake in tests.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type inTxKey struct{}
type hooksKey struct{}

// afterCommitHooks collects work that must run once the transaction commits, and
// must not run at all if it does not.
type afterCommitHooks struct {
	mu  sync.Mutex
	fns []func(context.Context)
}

func (h *afterCommitHooks) add(fn func(context.Context)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fns = append(h.fns, fn)
}

func (h *afterCommitHooks) run(ctx context.Context) {
	h.mu.Lock()
	fns := h.fns
	h.fns = nil
	h.mu.Unlock()
	for _, fn := range fns {
		fn(ctx)
	}
}

// InTransaction reports whether ctx belongs to a transaction opened by InTx.
//
// It exists so that infrastructure which must NOT be touched while a row lock is
// held — the read cache above all — can refuse rather than quietly serialise
// every concurrent writer behind a network round-trip. See Principle IV's
// no-network-call rule and Principle VII's transaction-boundary rule; the reason
// is the same in both cases, and it is the single most damaging mistake available
// in this area.
func InTransaction(ctx context.Context) bool {
	v, _ := ctx.Value(inTxKey{}).(bool)
	return v
}

// AfterCommit registers fn to run after the surrounding transaction commits,
// with the transaction marker already cleared so fn may use infrastructure that
// InTransaction guards.
//
// Outside a transaction fn runs immediately, so callers write the same line
// either way and no path has to ask which context it is on.
//
// fn does NOT run when the transaction rolls back or panics. That is the whole
// point: work registered here is work that is only correct once the write is
// durable.
func AfterCommit(ctx context.Context, fn func(context.Context)) {
	if h, ok := ctx.Value(hooksKey{}).(*afterCommitHooks); ok {
		h.add(fn)
		return
	}
	fn(ctx)
}

// InTx runs fn inside a single transaction, committing on success and rolling back
// on any error or panic. Every multi-statement write in this codebase goes through
// it so no path can accidentally half-commit (Constitution Principle IV).
//
// fn receives a derived context, not the caller's. It carries the transaction
// marker InTransaction reports on, and the registry AfterCommit appends to. Use
// it for every call made inside the transaction — passing the outer context
// instead silently escapes both guarantees.
func InTx(ctx context.Context, b Beginner, fn func(ctx context.Context, tx pgx.Tx) error) (err error) {
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	hooks := &afterCommitHooks{}
	txCtx := context.WithValue(ctx, hooksKey{}, hooks)
	txCtx = context.WithValue(txCtx, inTxKey{}, true)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = fn(txCtx, tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	// Only now, and on the caller's context: the marker is gone, so hooks may do
	// what they were forbidden from doing a moment ago.
	hooks.run(ctx)
	return nil
}
