{{- define "octo.observability.env" -}}
- name: PORT
  value: {{ .Values.observability.service.port | quote }}
# Who this replica is, and where its lease lives. The alerting evaluator runs on
# exactly one replica — two would open two incidents and send two emails for one
# outage — and a Lease needs an identity to be held as and a namespace to be held
# in. Their absence is also how the service recognises that it is not running in a
# cluster at all, in which case it evaluates in-process without an election.
- name: POD_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
# The orchestrator, which is where an alert action sends mail from. This service
# holds no provider credentials of its own: the Resend key lives in one place,
# encrypted in site_settings, and is decrypted by the one service that owns it.
- name: ORCHESTRATOR_URL
  value: {{ include "octo.orchestrator.url" . | quote }}
{{ include "octo.database.env" . }}
{{- if .Values.nats.enabled }}
# In-cluster NATS broker carrying the two subjects this service consumes as a
# competing consumer: internal.logs, and internal.traces from pods with tracing
# enabled.
- name: NATS_URL
  value: {{ include "octo.nats.url" . | quote }}
{{- end }}
# Redis, where the fold of a streaming block's trace records is held while it is
# open. Not guarded by an `enabled` check, unlike NATS_URL above: the helper
# resolves a managed Redis first and fails the render when there is neither, so
# this variable is always set — and this service will not start without it.
{{ include "octo.redis.env" . }}
{{- with .Values.observability.prices.sources }}
# Which rate cards price traced model calls, most preferred first. Unset uses
# openrouter then helicone, so a model either card knows is priced and only one
# neither knows is not.
- name: LLM_PRICES_SOURCES
  value: {{ . | quote }}
{{- end }}
{{- with .Values.observability.prices.url }}
# Helicone's rate card. Unset uses the service's own default (Helicone's public
# catalogue); point it at a mirror for a cluster with no egress, and note that a
# cluster which can reach neither still serves whatever prices are already in
# the database.
- name: LLM_PRICES_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.observability.prices.openrouterUrl }}
# OpenRouter's published model list, on the same terms. It needs no API key.
- name: LLM_PRICES_OPENROUTER_URL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.observability.prices.refreshInterval }}
- name: LLM_PRICES_REFRESH
  value: {{ . | quote }}
{{- end }}
{{- end }}
