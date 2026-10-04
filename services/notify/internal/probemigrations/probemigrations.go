// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package probemigrations встраивает goose-миграции базы пробы-источника
// notify-probe (схема kacho_notifyprobe): журнал подписки и лента извещений.
// Миграцию ленты пишет генератор (`notifygen init`), и её содержимое он же
// сверяет побайтово (`notifygen -check`); применённую миграцию не правят —
// только новая (ban #5).
//
// Пакет лежит под services/notify/internal, а не под cmd/notify-probe/internal:
// цепочку встраивает импортом точка наката каталога services/notify/cmd/migrator,
// а правило `internal` языка запрещает ей импорт из-под cmd/notify-probe
// (замысел З32, CX1-114). Таблица «имя базы → каталог» —
// services/notify/cmd/migrator/chains.yaml (база kacho_notifyprobe).
package probemigrations

import "embed"

// FS — встроенные миграции notify-probe (формат goose).
//
//go:embed *.sql
var FS embed.FS
