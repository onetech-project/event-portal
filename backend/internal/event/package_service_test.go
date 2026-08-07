package event_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

var (
	pkgWindowStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pkgWindowEnd   = time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
)

func componentOf(ticketTypeID uuid.UUID, perUnit int32) map[string]any {
	return map[string]any{"ticket_type_id": ticketTypeID.String(), "quantity_per_unit": perUnit}
}

// validPackageBody is a create body for a package over the given components.
// Tests corrupt one field at a time from here.
func validPackageBody(eventID uuid.UUID, components ...map[string]any) map[string]any {
	return map[string]any{
		"event_id":    eventID.String(),
		"name":        "Day 1 and 2",
		"description": nil,
		"price":       "50000.00",
		"sales_start": pkgWindowStart.Format(time.RFC3339),
		"sales_end":   pkgWindowEnd.Format(time.RFC3339),
		"is_active":   true,
		"components":  components,
	}
}

func marshalBody(t *testing.T, m map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return raw
}

// T061 (FR-036): a package holds no inventory, so any quota-like field in the
// request body is rejected rather than silently ignored — ignoring it would train
// a client to believe packages keep stock.
func TestCreatePackageRejectsAnyQuotaLikeField(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "quota-reject", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 10)

	for _, key := range []string{"quota", "stock", "inventory", "remaining", "capacity"} {
		t.Run(key, func(t *testing.T) {
			body := validPackageBody(ev.ID, componentOf(tt.ID, 1))
			body[key] = 100

			_, err := svc.CreatePackage(context.Background(), marshalBody(t, body))

			appErr := appErrOf(t, err)
			assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
			assert.Equal(t, apperr.CodeValidation, appErr.Code)
		})
	}
}

// T062: every field-level rule is enforced — the valid body below succeeds, and
// each one-field corruption is rejected with 400.
func TestCreatePackageRejectsInvalidBodies(t *testing.T) {
	svc, pool := newAdminService(t)
	ev := testsupport.SeedEvent(t, pool, "invalid-bodies", "PUBLISHED")
	day1 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 10)
	day2 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 2", "30000.00", 10)

	valid := validPackageBody(ev.ID, componentOf(day1.ID, 1), componentOf(day2.ID, 1))
	created, err := svc.CreatePackage(context.Background(), marshalBody(t, valid))
	require.NoError(t, err, "the reference body must be accepted first")
	require.NotEmpty(t, created.ID)

	cases := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{"empty components", func(m map[string]any) { m["components"] = []any{} }},
		{"duplicate ticket", func(m map[string]any) {
			m["components"] = []any{componentOf(day1.ID, 1), componentOf(day1.ID, 2)}
		}},
		{"quantity_per_unit of zero", func(m map[string]any) {
			m["components"] = []any{componentOf(day1.ID, 0)}
		}},
		{"negative price", func(m map[string]any) { m["price"] = "-100.00" }},
		{"inverted sales window", func(m map[string]any) {
			m["sales_start"] = pkgWindowEnd.Format(time.RFC3339)
			m["sales_end"] = pkgWindowStart.Format(time.RFC3339)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validPackageBody(ev.ID, componentOf(day1.ID, 1))
			tc.mutate(body)

			_, err := svc.CreatePackage(context.Background(), marshalBody(t, body))

			appErr := appErrOf(t, err)
			assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		})
	}
}

