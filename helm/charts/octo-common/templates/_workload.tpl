{{/*
  Generic workload renderers: container, pod spec, Deployment, StatefulSet.

  Every octo component renders through these, so a field added here (resources,
  securityContext, scheduling constraints) reaches all of them at once instead of
  being copy-pasted five times. Component-specific pieces that genuinely differ —
  environment variables above all — are passed in as pre-rendered strings by the
  parent chart rather than being expressed in values.

  Pod- and container-level settings are resolved through
  octo-common.componentConfig, so `root` + `component` are enough for a caller to
  pick up everything a deployment target configured.
*/}}

{{/*
  A single container. Emitted as a list item, so callers nindent it under
  `containers:` / `initContainers:`.

  Explicit keys: name, image, pullPolicy, command, args, ports,
                 env (pre-rendered string), readinessProbe, volumeMounts
  Resolved from values (root + component): resources, securityContext,
                 livenessProbe, startupProbe, extraEnv, envFrom, extraVolumeMounts
*/}}
{{- define "octo-common.container" -}}
{{- $cfg := dict -}}
{{- if and .root .component -}}
{{- $cfg = fromYaml (include "octo-common.componentConfig" (dict "root" .root "component" .component)) -}}
{{- end -}}
{{- $volumeMounts := concat (.volumeMounts | default list) ($cfg.extraVolumeMounts | default list) -}}
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
  {{- if or .env $cfg.extraEnv }}
  env:
    {{- with .env }}
    {{- . | nindent 4 }}
    {{- end }}
    {{- with $cfg.extraEnv }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  {{- end }}
  {{- with $cfg.envFrom }}
  envFrom:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .readinessProbe }}
  readinessProbe:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $cfg.livenessProbe }}
  livenessProbe:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $cfg.startupProbe }}
  startupProbe:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $volumeMounts }}
  volumeMounts:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with (.resources | default $cfg.resources) }}
  resources:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $cfg.securityContext }}
  securityContext:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}

{{/*
  Pod spec body (no `spec:` key — callers nindent it under their own).

  Explicit keys: serviceAccountName, restartPolicy, initContainers, containers, volumes
  Resolved from values: podSecurityContext, imagePullSecrets, nodeSelector,
                 tolerations, affinity, topologySpreadConstraints,
                 priorityClassName, extraVolumes
*/}}
{{- define "octo-common.podSpec" -}}
{{- $cfg := dict -}}
{{- if and .root .component -}}
{{- $cfg = fromYaml (include "octo-common.componentConfig" (dict "root" .root "component" .component)) -}}
{{- end -}}
{{- $volumes := concat (.volumes | default list) ($cfg.extraVolumes | default list) -}}
{{- with .serviceAccountName }}
serviceAccountName: {{ . }}
{{- end }}
{{- with .restartPolicy }}
restartPolicy: {{ . }}
{{- end }}
{{- with $cfg.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $cfg.podSecurityContext }}
securityContext:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $cfg.priorityClassName }}
priorityClassName: {{ . }}
{{- end }}
{{- with $cfg.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $cfg.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $cfg.affinity }}
affinity:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $cfg.topologySpreadConstraints }}
topologySpreadConstraints:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .initContainers }}
initContainers:
  {{- . | nindent 2 }}
{{- end }}
containers:
  {{- .containers | nindent 2 }}
{{- with $volumes }}
volumes:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
  Pod template metadata shared by Deployment and StatefulSet. podAnnotations
  carries the checksum annotations that roll pods when a Secret changes.
*/}}
{{- define "octo-common.podTemplateMetadata" -}}
{{- $cfg := fromYaml (include "octo-common.componentConfig" (dict "root" .root "component" .component)) -}}
{{- $annotations := merge (deepCopy ($cfg.podAnnotations | default dict)) (.extraPodAnnotations | default dict) -}}
metadata:
  {{- with $annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  labels:
    {{- include "octo-common.componentSelectorLabels" (dict "root" .root "component" .component) | nindent 4 }}
    {{- with $cfg.podLabels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
{{- end }}

{{/*
  Deployment. Keys: root, component, replicas, plus everything
  octo-common.podSpec and octo-common.container take.
*/}}
{{- define "octo-common.deployment" -}}
{{- $ctx := dict "root" .root "component" .component -}}
{{- $cfg := fromYaml (include "octo-common.componentConfig" $ctx) -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "octo-common.componentName" $ctx }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
spec:
  replicas: {{ .replicas }}
  {{- with $cfg.revisionHistoryLimit }}
  revisionHistoryLimit: {{ . }}
  {{- end }}
  {{- with $cfg.strategy }}
  strategy:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "octo-common.componentSelectorLabels" $ctx | nindent 6 }}
  template:
    {{- include "octo-common.podTemplateMetadata" . | nindent 4 }}
    spec:
      {{- include "octo-common.podSpec" . | nindent 6 }}
{{- end }}

{{/*
  StatefulSet. Additional keys: serviceName (the governing headless Service),
  volumeClaimTemplates (pre-rendered string).
*/}}
{{- define "octo-common.statefulset" -}}
{{- $ctx := dict "root" .root "component" .component -}}
{{- $cfg := fromYaml (include "octo-common.componentConfig" $ctx) -}}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ include "octo-common.componentName" $ctx }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
spec:
  replicas: {{ .replicas | default 1 }}
  serviceName: {{ .serviceName }}
  {{- with $cfg.podManagementPolicy }}
  podManagementPolicy: {{ . }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "octo-common.componentSelectorLabels" $ctx | nindent 6 }}
  template:
    {{- include "octo-common.podTemplateMetadata" . | nindent 4 }}
    spec:
      {{- include "octo-common.podSpec" . | nindent 6 }}
  {{- with .volumeClaimTemplates }}
  volumeClaimTemplates:
    {{- . | nindent 4 }}
  {{- end }}
{{- end }}
