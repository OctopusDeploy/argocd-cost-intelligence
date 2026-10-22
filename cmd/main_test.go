package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/config"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/rightsizing"
)

func TestOpenCostClientLogsRequestsOnlyAtDebug(t *testing.T) {
	t.Parallel()

	opencostServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[]}`))
	}))
	t.Cleanup(opencostServer.Close)

	for _, tt := range []struct {
		level   string
		wantLog bool
	}{
		{level: "", wantLog: false},
		{level: config.LogLevelInfo, wantLog: false},
		{level: config.LogLevelDebug, wantLog: true},
	} {
		t.Run("level "+tt.level, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			cfg := &config.Config{
				OpenCost: config.OpenCostConfig{BaseURL: opencostServer.URL, Timeout: 5 * time.Second},
				LogLevel: tt.level,
			}
			client := newOpenCostClient(cfg, nil, log.New(&out, "", 0))
			_, _ = client.GetNamespaceCost(context.Background(), "team-a")

			assert.Equal(t, tt.wantLog, bytes.Contains(out.Bytes(), []byte(opencostServer.URL)), out.String())
		})
	}
}

// fakeVPAChecker answers Check with err.
type fakeVPAChecker struct{ err error }

func (f fakeVPAChecker) Check(context.Context) error { return f.err }

func TestLogVPAAvailabilitySaysWhetherTheVPAAPICanBeRead(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		err  error
		want string
	}{
		"VPA installed":    {want: "VPA rightsizing on"},
		"no VPA installed": {err: fmt.Errorf("%w: not found", rightsizing.ErrUnavailable), want: "VPA rightsizing waiting for the VerticalPodAutoscaler API"},
		"check failed":     {err: errors.New("connection refused"), want: "checking the VerticalPodAutoscaler API failed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			logVPAAvailability(context.Background(), fakeVPAChecker{err: tt.err}, log.New(&out, "", 0))

			assert.Contains(t, out.String(), tt.want)
			if tt.err != nil {
				assert.NotEqual(t, "VPA rightsizing on\n", out.String())
			}
		})
	}
}
