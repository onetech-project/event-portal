package order

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// orderNumberAttempts bounds the retry loop for order-number collisions. The
// suffix has ~1e9 combinations per day, so needing even a second attempt is
// already vanishingly unlikely; the bound exists so a pathological failure surfaces
// instead of spinning.
const orderNumberAttempts = 5

// Timers are the server-owned booking deadlines (spec 008, config-backed:
// BOOKING_HOLD / PAYMENT_WINDOW / QR_REFRESH_AFTER).
type Timers struct {
	BookingHold    time.Duration
	PaymentWindow  time.Duration
	QRRefreshAfter time.Duration
}

// Service implements guest checkout.
type Service struct {
	pool    db.Beginner
	repo    *Repository
	events  EventProvider
	gateway PaymentGateway
	log     *logger.Logger
	now     func() time.Time
	timers  Timers
}

// NewService builds the order service. Timers default to the contract values;
// the composition root overrides them from config via WithTimers.
func NewService(pool db.Beginner, repo *Repository, events EventProvider, gateway PaymentGateway, log *logger.Logger) *Service {
	return &Service{
		pool:    pool,
		repo:    repo,
		events:  events,
		gateway: gateway,
		log:     log,
		now:     time.Now,
		timers: Timers{
			BookingHold:    time.Hour,
			PaymentWindow:  14 * time.Minute,
			QRRefreshAfter: 7 * time.Minute,
		},
	}
}

// WithTimers overrides the booking deadlines from config. Returns the service
// for chaining at the composition root.
func (s *Service) WithTimers(t Timers) *Service {
	s.timers = t
	return s
}

// reservedLine records what TX1 actually reserved, so the compensation path knows
// exactly how much quota to give back.
type reservedLine struct {
	TicketTypeID uuid.UUID
	Name         string
	Quantity     int32
	UnitPrice    decimal.Decimal
}

// Checkout runs the three-step purchase sequence required by
// research.md "Checkout transaction shape":
//
//	TX1  quota deduction + orders + order_items + attendees, committed atomically
//	     (ARCHITECTURE.md §3.4, Constitution Principle IV)
//	 →   Gateway.CreateTransaction, outside any transaction
//	TX2  stamp payment_url / payment_provider
//
// The gateway call is deliberately outside TX1: the quota-deducting UPDATE holds a
// row lock until commit, so a ~200-500ms provider round-trip inside it would
// serialize every concurrent buyer of the same ticket type. If that call fails, a
// compensating transaction cancels the order and restores its quota so the guest
// can safely retry (spec FR-021).
func (s *Service) Checkout(ctx context.Context, req CheckoutRequest) (OrderResponse, error) {
	// Reject what can be decided without I/O first, so a malformed request never
	// opens a transaction or takes a quota row lock.
	if err := req.Validate(); err != nil {
		return OrderResponse{}, err
	}

	created, total, reserved, expanded, err := s.reserve(ctx, req)
	if err != nil {
		return OrderResponse{}, err
	}

	session, err := s.gateway.CreateTransaction(ctx, s.paymentRequestFor(created, total, expanded, req))
	if err != nil {
		s.log.ErrorContext(ctx, "payment initiation failed; compensating",
			"order_number", created.OrderNumber, "provider", s.gateway.Name(), "error", err.Error())
		s.compensate(ctx, created, reserved)
		return OrderResponse{}, apperr.Wrap(err, 502, apperr.CodePaymentInitiationFailed,
			"We could not start the payment with the provider. No tickets were reserved — please try again.")
	}

	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.UpdatePaymentDetails(ctx, tx, created.ID, PaymentDetails{
			// The provider's hosted QR image is recorded for audit; the guest is
			// shown an image we render ourselves from QRString.
			PaymentURL: session.QRImageURL,
			Provider:   s.gateway.Name(),
			QRString:   session.QRString,
			ExpiresAt:  session.ExpiresAt,
		})
	}); err != nil {
		// The payment session exists but we could not record it. Compensating here
		// would strand a live payment session against a cancelled order, so the
		// order is left PENDING and the guest is asked to retry; the webhook still
		// resolves it either way.
		s.log.ErrorContext(ctx, "could not persist payment details",
			"order_number", created.OrderNumber, "error", err.Error())
		return OrderResponse{}, apperr.Wrap(err, 502, apperr.CodePaymentInitiationFailed,
			"We could not complete the payment setup. Please try again.")
	}

	s.log.InfoContext(ctx, "checkout completed",
		"order_number", created.OrderNumber, "provider", s.gateway.Name(), "total_amount", total.String())

	return OrderResponse{
		OrderNumber: created.OrderNumber,
		Status:      created.Status,
		TotalAmount: money.From(total),
		// Retained for compatibility and audit. The client no longer navigates
		// here: it routes in-app to the order page, which renders the QR itself
		// (spec FR-009).
		PaymentURL: session.QRImageURL,
	}, nil
}

