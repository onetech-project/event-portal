package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/internal/admin/adminsql"
)

// ErrNotFound reports that no admin matched.
var ErrNotFound = errors.New("admin: not found")

// Account is the internal view of an admins row.
type Account struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
}

// Repository is the only place in the codebase that talks to the admins table.
type Repository struct {
	queries *adminsql.Queries
}

// NewRepository builds a repository over a pool or any other DBTX.
func NewRepository(dbtx adminsql.DBTX) *Repository {
	return &Repository{queries: adminsql.New(dbtx)}
}

// GetByEmail looks up an admin account.
func (r *Repository) GetByEmail(ctx context.Context, email string) (Account, error) {
	row, err := r.queries.GetAdminByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("get admin by email: %w", err)
	}
	return Account{ID: row.ID, Email: row.Email, PasswordHash: row.PasswordHash}, nil
}
