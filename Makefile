.PHONY: help build run test test-integration test-opencost clean docker-build docker-build-ui docker-push \
        deploy-local deploy-local-down deploy-k8s helm-deploy helm-deploy-real helm-undeploy \
        lint lint-go lint-ui lint-helm lint-prometheus lint-actions lint-manifests lint-dockerfile \
        lint-shell lint-mock lint-notifications sync-mock ui-extension docs docs-serve

BINARY_NAME    = argocd-cost-intelligence
IMAGE_NAME     = argocd-cost-intelligence
UI_IMAGE_NAME  = $(IMAGE_NAME)-ui
IMAGE_TAG     ?= latest
REGISTRY      ?=
K3D_CLUSTER   ?= mycluster
HELM_RELEASE  ?= argocd-cost-intelligence
HELM_VALUES   ?= helm/values-local.yaml
ARGOCD_NS     ?= argocd
APP_NS        ?= argocd-cost-intel
OPENCOST_NS   ?= opencost

# Linters that run in a container, so nothing needs installing. Keep the tags in step with CI, which
# calls these same targets.
GOLANGCI_LINT_VERSION ?= v2.14.0
ACTIONLINT_IMAGE      ?= rhysd/actionlint:1.7.12
KUBECONFORM_IMAGE     ?= ghcr.io/yannh/kubeconform:v0.8.0
HADOLINT_IMAGE        ?= hadolint/hadolint:v2.15.1
SHELLCHECK_IMAGE      ?= koalaman/shellcheck:v0.11.0
PROMTOOL_IMAGE        ?= prom/prometheus:v3.15.0

DOCKER_RUN = docker run --rm -v "$(CURDIR):/repo" -w /repo
# What the chart renders with each feature on, so kubeconform sees every optional object.
HELM_TEMPLATE = helm template argocd-cost-intelligence ./helm --set postgres.auth.password=lint \
	--set metrics.serviceMonitor.enabled=true --set metrics.prometheusRule.enabled=true

help:
	@echo "Available targets:"
	@echo "  make build           - Build the binary"
	@echo "  make run             - Run the service locally"
	@echo "  make test            - Run unit tests"
	@echo "  make test-integration - Run integration tests (storage needs Docker)"
	@echo "  make test-opencost   - Run OpenCost client tests against a live or mock OpenCost"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make docker-build    - Build backend Docker image"
	@echo "  make docker-build-ui - Build UI Docker image"
	@echo "  make ui-extension    - Build the UI plugin archive and checksum for the extension installer (dist/)"
	@echo "  make docker-push     - Push Docker image to registry"
	@echo "  make docker-run      - Run Docker container locally"
	@echo "  make deploy-local    - Deploy with docker-compose"
	@echo "  make deploy-k8s      - Deploy to Kubernetes (raw manifests)"
	@echo "  make helm-deploy      - Build images, import into k3d, deploy with mock OpenCost"
	@echo "  make helm-deploy-real - Build images, import into k3d, deploy with real OpenCost + Prometheus"
	@echo "  make helm-undeploy    - Uninstall Helm release and clean up (mock or real)"
	@echo "  make logs            - View service logs"
	@echo "  make fmt             - Format code"
	@echo "  make lint            - Run every linter below"
	@echo "  make lint-go         - golangci-lint"
	@echo "  make lint-ui         - ESLint and type check for the UI plugin"
	@echo "  make lint-helm       - helm lint"
	@echo "  make lint-prometheus - promtool check and test of the alert rules"
	@echo "  make lint-actions    - actionlint for .github/workflows"
	@echo "  make lint-manifests  - kubeconform for deploy/ and the rendered chart"
	@echo "  make lint-dockerfile - hadolint for the Dockerfiles"
	@echo "  make lint-shell      - shellcheck for scripts/"
	@echo "  make lint-mock       - check deploy/mock-opencost.yaml matches mock-opencost-server.js"
	@echo "  make lint-notifications - check the copies in deploy/ and docs/ of what the hook writes to Argo CD (needs helm, jq)"
	@echo "  make sync-mock       - regenerate deploy/mock-opencost.yaml from mock-opencost-server.js"
	@echo "  make docs            - Build the documentation site into site/ (fails on a broken link)"
	@echo "  make docs-serve      - Serve the documentation site on http://127.0.0.1:8000/en/latest/"
	@echo ""
	@echo "Helm deploy overrides (e.g. make helm-deploy K3D_CLUSTER=mycluster):"
	@echo "  K3D_CLUSTER=$(K3D_CLUSTER)  HELM_RELEASE=$(HELM_RELEASE)"
	@echo "  HELM_VALUES=$(HELM_VALUES)"
	@echo "  ARGOCD_NS=$(ARGOCD_NS)      APP_NS=$(APP_NS)"

