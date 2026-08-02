{{- define "octo.logs.env" -}}
- name: PORT
  value: {{ .Values.logs.service.port | quote }}
- name: DATABASE_URL
  value: {{ include "octo.databaseURL" . | quote }}
{{- if .Values.nats.enabled }}
# In-cluster NATS broker carrying the internal.logs subject this service
# consumes as a competing consumer.
- name: NATS_URL
  value: {{ include "octo.nats.url" . | quote }}
{{- end }}
{{- end }}
