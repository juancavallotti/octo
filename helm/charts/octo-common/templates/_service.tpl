{{/*
  Generic Service renderer.

    include "octo-common.service" (dict
      "root" $
      "component" "platform"
      "type" "ClusterIP"
      "ports" (list (dict "name" "http" "port" 3000 "targetPort" 3000)))

  Call-site keys:
    component  — sets the name ({fullname}-{component}) and the component label
    name       — overrides the derived name (the NATS headless Service needs this)
    type       — the chart's default type; .Values.<component>.service.type wins
    headless   — emit clusterIP: None (and no type; a headless Service has none)
    publishNotReadyAddresses — surface pods before Ready (StatefulSet governors)
    ports      — list of {name, port, targetPort}

  Resolved from .Values.<component>.service:
    type                  ClusterIP / NodePort / LoadBalancer
    annotations           controller hooks — GKE's cloud.google.com/neg for
                          container-native load balancing, AWS's
                          service.beta.kubernetes.io/aws-* for NLB behaviour
    nodePorts             fixed host ports keyed by port name, for local clusters
                          (k3d/kind) that map them; unset lets Kubernetes allocate
    externalTrafficPolicy
    loadBalancerSourceRanges
*/}}
{{- define "octo-common.service" -}}
{{- $root := .root -}}
{{- $component := .component -}}
{{- $name := .name | default (include "octo-common.componentName" (dict "root" $root "component" $component)) -}}
{{- $svc := ((index $root.Values $component) | default dict).service | default dict -}}
{{- $nodePorts := $svc.nodePorts | default dict -}}
{{- $type := "" -}}
{{- if not .headless -}}
{{- $type = $svc.type | default .type -}}
{{- end -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ $name }}
  labels:
    {{- include "octo-common.componentLabels" (dict "root" $root "component" $component) | nindent 4 }}
  {{- with $svc.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- with $type }}
  type: {{ . }}
  {{- end }}
  {{- if .headless }}
  clusterIP: None
  {{- end }}
  {{- if .publishNotReadyAddresses }}
  publishNotReadyAddresses: true
  {{- end }}
  {{- with $svc.externalTrafficPolicy }}
  externalTrafficPolicy: {{ . }}
  {{- end }}
  {{- with $svc.loadBalancerSourceRanges }}
  loadBalancerSourceRanges:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  selector:
    {{- include "octo-common.componentSelectorLabels" (dict "root" $root "component" $component) | nindent 4 }}
  ports:
    {{- range .ports }}
    - name: {{ .name }}
      port: {{ .port }}
      targetPort: {{ .targetPort }}
      {{- with (index $nodePorts .name) }}
      nodePort: {{ . }}
      {{- end }}
    {{- end }}
{{- end }}
