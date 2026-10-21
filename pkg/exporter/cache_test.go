package exporter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

// blockingLister lists only once release is closed.
type blockingLister struct {
	calls   atomic.Int32
	release chan struct{}
}

func (l *blockingLister) List(context.Context) ([]Application, error) {
	l.calls.Add(1)
	<-l.release
	return []Application{{Ref: models.AppRef{Namespace: "argocd", Name: "web"}}}, nil
}

func TestCachedApplicationLister_SharesOneListBetweenRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		next := &blockingLister{release: make(chan struct{})}
		cache := NewCachedApplicationLister(next, time.Minute)

		var wg sync.WaitGroup
		lists := make([][]Application, 3)
		for i := range lists {
			wg.Go(func() {
				apps, err := cache.List(context.Background())
				assert.NoError(t, err)
				lists[i] = apps
			})
		}
		synctest.Wait()
		assert.Equal(t, int32(1), next.calls.Load(), "requests waiting together share one list")

		close(next.release)
		wg.Wait()
		for _, apps := range lists {
			assert.Len(t, apps, 1)
		}
	})
}

func TestCachedApplicationLister_StopsWaitingWhenTheRequestEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		next := &blockingLister{release: make(chan struct{})}
		cache := NewCachedApplicationLister(next, time.Minute)

		ctx, cancel := context.WithCancel(context.Background())
		var err error
		done := make(chan struct{})
		go func() {
			_, err = cache.List(ctx)
			close(done)
		}()
		synctest.Wait()
		cancel()
		<-done
		require.ErrorIs(t, err, context.Canceled)

		// The list carries on for the next request, which is served from it.
		close(next.release)
		synctest.Wait()
		apps, err := cache.List(context.Background())
		require.NoError(t, err)
		assert.Len(t, apps, 1)
		assert.Equal(t, int32(1), next.calls.Load())
	})
}

func TestCachedApplicationLister_KeepsAnEmptyList(t *testing.T) {
	t.Parallel()

	next := &countingLister{}
	cache := NewCachedApplicationLister(emptyLister{next}, time.Minute)

	for range 2 {
		apps, err := cache.List(context.Background())
		require.NoError(t, err)
		assert.Empty(t, apps)
	}
	assert.Equal(t, 1, next.calls)
}

// emptyLister counts its calls on next and lists no Applications, as a nil slice.
type emptyLister struct{ next *countingLister }

func (l emptyLister) List(ctx context.Context) ([]Application, error) {
	_, _ = l.next.List(ctx)
	return nil, nil
}