// Book runs TX-B (contracts/booking-flow.md §1): the reservation transaction
// without any buyer identity or gateway involvement. Fired on the T&C "Agree"
// click, immediately followed by RecordAgreement from the same click.
//
//	guard  event has authored terms (409001 — nothing to agree to otherwise)
//	TX-B   expand + validate items → aggregate demand → sorted guarded quota
//	       deduction → INSERT orders (PENDING, buyer NULL,
//	       payment_expires_at = now()+BOOKING_HOLD) → order_items → EMPTY
//	       attendee slots
//
// No network call anywhere in this request (Constitution Principle IV).
func (s *Service) Book(ctx context.Context, req BookRequest) (BookResponse, error) {
	if err := req.Validate(); err != nil {
		return BookResponse{}, err
	}

	// Read-only pre-TX guard: an event without authored terms has nothing the
	// guest could have agreed to, so no hold may be taken for it.
	if _, err := s.events.CurrentTerms(ctx, req.EventID); err != nil {
		if errors.Is(err, ErrNoTerms) {
			return BookResponse{}, apperr.Conflict(apperr.CodeTermsMissing,
				"This event has no Terms & Conditions to agree to yet.")
		}
		return BookResponse{}, err
	}

	var (
		created OrderRecord
		lastErr error
	)
	for attempt := range orderNumberAttempts {
		var err error
		created, err = s.bookOnce(ctx, req)
		if err == nil {
			s.log.InfoContext(ctx, "order booked",
				"order_number", created.OrderNumber,
				"total_amount", created.TotalAmount.String(),
				"hold_expires_at", created.PaymentExpiresAt)
			return BookResponse{
				OrderID:     created.OrderNumber,
				Status:      created.Status,
				TotalAmount: money.From(created.TotalAmount),
				ExpiresAt:   *created.PaymentExpiresAt,
			}, nil
		}
		if !errors.Is(err, ErrOrderNumberTaken) {
			return BookResponse{}, err
		}
		lastErr = err
		s.log.WarnContext(ctx, "order number collision; retrying", "attempt", attempt+1)
	}
	return BookResponse{}, fmt.Errorf("could not allocate a unique order number: %w", lastErr)
}

