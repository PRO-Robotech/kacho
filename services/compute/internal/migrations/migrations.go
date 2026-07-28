// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations

import "embed"

// FS — embedded боевые миграции kacho-compute (goose-формат).
// Source of truth — этот каталог, и только он; таблица operations объявлена
// здесь же, в 0001_initial.sql, схема kacho_compute.
//
//go:embed *.sql
var FS embed.FS
