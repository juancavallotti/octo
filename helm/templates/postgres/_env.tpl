{{- define "octo.postgres.env" -}}
- name: POSTGRES_USER
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.postgres.serviceName" . }}
      key: postgres-username
{{- /* Not this component's own Secret unconditionally: with
       postgres.auth.existingSecret the password lives in a Secret the caller
       owns, and initdb must read the same one every consumer connects with. */}}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.database.secretName" . }}
      key: {{ include "octo.database.passwordKey" . }}
- name: POSTGRES_DB
  valueFrom:
    secretKeyRef:
      name: {{ include "octo.postgres.serviceName" . }}
      key: postgres-database
# Subdir of the mount avoids initdb complaints if the volume root has
# other entries (e.g. lost+found).
- name: PGDATA
  value: /var/lib/postgresql/data/pgdata
{{- end }}

{{/*
  volumeClaimTemplates carry stable labels only — they are immutable once the
  StatefulSet exists, so the version-bearing octo-common.labels must not appear
  here or an upgrade to a new chart version would try (and fail) to change them.
*/}}
{{- define "octo.postgres.volumeClaimTemplates" -}}
- metadata:
    name: data
    labels:
      {{- include "octo-common.componentSelectorLabels" (dict "root" . "component" "postgres") | nindent 6 }}
  spec:
    accessModes:
      - ReadWriteOnce
    {{- if .Values.postgres.storage.hostPath }}
    # Static binding to the PersistentVolume in pv.yaml. Explicitly empty, not
    # omitted: an omitted class means "use the default StorageClass", which would
    # hand the claim straight back to the dynamic provisioner.
    storageClassName: ""
    {{- else if .Values.postgres.storage.storageClassName }}
    storageClassName: {{ .Values.postgres.storage.storageClassName | quote }}
    {{- end }}
    resources:
      requests:
        storage: {{ .Values.postgres.storage.size | quote }}
{{- end }}
