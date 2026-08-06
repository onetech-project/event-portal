package order

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock EventProvider for expandItem tests --------------------------------

type mockEventProvider struct {
	tickets  map[uuid.UUID]TicketTypeInfo
	packages map[uuid.UUID]PackageInfo
}

func (m *mockEventProvider) TicketTypeForCheckout(_ context.Context, _ pgx.Tx, id uuid.UUID) (TicketTypeInfo, error) {
	if t, ok := m.tickets[id]; ok {
		return t, nil
	}
	return TicketTypeInfo{}, assert.AnError
}

func (m *mockEventProvider) CheckAndDeductQuota(_ context.Context, _ pgx.Tx, _ uuid.UUID, _ int32) error {
	return nil
}

func (m *mockEventProvider) RestoreQuota(_ context.Context, _ pgx.Tx, _ uuid.UUID, _ int32) error {
	return nil
}

func (m *mockEventProvider) CurrentTerms(_ context.Context, eventID uuid.UUID) (EventTermsInfo, error) {
	return EventTermsInfo{ID: uuid.New(), EventID: eventID}, nil
}

func (m *mockEventProvider) PackageForCheckout(_ context.Context, _ pgx.Tx, id uuid.UUID) (PackageInfo, error) {
	if p, ok := m.packages[id]; ok {
		return p, nil
	}
	return PackageInfo{}, assert.AnError
}

// --- T042: mixed bundle + standalone aggregates correctly -------------------

