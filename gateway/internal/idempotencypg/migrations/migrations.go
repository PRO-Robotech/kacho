// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations — встроенный каталог goose-миграций общего хранилища
// однократности края.
//
// Каталог свой, а не в services/*: край — не доменный сервис, у него одна
// таблица и нет своего мигратора-бинаря. Схему накатывает сам процесс при
// построении хранилища, под advisory-lock'ом базы, поэтому несколько реплик,
// стартующих одновременно, накатывают её ровно один раз (см. store.go).
package migrations

import "embed"

// FS — встроенный каталог миграций (goose).
//
//go:embed *.sql
var FS embed.FS
