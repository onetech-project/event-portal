package order

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// RegistrationPrereqs is the 200 body of GET /ticket/register/:ticket_type_id —
// everything the registration form needs to render itself (spec 022 FR-010).
//
// Deliberately carries NO price, no fee and no quota figure. FR-015 forbids the
// first two on this surface, and a quota number would be live inventory the page
// must neither display nor gate on (Principle VII: an availability figure "MUST
// NOT gate, authorize, or short-circuit a sale").
type RegistrationPrereqs struct {
	TicketTypeID   uuid.UUID         `json:"ticket_type_id"`
	TicketTypeName string            `json:"ticket_type_name"`
	Event          RegistrationEvent `json:"event"`
	// EventTermsID names WHICH document; EventTermsUpdatedAt names WHICH VERSION.
	// Both are needed and they are not interchangeable — see the submit request.
	EventTermsID        uuid.UUID      `json:"event_terms_id"`
	EventTermsUpdatedAt time.Time      `json:"event_terms_updated_at"`
	Genders             []GenderOption `json:"genders"`
}

// RegistrationEvent is the event identity the form displays.
type RegistrationEvent struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// RegistrationRequest is the submit body of
// POST /ticket/register/:ticket_type_id: exactly one holder, plus the consent.
//
// One holder, not a list. A registration issues exactly one ticket (spec 022
// FR-028); registering a group means submitting the form once per person.
type RegistrationRequest struct {
	// Slug scopes the ticket type to an event, so a type id smuggled from another
	// event cannot be registered against this one.
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Dob   string `json:"dob"`
	// GenderID is the master entry's identifier (spec 022 FR-022, clarified
	// 2026-08-24), matching the holder forms. The NAME remains what the guest is
	// shown; it is no longer what the form submits.
	GenderID int16 `json:"gender_id"`
	Agreed   bool  `json:"agreed"`
	// EventTermsUpdatedAt is the VERSION of the Terms & Conditions the guest
	// actually read, echoed back from the prereqs call.
	//
	// It is the updated_at, NOT the row id, and that is load-bearing rather than
	// stylistic. `UpsertEventTerms` is `ON CONFLICT (event_id) DO UPDATE` and
	// `event_terms.event_id` is UNIQUE, so an admin edit overwrites in place and
	// PRESERVES the id — a comparison of ids can never detect an edit. (The same
	// discovery fixed a long-standing dead check in the booking flow's
	// RecordAgreement; see spec 022 research D8.)
	EventTermsUpdatedAt time.Time `json:"event_terms_updated_at"`
}

// RegistrationResponse is the 201 body. Deliberately minimal: FR-039 settles that
// the confirmation page shows no address and no reference, so returning either
// would put data on the wire that nothing may render.
type RegistrationResponse struct {
	Registered bool `json:"registered"`
}

// Validate checks everything decidable without the database, reporting EVERY
// offending field in one pass (FR-024) so a guest never fixes one field only to
// be told about the next.
//
// Every rule here is the checkout holder-form rule, reused rather than restated:
// the same phone pattern, the same phone message byte-for-byte, the same email
// shape test, the same date format and future check, the same gender master. Two
// different rules on two forms of one product is a defect, not a feature — a
// guest who can register with a phone number checkout would reject has found a
// bug, whichever way round it fails.
func (r RegistrationRequest) Validate(activeGenderIDs map[int16]struct{}) error {
	fields := map[string]string{}

	if strings.TrimSpace(r.Name) == "" {
		fields["name"] = "Visitor name is required."
	}
	if !emailShaped(r.Email) {
		fields["email"] = "A valid email address is required."
	}
	if !visitorPhonePattern.MatchString(strings.TrimSpace(r.Phone)) {
		fields["phone"] = visitorPhoneMessage
	}
	if dob, err := time.Parse(visitorDobFormat, r.Dob); err != nil {
		fields["dob"] = "Date of birth must be YYYY-MM-DD."
	} else if dob.After(time.Now()) {
		fields["dob"] = "Date of birth cannot be in the future."
	}
	if _, ok := activeGenderIDs[r.GenderID]; !ok {
		// Keyed "gender", not "gender_id": the guest sees a gender select, and
		// the identifier is a submission detail they have no use for.
		fields["gender"] = "Select a valid gender."
	}

	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"Some fields are missing or invalid.").WithData(fields)
	}

	// Consent is not a field failure: it is its own refusal with its own code, the
	// same one booking uses, so a client can react to it specifically by
	// re-opening the document rather than marking an input red.
	if !r.Agreed {
		return apperr.BadRequest(apperr.CodeTermsNotAccepted,
			"You must accept the Terms & Conditions to continue.")
	}
	if r.EventTermsUpdatedAt.IsZero() {
		return apperr.BadRequest(apperr.CodeValidation,
			"event_terms_updated_at is required.")
	}

	return nil
}
