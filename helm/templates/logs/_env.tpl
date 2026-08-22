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
{{- with .Values.logs.prices.sources }}
# Which rate cards price traced model calls, most preferred first. Unset uses
# openrouter then helicone, so a model either card knows is priced and only one
# neither knows is not.
- name: LLM_PRICES_SOURCES
  value: {{ . | quote }}
{{- end }}
{{- with .Values.logs.prices.url }}
# Helicone's rate card. Unset uses the service's own default (Helicone's public
# catalogue); point it at a mirror for a cluster with no egress, and note that a
# cluster which can reach neither still serves whatever prices are already in
# the database.
- name: LLM_PRICES_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.logs.prices.openrouterUrl }}
# OpenRouter's published model list, on the same terms. It needs no API key.
- name: LLM_PRICES_OPENROUTER_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.logs.prices.refreshInterval }}
- name: LLM_PRICES_REFRESH
  value: {{ . | quote }}
{{- end }}
{{- end }}
