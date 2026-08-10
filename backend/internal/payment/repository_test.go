package payment_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

func TestCreatePaymentAppendsARowPerNotification(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := payment.NewRepository(pool)
	ctx := context.Background()

	ord := testsupport.SeedOrder(t, pool, "ORD-PAYLOG", "PENDING")

	require.NoError(t, repo.CreatePayment(ctx, payment.PaymentLog{
		OrderID:       ord.ID,
		Provider:      "manjo",
		TransactionID: "tx-1",
		PaymentType:   "bank_transfer",
		Status:        "pending",
		RawResponse:   []byte(`{"transaction_status":"pending"}`),
	}))
	require.NoError(t, repo.CreatePayment(ctx, payment.PaymentLog{
		OrderID:       ord.ID,
		Provider:      "manjo",
		TransactionID: "tx-1",
		PaymentType:   "bank_transfer",
		Status:        "settlement",
		RawResponse:   []byte(`{"transaction_status":"settlement"}`),
	}))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM payments WHERE order_id = $1`, ord.ID).Scan(&count))
	assert.Equal(t, 2, count, "every notification is logged, not just the last one")
}

// deny and failure both fold into orders.status = CANCELLED, so the payments row
// is the only place they stay distinguishable.
func TestCreatePaymentPreservesTheRawProviderStatus(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := payment.NewRepository(pool)
	ctx := context.Background()

	ord := testsupport.SeedOrder(t, pool, "ORD-RAWSTATUS", "PENDING")

	for _, status := range []string{"deny", "failure"} {
		require.NoError(t, repo.CreatePayment(ctx, payment.PaymentLog{
			OrderID:       ord.ID,
			Provider:      "manjo",
			TransactionID: "tx-" + status,
			Status:        status,
			RawResponse:   []byte(`{"transaction_status":"` + status + `"}`),
		}))
	}

	rows, err := pool.Query(ctx, `SELECT status FROM payments WHERE order_id = $1 ORDER BY status`, ord.ID)
	require.NoError(t, err)
	defer rows.Close()

	var stored []string
	for rows.Next() {
		var status string
		require.NoError(t, rows.Scan(&status))
		stored = append(stored, status)
	}
	assert.Equal(t, []string{"deny", "failure"}, stored)
}

func TestCreatePaymentStoresTheFullPayloadAsJSONB(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := payment.NewRepository(pool)
	ctx := context.Background()

	ord := testsupport.SeedOrder(t, pool, "ORD-PAYLOAD", "PENDING")
	raw := []byte(`{"transaction_status":"settlement","fraud_status":"accept","gross_amount":"300000.00"}`)

	require.NoError(t, repo.CreatePayment(ctx, payment.PaymentLog{
		OrderID: ord.ID, Provider: "manjo", TransactionID: "tx-9",
		Status: "settlement", RawResponse: raw,
	}))

	var stored []byte
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT raw_response FROM payments WHERE order_id = $1`, ord.ID).Scan(&stored))

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(stored, &parsed))
	assert.Equal(t, "accept", parsed["fraud_status"])
	assert.Equal(t, "300000.00", parsed["gross_amount"])
}

func TestCreatePaymentAcceptsAMissingPaymentType(t *testing.T) {
	pool := testsupport.RequirePool(t)
	repo := payment.NewRepository(pool)
	ctx := context.Background()

	ord := testsupport.SeedOrder(t, pool, "ORD-NOTYPE", "PENDING")

	err := repo.CreatePayment(ctx, payment.PaymentLog{
		OrderID: ord.ID, Provider: "manjo", TransactionID: "tx-x",
		Status: "expire", RawResponse: []byte(`{}`),
	})

	require.NoError(t, err, "payments.payment_type is nullable")
}
