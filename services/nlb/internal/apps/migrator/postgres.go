// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// postgres.go — production-реализация [Dialect] для PostgreSQL через
// goose + pgx driver.
//
// Источник pattern'а — `kacho-vpc/internal/apps/migrator/postgres.go`.
package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"

	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/dbready"
)

// postgresDialect — реализация [Dialect] для PostgreSQL. Stateless; concurrent
// использование из одного процесса не предполагается (CLI гоняет одну
// команду за раз).
type postgresDialect struct{}

func newPostgresDialect() *postgresDialect { return &postgresDialect{} }

func (p *postgresDialect) Spec() DialectSpec { return SpecPostgres }

func (p *postgresDialect) Up(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error {
	db, err := openPgxDB(ctx, dsn, p.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := setupGoose(fsys, p.Spec()); err != nil {
		return err
	}
	if target == "" {
		return goose.UpContext(ctx, db, dir)
	}
	version, perr := parseTargetVersion(target)
	if perr != nil {
		return perr
	}
	return goose.UpToContext(ctx, db, dir, version)
}

func (p *postgresDialect) Down(ctx context.Context, dsn string, fsys fs.FS, dir string, target string) error {
	db, err := openPgxDB(ctx, dsn, p.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := setupGoose(fsys, p.Spec()); err != nil {
		return err
	}
	if target == "" {
		return goose.DownContext(ctx, db, dir)
	}
	version, perr := parseTargetVersion(target)
	if perr != nil {
		return perr
	}
	return goose.DownToContext(ctx, db, dir, version)
}

func (p *postgresDialect) Status(ctx context.Context, dsn string, fsys fs.FS, dir string, out io.Writer) error {
	db, err := openPgxDB(ctx, dsn, p.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := setupGoose(fsys, p.Spec()); err != nil {
		return err
	}
	_ = out // goose v3 пишет в свой logger; redirect — через goose.SetLogger
	return goose.StatusContext(ctx, db, dir)
}

// openPgxDB / setupGoose — общие helpers, переиспользуемы будущими диалектами
// (cockroach делит pgx driver и goose-dialect="postgres" — см. kacho-vpc).

func openPgxDB(ctx context.Context, dsn string, spec DialectSpec) (*sql.DB, error) {
	db, err := sql.Open(spec.SQLDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db (driver=%s): %w", spec.SQLDriver, err)
	}
	// Барьер готовности PG. sql.Open ЛЕНИВ (не дозванивается до сервера), поэтому
	// гонка init-контейнера с подом Postgres проявлялась не здесь, а на первой
	// goose-операции: мигратор падал и уходил в CrashLoopBackOff до подъёма PG.
	// Ждём ТОЛЬКО «БД не принимает соединения» и ТОЛЬКО в пределах бюджета;
	// неверный пароль / несуществующая БД / сломанная миграция падают сразу.
	if err := dbready.Wait(ctx, db, dbready.Options{}); err != nil {
		_ = db.Close()
		// Текст нейтральный: сюда приходит И «не дождались» (ошибка уже несёт
		// бюджет), И настоящая ошибка (пароль/DSN/БД) — второй случай называть
		// «not ready» было бы враньём в логе.
		return nil, fmt.Errorf("database connection check failed: %w", err)
	}
	return db, nil
}

func setupGoose(fsys fs.FS, spec DialectSpec) error {
	goose.SetBaseFS(fsys)
	if err := goose.SetDialect(spec.GooseDialect); err != nil {
		return fmt.Errorf("goose set dialect %q: %w", spec.GooseDialect, err)
	}
	return nil
}
