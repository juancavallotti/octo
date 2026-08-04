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
