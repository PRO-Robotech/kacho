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
Authority (узел:порт) слушателя хуков iam — для обратных вызовов службы личности.

СХЕМА СЮДА НЕ ВХОДИТ НАМЕРЕННО. Транспорт — решение ПРОФИЛЯ, и оно обязано стоять
у каждого места вызова, где его видно в обзоре: `.Values.kratos.config.hooks.scheme`
плюс этот помощник. Схема, спрятанная внутрь помощника, перестала бы читаться там,
где принимается решение; схема, выписанная константой, не переопределялась бы
профилем ВОВСЕ — и боевая посадка не смогла бы объявить полосу защищённой.
Держит это deploy/identity_callback_transport_test.go.

Узел и порт тоже берутся из значений; их умолчания выводятся из имени релиза и из
порта слушателя, поэтому профиль, ничего не объявивший, получает прежний адрес
байт в байт.
*/}}
{{- define "kacho-iam.hooksAuthority" -}}
{{- $h := (.Values.kratos.config.hooks | default dict) -}}
{{- $host := ($h.host | default (printf "%s-internal.%s.svc" (include "kacho-iam.fullname" .) .Release.Namespace)) -}}
{{- $port := ($h.port | default (.Values.service.internal.hooksHttpPort | default 9092)) -}}
{{- printf "%s:%v" $host $port -}}
{{- end -}}
