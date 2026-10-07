package opencost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

type Client struct {
	baseURL string
	client  *http.Client
	logger  *log.Logger
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithTransport sends the client's requests through transport, for example to instrument them.
func WithTransport(transport http.RoundTripper) ClientOption {
	return func(c *Client) { c.client.Transport = transport }
}

func NewClient(baseURL string, timeout time.Duration, logger *log.Logger, opts ...ClientOption) *Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	c := &Client{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type AllocationResponse struct {
	Code int                         `json:"code"`
	Data []map[string]AllocationData `json:"data"`
}

// AllocationProperties mirrors OpenCost's AllocationProperties struct.
// services is []string and labels/namespaceLabels are map[string]string,
// so this cannot be decoded as map[string]string.
type AllocationProperties struct {
	Cluster         string            `json:"cluster,omitempty"`
	Node            string            `json:"node,omitempty"`
	Container       string            `json:"container,omitempty"`
	Controller      string            `json:"controller,omitempty"`
	ControllerKind  string            `json:"controllerKind,omitempty"`
	Namespace       string            `json:"namespace,omitempty"`
	Pod             string            `json:"pod,omitempty"`
	ProviderID      string            `json:"providerID,omitempty"`
	Services        []string          `json:"services,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	NamespaceLabels map[string]string `json:"namespaceLabels,omitempty"`
}

type RawAllocationOnly struct {
	CPUCoreUsageMax float64 `json:"cpuCoreUsageMax"`
	RAMByteUsageMax float64 `json:"ramByteUsageMax"`
}

// SharedCostItem is one entry in the sharedCostBreakdown map returned by OpenCost.
// Each entry represents a shared namespace/controller and its cost contribution.
type SharedCostItem struct {
	TotalCost float64 `json:"totalCost"`
}

type AllocationData struct {
	Start                        time.Time                 `json:"start"`
	End                          time.Time                 `json:"end"`
	Minutes                      float64                   `json:"minutes"`
	CPUCoreHours                 float64                   `json:"cpuCoreHours"`
	CPUCoreRequestAverage        float64                   `json:"cpuCoreRequestAverage"`
	CPUCoreUsageAverage          float64                   `json:"cpuCoreUsageAverage"`
	CPUCoreLimitAverage          float64                   `json:"cpuCoreLimitAverage"`
	CPUCost                      float64                   `json:"cpuCost"`
	GPUHours                     float64                   `json:"gpuHours"`
	GPUCost                      float64                   `json:"gpuCost"`
	NetworkCost                  float64                   `json:"networkCost"`
	NetworkCrossZoneCost         float64                   `json:"networkCrossZoneCost"`
	NetworkCrossRegionCost       float64                   `json:"networkCrossRegionCost"`
	NetworkInternetCost          float64                   `json:"networkInternetCost"`
	NetworkNatGatewayEgressCost  float64                   `json:"networkNatGatewayEgressCost"`
	NetworkNatGatewayIngressCost float64                   `json:"networkNatGatewayIngressCost"`
	LoadBalancerCost             float64                   `json:"loadBalancerCost"`
	PVCost                       float64                   `json:"pvCost"`
	RAMByteHours                 float64                   `json:"ramByteHours"`
	RAMByteRequestAverage        float64                   `json:"ramByteRequestAverage"`
	RAMByteUsageAverage          float64                   `json:"ramByteUsageAverage"`
	RAMByteLimitAverage          float64                   `json:"ramByteLimitAverage"`
	RAMCost                      float64                   `json:"ramCost"`
	TotalCost                    float64                   `json:"totalCost"`
	SharedCost                   float64                   `json:"sharedCost"`
	SharedCostBreakdown          map[string]SharedCostItem `json:"sharedCostBreakdown,omitempty"`
	ExternalCost                 float64                   `json:"externalCost"`
	CarbonCost                   float64                   `json:"carbonCost"`
	Properties                   AllocationProperties      `json:"properties"`
	RawAllocationOnly            *RawAllocationOnly        `json:"rawAllocationOnly,omitempty"`
}

func (c *Client) logf(format string, args ...any) {
	if c.logger != nil {
		c.logger.Printf(format, args...)
	}
}

// monthHours is the length of the month costs are projected onto: 30 days.
const monthHours = 30 * 24

// defaultMonthlyFactor projects a full 24h window onto a 30-day month. It applies when
// OpenCost does not say which period it measured.
const defaultMonthlyFactor = 30.0

// monthlyFactor returns the multiplier that projects a cost OpenCost measured between start
// and end onto a 30-day month. OpenCost reports a namespace or pod only from when it first
// ran, so a namespace created an hour ago carries an hour of cost, not a day's: scaling by
// the full 24h window would put its monthly cost 24 times too low.
func monthlyFactor(start, end time.Time) float64 {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return defaultMonthlyFactor
	}
	return monthHours / end.Sub(start).Hours()
}

// measuredPeriod returns the period OpenCost has data for across a response: the earliest
// start and latest end of its allocations. Pods are scaled by this shared period rather
// than each by its own, so a pod replaced during the window is not projected onto a whole
// month alongside its replacement, and the pods still add up to the namespace total.
func measuredPeriod(resp AllocationResponse) (start, end time.Time) {
	for _, allocations := range resp.Data {
		for key, alloc := range allocations {
			if isVirtualAllocation(key) {
				continue
			}
			if !alloc.Start.IsZero() && (start.IsZero() || alloc.Start.Before(start)) {
				start = alloc.Start
			}
			if alloc.End.After(end) {
				end = alloc.End
			}
		}
	}
	return start, end
}

// last24h returns the 24 hours up to now, the period every point-in-time query covers.
func last24h() (start, end time.Time) {
	end = time.Now()
	return end.Add(-24 * time.Hour), end
}

// windowParam writes start and end as OpenCost's window parameter, in UTC: OpenCost rejects a
// window carrying a UTC offset (2026-10-01T18:43:58+03:00) as an "illegal window", so a backend
// whose local time zone is not UTC (TZ set on the pod, a laptop) would fail every query.
func windowParam(start, end time.Time) string {
	return start.UTC().Format(time.RFC3339) + "," + end.UTC().Format(time.RFC3339)
}

// isVirtualAllocation reports whether key names one of OpenCost's virtual entries, such as
// __idle__ or __unallocated__, rather than a real namespace, pod or label value.
func isVirtualAllocation(key string) bool {
	return len(key) > 2 && key[0] == '_' && key[len(key)-1] == '_'
}

func (c *Client) GetNamespaceCost(ctx context.Context, namespace string) (*models.OpenCostMetrics, error) {
	startTime, endTime := last24h()

	params := url.Values{}
	params.Add("window", windowParam(startTime, endTime))
	params.Add("aggregate", "namespace")
	params.Add("filter", fmt.Sprintf("namespace:%q", namespace))
	params.Add("includeRawAllocation", "true")

	requestURL := fmt.Sprintf("%s/allocation?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	c.logf("DEBUG opencost namespace cost response: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %w", err)
	}

	metrics := parseOpenCostResponse(result, startTime, endTime)
	return metrics, nil
}

// SummaryAllocationResponse mirrors the real OpenCost /allocation/summary wire format:
//
//	{ "code": 200, "data": { "sets": [ { "allocations": { "<ns>": { ... } } } ] } }
type SummaryAllocationResponse struct {
	Code int                           `json:"code"`
	Data SummaryAllocationResponseData `json:"data"`
}

type SummaryAllocationResponseData struct {
	Sets []SummaryAllocationSet `json:"sets"`
}

type SummaryAllocationSet struct {
	Allocations map[string]SummaryAllocationData `json:"allocations"`
}

type SummaryAllocationData struct {
	Start                 time.Time `json:"start"`
	End                   time.Time `json:"end"`
	CPUCoreRequestAverage float64   `json:"cpuCoreRequestAverage"`
	CPUCoreUsageAverage   float64   `json:"cpuCoreUsageAverage"`
	CPUCoreLimitAverage   float64   `json:"cpuCoreLimitAverage"`
	CPUCost               float64   `json:"cpuCost"`
	CPUCostIdle           float64   `json:"cpuCostIdle"`
	RAMByteRequestAverage float64   `json:"ramByteRequestAverage"`
	RAMByteUsageAverage   float64   `json:"ramByteUsageAverage"`
	RAMByteLimitAverage   float64   `json:"ramByteLimitAverage"`
	RAMCost               float64   `json:"ramCost"`
	RAMCostIdle           float64   `json:"ramCostIdle"`
	Efficiency            float64   `json:"efficiency"`
}

func (c *Client) GetNamespaceSummary(ctx context.Context, namespace string) (*models.EfficiencyMetrics, error) {
	params := url.Values{}
	params.Add("window", windowParam(last24h()))
	params.Add("aggregate", "namespace")
	params.Add("filter", fmt.Sprintf("namespace:%q", namespace))

	requestURL := fmt.Sprintf("%s/allocation/summary?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost summary request failed: %w", err)
	}
	defer resp.Body.Close()

	c.logf("DEBUG opencost summary response: %d", resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencost summary returned status %d: %s", resp.StatusCode, string(body))
	}

	c.logf("DEBUG opencost summary body: %s", body)

	var result SummaryAllocationResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost summary response: %w", err)
	}

	metrics := parseSummaryResponse(result)
	c.logf("DEBUG opencost summary parsed: sets=%d → metrics=%v", len(result.Data.Sets), metrics != nil)
	return metrics, nil
}

// PodAllocations is a namespace's pod-level allocation over the last 24 hours, from which both the
// namespace's pods and one application's share of them are measured.
type PodAllocations struct {
	resp       AllocationResponse
	start, end time.Time
}

func (c *Client) GetPodAllocations(ctx context.Context, namespace string) (*PodAllocations, error) {
	startTime, endTime := last24h()
	return c.podAllocations(ctx, namespace, startTime, endTime)
}

// GetRecentPodAllocations returns the pods of a namespace over the window up to now, for the cost
// rate of the applications deploying there.
func (c *Client) GetRecentPodAllocations(ctx context.Context, namespace string, window time.Duration) (*PodAllocations, error) {
	endTime := time.Now()
	return c.podAllocations(ctx, namespace, endTime.Add(-window), endTime)
}

func (c *Client) podAllocations(ctx context.Context, namespace string, startTime, endTime time.Time) (*PodAllocations, error) {
	params := url.Values{}
	params.Add("window", windowParam(startTime, endTime))
	params.Add("aggregate", "pod")
	params.Add("filter", fmt.Sprintf("namespace:%q", namespace))
	// Peak usage per pod, for an application's efficiency.
	params.Add("includeRawAllocation", "true")

	requestURL := fmt.Sprintf("%s/allocation?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	c.logf("DEBUG opencost pod breakdown response: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %w", err)
	}

	return &PodAllocations{resp: result, start: startTime, end: endTime}, nil
}

// GetContainerAllocations returns every container of every pod in a namespace over the last 24
// hours, with its requests, limits, usage and cost.
func (c *Client) GetContainerAllocations(ctx context.Context, namespace string) ([]models.ContainerAllocation, error) {
	startTime, endTime := last24h()

	params := url.Values{}
	params.Add("window", windowParam(startTime, endTime))
	params.Add("aggregate", "pod,container")
	params.Add("filter", fmt.Sprintf("namespace:%q", namespace))

	requestURL := fmt.Sprintf("%s/allocation?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %w", err)
	}
	return parseContainerAllocations(result), nil
}

// parseContainerAllocations keeps the allocations that are a real container of a real pod.
func parseContainerAllocations(resp AllocationResponse) []models.ContainerAllocation {
	containers := []models.ContainerAllocation{}
	for _, allocations := range resp.Data {
		for key, alloc := range allocations {
			props := alloc.Properties
			if isVirtualAllocation(key) || props.Pod == "" || props.Container == "" {
				continue
			}
			containers = append(containers, models.ContainerAllocation{
				Pod:             props.Pod,
				Container:       props.Container,
				ControllerKind:  props.ControllerKind,
				Controller:      props.Controller,
				Hours:           alloc.Minutes / 60,
				CPURequestCores: alloc.CPUCoreRequestAverage,
				CPUUsageCores:   alloc.CPUCoreUsageAverage,
				CPULimitCores:   alloc.CPUCoreLimitAverage,
				RAMRequestBytes: alloc.RAMByteRequestAverage,
				RAMUsageBytes:   alloc.RAMByteUsageAverage,
				RAMLimitBytes:   alloc.RAMByteLimitAverage,
				CPUCoreHours:    alloc.CPUCoreHours,
				RAMByteHours:    alloc.RAMByteHours,
				CPUCost:         alloc.CPUCost,
				RAMCost:         alloc.RAMCost,
			})
		}
	}
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].Pod != containers[j].Pod {
			return containers[i].Pod < containers[j].Pod
		}
		return containers[i].Container < containers[j].Container
	})
	return containers
}

// Pods returns every pod in the namespace with its monthly cost.
func (p *PodAllocations) Pods() []models.PodCost {
	return parsePodBreakdown(p.resp)
}

// ForApplication measures the pods the application's resources own: their monthly cost and its
// breakdown, their efficiency and the pods themselves, projected onto a month from the period those
// pods ran rather than the namespace's, so a new application in an older namespace is not
// under-reported.
func (p *PodAllocations) ForApplication(resources models.ManagedResources) *models.AppMeasurement {
	owned := ownedAllocations(p.resp, resources)
	metrics := parseOpenCostResponse(owned, p.start, p.end)
	// The raw allocations are already stored once, with the namespace's measurement.
	metrics.ResourceDetails = nil

	return &models.AppMeasurement{
		Controllers:      resources,
		TotalMonthlyCost: metrics.AllocationTotals.TotalCost,
		OpenCostOutput:   *metrics,
		Efficiency:       podEfficiency(owned),
		Pods:             parsePodBreakdown(owned),
	}
}

// HourlyCost is the cost per hour of a set of pods, by resource. The resources add up to the pods'
// cost, as OpenCost's total does: shared costs are not part of it. CPUIdle and MemoryIdle are the parts
// of CPU and Memory paid for requested capacity that went unused.
type HourlyCost struct {
	CPU, Memory, GPU, Storage, Network, LoadBalancer float64
	CPUIdle, MemoryIdle                              float64
}

// HourlyCostOf returns the average cost per hour of the pods the resources own, over the period the
// whole response measured. Every application in a namespace is divided by the same period, so their
// rates add up to the namespace's, and a pod that ran for part of it counts for that part.
func (p *PodAllocations) HourlyCostOf(resources models.ManagedResources) HourlyCost {
	hours := p.end.Sub(p.start).Hours()
	if start, end := measuredPeriod(p.resp); !start.IsZero() && end.After(start) {
		hours = end.Sub(start).Hours()
	}
	var cost HourlyCost
	if hours <= 0 {
		return cost
	}
	for _, allocations := range ownedAllocations(p.resp, resources).Data {
		for _, a := range allocations {
			cost.CPU += a.CPUCost
			cost.Memory += a.RAMCost
			cost.GPU += a.GPUCost
			cost.Storage += a.PVCost
			cost.Network += a.NetworkCost
			cost.LoadBalancer += a.LoadBalancerCost
			cost.CPUIdle += unusedCost(a.CPUCost, a.CPUCoreRequestAverage, a.CPUCoreUsageAverage)
			cost.MemoryIdle += unusedCost(a.RAMCost, a.RAMByteRequestAverage, a.RAMByteUsageAverage)
		}
	}
	for _, v := range []*float64{&cost.CPU, &cost.Memory, &cost.GPU, &cost.Storage, &cost.Network, &cost.LoadBalancer, &cost.CPUIdle, &cost.MemoryIdle} {
		*v /= hours
	}
	return cost
}

// ownedAllocations keeps the pod allocations the resources own, dropping OpenCost's virtual entries.
func ownedAllocations(resp AllocationResponse, resources models.ManagedResources) AllocationResponse {
	owned := AllocationResponse{Code: resp.Code, Data: make([]map[string]AllocationData, 0, len(resp.Data))}
	for _, allocations := range resp.Data {
		kept := make(map[string]AllocationData)
		for key, alloc := range allocations {
			if isVirtualAllocation(key) {
				continue
			}
			pod := alloc.Properties.Pod
			if pod == "" {
				pod = key
			}
			if resources.OwnsPod(alloc.Properties.Namespace, alloc.Properties.ControllerKind, alloc.Properties.Controller, pod) {
				kept[key] = alloc
			}
		}
		owned.Data = append(owned.Data, kept)
	}
	return owned
}

// podEfficiency measures how much of what a set of pods requests they use, and what the unused part
// of the request costs. Returns nil without pods.
//
// Requests, usage and limits are averaged over the period the pods ran, weighting each pod by its
// minutes, so a pod replaced during the window and its replacement count as one. Peaks are summed,
// which overstates them when pods replaced each other rather than ran side by side: a recommendation
// sized from them errs towards headroom.
//
// OpenCost's own idle fields (cpuCostIdle, ramCostIdle) describe unallocated cluster capacity and are
// zero on a pod. A pod using less than it requests pays for its request, so its idle cost is the
// unused share of its cost.
func podEfficiency(resp AllocationResponse) *models.EfficiencyMetrics {
	periodStart, periodEnd := measuredPeriod(resp)
	periodMinutes := 24 * 60.0
	if !periodStart.IsZero() && periodEnd.After(periodStart) {
		periodMinutes = periodEnd.Sub(periodStart).Minutes()
	}

	var e models.EfficiencyMetrics
	var cpuCost, ramCost, cpuIdle, ramIdle float64
	pods := 0
	for _, allocations := range resp.Data {
		for key, a := range allocations {
			if isVirtualAllocation(key) {
				continue
			}
			pods++
			share := 1.0
			if a.Minutes > 0 {
				share = math.Min(a.Minutes/periodMinutes, 1)
			}
			e.CPUCoresRequested += a.CPUCoreRequestAverage * share
			e.CPUCoresUsed += a.CPUCoreUsageAverage * share
			e.CPUCoresLimit += a.CPUCoreLimitAverage * share
			e.RAMBytesRequested += a.RAMByteRequestAverage * share
			e.RAMBytesUsed += a.RAMByteUsageAverage * share
			e.RAMBytesLimit += a.RAMByteLimitAverage * share
			if a.RawAllocationOnly != nil {
				e.CPUCoresUsedMax += a.RawAllocationOnly.CPUCoreUsageMax
				e.RAMBytesUsedMax += a.RawAllocationOnly.RAMByteUsageMax
			}
			cpuCost += a.CPUCost
			ramCost += a.RAMCost
			cpuIdle += unusedCost(a.CPUCost, a.CPUCoreRequestAverage, a.CPUCoreUsageAverage)
			ramIdle += unusedCost(a.RAMCost, a.RAMByteRequestAverage, a.RAMByteUsageAverage)
		}
	}
	if pods == 0 {
		return nil
	}

	if e.CPUCoresRequested > 0 {
		e.CPUEfficiency = e.CPUCoresUsed / e.CPUCoresRequested
	}
	if e.RAMBytesRequested > 0 {
		e.RAMEfficiency = e.RAMBytesUsed / e.RAMBytesRequested
	}
	// Weighted by cost, as OpenCost weighs its own total efficiency.
	if cpuCost+ramCost > 0 {
		e.TotalEfficiency = (e.CPUEfficiency*cpuCost + e.RAMEfficiency*ramCost) / (cpuCost + ramCost)
	}

	monthlyMultiplier := monthlyFactor(periodStart, periodEnd)
	e.CPUIdleCost = cpuIdle * monthlyMultiplier
	e.RAMIdleCost = ramIdle * monthlyMultiplier
	return &e
}

// unusedCost is the part of a cost paid for requested capacity that went unused.
func unusedCost(cost, request, usage float64) float64 {
	if request <= 0 || usage >= request {
		return 0
	}
	return cost * (request - usage) / request
}

func (c *Client) GetOrphanedPods(ctx context.Context, namespace string) ([]models.OrphanedPod, error) {
	params := url.Values{}
	params.Add("window", windowParam(last24h()))
	params.Add("aggregate", "pod")
	params.Add("filter", fmt.Sprintf("namespace:%q", namespace))

	requestURL := fmt.Sprintf("%s/allocation?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	c.logf("DEBUG opencost orphaned pods response: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %w", err)
	}

	return parseOrphanedPods(result), nil
}

func (c *Client) GetChargeback(ctx context.Context, labelKey, namespace string) ([]models.ChargebackEntry, error) {
	params := url.Values{}
	params.Add("window", windowParam(last24h()))
	params.Add("aggregate", "label:"+labelKey)
	if namespace != "" {
		params.Add("filter", fmt.Sprintf("namespace:%q", namespace))
	}

	allocURL := fmt.Sprintf("%s/allocation?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost chargeback alloc GET %s", allocURL)

	allocReq, err := http.NewRequestWithContext(ctx, http.MethodGet, allocURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create allocation request: %w", err)
	}
	allocResp, err := c.client.Do(allocReq)
	if err != nil {
		return nil, fmt.Errorf("opencost allocation request failed: %w", err)
	}
	defer allocResp.Body.Close()

	c.logf("DEBUG opencost chargeback alloc response: %d", allocResp.StatusCode)

	if allocResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(allocResp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", allocResp.StatusCode, string(body))
	}
	var allocResult AllocationResponse
	if err := json.NewDecoder(allocResp.Body).Decode(&allocResult); err != nil {
		return nil, fmt.Errorf("failed to decode allocation response: %w", err)
	}

	summaryURL := fmt.Sprintf("%s/allocation/summary?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost chargeback summary GET %s", summaryURL)

	summaryReq, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create summary request: %w", err)
	}
	summaryResp, err := c.client.Do(summaryReq)
	if err != nil {
		return nil, fmt.Errorf("opencost summary request failed: %w", err)
	}
	defer summaryResp.Body.Close()

	c.logf("DEBUG opencost chargeback summary response: %d", summaryResp.StatusCode)

	var summaryResult SummaryAllocationResponse
	if summaryResp.StatusCode == http.StatusOK {
		_ = json.NewDecoder(summaryResp.Body).Decode(&summaryResult)
	}

	return parseChargebackEntries(allocResult, summaryResult), nil
}

func parseChargebackEntries(alloc AllocationResponse, summary SummaryAllocationResponse) []models.ChargebackEntry {
	monthlyMultiplier := monthlyFactor(measuredPeriod(alloc))

	// Build efficiency map keyed by label value
	effMap := make(map[string]float64)
	for _, set := range summary.Data.Sets {
		for labelVal, a := range set.Allocations {
			effMap[labelVal] = a.Efficiency
		}
	}

	type costAgg struct{ cpu, ram, pv, network, lb, total float64 }
	costs := make(map[string]*costAgg)

	for _, allocations := range alloc.Data {
		for labelVal, a := range allocations {
			// Skip OpenCost virtual entries (__unallocated__, __idle__, etc.)
			if isVirtualAllocation(labelVal) {
				continue
			}
			if _, ok := costs[labelVal]; !ok {
				costs[labelVal] = &costAgg{}
			}
			c := costs[labelVal]
			c.cpu += a.CPUCost
			c.ram += a.RAMCost
			c.pv += a.PVCost
			c.network += a.NetworkCost
			c.lb += a.LoadBalancerCost
			c.total += a.TotalCost
		}
	}

	entries := make([]models.ChargebackEntry, 0, len(costs))
	for labelVal, cost := range costs {
		entries = append(entries, models.ChargebackEntry{
			LabelValue:  labelVal,
			CPUCost:     cost.cpu * monthlyMultiplier,
			RAMCost:     cost.ram * monthlyMultiplier,
			StorageCost: cost.pv * monthlyMultiplier,
			NetworkCost: cost.network * monthlyMultiplier,
			LBCost:      cost.lb * monthlyMultiplier,
			TotalCost:   cost.total * monthlyMultiplier,
			Efficiency:  effMap[labelVal],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TotalCost > entries[j].TotalCost
	})

	return entries
}

// AutocompleteResponse mirrors the wire format of OpenCost's /allocation/autocomplete.
type AutocompleteResponse struct {
	Code int `json:"code"`
	Data struct {
		Data []string `json:"data"`
	} `json:"data"`
}

// GetLabelKeys returns the label keys carried by pods over the last 24 hours (the chargeback
// window), in a namespace or, when namespace is empty, across the cluster. OpenCost reports them
// sanitized (app.kubernetes.io/name becomes app_kubernetes_io_name), a form aggregate=label:
// accepts as it is. Labels nobody would charge back by are left out; see isBookkeepingLabel.
// Needs OpenCost 1.120.3 or later.
func (c *Client) GetLabelKeys(ctx context.Context, namespace string) ([]string, error) {
	params := url.Values{}
	params.Add("window", windowParam(last24h()))
	params.Add("field", "label")
	if namespace != "" {
		params.Add("filter", fmt.Sprintf("namespace:%q", namespace))
	}

	requestURL := fmt.Sprintf("%s/allocation/autocomplete?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost label keys GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create autocomplete request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost autocomplete request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AutocompleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode autocomplete response: %w", err)
	}

	return chargebackLabelKeys(result.Data.Data), nil
}

// chargebackLabelKeys drops bookkeeping labels from keys and returns the rest sorted, without
// duplicates. It never returns nil, so an empty result encodes as [].
func chargebackLabelKeys(keys []string) []string {
	kept := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] || isBookkeepingLabel(key) {
			continue
		}
		seen[key] = true
		kept = append(kept, key)
	}
	sort.Strings(kept)
	return kept
}

// bookkeepingLabelPrefixes are the sanitized prefixes of labels Kubernetes, cloud providers and
// controllers set for their own use. OpenCost merges node and namespace labels into a pod's
// labels, so these show up next to the ones teams set. app_kubernetes_io_ is not among them:
// app.kubernetes.io/name and its siblings are the usual way to name an app.
var bookkeepingLabelPrefixes = []string{
	"kubernetes_io_",
	"beta_kubernetes_io_",
	"node_kubernetes_io_",
	"node_role_kubernetes_io_",
	"topology_kubernetes_io_",
	"failure_domain_beta_kubernetes_io_",
	"apps_kubernetes_io_",
	"batch_kubernetes_io_",
	"statefulset_kubernetes_io_",
	"eks_amazonaws_com_",
	"cloud_google_com_",
	"kubernetes_azure_com_",
	"karpenter_",
	"svccontroller_k3s_cattle_io_",
}

// bookkeepingLabels are whole sanitized keys set by controllers or OpenCost itself.
var bookkeepingLabels = map[string]bool{
	"controller_revision_hash": true,
	"controller_uid":           true,
	"job_name":                 true,
	"pod_template_generation":  true,
	"pod_template_hash":        true,
	"providerID":               true,
}

func isBookkeepingLabel(key string) bool {
	if bookkeepingLabels[key] {
		return true
	}
	for _, prefix := range bookkeepingLabelPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func parseOrphanedPods(resp AllocationResponse) []models.OrphanedPod {
	const minCostThreshold = 0.001
	monthlyMultiplier := monthlyFactor(measuredPeriod(resp))

	seen := make(map[string]bool)
	var pods []models.OrphanedPod

	for _, allocations := range resp.Data {
		for podKey, alloc := range allocations {
			// A pod is orphaned only when it uses no CPU or RAM
			if alloc.CPUCoreUsageAverage > 0 || alloc.RAMByteUsageAverage > 0 {
				continue
			}
			// Skip if no meaningful cost remains (pod fully wound down)
			if alloc.TotalCost*monthlyMultiplier < minCostThreshold {
				continue
			}
			// Skip OpenCost virtual entries like __idle__ or __unallocated__
			if isVirtualAllocation(podKey) {
				continue
			}

			name := podKey
			if alloc.Properties.Pod != "" {
				name = alloc.Properties.Pod
			}
			if seen[name] {
				continue
			}
			seen[name] = true

			pods = append(pods, models.OrphanedPod{
				Name:           name,
				Namespace:      alloc.Properties.Namespace,
				Controller:     alloc.Properties.Controller,
				ControllerKind: alloc.Properties.ControllerKind,
				CPUCost:        alloc.CPUCost * monthlyMultiplier,
				RAMCost:        alloc.RAMCost * monthlyMultiplier,
				StorageCost:    alloc.PVCost * monthlyMultiplier,
				TotalCost:      alloc.TotalCost * monthlyMultiplier,
			})
		}
	}

	sort.Slice(pods, func(i, j int) bool {
		return pods[i].TotalCost > pods[j].TotalCost
	})

	return pods
}

// dayWindow renders the UTC days from through to (both included; only their dates count) as
// OpenCost's window parameter. A range reaching past now ends at now, so the current day comes back
// partly measured.
func dayWindow(from, to, now time.Time) string {
	day := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	start := day(from.UTC())
	end := day(to.UTC()).AddDate(0, 0, 1)
	if limit := now.UTC().Truncate(time.Minute); end.After(limit) {
		end = limit
	}
	if !end.After(start) {
		end = start.Add(time.Minute)
	}
	return start.Format(time.RFC3339) + "," + end.Format(time.RFC3339)
}

// GetDailyTimeSeries returns a namespace's cost for each UTC day from through to. With resources, it covers only the pods
// they own: the series is then queried per controller, which OpenCost reports with each controller's
// kind and name. Pods without a controller fall under OpenCost's unallocated entry there, so an
// application's bare pods are left out of its series.
func (c *Client) GetDailyTimeSeries(
	ctx context.Context, namespace string, from, to time.Time, resources models.ManagedResources,
) ([]models.DailyCost, error) {
	aggregate := "namespace"
	if resources != nil {
		aggregate = "controller"
	}

	params := url.Values{}
	params.Add("window", dayWindow(from, to, time.Now()))
	params.Add("step", "1d")
	params.Add("aggregate", aggregate)
	params.Add("filter", fmt.Sprintf("namespace:%q", namespace))

	requestURL := fmt.Sprintf("%s/allocation?%s", c.baseURL, params.Encode())
	c.logf("DEBUG opencost GET %s", requestURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost daily request failed: %w", err)
	}
	defer resp.Body.Close()

	c.logf("DEBUG opencost daily timeseries response: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AllocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost daily response: %w", err)
	}

	if resources != nil {
		result = ownedControllers(result, namespace, resources)
	}
	return parseDailyTimeSeries(result), nil
}

// ownedControllers keeps the controller allocations the resources own. A day on which none of them
// ran is left with no entries, and the series skips it.
func ownedControllers(resp AllocationResponse, namespace string, resources models.ManagedResources) AllocationResponse {
	owned := AllocationResponse{Code: resp.Code, Data: make([]map[string]AllocationData, 0, len(resp.Data))}
	for _, allocations := range resp.Data {
		kept := make(map[string]AllocationData)
		for key, alloc := range allocations {
			if isVirtualAllocation(key) {
				continue
			}
			ns := alloc.Properties.Namespace
			if ns == "" {
				ns = namespace
			}
			if resources.OwnsPod(ns, alloc.Properties.ControllerKind, alloc.Properties.Controller, "") {
				kept[key] = alloc
			}
		}
		owned.Data = append(owned.Data, kept)
	}
	return owned
}

func parseDailyTimeSeries(resp AllocationResponse) []models.DailyCost {
	dailyCosts := make([]models.DailyCost, 0, len(resp.Data))

	for _, allocations := range resp.Data {
		var date string
		var totalCPU, totalMem, totalNetwork, totalStorage, totalCost float64
		for _, alloc := range allocations {
			if date == "" && !alloc.Start.IsZero() {
				date = alloc.Start.UTC().Format("2006-01-02")
			}
			totalCPU += alloc.CPUCost
			totalMem += alloc.RAMCost
			totalNetwork += alloc.NetworkCost
			totalStorage += alloc.PVCost
			totalCost += alloc.TotalCost
		}
		if date == "" {
			continue
		}
		if totalCost == 0 {
			totalCost = totalCPU + totalMem + totalNetwork + totalStorage
		}

		dailyCosts = append(dailyCosts, models.DailyCost{
			Date:        date,
			TotalCost:   totalCost,
			CPUCost:     totalCPU,
			MemoryCost:  totalMem,
			NetworkCost: totalNetwork,
			StorageCost: totalStorage,
		})
	}

	sort.Slice(dailyCosts, func(i, j int) bool {
		return dailyCosts[i].Date < dailyCosts[j].Date
	})

	return dailyCosts
}

func parsePodBreakdown(resp AllocationResponse) []models.PodCost {
	podMap := make(map[string]*models.PodCost)

	for _, allocations := range resp.Data {
		for podKey, alloc := range allocations {
			name := podKey
			if alloc.Properties.Pod != "" {
				name = alloc.Properties.Pod
			}

			if existing, ok := podMap[name]; ok {
				existing.CPUCost += alloc.CPUCost
				existing.RAMCost += alloc.RAMCost
				existing.TotalCost += alloc.TotalCost
			} else {
				podMap[name] = &models.PodCost{
					Name:           name,
					Namespace:      alloc.Properties.Namespace,
					Controller:     alloc.Properties.Controller,
					ControllerKind: alloc.Properties.ControllerKind,
					CPUCost:        alloc.CPUCost,
					RAMCost:        alloc.RAMCost,
					TotalCost:      alloc.TotalCost,
				}
			}
		}
	}

	const minCostThreshold = 0.001 // skip terminated/zero-cost pods
	monthlyMultiplier := monthlyFactor(measuredPeriod(resp))
	pods := make([]models.PodCost, 0, len(podMap))
	for _, p := range podMap {
		monthlyCost := p.TotalCost * monthlyMultiplier
		if monthlyCost < minCostThreshold {
			continue
		}
		pods = append(pods, models.PodCost{
			Name:           p.Name,
			Namespace:      p.Namespace,
			Controller:     p.Controller,
			ControllerKind: p.ControllerKind,
			CPUCost:        p.CPUCost * monthlyMultiplier,
			RAMCost:        p.RAMCost * monthlyMultiplier,
			TotalCost:      monthlyCost,
		})
	}
	return pods
}

func parseSummaryResponse(resp SummaryAllocationResponse) *models.EfficiencyMetrics {
	var agg SummaryAllocationData
	var count int

	for _, set := range resp.Data.Sets {
		for _, alloc := range set.Allocations {
			if !alloc.Start.IsZero() && (agg.Start.IsZero() || alloc.Start.Before(agg.Start)) {
				agg.Start = alloc.Start
			}
			if alloc.End.After(agg.End) {
				agg.End = alloc.End
			}
			agg.CPUCoreRequestAverage += alloc.CPUCoreRequestAverage
			agg.CPUCoreUsageAverage += alloc.CPUCoreUsageAverage
			agg.CPUCoreLimitAverage += alloc.CPUCoreLimitAverage
			agg.RAMByteRequestAverage += alloc.RAMByteRequestAverage
			agg.RAMByteUsageAverage += alloc.RAMByteUsageAverage
			agg.RAMByteLimitAverage += alloc.RAMByteLimitAverage
			agg.CPUCostIdle += alloc.CPUCostIdle
			agg.RAMCostIdle += alloc.RAMCostIdle
			agg.Efficiency += alloc.Efficiency
			count++
		}
	}

	if count == 0 {
		return nil
	}

	var cpuEff, ramEff float64
	if agg.CPUCoreRequestAverage > 0 {
		cpuEff = agg.CPUCoreUsageAverage / agg.CPUCoreRequestAverage
	}
	if agg.RAMByteRequestAverage > 0 {
		ramEff = agg.RAMByteUsageAverage / agg.RAMByteRequestAverage
	}

	monthlyMultiplier := monthlyFactor(agg.Start, agg.End)

	return &models.EfficiencyMetrics{
		CPUEfficiency:     cpuEff,
		RAMEfficiency:     ramEff,
		TotalEfficiency:   agg.Efficiency / float64(count),
		CPUCoresRequested: agg.CPUCoreRequestAverage,
		CPUCoresUsed:      agg.CPUCoreUsageAverage,
		RAMBytesRequested: agg.RAMByteRequestAverage,
		RAMBytesUsed:      agg.RAMByteUsageAverage,
		CPUIdleCost:       agg.CPUCostIdle * monthlyMultiplier,
		RAMIdleCost:       agg.RAMCostIdle * monthlyMultiplier,
		CPUCoresLimit:     agg.CPUCoreLimitAverage,
		RAMBytesLimit:     agg.RAMByteLimitAverage,
	}
}

func parseOpenCostResponse(resp AllocationResponse, startTime, endTime time.Time) *models.OpenCostMetrics {
	metrics := &models.OpenCostMetrics{
		// In UTC, like the window the request sent (see windowParam).
		Window: []string{
			startTime.UTC().Format(time.RFC3339),
			endTime.UTC().Format(time.RFC3339),
		},
		ResourceDetails: make(map[string]any),
	}

	var totalCPU, totalMem, totalStorage, totalGPU, totalNetwork, totalLB, totalCarbon, totalShared float64
	var totalNetXZ, totalNetXR, totalNetInternet, totalNetNat float64
	var peakCPU, peakRAM float64
	sharedBreakdown := make(map[string]float64)

	for i, allocations := range resp.Data {
		key := fmt.Sprintf("window-%d", i)
		for _, alloc := range allocations {
			if !alloc.Start.IsZero() {
				key = alloc.Start.UTC().Format(time.RFC3339)
			}
			break
		}
		metrics.ResourceDetails[key] = allocations
		for _, alloc := range allocations {
			totalCPU += alloc.CPUCost
			totalMem += alloc.RAMCost
			totalStorage += alloc.PVCost
			totalGPU += alloc.GPUCost
			totalNetwork += alloc.NetworkCost
			totalLB += alloc.LoadBalancerCost
			totalCarbon += alloc.CarbonCost
			totalShared += alloc.SharedCost
			totalNetXZ += alloc.NetworkCrossZoneCost
			totalNetXR += alloc.NetworkCrossRegionCost
			totalNetInternet += alloc.NetworkInternetCost
			totalNetNat += alloc.NetworkNatGatewayEgressCost + alloc.NetworkNatGatewayIngressCost
			if alloc.RawAllocationOnly != nil {
				peakCPU += alloc.RawAllocationOnly.CPUCoreUsageMax
				peakRAM += alloc.RawAllocationOnly.RAMByteUsageMax
			}
			for name, item := range alloc.SharedCostBreakdown {
				sharedBreakdown[name] += item.TotalCost
			}
		}
	}

	// Project onto a 30-day month from the period OpenCost actually measured.
	measuredStart, measuredEnd := measuredPeriod(resp)
	monthlyMultiplier := monthlyFactor(measuredStart, measuredEnd)
	if !measuredStart.IsZero() && measuredEnd.After(measuredStart) {
		metrics.MeasuredHours = measuredEnd.Sub(measuredStart).Hours()
	}

	var scaledSharedBreakdown map[string]float64
	if len(sharedBreakdown) > 0 {
		scaledSharedBreakdown = make(map[string]float64, len(sharedBreakdown))
		for k, v := range sharedBreakdown {
			scaledSharedBreakdown[k] = v * monthlyMultiplier
		}
	}

	metrics.AllocationTotals = models.AllocationTotal{
		CPUCost:                totalCPU * monthlyMultiplier,
		MemoryCost:             totalMem * monthlyMultiplier,
		StorageCost:            totalStorage * monthlyMultiplier,
		GPUCost:                totalGPU * monthlyMultiplier,
		NetworkCost:            totalNetwork * monthlyMultiplier,
		NetworkCrossZoneCost:   totalNetXZ * monthlyMultiplier,
		NetworkCrossRegionCost: totalNetXR * monthlyMultiplier,
		NetworkInternetCost:    totalNetInternet * monthlyMultiplier,
		NetworkNatGatewayCost:  totalNetNat * monthlyMultiplier,
		LoadBalancerCost:       totalLB * monthlyMultiplier,
		TotalCost:              (totalCPU + totalMem + totalStorage + totalGPU + totalNetwork + totalLB) * monthlyMultiplier,
		CarbonCost:             totalCarbon * monthlyMultiplier,
		SharedCost:             totalShared * monthlyMultiplier,
		SharedCostBreakdown:    scaledSharedBreakdown,
	}

	metrics.PeakCPUCores = peakCPU
	metrics.PeakRAMBytes = peakRAM

	return metrics
}

// GetDataQuality calls OpenCost's /pricingSourceStatus endpoint and returns a
// DataQuality struct. Errors and unexpected responses are surfaced as
// PricingSource="unknown" rather than failing the caller — data quality is
// advisory, not load-bearing.
func (c *Client) GetDataQuality(ctx context.Context) (*models.DataQuality, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/pricingSourceStatus", http.NoBody)
	if err != nil {
		return &models.DataQuality{PricingSource: "unknown", Message: "failed to build request"}, nil
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return &models.DataQuality{PricingSource: "unknown", Message: "OpenCost unreachable"}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logf("DEBUG opencost pricingSourceStatus: %d %s", resp.StatusCode, string(body))

	if resp.StatusCode != http.StatusOK {
		return &models.DataQuality{PricingSource: "unknown", Message: fmt.Sprintf("status %d", resp.StatusCode)}, nil
	}

	// The data field is a plain string message in most OpenCost versions.
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return &models.DataQuality{PricingSource: "unknown", Message: "unreadable response"}, nil
	}

	var msg string
	_ = json.Unmarshal(envelope.Data, &msg)

	return &models.DataQuality{
		PricingSource: classifyPricingSource(msg),
		Message:       msg,
	}, nil
}

// classifyPricingSource maps a raw OpenCost status message to one of three tiers:
// "real" (cloud-billing prices), "estimated" (on-demand defaults), "unknown".
func classifyPricingSource(msg string) string {
	if msg == "" {
		return "unknown"
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "default") || strings.Contains(lower, "list price") {
		return "estimated"
	}
	return "real"
}

// AssetsResponse mirrors the top-level shape of GET /assets.
type AssetsResponse struct {
	Code int                    `json:"code"`
	Data []map[string]AssetData `json:"data"`
}

type AssetProperties struct {
	Provider string `json:"provider"`
	Cluster  string `json:"cluster"`
}

type AssetData struct {
	Start      time.Time       `json:"start"`
	End        time.Time       `json:"end"`
	Type       string          `json:"type"`
	Properties AssetProperties `json:"properties"`
	TotalCost  float64         `json:"totalCost"`
}

// GetClusterOverhead fetches the ClusterManagement asset (EKS/GKE/AKS control-plane
// fee) from OpenCost /assets and returns the monthly cost. Returns an empty
// ClusterOverhead (cost = 0) when no ClusterManagement entry exists, which is
// normal for self-managed clusters.
func (c *Client) GetClusterOverhead(ctx context.Context) (*models.ClusterOverhead, error) {
	u, err := url.Parse(c.baseURL + "/assets")
	if err != nil {
		return nil, fmt.Errorf("parsing assets URL: %w", err)
	}
	q := u.Query()
	q.Set("window", "24h")
	q.Set("aggregate", "type")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating assets request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing assets request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencost returned status %d: %s", resp.StatusCode, body)
	}

	var result AssetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding assets response: %w", err)
	}

	for _, bucket := range result.Data {
		if mgmt, ok := bucket["ClusterManagement"]; ok {
			return &models.ClusterOverhead{
				ControlPlaneCost: mgmt.TotalCost * monthlyFactor(mgmt.Start, mgmt.End),
				Provider:         mgmt.Properties.Provider,
				Cluster:          mgmt.Properties.Cluster,
			}, nil
		}
	}

	return &models.ClusterOverhead{}, nil
}
