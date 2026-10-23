#!/bin/bash

BASE_URL=${BASE_URL:-http://localhost:8080}
# The token /on-sync requires; Docker Compose and `make run` use this one.
WEBHOOK_SECRET=${WEBHOOK_SECRET:-local-dev-webhook-secret}
# nginx is one of the deployments the mock OpenCost reports in "default" outside a cluster, so the
# application's own share has a cost.
APP=nginx
# Argo CD's proxy sets this header; the API refuses application routes without it.
APP_HEADER="Argocd-Application-Name: argocd:$APP"
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
curl -s -o /dev/null -w "HTTP %{http_code}\n" -X POST "$BASE_URL/api/cost-analysis/on-sync" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $WEBHOOK_SECRET" \
  -d "{
    \"applicationName\": \"$APP\",
    \"applicationNamespace\": \"argocd\",
    \"namespace\": \"default\",
    \"resources\": [{\"kind\": \"Deployment\", \"name\": \"$APP\", \"namespace\": \"default\"}],
    \"syncTimestamp\": \"$TIMESTAMP\",
    \"syncResult\": \"Succeeded\"
  }"
echo ""

# The service waits 5 seconds before it measures, then writes the record. Wait for the one of this
# sync, not one an earlier run left in the database.
echo "Waiting for the cost record..."
recorded=false
for _ in $(seq 1 20); do
  if curl -sf -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/$APP/actual" |
    jq -e --arg ts "$TIMESTAMP" '.syncTimestamp == $ts' >/dev/null 2>&1; then
    recorded=true
    break
  fi
  sleep 1
done
[ "$recorded" = true ] || echo "No record for this sync after 20 seconds; check the service log."
echo ""

# Test 4: Get actual cost
echo "4. Getting actual cost for $APP..."
curl -s -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/$APP/actual" | jq '.' || echo "No cost data yet"
echo ""

# Test 5: Get cost trend
echo "5. Getting 7-day cost trend for $APP..."
curl -s -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/$APP/trend?days=7" | jq '.' || echo "No trend data yet"
echo ""

# Test 6: Get namespace summary
echo "6. Getting namespace cost summary for 'default' (needs CLUSTER_SCOPE_APP=argocd:$APP on the server)..."
curl -s -H "$APP_HEADER" "$BASE_URL/api/cost-analysis/namespace/default/summary?days=30" | jq '.' || echo "No summary data yet"
echo ""

echo "=== Test Complete ==="
