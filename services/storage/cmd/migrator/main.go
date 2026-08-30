// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-migrator — накат миграций схемы БД kacho-storage (goose поверх
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
// DSN: --dsn > ENV KACHO_MIGRATOR_DSN > конфигурация kacho-storage (KACHO_STORAGE_*).
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"
	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
	"github.com/PRO-Robotech/kacho/pkg/dbready"
	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
)

// binaryName — имя бинаря одно на все семь сервисов; оно стоит в манифестах
// развёртывания и в текстах отказа, поэтому названо здесь один раз.
const binaryName = "kacho-migrator"

func main() {
	opts, err := migratorcli.Parse(binaryName, os.Args[1:])
	switch {
	case errors.Is(err, migratorcli.ErrHelpRequested):
		fmt.Println(migratorcli.Usage(binaryName))
		return
	case errors.Is(err, migratorcli.ErrNoCommand):
		// Форма вызова печатается ОТДЕЛЬНО, а исход остаётся отказом: ровно так
		// делегирующая форма печатает помощь и выходит кодом 1. Вшить форму
		// вызова в текст отказа значило бы сделать первую строку разной у семи.
		fmt.Println(migratorcli.Usage(binaryName))
		fail(err)
	case err != nil:
		fail(err)
	}

	dsn, err := migratorcli.ResolveDSN(opts.DSN, func() (string, error) {
		cfg, cerr := config.Load()
		if cerr != nil {
			return "", cerr
		}
		return cfg.MigrateDSN(), nil
	})
	if err != nil {
		fail(err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect(opts.Dialect); err != nil {
		fail(fmt.Errorf("goose dialect: %w", err))
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fail(fmt.Errorf("open db: %w", err))
	}
	defer func() { _ = db.Close() }()

	// Барьер готовности PG. sql.Open ЛЕНИВ (не дозванивается до сервера), поэтому
	// гонка init-контейнера с подом Postgres проявлялась не здесь, а ниже — на
	// goose: мигратор падал отказом и уходил в CrashLoopBackOff до подъёма
	// PG. Ждём ТОЛЬКО «БД не принимает соединения» и ТОЛЬКО в пределах бюджета;
	// неверный пароль / несуществующая БД / сломанная миграция падают сразу.
	if err := dbready.Wait(context.Background(), db, dbready.Options{}); err != nil {
		// Текст нейтральный: сюда приходит И «не дождались» (ошибка уже несёт
		// бюджет), И настоящая ошибка (пароль/DSN/БД) — второй случай называть
		// «not ready» было бы враньём в логе.
		fail(fmt.Errorf("database connection check failed: %w", err))
	}

	if err := run(db, opts); err != nil {
		fail(fmt.Errorf("migrate %s: %w", opts.Command, err))
	}
}

// fail подаёт отказ в форме, одной на семь точек наката (`Error: <предмет>`), и
// выходит кодом 1. Через журнал отказ больше не идёт: журнал ставит впереди
// метку времени, и она делала из одного контракта две редакции — для скрипта,
// читающего отказ образцом, это разные строки.
func fail(err error) {
	migratorcli.ReportError(os.Stderr, err)
	os.Exit(1)
}

// run исполняет разобранную команду. Вынесено из main, чтобы порядок ветвления
// читался целиком и чтобы `--target` было видно рядом с его отсутствием.
func run(db *sql.DB, opts migratorcli.Options) error {
	const dir = "."

	switch opts.Command {
	case migratorcli.CommandUp:
		// ЖИВОЙ СЧЁТ ПЕРЕД СНОСОМ. Таблица, которую роняет ещё не применённая
		// миграция, считается ЗДЕСЬ — пока строки ещё есть и пока отказ стоит одной
		// выкатки. Down-миграция возвращает форму, а не данные, поэтому «восстановимо»
		// про снос неверно: восстановима схема.
		//
		// Измеряющий гейт в internal/migrations отвечает на другой вопрос — сколько
		// сеет наша собственная цепочка, проигранная в пустую базу. Что написал
		// арендатор, не знает ни один контейнер, и узнать это можно только здесь.
		//
		// Недоступность базы — НЕ «ноль строк»: она отказ, а не разрешение.
		//
		// Счёт стоит ДО ОБЕИХ ветвей применения — и полной, и `--target`, — и
		// покрывает ВСЕ ещё не применённые сносы, включая те, до которых эта цель
		// не докатится. Ошибка здесь идёт в сторону отказа, а не разрешения; ветвь,
		// обходящая счёт ради цели, была бы тем самым глобальным выключателем,
		// которого у гейта нет намеренно. Лишний отказ снимается, как и всякий
		// другой, — именем конкретного сноса (см. dropguard.ApprovalEnv).
		//
		// Отказ идёт наверх ошибкой, а не через журнал: журнал ставит впереди метку
		// времени, и она делала из одного контракта две редакции (см. fail).
		if err := dropguard.Gate(context.Background(), db, "storage", migrations.FS, os.Stderr); err != nil {
			return err
		}
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
