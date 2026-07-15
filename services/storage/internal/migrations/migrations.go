// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations встраивает goose SQL-миграции kacho-storage (схема
// kacho_storage). Источник истины — эта директория. Применённую миграцию не
// редактируем — только новая (ban #5).
//
// Общая LRO-таблица operations синхронизируется из corelib
// (kacho-corelib/migrations/common) в подкаталог common/ таргетом
// `make sync-migrations`; rpc-implementer встраивает её и наполняет доменные
// таблицы (volumes / volume_attachments / snapshots / disk_types) при реализации
// первого async-RPC. В скелете встроен только placeholder 0001_initial.sql (схема
// без доменных таблиц).
package migrations

import "embed"

// FS — встроенные миграции kacho-storage (формат goose).
//
//go:embed *.sql
var FS embed.FS
