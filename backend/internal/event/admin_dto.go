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
	ID         uuid.UUID   `json:"id"`
	EventID    uuid.UUID   `json:"event_id"`
	Name       string      `json:"name"`
	Price      money.Money `json:"price"`
	Quota      int32       `json:"quota"`
	Sold       int         `json:"sold"`
	SalesStart time.Time   `json:"sales_start"`
	SalesEnd   time.Time   `json:"sales_end"`
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
	EventID    uuid.UUID   `json:"event_id"`
	Name       string      `json:"name"`
	Price      money.Money `json:"price"`
	Quota      int32       `json:"quota"`
	SalesStart time.Time   `json:"sales_start"`
	SalesEnd   time.Time   `json:"sales_end"`
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
