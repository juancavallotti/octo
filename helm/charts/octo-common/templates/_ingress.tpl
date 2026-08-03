{{/*
  Generic Ingress renderer.

    include "octo-common.ingress" (dict
      "root" $ "component" "platform" "ingress" .Values.ingress
      "backendPort" .Values.platform.service.port)

  The interesting part is how the certificate is obtained, because that is the
  single biggest difference between deployment targets. tls.mode selects:

    cert-manager      annotate with a ClusterIssuer; cert-manager issues per-host
                      (HTTP-01). The k3s bootstrap's own arrangement.
    secret            reference a TLS Secret that already exists — a pre-issued
                      wildcard, or one Terraform/ExternalSecrets put there.
    gke-managed-cert  Google-managed certificate; the cert lives in a
                      ManagedCertificate CRD, so the Ingress carries no tls block.
    acm               AWS ACM certificate by ARN on an ALB; likewise no tls block,
                      the ALB terminates.
    none              no TLS here — something upstream terminates it.

  Empty mode auto-selects to preserve the historical behaviour: cert-manager
  when a clusterIssuer is set, otherwise secret. tls.enabled=false means none.
*/}}
{{- define "octo-common.ingress.tlsMode" -}}
{{- $tls := .tls | default dict -}}
{{- $mode := "" -}}
{{- if not $tls.enabled -}}
{{- $mode = "none" -}}
{{- else -}}
{{- $mode = ($tls.mode | default (ternary "cert-manager" "secret" (not (empty $tls.clusterIssuer)))) -}}
{{- end -}}
{{- /*
  A closed enumeration, checked rather than assumed. The caller below dispatches
  on this with no final else, so an unrecognised value — a typo, a mode from a
  newer chart — silently matches nothing: the Ingress renders with no certificate
  annotation and no tls block, `helm install` reports success, and the endpoint
  serves plain HTTP. Failing the render is the difference between a mistake you
  fix in ten seconds and one you discover from a browser warning.
*/ -}}
{{- $valid := list "cert-manager" "secret" "gke-managed-cert" "acm" "none" -}}
{{- if not (has $mode $valid) -}}
{{- fail (printf "ingress.tls.mode %q is not one of %s" $mode (join ", " $valid)) -}}
{{- end -}}
{{- $mode -}}
{{- end }}

{{/*
  Every host the Ingress serves: the primary host plus any extraHosts.
*/}}
{{- define "octo-common.ingress.hosts" -}}
{{- /* required here rather than in values, because this is the one place that
       knows an Ingress is actually being rendered: ingress.host has no default
       (a published chart must not claim a domain), and an Ingress with an empty
       host silently matches every request the controller cannot place. */ -}}
{{- toYaml (prepend (.extraHosts | default list) (required "ingress.host is required when ingress.enabled is true" .host)) -}}
{{- end }}

{{- define "octo-common.ingress" -}}
{{- $root := .root -}}
{{- $ctx := dict "root" $root "component" .component -}}
{{- $ing := .ingress -}}
{{- $tls := $ing.tls | default dict -}}
{{- $mode := include "octo-common.ingress.tlsMode" (dict "tls" $tls) -}}
{{- $hosts := fromYamlArray (include "octo-common.ingress.hosts" $ing) -}}
{{- $ann := deepCopy ($ing.annotations | default dict) -}}
{{- if eq $mode "cert-manager" -}}
{{- $_ := set $ann "cert-manager.io/cluster-issuer" $tls.clusterIssuer -}}
{{- else if eq $mode "acm" -}}
{{- $_ := set $ann "alb.ingress.kubernetes.io/certificate-arn" $tls.certificateArn -}}
{{- else if eq $mode "gke-managed-cert" -}}
{{- $_ := set $ann "networking.gke.io/managed-certificates" (.managedCertificateName | default (include "octo-common.componentName" $ctx)) -}}
{{- end -}}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "octo-common.componentName" $ctx }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
  {{- with $ann }}
  annotations:
    {{- range $k, $v := . }}
    {{ $k }}: {{ $v | quote }}
    {{- end }}
  {{- end }}
spec:
  {{- with $ing.className }}
  ingressClassName: {{ . | quote }}
  {{- end }}
  {{- if or (eq $mode "cert-manager") (eq $mode "secret") }}
  tls:
    - hosts:
        {{- range $hosts }}
        - {{ . | quote }}
        {{- end }}
      secretName: {{ $tls.secretName | quote }}
  {{- end }}
  rules:
    {{- range $hosts }}
    - host: {{ . | quote }}
      http:
        paths:
          - path: {{ $ing.path | default "/" }}
            pathType: {{ $ing.pathType | default "Prefix" }}
            backend:
              service:
                name: {{ include "octo-common.componentName" $ctx }}
                port:
                  number: {{ $.backendPort }}
    {{- end }}
{{- end }}
