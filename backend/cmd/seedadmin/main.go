// Command seedadmin inserts (or updates) a local development admin account.
//
// It exists because there is no admin sign-up flow: the MVP has exactly one role
// and accounts are provisioned out of band. Passwords are hashed with bcrypt here
// so no plaintext password is ever written to the database or to a SQL file.
//
// Usage:
//
//	go run ./cmd/seedadmin -email admin@example.com -password 'change-me'
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/manjo/ticketing/backend/pkg/config"
	"github.com/manjo/ticketing/backend/pkg/db"
)

const minPasswordLength = 12

func main() {
	email := flag.String("email", "", "admin email address (required)")
	password := flag.String("password", "", "admin password (required)")
	flag.Parse()

	if err := run(strings.ToLower(strings.TrimSpace(*email)), *password); err != nil {
		fmt.Fprintln(os.Stderr, "seedadmin:", err)
		os.Exit(1)
	}
}

func run(email, password string) error {
	if email == "" || password == "" {
		return errors.New("both -email and -password are required")
	}
	if len(password) < minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}

	if err := config.LoadDotEnv(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Upsert so re-running the command rotates the password rather than failing on
	// the unique email constraint.
	_, err = pool.Exec(ctx, `
		INSERT INTO admins (email, password_hash)
		VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE
		SET password_hash = EXCLUDED.password_hash, updated_at = now()`,
		email, string(hash))
	if err != nil {
		return fmt.Errorf("upsert admin: %w", err)
	}

	fmt.Printf("admin %s is ready\n", email)
	return nil
}
