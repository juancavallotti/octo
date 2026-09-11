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
{{- if include "octo.kv.enabled" . }}
{{- /*
  The same AES key the orchestrator encrypts KV secret namespaces and provider
  credentials with, and the same one deliberately: this platform has one key for
  everything it keeps encrypted in Postgres, so there is one thing to hold, one to
  rotate, and one place to look when something will not decrypt.

  Here it seals the private half of every signing key. Absent, this service cannot
  store one at all and token signing stays off — unlike the orchestrator, which
  degrades by rejecting secret writes and carrying on. There is no equivalent
  half-measure for a keyset: writing private keys in the clear because a setting
  was missing is not a degraded mode, it is the failure the encryption exists to
  prevent.
*/}}
- name: KV_ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.kv.secretName" . }}
      key: {{ include "octo.kv.secretKey" . }}
{{- end }}
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
{{- /*
  The other audience this install answers to. A person signing in to the editor
  arrives with a token minted for the client id above; an MCP client arrives with
  one minted for the /mcp resource identifier. Both are this install, and the
  exchange has to accept both or MCP callers cannot get a platform token at all.

  Rendered by the same helper the platform is given, because the two values must
  match exactly: this is an audience check, and a mismatch is either a refused
  sign-in or — far worse, were it widened carelessly — an accepted token that was
  minted for somebody else's application.
*/}}
- name: IAM_ACCEPTED_AUDIENCES
  value: {{ include "octo.mcp.resource" . | quote }}
{{- end }}
{{- end }}
