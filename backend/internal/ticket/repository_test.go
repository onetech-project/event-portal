package ticket_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/db"
)

type ticketFixture struct {
	repo     *ticket.Repository
	pool     *testsupport.Pool
	orderID  uuid.UUID
	attendee uuid.UUID
	event    testsupport.Event
	ttype    testsupport.TicketType
}

func newTicketFixture(t *testing.T) ticketFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	ev := testsupport.SeedEvent(t, pool, "gate-night", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	// This fixture models a gate, so the event is RUNNING: SeedEvent's default
	// +30d/+31d would make every ticket NOT_YET_VALID and turn the whole
	// validation suite into a test of the window check alone (spec 015).
	// Individual window scenarios override this.
	_, err := pool.Exec(context.Background(), `
		UPDATE events SET start_date = now() - interval '1 hour', end_date = now() + interval '1 hour'
		WHERE id = $1`, ev.ID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		UPDATE ticket_types SET event_start = now() - interval '1 hour', event_end = now() + interval '1 hour'
		WHERE id = $1`, tt.ID)
	require.NoError(t, err)
	ord := testsupport.SeedOrder(t, pool, "ORD-TICKETS", "PAID")
	attendee := testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Budi Santoso", "budi@example.com")

	return ticketFixture{
		repo:     ticket.NewRepository(pool),
		pool:     pool,
		orderID:  ord.ID,
		attendee: attendee,
		event:    ev,
		ttype:    tt,
	}
}

func TestCreateTicketStoresTheCodeAndLeavesTheQRURLNull(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()

	err := db.InTx(ctx, f.pool, func(ctx context.Context, tx pgx.Tx) error {
		return f.repo.CreateTicket(ctx, tx, "ABC234DEFG", f.orderID, f.attendee)
	})
	require.NoError(t, err)

	var status string
	var qrCodeURL *string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT status, qr_code_url FROM tickets WHERE ticket_code = $1`, "ABC234DEFG").
		Scan(&status, &qrCodeURL))

	assert.Equal(t, "ACTIVE", status)
	// FR-022: there is no object storage in this MVP; QR images are rendered on
	// demand from the code, so nothing is ever persisted here.
	assert.Nil(t, qrCodeURL)
}

func TestCreateTicketRejectsADuplicateCode(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()

	require.NoError(t, db.InTx(ctx, f.pool, func(ctx context.Context, tx pgx.Tx) error {
		return f.repo.CreateTicket(ctx, tx, "DUPCODE234", f.orderID, f.attendee)
	}))

	err := db.InTx(ctx, f.pool, func(ctx context.Context, tx pgx.Tx) error {
		return f.repo.CreateTicket(ctx, tx, "DUPCODE234", f.orderID, f.attendee)
	})

	assert.ErrorIs(t, err, ticket.ErrCodeTaken)
}

func TestGetDetailByCodeJoinsAttendeeTicketTypeAndEvent(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "GATE234ABC", f.orderID, f.attendee, "ACTIVE")

	detail, err := f.repo.GetDetailByCode(ctx, "GATE234ABC")

	require.NoError(t, err)
	assert.Equal(t, "GATE234ABC", detail.TicketCode)
	assert.Equal(t, "ACTIVE", detail.Status)
	assert.Equal(t, "Budi Santoso", detail.AttendeeName)
	assert.Equal(t, "Regular", detail.TicketTypeName)
	assert.Equal(t, f.event.Name, detail.EventName)
}

func TestGetDetailByCodeReportsAnUnknownCode(t *testing.T) {
	f := newTicketFixture(t)

	_, err := f.repo.GetDetailByCode(context.Background(), "NOSUCHCODE")

	assert.ErrorIs(t, err, ticket.ErrNotFound)
}

// The stored code is canonical, so the lookup is an exact match. A caller that
// forgets to normalize its input gets a miss, not a silent case-insensitive hit.
func TestGetDetailByCodeIsAnExactMatch(t *testing.T) {
	f := newTicketFixture(t)
	testsupport.SeedTicket(t, f.pool, "EXACT234AB", f.orderID, f.attendee, "ACTIVE")

	_, err := f.repo.GetDetailByCode(context.Background(), "exact234ab")

	assert.ErrorIs(t, err, ticket.ErrNotFound)
}

