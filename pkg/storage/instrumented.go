package storage

import (
	"context"
	"errors"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// Observer is told about every storage operation: its name, its error (nil on success, and for a
// lookup that found nothing) and how long it took.
type Observer func(operation string, err error, took time.Duration)

// Instrumented wraps a CostStorage so observe sees every operation. Operations are named after the
// interface's methods, a fixed set, so they make bounded metric labels.
func Instrumented(next CostStorage, observe Observer) CostStorage {
	return &instrumented{next: next, observe: observe}
}

type instrumented struct {
	next    CostStorage
	observe Observer
}

// track times an operation started now; call the result with the operation's error.
func (s *instrumented) track(operation string) func(error) {
	start := time.Now()
	return func(err error) {
		if errors.Is(err, ErrNotFound) {
			err = nil
		}
		s.observe(operation, err, time.Since(start))
	}
}

func (s *instrumented) InitializeSchema(ctx context.Context) (err error) {
	done := s.track("InitializeSchema")
	defer func() { done(err) }()
	return s.next.InitializeSchema(ctx)
}

func (s *instrumented) SaveActual(ctx context.Context, actual *models.CostActual) (err error) {
	done := s.track("SaveActual")
	defer func() { done(err) }()
	return s.next.SaveActual(ctx, actual)
}

func (s *instrumented) GetLatestActual(ctx context.Context, app models.AppRef) (_ *models.CostActual, err error) {
	done := s.track("GetLatestActual")
	defer func() { done(err) }()
	return s.next.GetLatestActual(ctx, app)
}

func (s *instrumented) GetActualsInDateRange(ctx context.Context, app models.AppRef, startTime, endTime time.Time) (_ []models.CostActual, err error) {
	done := s.track("GetActualsInDateRange")
	defer func() { done(err) }()
	return s.next.GetActualsInDateRange(ctx, app, startTime, endTime)
}

func (s *instrumented) GetNamespaceCostSummary(ctx context.Context, namespace string, days int) (_ float64, err error) {
	done := s.track("GetNamespaceCostSummary")
	defer func() { done(err) }()
	return s.next.GetNamespaceCostSummary(ctx, namespace, days)
}

func (s *instrumented) ListNamespaces(ctx context.Context) (_ []models.NamespaceSummary, err error) {
	done := s.track("ListNamespaces")
	defer func() { done(err) }()
	return s.next.ListNamespaces(ctx)
}

func (s *instrumented) GetLatestActualsForNamespace(ctx context.Context, namespace string) (_ []models.CostActual, err error) {
	done := s.track("GetLatestActualsForNamespace")
	defer func() { done(err) }()
	return s.next.GetLatestActualsForNamespace(ctx, namespace)
}

func (s *instrumented) GetRecentActuals(ctx context.Context, app models.AppRef, limit int) (_ []models.CostActual, err error) {
	done := s.track("GetRecentActuals")
	defer func() { done(err) }()
	return s.next.GetRecentActuals(ctx, app, limit)
}

func (s *instrumented) SaveCloudEstimate(ctx context.Context, app models.AppRef, estimate *models.CloudServiceEstimate) (err error) {
	done := s.track("SaveCloudEstimate")
	defer func() { done(err) }()
	return s.next.SaveCloudEstimate(ctx, app, estimate)
}

func (s *instrumented) GetCloudEstimate(ctx context.Context, app models.AppRef) (_ *models.CloudServiceEstimate, err error) {
	done := s.track("GetCloudEstimate")
	defer func() { done(err) }()
	return s.next.GetCloudEstimate(ctx, app)
}

func (s *instrumented) ListCloudEstimates(ctx context.Context) (_ []models.AppCloudEstimate, err error) {
	done := s.track("ListCloudEstimates")
	defer func() { done(err) }()
	return s.next.ListCloudEstimates(ctx)
}

func (s *instrumented) ListLatestActuals(ctx context.Context) (_ []models.CostActual, err error) {
	done := s.track("ListLatestActuals")
	defer func() { done(err) }()
	return s.next.ListLatestActuals(ctx)
}

func (s *instrumented) SaveDailyCosts(ctx context.Context, app models.AppRef, namespace, scope string, days []models.DailyCost) (err error) {
	done := s.track("SaveDailyCosts")
	defer func() { done(err) }()
	return s.next.SaveDailyCosts(ctx, app, namespace, scope, days)
}

func (s *instrumented) GetDailyCosts(ctx context.Context, app models.AppRef, scope, from, to string) (_ []models.DailyCost, err error) {
	done := s.track("GetDailyCosts")
	defer func() { done(err) }()
	return s.next.GetDailyCosts(ctx, app, scope, from, to)
}

func (s *instrumented) GetDailyProgress(ctx context.Context, app models.AppRef) (_ *models.DailyProgress, err error) {
	done := s.track("GetDailyProgress")
	defer func() { done(err) }()
	return s.next.GetDailyProgress(ctx, app)
}

func (s *instrumented) SetDailyProgress(ctx context.Context, app models.AppRef, progress models.DailyProgress) (err error) {
	done := s.track("SetDailyProgress")
	defer func() { done(err) }()
	return s.next.SetDailyProgress(ctx, app, progress)
}

func (s *instrumented) PruneDailyCosts(ctx context.Context, before string) (err error) {
	done := s.track("PruneDailyCosts")
	defer func() { done(err) }()
	return s.next.PruneDailyCosts(ctx, before)
}

// Close is not a query, so it is not observed.
func (s *instrumented) Close() error { return s.next.Close() }
