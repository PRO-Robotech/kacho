// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrator — бизнес-логика отдельного бинаря cmd/migrator.
//
// dialect.go определяет абстракцию пакета — интерфейс [Dialect]. Продукт
// Postgres-only (rules: Postgres 16, database-per-service), поэтому реализация
// одна — [postgresDialect] (`postgres.go`); фабрика [NewDialect] резолвит ее по
// имени из CLI/конфига. Интерфейс — тонкий seam вокруг goose-конфигурации
// (goose-имя / driver / Up-Down-Status), а не задел под мульти-БД: второй
// диалект добавляется только когда станет реальным требованием (non-negotiable
// #11 — без speculative-абстракций).
//
// CLI-метадата диалекта живёт в общем пакете — [migratorcli.DialectSpec] и
// [migratorcli.SpecPostgres]. Здесь она НЕ переобъявляется: она была объявлена
// семь раз на дерево, и два текста отказа этого же шага успели разойтись
// (#1383). Дом общего пакета назван в docs/architecture/migrator-form.md.
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
// Реализация одна: [postgresDialect] (`postgres.go`) — через goose + pgx driver.
//
// Все методы принимают context.Context, DSN и embed.FS — это позволяет тестам
// подменять FS на `fstest.MapFS`, а боевому коду использовать
// `internal/migrations.FS`.
//
// Конструктор Dialect — [NewDialect].
type Dialect interface {
	// Up применяет миграции вверх. target=="" → до самой последней; иначе
	// до версии target (включительно).
	Up(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error

	// Down откатывает миграцию(и). target=="" → одна последняя; иначе до
	// версии target (включительно).
	Down(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error

	// Status печатает примененные/непримененные миграции в логгер goose.
	// out зарезервирован под будущий redirect (goose v3 пишет в свой logger).
	Status(ctx context.Context, dsn string, fsys fs.FS, dir string, out io.Writer) error

	// Spec возвращает CLI-метадату диалекта (имя, goose-имя, driver-имя для
	// sql.Open). Используется CLI для help / validation; runtime-логика
	// инкапсулирована в самих методах Up/Down/Status.
	Spec() migratorcli.DialectSpec
}

// NewDialect — фабрика, возвращает реализацию [Dialect] по имени. Поддерживается
// один диалект — "postgres" (продукт Postgres-only: Postgres 16,
// database-per-service). Неизвестное имя → ошибка. Второй диалект добавляется
// прямой веткой здесь, когда станет реальным требованием — без registry-таблицы /
// factory-типа под единственный элемент (non-negotiable #11).
func NewDialect(name string) (Dialect, error) {
	if name == migratorcli.SpecPostgres.Name {
		return newPostgresDialect(), nil
	}
	return nil, fmt.Errorf("unknown dialect %q (supported: %s)", name, migratorcli.SpecPostgres.Name)
}
