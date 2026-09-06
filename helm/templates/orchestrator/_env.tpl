{{- define "octo.orchestrator.env" -}}
- name: PORT
  value: {{ .Values.orchestrator.service.port | quote }}
{{ include "octo.database.env" . }}
- name: KUBE_NAMESPACE
  value: {{ .Release.Namespace | quote }}
- name: RUNTIME_IMAGE
  value: {{ include "octo-common.image" (dict "root" . "component" "runtime") | quote }}
{{- /* Which octo that image IS. The reference above may be digest-pinned, and a
       digest names no version — so a deployment made from it would record an
       address and nothing a person can read. Recorded per deployment at deploy
       time, which is what makes "this one is on an older runtime" answerable
       after the orchestrator has moved on. */}}
- name: RUNTIME_VERSION
  value: {{ include "octo-common.imageVersion" (dict "root" . "component" "runtime") | quote }}
{{- /* The agentic runner, for a deployment that asks for `runner: agentic`. Emitted
       with the release's image by default, since that image always ships — and NOT
       emitted at all when the repository is cleared, which is how an operator turns
       the runner off.

       Guarded on the repository rather than on the rendered reference, because
       octo-common.image renders one regardless: an empty repository yields ":tag",
       which is not the empty string the orchestrator reads as "no such runner here".
       Left ungated, clearing the repository would advertise a runner and land every
       agentic deploy in ImagePullBackOff on an image literally called ":tag", instead
       of refusing it with this value named. */}}
{{- $agentic := .Values.agenticrunner | default dict }}
{{- if or (get ($agentic.image | default dict) "repository") $agentic.repository }}
- name: AGENTIC_RUNNER_IMAGE
  value: {{ include "octo-common.image" (dict "root" . "component" "agenticrunner") | quote }}
- name: AGENTIC_RUNNER_WORKSPACE_SIZE
  value: {{ $agentic.workspaceSize | quote }}
{{- with $agentic.resources }}
{{- /* JSON, the way INGRESS_ANNOTATIONS already travels: the orchestrator unmarshals
       it straight into a container's resources block, so the values file writes the
       ordinary requests/limits shape and nothing has to be flattened into env vars. */}}
- name: AGENTIC_RUNNER_RESOURCES
  value: {{ toJson . | quote }}
{{- end }}
{{- end }}
# Runtime-services env the orchestrator injects into every deployed runtime
# pod: the backend module, the in-cluster KV URL (this orchestrator), and the
# ServiceAccount granting leases RBAC. ORCHESTRATOR_URL being set is what
# enables the injection.
- name: RUNTIME_SERVICES_MODULE
  value: {{ .Values.runtime.servicesModule | quote }}
- name: ORCHESTRATOR_URL
  value: {{ include "octo.orchestrator.url" . | quote }}
{{- /* The log aggregator's query API. The orchestrator never calls it — it binds
       this onto the platform agent's deployment, which is what lets Dr. Octo read
       stored logs and traces rather than only tailing live pods. Unconditional
       because the chart always deploys the aggregator; if that ever becomes
       optional, the agent install refuses with a message naming this variable
       rather than deploying an agent that half works. */}}
- name: LOGS_URL
  value: {{ include "octo.observability.url" . | quote }}
{{- if .Values.embeddings.enabled }}
{{- /* The embedding server. The orchestrator uses it directly — the backfill
       sweep and the query side of a semantic search both run next to the vectors
       — and it also injects this same address into every integration pod, so a
       flow can embed text without the installation handing out a provider key.
       Absent when there is no embedding server, which is what makes agent-memory
       search fall back to matching text rather than failing. */}}
