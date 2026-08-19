package order_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// Paging for the admin order and attendee lists (spec 021).
//
// The property under test throughout is the one that cannot be seen in a single
// unpaginated read: walking every page must return each record exactly once. It
// holds only because the ORDER BY ends in a primary key, so the tie fixtures
// below are the point of this file rather than decoration.

func page(n, size int) httpx.PageRequest { return httpx.NewPageRequest(n, size) }

// tieCreatedAt forces rows to share a created_at, which is what makes the
// leading sort key ambiguous. Seeding alone cannot do it: the default is now(),
// and two inserts differ by microseconds.
func tieCreatedAt(t *testing.T, f adminOrderFixture, ids ...uuid.UUID) {
	t.Helper()
	shared := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range ids {
		_, err := f.pool.Exec(context.Background(),
			`UPDATE orders SET created_at = $1 WHERE id = $2`, shared, id)
		require.NoError(t, err)
	}
}

// seedOrders creates n orders against the fixture's ticket type and returns
// their numbers.
func seedOrders(t *testing.T, f adminOrderFixture, n int, prefix string) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		ord := testsupport.SeedOrder(t, f.pool, fmt.Sprintf("%s-%02d", prefix, i), "PAID")
		testsupport.SeedOrderItem(t, f.pool, ord.ID, f.reg.ID, 1)
		ids = append(ids, ord.ID)
	}
	return ids
}

// walk collects every order number across every page, at the given page size.
func walkOrders(t *testing.T, f adminOrderFixture, filter order.OrderFilter, size int) []string {
	t.Helper()
	ctx := context.Background()

	filter.Page = page(1, size)
	first, err := f.svc.ListOrders(ctx, filter)
	require.NoError(t, err)

	seen := make([]string, 0, first.Total)
	for _, row := range first.Items {
		seen = append(seen, row.OrderNumber)
	}
	for n := 2; n <= first.TotalPages; n++ {
		filter.Page = page(n, size)
		p, err := f.svc.ListOrders(ctx, filter)
		require.NoError(t, err)
		for _, row := range p.Items {
			seen = append(seen, row.OrderNumber)
		}
	}
	return seen
}

// --- Counting ---------------------------------------------------------------

func TestOrderPageReportsTheTotalMatchingTheFilterNotTheTable(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 3, "ORD-PAID")

	pending := testsupport.SeedOrder(t, f.pool, "ORD-PEND", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, pending.ID, f.reg.ID, 1)

	paid := "PAID"
	p, err := f.svc.ListOrders(context.Background(),
		order.OrderFilter{Status: &paid, Page: page(1, 2)})

	require.NoError(t, err)
	assert.EqualValues(t, 3, p.Total, "the total counts what the filter matches")
	assert.Len(t, p.Items, 2, "but only one page of it is returned")
	assert.Equal(t, 2, p.TotalPages)
}

func TestCountAndPageAgreeUnderEveryFilterCombination(t *testing.T) {
	// The count and the page are two queries sharing one WHERE clause, written
	// twice. This is what catches them drifting apart.
	f := newAdminOrderFixture(t)
	otherType := testsupport.SeedTicketType(t, f.pool, f.other.ID, "Other", "100000.00", 10)

	seedOrders(t, f, 4, "ORD-MINE")
	theirs := testsupport.SeedOrder(t, f.pool, "ORD-THEIRS", "PAID")
	testsupport.SeedOrderItem(t, f.pool, theirs.ID, otherType.ID, 1)
	pending := testsupport.SeedOrder(t, f.pool, "ORD-PEND", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, pending.ID, f.reg.ID, 1)

	paid := "PAID"
	for _, filter := range []order.OrderFilter{
		{},
		{Status: &paid},
		{EventID: &f.event.ID},
		{Status: &paid, EventID: &f.event.ID},
	} {
		filter.Page = page(1, 100)
		p, err := f.svc.ListOrders(context.Background(), filter)
		require.NoError(t, err)
		assert.EqualValues(t, len(p.Items), p.Total,
			"count and page disagree; the two WHERE clauses have drifted")
	}
}

