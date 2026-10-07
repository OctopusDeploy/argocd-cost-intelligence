{{/*
Expand the name of the chart.
*/}}
{{- define "argocd-cost-intelligence.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "argocd-cost-intelligence.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "argocd-cost-intelligence.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.AppVersion | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels. Built as a map so a key in commonLabels that the chart also sets
cannot be emitted twice; the chart's value wins, so selector labels stay intact.
*/}}
{{- define "argocd-cost-intelligence.labels" -}}
{{- $labels := include "argocd-cost-intelligence.selectorLabels" . | fromYaml }}
{{- $_ := set $labels "helm.sh/chart" (include "argocd-cost-intelligence.chart" .) }}
{{- if .Chart.AppVersion }}
{{- $_ := set $labels "app.kubernetes.io/version" .Chart.AppVersion }}
{{- end }}
{{- $_ := set $labels "app.kubernetes.io/managed-by" .Release.Service }}
{{- toYaml (merge $labels (.Values.commonLabels | default dict)) }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "argocd-cost-intelligence.selectorLabels" -}}
app.kubernetes.io/name: {{ include "argocd-cost-intelligence.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "argocd-cost-intelligence.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "argocd-cost-intelligence.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Postgres hostname
*/}}
{{- define "argocd-cost-intelligence.postgres.host" -}}
{{- printf "postgres.%s.svc.cluster.local" .Values.namespace.name }}
{{- end }}

{{/*
Database secret name — returns the existingSecret name if set, otherwise the
Helm-managed secret name "postgres-credentials".
*/}}
{{- define "argocd-cost-intelligence.postgres.secretName" -}}
{{- if .Values.postgres.auth.existingSecret -}}
{{- .Values.postgres.auth.existingSecret -}}
{{- else -}}
postgres-credentials
{{- end -}}
{{- end }}

{{/*
Webhook secret name: webhook.existingSecret if set, otherwise the Helm-managed Secret. Either
holds the token Argo CD Notifications must send to POST /on-sync under the key "token".
*/}}
{{- define "argocd-cost-intelligence.webhook.secretName" -}}
{{- .Values.webhook.existingSecret | default "argocd-cost-intelligence-webhook" -}}
{{- end }}

{{/*
Webhook token for the Helm-managed Secret: the token already stored there when Helm can read it
(helm install/upgrade), otherwise a new random one. Computed once per render and cached in
.Values, so the Secret and the Deployment's checksum annotation always agree.
*/}}
{{- define "argocd-cost-intelligence.webhook.token" -}}
{{- if not (hasKey .Values "__webhookToken") -}}
{{- $existing := lookup "v1" "Secret" .Values.namespace.name "argocd-cost-intelligence-webhook" -}}
{{- $token := randAlphaNum 32 -}}
{{- if and $existing $existing.data (index $existing.data "token") -}}
{{- $token = index $existing.data "token" | b64dec -}}
{{- end -}}
{{- $_ := set .Values "__webhookToken" $token -}}
{{- end -}}
{{- index .Values "__webhookToken" -}}
{{- end }}

{{/*
OpenCost config secret name
*/}}
{{- define "argocd-cost-intelligence.openCost.secretName" -}}
argocd-cost-intel-config
{{- end }}

{{/*
Prometheus annotations for pods serving metrics, when metrics.podAnnotations is on. Empty otherwise.
*/}}
{{- define "argocd-cost-intelligence.metrics.scrapeAnnotations" -}}
{{- if and .Values.metrics.enabled .Values.metrics.podAnnotations -}}
prometheus.io/scrape: "true"
prometheus.io/port: {{ .Values.metrics.port | quote }}
prometheus.io/path: /metrics
{{- end -}}
{{- end }}

{{/*
NetworkPolicy ingress rule admitting metrics.allowedFrom to the metrics port. Empty when no peer is
listed, so the port stays closed.
*/}}
{{- define "argocd-cost-intelligence.metrics.ingressRule" -}}
{{- with .Values.metrics.allowedFrom -}}
# Prometheus → /metrics, on its own port: never the API port, whose proxy headers it could forge
- from:
    {{- toYaml . | nindent 4 }}
  ports:
    - port: {{ $.Values.metrics.port }}
      protocol: TCP
{{- end }}
{{- end }}