build:
	@echo "Building $(BINARY_NAME)..."
	CGO_ENABLED=0 go build -o bin/$(BINARY_NAME) ./cmd

run: build
	@echo "Running $(BINARY_NAME)..."
	DB_PASSWORD=test-password WEBHOOK_SECRET=local-dev-webhook-secret ./bin/$(BINARY_NAME)

test:
	@echo "Running tests..."
	go test -v ./...

test-integration:
	@echo "Running integration tests..."
	go test -v -tags=integration -timeout=120s ./...

test-opencost: ## Run OpenCost integration tests against a live server
	@echo "Tip: port-forward first:"
	@echo "  kubectl port-forward svc/opencost 9003:9003 -n $(OPENCOST_NS)"
	@echo ""
	OPENCOST_BASE_URL=$${OPENCOST_BASE_URL:-http://localhost:9003} \
	TEST_NAMESPACE=$${TEST_NAMESPACE:-argocd} \
	go test -v -tags=integration -timeout=30s ./pkg/opencost/...

clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f *.out

docker-build:
	@echo "Building backend Docker image $(IMAGE_NAME):$(IMAGE_TAG)..."
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-build-ui:
	@echo "Building UI Docker image $(UI_IMAGE_NAME):$(IMAGE_TAG)..."
	docker build -t $(UI_IMAGE_NAME):$(IMAGE_TAG) ./ui

# The archive and checksum file the release attaches for the Argo CD extension installer.
ui-extension:
	@echo "Packaging the UI plugin for the extension installer..."
	cd ui && npm ci && npm run build
	scripts/package-ui-extension.sh dist

docker-push:
	@echo "Pushing Docker image to registry..."
	@if [ -z "$(REGISTRY)" ]; then \
		echo "Error: REGISTRY not set. Usage: make docker-push REGISTRY=your-registry"; \
		exit 1; \
	fi
	docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

docker-run: docker-build
	@echo "Running Docker container..."
	docker run -p 8080:8080 \
		-e DB_PASSWORD=test-password \
		$(IMAGE_NAME):$(IMAGE_TAG)

deploy-local:
	@echo "Deploying with docker-compose..."
	docker-compose up -d
	@echo "Services started. Access at http://localhost:8080"
	@docker-compose ps

deploy-local-down:
	@echo "Stopping docker-compose services..."
	docker-compose down

deploy-k8s:
	@echo "Deploying to Kubernetes..."
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/secrets.yaml
	kubectl apply -f deploy/postgres.yaml
	kubectl apply -f deploy/rbac.yaml
	kubectl apply -f deploy/deployment.yaml
	kubectl apply -f deploy/exporter.yaml
	@echo "Services deployed to argocd-cost-intel namespace"
	@echo "Before the service can start, replace the webhook token placeholder in deploy/secrets.yaml (see the comment there)."
	@echo "Patch Argo CD notifications: copy the token into argocd-notifications-secret, then"
	@echo "  kubectl patch cm argocd-notifications-cm -n argocd --patch-file deploy/argocd-notifications-patch.yaml"
	@echo "(both steps are in the comment at the top of deploy/argocd-notifications-patch.yaml)"

KUBECTL_IMAGE ?= alpine/k8s:1.32.3

helm-deploy: docker-build docker-build-ui
	@echo "Importing images into k3d cluster '$(K3D_CLUSTER)'..."
	k3d image import $(IMAGE_NAME):$(IMAGE_TAG) $(UI_IMAGE_NAME):$(IMAGE_TAG) -c $(K3D_CLUSTER)
	@echo "Importing post-install hook image..."
	docker pull $(KUBECTL_IMAGE)
	k3d image import $(KUBECTL_IMAGE) -c $(K3D_CLUSTER)
	@echo "Deploying mock OpenCost..."
	@# The mock reuses the real chart's names (deploy/svc 'opencost'), and a
	@# Deployment's selector is immutable, so a real install must go first.
	@if helm status opencost -n $(OPENCOST_NS) >/dev/null 2>&1; then \
		echo "  Removing real OpenCost release so the mock can take its place..."; \
		helm uninstall opencost -n $(OPENCOST_NS) --wait; \
	fi
	kubectl apply -f deploy/mock-opencost.yaml
	@echo "Deploying via Helm (release=$(HELM_RELEASE), values=$(HELM_VALUES))..."
	helm upgrade --install $(HELM_RELEASE) ./helm -f $(HELM_VALUES) \
		--set postgres.auth.password=localdev \
		--wait --timeout 600s
	@echo "Restarting backend to pick up rebuilt image (imagePullPolicy: IfNotPresent)..."
	kubectl rollout restart deployment/$(HELM_RELEASE) -n $(APP_NS)
	kubectl rollout status deployment/$(HELM_RELEASE) -n $(APP_NS) --timeout=60s
	@echo ""
	@echo "Deploy complete. ArgoCD registration is handled by the Helm post-install hook."
	@echo "Port-forward ArgoCD: kubectl port-forward svc/argocd-server 18443:443 -n $(ARGOCD_NS)"
	@echo "Port-forward API:    kubectl port-forward svc/$(HELM_RELEASE) 18080:8080 -n $(APP_NS)"

helm-deploy-real: docker-build docker-build-ui
	@echo "==> Importing images into k3d cluster '$(K3D_CLUSTER)'..."
	k3d image import $(IMAGE_NAME):$(IMAGE_TAG) $(UI_IMAGE_NAME):$(IMAGE_TAG) -c $(K3D_CLUSTER)
	@echo "==> Importing post-install hook image..."
	docker pull $(KUBECTL_IMAGE)
	k3d image import $(KUBECTL_IMAGE) -c $(K3D_CLUSTER)
	@echo "==> Removing mock OpenCost, if present (it reuses the chart's resource names)..."
	@if kubectl get configmap mock-opencost-script -n $(OPENCOST_NS) >/dev/null 2>&1; then \
		kubectl delete deployment/opencost service/opencost configmap/mock-opencost-script \
			serviceaccount/mock-opencost -n $(OPENCOST_NS) --ignore-not-found --wait; \
		kubectl delete clusterrolebinding/mock-opencost clusterrole/mock-opencost --ignore-not-found; \
	fi
	@echo "==> Installing Prometheus + real OpenCost..."
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
	helm repo add opencost https://opencost.github.io/opencost-helm-chart 2>/dev/null || true
	helm repo update
	@PROM_NS=$$(helm list -A 2>/dev/null | awk '/^prometheus[[:space:]]/{print $$2}' | head -1); \
	if [ -n "$$PROM_NS" ]; then \
		echo "  Found existing Prometheus release in namespace '$$PROM_NS', reusing it."; \
	else \
		echo "  No existing Prometheus found, installing into $(OPENCOST_NS)..."; \
		helm upgrade --install prometheus prometheus-community/prometheus \
			--namespace $(OPENCOST_NS) --create-namespace \
			--set alertmanager.enabled=false \
			--set prometheus-pushgateway.enabled=false \
			--set server.persistentVolume.enabled=false \
			--wait --timeout 120s; \
		PROM_NS=$(OPENCOST_NS); \
	fi; \
	echo "  OpenCost will use Prometheus at http://prometheus-server.$${PROM_NS}.svc.cluster.local"; \
	helm upgrade --install opencost opencost/opencost \
		--namespace $(OPENCOST_NS) --create-namespace \
		--set opencost.prometheus.internal.namespaceName="$$PROM_NS" \
		--wait --timeout 120s
	@echo "==> Deploying ArgoCD Cost Intelligence (release=$(HELM_RELEASE), values=$(HELM_VALUES))..."
	helm upgrade --install $(HELM_RELEASE) ./helm -f $(HELM_VALUES) \
		--set postgres.auth.password=localdev \
		--wait --timeout 600s
	@echo "==> Restarting backend to pick up rebuilt image (imagePullPolicy: IfNotPresent)..."
	kubectl rollout restart deployment/$(HELM_RELEASE) -n $(APP_NS)
	kubectl rollout status deployment/$(HELM_RELEASE) -n $(APP_NS) --timeout=60s
	@echo ""
	@echo "==> Deploy complete (real OpenCost mode). ArgoCD registration is handled by the Helm post-install hook."
	@echo "    OpenCost needs ~2 min to collect initial metrics — costs may show zero until then."
	@echo "    Port-forward ArgoCD:      kubectl port-forward svc/argocd-server 18443:443 -n $(ARGOCD_NS)"
	@echo "    Port-forward API:         kubectl port-forward svc/$(HELM_RELEASE) 18080:8080 -n $(APP_NS)"
	@echo "    Port-forward OpenCost UI: kubectl port-forward svc/opencost 9090:9090 -n $(OPENCOST_NS)"

helm-undeploy:
	@echo "Uninstalling Helm release '$(HELM_RELEASE)'..."
	helm uninstall $(HELM_RELEASE) 2>/dev/null || true
	@echo "Removing OpenCost..."
	helm uninstall opencost -n $(OPENCOST_NS) 2>/dev/null || true
	kubectl delete -f deploy/mock-opencost.yaml --ignore-not-found 2>/dev/null || true
	@echo "Removing Prometheus (only if installed by this Makefile in $(OPENCOST_NS))..."
	@if helm list -n $(OPENCOST_NS) 2>/dev/null | grep -q "^prometheus[[:space:]]"; then \
		helm uninstall prometheus -n $(OPENCOST_NS); \
	else \
		echo "  Prometheus not in $(OPENCOST_NS), leaving it untouched."; \
	fi
	@echo "Waiting for namespaces to terminate..."
	kubectl wait --for=delete namespace/$(APP_NS) --timeout=90s 2>/dev/null || true
	kubectl delete namespace $(OPENCOST_NS) --ignore-not-found 2>/dev/null || true
	@echo "Undeploy complete."

logs:
	@echo "Viewing logs..."
	@if [ -n "$(POD)" ]; then \
		kubectl logs -f $(POD) -n argocd-cost-intel; \
	else \
		kubectl logs -f deployment/argocd-cost-intelligence -n argocd-cost-intel; \
	fi

fmt:
	@echo "Formatting code..."
	go fmt ./...

lint: lint-go lint-ui lint-helm lint-prometheus lint-actions lint-manifests lint-dockerfile lint-shell lint-mock lint-notifications

lint-go:
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..." && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))
	golangci-lint run --build-tags=integration ./...

lint-ui:
	@echo "Linting the UI..."
	cd ui && npm run lint && npm run typecheck

lint-helm:
	@echo "Linting the Helm chart..."
	helm lint ./helm --set postgres.auth.password=lint

lint-prometheus:
	@echo "Checking and testing the Prometheus rules..."
	$(DOCKER_RUN) --entrypoint sh $(PROMTOOL_IMAGE) -c \
		'promtool check rules helm/files/prometheus-rules.yaml && promtool test rules test/prometheus/*_test.yaml'

lint-actions:
	@echo "Linting the GitHub workflows..."
	$(DOCKER_RUN) $(ACTIONLINT_IMAGE) -color

# CRDs (ServiceMonitor, PrometheusRule) have no schema here, so objects of unknown kinds are skipped.
lint-manifests:
	@echo "Validating the Kubernetes manifests..."
	$(DOCKER_RUN) $(KUBECONFORM_IMAGE) -strict -summary -ignore-missing-schemas deploy
	$(HELM_TEMPLATE) | docker run --rm -i $(KUBECONFORM_IMAGE) -strict -summary -ignore-missing-schemas -

lint-dockerfile:
	@echo "Linting the Dockerfiles..."
	$(DOCKER_RUN) $(HADOLINT_IMAGE) hadolint Dockerfile ui/Dockerfile

lint-shell:
	@echo "Linting the shell scripts..."
	$(DOCKER_RUN) $(SHELLCHECK_IMAGE) scripts/*.sh

lint-mock:
	@echo "Checking the embedded mock OpenCost..."
	scripts/mock-opencost-sync.sh check

lint-notifications:
	@echo "Checking the copies of the hook's Argo CD settings against the chart..."
	scripts/notifications-payload-check.sh

sync-mock:
	@echo "Regenerating deploy/mock-opencost.yaml..."
	scripts/mock-opencost-sync.sh write

# The Read the Docs site (.readthedocs.yaml), built in a virtualenv of its own.
DOCS_VENV = .cache/docs-venv

# A stamp written only after pip succeeds, so a failed install is retried.
$(DOCS_VENV)/.installed: docs/requirements.txt
	python3 -m venv $(DOCS_VENV)
	$(DOCS_VENV)/bin/pip install -q -r docs/requirements.txt
	@touch $@

docs: $(DOCS_VENV)/.installed
	$(DOCS_VENV)/bin/mkdocs build --strict

docs-serve: $(DOCS_VENV)/.installed
	$(DOCS_VENV)/bin/mkdocs serve --strict

vendor:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

dev: clean build
	@echo "Development build complete"
	@./bin/$(BINARY_NAME) &

.PHONY: gen-manifests
gen-manifests:
	@echo "Generating Kubernetes manifests..."
	@echo "Creating deploy directory..."
	mkdir -p deploy
	@echo "Manifests already in deploy/ directory"
