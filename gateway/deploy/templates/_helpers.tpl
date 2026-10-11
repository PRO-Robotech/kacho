{{/*
api-gateway.authMode — посадка безопасности, ОДНИМ адресом.

Канон — `authMode` в корне значений; прежний адрес `authn.mode` принимается,
пока канон не задан. Оба заданы и различаются — отказ рендера: угадывать за
оператора, какой из двух адресов задаёт посадку, шаблон не вправе. Подробный
разбор — в одноимённом помощнике любого сервисного чарта.
*/}}
{{- define "api-gateway.authMode" -}}
{{- $vals := .Values | toYaml | fromYaml -}}
{{- $canonRaw := .Values.authMode -}}
{{- $legacyRaw := "" -}}
{{- $cur := $vals -}}
{{- if kindIs "map" $cur }}{{ $cur = dig "authn" (dict) $cur }}{{ else }}{{ $cur = dict }}{{ end -}}
{{- if kindIs "map" $cur }}{{ $legacyRaw = dig "mode" "" $cur }}{{ end -}}
{{- $canon := "" -}}{{- if $canonRaw }}{{ $canon = printf "%v" $canonRaw }}{{ end -}}
{{- $legacy := "" -}}{{- if $legacyRaw }}{{ $legacy = printf "%v" $legacyRaw }}{{ end -}}
{{- if and (ne $canon "") (ne $legacy "") (ne $canon $legacy) -}}
{{- fail (printf "authMode=%q и прежний адрес authn.mode=%q заданы одновременно и различаются: какой из двух адресов задаёт посадку — решает оператор, а не шаблон. Оставьте один, канон — authMode" $canon $legacy) -}}
{{- end -}}
{{- if ne $canon "" }}{{ $canon }}{{ else }}{{ $legacy }}{{ end -}}
{{- end -}}

{{/*
api-gateway.trustDomain — ДОМЕН ДОВЕРИЯ установки, ОДНИМ написанием.

Домен читают ДВЕ стороны: чеканка собственного сертификата края
(`certificate.yaml`) и сам край, опознающий по нему личность вызывающего,
пришедшего с проверенным клиентским сертификатом. Пока каждая брала умолчание
своей рукой, расходились они МОЛЧА: сертификат выпускался под новым доменом,
край признавал прежний, и законный вызывающий тихо проваливался на полосу
Bearer'а.

Умолчание живёт ЗДЕСЬ и только здесь. В коде его нет намеренно: непустое
умолчание сборки увело бы установку, забывшую назвать свой домен, в чужой, и
контроль выглядел бы включённым.
*/}}
{{- define "api-gateway.trustDomain" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $sp.trustDomain | default "kacho.cloud" -}}
{{- end -}}

{{/*
api-gateway.selectorLabels — МЕТКИ ПОДА КРАЯ, одним определением.

Их читают три стороны: селектор и шаблон пода собственного рабочего объекта,
селектор Service — и политики сети умбреллы, впускающие край к службам
(`templates/networkpolicy-*.yaml`). Пока политика писала метку края по памяти,
а под нёс другую, правило не пропускало НИКОГО, и край не доставал до службы
при зелёных рендере, установке и готовности (kacho#2941).

Умбрелла зовёт помощник со значениями ЭТОГО чарта (`umbrella.edgeSelectorLabels`),
поэтому читать здесь можно только `.Values`. Что каждое правило рендера
выбирает под и впускает того, кто звонит, держит
deploy/network_policy_admission_render_test.go.
*/}}
{{- define "api-gateway.selectorLabels" -}}
app: {{ required "api-gateway.name обязателен: из него выводится метка пода края" .Values.name }}
{{- end -}}

{{/*
api-gateway.frontLinkSAN — ИМЯ ЗВЕНА ФРОНТА В ЕГО ЛИСТЕ (kacho#3028, круг 5).

Одно правило на две стороны: имя, которое край принимает
(`KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_SANS`, выводится из `clientAddress.trustedPeers`),
и имя, которое умбрелла пишет в лист звена (templates/api-gateway-front-link-pki.yaml).
Аргумент — имя безголовой службы звена; результат — имя DNS в домене звеньев.
Две копии правила разошлись бы на той, которую забыли поправить, и край перестал
бы узнавать собственное звено — молча, адресом звена на всех клиентах.
*/}}
{{- define "api-gateway.frontLinkSAN" -}}
{{- printf "%s.front-link.kacho.internal" . -}}
{{- end -}}

{{/*
api-gateway.frontLinkSANs — имена звеньев через запятую по перечню служб звеньев.
*/}}
{{- define "api-gateway.frontLinkSANs" -}}
{{- $out := list -}}
{{- range $p := . -}}
{{- $out = append $out (include "api-gateway.frontLinkSAN" $p) -}}
{{- end -}}
{{- join "," $out -}}
{{- end -}}

{{/*
api-gateway.internalRestEnabled — поднимает ли чарт внутренний REST-слушатель
(kacho#3131). Слушатель отдаёт Internal* REST, и его транспорт — mTLS и только
он: материал (серверный лист, УЦ установки, клиентский лист оператора) есть лишь
под mtls.enable. Без mtls.enable слушатель НЕ объявляется вовсе (адрес пуст,
порта нет) — открытого текста ни в одном профиле. Читают deployment.yaml,
service.yaml и internal-rest-certificate.yaml — одним определением.
*/}}
{{- define "api-gateway.internalRestEnabled" -}}
{{- if and .Values.internalRestPort (.Values.mtls | default dict).enable -}}true{{- end -}}
{{- end -}}

{{/*
api-gateway.internalRestOperatorSAN — URI-имя операторской личности внутреннего
слушателя. Тот же домен доверия, что у листа края (api-gateway.trustDomain),
своё имя (internalRest.operatorSaName). Лист под этим именем выпускает
умбрелла (templates/bootstrap-operator-certificate.yaml); совпадение двух
написаний держит проба рендера gateway/deploy/internal_rest_mtls_render_test.go.
*/}}
{{- define "api-gateway.internalRestOperatorSAN" -}}
{{- $sp := (.Values.mtls | default dict).spiffe | default dict -}}
{{- $ir := .Values.internalRest | default dict -}}
{{- printf "spiffe://%s/ns/%s/sa/%s" (include "api-gateway.trustDomain" .) ($sp.namespace | default .Release.Namespace) ($ir.operatorSaName | default "kacho-internal-rest-operator") -}}
{{- end -}}

{{/*
api-gateway.internalRestClientSANs — круг клиентов внутреннего слушателя
(KACHO_API_GATEWAY_INTERNAL_REST_CLIENT_SANS): имя оператора и дополнительные
имена internalRest.extraClientSANs, через запятую.
*/}}
{{- define "api-gateway.internalRestClientSANs" -}}
{{- $ir := .Values.internalRest | default dict -}}
{{- $sans := list (include "api-gateway.internalRestOperatorSAN" .) -}}
{{- range $s := ($ir.extraClientSANs | default list) }}{{ $sans = append $sans $s }}{{ end -}}
{{- join "," $sans -}}
{{- end -}}
