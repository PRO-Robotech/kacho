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

{{/*
Помощник адреса слушателя хуков ПЕРЕЕХАЛ в _kratos-identity.tpl под именем
`kacho.identity.hooksAuthority`.

Причина не косметическая: адрес входит в содержимое настроек службы личности, а
это содержимое обязано вычисляться ДВАЖДЫ — в контексте нашего подчарта (карта
настроек) и в контексте подчарта провайдера (отпечаток содержимого в шаблоне
пода, без которого правка карты не перекатывает под). Прежняя редакция читала
`.Values.kratos.config.hooks` и `.Values.service.internal.hooksHttpPort` —
значения НАШЕГО подчарта, которых во втором контексте нет.

Копии здесь не оставлено намеренно: две реализации одного адреса разошлись бы
молча, и отпечаток перестал бы соответствовать содержимому.
*/}}
