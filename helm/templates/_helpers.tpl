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

{{/*
  The routing API this release publishes external endpoints with, validated.

  A closed enumeration, checked rather than assumed, for the same reason
  ingress.tls.mode is: every template that renders routing dispatches on this
  value with no final else, so an unrecognised one — a typo, a mode from a newer
  chart — matches nothing. No Ingress renders, no HTTPRoute renders, `helm
  install` reports success, and the editor is simply unreachable with nothing
  anywhere calling that an error.
*/}}
{{- define "octo.networking.mode" -}}
{{- $mode := (.Values.networking | default dict).mode | default "ingress" -}}
{{- $valid := list "ingress" "gateway" -}}
{{- if not (has $mode $valid) -}}
{{- fail (printf "networking.mode %q is not one of %s" $mode (join ", " $valid)) -}}
{{- end -}}
{{- $mode -}}
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

{{- define "octo.redis.serviceName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "redis") }}
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

{{- define "octo.devRuns.secretName" -}}
{{- include "octo-common.componentName" (dict "root" . "component" "devruns") }}
{{- end }}

{{/*
  ServiceAccount the deployed runtime pods run as. It grants the coordination.k8s.io
  leases RBAC the runtime's k8s services module needs for leader election.
*/}}
{{- /* Through octo-common.serviceAccountName, not componentName, so an explicit
       runtime.serviceAccount.name is honoured. The derived {fullname}-runtime is
       only the fallback. Setting that name is exactly how an operator points
       runtime pods at an account already bound to a GKE Workload Identity or an
       EKS IRSA role; deriving it here regardless meant the ServiceAccount object
       took the explicit name while the pods referenced the derived one, so the
       identity silently did not apply. */ -}}
{{- define "octo.runtime.serviceAccountName" -}}
{{- include "octo-common.serviceAccountName" (dict "root" . "component" "runtime") }}
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
  REDIS_URL, as an env entry rather than a bare string.

  It is a whole entry because the value does not always come from the same place.
  The bundled server takes no credentials — it is reachable only inside the
  namespace, exactly as NATS is — so its URL is a literal and there is nothing to
  hide. A managed Redis usually needs a password, and a password belongs in a
  Secret: written as a literal here it would be readable in the rendered
  Deployment by anyone who can read workloads, which is a wider audience than
  anyone who can read Secrets.

  So externalRedis.existingSecret names a Secret holding the whole URL, and the
  variable is bound to it by reference. externalRedis.url stays for the case with
  no credential in it — an internal address, or a server that authenticates some
  other way — and the values file says which is which.

  Unlike NATS_URL this is NOT optional for its consumers, so a chart with none of
  the three fails here rather than rendering a manifest that produces a pod which
  starts and then exits. The aggregator refuses to run without it — see the note
  on REDIS_URL in logs/main.go — because the alternative is an install that
  silently stops folding trace records and grows the traces table until somebody
  notices.
*/}}
{{- define "octo.redis.env" -}}
{{- if .Values.externalRedis.existingSecret -}}
- name: REDIS_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.externalRedis.existingSecret | quote }}
      key: {{ .Values.externalRedis.existingSecretKey | default "redis-url" | quote }}
{{- else -}}
- name: REDIS_URL
  value: {{ include "octo.redis.url" . | quote }}
{{- end -}}
{{- end }}

{{/*
  The in-cluster Redis URL, or a managed one given as a plain value. See
  octo.redis.env for why a credentialled URL does not come through here.
*/}}
{{- define "octo.redis.url" -}}
{{- if .Values.externalRedis.url -}}
{{- .Values.externalRedis.url -}}
{{- else if .Values.redis.enabled -}}
{{- printf "redis://%s.%s:%d" (include "octo.redis.serviceName" .) .Release.Namespace (int .Values.redis.service.port) -}}
{{- else -}}
{{- fail "redis is required: set redis.enabled=true, or point externalRedis.url (or externalRedis.existingSecret) at a Redis the cluster can reach" -}}
{{- end -}}
{{- end }}

{{/*
  In-cluster URL of the log-aggregator's query API, injected as LOGS_URL into both
  the platform and the orchestrator. It serves two views: /platform/logs reads
  stored log events, and /platform/traces reads stored traces — traces live in the
  same service, which is why one URL covers both.

  The orchestrator does not query it itself; it binds the value onto the platform
  agent's deployment so Dr. Octo can read the same history the two views do.
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
  Secret and key holding the database password. Four cases, and both halves —
  in-cluster and external — offer the same choice: a Secret you already own, or
  one the chart creates from a value you supply. An existing Secret is the better
  answer for anything long-lived, because Helm keeps every value it is given in
  the release history, where a password outlives the database it belonged to.
*/}}
{{- define "octo.database.secretName" -}}
{{- if .Values.postgres.enabled -}}
{{- if .Values.postgres.auth.existingSecret -}}
{{- .Values.postgres.auth.existingSecret -}}
{{- else -}}
{{- include "octo.postgres.serviceName" . -}}
{{- end -}}
{{- else if .Values.externalDatabase.existingSecret -}}
{{- .Values.externalDatabase.existingSecret -}}
{{- else -}}
{{- include "octo-common.componentName" (dict "root" . "component" "externaldb") -}}
{{- end -}}
{{- end }}

{{- define "octo.database.passwordKey" -}}
{{- if .Values.postgres.enabled -}}
{{- if .Values.postgres.auth.existingSecret -}}
{{- .Values.postgres.auth.existingSecretPasswordKey | default "postgres-password" -}}
{{- else -}}
postgres-password
{{- end -}}
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
{{- /*
  The password is deliberately NOT in DATABASE_URL.

  It used to be interpolated as $(OCTO_DB_PASSWORD), which kubelet expands with
  the raw Secret value — unescaped, because kubelet does not know it is building
  a URI. A password containing @ / : ? or # then changes how the DSN parses, and
  an `@` in particular is read as the host separator, so the pod fails to connect
  with an error naming a host nobody configured. The chart cannot escape its way
  out of this either: with externalDatabase.existingSecret the value is not
  knowable at template time, which is the whole point of an existing Secret.

  So the password travels as PGPASSWORD instead. All three Go services connect
  with pgx/v5, which honours the libpq environment variables as defaults for
  anything the connection string omits — and no escaping rules apply to an
  environment variable. PGPASSWORD is kept as the ONLY source, rather than a
  fallback alongside the URI form, so there is one answer to where the password
  comes from.
*/ -}}
- name: PGPASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.database.secretName" . }}
      key: {{ include "octo.database.passwordKey" . }}
- name: DATABASE_URL
  value: "postgres://{{ include "octo.database.user" . | urlquery }}@{{ include "octo.database.host" . }}:{{ include "octo.database.port" . }}/{{ include "octo.database.name" . }}?sslmode={{ include "octo.database.sslmode" . }}"
{{- end }}
