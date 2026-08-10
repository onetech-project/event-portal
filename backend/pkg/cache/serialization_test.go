package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/money"
)

// Cached values go to Redis as JSON and come back as Go structs. Two field types
// in this codebase can lose information on that trip and would do so silently:
//
//   - money.Money wraps a decimal and marshals as a fixed-scale STRING. Encoded
//     as a float instead, 175500.55 would come back as 175500.55000000001.
//   - *time.Time distinguishes "no sales end" from a zero timestamp. Flattened to
//     a value type, a nil would come back as 0001-01-01 and render as a real date.
//
// A wrong price served from cache is worse than no cache at all, so this is a
// required test rather than a nice-to-have.

// cachedShape mirrors the risky field types that appear across the cached DTOs
// (TicketTypeSummary, PackageSummaryDTO, OrderSummary, AttendeeSummary).
type cachedShape struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Price       money.Money `json:"price"`
	Quantity    int32       `json:"quantity"`
	SalesStart  time.Time   `json:"sales_start"`
	SalesEnd    *time.Time  `json:"sales_end"`
	PaidAt      *time.Time  `json:"paid_at"`
}

func TestMoneyRoundTripsExactly(t *testing.T) {
	amounts := []string{
		"0.00",
		"0.01",
		"1000.00",      // trailing zeros the pg driver strips on decode
		"175500.55",    // not exactly representable as a binary float
		"999999999.99", // the NUMERIC(12,2) ceiling
		"123456789.12",
	}
	for _, a := range amounts {
		t.Run(a, func(t *testing.T) {
			original := money.From(decimal.RequireFromString(a))

			raw, err := json.Marshal(cachedShape{Price: original})
			require.NoError(t, err)

			var back cachedShape
			require.NoError(t, json.Unmarshal(raw, &back))

			require.Equal(t, original.String(), back.Price.String(),
				"a cached price must be the same price")
			require.True(t, original.Decimal().Equal(back.Price.Decimal()))
		})
	}
}

func TestNilTimestampsStayNil(t *testing.T) {
	v := cachedShape{
		ID:         uuid.New(),
		SalesStart: time.Now().UTC().Truncate(time.Second),
		SalesEnd:   nil,
		PaidAt:     nil,
	}

	raw, err := json.Marshal(v)
	require.NoError(t, err)

	var back cachedShape
	require.NoError(t, json.Unmarshal(raw, &back))

	require.Nil(t, back.SalesEnd, "an absent timestamp must not become 0001-01-01")
	require.Nil(t, back.PaidAt)
	require.True(t, v.SalesStart.Equal(back.SalesStart))
}

func TestNilPointerFieldsStayNil(t *testing.T) {
	var back cachedShape
	raw, err := json.Marshal(cachedShape{Description: nil})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Nil(t, back.Description, "a nil description must not become an empty string")

	desc := "Includes one drink"
	raw, err = json.Marshal(cachedShape{Description: &desc})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &back))
	require.NotNil(t, back.Description)
	require.Equal(t, desc, *back.Description)
}

// The end-to-end property that matters: what Through returns on a hit must equal
// what it returned on the miss, byte for byte once re-serialized (FR-005).
func TestThroughHitEqualsThroughMiss(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	k := TicketTypesPublicKey(uuid.New())

	end := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	desc := "Early bird"
	source := []cachedShape{
		{
			ID: uuid.New(), Name: "Regular", Description: &desc,
			Price: money.From(decimal.RequireFromString("175500.55")), Quantity: 3,
			SalesStart: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), SalesEnd: &end,
		},
		{
			ID: uuid.New(), Name: "VIP", Description: nil,
			Price: money.From(decimal.RequireFromString("1000.00")), Quantity: 0,
			SalesStart: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), SalesEnd: nil,
		},
	}

	load := func(context.Context) ([]cachedShape, error) { return source, nil }

	fromMiss, err := Through(ctx, c, k, load)
	require.NoError(t, err)

	fromHit, err := Through(ctx, c, k, func(context.Context) ([]cachedShape, error) {
		t.Fatal("the second call must be served from cache, not the loader")
		return nil, nil
	})
	require.NoError(t, err)

	missJSON, err := json.Marshal(fromMiss)
	require.NoError(t, err)
	hitJSON, err := json.Marshal(fromHit)
	require.NoError(t, err)

	require.JSONEq(t, string(missJSON), string(hitJSON))

	// And both must equal what the uncached path would have produced.
	uncachedJSON, err := json.Marshal(source)
	require.NoError(t, err)
	require.JSONEq(t, string(uncachedJSON), string(hitJSON))
}

// An empty slice must survive as an empty slice, not become null — a client that
// iterates the response would break on null.
func TestEmptySliceRoundTripsAsEmptySlice(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	k := PackagesPublicKey(uuid.New())

	first, err := Through(ctx, c, k, func(context.Context) ([]cachedShape, error) {
		return []cachedShape{}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Empty(t, first)

	second, err := Through(ctx, c, k, func(context.Context) ([]cachedShape, error) {
		t.Fatal("must be a hit")
		return nil, nil
	})
	require.NoError(t, err)
	require.NotNil(t, second, "an empty list must not come back as null")
	require.Empty(t, second)
}
