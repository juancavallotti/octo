{{/*
  ServiceAccount + namespaced Role + RoleBinding, all sharing one name. Both the
  orchestrator (which manages per-integration workloads) and the runtime (which
  needs Leases for leader election) are exactly this shape, differing only in
  their rules.

    include "octo-common.rbac" (dict "root" $ "component" "orchestrator" "rules" (include "octo.orchestrator.rules" .))

  Roles are namespaced deliberately: the orchestrator can only act inside the
  release namespace, so an install can never reach across a cluster.
*/}}
{{- define "octo-common.rbac" -}}
{{- $ctx := dict "root" .root "component" .component -}}
{{- $name := .name | default (include "octo-common.componentName" $ctx) -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ $name }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
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
