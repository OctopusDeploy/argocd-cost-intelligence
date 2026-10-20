package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePinger struct {
	failures int
	calls    int
}

func (f *fakePinger) PingContext(context.Context) error {
	f.calls++
	if f.calls <= f.failures {
		return errors.New("no such host")
	}
	return nil
}

func TestPingUntilReadyWaitsForTheDatabase(t *testing.T) {
	p := &fakePinger{failures: 3}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pingUntilReady(ctx, p, time.Millisecond); err != nil {
		t.Fatalf("expected the database to be reached, got %v", err)
	}
	if p.calls != 4 {
		t.Errorf("expected 4 pings, got %d", p.calls)
	}
}

func TestPingUntilReadyGivesUpAndReportsTheCause(t *testing.T) {
	p := &fakePinger{failures: 1 << 30}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := pingUntilReady(ctx, p, time.Millisecond)
	if err == nil || err.Error() != "no such host" {
		t.Fatalf("expected the last ping error, got %v", err)
	}
}

type slowPinger struct{ calls int }

// The first ping fails for a real reason; the next one blocks until ctx ends, like a dial that
// is still in progress when the deadline arrives.
func (f *slowPinger) PingContext(ctx context.Context) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("no such host")
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestPingUntilReadyKeepsTheCauseWhenTheDeadlineCutsAPingShort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := pingUntilReady(ctx, &slowPinger{}, time.Millisecond)
	if err == nil || err.Error() != "no such host" {
		t.Fatalf("expected the cause of the first failure, got %v", err)
	}
}
