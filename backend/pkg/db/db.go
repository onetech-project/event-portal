// Package db owns the PostgreSQL connection pool and the transaction helper every
// domain uses to satisfy Constitution Principle IV.
package db

import (
	"context"
	"errors"
	"fmt"

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

// InTx runs fn inside a single transaction, committing on success and rolling back
// on any error or panic. Every multi-statement write in this codebase goes through
// it so no path can accidentally half-commit (Constitution Principle IV).
func InTx(ctx context.Context, b Beginner, fn func(tx pgx.Tx) error) (err error) {
	tx, err := b.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
