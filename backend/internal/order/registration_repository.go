package order

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/internal/order/ordersql"
)

// CreateRegistrationOrder inserts the free-registration order: PAID, zero total,
// contact identity and terms agreement stamped inline, marked
// registration-originated (spec 022).
//
// Everything is on the INSERT because it HAS to be. The two queries that would
// otherwise patch these columns afterwards — UpdateOrderBuyer and
// RecordTermsAgreement — are both guarded on `status = 'PENDING'`, so against an
// order created at PAID they match zero rows and their wrappers return false,
// which the service layer reports as 410 ORDER_EXPIRED. A registration that
// genuinely succeeded would be reported to the guest as gone.
func (r *Repository) CreateRegistrationOrder(
	ctx context.Context,
	tx pgx.Tx,
	orderNumber string,
	buyer RegistrationBuyer,
	eventTermsID uuid.UUID,
) (OrderRecord, error) {
	row, err := r.queries.WithTx(tx).CreateRegistrationOrder(ctx, ordersql.CreateRegistrationOrderParams{
		OrderNumber:  orderNumber,
		BuyerName:    &buyer.Name,
		BuyerEmail:   &buyer.Email,
		BuyerPhone:   &buyer.Phone,
		EventTermsID: uuid.NullUUID{UUID: eventTermsID, Valid: true},
	})
	if isUniqueViolation(err) {
		return OrderRecord{}, ErrOrderNumberTaken
	}
	if err != nil {
		return OrderRecord{}, fmt.Errorf("create registration order: %w", err)
	}
	return toOrderRecord(ordersql.GetOrderByIDRow(row)), nil
}

// RegistrationBuyer is the contact snapshot a registration writes onto its order.
//
// It is the registrant themself: one form, one holder, one ticket. buyer_email
// is the delivery address and is NOT optional — notification.SendTicketEmail
// reads orders.buyer_email and refuses an empty snapshot, so a registration that
// left it unset would issue a ticket and then silently fail to deliver it, after
// the guest was already told it had been sent (FR-039e).
type RegistrationBuyer struct {
	Name  string
	Email string
	Phone string
}

// There is deliberately NO per-address method here (spec 022 FR-023a).
//
// An earlier draft carried EmailHasIssuedTicketForEvent and
// LockRegistrationIdentity. The rule they enforced — one address, one place per
// event — was removed, so an address may register as many times as remaining
// quota allows. Nothing in this write path may be keyed on the email address:
// two submissions of one address must not contend, which is why the advisory
// lock went with the check instead of being kept for safety. A lock with no
// invariant left to protect only serialises the hot path.
