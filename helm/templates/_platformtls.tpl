{{- /*
  HTTPS beside plain HTTP for the platform APIs (orchestrator, iam,
  observability), rendered only with internalTLS.enabled. Each gets a
  Certificate from the internal CA for its Service names, mounted at
  /var/run/octo/tls, and the env that makes it open the HTTPS listener. Plain
  HTTP stays on its own port for runtimes that predate HTTPS platform URLs.
*/}}

{{- define "octo.platformTLS.httpsPort" -}}8443{{- end }}

{{- define "octo.platformTLS.secretName" -}}
{{- printf "%s-tls" (include "octo-common.componentName" (dict "root" .root "component" .component)) }}
{{- end }}

{{- /* The Certificate for one component's Service. */}}
{{- define "octo.platformTLS.certificate" -}}
{{- $svc := include "octo-common.componentName" (dict "root" .root "component" .component) -}}
{{- $ns := .root.Release.Namespace -}}
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ include "octo.platformTLS.secretName" . }}
  labels:
    {{- include "octo-common.componentLabels" (dict "root" .root "component" .component) | nindent 4 }}
spec:
  secretName: {{ include "octo.platformTLS.secretName" . }}
  dnsNames:
    - {{ $svc }}
    - {{ printf "%s.%s" $svc $ns }}
    - {{ printf "%s.%s.svc" $svc $ns }}
    - {{ printf "%s.%s.svc.cluster.local" $svc $ns }}
  privateKey:
    algorithm: ECDSA
    size: 256
  usages:
    - server auth
  issuerRef:
    name: {{ .root.Values.internalTLS.issuerName | quote }}
    kind: ClusterIssuer
    group: cert-manager.io
{{- end }}

{{- /* Env list entries, or nothing. Root context. */}}
{{- define "octo.platformTLS.env" -}}
{{- if .Values.internalTLS.enabled }}
- name: HTTPS_PORT
  value: {{ include "octo.platformTLS.httpsPort" . | quote }}
- name: OCTO_TLS_CERT_FILE
  value: /var/run/octo/tls/tls.crt
- name: OCTO_TLS_KEY_FILE
  value: /var/run/octo/tls/tls.key
{{- end }}
{{- end }}

{{- /* YAML arrays, "[]" when off; callers read them with fromYamlArray. */}}
{{- define "octo.platformTLS.volumes" -}}
{{- if .root.Values.internalTLS.enabled -}}
- name: platform-tls
  secret:
    secretName: {{ include "octo.platformTLS.secretName" . }}
    defaultMode: 0444
{{- else -}}
[]
{{- end }}
{{- end }}

{{- define "octo.platformTLS.volumeMounts" -}}
{{- if .Values.internalTLS.enabled -}}
- name: platform-tls
  mountPath: /var/run/octo/tls
  readOnly: true
{{- else -}}
[]
{{- end }}
{{- end }}

{{- define "octo.platformTLS.containerPorts" -}}
{{- if .Values.internalTLS.enabled -}}
- name: https
  containerPort: {{ include "octo.platformTLS.httpsPort" . }}
{{- else -}}
[]
{{- end }}
{{- end }}

{{- define "octo.platformTLS.servicePorts" -}}
{{- if .Values.internalTLS.enabled -}}
- name: https
  port: 443
  targetPort: {{ include "octo.platformTLS.httpsPort" . }}
{{- else -}}
[]
{{- end }}
{{- end }}
