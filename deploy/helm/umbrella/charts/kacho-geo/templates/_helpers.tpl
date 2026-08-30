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

{{/*
kacho-geo.dbSslMode — режим TLS до Postgres, ОДНИМ написанием.

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
{{- define "kacho-geo.dbSslMode" -}}
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
