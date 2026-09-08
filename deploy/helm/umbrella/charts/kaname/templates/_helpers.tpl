{{/*
Helper templates для kaname sub-chart.
*/}}

{{/* Полное имя релиза kaname — по соглашению просто .Values.name. */}}
{{- define "kaname.fullname" -}}
{{- default "kaname" .Values.name -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "kaname.labels" -}}
app: {{ include "kaname.fullname" . }}
app.kubernetes.io/name: {{ include "kaname.fullname" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Selector labels (без managed-by/instance — иначе подменится при reload). */}}
{{- define "kaname.selectorLabels" -}}
app: {{ include "kaname.fullname" . }}
{{- end -}}

{{/*
Container image reference. Prefers an immutable digest pin (repository@sha256:...)
when .Values.image.digest is set; otherwise falls back to repository:tag.
*/}}
{{- define "kaname.image" -}}
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

{{/*
kaname.laneServiceAud — адресат, которому докерная полоса выдачи чеканит `aud`.

ВЫВОДИТСЯ ИЗ ОБЪЯВЛЕННОЙ СТОРОНЫ РЕЕСТРА, а не объявляется вторым умолчанием.
Реестр называет это имя докер-клиенту в WWW-Authenticate (`KACHO_REGISTRY_SERVICE_AUD`),
клиент возвращает его в `?service=`, а наш подписант сверяет с объявленным и
отвергает всё прочее. Пока сверки не было, расхождение двух объявлений было
НЕВИДИМО — клиент echo-ит услышанное, реестр это и ждёт; со сверкой то же
расхождение означает отказ во входе на каждом запросе арендатора.

Источник один — `global.kacho.registry.serviceAud`; его же читает подчарт
registry (`registry.laneServiceAud`). `global` выбран потому, что это
единственное, что видно из ОБОИХ контекстов сабчартов, — тот же довод, что у
формирующих значений службы личности.

СОБСТВЕННАЯ РУЧКА `config.apiServer.registryToken.service` ОСТАЁТСЯ и означает
«чарт поставлен сам по себе». Объявить обе и по-разному — отказ рендера с
обеими величинами, а не тихий выбор одной: тихий выбор и есть класс, из-за
которого задача заведена.

ПУСТО ЗАКОННО и молчит здесь намеренно: слушателя полосы может не быть вовсе.
Если он есть, а имени нет — отказывает СТРАЖ СТАРТА процесса, который про
слушателя знает, а шаблон не знает.
*/}}
{{- define "kaname.laneServiceAud" -}}
{{- $source := (((.Values.global).kacho).registry).serviceAud | default "" -}}
{{- $own := (((.Values.config).apiServer).registryToken).service | default "" -}}
{{- if and $source $own (ne $source $own) -}}
{{- fail (printf "kaname: адресат докерной полосы объявлен ДВАЖДЫ и по-разному — global.kacho.registry.serviceAud=%q против kaname.config.apiServer.registryToken.service=%q. У полосы две стороны (реестр называет имя докер-клиенту, iam чеканит его в aud), и объявление у них одно: оставьте global.kacho.registry.serviceAud, а config.apiServer.registryToken.service снимите — он для одиночной установки чарта, где второй стороны нет." $source $own) -}}
{{- end -}}
{{- $source | default $own -}}
{{- end -}}

{{/*
kaname.authMode — посадка безопасности, ОДНИМ адресом.

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
{{- define "kaname.authMode" -}}
{{- $vals := .Values | toYaml | fromYaml -}}
{{- $canonRaw := .Values.authMode -}}
{{- $legacyRaw := "" -}}
{{- $cur := $vals -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "config" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "authn" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $legacyRaw = dig "mode" "" $cur }}{{ end -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "authMode=%q и прежний адрес config.authn.mode=%q заданы одновременно и различаются: какой из двух адресов задаёт посадку — решает оператор, а не шаблон. Оставьте один, канон — authMode" $canon $legacy) -}}
{{- end -}}
{{- if ne $canon "" }}{{ $canon }}{{ else }}{{ $legacy }}{{ end -}}
{{- end -}}

{{/*
kaname.trustDomain — ДОМЕН ДОВЕРИЯ установки, ОДНИМ написанием.

Домен читают ДВЕ стороны: чеканка сертификата (`certificate.yaml`) и процесс,
которому сертификаты предъявляют (`authn.trust-domain` в его файле настроек).
Пока каждая брала умолчание своей рукой, расходились они МОЛЧА: сертификат
выпускался под новым доменом, принимающая сторона знала прежний, и законный
отправитель переставал опознаваться — отказом, неотличимым от вызова без
личности.

Умолчание живёт ЗДЕСЬ и только здесь. В коде его нет намеренно: непустое
умолчание сборки увело бы установку, забывшую назвать свой домен, в чужой, и
контроль выглядел бы включённым.
*/}}
{{- define "kaname.trustDomain" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $sp.trustDomain | default "kacho.cloud" -}}
{{- end -}}
