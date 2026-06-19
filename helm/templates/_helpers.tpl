{{- define "octo.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "octo.fullname" -}}
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

{{- define "octo.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "octo.labels" -}}
helm.sh/chart: {{ include "octo.chart" . }}
{{ include "octo.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "octo.selectorLabels" -}}
app.kubernetes.io/name: {{ include "octo.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "octo.postgres.serviceName" -}}
{{ include "octo.fullname" . }}-postgres
{{- end }}

{{- define "octo.orchestrator.serviceName" -}}
{{ include "octo.fullname" . }}-orchestrator
{{- end }}

{{- define "octo.editor.serviceName" -}}
{{ include "octo.fullname" . }}-editor
{{- end }}

{{/*
  Build a fully-qualified image reference. Call with a dict carrying the chart
  root and the component repository, e.g.:
    include "octo.image" (dict "root" $ "repo" .Values.editor.repository)
  When .Values.image.registry is set it is prefixed; otherwise the repo is used
  bare (local :dev images). Tag falls back to the shared .Values.image.tag.
*/}}
{{- define "octo.image" -}}
{{- $reg := .root.Values.image.registry | default "" | trimSuffix "/" -}}
{{- $tag := .tag | default .root.Values.image.tag -}}
{{- if $reg -}}
{{- printf "%s/%s:%s" $reg .repo $tag -}}
{{- else -}}
{{- printf "%s:%s" .repo $tag -}}
{{- end -}}
{{- end }}

{{/*
  Postgres connection string for the orchestrator (in-cluster, sslmode disabled).
*/}}
{{- define "octo.databaseURL" -}}
{{- printf "postgres://%s:%s@%s:%d/%s?sslmode=disable" .Values.postgres.auth.username .Values.postgres.auth.password (include "octo.postgres.serviceName" .) (int .Values.postgres.service.port) .Values.postgres.auth.database -}}
{{- end }}
