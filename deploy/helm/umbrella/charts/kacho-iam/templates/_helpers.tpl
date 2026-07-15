{{/*
Helper templates для kacho-iam sub-chart.
*/}}

{{/* Полное имя релиза kacho-iam — по соглашению просто .Values.name. */}}
{{- define "kacho-iam.fullname" -}}
{{- default "kacho-iam" .Values.name -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "kacho-iam.labels" -}}
app: {{ include "kacho-iam.fullname" . }}
app.kubernetes.io/name: {{ include "kacho-iam.fullname" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Selector labels (без managed-by/instance — иначе подменится при reload). */}}
{{- define "kacho-iam.selectorLabels" -}}
app: {{ include "kacho-iam.fullname" . }}
{{- end -}}

{{/*
Container image reference. Prefers an immutable digest pin (repository@sha256:...)
when .Values.image.digest is set; otherwise falls back to repository:tag.
*/}}
{{- define "kacho-iam.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}
{{- end -}}
