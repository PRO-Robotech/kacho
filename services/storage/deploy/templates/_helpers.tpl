{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
storage.fullname — the base resource name for every object in this chart.

By default it is "<Release.Name>-storage" (e.g. kacho-umbrella-storage), but a
`fullnameOverride` pins it to a stable value regardless of the release name. The
umbrella sets fullnameOverride=kacho-storage so the Service/Deployment/DNS name is
`kacho-storage` — the name every consumer dials (compute→storage
storageInternalAddr default kacho-storage.kacho.svc:9091, the
compute server-cert serverName.storage SAN, the geo/iam config defaults) and the
name deploy/Makefile reload-svc expects (DEPLOY_NAME=kacho-storage). Without the
override the Service renders as kacho-umbrella-storage and none of those resolve.
*/}}
{{- define "storage.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-storage" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
storage.labels — common labels for every object.
*/}}
{{- define "storage.labels" -}}
app: {{ include "storage.fullname" . }}
app.kubernetes.io/name: {{ include "storage.fullname" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
kacho-storage.authMode — посадка безопасности, ОДНИМ адресом.

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
{{- define "kacho-storage.authMode" -}}
{{- $vals := .Values | toYaml | fromYaml -}}
{{- $canonRaw := .Values.authMode -}}
{{- $legacyRaw := "" -}}
{{- $cur := $vals -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "config" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $legacyRaw = dig "authMode" "" $cur }}{{ end -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "authMode=%q и прежний адрес config.authMode=%q заданы одновременно и различаются: какой из двух адресов задаёт посадку — решает оператор, а не шаблон. Оставьте один, канон — authMode" $canon $legacy) -}}
{{- end -}}
{{- if ne $canon "" }}{{ $canon }}{{ else }}{{ $legacy }}{{ end -}}
{{- end -}}
