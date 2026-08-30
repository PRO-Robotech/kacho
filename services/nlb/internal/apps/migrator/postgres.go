// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// postgres.go — production-реализация [Dialect] для PostgreSQL через
// goose + pgx driver.
//
// Источник pattern'а — `kacho-vpc/internal/apps/migrator/postgres.go`.
package migrator

import (
	"context"
	"io"
	"io/fs"

	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
)

// postgresDialect — реализация [Dialect] для PostgreSQL. Stateless; concurrent
// использование из одного процесса не предполагается (CLI гоняет одну
// команду за раз).
type postgresDialect struct{}

func newPostgresDialect() *postgresDialect { return &postgresDialect{} }

func (p *postgresDialect) Spec() migratorcli.DialectSpec { return migratorcli.SpecPostgres }

func (p *postgresDialect) Up(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error {
	db, err := migratorcli.OpenDB(ctx, dsn, p.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := migratorcli.SetupGoose(fsys, p.Spec()); err != nil {
		return err
	}
	// ПРОПУЩЕННЫЕ МИГРАЦИИ ПРИНИМАЮТСЯ, и это не послабление, а следствие схемы
	// нумерации. Номер у нас — «задача × 1000 + порядок», и он НЕ хронологичен by
	// construction: задача #708 закрывается после #800, файл `708001` появляется в
	// дереве позже, чем `800001`. База, накатившая больший номер раньше, при
	// обновлении видит «пропущенную миграцию перед текущей версией» и отказывает —
	// служба не стартует вовсе.
	//
	// Замер на момент правки: таких пар в дереве 22, во ВСЕХ семи сервисах.
	// Конвейер их не видит by construction — он всегда поднимает чистую базу, где
	// пропущенных нет; воспроизводится только на обновлении развёрнутой (#1012).
	//
	// Приём пропущенной означает ПРИМЕНИТЬ её, а не пропустить; порядок внутри
	// одной задачи (`NNN001` до `NNN002`) goose сохраняет независимо от опции.
	if target == "" {
		return goose.UpContext(ctx, db, dir, goose.WithAllowMissing())
	}
	version, perr := migratorcli.ParseTargetVersion(target)
	if perr != nil {
		return perr
	}
	return goose.UpToContext(ctx, db, dir, version, goose.WithAllowMissing())
}

func (p *postgresDialect) Down(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error {
	db, err := migratorcli.OpenDB(ctx, dsn, p.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := migratorcli.SetupGoose(fsys, p.Spec()); err != nil {
		return err
	}
	if target == "" {
		return goose.DownContext(ctx, db, dir)
	}
	version, perr := migratorcli.ParseTargetVersion(target)
	if perr != nil {
		return perr
	}
	return goose.DownToContext(ctx, db, dir, version)
}

func (p *postgresDialect) Status(ctx context.Context, dsn string, fsys fs.FS, dir string, out io.Writer) error {
	db, err := migratorcli.OpenDB(ctx, dsn, p.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := migratorcli.SetupGoose(fsys, p.Spec()); err != nil {
		return err
	}
	_ = out // goose v3 пишет в свой logger; redirect — через goose.SetLogger
	return goose.StatusContext(ctx, db, dir)
}
