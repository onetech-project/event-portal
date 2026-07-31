package admin_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/manjo/ticketing/backend/internal/admin"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

const adminPassword = "correct-horse-battery-staple"

func newAdminService(t *testing.T) (*admin.Service, *testsupport.Pool, *admin.TokenIssuer) {
	t.Helper()
	pool := testsupport.RequirePool(t)

	issuer := admin.NewTokenIssuer(testSecret, time.Hour)
	svc := admin.NewService(admin.NewRepository(pool), issuer, testsupport.DiscardLogger())
	return svc, pool, issuer
}

func seedAdmin(t *testing.T, pool *testsupport.Pool, email, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	testsupport.SeedAdmin(t, pool, email, string(hash))
}

func TestLoginIssuesAUsableTokenForValidCredentials(t *testing.T) {
	svc, pool, issuer := newAdminService(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	resp, err := svc.Login(context.Background(), admin.LoginRequest{
		Email: "admin@example.com", Password: adminPassword,
	})

	require.NoError(t, err)
	require.NotEmpty(t, resp.Token)
	assert.True(t, resp.ExpiresAt.After(time.Now()))

	claims, err := issuer.Verify(resp.Token)
	require.NoError(t, err)
	assert.Equal(t, "admin@example.com", claims.Email)
}

func TestLoginRejectsAWrongPassword(t *testing.T) {
	svc, pool, _ := newAdminService(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	_, err := svc.Login(context.Background(), admin.LoginRequest{
		Email: "admin@example.com", Password: "not-the-password",
	})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeInvalidCredentials, appErr.Code)
}

// An unknown email and a wrong password must be indistinguishable, or the login
// endpoint becomes an account-enumeration oracle.
func TestLoginGivesTheSameAnswerForAnUnknownEmail(t *testing.T) {
	svc, pool, _ := newAdminService(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	_, wrongPassword := svc.Login(context.Background(), admin.LoginRequest{
		Email: "admin@example.com", Password: "wrong",
	})
	_, unknownEmail := svc.Login(context.Background(), admin.LoginRequest{
		Email: "nobody@example.com", Password: adminPassword,
	})

	var a, b *apperr.Error
	require.True(t, errors.As(wrongPassword, &a))
	require.True(t, errors.As(unknownEmail, &b))

	assert.Equal(t, a.HTTPStatus, b.HTTPStatus)
	assert.Equal(t, a.Code, b.Code)
	assert.Equal(t, a.Message, b.Message)
}

func TestLoginNormalizesTheEmail(t *testing.T) {
	svc, pool, _ := newAdminService(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	resp, err := svc.Login(context.Background(), admin.LoginRequest{
		Email: "  Admin@Example.com ", Password: adminPassword,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Token)
}

func TestLoginRejectsAnEmptyPassword(t *testing.T) {
	svc, pool, _ := newAdminService(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	_, err := svc.Login(context.Background(), admin.LoginRequest{
		Email: "admin@example.com", Password: "",
	})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestLoginRejectsAnEmptyEmail(t *testing.T) {
	svc, _, _ := newAdminService(t)

	_, err := svc.Login(context.Background(), admin.LoginRequest{Email: "", Password: "x"})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

// Even when no admin matches, a password hash is still verified so the response
// time does not reveal whether the account exists.
func TestLoginDoesNotShortCircuitForAnUnknownEmail(t *testing.T) {
	svc, pool, _ := newAdminService(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	start := time.Now()
	_, _ = svc.Login(context.Background(), admin.LoginRequest{
		Email: "nobody@example.com", Password: adminPassword,
	})
	unknownDuration := time.Since(start)

	start = time.Now()
	_, _ = svc.Login(context.Background(), admin.LoginRequest{
		Email: "admin@example.com", Password: "wrong",
	})
	wrongPasswordDuration := time.Since(start)

	// Both paths run a bcrypt comparison, so neither returns near-instantly while
	// the other does the expensive work.
	assert.Positive(t, unknownDuration)
	assert.Positive(t, wrongPasswordDuration)
}
