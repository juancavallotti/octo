{{- /*
The embedding server's environment. Four variables, and the whole of its
configuration.

Deliberately not a settings page. The model cannot be changed after anything has
been embedded — vectors carry no record of which model produced them, so a store
holding two models' cannot be ranked coherently — and a control that must never
be touched is not a setting. It is a chart value, changed the way a chart value
is changed, with the re-embedding that implies.
*/ -}}
{{- define "octo.embeddings.env" -}}
- name: HTTP_PORT
  value: {{ .Values.embeddings.service.port | quote }}
- name: HTTP_HOST
  value: "0.0.0.0"
- name: EMBEDDING_CONNECTOR_TYPE
  value: {{ .Values.embeddings.connectorType | quote }}
- name: EMBEDDING_MODEL
  value: {{ .Values.embeddings.model | quote }}
{{- /*
The stored vector width, and it must match the `vector(N)` column in
sql/schema.sql. Requested of the provider rather than assumed of it: most
embeddings APIs take a desired-dimensions parameter. A model that cannot honour
it fails the call and the failure is reported, which is a better outcome than a
guard here listing which model can do what.
*/}}
- name: EMBEDDING_DIMENSIONS
  value: {{ .Values.embeddings.dimensions | quote }}
{{- if .Values.embeddings.existingSecret }}
- name: EMBEDDING_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.embeddings.existingSecret | quote }}
      key: {{ .Values.embeddings.existingSecretKey | quote }}
{{- else }}
- name: EMBEDDING_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "octo-common.componentName" (dict "root" $ "component" "embeddings") | quote }}
      key: apiKey
{{- end }}
{{- end }}
