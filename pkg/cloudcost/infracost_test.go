package cloudcost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInfracost(t *testing.T) {
	t.Run("parses a standard breakdown", func(t *testing.T) {
		raw := []byte(`{
			"currency": "USD",
			"totalMonthlyCost": "115.00",
			"projects": [{
				"breakdown": {
					"totalMonthlyCost": "115.00",
					"resources": [
						{"name": "aws_db_instance.main", "resourceType": "aws_db_instance", "monthlyCost": "72.00"},
						{"name": "aws_elasticache_cluster.cache", "resourceType": "aws_elasticache_cluster", "monthlyCost": "38.00"},
						{"name": "aws_s3_bucket.assets", "resourceType": "aws_s3_bucket", "monthlyCost": "5.00"}
					]
				}
			}]
		}`)

		est, err := ParseInfracost(raw)
		require.NoError(t, err)
		assert.Equal(t, "infracost", est.Source)
		assert.InDelta(t, 115.00, est.TotalMonthlyCost, 1e-9)
		assert.Equal(t, "USD", est.Currency)
		assert.Len(t, est.Resources, 3)
		assert.Equal(t, "aws_db_instance.main", est.Resources[0].Name)
		assert.Equal(t, "aws_db_instance", est.Resources[0].ResourceType)
		assert.InDelta(t, 72.00, est.Resources[0].MonthlyCost, 1e-9)
	})

	t.Run("skips zero-cost resources", func(t *testing.T) {
		raw := []byte(`{
			"currency": "USD",
			"totalMonthlyCost": "72.00",
			"projects": [{
				"breakdown": {
					"totalMonthlyCost": "72.00",
					"resources": [
						{"name": "aws_db_instance.main", "resourceType": "aws_db_instance", "monthlyCost": "72.00"},
						{"name": "aws_iam_role.app", "resourceType": "aws_iam_role", "monthlyCost": "0"},
						{"name": "aws_s3_bucket.logs", "resourceType": "aws_s3_bucket", "monthlyCost": "null"}
					]
				}
			}]
		}`)

		est, err := ParseInfracost(raw)
		require.NoError(t, err)
		assert.Len(t, est.Resources, 1)
		assert.Equal(t, "aws_db_instance.main", est.Resources[0].Name)
	})

	t.Run("defaults currency to USD when absent", func(t *testing.T) {
		raw := []byte(`{"totalMonthlyCost": "10.00", "projects": []}`)
		est, err := ParseInfracost(raw)
		require.NoError(t, err)
		assert.Equal(t, "USD", est.Currency)
	})

	t.Run("aggregates total from project breakdown when top-level total is missing", func(t *testing.T) {
		raw := []byte(`{
			"currency": "USD",
			"projects": [{
				"breakdown": {
					"totalMonthlyCost": "50.00",
					"resources": [
						{"name": "aws_instance.web", "resourceType": "aws_instance", "monthlyCost": "50.00"}
					]
				}
			}]
		}`)

		est, err := ParseInfracost(raw)
		require.NoError(t, err)
		assert.InDelta(t, 50.00, est.TotalMonthlyCost, 1e-9)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		_, err := ParseInfracost([]byte(`not json`))
		assert.Error(t, err)
	})

	t.Run("returns empty resources on empty projects", func(t *testing.T) {
		raw := []byte(`{"currency": "USD", "totalMonthlyCost": "0", "projects": []}`)
		est, err := ParseInfracost(raw)
		require.NoError(t, err)
		assert.Empty(t, est.Resources)
	})
}
