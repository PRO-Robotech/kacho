{{/*
Copyright (c) PRO-Robotech
SPDX-License-Identifier: BUSL-1.1
*/}}

{{/*
ПЕРЕЧЕНЬ ИСТОЧНИКОВ NOTIFY — ВЫВОДИТСЯ, РУЧНОГО НЕТ (замысел З28, NTF1-N03).

notify.pluggableSources — ЗАКРЫТАЯ таблица подключаемых источников: записи
`{module, feedAddr, san, classes, recipientForms, authorization}` (форма
KACHO_NOTIFY_SOURCES, З20). Поле `authorization` таблица несёт литералом:
`kaname` — `certificate`, прочие — `resolveSend` (Д72).

В этой полосе (NTF-1 D1) таблица ПУСТА: ни один модуль-источник ещё не
подключён. Строку `notify-probe`, флаг `global.kacho.notifications.enabled`,
переопределение модуля и помощник `kacho.notifications.enabledFor` вносит
полоса D2 — одним изменением, с которым notify и входит в рендер цепочек
зонтика (З28 «Узел стенда»). Пока таблица пуста, перечень пуст в каждой
цепочке, и объектов notify 0 (NTF1-N04).

notify.sourceRoster — выведенный перечень как JSON-массив. Вход — только
`.Values.global` аргументом (`dict "global" .Values.global`), а не контекст
вызывающего: подчарт зонтика видит только свои значения и `global`, и тот же
помощник обязан давать один ответ в чарте notify и в подчарте kaname (З13, З28).
*/}}
{{- define "notify.pluggableSources" -}}
{{- list | toJson -}}
{{- end -}}

{{- define "notify.sourceRoster" -}}
{{- include "notify.pluggableSources" . -}}
{{- end -}}
