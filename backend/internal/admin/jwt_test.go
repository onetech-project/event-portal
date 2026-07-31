package admin_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/admin"
)

const testSecret = "test-secret-that-is-long-enough!!"

func TestIssueThenVerifyRoundTrips(t *testing.T) {
	issuer := admin.NewTokenIssuer(testSecret, time.Hour)
	id := uuid.New()

	token, expiresAt, err := issuer.Issue(id, "admin@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, 5*time.Second)

	claims, err := issuer.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, id, claims.AdminID)
	assert.Equal(t, "admin@example.com", claims.Email)
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	issuer := admin.NewTokenIssuer(testSecret, -time.Minute)

	token, _, err := issuer.Issue(uuid.New(), "admin@example.com")
	require.NoError(t, err)

	_, err = issuer.Verify(token)
	require.Error(t, err)
}

func TestVerifyRejectsTokenSignedWithAnotherSecret(t *testing.T) {
	token, _, err := admin.NewTokenIssuer("a-completely-different-secret-key", time.Hour).
		Issue(uuid.New(), "admin@example.com")
	require.NoError(t, err)

	_, err = admin.NewTokenIssuer(testSecret, time.Hour).Verify(token)

	require.Error(t, err)
}

func TestVerifyRejectsGarbage(t *testing.T) {
	_, err := admin.NewTokenIssuer(testSecret, time.Hour).Verify("not.a.token")
	require.Error(t, err)
}

// A token whose header claims an asymmetric algorithm must not be accepted by an
// HMAC verifier — the classic algorithm-confusion attack.
func TestVerifyRejectsAlgorithmConfusion(t *testing.T) {
	forged := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": uuid.New().String(),
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	raw, err := forged.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = admin.NewTokenIssuer(testSecret, time.Hour).Verify(raw)

	require.Error(t, err)
}

func TestVerifyRejectsTokenWithNonUUIDSubject(t *testing.T) {
	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "not-a-uuid",
		"email": "admin@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	raw, err := forged.SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = admin.NewTokenIssuer(testSecret, time.Hour).Verify(raw)

	require.Error(t, err)
}
