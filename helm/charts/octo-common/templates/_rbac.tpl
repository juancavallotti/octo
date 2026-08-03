{{/*
  ServiceAccount + namespaced Role + RoleBinding, all sharing one name. Both the
  orchestrator (which manages per-integration workloads) and the runtime (which
  needs Leases for leader election) are exactly this shape, differing only in
  their rules.

    include "octo-common.rbac" (dict "root" $ "component" "orchestrator" "rules" (include "octo.orchestrator.rules" .))

  serviceAccount.create=false lets a cluster supply its own account — the usual
  reason being an identity binding the chart has no business owning, such as a
  GKE Workload Identity or EKS IRSA account created by Terraform. rbac.create
  =false does the same for the Role/RoleBinding pair.

  Roles are namespaced deliberately: the orchestrator can only act inside the
  release namespace, so an install can never reach across a cluster.
*/}}
{{- define "octo-common.rbac" -}}
{{- $ctx := dict "root" .root "component" .component -}}
{{- $cfg := fromYaml (include "octo-common.componentConfig" $ctx) -}}
{{- $sa := $cfg.serviceAccount | default dict -}}
{{- $rbac := $cfg.rbac | default dict -}}
{{- $name := include "octo-common.serviceAccountName" $ctx -}}
{{- if ne (toString $sa.create) "false" -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ $name }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
  {{- with $sa.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- with $sa.automountServiceAccountToken }}
automountServiceAccountToken: {{ . }}
{{- end }}
{{- end }}
{{- if ne (toString $rbac.create) "false" }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ $name }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
rules:
  {{- .rules | nindent 2 }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ $name }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
subjects:
  - kind: ServiceAccount
    name: {{ $name }}
    namespace: {{ .root.Release.Namespace }}
roleRef:
  kind: Role
  name: {{ $name }}
  apiGroup: rbac.authorization.k8s.io
{{- end }}
{{- end }}
