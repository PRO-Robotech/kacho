{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
ЗОНА DNS СТЕНДА — ОДНО МЕСТО РЕШЕНИЯ (NTF-1 Р19 «Профиль стенда», замысел §12а
«Зона стенда», CX1-130, CX1-135; Д104).

Объявление одно — `global.kacho.standDNS {enabled, serviceIP, clusterDomain}`.
Ключ ЧИТАЕТСЯ ТОЛЬКО ЗДЕСЬ: шаблон сервера зоны зонтика
(`umbrella/templates/stand-dns.yaml`), страж почтовой полосы зонтика (блок (6)),
помощник пода `notify.dnsConfig` и строка `KACHO_NOTIFY_STAND_DNS` зовут
помощники этого файла и ключа не читают сами (перепись D10:
`git grep -n 'standDNS' -- deploy/helm` — чтения только здесь).

Вход каждого помощника — корень вызывающего (`.`): нужны `.Values.global` и
`.Release.Name`. Подчарт notify видит только `global` (CX1-106), поэтому
объявление лежит под `global`, а не под ключом зонтика.

notify.standDNS — исход строкой, закрытый набор:
  · `off` — `enabled` не задан или `false`;
  · `<релиз>-mailpit` — `enabled: true`, `serviceIP` и `clusterDomain` непусты, и
    хост узла почты (без порта и учётной части) равен приёмнику стенда ЭТОГО
    релиза;
  · отказ рендера — во всех прочих сочетаниях, с именем ключа.
Значение — то же, что уходит процессу ручкой `KACHO_NOTIFY_STAND_DNS`: `off`
либо хост приёмника стенда (Д104).
*/}}
{{- define "notify.standDNS" -}}
{{- $sd := dig "kacho" "standDNS" (dict) (.Values.global | default dict) -}}
{{- if not (kindIs "map" $sd) -}}
{{- fail (printf "global.kacho.standDNS = %s (%s) — ожидается словарь {enabled, serviceIP, clusterDomain}" (toJson $sd) (kindOf $sd)) -}}
{{- end -}}
{{- $en := index $sd "enabled" -}}
{{- if kindIs "invalid" $en -}}
off
{{- else if not (kindIs "bool" $en) -}}
{{- fail (printf "global.kacho.standDNS.enabled = %s (%s) — ожидается булево значение true либо false без кавычек" (toJson $en) (kindOf $en)) -}}
{{- else if not $en -}}
off
{{- else -}}
{{- if not (index $sd "serviceIP") -}}
{{- fail "global.kacho.standDNS.serviceIP: зона DNS стенда включена (global.kacho.standDNS.enabled), а адрес службы сервера зоны не задан — значение берётся из файла своего кластера (deploy/stacks-mail.txt, третье поле)" -}}
{{- end -}}
{{- if not (index $sd "clusterDomain") -}}
{{- fail "global.kacho.standDNS.clusterDomain: зона DNS стенда включена (global.kacho.standDNS.enabled), а домен кластера не задан — значение берётся из файла своего кластера (deploy/stacks-mail.txt, третье поле)" -}}
{{- end -}}
{{- $want := printf "%s-mailpit" .Release.Name -}}
{{- $uri := dig "kacho" "identity" "smtp" "connectionURI" "" (.Values.global | default dict) -}}
{{- $host := "" -}}
{{- if $uri -}}
{{- $host = (splitList ":" ((urlParse (tpl $uri .)).host | default "")) | first -}}
{{- end -}}
{{- if ne $host $want -}}
{{- fail (printf "global.kacho.standDNS.enabled: зона DNS стенда включена, а узел почты %q — не приёмник стенда %q (Д104): notify сверял бы записи домена отправителя с подсаженной зоной, а письма уходили бы на другой узел" $host $want) -}}
{{- end -}}
{{- $want -}}
{{- end -}}
{{- end -}}

{{/*
notify.standDNSZone — домен зоны стенда: домен адреса отправителя установки
(узел почты, поле `fromAddress`; Д101), строчными. Адрес вне формы
`<локальная часть>@<домен>` — пусто: форму адреса судит страж старта notify
(Д45), а пустую зону отвергает шаблон сервера зоны зонтика.
*/}}
{{- define "notify.standDNSZone" -}}
{{- $from := dig "kacho" "identity" "smtp" "fromAddress" "" (.Values.global | default dict) -}}
{{- $parts := splitList "@" $from -}}
{{- if eq (len $parts) 2 -}}
{{- last $parts | lower | trimSuffix "." -}}
{{- end -}}
{{- end -}}

{{/*
notify.standDNSServer — объявление сервера зоны для шаблона зонтика, JSON
{serviceIP, clusterDomain, zone}. Зовётся только при исходе, отличном от `off`.
*/}}
{{- define "notify.standDNSServer" -}}
{{- $sd := dig "kacho" "standDNS" (dict) (.Values.global | default dict) -}}
{{- dict "serviceIP" (toString (index $sd "serviceIP")) "clusterDomain" (toString (index $sd "clusterDomain")) "zone" (include "notify.standDNSZone" .) | toJson -}}
{{- end -}}

{{/*
notify.dnsConfig — форма резолвера пода notify (CX1-130): при включённой зоне —
`dnsPolicy: None` и `dnsConfig` на сервер зоны с поиском, как у пода с
`ClusterFirst` (`<ns>.svc.<домен>`, `svc.<домен>`, `<домен>`, `ndots: 5`);
сервер зоны пересылает остальные имена DNS кластера. При `off` — пусто: под
остаётся на резолвере кластера.
*/}}
{{- define "notify.dnsConfig" -}}
{{- if ne (include "notify.standDNS" .) "off" -}}
{{- $srv := include "notify.standDNSServer" . | fromJson -}}
{{- $cd := $srv.clusterDomain -}}
dnsPolicy: None
dnsConfig:
  nameservers:
    - {{ $srv.serviceIP | quote }}
  {{- /* Домены поиска — АБСОЛЮТНОЙ формой (с точкой): адрес соседа в дереве
       пишется либо коротким `<служба>.<ns>.svc`, либо абсолютным
       (deploy/tests/helm/neighbour-address-form-test.sh), а Kubernetes и
       резолвер Go принимают домен поиска с точкой на конце как тот же домен. */}}
  searches:
    - {{ printf "%s.svc.%s." .Release.Namespace $cd | quote }}
    - {{ printf "svc.%s." $cd | quote }}
    - {{ printf "%s." $cd | quote }}
  options:
    - name: ndots
      value: "5"
{{- end -}}
{{- end -}}
