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
  Postgres connection string for the orchestrator (in-cluster, sslmode disabled).
*/}}
{{- define "octo.databaseURL" -}}
{{- printf "postgres://%s:%s@%s:%d/%s?sslmode=disable" .Values.postgres.auth.username .Values.postgres.auth.password (include "octo.postgres.serviceName" .) (int .Values.postgres.service.port) .Values.postgres.auth.database -}}
{{- end }}
