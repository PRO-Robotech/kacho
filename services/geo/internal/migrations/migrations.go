// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations встраивает goose SQL-миграции kacho-geo (схема kacho_geo).
// Источник истины — эта директория. Примененную миграцию не редактируем — только новая.
package migrations

import "embed"

// FS — встроенные миграции kacho-geo (формат goose).
//
//go:embed *.sql
var FS embed.FS
