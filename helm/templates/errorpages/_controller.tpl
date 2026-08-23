{{/*
  Which ingress controller serves this install, for the catch-all's annotations.

  Deliberately NOT orchestrator.ingressClass. That is an IngressClass *name*, and a
  name is free-form: "traefik-public" and "nginx-internal" are ordinary, and both
  would fail a match against the controller names below. Getting this wrong is not
  cosmetic — the Traefik branch is what pins the catch-all's priority to the
  bottom, and without it a wildcard host becomes a long HostRegexp that OUTRANKS
  every exact Host and swallows every published endpoint.

  So the class is used only for spec.ingressClassName, and the controller is a
  value of its own: errorPages.controller, which falls back to reading the common
  class names when it is not set. When neither yields an answer the chart refuses
  to render rather than guess — see the fail in catchall.yaml.

  Returns "nginx", "traefik", "alb", or "".
*/}}
{{- define "octo.errorPages.controller" -}}
{{- $known := list "nginx" "traefik" "alb" -}}
{{- $explicit := .Values.errorPages.controller | default "" -}}
{{- if $explicit -}}
{{- /* An explicit value that is not one of the three is refused, and this one IS
       worth failing over — unlike an unrecognised IngressClass name, which is an
       ordinary thing to have and merely means the catch-all is skipped. Here
       somebody typed a controller name and got it wrong, and quietly rendering an
       unpinned wildcard would be the worst of the three outcomes. */}}
{{- if not (has $explicit $known) -}}
{{- fail (printf "errorPages.controller is %q; expected one of nginx, traefik, alb (or leave it empty to read the controller off orchestrator.ingressClass)." $explicit) -}}
{{- end -}}
{{- $explicit -}}
{{- else -}}
{{- /* Matched a segment at a time, not as a substring.

       A substring test reads "notraefik" as Traefik and would hang Traefik's
       annotations on a controller that is not Traefik — the same unpinned-wildcard
       hazard this helper exists to prevent, arrived at from the other direction.
       Exact equality is no good either: "traefik-public" and "internal-nginx" are
       ordinary names and are exactly the case the fallback is for.

       So the class is split on the separators a Kubernetes name may contain and
       each segment is compared whole. "traefik-public" matches, "notraefik" does
       not, and anything genuinely unusual falls through to errorPages.controller. */}}
{{- $class := lower (.Values.orchestrator.ingressClass | default "") -}}
{{- $segments := splitList "-" (replace "." "-" $class) -}}
{{- if has "nginx" $segments -}}
nginx
{{- else if has "traefik" $segments -}}
traefik
{{- else if has "alb" $segments -}}
alb
{{- end -}}
{{- end -}}
{{- end }}
