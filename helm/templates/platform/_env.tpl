{{/*
  Environment for the editor/platform container. Env is the one part of a workload
  that is genuinely component-specific, so it stays in the parent chart and is
  handed to the octo-common workload renderer as a pre-rendered block.
*/}}
{{- define "octo.platform.env" -}}
# Image defaults; OCTO_RUN_DIR is backed by the writable emptyDir below.
- name: OCTO_BIN_PATH
  value: /usr/local/bin/octo
# The test runner the editor's Testing tab spawns. Unset would leave
# the tab able to author suites but not run them.
- name: DOLPHIN_BIN_PATH
  value: /usr/local/bin/dolphin
- name: OCTO_RUN_DIR
  value: /app/.octo-run
# Enables the integration/folder management UI; the editor's BFF proxy
# fronts the orchestrator at its in-cluster Service DNS.
- name: ORCHESTRATOR_URL
  value: "http://{{ include "octo.orchestrator.serviceName" . }}:{{ .Values.orchestrator.service.port }}"
{{- if .Values.nats.enabled }}
# In-cluster NATS broker. The BFF subscribes to deployment-status and
# integration-write subjects and serves them to the browser as SSE
# (issue #74). Omitted when NATS is off, in which case those streams
# fall back to polling / the in-process bus.
- name: NATS_URL
  value: {{ include "octo.nats.url" . | quote }}
# NATS monitoring HTTP service (port 8222); the /platform/queues view
# polls /varz + /connz from here directly. Omitted when NATS is off, in
# which case the queues view shows its unavailable state.
- name: NATS_MONITOR_URL
  value: {{ include "octo.nats.monitorUrl" . | quote }}
{{- end }}
# Log-aggregator query API; the /platform/logs view reads stored log
# events from here directly.
- name: LOGS_URL
  value: {{ include "octo.logs.url" . | quote }}
{{- if .Values.auth.oidc.enabled }}
# OIDC SSO (Auth.js). The presence of AUTH_EETR_ISSUER + AUTH_SECRET
# turns auth on in the editor. Issuer and client id are non-secret plain
# values; the client secret and session secret come from the auth Secret.
- name: AUTH_EETR_ISSUER
  value: {{ .Values.auth.oidc.issuer | quote }}
- name: AUTH_EETR_CLIENT_ID
  value: {{ .Values.auth.oidc.clientId | quote }}
- name: AUTH_EETR_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.auth.secretName" . }}
      key: oidc-client-secret
- name: AUTH_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.auth.secretName" . }}
      key: auth-secret
# Callback/cookie URL behind the ingress (auth-code redirect target).
- name: AUTH_URL
  value: "https://{{ .Values.ingress.host }}"
- name: AUTH_TRUST_HOST
  value: "true"
{{- with .Values.auth.writeRoles }}
- name: AUTH_WRITE_ROLES
  value: {{ . | quote }}
{{- end }}
{{- with .Values.auth.rolesClaim }}
- name: AUTH_ROLES_CLAIM
  value: {{ . | quote }}
{{- end }}
{{- end }}
{{- end }}