func TestAnEventWithNoTicketTypesReportsZeroNotAnUnfilteredCount(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 3, "ORD-X")
	empty := testsupport.SeedEvent(t, f.pool, "no-types-paged", "PUBLISHED")

	p, err := f.svc.ListOrders(context.Background(),
		order.OrderFilter{EventID: &empty.ID, Page: page(1, 20)})

	require.NoError(t, err)
	assert.Empty(t, p.Items)
	assert.EqualValues(t, 0, p.Total, "the short-circuit must not skip the filter")
	assert.Equal(t, 0, p.TotalPages)
}

// --- Walking ----------------------------------------------------------------

func TestPageTwoIsDisjointFromPageOne(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 5, "ORD-W")
	ctx := context.Background()

	first, err := f.svc.ListOrders(ctx, order.OrderFilter{Page: page(1, 2)})
	require.NoError(t, err)
	second, err := f.svc.ListOrders(ctx, order.OrderFilter{Page: page(2, 2)})
	require.NoError(t, err)

	require.Len(t, first.Items, 2)
	require.Len(t, second.Items, 2)
	for _, a := range first.Items {
		for _, b := range second.Items {
			assert.NotEqual(t, a.OrderNumber, b.OrderNumber,
				"%s appears on both pages", a.OrderNumber)
		}
	}
}

func TestWalkingEveryPageReturnsEveryOrderExactlyOnce(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 7, "ORD-EACH")

	seen := walkOrders(t, f, order.OrderFilter{}, 2)

	assert.Len(t, seen, 7, "no order missed and none repeated")
	assert.Len(t, unique(seen), 7)
}

func TestWalkingEveryPageAgreesWithOneUnpaginatedRead(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 6, "ORD-SAME")

	whole, err := f.svc.ListOrders(context.Background(), order.OrderFilter{Page: page(1, 100)})
	require.NoError(t, err)

	walked := walkOrders(t, f, order.OrderFilter{}, 2)

	numbers := make([]string, 0, len(whole.Items))
	for _, row := range whole.Items {
		numbers = append(numbers, row.OrderNumber)
	}
	assert.Equal(t, numbers, walked, "paging must not reorder the list")
}

// --- Ties (spec 021 research R4) --------------------------------------------

func TestOrdersTiedOnCreatedAtStillWalkCleanly(t *testing.T) {
	// Without `o.id DESC` after `o.created_at DESC`, PostgreSQL is free to order
	// these five differently for two different OFFSETs — showing a duplicate on
	// page 2 while hiding another record entirely. This is the failure that is
	// invisible in a single unpaginated read.
	f := newAdminOrderFixture(t)
	ids := seedOrders(t, f, 5, "ORD-TIE")
	tieCreatedAt(t, f, ids...)

	seen := walkOrders(t, f, order.OrderFilter{}, 2)

	assert.Len(t, seen, 5)
	assert.Len(t, unique(seen), 5, "a tie broke the walk: %v", seen)
}

func TestTiedOrdersOrderIdenticallyAcrossRepeatedReads(t *testing.T) {
	f := newAdminOrderFixture(t)
	ids := seedOrders(t, f, 5, "ORD-STABLE")
	tieCreatedAt(t, f, ids...)

	firstWalk := walkOrders(t, f, order.OrderFilter{}, 2)
	for i := 0; i < 5; i++ {
		assert.Equal(t, firstWalk, walkOrders(t, f, order.OrderFilter{}, 2),
			"the ordering must be deterministic, not merely sorted")
	}
}

func TestAttendeesSharingAnOrderAndANameStillWalkCleanly(t *testing.T) {
	// The worst tie in the product: one order's attendees all share the order's
	// created_at, and two people called "Budi" on one order is ordinary.
	f := newAdminOrderFixture(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-DUPES", "PAID")
	for i := 0; i < 6; i++ {
		testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID, "Budi", "budi@example.com")
	}
	ctx := context.Background()

	rows := 0
	for n := 1; ; n++ {
		p, err := f.svc.ListAttendees(ctx, order.AttendeeFilter{Page: page(n, 2)})
		require.NoError(t, err)
		rows += len(p.Items)
		if n >= p.TotalPages {
			assert.EqualValues(t, 6, p.Total)
			break
		}
	}

	// The rows are indistinguishable by name and email, so a walk that lost one
	// to a tie and duplicated another would still look plausible row by row. The
	// count across the whole walk is what catches it.
	assert.Equal(t, 6, rows, "every attendee seen exactly once across the walk")
}

// --- Clamping ---------------------------------------------------------------

