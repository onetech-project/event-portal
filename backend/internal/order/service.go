package order

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// orderNumberAttempts bounds the retry loop for order-number collisions. The
// suffix has ~1e9 combinations per day, so needing even a second attempt is
// already vanishingly unlikely; the bound exists so a pathological failure surfaces
// instead of spinning.
const orderNumberAttempts = 5

// Timers are the booking deadlines this domain owns (config-backed:
// BOOKING_HOLD / PAYMENT_WINDOW).
//
// PaymentWindow is no longer among them in the payment phase: the gateway
// returns the deadline it will enforce and checkout adopts it verbatim
// (FR-009). The value survives here only as the fallback the payment adapter
// substitutes when nothing usable came back.
type Timers struct {
	BookingHold   time.Duration
	PaymentWindow time.Duration
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
	cache   cache.Lists
	// payments reads back what a started payment was recorded under. Optional:
	// nil means the checkout response carries no external reference, which is the
	// same degradation as an order that never opened a session.
	payments PaymentRecords
	// issuer and deliverer are the free-registration fulfilment seams (spec 022).
	// Nil until the composition root installs them; a registration recorded
	// without them logs loudly rather than silently issuing nothing.
	issuer    TicketIssuer
	deliverer TicketDeliverer
	// fulfillment tracks in-flight post-commit registration work, so graceful
	// shutdown drains a registration accepted moments before SIGTERM instead of
	// dropping its ticket on the floor.
	fulfillment sync.WaitGroup
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
		cache:   cache.NoOp{},
		timers: Timers{
			BookingHold:   time.Hour,
			PaymentWindow: 15 * time.Minute,
		},
	}
}

// WithTimers overrides the booking deadlines from config. Returns the service
// for chaining at the composition root.
func (s *Service) WithTimers(t Timers) *Service {
	s.timers = t
	return s
}

// WithPaymentRecords installs the read-back seam for a started payment's
// external reference (spec 017). Optional by construction: without it checkout
// still works and simply reports no reference, which is what a test that does
// not care about the reference should get.
func (s *Service) WithPaymentRecords(p PaymentRecords) *Service {
	if p != nil {
		s.payments = p
	}
	return s
}

