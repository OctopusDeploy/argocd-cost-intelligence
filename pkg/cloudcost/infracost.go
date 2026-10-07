// Package cloudcost normalises cost estimates from IaC cost tools (Infracost, Terracost, …)
// into the tool-agnostic models.CloudServiceEstimate. Each tool gets its own Parse* function;
// the storage and API layers only ever see the normalised model.
package cloudcost

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

// infracostJSON mirrors the schema produced by `infracost breakdown --format json`.
// Only the fields we consume are declared; unknown fields are ignored.
type infracostJSON struct {
	Currency         string `json:"currency"`
	TotalMonthlyCost string `json:"totalMonthlyCost"`
	Projects         []struct {
		Breakdown struct {
			TotalMonthlyCost string `json:"totalMonthlyCost"`
			Resources        []struct {
				Name         string `json:"name"`
				ResourceType string `json:"resourceType"`
				MonthlyCost  string `json:"monthlyCost"`
			} `json:"resources"`
		} `json:"breakdown"`
	} `json:"projects"`
}

// ParseInfracost converts raw `infracost breakdown --format json` output into a
// tool-agnostic CloudServiceEstimate. Resources with null or zero monthly cost are
// omitted. Returns an error only when the JSON cannot be parsed at all.
func ParseInfracost(data []byte) (*models.CloudServiceEstimate, error) {
	var raw infracostJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid infracost JSON: %w", err)
	}

	currency := raw.Currency
	if currency == "" {
		currency = "USD"
	}

	total := parseDecimal(raw.TotalMonthlyCost)

	var resources []models.CloudServiceResource
	for _, proj := range raw.Projects {
		// Use project-level total if top-level total is missing.
		if total == 0 {
			total += parseDecimal(proj.Breakdown.TotalMonthlyCost)
		}
		for _, r := range proj.Breakdown.Resources {
			cost := parseDecimal(r.MonthlyCost)
			if cost == 0 {
				continue
			}
			resources = append(resources, models.CloudServiceResource{
				Name:         r.Name,
				ResourceType: r.ResourceType,
				MonthlyCost:  cost,
			})
		}
	}

	return &models.CloudServiceEstimate{
		Source:           "infracost",
		TotalMonthlyCost: total,
		Currency:         currency,
		Resources:        resources,
		PushedAt:         time.Now().UTC(),
	}, nil
}

// parseDecimal converts an Infracost decimal string (e.g. "72.00", "null", "") to float64.
// Returns 0 on any parse failure — callers treat 0 as "unknown / not applicable".
func parseDecimal(s string) float64 {
	if s == "" || s == "null" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
