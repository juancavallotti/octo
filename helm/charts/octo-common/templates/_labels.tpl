{{/*
  Label helpers.

  octo-common.labels        — full metadata labels (chart, name, instance, version, managed-by)
  octo-common.selectorLabels — the stable subset safe to use in a selector

  Both take the chart root context. The component-scoped variants take a dict and
  append app.kubernetes.io/component, which is what every workload actually uses;
  they exist so the component line can never drift from the base set.
*/}}

{{- define "octo-common.labels" -}}
helm.sh/chart: {{ include "octo-common.chart" . }}
{{ include "octo-common.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "octo-common.selectorLabels" -}}
app.kubernetes.io/name: {{ include "octo-common.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
  Usage: {{ include "octo-common.componentLabels" (dict "root" $ "component" "platform") }}
*/}}
{{- define "octo-common.componentLabels" -}}
{{ include "octo-common.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "octo-common.componentSelectorLabels" -}}
{{ include "octo-common.selectorLabels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- end }}