func TestMarkUsedFlipsAnActiveTicket(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "USEME234AB", f.orderID, f.attendee, "ACTIVE")

	applied, err := f.repo.MarkUsed(ctx, "USEME234AB")

	require.NoError(t, err)
	assert.True(t, applied)

	status, err := f.repo.GetStatusByCode(ctx, "USEME234AB")
	require.NoError(t, err)
	assert.Equal(t, "USED", status)
}

func TestMarkUsedIsRejectedTheSecondTime(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "ONCE234ABC", f.orderID, f.attendee, "ACTIVE")

	first, err := f.repo.MarkUsed(ctx, "ONCE234ABC")
	require.NoError(t, err)
	second, err := f.repo.MarkUsed(ctx, "ONCE234ABC")
	require.NoError(t, err)

	assert.True(t, first)
	assert.False(t, second, "a ticket may only be consumed once")
}

func TestMarkUsedDoesNotConsumeARevokedTicket(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "REVOKED234", f.orderID, f.attendee, "REVOKED")

	applied, err := f.repo.MarkUsed(ctx, "REVOKED234")

	require.NoError(t, err)
	assert.False(t, applied)

	status, err := f.repo.GetStatusByCode(ctx, "REVOKED234")
	require.NoError(t, err)
	assert.Equal(t, "REVOKED", status, "the guarded update must not overwrite a revoked ticket")
}

func TestMarkUsedRecordsTheTransitionTime(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "STAMP234AB", f.orderID, f.attendee, "ACTIVE")

	var before, after *string
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT updated_at::text FROM tickets WHERE ticket_code = $1`, "STAMP234AB").Scan(&before))

	_, err := f.repo.MarkUsed(ctx, "STAMP234AB")
	require.NoError(t, err)

	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT updated_at::text FROM tickets WHERE ticket_code = $1`, "STAMP234AB").Scan(&after))

	// SCHEMA.md is LOCKED and the tickets table has no used_at column, so
	// updated_at is what records when the ticket was consumed.
	assert.NotEqual(t, before, after)
}

func TestMarkUsedOnAnUnknownCodeAppliesNothing(t *testing.T) {
	f := newTicketFixture(t)

	applied, err := f.repo.MarkUsed(context.Background(), "GHOST234AB")

	require.NoError(t, err)
	assert.False(t, applied)
}

func TestGetStatusByCodeReportsAnUnknownCode(t *testing.T) {
	f := newTicketFixture(t)

	_, err := f.repo.GetStatusByCode(context.Background(), "GHOST234AB")

	assert.ErrorIs(t, err, ticket.ErrNotFound)
}

func TestListByOrderIDReturnsEveryTicketForTheOrder(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()
	second := testsupport.SeedAttendee(t, f.pool, f.orderID, f.ttype.ID, "Sari", "sari@example.com")

	testsupport.SeedTicket(t, f.pool, "AAA234BBBC", f.orderID, f.attendee, "ACTIVE")
	testsupport.SeedTicket(t, f.pool, "BBB234CCCD", f.orderID, second, "USED")

	tickets, err := f.repo.ListByOrderID(ctx, f.orderID)

	require.NoError(t, err)
	require.Len(t, tickets, 2)
	codes := []string{tickets[0].TicketCode, tickets[1].TicketCode}
	assert.ElementsMatch(t, []string{"AAA234BBBC", "BBB234CCCD"}, codes)
}

func TestCountByOrderID(t *testing.T) {
	f := newTicketFixture(t)
	ctx := context.Background()

	count, err := f.repo.CountByOrderID(ctx, f.orderID)
	require.NoError(t, err)
	assert.Zero(t, count)

	testsupport.SeedTicket(t, f.pool, "CNT234ABCD", f.orderID, f.attendee, "ACTIVE")

	count, err = f.repo.CountByOrderID(ctx, f.orderID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
