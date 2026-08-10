package payment

import (
	"context"
	"fmt"
	"time"

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

// PaymentRecord is one row of the payments log, as staff read it.
type PaymentRecord struct {
	ID            uuid.UUID
	OrderID       uuid.UUID
	Provider      string
	TransactionID string
	PaymentType   string
	// Status is either the gateway's raw status or one of this domain's own
	// markers. Both live in the same column, which is what makes the sequence a
	// single readable narrative rather than two interleaved ones.
	Status      string
	RawResponse []byte
	CreatedAt   time.Time
}

// ListByOrder returns every row recorded against one order, newest first.
func (r *Repository) ListByOrder(ctx context.Context, orderID uuid.UUID) ([]PaymentRecord, error) {
	rows, err := r.queries.ListPaymentsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list payments for order: %w", err)
	}
	return toPaymentRecords(rows), nil
}

func toPaymentRecords(rows []paymentsql.Payment) []PaymentRecord {
	out := make([]PaymentRecord, 0, len(rows))
	for _, row := range rows {
		rec := PaymentRecord{
			ID:            row.ID,
			OrderID:       row.OrderID,
			Provider:      row.Provider,
			TransactionID: row.TransactionID,
			Status:        row.Status,
			RawResponse:   row.RawResponse,
		}
		if row.PaymentType != nil {
			rec.PaymentType = *row.PaymentType
		}
		if row.CreatedAt != nil {
			rec.CreatedAt = *row.CreatedAt
		}
		out = append(out, rec)
	}
	return out
}