func TestAPageBeyondTheEndServesTheLastPage(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 5, "ORD-CLAMP")

	p, err := f.svc.ListOrders(context.Background(), order.OrderFilter{Page: page(999, 2)})

	require.NoError(t, err)
	assert.Equal(t, 3, p.Page, "five orders at two per page is three pages")
	assert.Len(t, p.Items, 1)
	assert.EqualValues(t, 5, p.Total)
}

func TestAnEmptyListIsPageOneOfZero(t *testing.T) {
	f := newAdminOrderFixture(t)

	p, err := f.svc.ListOrders(context.Background(), order.OrderFilter{Page: page(9, 20)})

	require.NoError(t, err)
	assert.Equal(t, 1, p.Page, "there is no page 9 to clamp to, and no page 0 to report")
	assert.Equal(t, 0, p.TotalPages)
	assert.NotNil(t, p.Items, "an empty page must serialize as [] not null")
}

func TestABareFilterMeansTheFirstPageNotNoRows(t *testing.T) {
	// The zero value of PageRequest asks for LIMIT 0. Normalisation is what stops
	// OrderFilter{} from returning an empty page for a list that has rows.
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 3, "ORD-BARE")

	p, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	assert.Len(t, p.Items, 3)
	assert.Equal(t, 1, p.Page)
	assert.Equal(t, httpx.DefaultPageSize, p.PageSize)
}

func TestAnOversizedPageIsCappedByTheService(t *testing.T) {
	f := newAdminOrderFixture(t)
	seedOrders(t, f, 3, "ORD-CAP")

	p, err := f.svc.ListOrders(context.Background(),
		order.OrderFilter{Page: httpx.PageRequest{Page: 1, Size: 5000}})

	require.NoError(t, err)
	assert.Equal(t, httpx.MaxPageSize, p.PageSize)
}

// --- Attendees --------------------------------------------------------------

func TestAttendeePageReportsItsOwnTotal(t *testing.T) {
	f := newAdminOrderFixture(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ATT-PAGE", "PAID")
	for i := 0; i < 5; i++ {
		testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID,
			fmt.Sprintf("Guest %02d", i), fmt.Sprintf("g%02d@example.com", i))
	}

	p, err := f.svc.ListAttendees(context.Background(), order.AttendeeFilter{Page: page(1, 2)})

	require.NoError(t, err)
	assert.Len(t, p.Items, 2)
	assert.EqualValues(t, 5, p.Total)
	assert.Equal(t, 3, p.TotalPages)
}

func TestAttendeeCountRespectsTheEventFilter(t *testing.T) {
	f := newAdminOrderFixture(t)
	otherType := testsupport.SeedTicketType(t, f.pool, f.other.ID, "Other", "100000.00", 10)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ATT-EV", "PAID")

	testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID, "Mine", "mine@example.com")
	testsupport.SeedAttendee(t, f.pool, ord.ID, otherType.ID, "Theirs", "theirs@example.com")

	p, err := f.svc.ListAttendees(context.Background(),
		order.AttendeeFilter{EventID: &f.event.ID, Page: page(1, 20)})

	require.NoError(t, err)
	assert.EqualValues(t, 1, p.Total)
	require.Len(t, p.Items, 1)
	assert.Equal(t, "Mine", p.Items[0].Name)
}

// --- Fees -------------------------------------------------------------------

func TestFeesPaginateAndReportTheirTotal(t *testing.T) {
	f := newAdminOrderFixture(t)
	for i := 0; i < 5; i++ {
		testsupport.SeedFee(t, f.pool, fmt.Sprintf("Fee %02d", i), "FIXED", "1000.00", i)
	}

	p, err := f.svc.Fees(context.Background(), page(2, 2))

	require.NoError(t, err)
	assert.Len(t, p.Items, 2)
	assert.EqualValues(t, 5, p.Total)
	assert.Equal(t, 3, p.TotalPages)
	assert.Equal(t, 2, p.Page)
}

func TestFeesClampAPageBeyondTheEnd(t *testing.T) {
	f := newAdminOrderFixture(t)
	for i := 0; i < 3; i++ {
		testsupport.SeedFee(t, f.pool, fmt.Sprintf("F%02d", i), "FIXED", "1000.00", i)
	}

	p, err := f.svc.Fees(context.Background(), page(99, 2))

	require.NoError(t, err)
	assert.Equal(t, 2, p.Page)
	assert.Len(t, p.Items, 1)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
