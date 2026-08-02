{{/*
  Opaque Secret from a pre-rendered stringData block.

    include "octo-common.secret" (dict
      "root" $ "component" "auth" "name" (include "octo.auth.secretName" .)
      "stringData" (include "octo.auth.secretData" .))

  Callers own the keys because each Secret's contents are component-specific;
  what is shared is the naming, labelling and type.
*/}}
{{- define "octo-common.secret" -}}
{{- $ctx := dict "root" .root "component" .component -}}
apiVersion: v1
kind: Secret
metadata:
  name: {{ .name | default (include "octo-common.componentName" $ctx) }}
  labels:
    {{- include "octo-common.componentLabels" $ctx | nindent 4 }}
type: Opaque
stringData:
  {{- .stringData | nindent 2 }}
{{- end }}
