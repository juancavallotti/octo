{{- define "octo.logs.env" -}}
- name: PORT
  value: {{ .Values.logs.service.port | quote }}
{{ include "octo.database.env" . }}
{{- if .Values.nats.enabled }}
# In-cluster NATS broker carrying the two subjects this service consumes as a
# competing consumer: internal.logs, and internal.traces from pods with tracing
# enabled.
- name: NATS_URL
  value: {{ include "octo.nats.url" . | quote }}
{{- end }}
{{- with .Values.logs.prices.url }}
# Rate card for pricing traced model calls. Unset uses the service's own default
# (Helicone's public catalogue); point it at a mirror for a cluster with no
# egress, and note that a cluster which can reach neither still serves whatever
# prices are already in the database.
- name: LLM_PRICES_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.logs.prices.refreshInterval }}
- name: LLM_PRICES_REFRESH
  value: {{ . | quote }}
{{- end }}
{{- end }}
