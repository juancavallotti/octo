{{/*
  Build a fully-qualified image reference for a component.

    include "octo-common.image" (dict "root" $ "component" "platform")
    include "octo-common.image" (dict "root" $ "component" "postgres" "thirdParty" true)

  Resolution, most specific first:
    repository  <component>.image.repository, else <component>.repository
    registry    <component>.image.registry, else the shared image.registry
                (skipped entirely for thirdParty images -- see below)
    digest      <component>.image.digest wins over any tag, giving repo@sha256:...
    tag         <component>.image.tag, else image.tag, else .Chart.AppVersion

  The appVersion fallback is what lets a chart pulled straight from OCI install
  the images of its own release rather than drifting onto whatever :latest
  happens to be.

  thirdParty marks images that are NOT published under this project's registry
  (postgres, nats). They must not inherit image.registry -- that host holds the
  octo-* images only -- so they take their registry solely from their own
  image.registry, empty meaning Docker Hub. Setting it points them at a mirror
  for clusters that can't reach Docker Hub.
*/}}
{{- define "octo-common.imageConfig" -}}
{{- $component := (index .root.Values .component) | default dict -}}
{{- toYaml ($component.image | default dict) -}}
{{- end }}

{{- define "octo-common.image" -}}
{{- $global := .root.Values.image | default dict -}}
{{- $component := (index .root.Values .component) | default dict -}}
{{- $img := $component.image | default dict -}}
{{- $repo := $img.repository | default $component.repository -}}
{{- $registry := $img.registry -}}
{{- if and (not $registry) (not .thirdParty) -}}
{{- $registry = $global.registry -}}
{{- end -}}
{{- $registry = $registry | default "" | trimSuffix "/" -}}
{{- $ref := $repo -}}
{{- if $registry -}}
{{- $ref = printf "%s/%s" $registry $repo -}}
{{- end -}}
{{- if $img.digest -}}
{{- printf "%s@%s" $ref $img.digest -}}
{{- else -}}
{{- printf "%s:%s" $ref ($img.tag | default $global.tag | default .root.Chart.AppVersion) -}}
{{- end -}}
{{- end }}

{{/*
  Pull policy for a component: its own image.pullPolicy, else the shared one.
*/}}
{{- define "octo-common.imagePullPolicy" -}}
{{- $component := (index .root.Values .component) | default dict -}}
{{- $img := $component.image | default dict -}}
{{- $img.pullPolicy | default (.root.Values.image | default dict).pullPolicy -}}
{{- end }}
