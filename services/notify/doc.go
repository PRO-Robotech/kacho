// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package notify — пакет-владелец каталога шаблонов `notifications/` службы
// notify. Генератор постановки (`notifygen`, corelib) кладёт порождённые
// `notifications_<имя>.gen.go` в пакет каталога, которому принадлежит
// `notifications/`, и берёт имя пакета из его не-тестовых файлов: этот файл —
// объявление пакета, без которого имя порождённого файла не из чего взять.
//
// Сегодня в каталоге один шаблон — `probe-hello` пробы-источника
// `cmd/notify-probe` (стендовый объект); его `SendProbeHello` зовёт глагол
// пробы.
package notify