func (s *Service) bookOnce(ctx context.Context, req BookRequest) (OrderRecord, error) {
	orderNumber, err := GenerateOrderNumber(s.now())
	if err != nil {
		return OrderRecord{}, err
	}

	var created OrderRecord
	txErr := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		now := s.now()

		expanded := make([]ExpandedItem, 0, len(req.Items))
		for _, item := range req.Items {
			expandedItem, err := expandItem(ctx, s.events, tx, item, now)
			if err != nil {
				return err
			}
			// The booking is scoped to one event; an item from another event
			// smuggled into the body must not take quota under this hold.
			if expandedItem.EventID != req.EventID {
				return apperr.BadRequest(apperr.CodeValidation,
					"All items must belong to the event being booked.")
			}
			expanded = append(expanded, expandedItem)
		}

		demand := aggregateDemand(expanded)
		for _, ttID := range sortedTicketTypeIDs(demand) {
			if err := s.events.CheckAndDeductQuota(ctx, tx, ttID, demand[ttID]); err != nil {
				if errors.Is(err, ErrInsufficientQuota) {
					return apperr.BadRequest(apperr.CodeInsufficientQuota,
						fmt.Sprintf("Only fewer than %d ticket(s) remain.", demand[ttID]))
				}
				return err
			}
		}

		subtotal := decimal.Zero
		for _, item := range expanded {
			subtotal = subtotal.Add(item.UnitPrice.Mul(decimal.NewFromInt32(item.Quantity)))
		}

		// Fees are data (clarified 2026-08-05): the active master rows are
		// applied to the subtotal and frozen onto the order here, so a later
		// fee edit never changes what this order shows or charges.
		activeFees, err := s.repo.ListActiveFees(ctx)
		if err != nil {
			return err
		}
		feeLines := computeFeeLines(subtotal, activeFees)
		total := subtotal
		for _, line := range feeLines {
			total = total.Add(line.Amount)
		}

		created, err = s.repo.CreateBookedOrder(ctx, tx, orderNumber, total, subtotal, now.Add(s.timers.BookingHold))
		if err != nil {
			return err
		}

		for i, line := range feeLines {
			if err := s.repo.CreateOrderFee(ctx, tx, created.ID, line.Name, line.Amount, int32(i+1)); err != nil {
				return err
			}
		}

		for _, item := range expanded {
			if err := s.repo.CreateOrderItem(ctx, tx, created.ID, item.Ref, item.Quantity, item.UnitPrice); err != nil {
				return err
			}
			// Empty slots, one per constituent unit, bound to the concrete ticket
			// type each will open (and the package origin for bundle lines). The
			// sorted iteration keeps slot order deterministic.
			if item.Ref.IsPackage() {
				// Unit by unit (spec 010): each purchased bundle unit gets its
				// per-unit composition of slots stamped with the unit ordinal,
				// so one visitor form later fills exactly one unit.
				for unit := int16(1); unit <= int16(item.Quantity); unit++ {
					u := unit
					for _, ttID := range sortedTicketTypeIDs(item.PerUnitDemand) {
						ref := AttendeeRef{
							TicketTypeID: ttID,
							PackageID:    item.Ref.PackageID,
							PackageUnit:  &u,
						}
						for range item.PerUnitDemand[ttID] {
							if _, err := s.repo.CreateAttendeeSlot(ctx, tx, created.ID, ref); err != nil {
								return err
							}
						}
					}
				}
				continue
			}
			for _, ttID := range sortedTicketTypeIDs(item.Demand) {
				ref := AttendeeRef{TicketTypeID: ttID}
				for range item.Demand[ttID] {
					if _, err := s.repo.CreateAttendeeSlot(ctx, tx, created.ID, ref); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if txErr != nil {
		return OrderRecord{}, txErr
	}
	return created, nil
}

// RecordAgreement stamps the durable T&C agreement on a held order
// (contracts/booking-flow.md §2). Idempotent: re-recording the same agreement
// overwrites with the same values.
func (s *Service) RecordAgreement(ctx context.Context, orderNumber string, req AgreementRequest) error {
	if !req.Agreed {
		return apperr.BadRequest(apperr.CodeTermsNotAccepted,
			"You must accept the Terms & Conditions to continue.")
	}
	if req.EventTermsID == uuid.Nil {
		return apperr.BadRequest(apperr.CodeValidation, "event_terms_id is required.")
	}

	ord, err := s.repo.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}
	if err != nil {
		return err
	}
	if ord.Status != "PENDING" ||
		(ord.PaymentExpiresAt != nil && s.now().After(*ord.PaymentExpiresAt)) {
		return apperr.New(http.StatusGone, apperr.CodeOrderExpired,
			"This order has expired. Please book again.")
	}

	eventID, err := s.repo.EventIDForOrder(ctx, ord.ID)
	if err != nil {
		return err
	}
	current, err := s.events.CurrentTerms(ctx, eventID)
	if errors.Is(err, ErrNoTerms) {
		// Terms cannot be deleted through the API, but if the document is gone the
		// guest's agreement is to something that no longer exists.
		return apperr.Conflict(apperr.CodeTermsChanged,
			"The Terms & Conditions changed. Please review the current version.")
	}
	if err != nil {
		return err
	}
	if current.ID != req.EventTermsID {
		return apperr.Conflict(apperr.CodeTermsChanged,
			"The Terms & Conditions changed while you were reading them. Please review the current version.")
	}

	updated, err := s.repo.RecordTermsAgreement(ctx, ord.ID, current.ID)
	if err != nil {
		return err
	}
	if !updated {
		// The PENDING guard matched no row: the order moved on between our read
		// and this write (sweeper, webhook).
		return apperr.New(http.StatusGone, apperr.CodeOrderExpired,
			"This order has expired. Please book again.")
	}

	s.log.InfoContext(ctx, "terms agreement recorded",
		"order_number", ord.OrderNumber, "event_terms_id", current.ID.String())
	return nil
}

// CheckoutOrder runs the Option B checkout (contracts/booking-flow.md §3):
// one call carries the buyer + visitor forms AND starts payment.
//
//	guards  order exists · PENDING · unexpired · terms recorded ·
//	        payment not already started (already started → 409004 whose data
//	        is the current QR payload, so a retried call is safe)
//	TX-D    save buyer + fill every slot (nothing was persisted before this)
//	network Gateway.CreateTransaction — outside any transaction
//	TX-P    stamp payment fields + payment_expires_at = now()+PAYMENT_WINDOW,
//	        guarded on the QR still being unset
//
// Genders returns the active gender master list for the forms' options
// (GET /ticket/genders, clarified 2026-08-05).
func (s *Service) Genders(ctx context.Context) ([]GenderOption, error) {
	records, err := s.repo.ListActiveGenders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GenderOption, 0, len(records))
	for _, r := range records {
		out = append(out, GenderOption{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

// activeGenderSet loads the gender master list as a membership set for
// Validate (clarified 2026-08-05).
func (s *Service) activeGenderSet(ctx context.Context) (map[string]bool, error) {
	records, err := s.repo.ListActiveGenders(ctx)
	if err != nil {
		return nil, err
	}
	valid := make(map[string]bool, len(records))
	for _, r := range records {
		valid[r.Name] = true
	}
	return valid, nil
}

// A gateway failure after TX-D leaves the order PENDING with its forms saved
// and the hold deadline untouched — the guest retries and only the gateway leg
// re-runs. No compensation: quota was committed at booking and stays held by
// the live order.
func (s *Service) CheckoutOrder(ctx context.Context, orderNumber string, req CheckoutFormsRequest) (CheckoutQRResponse, error) {
	validGenders, err := s.activeGenderSet(ctx)
	if err != nil {
		return CheckoutQRResponse{}, err
	}
	if err := req.Validate(validGenders); err != nil {
		return CheckoutQRResponse{}, err
	}

	ord, err := s.repo.GetOrderByNumber(ctx, orderNumber)
	if errors.Is(err, ErrNotFound) {
		return CheckoutQRResponse{}, apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}
	if err != nil {
		return CheckoutQRResponse{}, err
	}

	now := s.now()
	if ord.Status != "PENDING" ||
		(ord.PaymentExpiresAt != nil && now.After(*ord.PaymentExpiresAt)) {
		return CheckoutQRResponse{}, apperr.New(http.StatusGone, apperr.CodeOrderExpired,
			"This order can no longer be paid. Please book again.")
	}
	if ord.TermsAgreedAt == nil {
		return CheckoutQRResponse{}, apperr.Conflict(apperr.CodeTermsNotRecorded,
			"The Terms & Conditions agreement was not recorded for this order yet.")
	}
	if ord.PaymentQRString != nil && *ord.PaymentQRString != "" {
		// Idempotent retry: the payment already exists, hand back its payload as
		// the 409004's data instead of opening a second session.
		return CheckoutQRResponse{}, apperr.Conflict(apperr.CodePaymentAlreadyStarted,
			"Payment for this order has already started.").WithData(s.qrResponseFor(ord))
	}

	slots, err := s.repo.ListAttendeeSlotsByOrderID(ctx, ord.ID)
	if err != nil {
		return CheckoutQRResponse{}, err
	}
	if err := matchVisitorsToSlots(req.Attendees, slots); err != nil {
		return CheckoutQRResponse{}, err
	}
	if err := validateBundleUnitConsistency(req.Attendees, slots); err != nil {
		return CheckoutQRResponse{}, err
	}

	// Validated as parseable above.
	buyerDob, _ := time.Parse(visitorDobFormat, req.BuyerDob)

	// TX-D: buyer + every slot, atomically — a retried checkout overwrites.
	var paymentItems []PaymentItem
	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		updated, err := s.repo.UpdateOrderBuyer(ctx, tx, ord.ID, BuyerDetails{
			Name:   req.BuyerName,
			Email:  req.BuyerEmail,
			Phone:  req.BuyerPhone,
			Dob:    buyerDob,
			Gender: req.BuyerGender,
		})
		if err != nil {
			return err
		}
		if !updated {
			return apperr.New(http.StatusGone, apperr.CodeOrderExpired,
				"This order can no longer be paid. Please book again.")
		}
		for _, v := range req.Attendees {
			dob, _ := time.Parse(visitorDobFormat, v.Dob) // validated above
			filled, err := s.repo.UpdateAttendeeDetails(ctx, tx, v.ID, ord.ID, SlotDetails{
				Name: v.Name, Email: v.Email, Phone: v.Phone, Dob: dob, Gender: v.Gender,
			})
			if err != nil {
				return err
			}
			if !filled {
				return apperr.BadRequest(apperr.CodeValidation,
					"A visitor form references a slot that does not belong to this order.")
			}
		}

		// Provider line items, read inside the same tx.
		items, err := s.repo.ListOrderItemsByOrderID(ctx, ord.ID)
		if err != nil {
			return err
		}
		paymentItems = s.paymentItemsFor(ctx, tx, items)
		return nil
	}); err != nil {
		return CheckoutQRResponse{}, err
	}

	// Gateway call — outside any transaction (Constitution Principle IV).
	session, err := s.gateway.CreateTransaction(ctx, PaymentRequest{
		OrderNumber:   ord.OrderNumber,
		GrossAmount:   ord.TotalAmount,
		CustomerName:  req.BuyerName,
		CustomerEmail: req.BuyerEmail,
		CustomerPhone: req.BuyerPhone,
		Items:         paymentItems,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "payment initiation failed; order stays payable",
			"order_number", ord.OrderNumber, "provider", s.gateway.Name(), "error", err.Error())
		return CheckoutQRResponse{}, apperr.Wrap(err, http.StatusBadGateway, apperr.CodePaymentInitiationFailed,
			"We could not start the payment with the provider. Your details are saved — please try again.")
	}

	// TX-P: the server-owned 14-minute window replaces the booking hold. The
	// provider session is valid longer (PAYMENT_EXPIRY ≥ 15m, config invariant),
	// so our deadline always falls inside it.
	deadline := s.now().Add(s.timers.PaymentWindow)
	var stamped bool
	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		stamped, err = s.repo.UpdatePaymentDetailsIfUnstarted(ctx, tx, ord.ID, PaymentDetails{
			PaymentURL: session.QRImageURL,
			Provider:   s.gateway.Name(),
			QRString:   session.QRString,
			ExpiresAt:  deadline,
		})
		return err
	}); err != nil {
		// The session exists but we could not record it; the order stays PENDING
		// and a retry hits the idempotent branch or re-creates. The webhook
		// resolves whichever session is paid.
		s.log.ErrorContext(ctx, "could not persist payment details",
			"order_number", ord.OrderNumber, "error", err.Error())
		return CheckoutQRResponse{}, apperr.Wrap(err, http.StatusBadGateway, apperr.CodePaymentInitiationFailed,
			"We could not complete the payment setup. Please try again.")
	}
	if !stamped {
		// A concurrent checkout won TX-P; serve the QR it stored.
		current, err := s.repo.GetOrderByNumber(ctx, orderNumber)
		if err != nil {
			return CheckoutQRResponse{}, err
		}
		return s.qrResponseFor(current), nil
	}

	s.log.InfoContext(ctx, "checkout started payment",
		"order_number", ord.OrderNumber, "provider", s.gateway.Name(),
		"payment_expires_at", deadline.UTC())

	return CheckoutQRResponse{
		OrderID:               ord.OrderNumber,
		QRString:              session.QRString,
		ExpiresAt:             deadline.UTC(),
		QRImageURL:            TicketQRImagePath(ord.OrderNumber),
		QRRefreshAfterSeconds: int(s.timers.QRRefreshAfter.Seconds()),
	}, nil
}

// qrResponseFor rebuilds the checkout response from an order's stored payment
// fields — the idempotent-retry and lost-race branches.
func (s *Service) qrResponseFor(ord OrderRecord) CheckoutQRResponse {
	resp := CheckoutQRResponse{
		OrderID:               ord.OrderNumber,
		QRImageURL:            TicketQRImagePath(ord.OrderNumber),
		QRRefreshAfterSeconds: int(s.timers.QRRefreshAfter.Seconds()),
	}
	if ord.PaymentQRString != nil {
		resp.QRString = *ord.PaymentQRString
	}
	if ord.PaymentExpiresAt != nil {
		resp.ExpiresAt = ord.PaymentExpiresAt.UTC()
	}
	return resp
}

// paymentItemsFor builds the provider's line items from stored order lines.
// A name that cannot be resolved falls back to the order number — line labels
// are cosmetic on the provider page and must never fail a checkout.
func (s *Service) paymentItemsFor(ctx context.Context, tx pgx.Tx, items []OrderItemRecord) []PaymentItem {
	out := make([]PaymentItem, 0, len(items))
	for _, item := range items {
		p := PaymentItem{Price: item.Price, Quantity: item.Quantity}
		if item.Ref.IsPackage() {
			p.ID = item.Ref.PackageID.UUID.String()
			if pkg, err := s.events.PackageForCheckout(ctx, tx, item.Ref.PackageID.UUID); err == nil {
				p.Name = pkg.Name
			}
		} else if item.Ref.TicketTypeID.Valid {
			p.ID = item.Ref.TicketTypeID.UUID.String()
			if tt, err := s.events.TicketTypeForCheckout(ctx, tx, item.Ref.TicketTypeID.UUID); err == nil {
				p.Name = tt.Name
			}
		}
		out = append(out, p)
	}
	return out
}

// matchVisitorsToSlots requires the forms to cover the order's slots exactly:
// every slot filled, no unknown or repeated slot ids (booking-flow.md §3).
func matchVisitorsToSlots(visitors []CheckoutVisitor, slots []AttendeeSlotRecord) error {
	valid := make(map[uuid.UUID]bool, len(slots))
	for _, slot := range slots {
		valid[slot.ID] = true
	}

	fields := map[string]string{}
	for i, v := range visitors {
		if !valid[v.ID] {
			fields[fmt.Sprintf("attendees[%d].id", i)] = "This slot does not belong to the order."
		}
	}
	if len(visitors) != len(slots) && len(fields) == 0 {
		fields["attendees"] = fmt.Sprintf(
			"This order has %d ticket(s); a visitor form is required for each.", len(slots))
	}
	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"The visitor forms do not match the order's tickets.").WithData(fields)
	}
	return nil
}