// WithCache installs the list cache (Constitution Principle VII). Booking moves
// quota, so this domain invalidates the event scope as well as the order one.
func (s *Service) WithCache(c cache.Lists) *Service {
	if c != nil {
		s.cache = c
	}
	return s
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
	txErr := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
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
		// fee edit never changes what this order shows or charges. Read on
		// THIS transaction — a pool read here would take a second connection
		// while quota row locks are held, starving the pool under load.
		activeFees, err := s.repo.ListActiveFeesTx(ctx, tx)
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

		// The highest-frequency invalidation in the system, and the one that most
		// needs to stay outside the lock window.
		//
		// This registers the work; it does not do it. The quota-deducting UPDATE
		// above holds a row lock until COMMIT, so issuing a Redis command here
		// would serialise every concurrent buyer of the same ticket type behind a
		// network round-trip — the same collapse Principle IV bans gateway calls
		// to prevent, and what Principle VII's transaction-boundary rule forbids.
		// InvalidateAfterCommit defers it past COMMIT; the cache client also
		// refuses outright on a transaction context, so a future refactor that
		// inlines it fails loudly instead of quietly halving checkout throughput.
		//
		// Both scopes: quota moved (event) and an order appeared (orders).
		cache.InvalidateAfterCommit(ctx, s.cache, cache.Orders(), cache.Event(req.EventID))
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
	// The id names WHICH document; updated_at names WHICH VERSION. Only the
	// second can detect an edit — see AgreementRequest.EventTermsUpdatedAt.
	//
	// Truncated to the second because the value round-trips through JSON and
	// back, and sub-second drift is not a document change.
	staleDocument := current.ID != req.EventTermsID
	staleVersion := req.EventTermsUpdatedAt != nil &&
		!current.UpdatedAt.Truncate(time.Second).Equal(req.EventTermsUpdatedAt.UTC().Truncate(time.Second))
	if staleDocument || staleVersion {
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

// genderMaps loads the gender master list for checkout: every NAME → its id,
// plus the set of names still offered to new forms.
//
// Two maps rather than one because checkout now asks two different questions
// (spec 011 FR-031, clarified 2026-08-19). "Does this name exist" is a shape
// check and uses `known`, which includes retired entries — a slot keeps whatever
// gender it was saved with, so a restored form can legitimately submit one.
// "May THIS slot use it" is a separate rule applied once the slots are loaded.
//
// `known` is also what resolves the stored gender_id (FR-018): the active-only
// map yields the zero value for a retired name, which writes an invalid foreign
// key rather than refusing.
func (s *Service) genderMaps(ctx context.Context) (known map[string]int16, active map[string]struct{}, err error) {
	records, err := s.repo.ListGenders(ctx)
	if err != nil {
		return nil, nil, err
	}
	known = make(map[string]int16, len(records))
	active = make(map[string]struct{}, len(records))
	for _, r := range records {
		known[r.Name] = r.ID
		if r.IsActive {
			active[r.Name] = struct{}{}
		}
	}
	return known, active, nil
}

// A gateway failure after TX-D leaves the order PENDING with its forms saved
// and the hold deadline untouched — the guest retries and only the gateway leg
// re-runs. No compensation: quota was committed at booking and stays held by
// the live order.
func (s *Service) CheckoutOrder(ctx context.Context, orderNumber string, req CheckoutFormsRequest) (CheckoutQRResponse, error) {
	knownGenders, activeGenders, err := s.genderMaps(ctx)
	if err != nil {
		return CheckoutQRResponse{}, err
	}
	if err := req.Validate(knownGenders); err != nil {
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
			"Payment for this order has already started.").WithData(s.qrResponseFor(ctx, ord))
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
	if err := validateRetiredGenders(req.Attendees, slots, activeGenders); err != nil {
		return CheckoutQRResponse{}, err
	}

	// The primary contact is the holder of the TOPMOST form: the visitor mapped
	// to the first slot in canonical slot order — never attendees[0] of the
	// client-controlled array (spec 011, contract §1).
	primary := primaryContact(req.Attendees, slots)

	// TX-D: primary-contact snapshot + every slot, atomically — a retried
	// checkout overwrites.
	var paymentItems []PaymentItem
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		updated, err := s.repo.UpdateOrderBuyer(ctx, tx, ord.ID, BuyerDetails{
			Name:  primary.Name,
			Email: primary.Email,
			Phone: primary.Phone,
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
				Name: v.Name, Email: v.Email, Phone: v.Phone, Dob: dob,
				// Membership was validated above, so the name always resolves —
				// from the FULL map, because a retired gender that its own slot
				// already carried is accepted and still needs its real id.
				GenderID: knownGenders[v.Gender],
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

	// Gateway call — outside any transaction (Constitution Principle IV). The
	// customer is the primary contact (spec 011, FR-017).
	session, err := s.gateway.CreateTransaction(ctx, PaymentRequest{
		OrderNumber:   ord.OrderNumber,
		GrossAmount:   ord.TotalAmount,
		CustomerName:  primary.Name,
		CustomerEmail: primary.Email,
		CustomerPhone: primary.Phone,
		Items:         paymentItems,
	})
	if errors.Is(err, ErrGatewaySessionDuplicate) {
		// The gateway has already issued a code for this reference and will not
		// issue another, and nothing can fetch the existing one. The order is
		// unpayable from birth: the guest will never be shown anything to scan.
		//
		// The seats have already been released by the time this returns — holding
		// them to a deadline that cannot end in a payment would only keep them from
		// someone who could actually buy them.
		s.log.ErrorContext(ctx, "gateway refused a duplicate reference; order cannot be paid",
			"order_number", ord.OrderNumber, "provider", s.gateway.Name(), "error", err.Error())
		return CheckoutQRResponse{}, apperr.Wrap(err, http.StatusConflict, apperr.CodePaymentSessionDuplicate,
			"This order could not be set up for payment and its seats have been released. Please book again.")
	}
	if err != nil {
		s.log.ErrorContext(ctx, "payment initiation failed; order stays payable",
			"order_number", ord.OrderNumber, "provider", s.gateway.Name(), "error", err.Error())
		return CheckoutQRResponse{}, apperr.Wrap(err, http.StatusBadGateway, apperr.CodePaymentInitiationFailed,
			"We could not start the payment with the provider. Your details are saved — please try again.")
	}

	// TX-P: the gateway's own deadline replaces the booking hold (FR-009).
	//
	// This is the change that removes a whole class of disagreement. Previously
	// each side computed a deadline and we relied on ours being the shorter; now
	// there is one deadline, the gateway enforces it, and the countdown the guest
	// watches is the same instant the code actually stops working. The adapter
	// guarantees ExpiresAt is set even when the gateway returned nothing usable —
	// it substitutes the fallback and signals — so there is no zero value to
	// defend against here.
	deadline := session.ExpiresAt
	var stamped bool
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		stamped, err = s.repo.UpdatePaymentDetailsIfUnstarted(ctx, tx, ord.ID, PaymentDetails{
			PaymentURL: session.QRImageURL,
			Provider:   s.gateway.Name(),
			QRString:   session.QRString,
			ExpiresAt:  deadline,
		})
		if err != nil {
			return err
		}
		// The admin order table shows provider, payment deadline and buyer
		// details, all of which this checkout wrote. Orders scope only — no quota
		// moved here.
		cache.InvalidateAfterCommit(ctx, s.cache, cache.Orders())
		return nil
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
		return s.qrResponseFor(ctx, current), nil
	}

	s.log.InfoContext(ctx, "checkout started payment",
		"order_number", ord.OrderNumber, "provider", s.gateway.Name(),
		"payment_expires_at", deadline.UTC(),
		// Which side the deadline came from, recorded at the moment it is stamped.
		// A fallback deadline is one the gateway never agreed to, so the two sides
		// can disagree about when the code stops working — worth being able to see
		// in the logs of the order that misbehaved, not only in the adapter's.
		"expiry_from_gateway", session.ExpiryFromGateway)

	return CheckoutQRResponse{
		OrderID:    ord.OrderNumber,
		QRString:   session.QRString,
		ExpiresAt:  deadline.UTC(),
		QRImageURL: TicketQRImagePath(ord.OrderNumber),
		// Straight from the gateway's answer: this is the branch that opened the
		// session, so the reference is already in hand and no read-back is needed
		// to know which session the guest is about to pay.
		ExtRefID: session.ProviderRef,
	}, nil
}

// qrResponseFor rebuilds the checkout response from an order's stored payment
// fields — the idempotent-retry and lost-race branches.
//
// The external reference is READ rather than taken from the caller's own
// session, and that distinction is load-bearing on the lost-race branch: the
// caller there holds a session the order did not keep, so its own reference
// would name the wrong one. Reading gives both branches the session the guest is
// actually paying (FR-011).
//
// A missing provider or a failed lookup costs the reference and nothing else. It
// is a support identifier; refusing to answer a checkout because one could not
// be read would trade a payable order for an audit convenience.
func (s *Service) qrResponseFor(ctx context.Context, ord OrderRecord) CheckoutQRResponse {
	resp := CheckoutQRResponse{
		OrderID:    ord.OrderNumber,
		QRImageURL: TicketQRImagePath(ord.OrderNumber),
	}
	if ord.PaymentQRString != nil {
		resp.QRString = *ord.PaymentQRString
	}
	if ord.PaymentExpiresAt != nil {
		resp.ExpiresAt = ord.PaymentExpiresAt.UTC()
	}
	if s.payments != nil {
		ref, err := s.payments.ExternalRefForOrder(ctx, ord.ID)
		if err != nil {
			s.log.ErrorContext(ctx, "could not read the payment's external reference; answering without it",
				"order_number", ord.OrderNumber, "error", err.Error())
		}
		resp.ExtRefID = ref
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

// primaryContact returns the visitor filling the FIRST slot in canonical slot
// order — the topmost form on screen (standalone slots sort before bundle
// slots, spec 010 §1), which spec 011 makes the order's primary contact. Runs
// after matchVisitorsToSlots, so the first slot's visitor is guaranteed to
// exist.
func primaryContact(visitors []CheckoutVisitor, slots []AttendeeSlotRecord) CheckoutVisitor {
	if len(slots) == 0 {
		return CheckoutVisitor{}
	}
	for _, v := range visitors {
		if v.ID == slots[0].ID {
			return v
		}
	}
	return CheckoutVisitor{}
}

// validateRetiredGenders enforces spec 011 FR-031 (clarified 2026-08-19): a
// gender that is no longer offered may still be submitted, but only by the slot
// that already carries it.
//
// This is what makes the restored form submittable. The master list serves
// active entries only while a slot keeps whatever it was saved with, so a guest
// returning to a form whose gender was retired in the meantime would otherwise
// be refused for a value they never chose and cannot see is wrong. Allowing it
// anywhere else would let a submission put a retired gender on a fresh slot,
// which is the master list's retirement being undone one order at a time.
//
// Runs after matchVisitorsToSlots, so every visitor id maps to a real slot.
func validateRetiredGenders(
	visitors []CheckoutVisitor,
	slots []AttendeeSlotRecord,
	activeGenders map[string]struct{},
) error {
	held := make(map[uuid.UUID]string, len(slots))
	for _, slot := range slots {
		if slot.Gender != nil {
			held[slot.ID] = *slot.Gender
		}
	}

	fields := map[string]string{}
	for i, v := range visitors {
		if _, ok := activeGenders[v.Gender]; ok {
			continue
		}
		if held[v.ID] != v.Gender {
			// Same code, key and text as the ordinary refusal in Validate: from
			// the guest's side this is one rule about one field, and a second
			// wording would only tell them the server has two.
			fields[fmt.Sprintf("attendees[%d].gender", i)] = "Select a valid gender."
		}
	}
	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"Some fields are missing or invalid.").WithData(fields)
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
