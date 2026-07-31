package event_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/db"
)

func TestListPublishedEventsReturnsOnlyPublished(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	published := testsupport.SeedEvent(t, pool, "published-one", "PUBLISHED")
	testsupport.SeedEvent(t, pool, "draft-one", "DRAFT")
	testsupport.SeedEvent(t, pool, "completed-one", "COMPLETED")

	events, err := repo.ListPublishedEvents(ctx)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, published.ID, events[0].ID)
	assert.Equal(t, "published-one", events[0].Slug)
}

func TestGetPublishedEventBySlugReturnsTheEvent(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)

	seeded := testsupport.SeedEvent(t, pool, "jazz-night", "PUBLISHED")

	got, err := repo.GetPublishedEventBySlug(context.Background(), "jazz-night")

	require.NoError(t, err)
	assert.Equal(t, seeded.ID, got.ID)
	assert.Equal(t, "Test Venue", got.Venue)
	require.NotNil(t, got.BannerURL)
	assert.Equal(t, "https://cdn.example.com/banner.png", *got.BannerURL)
}

func TestGetPublishedEventBySlugHidesUnpublishedEvents(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	testsupport.SeedEvent(t, pool, "secret-draft", "DRAFT")

	_, err := repo.GetPublishedEventBySlug(context.Background(), "secret-draft")

	assert.ErrorIs(t, err, event.ErrNotFound)
}

func TestGetPublishedEventBySlugReportsUnknownSlug(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)

	_, err := repo.GetPublishedEventBySlug(context.Background(), "no-such-slug")

	assert.ErrorIs(t, err, event.ErrNotFound)
}

func TestListTicketTypesByEventIDReturnsQuotaAsRemaining(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)

	seeded := testsupport.SeedEvent(t, pool, "with-types", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, seeded.ID, "VIP", "500000.00", 10)
	testsupport.SeedTicketType(t, pool, seeded.ID, "Regular", "150000.00", 100)

	types, err := repo.ListTicketTypesByEventID(context.Background(), seeded.ID)

	require.NoError(t, err)
	require.Len(t, types, 2)
	// Ordered by price ascending.
	assert.Equal(t, "Regular", types[0].Name)
	assert.Equal(t, "150000.00", types[0].Price.StringFixed(2))
	assert.Equal(t, int32(100), types[0].Quota)
	assert.Equal(t, "VIP", types[1].Name)
}

// --- Quota (EventProvider) ------------------------------------------------

func TestCheckAndDeductQuotaDecrementsRemainingQuota(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "deduct", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 10)

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		return repo.CheckAndDeductQuota(ctx, tx, tt.ID, 3)
	})

	require.NoError(t, err)
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, pool, tt.ID))
}

func TestCheckAndDeductQuotaRejectsMoreThanRemaining(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "oversell", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 2)

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		return repo.CheckAndDeductQuota(ctx, tx, tt.ID, 3)
	})

	assert.ErrorIs(t, err, event.ErrInsufficientQuota)
	assert.Equal(t, int32(2), testsupport.QuotaOf(t, pool, tt.ID), "quota must be untouched")
}

func TestCheckAndDeductQuotaIsAtomicUnderConcurrency(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "race", "PUBLISHED")
	// 10 buyers race for 5 seats, 1 seat each: exactly 5 may win.
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 5)

	const buyers = 10
	var wg sync.WaitGroup
	results := make([]error, buyers)
	for i := range buyers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = db.InTx(ctx, pool, func(tx pgx.Tx) error {
				return repo.CheckAndDeductQuota(ctx, tx, tt.ID, 1)
			})
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		} else {
			assert.ErrorIs(t, err, event.ErrInsufficientQuota)
		}
	}
	assert.Equal(t, 5, succeeded, "exactly the available quota may be sold")
	assert.Equal(t, int32(0), testsupport.QuotaOf(t, pool, tt.ID))
}

func TestCheckAndDeductQuotaReportsUnknownTicketType(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "unknown-tt", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 5)
	missing := ev.ID // a valid UUID that is not a ticket type id
	_ = tt

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		return repo.CheckAndDeductQuota(ctx, tx, missing, 1)
	})

	assert.ErrorIs(t, err, event.ErrInsufficientQuota,
		"a missing row is indistinguishable from no quota and must not 500")
}

func TestRestoreQuotaAddsBackTheReservedAmount(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "restore", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 10)

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := repo.CheckAndDeductQuota(ctx, tx, tt.ID, 4); err != nil {
			return err
		}
		return repo.RestoreQuota(ctx, tx, tt.ID, 4)
	})

	require.NoError(t, err)
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, pool, tt.ID))
}

func TestDeductRollsBackWithItsTransaction(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "rollback", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 10)

	err := db.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := repo.CheckAndDeductQuota(ctx, tx, tt.ID, 4); err != nil {
			return err
		}
		return assert.AnError // abort after deducting
	})

	require.Error(t, err)
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, pool, tt.ID),
		"quota deduction must not survive a rolled-back checkout")
}

func TestGetTicketTypeByIDReturnsSalesWindowAndPrice(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)

	ev := testsupport.SeedEvent(t, pool, "sales-window", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "175500.00", 10)

	got, err := repo.GetTicketTypeByID(context.Background(), nil, tt.ID)

	require.NoError(t, err)
	assert.Equal(t, "175500.00", got.Price.StringFixed(2))
	assert.Equal(t, ev.ID, got.EventID)
	assert.True(t, got.SalesStart.Before(got.SalesEnd))
}

func TestGetTicketTypeByIDReportsMissingRow(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := event.NewRepository(pool)
	ev := testsupport.SeedEvent(t, pool, "missing-tt", "PUBLISHED")

	_, err := repo.GetTicketTypeByID(context.Background(), nil, ev.ID)

	assert.ErrorIs(t, err, event.ErrNotFound)
}