// validateBundleUnitConsistency enforces spec 010 FR-003: one visitor form
// fills a whole bundle unit, so every submitted visitor mapped to slots of the
// same (package_id, package_unit) must be field-identical — the server owns
// this invariant, not client courtesy. Slots without a unit (standalone, or
// bundle slots booked before spec 010) are exempt. Runs after
// matchVisitorsToSlots, so every visitor id is known to map to a real slot.
func validateBundleUnitConsistency(visitors []CheckoutVisitor, slots []AttendeeSlotRecord) error {
	unitOf := make(map[uuid.UUID]string, len(slots))
	for _, slot := range slots {
		if slot.PackageID.Valid && slot.PackageUnit != nil {
			unitOf[slot.ID] = fmt.Sprintf("%s:%d", slot.PackageID.UUID, *slot.PackageUnit)
		}
	}

	const divergedMsg = "All tickets in the same bundle must use the same visitor information."
	first := map[string]CheckoutVisitor{}
	fields := map[string]string{}
	for i, v := range visitors {
		unit, inUnit := unitOf[v.ID]
		if !inUnit {
			continue
		}
		ref, seen := first[unit]
		if !seen {
			first[unit] = v
			continue
		}
		for _, field := range visitorFieldDiffs(ref, v) {
			fields[fmt.Sprintf("attendees[%d].%s", i, field)] = divergedMsg
		}
	}
	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"The visitor forms do not match the order's tickets.").WithData(fields)
	}
	return nil
}

