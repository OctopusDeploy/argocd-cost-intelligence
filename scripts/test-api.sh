#!/bin/bash

BASE_URL=${BASE_URL:-http://localhost:8080}
# The token /on-sync requires; docker-compose and `make run` use this one.
WEBHOOK_SECRET=${WEBHOOK_SECRET:-local-dev-webhook-secret}
# Argo CD's proxy sets this header; the API refuses application routes without it.
APP_HEADER="Argocd-Application-Name: default:test-app"
TIMESTAMP=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

echo "=== Testing ArgoCD Cost Intelligence API ==="
echo "Base URL: $BASE_URL"
echo ""

# Test 1: Health check
echo "1. Testing /health endpoint..."
curl -s "$BASE_URL/health" | jq '.' || echo "Failed"
echo ""

# Test 2: Readiness check
echo "2. Testing /ready endpoint..."
curl -s "$BASE_URL/ready" | jq '.' || echo "Failed"
echo ""

# Test 3: Send sync notification
echo "3. Sending sync notification (on-sync webhook)..."
curl -s -X POST "$BASE_URL/api/cost-analysis/on-sync" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $WEBHOOK_SECRET" \
  -d "{
    \"applicationName\": \"test-app\",
    \"applicationNamespace\": \"default\",
    \"namespace\": \"default\",
    \"syncTimestamp\": \"$TIMESTAMP\",
    \"syncResult\": \"Succeeded\"
  }" | jq '.'
echo ""

# Wait a moment for processing
echo "Waiting 2 seconds for webhook processing..."
sleep 2

# Test 4: Get actual cost
echo "4. Getting actual cost for test-app..."
curl -s -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/test-app/actual" | jq '.' || echo "No cost data yet"
echo ""

# Test 5: Get cost trend
echo "5. Getting 7-day cost trend for test-app..."
curl -s -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/test-app/trend?days=7" | jq '.' || echo "No trend data yet"
echo ""

# Test 6: Get namespace summary
echo "6. Getting namespace cost summary for 'default' (needs CLUSTER_SCOPE_APP=default:test-app on the server)..."
curl -s -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/namespace/default/summary?days=30" | jq '.' || echo "No summary data yet"
echo ""

echo "=== Test Complete ==="
