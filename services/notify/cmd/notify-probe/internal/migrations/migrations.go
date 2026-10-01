// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations встраивает goose-миграции базы пробы-источника
// notify-probe (схема kacho_notifyprobe): журнал подписки и лента извещений.
// Миграцию ленты пишет генератор (`notifygen init`), и её содержимое он же
// сверяет побайтово (`notifygen -check`); применённую миграцию не правят —
// только новая (ban #5).
package migrations

import "embed"

// FS — встроенные миграции notify-probe (формат goose).
//
//go:embed *.sql
var FS embed.FS
