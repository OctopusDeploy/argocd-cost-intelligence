package storage

import (
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/lib/pq/pqerror"
)

func TestNoSchema_OnlyAMissingTable(t *testing.T) {
	missing := &pq.Error{Code: pqerror.UndefinedTable, Message: `relation "cloud_estimates" does not exist`}
	if err := noSchema(missing); !errors.Is(err, ErrNoSchema) || !errors.Is(err, missing) {
		t.Errorf("a missing table should wrap ErrNoSchema and the driver's error, got %v", err)
	}

	for _, err := range []error{
		&pq.Error{Code: pqerror.UndefinedColumn},
		errors.New("connection refused"),
	} {
		if errors.Is(noSchema(err), ErrNoSchema) {
			t.Errorf("%v is not a missing table", err)
		}
	}
}
