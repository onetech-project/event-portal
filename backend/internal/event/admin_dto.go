package event

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// Event statuses, matching the CHECK constraint on events.status.
const (
	StatusDraft     = "DRAFT"
	StatusPublished = "PUBLISHED"
	StatusCompleted = "COMPLETED"
)

// slugPattern keeps slugs URL-safe: they appear directly in /events/:slug.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// EventAdminView is an event as the admin dashboard sees it — all statuses, unlike
// the guest-facing view.
type EventAdminView struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description *string    `json:"description"`
	Venue       string     `json:"venue"`
	Address     string     `json:"address"`
	StartDate   time.Time  `json:"start_date"`
	EndDate     time.Time  `json:"end_date"`
	BannerURL   *string    `json:"banner_url"`
	Status      string     `json:"status"`
	Scale       *int64     `json:"scale"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

// EventAdminDetail adds the event's ticket types to the admin view.
type EventAdminDetail struct {
	EventAdminView
	TicketTypes []TicketTypeAdminView `json:"ticket_types"`
}

// TicketTypeAdminView is a ticket type as the admin dashboard sees it.
//
// Quota is the REMAINING quota — the same live counter guest checkout decrements
// and cancel/expire/deny/failure restore. It is not an original allocation, and
// the admin UI must label it "Sisa Kuota / Remaining Quota" (constitution,
// Critical Data Flow Rules).
//
// Sold is derived (SUM(order_items.quantity)) and read-only; it is supplied for
// context and ignored if a client sends it back.
type TicketTypeAdminView struct {
	ID      uuid.UUID `json:"id"`
	EventID uuid.UUID `json:"event_id"`
	Name    string    `json:"name"`
	// Description replaces the booking card's standard non-refundable notice.
	Description *string     `json:"description"`
	Price       money.Money `json:"price"`
	Quota       int32       `json:"quota"`
	Sold        int         `json:"sold"`
	SalesStart  time.Time   `json:"sales_start"`
	SalesEnd    time.Time   `json:"sales_end"`
}

// EventRequest is the create/update body for an event. Both verbs take the same
// shape and validation (contracts/api.md).
//
// BannerURL is a plain URL string stored verbatim — this MVP has no upload
// endpoint and no object storage.
type EventRequest struct {
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description *string   `json:"description"`
	Venue       string    `json:"venue"`
	Address     string    `json:"address"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	BannerURL   *string   `json:"banner_url"`
	Status      string    `json:"status"`
	// Scale is the expected visitor count for the detail page's info bar
	// ("30.000+ Visitors", Figma 4-5). Optional; null clears it.
	Scale *int64 `json:"scale"`
}

// Validate checks everything decidable without the database. Slug uniqueness is
// not checked here — only the unique constraint can decide that without a race.
func (r EventRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "name is required.")
	}
	if strings.TrimSpace(r.Slug) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "slug is required.")
	}
	if !slugPattern.MatchString(r.Slug) {
		return apperr.BadRequest(apperr.CodeValidation,
			"slug must be lowercase letters, digits, and single hyphens (for example \"jazz-night-2026\").")
	}
	if strings.TrimSpace(r.Venue) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "venue is required.")
	}
	if strings.TrimSpace(r.Address) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "address is required.")
	}
	if r.StartDate.IsZero() || r.EndDate.IsZero() {
		return apperr.BadRequest(apperr.CodeValidation, "start_date and end_date are required.")
	}
	if r.EndDate.Before(r.StartDate) {
		return apperr.BadRequest(apperr.CodeInvalidDateRange, "end_date must not be before start_date.")
	}

	switch r.Status {
	case StatusDraft, StatusPublished, StatusCompleted:
	default:
		return apperr.BadRequest(apperr.CodeValidation,
			fmt.Sprintf("status must be one of %s, %s, or %s.", StatusDraft, StatusPublished, StatusCompleted))
	}
	return nil
}

// TicketTypeRequest is the create/update body for a ticket type.
//
// EventID is required on create and immutable on update, where it is ignored.
// Quota is the absolute remaining quota: the server never re-subtracts past sales
// from the submitted value.
type TicketTypeRequest struct {
	EventID uuid.UUID `json:"event_id"`
	Name    string    `json:"name"`
	// Description replaces the booking card's standard non-refundable notice for
	// this ticket. Optional: blank keeps the standard wording.
	Description *string     `json:"description"`
	Price       money.Money `json:"price"`
	Quota       int32       `json:"quota"`
	SalesStart  time.Time   `json:"sales_start"`
	SalesEnd    time.Time   `json:"sales_end"`
}

// Validate checks the fields that do not require a database lookup. Whether
// EventID refers to a real event is checked by the service.
func (r TicketTypeRequest) Validate(requireEventID bool) error {
	if requireEventID && r.EventID == uuid.Nil {
		return apperr.BadRequest(apperr.CodeValidation, "event_id is required.")
	}
	if strings.TrimSpace(r.Name) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "name is required.")
	}
	if r.Price.Decimal().IsNegative() {
		return apperr.BadRequest(apperr.CodeValidation, "price must not be negative.")
	}
	if r.Quota < 0 {
		return apperr.BadRequest(apperr.CodeValidation, "quota must not be negative.")
	}
	if r.SalesStart.IsZero() || r.SalesEnd.IsZero() {
		return apperr.BadRequest(apperr.CodeValidation, "sales_start and sales_end are required.")
	}
	if r.SalesEnd.Before(r.SalesStart) {
		return apperr.BadRequest(apperr.CodeInvalidDateRange, "sales_end must not be before sales_start.")
	}
	return nil
}

