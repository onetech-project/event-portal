package event

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// Service holds the event domain's business rules.
//
// It also implements the quota contract the order domain depends on. That contract
// is declared by its consumer as order.EventProvider (ARCHITECTURE.md §3.2 shows
// the interface living with the consumer), so the two domains share a shape rather
// than an import edge — nothing in internal/order imports this package.
//
// The quota semantics that contract carries are worth stating once, here, because
// every caller depends on them: ticket_types.quota is the REMAINING quota, not an
// original allocation. Checkout decrements it and cancel/expire/deny/failure
// restore it (spec FR-018). SCHEMA.md defines no quota_total column, so there is
// never a total to reconcile against.
type Service struct {
	pool   db.Beginner
	repo   *Repository
	orders OrderChecker
	log    *logger.Logger
	cache  cache.Lists
}

// NewService builds the event service.
//
// orders may be nil for callers that only use the guest-facing catalog reads and
// the quota contract; the admin delete guards and sold counts require it.
//
// The cache starts as a no-op, so a service built without WithCache behaves
// exactly as it did before the cache existed — which is what every existing test
// relies on.
func NewService(pool db.Beginner, repo *Repository, orders OrderChecker, log *logger.Logger) *Service {
	return &Service{pool: pool, repo: repo, orders: orders, log: log, cache: cache.NoOp{}}
}

// WithCache installs the list cache (Constitution Principle VII). Passing nil
// leaves the no-op in place rather than panicking later on a read.
func (s *Service) WithCache(c cache.Lists) *Service {
	if c != nil {
		s.cache = c
	}
	return s
}

// ListPublishedEvents returns the guest-facing catalog.
//
// This is the highest-traffic read in the product — every guest starts here — and
// it changes only when an admin edits an event, so it is cached. Freshness comes
// from the admin write path invalidating the events scope on commit, not from
// expiry: a newly published event is visible on the very next request.
func (s *Service) ListPublishedEvents(ctx context.Context) ([]EventSummary, error) {
	return cache.Through(ctx, s.cache, cache.EventsPublicKey(),
		func(ctx context.Context) ([]EventSummary, error) {
			return s.repo.ListPublishedEvents(ctx)
		})
}

