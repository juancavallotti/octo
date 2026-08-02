{{/*
  octo-specific helpers. Generic naming, labelling and image resolution live in
  the octo-common library chart (helm/charts/octo-common); what stays here is the
  wiring that is particular to this platform — which component answers on which
  in-cluster URL, and how the database DSN is assembled.

  Every resource name is {fullname}-{component}, so these are all thin wrappers
  over octo-common.componentName rather than independent string building.
*/}}

{{- define "octo.componentName" -}}
{{- include "octo-common.componentName" (dict "root" .root "component" .component) }}
{{- end }}

{{- define "octo.postgres.serviceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "postgres") }}
{{- end }}

{{- define "octo.orchestrator.serviceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "orchestrator") }}
{{- end }}

{{- define "octo.nats.serviceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "nats") }}
{{- end }}

{{- define "octo.nats.headlessServiceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "nats-headless") }}
{{- end }}

{{- define "octo.platform.serviceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "platform") }}
{{- end }}

{{- define "octo.logs.serviceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "logs") }}
{{- end }}

{{- define "octo.auth.secretName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "auth") }}
{{- end }}

{{- define "octo.kv.secretName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "kv") }}
{{- end }}

{{/*
  ServiceAccount the deployed runtime pods run as. It grants the coordination.k8s.io
  leases RBAC the runtime's k8s services module needs for leader election.
*/}}
{{- define "octo.runtime.serviceAccountName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "runtime") }}
{{- end }}

{{/*
  In-cluster URL of the orchestrator KV API, injected into runtime pods as
  ORCHESTRATOR_URL so the k8s services module can reach the deployment-scoped store.
*/}}
{{- define "octo.orchestrator.url" -}}
{{- printf "http://%s.%s:%d" (include "octo.orchestrator.serviceName" .) .Release.Namespace (int .Values.orchestrator.service.port) -}}
{{- end }}

{{/*
  In-cluster URL of the NATS broker. Injected into components as NATS_URL so they
  can publish/subscribe for cross-node pub-sub (see the pub-sub migration tracked
  in the issue tracker).
*/}}
{{- define "octo.nats.url" -}}
{{- printf "nats://%s.%s:%d" (include "octo.nats.serviceName" .) .Release.Namespace (int .Values.nats.service.port) -}}
{{- end }}

{{/*
  In-cluster URL of the log-aggregator's query API, injected into the platform as
  LOGS_URL so its /logs view can read stored log events.
*/}}
{{- define "octo.logs.url" -}}
{{- printf "http://%s.%s:%d" (include "octo.logs.serviceName" .) .Release.Namespace (int .Values.logs.service.port) -}}
{{- end }}

{{/*
  The NATS monitoring HTTP base URL (port 8222), which the platform polls for
  queue stats (/varz, /connz). Same service as octo.nats.url, http scheme + the
  monitor port.
*/}}
{{- define "octo.nats.monitorUrl" -}}
{{- printf "http://%s.%s:%d" (include "octo.nats.serviceName" .) .Release.Namespace (int .Values.nats.service.monitorPort) -}}
{{- end }}

{{/*
  Database coordinates. The chart either runs Postgres itself (postgres.enabled)
  or points at a managed instance — Cloud SQL on GKE, RDS on EKS — so every
  consumer resolves the host/port/user/database/sslmode through these rather
  than assuming the in-cluster StatefulSet.
*/}}
{{- define "octo.database.host" -}}
{{- if .Values.postgres.enabled -}}
{{- include "octo.postgres.serviceName" . -}}
{{- else -}}
{{- required "externalDatabase.host is required when postgres.enabled is false" .Values.externalDatabase.host -}}
{{- end -}}
{{- end }}

{{- define "octo.database.port" -}}
{{- if .Values.postgres.enabled -}}
{{- .Values.postgres.service.port -}}
{{- else -}}
{{- .Values.externalDatabase.port | default 5432 -}}
{{- end -}}
{{- end }}

{{- define "octo.database.user" -}}
{{- if .Values.postgres.enabled -}}
{{- .Values.postgres.auth.username -}}
{{- else -}}
{{- .Values.externalDatabase.user -}}
{{- end -}}
{{- end }}

{{- define "octo.database.name" -}}
{{- if .Values.postgres.enabled -}}
{{- .Values.postgres.auth.database -}}
{{- else -}}
{{- .Values.externalDatabase.database -}}
{{- end -}}
{{- end }}

{{- define "octo.database.sslmode" -}}
{{- if .Values.postgres.enabled -}}
{{- .Values.postgres.sslmode | default "disable" -}}
{{- else -}}
{{- .Values.externalDatabase.sslmode | default "require" -}}
{{- end -}}
{{- end }}

{{/*
  Secret and key holding the database password. In-cluster it is the Secret this
  chart generates; external it is either a Secret you already have (the managed
  route — the password is issued by the cloud provider, not by Helm) or one the
  chart creates from externalDatabase.password.
*/}}
{{- define "octo.database.secretName" -}}
{{- if .Values.postgres.enabled -}}
{{- include "octo.postgres.serviceName" . -}}
{{- else if .Values.externalDatabase.existingSecret -}}
{{- .Values.externalDatabase.existingSecret -}}
{{- else -}}
{{- include "octo-common.componentName" (dict "root" . "component" "externaldb") -}}
{{- end -}}
{{- end }}

{{- define "octo.database.passwordKey" -}}
{{- if .Values.postgres.enabled -}}
postgres-password
{{- else if .Values.externalDatabase.existingSecret -}}
{{- .Values.externalDatabase.existingSecretPasswordKey | default "password" -}}
{{- else -}}
password
{{- end -}}
{{- end }}

{{/*
  Env pair every database consumer mounts.

  The password is NEVER interpolated into the DSN at template time — that would
  put it in plaintext in the Deployment spec, readable by anyone with `get
  deployment`. Instead it lands as its own env var from a secretKeyRef, and the
  DSN references it with $(...): kubelet expands $(VAR) in an env value from
  variables declared EARLIER in the same container, including ones sourced from
  a Secret. Order matters here, which is why both vars come from one template.

  This is also what makes existingSecret work at all: a chart cannot read
  another Secret's contents, but the pod can.
*/}}
{{- define "octo.database.env" -}}
- name: OCTO_DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.database.secretName" . }}
      key: {{ include "octo.database.passwordKey" . }}
- name: DATABASE_URL
  value: "postgres://{{ include "octo.database.user" . }}:$(OCTO_DB_PASSWORD)@{{ include "octo.database.host" . }}:{{ include "octo.database.port" . }}/{{ include "octo.database.name" . }}?sslmode={{ include "octo.database.sslmode" . }}"
{{- end }}
