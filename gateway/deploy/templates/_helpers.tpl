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
