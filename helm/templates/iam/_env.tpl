{{- define "octo.iam.env" -}}
- name: PORT
  value: {{ .Values.iam.service.port | quote }}
{{ include "octo.database.env" . }}
{{- /*
  The issuer this service stamps into every token it mints, and advertises at
  /.well-known/openid-configuration. Its own in-cluster address, because that is
  where a verifier following the discovery document has to land: a service that
  claimed to be reachable somewhere it is not would publish a document pointing at
  nothing.
*/}}
- name: IAM_ISSUER
  value: {{ include "octo.iam.url" . | quote }}
{{- with .Values.iam.audience }}
- name: IAM_AUDIENCE
  value: {{ . | quote }}
{{- end }}
{{- with .Values.iam.tokenTtl }}
- name: IAM_TOKEN_TTL
  value: {{ . | quote }}
{{- end }}
{{- with .Values.iam.keyLifetime }}
- name: IAM_KEY_LIFETIME
  value: {{ . | quote }}
{{- end }}
{{- if .Values.auth.oidc.enabled }}
{{- /*
  The identity provider whose sign-in tokens this service exchanges. The SAME two
  values the editor is configured with, and read from the same place on purpose:
  one install has one identity provider, and a second pair of settings would be a
  way for the two halves to end up pointed at different ones.

  No client secret. This service verifies tokens somebody else's authorization
  flow already issued; it never runs that flow, so it needs no credential at the
  provider and is not given one.
*/}}
- name: OIDC_ISSUER
  value: {{ .Values.auth.oidc.issuer | quote }}
- name: OIDC_CLIENT_ID
  value: {{ .Values.auth.oidc.clientId | quote }}
{{- end }}
{{- end }}
