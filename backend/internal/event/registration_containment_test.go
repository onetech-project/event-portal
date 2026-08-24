package event_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// --- spec 022 US2: registration-only types are outside the purchase path -----

// FR-006: the guest list must not carry them.
func TestGuestTicketListExcludesRegistrationOnlyTypes(t *testing.T) {
	svc, pool := newService(t)
	ev := testsupport.SeedEvent(t, pool, "containment", "PUBLISHED")
	paid := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	hidden := testsupport.SeedRegistrationTicketType(t, pool, ev.ID, "Invitation Access", 10)

	types, err := svc.TicketTypesForEventSlug(context.Background(), ev.Slug)
	require.NoError(t, err)

	ids := make([]string, 0, len(types))
	for _, tt := range types {
		ids = append(ids, tt.ID.String())
	}
	assert.Contains(t, ids, paid.ID.String())
	assert.NotContains(t, ids, hidden.ID.String(),
		"a registration-only type must appear on no guest purchase surface")
}

// FR-009: admin sees everything. Containment applies to the purchase path, not
// to administration — an admin who cannot see the type cannot manage it.
func TestAdminTicketListIncludesRegistrationOnlyTypes(t *testing.T) {
	svc, pool := newService(t)
	ev := testsupport.SeedEvent(t, pool, "admin-sees-all", "PUBLISHED")
	hidden := testsupport.SeedRegistrationTicketType(t, pool, ev.ID, "Invitation Access", 10)

	views, err := svc.ListTicketTypes(context.Background(), ev.ID)
	require.NoError(t, err)

	var found bool
	for _, tt := range views {
		if tt.ID == hidden.ID {
			found = true
			assert.False(t, tt.IsVisible, "and it must be labelled as such")
		}
	}
	assert.True(t, found, "the admin list must carry registration-only types")
}

// THE TRAP (research D17). ListTicketTypeIDsByEventID feeds the DeleteEvent
// guard. Filtering registration-only types out of it — the instinctive
// "add the filter everywhere" move — would make the guard skip them, so the
// delete would proceed and hit ON DELETE RESTRICT, surfacing a raw constraint
// violation instead of the 400 Principle VI requires.
func TestDeleteGuardStillSeesRegistrationOnlyTypes(t *testing.T) {
	_, pool := newService(t)
	ev := testsupport.SeedEvent(t, pool, "delete-guard", "PUBLISHED")
	tt := testsupport.SeedRegistrationTicketType(t, pool, ev.ID, "Invitation Access", 10)

	repo := event.NewRepository(pool)
	ids, err := repo.ListTicketTypeIDsByEventID(context.Background(), nil, ev.ID)
	require.NoError(t, err)

	found := false
	for _, id := range ids {
		if id == tt.ID {
			found = true
		}
	}
	assert.True(t, found,
		"the id list backing the delete guard must NOT be filtered — see spec 022 research D17")
}