// T064 (R-004): a composition edit under an open PENDING order is rejected with
// 409, because expiry reconstructs a hold from the current composition and would
// restore a different amount than was deducted. Field edits — name, price,
// window, status — never touch that reconstruction and always succeed.
func TestCompositionEditLockedWhilePendingOrderExists(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, pool, "composition-lock", "PUBLISHED")
	day1 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 10)
	day2 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 2", "30000.00", 10)
	day3 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 3", "30000.00", 10)
	pkg := testsupport.SeedPackage(t, pool, ev.ID, "Day 1+2", "50000.00", true)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, day1.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, day2.ID, ev.ID, 1)

	ord := testsupport.SeedOrder(t, pool, "ORD-LOCKED", "PENDING")
	testsupport.SeedOrderItemPackage(t, pool, ord.ID, pkg.ID, 1, decimal.NewFromInt(50000))
	testsupport.SeedAttendeeWithPackage(t, pool, ord.ID, day1.ID, uuid.NullUUID{UUID: pkg.ID, Valid: true}, "Budi", "budi@example.com")

	// Same composition, new name and price: allowed even under the open order.
	sameComposition := validPackageBody(ev.ID, componentOf(day1.ID, 1), componentOf(day2.ID, 1))
	sameComposition["name"] = "Day 1+2 (reprice)"
	sameComposition["price"] = "55000.00"
	updated, err := svc.UpdatePackage(ctx, pkg.ID, marshalBody(t, sameComposition))
	require.NoError(t, err)
	assert.Equal(t, "Day 1+2 (reprice)", updated.Name)

	// Swapping day2 for day3 is a composition change: locked with 409.
	changed := validPackageBody(ev.ID, componentOf(day1.ID, 1), componentOf(day3.ID, 1))
	_, err = svc.UpdatePackage(ctx, pkg.ID, marshalBody(t, changed))
	appErr := appErrOf(t, err)
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodePackageCompositionLocked, appErr.Code)

	// Once the order resolves the lock lifts: PAID orders never drift because
	// their hold is permanent.
	_, err = pool.Exec(ctx, "UPDATE orders SET status_id = (SELECT id FROM order_statuses WHERE name = 'PAID') WHERE id = $1", ord.ID)
	require.NoError(t, err)
	_, err = svc.UpdatePackage(ctx, pkg.ID, marshalBody(t, changed))
	require.NoError(t, err)
}

// T065 (FR-037): every delete that a package blocks returns a clean 400 naming
// the reason, never a raw constraint violation.
func TestDeleteGuardsReturnClean400s(t *testing.T) {
	svc, pool := newAdminService(t)
	ctx := context.Background()

	t.Run("ordered package", func(t *testing.T) {
		ev := testsupport.SeedEvent(t, pool, "del-pkg", "PUBLISHED")
		tt := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 10)
		pkg := testsupport.SeedPackage(t, pool, ev.ID, "Sold bundle", "50000.00", true)
		testsupport.SeedPackageTicket(t, pool, pkg.ID, tt.ID, ev.ID, 1)
		ord := testsupport.SeedOrder(t, pool, "ORD-DELPKG", "PENDING")
		testsupport.SeedOrderItemPackage(t, pool, ord.ID, pkg.ID, 1, decimal.NewFromInt(50000))

		err := svc.DeletePackage(ctx, pkg.ID)

		appErr := appErrOf(t, err)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, apperr.CodePackageHasOrders, appErr.Code)
		_, err = svc.GetPackageAdmin(ctx, pkg.ID)
		require.NoError(t, err, "the package must survive the rejected delete")
	})

	t.Run("ticket type used by a package", func(t *testing.T) {
		ev := testsupport.SeedEvent(t, pool, "del-tt", "PUBLISHED")
		tt := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 10)
		pkg := testsupport.SeedPackage(t, pool, ev.ID, "Depends on Day 1", "30000.00", true)
		testsupport.SeedPackageTicket(t, pool, pkg.ID, tt.ID, ev.ID, 1)

		err := svc.DeleteTicketType(ctx, tt.ID)

		appErr := appErrOf(t, err)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Contains(t, appErr.Message, pkg.Name,
			"the 400 names the packages the administrator must unpick")
	})

	t.Run("event with packages", func(t *testing.T) {
		ev := testsupport.SeedEvent(t, pool, "del-ev", "PUBLISHED")
		testsupport.SeedPackage(t, pool, ev.ID, "Only bundle", "10000.00", true)

		err := svc.DeleteEvent(ctx, ev.ID)

		appErr := appErrOf(t, err)
		assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
		assert.Equal(t, apperr.CodeEventHasOrders, appErr.Code)
	})
}
