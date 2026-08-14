package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	// ExtRefID is the gateway's own reference for the payment session, known only
	// at session open. Empty on every notification row: a callback carries our
	// order number and the gateway's network transaction id, never this.
	ExtRefID string
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
	// Empty means "the gateway supplied none", which must stay distinguishable
	// from a gateway that answered with a blank one — so NULL, never ''.
	var extRefID *string
	if log.ExtRefID != "" {
		extRefID = &log.ExtRefID
	}

	_, err := r.queries.CreatePayment(ctx, paymentsql.CreatePaymentParams{
		OrderID:       log.OrderID,
		Provider:      log.Provider,
		TransactionID: log.TransactionID,
		PaymentType:   paymentType,
		Status:        log.Status,
		RawResponse:   log.RawResponse,
		ExtRefID:      extRefID,
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
	// ExtRefID is the gateway's own reference for the payment session. Non-empty
	// on the SESSION_OPENED row and empty on every other, which is what lets a
	// reader resolve one order-level value out of the whole sequence.
	ExtRefID  string
	CreatedAt time.Time
}

// ListByOrder returns every row recorded against one order, newest first.
func (r *Repository) ListByOrder(ctx context.Context, orderID uuid.UUID) ([]PaymentRecord, error) {
	rows, err := r.queries.ListPaymentsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list payments for order: %w", err)
	}
	return toPaymentRecords(rows), nil
}

// ExternalRefByOrderID returns the gateway's own reference for an order's payment
// session, or the empty string when none was ever recorded.
//
// No row is a NORMAL answer, not an error: an order whose session never opened,
// and every order predating the column, simply has no reference. Turning that
// into an error would force each caller to re-decide that "absent" is fine, and
// the checkout response has to carry the field present-and-empty either way.
func (r *Repository) ExternalRefByOrderID(ctx context.Context, orderID uuid.UUID) (string, error) {
	ref, err := r.queries.GetExternalRefByOrderID(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read external reference for order: %w", err)
	}
	if ref == nil {
		return "", nil
	}
	return *ref, nil
}

func toPaymentRecords(rows []paymentsql.ListPaymentsByOrderIDRow) []PaymentRecord {
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
		if row.ExtRefID != nil {
			rec.ExtRefID = *row.ExtRefID
		}
		if row.CreatedAt != nil {
			rec.CreatedAt = *row.CreatedAt
		}
		out = append(out, rec)
	}
	return out
}