// GetPublishedEventBySlug returns one published event's content-only detail
// (clarification 2026-08-05): no ticket or package data — those load through
// TicketTypesForEventSlug / PackagesForEventSlug on the selection page only.
// Unpublished events are reported as not found so drafts stay invisible.
func (s *Service) GetPublishedEventBySlug(ctx context.Context, slug string) (EventDetail, error) {
	detail, err := s.repo.GetPublishedEventBySlug(ctx, slug)
	if errors.Is(err, ErrNotFound) {
		return EventDetail{}, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	if err != nil {
		return EventDetail{}, err
	}

	hasTerms, err := s.repo.EventHasTerms(ctx, detail.ID)
	if err != nil {
		return EventDetail{}, err
	}
	detail.HasTerms = hasTerms

	// CMS content blocks, position-ordered (T032). Three cheap indexed reads;
	// the detail page is static content, not live inventory.
	activities, err := s.repo.ListActivities(ctx, detail.ID)
	if err != nil {
		return EventDetail{}, err
	}
	detail.Activities = make([]ActivityDTO, 0, len(activities))
	for _, row := range activities {
		detail.Activities = append(detail.Activities, ActivityDTO{
			ID: row.ID, Title: row.Title, Description: row.Description,
			Icon: row.Icon, Position: row.Position,
		})
	}

	stars, err := s.repo.ListGuestStars(ctx, detail.ID)
	if err != nil {
		return EventDetail{}, err
	}
	detail.GuestStars = make([]GuestStarDTO, 0, len(stars))
	for _, row := range stars {
		detail.GuestStars = append(detail.GuestStars, GuestStarDTO{
			ID: row.ID, Name: row.Name, Position: row.Position,
		})
	}

	guidelines, err := s.repo.ListGuidelines(ctx, detail.ID)
	if err != nil {
		return EventDetail{}, err
	}
	detail.Guidelines = make([]GuidelineDTO, 0, len(guidelines))
	for _, row := range guidelines {
		detail.Guidelines = append(detail.Guidelines, GuidelineDTO{
			ID: row.ID, Description: row.Description, Icon: row.Icon, Position: row.Position,
		})
	}
	return detail, nil
}

// TermsForEventSlug returns the published event's current Terms & Conditions
// document (GET /ticket/terms-condition/:event_id).
//
// The two failure modes carry distinct codes on purpose: an unknown or
// unpublished event is 404001, while a known event whose organizer has not
// authored terms yet is 404002 — the dialog words those differently.
func (s *Service) TermsForEventSlug(ctx context.Context, slug string) (EventTermsDTO, error) {
	detail, err := s.repo.GetPublishedEventBySlug(ctx, slug)
	if errors.Is(err, ErrNotFound) {
		return EventTermsDTO{}, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	if err != nil {
		return EventTermsDTO{}, err
	}

	row, err := s.repo.GetEventTermsByEventID(ctx, detail.ID)
	if errors.Is(err, ErrNotFound) {
		return EventTermsDTO{}, apperr.NotFound(apperr.CodeTermsMissing,
			"This event has no Terms & Conditions yet.")
	}
	if err != nil {
		return EventTermsDTO{}, err
	}
	return EventTermsDTO{ID: row.ID, Content: row.Content, UpdatedAt: row.UpdatedAt}, nil
}

// TicketTypesForEventSlug returns the sellable ticket types of a published
// event with live remaining quota (GET /ticket/:event_id).
func (s *Service) TicketTypesForEventSlug(ctx context.Context, slug string) ([]TicketTypeSummary, error) {
	detail, err := s.repo.GetPublishedEventBySlug(ctx, slug)
	if errors.Is(err, ErrNotFound) {
		return nil, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	if err != nil {
		return nil, err
	}

	// Cached per event. QuotaRemaining is live inventory, so this list is only
	// safe to cache because every quota movement invalidates the event scope on
	// commit — booking, cancellation, expiry, denial and failure alike. The
	// number here is a DISPLAY value: no sale is ever authorised against it, only
	// against the row-locked UPDATE in the booking transaction (Principle VII).
	return cache.Through(ctx, s.cache, cache.TicketTypesPublicKey(detail.ID),
		func(ctx context.Context) ([]TicketTypeSummary, error) {
			rows, err := s.repo.ListTicketTypesByEventID(ctx, detail.ID)
			if err != nil {
				return nil, err
			}

			out := make([]TicketTypeSummary, 0, len(rows))
			for _, row := range rows {
				out = append(out, TicketTypeSummary{
					ID:             row.ID,
					Name:           row.Name,
					Description:    row.Description,
					Price:          money.From(row.Price),
					QuotaRemaining: row.Quota,
					SalesStart:     row.SalesStart,
					SalesEnd:       row.SalesEnd,
					EventStart:     row.EventStart,
					EventEnd:       row.EventEnd,
				})
			}
			return out, nil
		})
}

// PackagesForEventSlug returns the ACTIVE packages of a published event with
// derived availability (GET /packages/:event_id).
func (s *Service) PackagesForEventSlug(ctx context.Context, slug string) ([]PackageSummaryDTO, error) {
	detail, err := s.repo.GetPublishedEventBySlug(ctx, slug)
	if errors.Is(err, ErrNotFound) {
		return nil, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	if err != nil {
		return nil, err
	}
	return s.PackagesForEvent(ctx, detail.ID)
}

// PackagesForEvent returns every ACTIVE package of an event as public summary
// rows with derived availability, in exactly two queries regardless of package
// count. Used by GET /events/:slug and the availability-only polling route.
func (s *Service) PackagesForEvent(ctx context.Context, eventID uuid.UUID) ([]PackageSummaryDTO, error) {
	// Same reasoning as the ticket list: AvailableUnits is derived from the
	// constituents' remaining quota, so it moves with every booking and every
	// restore, and every one of those bumps this event's scope.
	return cache.Through(ctx, s.cache, cache.PackagesByEventKey(eventID),
		func(ctx context.Context) ([]PackageSummaryDTO, error) {
			rows, err := s.repo.ListPackagesWithAvailabilityByEventID(ctx, eventID)
			if err != nil {
				return nil, err
			}

			// Only ACTIVE packages appear to guests; inactive ones are admin-visible only.
			filtered := rows[:0]
			for _, row := range rows {
				if row.IsActive {
					filtered = append(filtered, row)
				}
			}

			return s.assemblePackageSummaries(ctx, filtered)
		})
}

// assemblePackageSummaries attaches batched components and maps rows to the
// public DTO. Rows already carry availability, so this never re-queries it.
func (s *Service) assemblePackageSummaries(ctx context.Context, rows []PackageRowWithAvailability) ([]PackageSummaryDTO, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	components, err := s.repo.ListPackageComponentsByPackageIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byPackage := make(map[uuid.UUID][]PackageComponentRow, len(rows))
	for _, c := range components {
		byPackage[c.PackageID] = append(byPackage[c.PackageID], c)
	}

	out := make([]PackageSummaryDTO, 0, len(rows))
	for _, row := range rows {
		purchasable := false
		if row.Purchasable != nil {
			purchasable = *row.Purchasable
		}
		packageComponents := byPackage[row.ID]
		componentDTOs := make([]PackageComponentDTO, 0, len(packageComponents))
		for _, c := range packageComponents {
			componentDTOs = append(componentDTOs, PackageComponentDTO{
				TicketTypeID:    c.TicketTypeID.String(),
				TicketTypeName:  c.Name,
				QuantityPerUnit: c.QuantityPerUnit,
			})
		}
		out = append(out, PackageSummaryDTO{
			ID:             row.ID.String(),
			Name:           row.Name,
			Description:    row.Description,
			Price:          money.From(row.Price),
			SalesStart:     row.SalesStart,
			SalesEnd:       row.SalesEnd,
			AvailableUnits: row.AvailableUnits,
			Purchasable:    purchasable,
			Components:     componentDTOs,
		})
	}
	return out, nil
}

// --- Quota contract (consumed by the order domain) ------------------------

// CheckAndDeductQuota atomically reserves qty seats on a ticket type inside the
// caller's transaction, returning an insufficient-quota error when the remaining
// quota cannot cover the request.
func (s *Service) CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	return s.repo.CheckAndDeductQuota(ctx, tx, ticketTypeID, qty)
}

// RestoreQuota returns qty seats to a ticket type inside the caller's transaction.
func (s *Service) RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	return s.repo.RestoreQuota(ctx, tx, ticketTypeID, qty)
}

// CurrentTermsForEvent returns the identity of the event's current Terms &
// Conditions document, or ErrNotFound when none is authored. The order domain
// consumes this through its EventProvider contract: booking refuses events
// without terms, and agreement recording pins the id the guest actually saw.
func (s *Service) CurrentTermsForEvent(ctx context.Context, eventID uuid.UUID) (TermsRow, error) {
	return s.repo.GetEventTermsByEventID(ctx, eventID)
}

// GetTicketTypeForCheckout returns the current server-side price and sales window
// for a ticket type, so checkout never trusts a client-supplied total. It reads
// inside the caller's transaction.
func (s *Service) GetTicketTypeForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (TicketTypeRow, error) {
	row, err := s.repo.GetTicketTypeByID(ctx, tx, id)
	if errors.Is(err, ErrNotFound) {
		return TicketTypeRow{}, apperr.BadRequest(apperr.CodeTicketTypeNotFound,
			fmt.Sprintf("Ticket type %s does not exist.", id))
	}
	if err != nil {
		return TicketTypeRow{}, err
	}
	return row, nil
}
