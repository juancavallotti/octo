{{/*
  Build a fully-qualified image reference.

    include "octo-common.image" (dict "root" $ "repo" .Values.platform.repository)
    include "octo-common.image" (dict "root" $ "repo" "postgres" "tag" "16-alpine")

  When .Values.image.registry is set it is prefixed; otherwise the repo is used
  bare, which is what local clusters want (images side-loaded with
  `k3d image import` have no registry host). Tag falls back to the shared
  .Values.image.tag.
*/}}
{{- define "octo-common.image" -}}
{{- $reg := .root.Values.image.registry | default "" | trimSuffix "/" -}}
{{- $tag := .tag | default .root.Values.image.tag -}}
{{- if $reg -}}
{{- printf "%s/%s:%s" $reg .repo $tag -}}
{{- else -}}
{{- printf "%s:%s" .repo $tag -}}
{{- end -}}
{{- end }}
