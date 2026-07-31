package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/db"
)

func TestNewPoolRejectsMalformedDSN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.NewPool(ctx, "not-a-dsn")

	require.Error(t, err)
}

func TestNewPoolRejectsEmptyDSN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.NewPool(ctx, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}