// visitorFieldDiffs names the identity fields on which two visitors differ.
func visitorFieldDiffs(a, b CheckoutVisitor) []string {
	var diffs []string
	if a.Name != b.Name {
		diffs = append(diffs, "name")
	}
	if a.Email != b.Email {
		diffs = append(diffs, "email")
	}
	if a.Phone != b.Phone {
		diffs = append(diffs, "phone")
	}
	if a.Dob != b.Dob {
		diffs = append(diffs, "dob")
	}
	if a.Gender != b.Gender {
		diffs = append(diffs, "gender")
	}
	return diffs
}

// TicketQRImagePath is where the order's QR code is rendered on demand
// (spec 008 path; the handler and DTO agree on this one definition).
func TicketQRImagePath(orderNumber string) string {
	return fmt.Sprintf("/api/v1/ticket/order/%s/qris.png", orderNumber)
}

// reserve runs TX1, retrying only on an order-number collision.
func (s *Service) reserve(ctx context.Context, req CheckoutRequest) (OrderRecord, decimal.Decimal, []reservedLine, []ExpandedItem, error) {
	var lastErr error
	for attempt := range orderNumberAttempts {
		created, total, reserved, expanded, err := s.reserveOnce(ctx, req)
		if err == nil {
			return created, total, reserved, expanded, nil
		}
		if !errors.Is(err, ErrOrderNumberTaken) {
			return OrderRecord{}, decimal.Zero, nil, nil, err
		}
		lastErr = err
		s.log.WarnContext(ctx, "order number collision; retrying", "attempt", attempt+1)
	}
	return OrderRecord{}, decimal.Zero, nil, nil, fmt.Errorf("could not allocate a unique order number: %w", lastErr)
}

