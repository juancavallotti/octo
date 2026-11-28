{{- /*
  Names of internalTLS's cluster-scoped objects. Empty values derive them from
  the release and its namespace, so two releases enabling internal TLS with the
  defaults do not claim the same ClusterIssuer or Bundle. The derived name stays
  within 53 characters so "-selfsigned" still fits a 63-character name and the
  CA certificate's commonName fits its 64-character limit. Root context.
*/}}

{{- define "octo.internalTLS.defaultName" -}}
{{- printf "%s-%s-internal-ca" (include "octo-common.fullname" .) .Release.Namespace | trunc 53 | trimSuffix "-" }}
{{- end }}

{{- /* The CA ClusterIssuer that signs every internal certificate; also the CA Certificate and its Secret. */}}
{{- define "octo.internalTLS.issuerName" -}}
{{- default (include "octo.internalTLS.defaultName" .) .Values.internalTLS.issuerName }}
{{- end }}

{{- /* The trust-manager Bundle, and the ConfigMap it publishes in every namespace. */}}
{{- define "octo.internalTLS.caConfigMap" -}}
{{- default (include "octo.internalTLS.defaultName" .) .Values.internalTLS.caConfigMap }}
{{- end }}