// PackageComponentRequest is one constituent line of a package create/update
// body. QuantityPerUnit is how many of the ticket one package unit consumes.
type PackageComponentRequest struct {
	TicketTypeID    uuid.UUID `json:"ticket_type_id"`
	QuantityPerUnit int32     `json:"quantity_per_unit"`
}

// PackageRequest is the create/update body for a package.
//
// EventID is required on create and immutable on update, where it is ignored.
// Price is the package's all-in price and is deliberately NOT validated against
// the sum of its constituents. There is no quota field because a package has
// none — a request that carries any quota-like field is rejected, not ignored
// (FR-036).
type PackageRequest struct {
	EventID     uuid.UUID   `json:"event_id"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Price       money.Money `json:"price"`
	SalesStart  time.Time   `json:"sales_start"`
	SalesEnd    time.Time   `json:"sales_end"`
	// Replaced the ACTIVE|INACTIVE string in migration 0013 (spec 011 FR-029).
	// A package flag has two states and no master list behind it, so a boolean
	// carries everything the strings did — unlike the order status, which keeps
	// its name on the wire because those names must round-trip.
	IsActive   bool                      `json:"is_active"`
	Components []PackageComponentRequest `json:"components"`
}

// quotaLikeKeys are request keys that would smuggle inventory onto a package.
// FR-036 says reject rather than silently ignore, because ignoring a quota field
// trains a client to believe packages hold stock.
var quotaLikeKeys = map[string]bool{
	"quota": true, "stock": true, "inventory": true, "remaining": true, "capacity": true,
}

// Validate checks everything decidable without the database, and rejects any
// quota-like field that slipped through decoding.
func (r PackageRequest) Validate(requireEventID bool) error {
	if requireEventID && r.EventID == uuid.Nil {
		return apperr.BadRequest(apperr.CodeValidation, "event_id is required.")
	}
	if strings.TrimSpace(r.Name) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "name is required.")
	}
	if r.Price.Decimal().IsNegative() {
		return apperr.BadRequest(apperr.CodeValidation, "price must not be negative.")
	}
	if r.SalesStart.IsZero() || r.SalesEnd.IsZero() {
		return apperr.BadRequest(apperr.CodeValidation, "sales_start and sales_end are required.")
	}
	if r.SalesEnd.Before(r.SalesStart) {
		return apperr.BadRequest(apperr.CodeInvalidDateRange, "sales_end must not be before sales_start.")
	}
	// No validation for is_active: a bool has no invalid value. The two-value
	// switch this replaced existed only because the column was a string.
	if len(r.Components) == 0 {
		return apperr.BadRequest(apperr.CodeValidation, "components must not be empty: a bundle needs at least one constituent.")
	}

	seen := make(map[uuid.UUID]struct{}, len(r.Components))
	for i, component := range r.Components {
		if component.TicketTypeID == uuid.Nil {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("components[%d].ticket_type_id is required.", i))
		}
		if component.QuantityPerUnit < 1 {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("components[%d].quantity_per_unit must be at least 1.", i))
		}
		if _, dup := seen[component.TicketTypeID]; dup {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("Ticket type %s appears more than once in components; combine repeats into quantity_per_unit.", component.TicketTypeID))
		}
		seen[component.TicketTypeID] = struct{}{}
	}
	return nil
}

// PackageAdminDTO is the administrator's view of a package. It too has no quota
// field — the admin surface must never present or accept one (FR-036).
type PackageAdminDTO struct {
	ID             string                `json:"id"`
	EventID        string                `json:"event_id"`
	Name           string                `json:"name"`
	Description    *string               `json:"description"`
	Price          money.Money           `json:"price"`
	SalesStart     time.Time             `json:"sales_start"`
	SalesEnd       time.Time             `json:"sales_end"`
	IsActive       bool                  `json:"is_active"`
	Components     []PackageComponentDTO `json:"components"`
	AvailableUnits int32                 `json:"available_units"`
	Sold           int32                 `json:"sold"`
	CreatedAt      *time.Time            `json:"created_at"`
	UpdatedAt      *time.Time            `json:"updated_at"`
}

// PackageAvailabilityView is the diagnostic response for a single package
// (GET /admin/packages/:id/availability): why it is or is not offered.
type PackageAvailabilityView struct {
	PackageID          string                         `json:"package_id"`
	AvailableUnits     int32                          `json:"available_units"`
	Purchasable        bool                           `json:"purchasable"`
	LimitingTicketType *PackageLimitingTicket         `json:"limiting_ticket_type"`
	Components         []PackageAvailabilityComponent `json:"components"`
}

// PackageLimitingTicket names the scarcest constituent.
type PackageLimitingTicket struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	QuotaRemaining int32  `json:"quota_remaining"`
}

// PackageAvailabilityComponent shows one constituent's contribution.
type PackageAvailabilityComponent struct {
	TicketTypeID    string `json:"ticket_type_id"`
	Name            string `json:"name"`
	QuotaRemaining  int32  `json:"quota_remaining"`
	QuantityPerUnit int32  `json:"quantity_per_unit"`
	UnitsSupported  int32  `json:"units_supported"`
}