func (s *Service) reserveOnce(ctx context.Context, req CheckoutRequest) (OrderRecord, decimal.Decimal, []reservedLine, []ExpandedItem, error) {
	orderNumber, err := GenerateOrderNumber(s.now())
	if err != nil {
		return OrderRecord{}, decimal.Zero, nil, nil, err
	}

	var (
		created  OrderRecord
		total    decimal.Decimal
		reserved []reservedLine
		expanded []ExpandedItem
	)

	txErr := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Reset per attempt: a retried transaction must not accumulate state.
		total = decimal.Zero
		reserved = reserved[:0]
		expanded = expanded[:0]

		now := s.now()

		// Step 1+2: Resolve and validate every item server-side, expanding
		// packages into per-ticket-type demand (contracts/checkout-transaction.md §2.2).
		expanded = make([]ExpandedItem, 0, len(req.Items))
		for _, item := range req.Items {
			expandedItem, err := expandItem(ctx, s.events, tx, item, now)
			if err != nil {
				return err
			}
			expanded = append(expanded, expandedItem)
		}

		// Step 2.5: The cheap Validate pass cannot size package attendee slots
		// without the composition, so the exact per-(package_id, ticket_type_id)
		// counts are checked now, against the expansion (§2.2).
		if err := validateAttendees(req, expanded); err != nil {
			return err
		}

		// Step 3: Aggregate demand per ticket type across the ENTIRE selection
		// (§2.3). This prevents the oversell where a bundle + standalone of the
		// same ticket type are validated in isolation from each other.
		demand := aggregateDemand(expanded)

		// Step 4: Deterministic global lock order (§2.4). Two overlapping bundles
		// bought concurrently in opposing natural order must not deadlock.
		sortedIDs := sortedTicketTypeIDs(demand)

		// Step 5: Guarded deduction in sorted order (§2.5). The atomic
		// WHERE ... AND quota >= qty prevents oversell; zero rows = insufficient.
		for _, ttID := range sortedIDs {
			if err := s.events.CheckAndDeductQuota(ctx, tx, ttID, demand[ttID]); err != nil {
				if errors.Is(err, ErrInsufficientQuota) {
					return apperr.BadRequest(apperr.CodeInsufficientQuota,
						fmt.Sprintf("Only fewer than %d ticket(s) remain.", demand[ttID]))
				}
				return err
			}
			// Record what was deducted, per ticket type, for compensation and
			// payment items. Name comes from whichever expanded item contributed
			// this ticket type — for a standalone line it is the ticket's own name;
			// for a bundle component it is the ticket name from the composition.
			reserved = append(reserved, reservedLine{
				TicketTypeID: ttID,
				Name:         ticketNameFor(ttID, expanded),
				Quantity:     demand[ttID],
				UnitPrice:    priceFor(ttID, expanded),
			})
		}

		// Step 6: Persist order, order_items, attendees.
		//
		// total_amount is SUM(line quantity × server-side price), never from the
		// client (FR-006, FR-025).
		for _, item := range expanded {
			total = total.Add(item.UnitPrice.Mul(decimal.NewFromInt32(item.Quantity)))
		}

		created, err = s.repo.CreateOrder(ctx, tx, CreateOrderParams{
			OrderNumber: orderNumber,
			BuyerName:   req.BuyerName,
			BuyerEmail:  req.BuyerEmail,
			BuyerPhone:  req.BuyerPhone,
			TotalAmount: total,
		})
		if err != nil {
			return err
		}

		// One order_items row per SELECTED line, not per expanded constituent.
		// A package line is stored once at the package's own price (§2.6).
		for _, item := range expanded {
			if err := s.repo.CreateOrderItem(ctx, tx, created.ID, item.Ref, item.Quantity, item.UnitPrice); err != nil {
				return err
			}
		}

		// Attendees, one row per constituent unit. For a package line of quantity
		// q with components c1..cN, there are q × Σ(ci.quantity) attendee rows,
		// each bound to the specific ticket type it opens (§2.6).
		for _, attendee := range req.Attendees {
			ref := AttendeeRef{TicketTypeID: attendee.TicketTypeID}
			if attendee.PackageID != nil {
				ref.PackageID = uuid.NullUUID{UUID: *attendee.PackageID, Valid: true}
			}
			if _, err := s.repo.CreateAttendee(ctx, tx, created.ID, ref, attendee.Name, attendee.Email); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return OrderRecord{}, decimal.Zero, nil, nil, txErr
	}

	return created, total, reserved, expanded, nil
}

