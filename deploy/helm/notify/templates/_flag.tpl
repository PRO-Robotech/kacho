{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
ФЛАГ УСТАНОВКИ: ГЕНЕРИРОВАТЬ ЛИ ПОЧТОВЫЕ ЗАПРОСЫ (NTF-1 Р9, замысел З28; NTF1-N01,
N02, УК20; CX1-106, CX1-113).

kacho.notifications.enabledFor — действующее значение флага модуля строкой
`true` / `false`. Объявление одно:
  · `global.kacho.notifications.enabled` — флаг установки, булево, умолчания нет;
  · `global.kacho.notifications.modules.<ключ модуля>.enabled` — переопределение
    модуля, булево; нет ключа — модуль наследует флаг установки.

Вход — ТОЛЬКО `.Values.global`, переданный аргументом:
  include "kacho.notifications.enabledFor" (dict "global" .Values.global "module" "<ключ>")
а не контекст вызывающего. Подчарт зонтика видит только свои значения и
`global`: переопределение под ключом соседнего подчарта одному из читателей
(чарт notify — перечень источников; подчарт модуля — его переменная процесса)
было бы не видно, и перечень разошёлся бы с флагом модуля без единой ошибки
рендера.

Вход безопасен для nil: `global`, `global.kacho`, узел `notifications` —
каждый проверяется `kindIs "map"` по шагу, и отсутствие любого — `fail` с именем
ручки, а не падение разыменования («interface conversion»). Значение судится
`kindIs "bool"`: строка «false» (`--set-string`, накладка в кавычках) и пустая
строка — отказ с именем ручки, а не молчаливое «выключено». `default`, `or` и
`if` над значением здесь нет: каждое из них прочло бы «незадано» как «false».

Тело одно на весь релиз — здесь, в чарте notify (CX1-113): чарт notify входит в
каждый рендер зонтика (зависимость без `condition`) и в рендер ноги без зонтика.
Одиночный рендер подчарта kaname получает копию этого файла обёрткой
`render_kaname_alone` / `renderKanameAlone` (deploy/tests/helm/lib/render-chain.sh,
deploy/kaname_alone_render_test.go); второго тела в дереве нет —
deploy/notify_helpers_single_define_test.go.
*/}}
{{- define "kacho.notifications.enabledFor" -}}
{{- $knob := "global.kacho.notifications.enabled" -}}
{{- $module := .module -}}
{{- if not (kindIs "string" $module) -}}
{{- fail "kacho.notifications.enabledFor: ключ модуля не передан — вызов обязан называть модуль (dict \"global\" .Values.global \"module\" \"<ключ>\")" -}}
{{- end -}}
{{- $g := .global -}}
{{- if not (kindIs "map" $g) -}}
{{- fail (printf "%s не задан: значений `global` нет — флаг установки объявляется явно, true либо false (NTF1-N01)" $knob) -}}
{{- end -}}
{{- $k := index $g "kacho" -}}
{{- if not (kindIs "map" $k) -}}
{{- fail (printf "%s не задан: узла `global.kacho` нет — флаг установки объявляется явно, true либо false (NTF1-N01)" $knob) -}}
{{- end -}}
{{- $n := index $k "notifications" -}}
{{- if not (kindIs "map" $n) -}}
{{- fail (printf "%s не задан: узла `global.kacho.notifications` нет — флаг установки объявляется явно, true либо false (NTF1-N01)" $knob) -}}
{{- end -}}
{{- /* Нулевое значение слоя (`--set …=null`) под `global` helm доносит ключом
     с `null`, а не снимает его: «ключ с null» читается как «ключа нет». */ -}}
{{- if or (not (hasKey $n "enabled")) (kindIs "invalid" (index $n "enabled")) -}}
{{- fail (printf "%s не задан — флаг установки объявляется явно, true либо false; незаданный не читается ни как «включено», ни как «выключено» (NTF1-N01)" $knob) -}}
{{- end -}}
{{- $global := index $n "enabled" -}}
{{- if not (kindIs "bool" $global) -}}
{{- fail (printf "%s = %s (%s) — ожидается булево значение true либо false без кавычек (УК20)" $knob (toJson $global) (kindOf $global)) -}}
{{- end -}}
{{- $value := $global -}}
{{- $mods := index $n "modules" -}}
{{- if not (kindIs "invalid" $mods) -}}
{{- if not (kindIs "map" $mods) -}}
{{- fail (printf "global.kacho.notifications.modules = %s (%s) — ожидается словарь «ключ модуля → {enabled}» (CX1-106)" (toJson $mods) (kindOf $mods)) -}}
{{- end -}}
{{- $m := index $mods $module -}}
{{- if not (kindIs "invalid" $m) -}}
{{- $mknob := printf "global.kacho.notifications.modules.%s.enabled" $module -}}
{{- if not (kindIs "map" $m) -}}
{{- fail (printf "global.kacho.notifications.modules.%s = %s (%s) — ожидается словарь {enabled: true|false} (CX1-106)" $module (toJson $m) (kindOf $m)) -}}
{{- end -}}
{{- $mv := index $m "enabled" -}}
{{- if not (kindIs "invalid" $mv) -}}
{{- if not (kindIs "bool" $mv) -}}
{{- fail (printf "%s = %s (%s) — ожидается булево значение true либо false без кавычек (УК20)" $mknob (toJson $mv) (kindOf $mv)) -}}
{{- end -}}
{{- $value = $mv -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- ternary "true" "false" $value -}}
{{- end -}}
