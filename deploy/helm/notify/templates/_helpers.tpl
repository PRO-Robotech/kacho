{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
notify.fullname — имя каждого объекта чарта. Оно же — имя учётки notify и,
значит, сегмент `sa/` его SPIFFE-идентичности; имя объекта ключа сетки —
`<полное имя>-recipient-key` (CX1-68 (г)).
*/}}
{{- define "notify.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "notify.labels" -}}
app: {{ include "notify.fullname" . }}
app.kubernetes.io/name: {{ include "notify.fullname" . }}
app.kubernetes.io/component: notify
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "notify.selectorLabels" -}}
app: {{ include "notify.fullname" . }}
{{- end -}}

{{/*
notify.renders — рендерится ли notify: ровно при непустом выведенном перечне
источников (NTF1-N04). Каждый шаблон чарта открывается этим условием — второго
включателя нет (запись зависимости зонтика без `condition`, CX1-99).
*/}}
{{- define "notify.renders" -}}
{{- if gt (len (include "notify.sourceRoster" (dict "global" .Values.global) | fromJsonArray)) 0 -}}true{{- end -}}
{{- end -}}

{{/*
notify.mailNode — узел почты установки `global.kacho.identity.smtp` (Д44, Д47)
как JSON; путь один в зонтике и в самостоятельной поставке (NTF1-I06).
*/}}
{{- define "notify.mailNode" -}}
{{- dig "kacho" "identity" "smtp" (dict) .Values.global | toJson -}}
{{- end -}}

{{/*
notify.trustDomain — домен доверия SPIFFE-идентичности notify, ОДНИМ
написанием: его берёт выпуск листа (certificate.yaml). Умолчания здесь нет —
значение `mtls.trustDomain` задаёт установка; пустое — отказ рендера.
*/}}
{{- define "notify.trustDomain" -}}
{{- required "mtls.trustDomain (чарт notify): домен доверия SPIFFE-идентичности notify не задан" .Values.mtls.trustDomain -}}
{{- end -}}

{{/*
Каталоги файлов в поде: якорь узла почты и лист удостоверения notify.
*/}}
{{- define "notify.anchorDir" -}}/var/run/kacho/notify/smtp-trust-anchor{{- end -}}
{{- define "notify.peerTLSDir" -}}/var/run/kacho/notify/tls{{- end -}}
