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

Канон — `sslMode` (camelCase, как остальные ключи продукта). Прежнее написание
принимается ПЕРЕХОДНО: смена имени ключа ломает того, кто уже развернул, а
молчаливый откат к умолчанию чарта означал бы здесь ОТКРЫТЫЙ ТЕКСТ до базы —
отказ, который никто не заметит.

Заданы оба и различаются — ОТКАЗ РЕНДЕРА, а не выбор одного из двух: угадывать,
какое из двух написаний оператор считал действующим, значит решать за него
вопрос про шифрование. Пустое значение — тоже отказ: оно молча дало бы
открытый текст.
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
