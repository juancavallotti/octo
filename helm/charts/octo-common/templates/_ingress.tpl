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
{{- if not $tls.enabled -}}
none
{{- else -}}
{{- $tls.mode | default (ternary "cert-manager" "secret" (not (empty $tls.clusterIssuer))) -}}
{{- end -}}
{{- end }}

{{/*
  Every host the Ingress serves: the primary host plus any extraHosts.
*/}}
{{- define "octo-common.ingress.hosts" -}}
{{- toYaml (prepend (.extraHosts | default list) .host) -}}
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
