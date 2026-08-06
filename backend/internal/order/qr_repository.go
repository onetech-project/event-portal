package order

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/internal/order/ordersql"
)

// UpdatePaymentQRByID swaps the order's QR payload without touching its
// deadline (QR re-issue, FR-015), guarded on the order still being PENDING.
// Consumed by the payment domain through its OrderProvider contract.
func (r *Repository) UpdatePaymentQRByID(ctx context.Context, orderID uuid.UUID, url, qrString string) (bool, error) {
	rows, err := r.queries.UpdatePaymentQR(ctx, ordersql.UpdatePaymentQRParams{
		ID:              orderID,
		PaymentUrl:      &url,
		PaymentQrString: &qrString,
	})
	if err != nil {
		return false, fmt.Errorf("swap payment QR: %w", err)
	}
	return rows > 0, nil
}
