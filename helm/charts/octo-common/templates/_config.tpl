{{/*
  Effective per-component configuration: .Values.<component> layered over
  .Values.defaults.

  Every deployment target wants the same pod-level settings applied to all five
  workloads at once — GKE Autopilot needs resources and a restricted
  securityContext everywhere, EKS needs the same scheduling constraints
  everywhere. Without a shared block each profile would repeat itself five times,
  which is exactly how the values files drift. So `defaults` holds the common
  case and a component block overrides only what it actually differs on.

  Returns YAML; callers do:
    {{- $cfg := fromYaml (include "octo-common.componentConfig" (dict "root" $ "component" "platform")) }}
*/}}
{{- define "octo-common.componentConfig" -}}
{{- $defaults := .root.Values.defaults | default dict -}}
{{- $component := (index .root.Values .component) | default dict -}}
{{- toYaml (merge (deepCopy $component) $defaults) -}}
{{- end }}

{{/*
  Name of the ServiceAccount a component's pods run as: an explicit
  serviceAccount.name wins, otherwise the derived {fullname}-{component}. Used
  both by the ServiceAccount template and by the workloads that reference it, so
  the two can never disagree.
*/}}
{{- define "octo-common.serviceAccountName" -}}
{{- $cfg := fromYaml (include "octo-common.componentConfig" .) -}}
{{- $sa := $cfg.serviceAccount | default dict -}}
{{- $sa.name | default (include "octo-common.componentName" .) -}}
{{- end }}
