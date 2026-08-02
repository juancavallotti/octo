{{/*
  Naming helpers. These take the chart root context (`.`) directly, matching the
  standard Helm scaffold, so `include "octo-common.fullname" .` works unchanged
  from the parent chart's templates.
*/}}

{{- define "octo-common.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "octo-common.fullname" -}}
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

{{- define "octo-common.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
  Name of a component's resources: {fullname}-{component}. Every component's
  Deployment/StatefulSet/Service/Secret shares this one name, so callers that
  need to reference another component (in-cluster URLs, secretKeyRefs) resolve it
  the same way rather than re-deriving the suffix.

  Usage: {{ include "octo-common.componentName" (dict "root" $ "component" "platform") }}
*/}}
{{- define "octo-common.componentName" -}}
{{- printf "%s-%s" (include "octo-common.fullname" .root) .component | trunc 63 | trimSuffix "-" }}
{{- end }}
