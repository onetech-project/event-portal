package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/internal/payment/paymentsql"
)

// PaymentLog is one provider notification, recorded for audit and support.
type PaymentLog struct {
	OrderID       uuid.UUID
	Provider      string
	TransactionID string
	PaymentType   string
	// Status is the provider's RAW transaction_status, not the mapped order
	// status. It is what keeps deny and failure distinguishable after both have
	// folded into orders.status = 'CANCELLED'.
	Status string
	// RawResponse is the complete notification payload.
	RawResponse []byte
}

// Repository is the only place in the codebase that talks to the payments table.
type Repository struct {
	queries *paymentsql.Queries
}

// NewRepository builds a repository over a pool or any other DBTX.
func NewRepository(dbtx paymentsql.DBTX) *Repository {
	return &Repository{queries: paymentsql.New(dbtx)}
}

// CreatePayment appends one row per notification received. Rows are never updated
// in place: the sequence of notifications is itself the audit trail.
func (r *Repository) CreatePayment(ctx context.Context, log PaymentLog) error {
	var paymentType *string
	if log.PaymentType != "" {
		paymentType = &log.PaymentType
	}

	_, err := r.queries.CreatePayment(ctx, paymentsql.CreatePaymentParams{
		OrderID:       log.OrderID,
		Provider:      log.Provider,
		TransactionID: log.TransactionID,
		PaymentType:   paymentType,
		Status:        log.Status,
		RawResponse:   log.RawResponse,
	})
	if err != nil {
		return fmt.Errorf("record payment notification: %w", err)
	}
	return nil
}

// CountReissuedQRs reports how many QR re-issues the order has logged, which
// drives the next -R{n} suffix.
func (r *Repository) CountReissuedQRs(ctx context.Context, orderID uuid.UUID) (int64, error) {
	n, err := r.queries.CountReissuedQRs(ctx, orderID)
	if err != nil {
		return 0, fmt.Errorf("count reissued QRs: %w", err)
	}
	return n, nil
}
