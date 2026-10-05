{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
ТАБЛИЦА МОДУЛЕЙ И ВЫВЕДЕННЫЙ ПЕРЕЧЕНЬ ИСТОЧНИКОВ NOTIFY — РУЧНОГО ПЕРЕЧНЯ НЕТ
(NTF-1 Р6, Р9; замысел З28; NTF1-N02…N04; CX1-83, CX1-106, CX1-111, CX1-113).

kacho.notifications.moduleTable — ЗАКРЫТАЯ таблица модулей, JSON-массив строк:
  key        — ключ модуля под `global.kacho.notifications.modules`;
  ownFlagKey — путь СОБСТВЕННОГО ключа флага модуля в значениях его подчарта
               (от корня значений зонтика). Из имени модуля он не выводится: у
               kaname он `kaname.config.notifications.enabled`, у пробы —
               `notifyProbe.notifications.enabled`. Ключ этого пути, заданный в
               любом слое, — отказ рендера с полным путём (страж зонтика
               templates/notifications-flag-guard.yaml): второго пути к значению
               флага нет, значение читается только из `global`;
  source     — у модуля-источника: запись перечня
               `{module, feedAddr, san, classes, recipientForms, authorization}`
               (форма KACHO_NOTIFY_SOURCES, З20). Поле `authorization` — литерал:
               `kaname` — `certificate`, прочие — `resolveSend` (Д72).

Строки на этой голове (полоса D2):
  · `notifyProbe` — стендовая проба-источник (templates/notify-probe.yaml
    зонтика). Адрес ленты и SAN — литералы установки: учётка и служба пробы
    названы постоянным именем `kacho-notify-probe` в пространстве `kacho`, домен
    доверия — `kacho.cloud` (`umbrella.trustDomain`). Декларации
    `global.kacho.spiffe.<служба>` и общий помощник URI заводит полоса J2 — тогда
    SAN строки берётся из декларации;
  · `kaname` — флаг почты службы доступа; полей источника нет — их вносит NTF-2,
    и только тогда kaname входит в перечень.

kacho.notifications.sources — выведенный перечень (JSON-массив записей `source`):
модули с полем `source` и действующим флагом `true`
(`kacho.notifications.enabledFor`). Вход — только `.Values.global` аргументом
(`dict "global" .Values.global`): помощник даёт один ответ в чарте notify и в
подчарте kaname. Ключ под `global.kacho.notifications.modules`, которого нет в
таблице, — отказ с именем ключа и перечнем ключей таблицы (CX1-111 (б)):
`modules.notifyprobe` иначе был бы принят и не прочитан никем.

kacho.notifications.ownFlagKeys — страж собственных ключей флага: вход
`dict "values" .Values` КОРНЯ значений зонтика (только шаблон зонтика видит
`.Values.<подчарт>`); ключ пути `ownFlagKey` любой строки, заданный в слое, —
отказ с полным путём (CX1-111 (а)). Пустой вывод — ключей нет.

notify.sourceLimits — ручки на источник (`sourceLimits` чарта, в зонтике —
`notify.sourceLimits`): ключ — имя модуля записи перечня; ключ не модуль-источник
таблицы — отказ с именем ключа; источник перечня без записи — отказ с именем
модуля (умолчания нет; NTF1-N03, H08). В процесс уходят записи ровно модулей
перечня — процесс отвергает запись вне перечня.
*/}}
{{- define "kacho.notifications.moduleTable" -}}
{{- list
  (dict "key" "notifyProbe" "ownFlagKey" "notifyProbe.notifications.enabled" "source" (dict "module" "notify-probe" "feedAddr" "kacho-notify-probe.kacho.svc:9091" "san" "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify-probe" "classes" (list "notice") "recipientForms" (list "address") "authorization" "resolveSend"))
  (dict "key" "kaname" "ownFlagKey" "kaname.config.notifications.enabled")
| toJson -}}
{{- end -}}

{{- define "kacho.notifications.sources" -}}
{{- $global := .global -}}
{{- $table := include "kacho.notifications.moduleTable" . | fromJsonArray -}}
{{- $keys := list -}}
{{- range $table -}}
{{- $keys = append $keys .key -}}
{{- end -}}
{{- /* Узел переопределений — пошагово, `kindIs "map"` на каждом шаге: `dig`
     по узлу не-словарю падает разыменованием. Не-словарь на любом шаге здесь
     пропускается — его отвергает `enabledFor` с именем ручки. */ -}}
{{- $mods := dict -}}
{{- if kindIs "map" $global -}}
{{- $k := index $global "kacho" -}}
{{- if kindIs "map" $k -}}
{{- $n := index $k "notifications" -}}
{{- if kindIs "map" $n -}}
{{- $mods = index $n "modules" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if kindIs "map" $mods -}}
{{- range $k, $_ := $mods -}}
{{- if not (has $k $keys) -}}
{{- fail (printf "global.kacho.notifications.modules.%s — ключа модуля %q нет в таблице модулей notify (ключи таблицы: %s); такое переопределение было бы принято и не прочитано никем (CX1-111 (б))" $k $k (join ", " $keys)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $out := list -}}
{{- range $table -}}
{{- if hasKey . "source" -}}
{{- if eq (include "kacho.notifications.enabledFor" (dict "global" $global "module" .key)) "true" -}}
{{- $out = append $out .source -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $out | toJson -}}
{{- end -}}

{{- define "kacho.notifications.ownFlagKeys" -}}
{{- $root := .values -}}
{{- $found := list -}}
{{- range (include "kacho.notifications.moduleTable" . | fromJsonArray) -}}
{{- $node := $root -}}
{{- $path := splitList "." .ownFlagKey -}}
{{- $last := last $path -}}
{{- range (initial $path) -}}
{{- if kindIs "map" $node -}}
{{- $node = index $node . -}}
{{- end -}}
{{- end -}}
{{- if and (kindIs "map" $node) (hasKey $node $last) -}}
{{- $found = append $found .ownFlagKey -}}
{{- end -}}
{{- end -}}
{{- join ", " $found -}}
{{- end -}}

{{- define "notify.sourceLimits" -}}
{{- $limits := .limits | default dict -}}
{{- if not (kindIs "map" $limits) -}}
{{- fail (printf "notify.sourceLimits (значение sourceLimits чарта notify) = %s — ожидается словарь «модуль → {rate, burst, paused}»" (toJson $limits)) -}}
{{- end -}}
{{- $modules := list -}}
{{- range (include "kacho.notifications.moduleTable" . | fromJsonArray) -}}
{{- if hasKey . "source" -}}
{{- $modules = append $modules .source.module -}}
{{- end -}}
{{- end -}}
{{- range $m, $_ := $limits -}}
{{- if not (has $m $modules) -}}
{{- fail (printf "notify.sourceLimits.%s — модуля %q нет среди источников таблицы модулей notify (источники таблицы: %s): ручки на источник не порождают источника (NTF1-N03)" $m $m (join ", " $modules)) -}}
{{- end -}}
{{- end -}}
{{- $out := dict -}}
{{- range .roster -}}
{{- if not (hasKey $limits .module) -}}
{{- fail (printf "notify.sourceLimits.%s не задан — у источника %q выведенного перечня нет записи {rate, burst, paused}; умолчания у ручек на источник нет (NTF1-N03, H08)" .module .module) -}}
{{- end -}}
{{- $_ := set $out .module (index $limits .module) -}}
{{- end -}}
{{- toJson $out -}}
{{- end -}}
