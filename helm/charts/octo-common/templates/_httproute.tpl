{{/*
  Generic HTTPRoute renderer — the Gateway API counterpart of _ingress.tpl.

    include "octo-common.httproute" (dict
      "root" $ "component" "platform" "ingress" .Values.ingress
      "gateway" .Values.networking.gateway
      "backendPort" .Values.platform.service.port)

  It is much shorter than the Ingress renderer, and the missing parts are the
  point rather than an omission. An Ingress has to carry the ingress class, the
  certificate arrangement and whatever controller-specific annotations the target
  needs, because it has no way to say "attach me to that proxy". A route says
  which hostnames go to which Service and attaches to a Gateway; the listener on
  that Gateway owns the certificate and the proxy. So there is no tls block here
  and no mode to dispatch on — see the Gateway template for where that went.

  Host and path come from the same `ingress` values as the Ingress path: those
  describe where the editor is published, which is a question the routing API
  does not change. The class, annotations and tls keys of that block are Ingress
  -only and are ignored here.
*/}}
{{- define "octo-common.httproute" -}}
{{- $root := .root -}}
{{- $ctx := dict "root" $root "component" .component -}}
{{- $ing := .ingress -}}
{{- $gw := .gateway | default dict -}}
{{- $hosts := fromYamlArray (include "octo-common.ingress.hosts" $ing) -}}
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: {{ include "octo-common.componentName" $ctx }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
spec:
  parentRefs:
    - name: {{ include "octo-common.gateway.name" (dict "root" $root "gateway" $gw) | quote }}
      {{- /* Omitted when the Gateway shares our namespace, which is what an
             absent namespace already means. A Gateway elsewhere must allow
             attachment from ours through its listener's allowedRoutes — there is
             nothing this chart can set from this side to grant that. */}}
      {{- if and $gw.namespace (ne $gw.namespace $root.Release.Namespace) }}
      namespace: {{ $gw.namespace | quote }}
      {{- end }}
      {{- /* A listener is selected by hostname when its own hostname matches, so
             naming one is usually unnecessary. It matters where a Gateway serves
             the same hostname on both an HTTP and an HTTPS listener: without a
             sectionName the route attaches to both, and the endpoint is quietly
             served in plaintext as well. */}}
      {{- with $gw.sectionName }}
      sectionName: {{ . | quote }}
      {{- end }}
  hostnames:
    {{- range $hosts }}
    - {{ . | quote }}
    {{- end }}
  rules:
    - matches:
        - path:
            {{- /* Ingress spells the prefix match "Prefix"; Gateway API spells it
                   "PathPrefix". "Exact" is the same word in both. Ingress's third
                   value, ImplementationSpecific, has no counterpart at all, so it
                   maps to the prefix match rather than rendering a value the CRD
                   rejects — the same thing most controllers do with it. */}}
            type: {{ eq ($ing.pathType | default "Prefix") "Exact" | ternary "Exact" "PathPrefix" }}
            value: {{ $ing.path | default "/" | quote }}
      backendRefs:
        - name: {{ include "octo-common.componentName" $ctx }}
          port: {{ $.backendPort }}
{{- end }}

{{/*
  Name of the Gateway routes attach to.

  An explicit name is the normal case: the Gateway is the cluster's ingress
  infrastructure and it already has a name. It is only derived when this chart
  creates the Gateway itself, which is the all-in-one install.
*/}}
{{- define "octo-common.gateway.name" -}}
{{- $gw := .gateway | default dict -}}
{{- if $gw.name -}}
{{- $gw.name -}}
{{- else if $gw.create -}}
{{- include "octo-common.componentName" (dict "root" .root "component" "gateway") -}}
{{- else -}}
{{- fail "networking.gateway.name is required when networking.mode is gateway (or set networking.gateway.create=true to have the chart create one)" -}}
{{- end -}}
{{- end }}
