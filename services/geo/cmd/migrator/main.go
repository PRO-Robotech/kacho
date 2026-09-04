// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-migrator — накат миграций схемы БД kacho-geo (goose поверх
// встроенной `internal/migrations`). Отдельная точка сборки: serve-бинарь схему
// не меняет (least-privilege), миграции гоняет одноразовый init-контейнер.
//
//	kacho-migrator [--dsn DSN] [--dialect postgres] {up|down|status} [--target VERSION]
//
// Разбор аргументов и САМ НАКАТ — общие на все семь точек наката
// (`pkg/migratorcli` и `pkg/migratorrun`), и это не украшение. Разбор:
// собственный МОЛЧА терял флаг, написанный после подкоманды, поэтому
// `kacho-migrator up --dsn X` накатывал не на ту базу и выглядел успехом.
// Накат: форм было ДВЕ, и различие никем не решалось — оно завелось побочным
// эффектом того, что службы заводились в разное время.
//
// Здесь остаётся ровно то, что у службы своё: её имя, её цепочка миграций и её
// способ добыть DSN из собственной конфигурации.
//
// Поверхность CLI объявлена в docs/architecture/migrator-cli.md, форма самой
// точки наката — в docs/architecture/migrator-form.md.
//
// DSN: --dsn > ENV KACHO_MIGRATOR_DSN > конфигурация kacho-geo (KACHO_GEO_*).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/pkg/migratorrun"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/geo/internal/migrations"
)

const (
	// binaryName — имя бинаря одно на все семь служб; оно стоит в манифестах
	// развёртывания и в текстах отказа, поэтому названо здесь один раз.
	binaryName = "kacho-migrator"

	// serviceName — чью цепочку применяет эта точка наката. Живой счёт строк
	// перед сносом называет им то, что стережёт, поэтому безымянной точки наката
	// не бывает: предусловия отказывают ей в старте.
	serviceName = "geo"

	// migrationsDir — путь внутри встроенной файловой системы; её корень — ".".
	migrationsDir = "."
)

// dsnExtraSources — чем ЭТА служба заполняет DSN СВЕРХ двух общих (`--dsn` и
// KACHO_MIGRATOR_DSN), в порядке убывания приоритета. Два общих здесь НЕ
// перечисляются намеренно: их печатает сам общий пакет, поэтому умолчать
// источник, который перебивает названные, нельзя by construction. Ровно это и
// случилось однажды — текст отказа называл третий источник и умалчивал второй.
var dsnExtraSources = []string{"kacho-geo config (KACHO_GEO_*)"}

func main() {
	opts, err := migratorcli.Parse(binaryName, os.Args[1:])
	switch {
	case errors.Is(err, migratorcli.ErrHelpRequested):
		fmt.Println(migratorcli.Usage(binaryName))
		return
	case errors.Is(err, migratorcli.ErrNoCommand):
		// Форма вызова печатается ОТДЕЛЬНО, а исход остаётся отказом. Вшить форму
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

	runner, err := migratorrun.New(migratorrun.Config{
		Service:         serviceName,
		Dialect:         opts.Dialect,
		DSN:             dsn,
		FS:              migrations.FS,
		MigrationsDir:   migrationsDir,
		DSNExtraSources: dsnExtraSources,
	})
	if err != nil {
		fail(err)
	}

	if err := run(context.Background(), runner, opts); err != nil {
		fail(fmt.Errorf("migrate %s: %w", opts.Command, err))
	}
}

// fail подаёт отказ в форме, одной на семь точек наката (`Error: <предмет>`), и
// выходит кодом 1. Через журнал отказ не идёт: журнал ставит впереди метку
// времени, и она делала из одного контракта две редакции — для скрипта,
// читающего отказ образцом, это разные строки.
func fail(err error) {
	migratorcli.ReportError(os.Stderr, err)
	os.Exit(1)
}

// run исполняет разобранную команду. Счёт строк перед сносом здесь НЕ ВИДЕН, и
// это построение, а не пропуск: он живёт внутри [migratorrun.Runner.Up], откуда
// его не обойти, не обойдя сам Up. Прежде он стоял отдельным оператором ровно
// здесь — то есть шагом, который однажды могли не позвать.
func run(ctx context.Context, r *migratorrun.Runner, opts migratorcli.Options) error {
	switch opts.Command {
	case migratorcli.CommandUp:
		return r.Up(ctx, opts.Target)
	case migratorcli.CommandDown:
		return r.Down(ctx, opts.Target)
	case migratorcli.CommandStatus:
		return r.Status(ctx, os.Stdout)
	}
	// Недостижимо: перечень подкоманд закрыт разбором. Ветка существует, чтобы
	// расширение перечня не проходило молча — молчаливый успех на неизвестной
	// команде и есть тот класс, ради которого задача заведена.
	return fmt.Errorf("unhandled command %q", opts.Command)
}