func TestExpandAndAggregateMixedCart(t *testing.T) {
	now := time.Now()
	day1 := uuid.MustParse("aaaa1111-1111-1111-1111-111111111111")
	day2 := uuid.MustParse("aaaa2222-2222-2222-2222-222222222222")
	bundleID := uuid.MustParse("bbbb1111-1111-1111-1111-111111111111")

	events := &mockEventProvider{
		tickets: map[uuid.UUID]TicketTypeInfo{
			day1: {ID: day1, Name: "Day 1", Price: decimal.NewFromInt(30000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
			day2: {ID: day2, Name: "Day 2", Price: decimal.NewFromInt(20000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
		},
		packages: map[uuid.UUID]PackageInfo{
			bundleID: {
				ID: bundleID, Name: "Day 1+2 Bundle", Price: decimal.NewFromInt(50000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour),
				Components: []PackageComponentInfo{
					{TicketTypeID: day1, Quantity: 1, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
					{TicketTypeID: day2, Quantity: 1, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
				},
			},
		},
	}

	items := []CheckoutItem{
		{PackageID: &bundleID, Quantity: 1}, // 1 × Bundle(Day1, Day2)
		{TicketTypeID: &day1, Quantity: 2},  // 2 × Day1
	}

	var expanded []ExpandedItem
	for _, item := range items {
		ex, err := expandItem(context.Background(), events, nil, item, now)
		require.NoError(t, err)
		expanded = append(expanded, ex)
	}

	agg := aggregateDemand(expanded)

	assert.Equal(t, int32(3), agg[day1], "Day 1: 1 from bundle + 2 standalone = 3")
	assert.Equal(t, int32(1), agg[day2], "Day 2: 1 from bundle = 1")
}

// --- T043: sortedTicketTypeIDs is deterministic across repeated calls -------

func TestSortedTicketTypeIDsIsDeterministic(t *testing.T) {
	ids := []uuid.UUID{
		uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff"),
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		uuid.MustParse("55555555-5555-5555-5555-555555555555"),
	}

	demand := map[uuid.UUID]int32{}
	for _, id := range ids {
		demand[id] = 1
	}

	// Run sortedTicketTypeIDs many times — the output must always be identical.
	first := sortedTicketTypeIDs(demand)
	require.Len(t, first, len(ids))

	for range 100 {
		got := sortedTicketTypeIDs(demand)
		assert.Equal(t, first, got, "sort order must be stable across calls")
	}

	// Verify ascending UUID string order.
	for i := 1; i < len(first); i++ {
		assert.Less(t, first[i-1].String(), first[i].String(),
			"result must be ascending by UUID string")
	}
}

// --- T044: quantity_per_unit > 1 multiplies correctly ----------------------

func TestExpandPackageQuantityPerUnitMultiplier(t *testing.T) {
	now := time.Now()
	ttA := uuid.MustParse("cccc1111-1111-1111-1111-111111111111")
	ttB := uuid.MustParse("cccc2222-2222-2222-2222-222222222222")
	bundleID := uuid.MustParse("dddd1111-1111-1111-1111-111111111111")

	events := &mockEventProvider{
		tickets: map[uuid.UUID]TicketTypeInfo{
			ttA: {ID: ttA, Name: "Ticket A", Price: decimal.NewFromInt(10000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
			ttB: {ID: ttB, Name: "Ticket B", Price: decimal.NewFromInt(10000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
		},
		packages: map[uuid.UUID]PackageInfo{
			bundleID: {
				ID: bundleID, Name: "Mega Pack", Price: decimal.NewFromInt(40000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour),
				Components: []PackageComponentInfo{
					{TicketTypeID: ttA, Quantity: 2, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
					{TicketTypeID: ttB, Quantity: 3, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
				},
			},
		},
	}

	item := CheckoutItem{PackageID: &bundleID, Quantity: 2} // 2 package units

	ex, err := expandItem(context.Background(), events, nil, item, now)
	require.NoError(t, err)

	assert.Equal(t, int32(4), ex.Demand[ttA], "2 units × 2 per unit = 4")
	assert.Equal(t, int32(6), ex.Demand[ttB], "2 units × 3 per unit = 6")
}

// --- aggregateDemand with standalone tickets only ---------------------------

func TestAggregateDemandStandaloneTickets(t *testing.T) {
	day1 := uuid.New()
	day2 := uuid.New()

	items := []ExpandedItem{
		{Ref: TicketLine(day1), Quantity: 3, UnitPrice: decimal.NewFromInt(10000),
			Demand: map[uuid.UUID]int32{day1: 3}},
		{Ref: TicketLine(day2), Quantity: 1, UnitPrice: decimal.NewFromInt(20000),
			Demand: map[uuid.UUID]int32{day2: 1}},
	}

	agg := aggregateDemand(items)
	assert.Equal(t, int32(3), agg[day1])
	assert.Equal(t, int32(1), agg[day2])
}

// --- empty input produces empty output -------------------------------------

func TestAggregateDemandEmptyInput(t *testing.T) {
	agg := aggregateDemand(nil)
	assert.Empty(t, agg)
}

func TestSortedTicketTypeIDsEmptyMap(t *testing.T) {
	result := sortedTicketTypeIDs(map[uuid.UUID]int32{})
	assert.Empty(t, result)
}

// --- single bundle with multiple components --------------------------------

func TestExpandPackageSingleUnitMultiComponent(t *testing.T) {
	now := time.Now()
	ttA := uuid.MustParse("eeee1111-1111-1111-1111-111111111111")
	ttB := uuid.MustParse("eeee2222-2222-2222-2222-222222222222")
	ttC := uuid.MustParse("eeee3333-3333-3333-3333-333333333333")
	bundleID := uuid.MustParse("ffff1111-1111-1111-1111-111111111111")

	events := &mockEventProvider{
		packages: map[uuid.UUID]PackageInfo{
			bundleID: {
				ID: bundleID, Name: "Full Festival", Price: decimal.NewFromInt(75000),
				SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour),
				Components: []PackageComponentInfo{
					{TicketTypeID: ttA, Quantity: 1, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
					{TicketTypeID: ttB, Quantity: 1, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
					{TicketTypeID: ttC, Quantity: 2, SalesStart: now.Add(-time.Hour), SalesEnd: now.Add(time.Hour)},
				},
			},
		},
	}

	item := CheckoutItem{PackageID: &bundleID, Quantity: 1}
	ex, err := expandItem(context.Background(), events, nil, item, now)
	require.NoError(t, err)

	assert.Equal(t, int32(1), ex.Demand[ttA])
	assert.Equal(t, int32(1), ex.Demand[ttB])
	assert.Equal(t, int32(2), ex.Demand[ttC])
	assert.Equal(t, "Full Festival", ex.PackageName)
	assert.True(t, ex.Ref.IsPackage())
}
