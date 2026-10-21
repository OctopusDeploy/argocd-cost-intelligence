package exporter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

type countingLister struct {
	calls int
	err   error
}

func (l *countingLister) List(context.Context) ([]Application, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return []Application{{Ref: models.AppRef{Namespace: "argocd", Name: "web"}}}, nil
}

func TestCachedApplicationLister_KeepsTheListForTTL(t *testing.T) {
	t.Parallel()

	next := &countingLister{}
	cache := NewCachedApplicationLister(next, time.Minute)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }

	for range 3 {
		apps, err := cache.List(context.Background())
		require.NoError(t, err)
		assert.Len(t, apps, 1)
	}
	assert.Equal(t, 1, next.calls)

	now = now.Add(time.Minute)
	_, err := cache.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, next.calls, "an expired list is listed again")
}

func TestCachedApplicationLister_RetriesAfterAFailure(t *testing.T) {
	t.Parallel()

	next := &countingLister{err: errors.New("forbidden")}
	cache := NewCachedApplicationLister(next, time.Minute)

	_, err := cache.List(context.Background())
	require.Error(t, err)

	next.err = nil
	apps, err := cache.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, apps, 1)
	assert.Equal(t, 2, next.calls)
}
