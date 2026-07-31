package admin

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// invalidCredentialsMessage is deliberately identical for an unknown email and a
// wrong password. Distinguishing them would turn login into an account-enumeration
// oracle.
const invalidCredentialsMessage = "Email or password is incorrect."

// dummyHash is a valid bcrypt hash of a value nobody knows. When no admin matches
// the submitted email, the password is compared against this instead of returning
// early, so both failure paths take comparable time and the response cannot reveal
// whether the account exists.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// Service implements admin authentication.
type Service struct {
	repo   *Repository
	issuer *TokenIssuer
	log    *logger.Logger
}

// NewService builds the admin service.
func NewService(repo *Repository, issuer *TokenIssuer, log *logger.Logger) *Service {
	return &Service{repo: repo, issuer: issuer, log: log}
}

// Login verifies credentials and issues an access token.
func (s *Service) Login(ctx context.Context, req LoginRequest) (LoginResponse, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))

	account, err := s.repo.GetByEmail(ctx, email)
	switch {
	case errors.Is(err, ErrNotFound):
		// Burn the same work a real comparison would, then fail identically.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		s.log.WarnContext(ctx, "failed admin login", "email", email, "reason", "unknown account")
		return LoginResponse{}, apperr.Unauthorized(apperr.CodeInvalidCredentials, invalidCredentialsMessage)
	case err != nil:
		return LoginResponse{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(req.Password)); err != nil {
		s.log.WarnContext(ctx, "failed admin login", "email", email, "reason", "password mismatch")
		return LoginResponse{}, apperr.Unauthorized(apperr.CodeInvalidCredentials, invalidCredentialsMessage)
	}

	token, expiresAt, err := s.issuer.Issue(account.ID, account.Email)
	if err != nil {
		return LoginResponse{}, err
	}

	s.log.InfoContext(ctx, "admin logged in", "admin_id", account.ID.String(), "email", account.Email)
	return LoginResponse{Token: token, ExpiresAt: expiresAt}, nil
}
