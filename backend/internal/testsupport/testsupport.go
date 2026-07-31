// Package testsupport provides the shared Postgres fixture for repository-level
// tests. Tests that need a database call RequirePool, which skips the test when
// TEST_DATABASE_URL is unset so the unit suite still runs on a bare checkout.
package testsupport

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// Pool aliases the pgx pool so test helper signatures stay short.
type Pool = pgxpool.Pool

// DiscardLogger returns a logger that swallows output, keeping test runs readable.
func DiscardLogger() *logger.Logger {
	return logger.NewWithWriter(io.Discard, logger.LevelError)
}

// RequirePool returns a pool against the test database, truncating every table
// first so each test starts from a known-empty schema.
func RequirePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database-backed test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	Truncate(t, pool)
	return pool
}

// Truncate empties every table. CASCADE handles the FK graph in one statement.
func Truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE tickets, payments, attendees, order_items, orders, ticket_types, events, admins CASCADE`)
	require.NoError(t, err)
}

// Event describes a seeded event row.
type Event struct {
	ID     uuid.UUID
	Name   string
	Slug   string
	Status string
}

// SeedEvent inserts an event and returns it. Dates default to a window around now.
func SeedEvent(t *testing.T, pool *pgxpool.Pool, slug, status string) Event {
	t.Helper()

	var id uuid.UUID
	name := "Event " + slug
	err := pool.QueryRow(context.Background(), `
		INSERT INTO events (name, slug, description, venue, address, start_date, end_date, banner_url, status)
		VALUES ($1, $2, 'A test event', 'Test Venue', 'Test Address', now() + interval '30 days',
		        now() + interval '31 days', 'https://cdn.example.com/banner.png', $3)
		RETURNING id`, name, slug, status).Scan(&id)
	require.NoError(t, err)

	return Event{ID: id, Name: name, Slug: slug, Status: status}
}

// TicketType describes a seeded ticket_types row.
type TicketType struct {
	ID      uuid.UUID
	EventID uuid.UUID
	Name    string
	Price   decimal.Decimal
	Quota   int32
}

// SeedTicketType inserts a ticket type whose sales window is currently open.
func SeedTicketType(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, name string, price string, quota int32) TicketType {
	t.Helper()

	amount, err := decimal.NewFromString(price)
	require.NoError(t, err)

	var id uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO ticket_types (event_id, name, price, quota, sales_start, sales_end)
		VALUES ($1, $2, $3, $4, now() - interval '1 day', now() + interval '29 days')
		RETURNING id`, eventID, name, amount, quota).Scan(&id)
	require.NoError(t, err)

	return TicketType{ID: id, EventID: eventID, Name: name, Price: amount, Quota: quota}
}

// SeedTicketTypeWindow inserts a ticket type with an explicit sales window, used to
// exercise the not-yet-open and already-closed cases.
func SeedTicketTypeWindow(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, name string, quota int32, start, end time.Time) TicketType {
	t.Helper()

	var id uuid.UUID
	price := decimal.NewFromInt(100000)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO ticket_types (event_id, name, price, quota, sales_start, sales_end)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, eventID, name, price, quota, start, end).Scan(&id)
	require.NoError(t, err)

	return TicketType{ID: id, EventID: eventID, Name: name, Price: price, Quota: quota}
}

// Order describes a seeded orders row.
type Order struct {
	ID          uuid.UUID
	OrderNumber string
	BuyerEmail  string
	Status      string
}

// SeedOrder inserts an order in the given status.
func SeedOrder(t *testing.T, pool *pgxpool.Pool, orderNumber, status string) Order {
	t.Helper()

	var id uuid.UUID
	email := "buyer@example.com"
	err := pool.QueryRow(context.Background(), `
		INSERT INTO orders (order_number, buyer_name, buyer_email, buyer_phone, total_amount, status)
		VALUES ($1, 'Test Buyer', $2, '+628123456789', 250000, $3)
		RETURNING id`, orderNumber, email, status).Scan(&id)
	require.NoError(t, err)

	return Order{ID: id, OrderNumber: orderNumber, BuyerEmail: email, Status: status}
}

// SeedOrderItem inserts an order_items row linking an order to a ticket type.
func SeedOrderItem(t *testing.T, pool *pgxpool.Pool, orderID, ticketTypeID uuid.UUID, quantity int32) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO order_items (order_id, ticket_type_id, quantity, price)
		VALUES ($1, $2, $3, 100000)`, orderID, ticketTypeID, quantity)
	require.NoError(t, err)
}

// SeedAttendee inserts an attendee row and returns its id.
func SeedAttendee(t *testing.T, pool *pgxpool.Pool, orderID, ticketTypeID uuid.UUID, name, email string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO attendees (order_id, ticket_type_id, name, email)
		VALUES ($1, $2, $3, $4) RETURNING id`, orderID, ticketTypeID, name, email).Scan(&id)
	require.NoError(t, err)
	return id
}

// SeedTicket inserts a ticket row in the given status.
func SeedTicket(t *testing.T, pool *pgxpool.Pool, code string, orderID, attendeeID uuid.UUID, status string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tickets (ticket_code, order_id, attendee_id, status)
		VALUES ($1, $2, $3, $4) RETURNING id`, code, orderID, attendeeID, status).Scan(&id)
	require.NoError(t, err)
	return id
}

// SeedAdmin inserts an admin with the given bcrypt hash.
func SeedAdmin(t *testing.T, pool *pgxpool.Pool, email, passwordHash string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO admins (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash).Scan(&id)
	require.NoError(t, err)
	return id
}

// QuotaOf reads a ticket type's current remaining quota.
func QuotaOf(t *testing.T, pool *pgxpool.Pool, ticketTypeID uuid.UUID) int32 {
	t.Helper()

	var quota int32
	err := pool.QueryRow(context.Background(),
		`SELECT quota FROM ticket_types WHERE id = $1`, ticketTypeID).Scan(&quota)
	require.NoError(t, err)
	return quota
}

// OrderStatusOf reads an order's current status.
func OrderStatusOf(t *testing.T, pool *pgxpool.Pool, orderID uuid.UUID) string {
	t.Helper()

	var status string
	err := pool.QueryRow(context.Background(),
		`SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status)
	require.NoError(t, err)
	return status
}
