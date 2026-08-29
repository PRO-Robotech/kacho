// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-migrator — накат миграций схемы БД kacho-registry (goose поверх
// embed `internal/migrations`). Отдельная точка сборки: serve-бинарь схему не
// меняет (least-privilege), миграции гоняет одноразовый init-контейнер.
//
//	kacho-migrator [--dsn DSN] [--dialect postgres] {up|down|status} [--target VERSION]
//
// Разбор аргументов — общий на все точки наката прямой формы
// (`pkg/migratorcli`), и это не украшение: собственный разбор МОЛЧА терял флаг,
// написанный после подкоманды, поэтому `kacho-migrator up --dsn X` накатывал не
// на ту базу и выглядел успехом. Поверхность CLI объявлена в
// docs/architecture/migrator-cli.md, форма самой точки наката — в
// docs/architecture/migrator-form.md.
//
// DSN: --dsn > ENV KACHO_MIGRATOR_DSN > конфигурация kacho-registry (KACHO_REGISTRY_*).
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"
	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/dbready"
	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// binaryName — имя бинаря одно на все семь сервисов; оно стоит в манифестах
// развёртывания и в текстах отказа, поэтому названо здесь один раз.
const binaryName = "kacho-migrator"

func main() {
	opts, err := migratorcli.Parse(binaryName, os.Args[1:])
	if errors.Is(err, migratorcli.ErrHelpRequested) {
		fmt.Println(migratorcli.Usage(binaryName))
		return
	}
	if err != nil {
		log.Fatal(err)
	}

	dsn, err := migratorcli.ResolveDSN(opts.DSN, func() (string, error) {
		cfg, cerr := config.Load()
		if cerr != nil {
			return "", cerr
		}
		return cfg.MigrateDSN(), nil
	})
	if err != nil {
		log.Fatal(err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect(opts.Dialect); err != nil {
		log.Fatalf("goose dialect: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Барьер готовности PG. sql.Open ЛЕНИВ (не дозванивается до сервера), поэтому
	// гонка init-контейнера с подом Postgres проявлялась не здесь, а ниже — на
	// goose: мигратор падал log.Fatalf'ом и уходил в CrashLoopBackOff до подъёма
	// PG. Ждём ТОЛЬКО «БД не принимает соединения» и ТОЛЬКО в пределах бюджета;
	// неверный пароль / несуществующая БД / сломанная миграция падают сразу.
	if err := dbready.Wait(context.Background(), db, dbready.Options{}); err != nil {
		// Текст нейтральный: сюда приходит И «не дождались» (ошибка уже несёт
		// бюджет), И настоящая ошибка (пароль/DSN/БД) — второй случай называть
		// «not ready» было бы враньём в логе.
		log.Fatalf("database connection check failed: %v", err)
	}

	if err := run(db, opts); err != nil {
		log.Fatalf("migrate %s: %v", opts.Command, err)
	}
}

// run исполняет разобранную команду. Вынесено из main, чтобы порядок ветвления
// читался целиком и чтобы `--target` было видно рядом с его отсутствием.
func run(db *sql.DB, opts migratorcli.Options) error {
	const dir = "."

	switch opts.Command {
	case migratorcli.CommandUp:
		// ПРОПУЩЕННЫЕ МИГРАЦИИ ПРИНИМАЮТСЯ, и это не послабление, а следствие схемы
		// нумерации. Номер у нас — «задача × 1000 + порядок», и он НЕ хронологичен by
		// construction: задача закрывается не по порядку номеров, поэтому файл с
		// меньшим номером появляется в дереве позже. База, накатившая больший номер
		// раньше, при обновлении видит «пропущенную миграцию перед текущей версией»
		// и отказывает — служба не стартует вовсе.
		//
		// Приём пропущенной означает ПРИМЕНИТЬ её, а не пропустить; порядок внутри
		// одной задачи (`NNN001` до `NNN002`) goose сохраняет независимо от опции.
		if opts.Target == "" {
			return goose.Up(db, dir, goose.WithAllowMissing())
		}
		version, err := migratorcli.ParseTargetVersion(opts.Target)
		if err != nil {
			return err
		}
		return goose.UpTo(db, dir, version, goose.WithAllowMissing())

	case migratorcli.CommandDown:
		if opts.Target == "" {
			return goose.Down(db, dir)
		}
		version, err := migratorcli.ParseTargetVersion(opts.Target)
		if err != nil {
			return err
		}
		return goose.DownTo(db, dir, version)

	case migratorcli.CommandStatus:
		return goose.Status(db, dir)
	}
	// Недостижимо: перечень подкоманд закрыт разбором. Ветка существует, чтобы
	// расширение перечня не проходило молча — молчаливый успех на неизвестной
	// команде и есть тот класс, ради которого задача заведена.
	return fmt.Errorf("unhandled command %q", opts.Command)
}
