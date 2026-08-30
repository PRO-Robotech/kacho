// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrator — бизнес-логика отдельного бинаря cmd/migrator
// . До миграции kacho-nlb запускаются
// исключительно через этот отдельный binary; никакого `switch os.Args[1]` в
// kacho-loadbalancer.
//
// dialect.go определяет ключевую абстракцию пакета — интерфейс [Dialect].
// Единственная поддерживаемая БД — PostgreSQL (`postgres.go`); [ResolveDialect]
// выбирает реализацию по имени из CLI/конфига (неизвестное имя → ошибка). Это
// держит per-dialect tweaks за интерфейсом, без if-ветвей внутри общего Runner'а.
//
// Метадата диалекта живёт в общем пакете — [migratorcli.DialectSpec] и
// [migratorcli.SpecPostgres]; здесь она НЕ переобъявляется (#1383,
// docs/architecture/migrator-form.md).
package migrator

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

// Dialect — абстракция SQL-диалекта для миграций.
//
// Реализация:
//   - [postgresDialect] (`postgres.go`) — production, через goose + pgx driver.
type Dialect interface {
	// Up применяет миграции вверх. target=="" → до самой последней; иначе
	// до версии target (включительно).
	Up(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error

	// Down откатывает миграцию(и). target=="" → одна последняя; иначе до
	// версии target (включительно).
	Down(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error

	// Status печатает применённые/неприменённые миграции (через goose-logger).
	Status(ctx context.Context, dsn string, fsys fs.FS, dir string, out io.Writer) error

	// Spec — описательная метадата для CLI / help / тестов.
	Spec() migratorcli.DialectSpec
}

// ResolveDialect выбирает реализацию [Dialect] по имени из CLI/конфига.
// postgres — единственный поддерживаемый диалект; любое другое имя → ошибка
// (потребляется cmd/migrator: `--dialect <name>`).
func ResolveDialect(name string) (Dialect, error) {
	if name != migratorcli.SpecPostgres.Name {
		return nil, fmt.Errorf("unknown dialect %q (supported: %s)", name, migratorcli.SpecPostgres.Name)
	}
	return newPostgresDialect(), nil
}
