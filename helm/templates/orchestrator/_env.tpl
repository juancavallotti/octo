{{- define "octo.orchestrator.env" -}}
- name: PORT
  value: {{ .Values.orchestrator.service.port | quote }}
{{ include "octo.database.env" . }}
- name: KUBE_NAMESPACE
  value: {{ .Release.Namespace | quote }}
- name: RUNTIME_IMAGE
  value: {{ include "octo-common.image" (dict "root" . "component" "runtime") | quote }}
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
  value: {{ include "octo.logs.url" . | quote }}
{{- if .Values.nats.enabled }}
# In-cluster NATS broker for cross-node pub-sub. Set so the URL is
# discoverable; consumers land with the pub-sub migration.
- name: NATS_URL
  value: {{ include "octo.nats.url" . | quote }}
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
{{- if .Values.kv.encryptionKey }}
# AES-256 key for encrypting KV secret namespaces at rest. Absent =>
# secret writes rejected, plain KV still works.
- name: KV_ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.kv.secretName" . }}
      key: kv-encryption-key
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
      key: dev-run-hash-secret
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
{{- if .Values.orchestrator.ingressAnnotations }}
- name: INGRESS_ANNOTATIONS
  value: {{ .Values.orchestrator.ingressAnnotations | toJson | quote }}
{{- end }}
{{- end }}
{{- end }}
