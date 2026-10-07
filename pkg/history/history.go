// Package history keeps each application's cost per UTC day in storage, so a daily series can reach
// further back than OpenCost, which answers only for the days its Prometheus still keeps.
//
// A Recorder stores every application's days once they are over; Daily serves a series from the
// stored days and asks OpenCost only for the days after them.
package history

import (
	"context"
	"fmt"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// Source is where daily costs come from: OpenCost.
type Source interface {
	GetDailyTimeSeries(ctx context.Context, namespace string, from, to time.Time, resources models.ManagedResources) ([]models.DailyCost, error)
}

// Reader is the storage Daily reads.
type Reader interface {
	GetDailyProgress(ctx context.Context, app models.AppRef) (*models.DailyProgress, error)
	GetDailyCosts(ctx context.Context, app models.AppRef, scope, from, to string) ([]models.DailyCost, error)
}

// Target is where an application's cost is measured: the namespace it deploys to and the resources
// it manages there, nil meaning the whole namespace.
type Target struct {
	App       models.AppRef
	Namespace string
	Resources models.ManagedResources
}

// Daily returns the target's cost for each UTC day from through to: the days recorded in the
// target's current scope, and OpenCost's figures for the days after them, today among them. With
// nothing recorded in that scope, every day comes from OpenCost. Days before the recorded history are
// not asked for: the recorder already asked OpenCost about them, or they are older than it keeps.
func Daily(ctx context.Context, store Reader, source Source, target Target, from, to time.Time) ([]models.DailyCost, error) {
	from, to = utcDay(from), utcDay(to)
	scope := models.ScopeOf(target.Resources)

	liveFrom := from
	days := make([]models.DailyCost, 0, max(0, int(to.Sub(from).Hours()/24)+1))
	progress, err := store.GetDailyProgress(ctx, target.App)
	if err != nil {
		return nil, err
	}
	if progress != nil && progress.Scope == scope {
		through, err := time.Parse(time.DateOnly, progress.Through)
		if err != nil {
			return nil, fmt.Errorf("invalid recorded day %q: %w", progress.Through, err)
		}
		if !through.Before(from) {
			storedTo := through
			if to.Before(storedTo) {
				storedTo = to
			}
			stored, err := store.GetDailyCosts(ctx, target.App, scope, formatDay(from), formatDay(storedTo))
			if err != nil {
				return nil, err
			}
			days = append(days, stored...)
			liveFrom = through.AddDate(0, 0, 1)
		}
	}
	if liveFrom.After(to) {
		return days, nil
	}

	live, err := source.GetDailyTimeSeries(ctx, target.Namespace, liveFrom, to, target.Resources)
	if err != nil {
		return nil, err
	}
	return append(days, within(live, liveFrom, to)...), nil
}

// within keeps the days from from through to.
func within(days []models.DailyCost, from, to time.Time) []models.DailyCost {
	first, last := formatDay(from), formatDay(to)
	kept := make([]models.DailyCost, 0, len(days))
	for _, day := range days {
		if day.Date >= first && day.Date <= last {
			kept = append(kept, day)
		}
	}
	return kept
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func formatDay(t time.Time) string {
	return t.Format(time.DateOnly)
}