- name: EMBEDDINGS_URL
  value: {{ include "octo.embeddings.url" . | quote }}
{{- end }}
{{- if .Values.nats.enabled }}
# In-cluster NATS broker for cross-node pub-sub. Set so the URL is
# discoverable; consumers land with the pub-sub migration.
- name: NATS_URL
  value: {{ include "octo.nats.url" . | quote }}
{{- end }}
# Redis, shared with the aggregator. The orchestrator uses it for the volatile KV
# tier — serving those objects to the browser, sweeping them on undeploy — and to
# report whether the cluster's Redis is reachable, which is the question an
# operator asks before anything else.
{{ include "octo.redis.env" . }}
{{- /* How integration pods should reach the same Redis. They connect directly,
       exactly as they do to NATS: the volatile tier has no database and no
       encryption key, so an orchestrator hop would buy nothing and cost a round
       trip on the one tier whose point is being cheap.

       The orchestrator builds those pods' env in Go, so it needs to know which
       shape to emit — and a credentialled URL must stay a reference. It cannot
       read that off its own REDIS_URL: by the time the variable reaches the
       process the secret has already been resolved into a plain string, and
       copying that string into every integration Deployment is precisely what
       octo.redis.env exists to avoid. So the Secret's coordinates are passed
       alongside it, and the orchestrator re-references rather than re-values. */}}
{{- if .Values.externalRedis.existingSecret }}
- name: REDIS_URL_SECRET_NAME
  value: {{ .Values.externalRedis.existingSecret | quote }}
- name: REDIS_URL_SECRET_KEY
  value: {{ .Values.externalRedis.existingSecretKey | default "redis-url" | quote }}
{{- end }}
- name: RUNTIME_SERVICE_ACCOUNT
  value: {{ include "octo-common.serviceAccountName" (dict "root" . "component" "runtime") | quote }}
{{- /* Pull secrets for the integration pods the orchestrator creates. Resolved
       through componentConfig, so defaults.imagePullSecrets covers them along
       with every chart workload and runtime.imagePullSecrets overrides just
       these — the same layering every other pod-level setting follows.
       Otherwise a mirrored-registry install brings up a healthy platform whose
       every deployed integration sits in ErrImagePull.
       Names only, comma-joined: the orchestrator builds the references. */}}
{{- $runtime := fromYaml (include "octo-common.componentConfig" (dict "root" . "component" "runtime")) }}
{{- with $runtime.imagePullSecrets }}
{{- $names := list }}
{{- range . }}
{{- $names = append $names (required "every entry in imagePullSecrets needs a name" .name) }}
{{- end }}
- name: RUNTIME_IMAGE_PULL_SECRETS
  value: {{ join "," $names | quote }}
{{- end }}
{{- if include "octo.kv.enabled" . }}
# AES-256 key for encrypting KV secret namespaces at rest. Absent =>
# secret writes rejected, plain KV still works.
- name: KV_ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.kv.secretName" . }}
      key: {{ include "octo.kv.secretKey" . }}
{{- end }}
{{- if .Values.orchestrator.baseDomain }}
- name: BASE_DOMAIN
  value: {{ .Values.orchestrator.baseDomain | quote }}
{{- end }}
{{- if .Values.orchestrator.devRuns.enabled }}
{{- /* Dev runs: the editor's Run as a pod of its own. The orchestrator reads the two
       images together with ORCHESTRATOR_URL (set above) to decide the feature is on —
       each one's absence alone would produce a pod that fails rather than a feature
       that degrades, so they are emitted as a set or not at all.

       Note which runtime image this is: devruntime, the standalone build. The
       RUNTIME_IMAGE above is the k8s build, which exits without a runtime-services
       module and has no business in an editor's run. */}}
- name: DEV_RUNTIME_IMAGE
  value: {{ include "octo-common.image" (dict "root" . "component" "devruntime") | quote }}
- name: DEV_SIDECAR_IMAGE
  value: {{ include "octo-common.image" (dict "root" . "component" "devsidecar") | quote }}
- name: DEV_RUN_SIDECAR_PORT
  value: {{ .Values.orchestrator.devRuns.sidecarPort | quote }}
- name: DEV_RUN_IDLE_TIMEOUT
  value: {{ .Values.orchestrator.devRuns.idleTimeout | quote }}
- name: DEV_RUN_HASH_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.devRuns.secretName" . }}
      key: {{ include "octo.devRuns.secretKey" . }}
{{- end }}
{{- if .Values.orchestrator.podStats.enabled }}
{{- /* The pod stats sidecar, injected into every deployed integration pod.

       The image is the off switch on the orchestrator side: unset means no
       deployment gains a container. That is why the whole block is gated rather
       than an `enabled` flag being passed through — there is one way for the
       feature to be off, and it is the same one whether the chart or an operator
       turned it off.

       The orchestrator ALSO requires a Redis before it injects anything, which
       this template does not check. Deliberately: an install can acquire a Redis
       later, and failing the render here would make `podStats.enabled` mean
       something subtly different from what the orchestrator does with it. */}}
