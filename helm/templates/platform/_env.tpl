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
{{- with .Values.orchestrator.baseDomain }}
# The wildcard integrations are published under. The editor serves nothing but the
# gateway error page on a hostname beneath it — see apps/platform/proxy.ts — so a
# controller whose catch-all rewrite does not take cannot publish the editor there.
- name: BASE_DOMAIN
  value: {{ . | quote }}
{{- end }}
{{- if and .Values.ingress.enabled .Values.ingress.host }}
{{- /* tls normalized through `default dict`: `and` does not short-circuit a
       lookup, so a values file that set `ingress.tls: null` would fail to render
       here rather than fall back. */}}
{{- $tls := .Values.ingress.tls | default dict }}
{{- $scheme := ternary "https" "http" (and $tls.enabled (ne (toString ($tls.mode | default "")) "none")) }}
# Where the gateway error page's way-out link points. From the chart rather than
# from the request: a link built out of a Host header would let whoever sent the
# request choose where that page sends people next.
- name: PLATFORM_URL
  value: {{ printf "%s://%s" $scheme .Values.ingress.host | quote }}
{{- /* Every hostname the editor itself answers on. The guard above treats a host
       under the wildcard as an integration host, and the editor may legitimately
       BE under it — ingress.host=octo.apps.example.com with
       baseDomain=apps.example.com is an ordinary layout. Without this list the
       editor would serve the error page to itself. */}}
- name: PLATFORM_HOSTS
  value: {{ concat (list .Values.ingress.host) (.Values.ingress.extraHosts | default list) | join "," | quote }}
{{- end }}
# Log-aggregator query API. Both the /platform/logs and /platform/traces
# views read from here directly — traces are stored by the same service,
# so one URL serves both and neither needs the orchestrator in the path.
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
# Callback/cookie origin (the auth-code redirect target). Auth.js builds every
# callback URL against it, and it must match a redirect URI registered with the
# identity provider exactly — scheme included.
{{- if .Values.auth.url }}
- name: AUTH_URL
  value: {{ .Values.auth.url | quote }}
{{- else }}
{{- /* Derived as https://{ingress.host}, which is right wherever the endpoint
       terminates TLS — every real deployment. It is a default rather than the
       only answer because the chart cannot always know the scheme: with
       networking.mode=gateway the endpoint's certificate belongs to a Gateway
       listener this chart may not own, and a local install may serve plain HTTP.
       Both are auth.url's job to state.

       required on the host, because the failure is otherwise invisible until
       someone tries to log in: with no host this renders "https://" and the
       redirect dies at the identity provider rather than here. */}}
- name: AUTH_URL
  value: "https://{{ required "ingress.host is required when auth.oidc.enabled is true — it is the OIDC callback origin. Set auth.url instead to name the origin explicitly." .Values.ingress.host }}"
{{- end }}
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
