package storage

import (
	"database/sql"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithPool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		maxOpen      int
		maxIdle      int
		wantMaxOpen  int
		wantUnlimits bool
	}{
		{name: "limits applied", maxOpen: 7, maxIdle: 3, wantMaxOpen: 7},
		{name: "zero leaves the driver default (unlimited)", maxOpen: 0, maxIdle: 0, wantUnlimits: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// sql.Open does not connect, so no database is needed to inspect the pool.
			db, err := sql.Open("postgres", "postgres://u:p@localhost:1/db?sslmode=disable")
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			WithPool(tt.maxOpen, tt.maxIdle)(db)

			if tt.wantUnlimits {
				assert.Equal(t, 0, db.Stats().MaxOpenConnections)
				return
			}
			assert.Equal(t, tt.wantMaxOpen, db.Stats().MaxOpenConnections)
		})
	}
}
