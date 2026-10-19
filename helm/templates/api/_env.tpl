{{- define "octo.api.env" -}}
- name: HTTP_PORT
  value: {{ .Values.api.service.port | quote }}
- name: IAM_URL
  value: {{ include "octo.iam.url" . | quote }}
- name: ORCHESTRATOR_URL
  value: {{ include "octo.orchestrator.url" . | quote }}
- name: OBSERVABILITY_URL
  value: {{ include "octo.observability.url" . | quote }}
- name: API_RESOURCE_URL
  value: {{ include "octo.api.resource" . | quote }}
{{- /* The same issuer iam and the editor are configured with: the provider an
       OAuth client is sent to register with is the one whose tokens iam will
       exchange. */}}
- name: OIDC_ISSUER
  value: {{ required "auth.oidc.issuer is required — it is the identity provider the public API sends OAuth clients to." .Values.auth.oidc.issuer | quote }}
{{- end }}
