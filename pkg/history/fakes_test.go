package history

import (
	"context"
	"sort"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// fakeStore keeps daily history in memory.
type fakeStore struct {
	latest   []models.CostActual
	days     map[models.AppRef]map[string]storedDay
	progress map[models.AppRef]models.DailyProgress
	prunedTo string
	// progressErr fails GetDailyProgress for every application.
	progressErr error
}

type storedDay struct {
	cost      models.DailyCost
	namespace string
	scope     string
}

func newFakeStore(latest ...models.CostActual) *fakeStore {
	return &fakeStore{
		latest:   latest,
		days:     map[models.AppRef]map[string]storedDay{},
		progress: map[models.AppRef]models.DailyProgress{},
	}
}

func (s *fakeStore) ListLatestActuals(context.Context) ([]models.CostActual, error) {
	return s.latest, nil
}

func (s *fakeStore) SaveDailyCosts(_ context.Context, app models.AppRef, namespace, scope string, days []models.DailyCost) error {
	if s.days[app] == nil {
		s.days[app] = map[string]storedDay{}
	}
	for _, day := range days {
		s.days[app][day.Date] = storedDay{cost: day, namespace: namespace, scope: scope}
	}
	return nil
}

func (s *fakeStore) GetDailyCosts(_ context.Context, app models.AppRef, scope, from, to string) ([]models.DailyCost, error) {
	result := []models.DailyCost{}
	for date, day := range s.days[app] {
		if day.scope == scope && date >= from && date <= to {
			result = append(result, day.cost)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

func (s *fakeStore) GetDailyProgress(_ context.Context, app models.AppRef) (*models.DailyProgress, error) {
	if s.progressErr != nil {
		return nil, s.progressErr
	}
	progress, ok := s.progress[app]
	if !ok {
		return nil, nil
	}
	return &progress, nil
}

func (s *fakeStore) SetDailyProgress(_ context.Context, app models.AppRef, progress models.DailyProgress) error {
	s.progress[app] = progress
	return nil
}

func (s *fakeStore) PruneDailyCosts(_ context.Context, before string) error {
	s.prunedTo = before
	for _, days := range s.days {
		for date := range days {
			if date < before {
				delete(days, date)
			}
		}
	}
	return nil
}

// storedDates lists an application's stored days, oldest first.
func (s *fakeStore) storedDates(app models.AppRef) []string {
	dates := make([]string, 0, len(s.days[app]))
	for date := range s.days[app] {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return dates
}

// fakeOpenCost answers with the cost it has for each asked day, as OpenCost does for the days its
// Prometheus keeps, and records what it was asked.
type fakeOpenCost struct {
	// costs holds a cost per namespace and UTC day.
	costs map[string]map[string]float64
	// fail makes every query fail with it.
	fail  error
	calls []call
}

type call struct {
	namespace, from, to string
	scope               string
}

func (o *fakeOpenCost) GetDailyTimeSeries(
	_ context.Context, namespace string, from, to time.Time, resources models.ManagedResources,
) ([]models.DailyCost, error) {
	o.calls = append(o.calls, call{namespace, formatDay(from), formatDay(to), models.ScopeOf(resources)})
	if o.fail != nil {
		return nil, o.fail
	}
	days := []models.DailyCost{}
	for day := utcDay(from); !day.After(to); day = day.AddDate(0, 0, 1) {
		if cost, ok := o.costs[namespace][formatDay(day)]; ok {
			days = append(days, models.DailyCost{Date: formatDay(day), TotalCost: cost, CPUCost: cost})
		}
	}
	return days, nil
}

// costsFrom gives namespace cost on every day from first through last.
func costsFrom(namespace, first, last string, cost float64) map[string]map[string]float64 {
	days := map[string]float64{}
	for day := mustDay(first); !day.After(mustDay(last)); day = day.AddDate(0, 0, 1) {
		days[formatDay(day)] = cost
	}
	return map[string]map[string]float64{namespace: days}
}

func mustDay(s string) time.Time {
	day, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return day
}

// record is an application's latest cost record, measured by its controllers when controllers is
// not nil and by its whole namespace otherwise.
func record(app models.AppRef, namespace string, controllers models.ManagedResources) models.CostActual {
	actual := models.CostActual{ApplicationNamespace: app.Namespace, ApplicationName: app.Name, DeploymentNamespace: namespace}
	if controllers != nil {
		actual.App = &models.AppMeasurement{Controllers: controllers}
	}
	return actual
}
