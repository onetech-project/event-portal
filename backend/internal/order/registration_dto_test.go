package order

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

func validGenderSet() map[string]int16 { return map[string]int16{"Man": 1, "Woman": 2} }

func validRegistration() RegistrationRequest {
	return RegistrationRequest{
		Slug:                "jive-jakarta-2026",
		Name:                "Halo Registrant",
		Email:               "halo@example.com",
		Phone:               "628125567820",
		Dob:                 "1996-04-12",
		Gender:              "Man",
		Agreed:              true,
		EventTermsUpdatedAt: time.Now(),
	}
}

// fieldsOf pulls the field→message map out of a validation refusal.
func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	require.Error(t, err)
	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	fields, ok := appErr.Data.(map[string]string)
	require.True(t, ok, "validation refusal must carry a field map, got %T", appErr.Data)
	return fields
}

func TestRegistrationAcceptsAWellFormedForm(t *testing.T) {
	require.NoError(t, validRegistration().Validate(validGenderSet()))
}

func TestRegistrationRejectsAnEmptyName(t *testing.T) {
	req := validRegistration()
	req.Name = "   "
	assert.Equal(t, "Visitor name is required.", fieldsOf(t, req.Validate(validGenderSet()))["name"])
}

func TestRegistrationRejectsAMalformedEmail(t *testing.T) {
	req := validRegistration()
	req.Email = "halo@gmailcom@"
	assert.Contains(t, fieldsOf(t, req.Validate(validGenderSet())), "email")
}

// The phone rule is INHERITED from the checkout holder forms, not reinvented, and
// this pins that. The Figma comp for the registration form shows "+628125567820"
// in its filled state — that value is rejected, because the `+` is not a digit.
// Two different phone rules on two forms of one product is a defect; a guest who
// can register with a number checkout would refuse has found a bug.
func TestRegistrationInheritsTheCheckoutPhoneRuleExactly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phone string
		ok    bool
	}{
		{"12 digits", "628125567820", true},
		{"15 digits", "628125567820123", true},
		{"11 digits is too short", "08123456789", false},
		{"16 digits is too long", "6281255678201234", false},
		{"a leading plus is not a digit", "+628125567820", false},
		{"spaces are not digits", "62 812 5567 820", false},
		{"hyphens are not digits", "62-812-5567-820", false},
		{"letters are not digits", "62812556782O", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validRegistration()
			req.Phone = tc.phone
			err := req.Validate(validGenderSet())
			if tc.ok {
				require.NoError(t, err)
				return
			}
			assert.Equal(t, visitorPhoneMessage, fieldsOf(t, err)["phone"],
				"the message must be the checkout one, byte for byte")
		})
	}
}

func TestRegistrationRejectsAFutureDateOfBirth(t *testing.T) {
	req := validRegistration()
	req.Dob = time.Now().AddDate(0, 0, 1).Format(visitorDobFormat)
	assert.Equal(t, "Date of birth cannot be in the future.",
		fieldsOf(t, req.Validate(validGenderSet()))["dob"])
}

func TestRegistrationRejectsAMalformedDateOfBirth(t *testing.T) {
	req := validRegistration()
	req.Dob = "12/12/2012"
	assert.Equal(t, "Date of birth must be YYYY-MM-DD.",
		fieldsOf(t, req.Validate(validGenderSet()))["dob"])
}

func TestRegistrationRejectsAGenderOutsideTheMaster(t *testing.T) {
	req := validRegistration()
	req.Gender = "Attack Helicopter"
	assert.Equal(t, "Select a valid gender.",
		fieldsOf(t, req.Validate(validGenderSet()))["gender"])
}

// FR-024: every offending field in ONE pass, so a guest correcting all of them
// succeeds on the next attempt rather than discovering the next failure.
func TestRegistrationReportsEveryBadFieldAtOnce(t *testing.T) {
	req := validRegistration()
	req.Name = ""
	req.Email = "nope"
	req.Phone = "123"
	req.Dob = "not-a-date"
	req.Gender = "unknown"

	fields := fieldsOf(t, req.Validate(validGenderSet()))
	assert.ElementsMatch(t, []string{"name", "email", "phone", "dob", "gender"}, keysOf(fields))
}

// Consent is its own refusal with its own code, not a red input, so the client
// can respond by re-opening the document rather than marking a field.
func TestRegistrationRejectsUnacceptedTerms(t *testing.T) {
	req := validRegistration()
	req.Agreed = false

	err := req.Validate(validGenderSet())
	require.Error(t, err)
	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperr.CodeTermsNotAccepted, appErr.Code)
}

// Shape errors are reported BEFORE consent, so a guest with a bad phone number
// and an unticked box is told about both problems in the order they can fix them
// — not sent back to re-read a document over a typo.
func TestRegistrationReportsFieldErrorsBeforeTheConsentRefusal(t *testing.T) {
	req := validRegistration()
	req.Agreed = false
	req.Phone = "123"

	var appErr *apperr.Error
	require.ErrorAs(t, req.Validate(validGenderSet()), &appErr)
	assert.Equal(t, apperr.CodeValidation, appErr.Code)
}

func TestRegistrationRequiresATermsVersion(t *testing.T) {
	req := validRegistration()
	req.EventTermsUpdatedAt = time.Time{}

	var appErr *apperr.Error
	require.ErrorAs(t, req.Validate(validGenderSet()), &appErr)
	assert.Equal(t, apperr.CodeValidation, appErr.Code)
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
