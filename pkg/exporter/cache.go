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

	mu      sync.Mutex
	apps    []Application
	expires time.Time
	// listing is the list in progress, which requests arriving together share.
	listing *listing
}

type listing struct {
	done chan struct{}
	apps []Application
	err  error
}

// NewCachedApplicationLister caches what next lists for ttl.
func NewCachedApplicationLister(next ApplicationLister, ttl time.Duration) *CachedApplicationLister {
	return &CachedApplicationLister{next: next, ttl: ttl, now: time.Now}
}

// List returns the cached Applications, or waits for them to be listed when the cached list has
// expired. A request that ends while it waits returns at once; the list carries on for the others.
func (c *CachedApplicationLister) List(ctx context.Context) ([]Application, error) {
	c.mu.Lock()
	if !c.expires.IsZero() && c.now().Before(c.expires) {
		apps := c.apps
		c.mu.Unlock()
		return apps, nil
	}
	l := c.listing
	if l == nil {
		l = &listing{done: make(chan struct{})}
		c.listing = l
		go c.list(context.WithoutCancel(ctx), l)
	}
	c.mu.Unlock()

	select {
	case <-l.done:
		return l.apps, l.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *CachedApplicationLister) list(ctx context.Context, l *listing) {
	l.apps, l.err = c.next.List(ctx)
	c.mu.Lock()
	if l.err == nil {
		c.apps, c.expires = l.apps, c.now().Add(c.ttl)
	}
	c.listing = nil
	c.mu.Unlock()
	close(l.done)
}