- name: STATS_SIDECAR_IMAGE
  value: {{ include "octo-common.image" (dict "root" . "component" "statssidecar") | quote }}
- name: STATS_SIDECAR_PORT
  value: {{ .Values.orchestrator.podStats.port | quote }}
- name: STATS_SAMPLE_INTERVAL
  value: {{ .Values.orchestrator.podStats.sampleInterval | quote }}
- name: STATS_ROLLUP_INTERVAL
  value: {{ .Values.orchestrator.podStats.rollupInterval | quote }}
- name: STATS_RETENTION
  value: {{ .Values.orchestrator.podStats.retention | quote }}
{{- end }}
{{- /* Which API the orchestrator publishes per-integration endpoints with, and
       the settings that API needs. Only one set is emitted: the other is inert
       in the orchestrator anyway, and a pod environment listing an IngressClass
       on a release that creates no Ingresses is a claim about the deployment
       that isn't true. A values file mid-migration can hold both. */}}
{{- $mode := include "octo.networking.mode" . }}
- name: ENDPOINT_API
  value: {{ $mode | quote }}
{{- if eq $mode "gateway" }}
# The Gateway per-integration HTTPRoutes attach to. Certificates and controller
# configuration live on its listeners, which is why none of the Ingress settings
# below have a counterpart here.
- name: GATEWAY_NAME
  value: {{ include "octo-common.gateway.name" (dict "root" $ "gateway" .Values.networking.gateway) | quote }}
- name: GATEWAY_NAMESPACE
  value: {{ .Values.networking.gateway.namespace | default .Release.Namespace | quote }}
{{- with .Values.networking.gateway.sectionName }}
- name: GATEWAY_SECTION_NAME
  value: {{ . | quote }}
{{- end }}
{{- else }}
{{- if .Values.orchestrator.clusterIssuer }}
- name: CLUSTER_ISSUER
  value: {{ .Values.orchestrator.clusterIssuer | quote }}
{{- end }}
{{- if .Values.wildcardTLS.enabled }}
# Per-integration ingresses reference the shared wildcard Secret
# instead of issuing a per-host cert via CLUSTER_ISSUER.
- name: WILDCARD_TLS_SECRET
  value: {{ .Values.wildcardTLS.secretName | quote }}
{{- end }}
{{- if .Values.orchestrator.ingressClass }}
- name: INGRESS_CLASS
  value: {{ .Values.orchestrator.ingressClass | quote }}
{{- end }}
{{- /* Extra annotations on every per-deployment Ingress the orchestrator creates.
       errorPages.backendErrors adds two of them here rather than in a template of
       its own, because branding a 502 means telling the controller serving THAT
       deployment where to fetch its error page — there is no separate object to
       hang it on. nginx-only: Traefik expresses the same thing as an errors
       Middleware attached per router, and ALB does not express it at all. */}}
{{- $ingressAnn := deepCopy (.Values.orchestrator.ingressAnnotations | default dict) }}
{{- /* The resolved controller, not the class name — same reason catchall.yaml
       uses it: a class named "internal-nginx" with errorPages.controller=nginx
       would otherwise render the catch-all and silently skip these, leaving the
       502/503/504 responses unbranded. */}}
{{- if and .Values.errorPages.enabled .Values.errorPages.backendErrors
          (eq (include "octo.errorPages.controller" .) "nginx") }}
{{- $ingressAnn = set $ingressAnn "nginx.ingress.kubernetes.io/custom-http-errors" "502,503,504" }}
{{- $ingressAnn = set $ingressAnn "nginx.ingress.kubernetes.io/default-backend" (include "octo.platform.serviceName" .) }}
{{- end }}
{{- with $ingressAnn }}
- name: INGRESS_ANNOTATIONS
  value: {{ . | toJson | quote }}
{{- end }}
{{- end }}
{{- end }}