// compensate undoes a committed TX1 after the gateway call failed: the order is
// cancelled and every reserved seat is returned, in one transaction (spec FR-021).
func (s *Service) compensate(ctx context.Context, created OrderRecord, reserved []reservedLine) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cancelled, err := s.repo.UpdateOrderStatusIfPending(ctx, tx, created.ID, "CANCELLED")
		if err != nil {
			return err
		}
		if !cancelled {
			// Something else already moved the order on (a webhook that beat us
			// here). Its quota accounting is that path's responsibility, not ours.
			return nil
		}
		for _, line := range reserved {
			if err := s.events.RestoreQuota(ctx, tx, line.TicketTypeID, line.Quantity); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Nothing more can be done in-band. This is logged loudly because it is the
		// one path that can leave quota held by an order that will never be paid.
		s.log.ErrorContext(ctx, "compensation failed; quota may remain reserved",
			"order_number", created.OrderNumber, "order_id", created.ID.String(), "error", err.Error())
	}
}

// ticketNameFor returns the human name for a ticket type ID from the set of
// expanded items. For a standalone line it is the ticket's own name; for a
// bundle component it is the ticket name from the composition query.
func ticketNameFor(ttID uuid.UUID, items []ExpandedItem) string {
	for _, item := range items {
		if name, ok := item.Demand[ttID]; ok && name > 0 {
			if item.TicketName != "" {
				return item.TicketName
			}
		}
	}
	return ""
}

