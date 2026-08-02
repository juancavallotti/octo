{{/*
  Generic workload renderers: container, pod spec, Deployment, StatefulSet.

  Every octo component renders through these, so a field added here (resources,
  securityContext, scheduling constraints) reaches all of them at once instead of
  being copy-pasted five times. Component-specific pieces that genuinely differ —
  environment variables above all — are passed in as pre-rendered strings by the
  parent chart rather than being expressed in values.
*/}}

{{/*
  A single container. Emitted as a list item, so callers nindent it under
  `containers:` / `initContainers:`.

  Keys: name, image, pullPolicy, command, args, ports, env (pre-rendered string),
        readinessProbe, volumeMounts, resources
*/}}
{{- define "octo-common.container" -}}
- name: {{ .name }}
  image: {{ .image | quote }}
  {{- with .pullPolicy }}
  imagePullPolicy: {{ . }}
  {{- end }}
  {{- with .command }}
  command:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .args }}
  args:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .ports }}
  ports:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .env }}
  env:
    {{- . | nindent 4 }}
  {{- end }}
  {{- with .readinessProbe }}
  readinessProbe:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .volumeMounts }}
  volumeMounts:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .resources }}
  resources:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}

{{/*
  Pod spec body (no `spec:` key — callers nindent it under their own).

  Keys: serviceAccountName, restartPolicy, initContainers (pre-rendered string),
        containers (pre-rendered string), volumes
*/}}
{{- define "octo-common.podSpec" -}}
{{- with .serviceAccountName }}
serviceAccountName: {{ . }}
{{- end }}
{{- with .restartPolicy }}
restartPolicy: {{ . }}
{{- end }}
{{- with .initContainers }}
initContainers:
  {{- . | nindent 2 }}
{{- end }}
containers:
  {{- .containers | nindent 2 }}
{{- with .volumes }}
volumes:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
  Deployment. Keys: root, component, replicas, plus everything octo-common.podSpec takes.
*/}}
{{- define "octo-common.deployment" -}}
{{- $ctx := dict "root" .root "component" .component -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "octo-common.componentName" $ctx }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
spec:
  replicas: {{ .replicas }}
  selector:
    matchLabels:
      {{- include "octo-common.componentSelectorLabels" $ctx | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "octo-common.componentSelectorLabels" $ctx | nindent 8 }}
    spec:
      {{- include "octo-common.podSpec" . | nindent 6 }}
{{- end }}

{{/*
  StatefulSet. Additional keys: serviceName (the governing headless Service),
  volumeClaimTemplates (pre-rendered string).
*/}}
{{- define "octo-common.statefulset" -}}
{{- $ctx := dict "root" .root "component" .component -}}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ include "octo-common.componentName" $ctx }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
spec:
  replicas: {{ .replicas | default 1 }}
  serviceName: {{ .serviceName }}
  selector:
    matchLabels:
      {{- include "octo-common.componentSelectorLabels" $ctx | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "octo-common.componentSelectorLabels" $ctx | nindent 8 }}
    spec:
      {{- include "octo-common.podSpec" . | nindent 6 }}
  {{- with .volumeClaimTemplates }}
  volumeClaimTemplates:
    {{- . | nindent 4 }}
  {{- end }}
{{- end }}
