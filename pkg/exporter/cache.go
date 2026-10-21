package exporter

import (
	"context"
	"sync"
	"time"
)

// CachedApplicationLister keeps the Applications next lists for ttl, so a page every viewer refreshes
// does not list every Application in the cluster on each request. A failed list is not kept: the
// next request lists again.
type CachedApplicationLister struct {
	next ApplicationLister
	ttl  time.Duration
	now  func() time.Time

	// mu is held while listing, so requests arriving together share one list.
	mu      sync.Mutex
	apps    []Application
	expires time.Time
}

// NewCachedApplicationLister caches what next lists for ttl.
func NewCachedApplicationLister(next ApplicationLister, ttl time.Duration) *CachedApplicationLister {
	return &CachedApplicationLister{next: next, ttl: ttl, now: time.Now}
}

// List returns the cached Applications, or lists them when the cached list has expired.
func (c *CachedApplicationLister) List(ctx context.Context) ([]Application, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.apps != nil && c.now().Before(c.expires) {
		return c.apps, nil
	}
	apps, err := c.next.List(ctx)
	if err != nil {
		return nil, err
	}
	c.apps, c.expires = apps, c.now().Add(c.ttl)
	return apps, nil
}
