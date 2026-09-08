{{/*
kacho-nlb helm helpers — standard templates for name / fullname / labels /
selectors + chart-specific helpers (image ref, DB DSN, peer-config render).
*/}}

{{/*
Expand the name of the chart.
*/}}
{{- define "kacho-nlb.name" -}}
{{- default .Chart.Name .Values.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully-qualified name — defaults to .Values.name (chart already namespaced).
*/}}
{{- define "kacho-nlb.fullname" -}}
{{- default (include "kacho-nlb.name" .) .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Chart label string ("kacho-nlb-0.2.0").
*/}}
{{- define "kacho-nlb.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Standard labels — merged onto every generated object.
*/}}
{{- define "kacho-nlb.labels" -}}
helm.sh/chart: {{ include "kacho-nlb.chart" . }}
app.kubernetes.io/name: {{ include "kacho-nlb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kacho
{{- with .Values.extraLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/*
Selector labels (matchLabels / Service.selector — must NOT include version /
chart labels, which can change between releases).
*/}}
{{- define "kacho-nlb.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kacho-nlb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app: {{ include "kacho-nlb.name" . }}
{{- end -}}

{{/*
Container image reference — uses .Values.image.tag, falls back to
.Chart.AppVersion if tag is empty (CI bumps appVersion via --set on build).
*/}}
{{- define "kacho-nlb.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Migrator image — defaults to main app image so a single image ships both
binaries (kacho-loadbalancer + kacho-migrator).
*/}}
{{- define "kacho-nlb.migratorImage" -}}
{{- if and .Values.migrator.image.repository .Values.migrator.image.tag -}}
{{- printf "%s:%s" .Values.migrator.image.repository .Values.migrator.image.tag -}}
{{- else -}}
{{- include "kacho-nlb.image" . -}}
{{- end -}}
{{- end -}}

{{/*
ServiceAccount name — honours create=false (uses pre-existing SA).
*/}}
{{- define "kacho-nlb.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "kacho-nlb.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
DB Secret name — either pre-existing (.Values.db.existingSecret) or
chart-generated (<fullname>-db).
*/}}
{{- define "kacho-nlb.dbSecretName" -}}
{{- if .Values.db.existingSecret -}}
{{- .Values.db.existingSecret -}}
{{- else -}}
{{- printf "%s-db" (include "kacho-nlb.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
DB Secret key — for existingSecret use .Values.db.existingSecretKey; for the
chart-generated one we always store the password under "password".
*/}}
{{- define "kacho-nlb.dbSecretKey" -}}
{{- if .Values.db.existingSecret -}}
{{- .Values.db.existingSecretKey -}}
{{- else -}}
password
{{- end -}}
{{- end -}}

{{/*
DB DSN template — embeds the password placeholder $(KACHO_NLB_DB_PASSWORD).
Used inside config.yaml (Postgres URL); kacho-nlb resolves the env-var via
viper when the container starts.
*/}}
{{- define "kacho-nlb.dbDSN" -}}
postgres://{{ .Values.db.user }}:$(KACHO_NLB_DB_PASSWORD)@{{ .Values.db.host }}:{{ .Values.db.port }}/{{ .Values.db.name }}?sslmode={{ include "kacho-nlb.dbSslMode" .Values.db }}&search_path={{ .Values.db.name }},public
{{- end -}}

{{/*
ConfigMap name.
*/}}
{{- define "kacho-nlb.configMapName" -}}
{{- printf "%s-config" (include "kacho-nlb.fullname" .) -}}
{{- end -}}

{{/*
kacho-nlb.dbSslMode — режим TLS до Postgres, ОДНИМ написанием.

Ключ звался в дереве двумя способами: `sslMode` у vpc/compute/iam/storage и
`sslmode` здесь. Оба означали одно, и оба лежали в одном профиле, поэтому
проверка «весь ли флот шифрован» одним `grep sslMode values.prod.yaml` молча
отвечала про половину флота и выглядела исчерпывающей. Это тот вид ошибки,
который не выдаёт себя ничем: сегодня обе половины стоят в `require`, и ответ
случайно верен.

Канон — `sslMode` (camelCase, как остальные ключи продукта). Это ПЕРЕИМЕНОВАНИЕ
ПОД ОХРАНОЙ, а не совместимость: канон объявлен умолчанием чарта, поэтому
профиль, задающий прежнее написание, даёт РАСХОЖДЕНИЕ и отказ рендера с обоими
значениями в тексте. Называть это «переходным приёмом» было бы неверно — приём
срабатывает лишь там, где канон не объявлен умолчанием.

Что механизм гарантирует на самом деле, и ради чего он здесь: молчаливого
отката к умолчанию чарта НЕ БУДЕТ. Для ключа, чьё умолчание у половины чартов —
`disable`, такой откат означал бы ОТКРЫТЫЙ ТЕКСТ до базы, то есть отказ, который
никто не заметит. Оператор вместо этого получает отказ выкатки, называющий оба
написания и канон; старое имя истекает громко и сразу, а не живёт вечной
совместимостью.

Заданы оба и различаются — ОТКАЗ РЕНДЕРА, а не выбор одного из двух: угадывать,
какое из двух написаний оператор считал действующим, значит решать за него
вопрос про шифрование. Пустое значение — тоже отказ.
*/}}
{{- define "kacho-nlb.dbSslMode" -}}
{{- $canonRaw := dig "sslMode" "" . -}}
{{- $legacyRaw := dig "sslmode" "" . -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "sslMode=%q и устаревшее sslmode=%q заданы одновременно и различаются: какое из двух написаний действует — решает оператор, а не шаблон. Оставьте одно, канон — sslMode" $canon $legacy) -}}
{{- end -}}
{{- $v := $canon -}}
{{- if eq $v "" }}{{ $v = $legacy }}{{ end -}}
{{- if eq $v "" -}}
{{- fail "sslMode не задан: канал к Postgres обязан быть объявлен явно (disable|require|verify-ca|verify-full); пустое значение молча дало бы открытый текст" -}}
{{- end -}}
{{- $v -}}
{{- end -}}

{{/*
kacho-nlb.authMode — посадка безопасности, ОДНИМ адресом.

Ручку звали в дереве шестью разными адресами и двумя именами: `authn.mode`,
`config.authn.mode`, `auth.mode`, `config.mode`, `config.authMode`, `authMode`.
Ночью первый вопрос оператора — «в какой посадке работает этот кластер», и
задать его одной командой по одному ключу было НЕЛЬЗЯ: надо знать семь адресов.
Ужесточить флот одним `--set` тоже нельзя — путей столько же, сколько сервисов.

Канон — `authMode` в корне значений сервиса (camelCase, как прочие ключи
продукта; так уже было объявлено у registry и geo).

Это ПЕРЕИМЕНОВАНИЕ ПОД ОХРАНОЙ, а не совместимость: канон объявлен умолчанием
чарта, поэтому профиль, задающий прежний адрес, даёт расхождение и отказ
рендера с обоими значениями в тексте. Гарантия здесь одна и ради неё всё:
молчаливого отката к умолчанию чарта НЕ БУДЕТ. Для ручки, задающей посадку
безопасности, такой откат означал бы стенд, который называет себя одним, а
работает другим, — и заметить это можно только по последствиям.
*/}}
{{- define "kacho-nlb.authMode" -}}
{{- $vals := .Values | toYaml | fromYaml -}}
{{- $canonRaw := .Values.authMode -}}
{{- $legacyRaw := "" -}}
{{- $cur := $vals -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "config" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $legacyRaw = dig "mode" "" $cur }}{{ end -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "authMode=%q и прежний адрес config.mode=%q заданы одновременно и различаются: какой из двух адресов задаёт посадку — решает оператор, а не шаблон. Оставьте один, канон — authMode" $canon $legacy) -}}
{{- end -}}
{{- if ne $canon "" }}{{ $canon }}{{ else }}{{ $legacy }}{{ end -}}
{{- end -}}

{{/*
kacho-nlb.trustDomain — ДОМЕН ДОВЕРИЯ установки, ОДНИМ написанием.

Домен читают ДВЕ стороны: чеканка собственного сертификата
(`certificate.yaml`) и процесс, опознающий по нему личность предъявителя. Пока
каждая брала умолчание своей рукой, расходились они МОЛЧА: сертификат
выпускался под новым доменом, принимающая сторона знала прежний, и законный
отправитель переставал опознаваться — отказом, неотличимым от вызова без
личности.

Умолчание живёт ЗДЕСЬ и только здесь. В коде его нет намеренно: непустое
умолчание сборки увело бы установку, забывшую назвать свой домен, в чужой, и
контроль выглядел бы включённым.
*/}}
{{- define "kacho-nlb.trustDomain" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $sp.trustDomain | default "kacho.cloud" -}}
{{- end -}}
