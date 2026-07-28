// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrations встраивает goose SQL-миграции kacho-storage (схема
// kacho_storage). Источник истины — эта директория. Применённую миграцию не
// редактируем — только новая (ban #5).
//
// Общая LRO-таблица operations живёт здесь же — 0002_operations.sql, схема
// kacho_storage. Отдельного набора «общих» миграций у сервиса нет: каждый сервис
// заводит свою operations в собственной схеме, а «синхронизация из corelib» была
// заявлена, но никогда не работала — подкаталог common/ в этот embed вообще не
// попадал (`*.sql` не рекурсивен), поэтому лежавшие там копии не применял никто.
package migrations

import "embed"

// FS — встроенные миграции kacho-storage (формат goose).
//
//go:embed *.sql
var FS embed.FS
