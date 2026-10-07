package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ppapapetrou76/argocd-cost-intelligence/pkg/models"
)

// failingStorage answers the methods the test calls; the embedded interface panics for the rest.
type failingStorage struct {
	CostStorage
	err error
}

func (s failingStorage) SaveActual(context.Context, *models.CostActual) error { return s.err }

func (s failingStorage) GetLatestActual(_ context.Context, app models.AppRef) (*models.CostActual, error) {
	return nil, fmt.Errorf("no cost actual found for application %s: %w", app, ErrNotFound)
}

func (s failingStorage) ListCloudEstimates(context.Context) ([]models.AppCloudEstimate, error) {
	return []models.AppCloudEstimate{{App: models.AppRef{Namespace: "argocd", Name: "web"}}}, nil
}

func TestInstrumented_ObservesEveryOperationAndPassesItsResultOn(t *testing.T) {
	t.Parallel()

	var seen []string
	failure := errors.New("connection refused")
	s := Instrumented(failingStorage{err: failure}, func(operation string, err error, took time.Duration) {
		assert.GreaterOrEqual(t, took, time.Duration(0))
		seen = append(seen, fmt.Sprintf("%s: %v", operation, err))
	})

	require.ErrorIs(t, s.SaveActual(context.Background(), &models.CostActual{}), failure)
	_, err := s.GetLatestActual(context.Background(), models.AppRef{Namespace: "argocd", Name: "web"})
	require.ErrorIs(t, err, ErrNotFound, "the caller still sees that nothing was found")
	estimates, err := s.ListCloudEstimates(context.Background())
	require.NoError(t, err)
	assert.Len(t, estimates, 1)

	assert.Equal(t, []string{
		"SaveActual: connection refused",
		"GetLatestActual: <nil>", // finding nothing is an answer, not a database error
		"ListCloudEstimates: <nil>",
	}, seen)
}