// priceFor returns the unit price for a ticket type ID. For standalone lines
// this is the ticket type's own price; for a bundle component it is derived
// from the package line's price. Since we record per-ticket-type holds but
// package lines store the package price, this returns the ticket type's own
// price for standalone lines and the package price divided evenly for components.
func priceFor(ttID uuid.UUID, items []ExpandedItem) decimal.Decimal {
	for _, item := range items {
		if item.TicketName != "" {
			if _, ok := item.Demand[ttID]; ok {
				return item.UnitPrice
			}
		}
	}
	return decimal.Zero
}

func (s *Service) paymentRequestFor(created OrderRecord, total decimal.Decimal, expanded []ExpandedItem, req CheckoutRequest) PaymentRequest {
	items := make([]PaymentItem, 0, len(expanded))
	for _, item := range expanded {
		name := item.TicketName
		id := ""
		if item.Ref.IsPackage() {
			name = item.PackageName
			id = item.Ref.PackageID.UUID.String()
		} else {
			id = item.Ref.TicketTypeID.UUID.String()
		}
		items = append(items, PaymentItem{
			ID:       id,
			Name:     name,
			Price:    item.UnitPrice,
			Quantity: item.Quantity,
		})
	}
	return PaymentRequest{
		OrderNumber:   created.OrderNumber,
		GrossAmount:   total,
		CustomerName:  req.BuyerName,
		CustomerEmail: req.BuyerEmail,
		CustomerPhone: req.BuyerPhone,
		Items:         items,
	}
}

// FeeLine is one computed fee at booking: the display name with any percentage
// baked in ("PPN (11%)"), and the rupiah amount it adds to the total.
type FeeLine struct {
	Name   string
	Amount decimal.Decimal
}

// computeFeeLines applies the active fee master rows to a subtotal: a PERCENT
// fee takes value% of the subtotal (rounded to 2 dp), a FIXED fee is a flat
// amount. Order is the master rows' display order.
func computeFeeLines(subtotal decimal.Decimal, fees []FeeRecord) []FeeLine {
	lines := make([]FeeLine, 0, len(fees))
	for _, fee := range fees {
		if fee.FeeType == "PERCENT" {
			lines = append(lines, FeeLine{
				Name:   fmt.Sprintf("%s (%s%%)", fee.Name, trimDecimal(fee.Value)),
				Amount: subtotal.Mul(fee.Value).Div(decimal.NewFromInt(100)).Round(2),
			})
			continue
		}
		lines = append(lines, FeeLine{Name: fee.Name, Amount: fee.Value})
	}
	return lines
}

// trimDecimal renders "11.00" as "11" and "2.50" as "2.5" — the percentage a
// guest reads, not the numeric's storage scale.
func trimDecimal(v decimal.Decimal) string {
	if v.Equal(v.Truncate(0)) {
		return v.Truncate(0).String()
	}
	if v.Equal(v.Truncate(1)) {
		return v.Truncate(1).String()
	}
	return v.String()
}
