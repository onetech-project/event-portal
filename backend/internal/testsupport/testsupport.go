// Package testsupport provides the shared Postgres fixture for repository-level
// tests. Tests that need a database call RequirePool, which skips the test when
// TEST_DATABASE_URL is unset so the unit suite still runs on a bare checkout.
package testsupport

import (
	"context"
	"encoding/json"
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
		`TRUNCATE tickets, payments, attendees, order_fees, order_items, orders, package_tickets, packages, ticket_types, events, admins, fees CASCADE`)
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

// SeedTicketType inserts a ticket type whose sales window is currently open. Its
// event (admission) window matches SeedEvent's +30d/+31d, so containment holds.
func SeedTicketType(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, name string, price string, quota int32) TicketType {
	t.Helper()

	amount, err := decimal.NewFromString(price)
	require.NoError(t, err)

	var id uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO ticket_types (event_id, name, price, quota, sales_start, sales_end, event_start, event_end)
		VALUES ($1, $2, $3, $4, now() - interval '1 day', now() + interval '29 days',
		        now() + interval '30 days', now() + interval '31 days')
		RETURNING id`, eventID, name, amount, quota).Scan(&id)
	require.NoError(t, err)

	return TicketType{ID: id, EventID: eventID, Name: name, Price: amount, Quota: quota}
}

// SeedRegistrationTicketType inserts a REGISTRATION-ONLY ticket type (spec 022):
// obtained by registering, never by buying. Priced at zero by convention — the
// column is deliberately independent of price (FR-003), so the price here is a
// realistic default rather than something the flag implies.
func SeedRegistrationTicketType(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, name string, quota int32) TicketType {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO ticket_types (event_id, name, price, quota, sales_start, sales_end,
		                          event_start, event_end, is_visible)
		VALUES ($1, $2, 0, $3, now() - interval '1 day', now() + interval '29 days',
		        now() + interval '30 days', now() + interval '31 days', FALSE)
		RETURNING id`, eventID, name, quota).Scan(&id)
	require.NoError(t, err)

	return TicketType{ID: id, EventID: eventID, Name: name, Price: decimal.Zero, Quota: quota}
}

// SeedTicketTypeWindow inserts a ticket type with an explicit sales window, used to
// exercise the not-yet-open and already-closed cases.
func SeedTicketTypeWindow(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, name string, quota int32, start, end time.Time) TicketType {
	t.Helper()

	var id uuid.UUID
	price := decimal.NewFromInt(100000)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO ticket_types (event_id, name, price, quota, sales_start, sales_end, event_start, event_end)
		VALUES ($1, $2, $3, $4, $5, $6, now() + interval '30 days', now() + interval '31 days')
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
		INSERT INTO orders (order_number, buyer_name, buyer_email, buyer_phone, total_amount, status_id)
		VALUES ($1, 'Test Buyer', $2, '+628123456789', 250000,
		        (SELECT id FROM order_statuses WHERE name = $3))
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
		`SELECT os.name FROM orders o
		 JOIN order_statuses os ON os.id = o.status_id
		 WHERE o.id = $1`, orderID).Scan(&status)
	require.NoError(t, err)
	return status
}

// Package describes a seeded packages row.
type Package struct {
	ID       uuid.UUID
	EventID  uuid.UUID
	Name     string
	Price    decimal.Decimal
	IsActive bool
}

// SeedPackage inserts a package with an open sales window and returns it.
func SeedPackage(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, name string, price string, isActive bool) Package {
	t.Helper()

	amount, err := decimal.NewFromString(price)
	require.NoError(t, err)

	var id uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO packages (event_id, name, price, sales_start, sales_end, is_active)
		VALUES ($1, $2, $3, now() - interval '1 day', now() + interval '29 days', $4)
		RETURNING id`, eventID, name, amount, isActive).Scan(&id)
	require.NoError(t, err)

	return Package{ID: id, EventID: eventID, Name: name, Price: amount, IsActive: isActive}
}

// SeedPackageTicket inserts a package_tickets junction row linking a package to
// a ticket type with the given quantity per unit.
func SeedPackageTicket(t *testing.T, pool *pgxpool.Pool, packageID, ticketTypeID, eventID uuid.UUID, quantityPerUnit int32) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO package_tickets (package_id, ticket_type_id, event_id, quantity)
		VALUES ($1, $2, $3, $4)`, packageID, ticketTypeID, eventID, quantityPerUnit)
	require.NoError(t, err)
}

// SeedOrderItemPackage inserts an order_items row for a package line.
func SeedOrderItemPackage(t *testing.T, pool *pgxpool.Pool, orderID, packageID uuid.UUID, quantity int32, price decimal.Decimal) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO order_items (order_id, package_id, quantity, price)
		VALUES ($1, $2, $3, $4)`, orderID, packageID, quantity, price)
	require.NoError(t, err)
}

// SeedAttendeeWithPackage inserts an attendee with a package origin and returns
// its id.
func SeedAttendeeWithPackage(t *testing.T, pool *pgxpool.Pool, orderID, ticketTypeID uuid.UUID, packageID uuid.NullUUID, name, email string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO attendees (order_id, ticket_type_id, package_id, name, email)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, orderID, ticketTypeID, packageID, name, email).Scan(&id)
	require.NoError(t, err)
	return id
}

// PackageAvailableUnits reads a package's derived available units using the
// same query the public API uses, for integration test assertions.
func PackageAvailableUnits(t *testing.T, pool *pgxpool.Pool, packageID uuid.UUID) int32 {
	t.Helper()

	var units int32
	err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(MIN(tt.quota / pt.quantity), 0)::int
		FROM package_tickets pt
		JOIN ticket_types tt ON tt.id = pt.ticket_type_id
		WHERE pt.package_id = $1`, packageID).Scan(&units)
	require.NoError(t, err)
	return units
}

// UnwrapData asserts a response rides the {code, message, data} envelope
// (specs/008) and returns the raw data payload for the caller to decode into
// its typed DTO. Success responses always carry code 200000.
func UnwrapData(t *testing.T, body []byte) json.RawMessage {
	t.Helper()
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.Equal(t, 200000, envelope.Code, "success envelope code")
	return envelope.Data
}

// SeedEventTerms authors a Terms & Conditions document for an event (spec 008)
// and returns its id. Booking is refused for events without one.
func SeedEventTerms(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID, content string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO event_terms (event_id, content) VALUES ($1, $2)
		ON CONFLICT (event_id) DO UPDATE SET content = EXCLUDED.content, updated_at = now()
		RETURNING id`, eventID, content).Scan(&id)
	require.NoError(t, err)
	return id
}

// SeedFee inserts one active fee master row (value is a decimal string, e.g.
// "11.00" for an 11% PERCENT fee or "1200.00" for a FIXED amount).
func SeedFee(t *testing.T, pool *pgxpool.Pool, name, feeType, value string, position int) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		`INSERT INTO fees (name, fee_type, value, position) VALUES ($1, $2, $3::numeric, $4)`,
		name, feeType, value, position)
	require.NoError(t, err)
}
