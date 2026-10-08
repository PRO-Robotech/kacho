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
{{- if gt (len (include "kacho.notifications.sources" (dict "global" .Values.global) | fromJsonArray)) 0 -}}true{{- end -}}
{{- end -}}

{{/*
notify.image — образ рабочей нагрузки notify, ОДНО написание на все её
контейнеры (migrate и notify). Тега нет — отказ рендера с именем ручки, а не
`latest` и не образ без тега (решение Д76 (2)): пока перечень источников пуст,
объектов notify нет и пустой пин профиля законен; notify, включённый без тега,
fail-closed. Тег ставится, когда образ опубликован конвейером линии.
*/}}
{{- define "notify.image" -}}
{{- printf "%s:%s" .Values.image.repository (required "notify.image.tag (значение image.tag чарта notify): тег образа не задан, а notify рендерится (перечень источников непуст) — задайте тег образа, опубликованного конвейером" .Values.image.tag) -}}
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

{{/*
notify.dkimNode — объект ключа DKIM установки, узел
`global.kacho.identity.smtp.dkim` `{secretName, privateKeyKey, selectorKey}`
(замысел §8, §12а «Ключ и селектор DKIM»), JSON. Объекта чарт НЕ рендерит: в
боевой посадке его создаёт оператор, на стенде — посев
`deploy/scripts/seed-stand-dkim.sh`. Пустое поле при рендерящемся notify —
отказ рендера с полным именем ключа узла.
*/}}
{{- define "notify.dkimNode" -}}
{{- $d := dig "kacho" "identity" "smtp" "dkim" (dict) (.Values.global | default dict) -}}
{{- $secret := required "global.kacho.identity.smtp.dkim.secretName: объект ключа DKIM установки не назван, а notify рендерится (перечень источников непуст) — в боевой посадке его создаёт оператор, на стенде посев seed-stand-dkim.sh" (index $d "secretName") -}}
{{- $key := required "global.kacho.identity.smtp.dkim.privateKeyKey: ключ объекта DKIM с закрытым ключом не назван" (index $d "privateKeyKey") -}}
{{- $sel := required "global.kacho.identity.smtp.dkim.selectorKey: ключ объекта DKIM с селектором не назван" (index $d "selectorKey") -}}
{{- dict "secretName" $secret "privateKeyKey" $key "selectorKey" $sel | toJson -}}
{{- end -}}

{{/*
Каталог тома объекта ключа DKIM — без `subPath`: обновление объекта kubelet
доставляет новым поколением `..data`, и на этом стоит смена ключа (§12а).
*/}}
{{- define "notify.dkimDir" -}}/etc/kacho/notify/dkim{{- end -}}

{{/*
notify.addressKeySecretName — имя объекта ключа отпечатка адреса, который
монтирует под: объект стенда (`addressKeySecret.name`, Д123) либо объект чарта с
постоянным именем `<полное имя>-address-key` (NTF-4 Д23).
*/}}
{{- define "notify.addressKeySecretName" -}}
{{- (.Values.addressKeySecret | default dict).name | default (printf "%s-address-key" (include "notify.fullname" .)) -}}
{{- end -}}

{{/*
notify.addressKeyDir — каталог монтирования объекта ключа отпечатка адреса
(NTF-4 Р15, Д23). Значение ручки KACHO_NOTIFY_ADDRESS_KEY_DIR — этот же
помощник: путь ручки и путь тома не расходятся по построению.
*/}}
{{- define "notify.addressKeyDir" -}}/etc/kacho/notify/address-key{{- end -}}

{{/*
notify.recipientKeySecretName — имя объекта ключа сетки, на которое ссылается
переменная пода: объект стенда (`recipientKeySecret.name`, Д123) либо объект
чарта с постоянным именем `<полное имя>-recipient-key` (CX1-68 (г)).
*/}}
{{- define "notify.recipientKeySecretName" -}}
{{- (.Values.recipientKeySecret | default dict).name | default (printf "%s-recipient-key" (include "notify.fullname" .)) -}}
{{- end -}}
