{{/*
Helper templates for the kacho-geo sub-chart.
*/}}

{{/*
Container image reference. Prefers an immutable digest pin when
.Values.imageDigest is set: strips any trailing `:tag` from .Values.image and
appends `@sha256:...`. Otherwise returns .Values.image verbatim.
*/}}
{{- define "kacho-geo.image" -}}
{{- if .Values.imageDigest -}}
{{ regexReplaceAll ":[^:/]+$" .Values.image "" }}@{{ .Values.imageDigest }}
{{- else -}}
{{ .Values.image }}
{{- end -}}
{{- end -}}
