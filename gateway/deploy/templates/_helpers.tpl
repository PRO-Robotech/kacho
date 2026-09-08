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
