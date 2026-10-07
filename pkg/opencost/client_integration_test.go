//go:build integration

package opencost

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests hit a real OpenCost API.
//
// Setup:
//   kubectl port-forward svc/opencost 9003:9003 -n opencost
//
// Run:
//   OPENCOST_BASE_URL=http://localhost:9003 go test -v -tags=integration ./pkg/opencost/...
//   # or: make test-integration OPENCOST_BASE_URL=http://localhost:9003

func opencostBaseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("OPENCOST_BASE_URL")
	if u == "" {
		u = "http://localhost:9003"
	}
	// Skip, rather than fail, when nothing is listening: these tests need a live OpenCost or
	// the mock (node mock-opencost-server.js).
	parsed, err := url.Parse(u)
	if err == nil {
		conn, dialErr := net.DialTimeout("tcp", parsed.Host, time.Second)
		if dialErr != nil {
			t.Skipf("no OpenCost reachable at %s (%v)", u, dialErr)
		}
		_ = conn.Close()
	}
	return u
}

// TestIntegration_RawAllocationResponse logs the raw JSON so we can see the
// exact wire format whenever the Go structs need updating.
func TestIntegration_RawAllocationResponse(t *testing.T) {
	baseURL := opencostBaseURL(t)
	url := baseURL + "/allocation?window=24h&aggregate=namespace&format=json"

	resp, err := http.Get(url) //nolint:gosec,noctx
	require.NoError(t, err, "GET %s failed — is OpenCost port-forwarded?", url)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	t.Logf("Raw /allocation response (status %d):\n%s", resp.StatusCode, prettyJSON(body))
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result AllocationResponse
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "AllocationResponse failed to decode — struct needs updating")
	t.Logf("Decoded: %d window(s)", len(result.Data))
}

// TestIntegration_RawSummaryResponse is the key test for catching the
// "cannot unmarshal X into SummaryAllocationData" class of bugs.
func TestIntegration_RawSummaryResponse(t *testing.T) {
	baseURL := opencostBaseURL(t)
	url := baseURL + "/allocation/summary?window=24h&aggregate=namespace"

	resp, err := http.Get(url) //nolint:gosec,noctx
	require.NoError(t, err, "GET %s failed — is OpenCost port-forwarded?", url)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	t.Logf("Raw /allocation/summary response (status %d):\n%s", resp.StatusCode, prettyJSON(body))
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result SummaryAllocationResponse
	err = json.Unmarshal(body, &result)
	assert.NoError(t, err, "SummaryAllocationResponse failed to decode — struct needs updating")
}

// TestIntegration_GetNamespaceSummary exercises the full client path including
// parseSummaryResponse, which is where decode failures have appeared.
func TestIntegration_GetNamespaceSummary(t *testing.T) {
	namespace := os.Getenv("TEST_NAMESPACE")
	if namespace == "" {
		namespace = "argocd"
	}

	client := NewClient(opencostBaseURL(t), 10*time.Second, nil)
	metrics, err := client.GetNamespaceSummary(context.Background(), namespace)
	require.NoError(t, err, "GetNamespaceSummary decode error — struct/parser mismatch with real API")

	if metrics == nil {
		t.Log("GetNamespaceSummary returned nil (no data for namespace yet — this is OK)")
		return
	}

	t.Logf("Efficiency metrics for namespace %q:", namespace)
	t.Logf("  CPUEfficiency:   %.4f", metrics.CPUEfficiency)
	t.Logf("  RAMEfficiency:   %.4f", metrics.RAMEfficiency)
	t.Logf("  TotalEfficiency: %.4f", metrics.TotalEfficiency)
	t.Logf("  CPUCoresRequested: %.4f", metrics.CPUCoresRequested)
	t.Logf("  CPUCoresUsed:      %.4f", metrics.CPUCoresUsed)
	t.Logf("  CPUIdleCost:  $%.4f/mo", metrics.CPUIdleCost)
	t.Logf("  RAMIdleCost:  $%.4f/mo", metrics.RAMIdleCost)
}

// TestIntegration_GetNamespaceCost exercises the allocation endpoint.
func TestIntegration_GetNamespaceCost(t *testing.T) {
	namespace := os.Getenv("TEST_NAMESPACE")
	if namespace == "" {
		namespace = "argocd"
	}

	client := NewClient(opencostBaseURL(t), 10*time.Second, nil)
	metrics, err := client.GetNamespaceCost(context.Background(), namespace)
	require.NoError(t, err, "GetNamespaceCost decode error")
	require.NotNil(t, metrics)

	t.Logf("Cost metrics for namespace %q:", namespace)
	t.Logf("  CPU:     $%.4f/mo", metrics.AllocationTotals.CPUCost)
	t.Logf("  Memory:  $%.4f/mo", metrics.AllocationTotals.MemoryCost)
	t.Logf("  Total:   $%.4f/mo", metrics.AllocationTotals.TotalCost)
}

// TestIntegration_GetPodAllocations exercises pod-level aggregation.
func TestIntegration_GetPodAllocations(t *testing.T) {
	namespace := os.Getenv("TEST_NAMESPACE")
	if namespace == "" {
		namespace = "argocd"
	}

	client := NewClient(opencostBaseURL(t), 10*time.Second, nil)
	allocations, err := client.GetPodAllocations(context.Background(), namespace)
	require.NoError(t, err, "GetPodAllocations decode error")
	require.NotNil(t, allocations)

	pods := allocations.Pods()
	t.Logf("Pod breakdown for namespace %q (%d pods):", namespace, len(pods))
	for _, p := range pods {
		assert.NotEmpty(t, p.Name)
		assert.Positive(t, p.TotalCost, p.Name)
		t.Logf("  %-40s  CPU $%.4f  RAM $%.4f  Total $%.4f", p.Name, p.CPUCost, p.RAMCost, p.TotalCost)
	}
}

func TestIntegration_GetLabelKeys(t *testing.T) {
	namespace := os.Getenv("TEST_NAMESPACE")
	if namespace == "" {
		namespace = "argocd"
	}

	client := NewClient(opencostBaseURL(t), 10*time.Second, nil)
	for _, ns := range []string{"", namespace} {
		keys, err := client.GetLabelKeys(context.Background(), ns)
		require.NoError(t, err, "GetLabelKeys failed — OpenCost older than 1.120.3 has no /allocation/autocomplete")
		for _, key := range keys {
			assert.False(t, isBookkeepingLabel(key), key)
		}
		t.Logf("Label keys in namespace %q (%d): %v", ns, len(keys), keys)
	}
}

func prettyJSON(b []byte) string {
	var buf []byte
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	buf, _ = json.MarshalIndent(v, "", "  ")
	return string(buf)
}
