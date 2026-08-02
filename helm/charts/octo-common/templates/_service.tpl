{{/*
  Generic Service renderer.

    include "octo-common.service" (dict
      "root" $
      "component" "platform"
      "type" "ClusterIP"
      "ports" (list (dict "name" "http" "port" 3000 "targetPort" 3000)))

  Keys:
    component  — sets the name ({fullname}-{component}) and the component label
    name       — overrides the derived name (the NATS headless Service needs this)
    type       — emitted only when set, so a headless Service can omit it
    headless   — emit clusterIP: None
    publishNotReadyAddresses — surface pods before Ready (StatefulSet governors)
    ports      — list of {name, port, targetPort}
*/}}
{{- define "octo-common.service" -}}
{{- $root := .root -}}
{{- $component := .component -}}
{{- $name := .name | default (include "octo-common.componentName" (dict "root" $root "component" $component)) -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ $name }}
  labels:
    {{- include "octo-common.componentLabels" (dict "root" $root "component" $component) | nindent 4 }}
spec:
  {{- with .type }}
  type: {{ . }}
  {{- end }}
  {{- if .headless }}
  clusterIP: None
  {{- end }}
  {{- if .publishNotReadyAddresses }}
  publishNotReadyAddresses: true
  {{- end }}
  selector:
    {{- include "octo-common.componentSelectorLabels" (dict "root" $root "component" $component) | nindent 4 }}
  ports:
    {{- range .ports }}
    - name: {{ .name }}
      port: {{ .port }}
      targetPort: {{ .targetPort }}
    {{- end }}
{{- end }}
