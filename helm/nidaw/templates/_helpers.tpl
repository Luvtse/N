{{/*
============================================================================
NIDAW Helm Chart - Template Helpers
============================================================================
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "nidaw.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nidaw.fullname" -}}
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
{{- define "nidaw.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nidaw.labels" -}}
helm.sh/chart: {{ include "nidaw.chart" . }}
{{ include "nidaw.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.global.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nidaw.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nidaw.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Backend labels
*/}}
{{- define "nidaw.backend.labels" -}}
{{ include "nidaw.labels" . }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Backend selector labels
*/}}
{{- define "nidaw.backend.selectorLabels" -}}
{{ include "nidaw.selectorLabels" . }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
Frontend labels
*/}}
{{- define "nidaw.frontend.labels" -}}
{{ include "nidaw.labels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Frontend selector labels
*/}}
{{- define "nidaw.frontend.selectorLabels" -}}
{{ include "nidaw.selectorLabels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nidaw.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "nidaw.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Backend service name
*/}}
{{- define "nidaw.backend.fullname" -}}
{{- printf "%s-backend" (include "nidaw.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Frontend service name
*/}}
{{- define "nidaw.frontend.fullname" -}}
{{- printf "%s-frontend" (include "nidaw.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create image pull secret name
*/}}
{{- define "nidaw.imagePullSecret" -}}
{{- if .Values.global.imagePullSecrets }}
{{- printf "%s" (first .Values.global.imagePullSecrets).name }}
{{- end }}
{{- end }}

{{/*
Generate environment variables from map
*/}}
{{- define "nidaw.envVars" -}}
{{- range $key, $value := . }}
- name: {{ $key }}
  value: {{ $value | quote }}
{{- end }}
{{- end }}

{{/*
Generate secret environment variables
*/}}
{{- define "nidaw.secretEnvVars" -}}
{{- $secretName := .secretName }}
{{- range .keys }}
- name: {{ .name }}
  valueFrom:
    secretKeyRef:
      name: {{ $secretName }}
      key: {{ .key }}
{{- end }}
{{- end }}

{{/*
Backend URL helper
*/}}
{{- define "nidaw.backendUrl" -}}
{{- if .Values.ingress.enabled }}
{{- printf "https://%s" (first .Values.ingress.hosts).host }}
{{- else }}
{{- printf "http://%s:%v" (include "nidaw.backend.fullname" .) .Values.backend.service.port }}
{{- end }}
{{- end }}

{{/*
WebSocket URL helper
*/}}
{{- define "nidaw.wsUrl" -}}
{{- if .Values.ingress.enabled }}
{{- printf "wss://%s/ws" (first .Values.ingress.hosts).host }}
{{- else }}
{{- printf "ws://%s:%v/ws" (include "nidaw.backend.fullname" .) .Values.backend.service.port }}
{{- end }}
{{- end }}

{{/*
Checksum annotation for configmap
*/}}
{{- define "nidaw.configMapChecksum" -}}
checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
{{- end }}

{{/*
Checksum annotation for secret
*/}}
{{- define "nidaw.secretChecksum" -}}
checksum/secret: {{ include (print $.Template.BasePath "/secret.yaml") . | sha256sum }}
{{- end }}

{{/*
Resource limits helper
*/}}
{{- define "nidaw.resources" -}}
{{- if . }}
resources:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
Affinity helper for high availability
*/}}
{{- define "nidaw.antiAffinity" -}}
{{- $component := .component }}
{{- $fullname := .fullname }}
podAntiAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      podAffinityTerm:
        labelSelector:
          matchLabels:
            app.kubernetes.io/name: {{ $fullname }}
            app.kubernetes.io/component: {{ $component }}
        topologyKey: kubernetes.io/hostname
    - weight: 50
      podAffinityTerm:
        labelSelector:
          matchLabels:
            app.kubernetes.io/name: {{ $fullname }}
            app.kubernetes.io/component: {{ $component }}
        topologyKey: topology.kubernetes.io/zone
{{- end }}

{{/*
Topology spread constraints
*/}}
{{- define "nidaw.topologySpread" -}}
{{- $component := .component }}
{{- $fullname := .fullname }}
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: {{ $fullname }}
        app.kubernetes.io/component: {{ $component }}
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: {{ $fullname }}
        app.kubernetes.io/component: {{ $component }}
{{- end }}