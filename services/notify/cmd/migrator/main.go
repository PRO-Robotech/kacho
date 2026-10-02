// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — точка наката каталога services/notify, бинарь `kacho-migrator`
// (kacho#2915, замысел З32, решения Д74, Д77).
//
//	kacho-migrator up [--target <version>]
//	kacho-migrator down [--target <version>]
//	kacho-migrator status
//
// # Одна точка — две базы
//
// Каталог несёт два процесса (`cmd/notify` и `cmd/notify-probe`) и по базе на
// каждый (database per service). Точка у каталога одна, и цепочку она выбирает
// ПО ИМЕНИ БАЗЫ В DSN по закрытой таблице `chains.yaml` («имя базы → каталог
// цепочки»). Имя вне таблицы — отказ с именем базы и перечнем допустимых, без
// наката. Запасной ветки на имя вне таблицы нет, флага, ручки или переменной
// выбора цепочки — тоже: их появление сделало бы инъекцию «строка снята»
// зелёной.
//
// # DSN — только `--dsn` или KACHO_MIGRATOR_DSN
//
// Конфигурации процесса точка не читает: у каталога их две, и выбор по ней был
// бы третьим местом решения о цепочке. Приоритет источников — общий
// (`migratorcli.ResolveDSN`), запасного источника у этой точки нет.
//
// # Таблица и встроенные FS
//
// Таблица читается `//go:embed`; встроенная FS цепочки — импорт её пакета.
// Соответствие «каталог строки ↔ встроенная FS» сверяется при старте:
// множества не равны — отказ старта с обоими множествами. Второго литерала
// таблицы нет, есть проверка равенства.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует "pgx" driver для sql.Open

	"github.com/PRO-Robotech/corelib/migratorcli"
	"github.com/PRO-Robotech/corelib/migratorrun"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
	"github.com/PRO-Robotech/kacho/services/notify/internal/probemigrations"
)

const (
	// binaryName — имя бинаря одно на все точки наката продукта.
	binaryName = "kacho-migrator"

	// serviceName — чьи цепочки применяет точка: каталог службы.
	serviceName = "notify"

	// pointDir — каталог точки от корня: им точка называет себя в отказах.
	pointDir = "services/notify/cmd/migrator"

	// migrationsDir — путь внутри встроенной FS; её корень — ".".
	migrationsDir = "."
)

// chainsTable — таблица «имя базы → каталог цепочки».
//
//go:embed chains.yaml
var chainsTable []byte

// embedded — встроенные FS цепочек точки по каталогу от корня. Строка таблицы
// без FS здесь и FS без строки таблицы — отказ старта (sameSets).
var embedded = map[string]fs.FS{
	"services/notify/internal/probemigrations": probemigrations.FS,
}

func main() {
	opts, err := migratorcli.Parse(binaryName, os.Args[1:])
	switch {
	case errors.Is(err, migratorcli.ErrHelpRequested):
		fmt.Println(migratorcli.Usage(binaryName))
		return
	case errors.Is(err, migratorcli.ErrNoCommand):
		fmt.Println(migratorcli.Usage(binaryName))
		fail(err)
	case err != nil:
		fail(err)
	}

	// Запасного источника у точки нет: конфигурации процесса она не читает.
	dsn, err := migratorcli.ResolveDSN(opts.DSN, nil)
	if err != nil {
		fail(err)
	}

	_, fsys, err := selectChain(chainsTable, embedded, dsn)
	if err != nil {
		fail(err)
	}

	runner, err := migratorrun.New(migratorrun.Config{
		Service:       serviceName,
		Dialect:       opts.Dialect,
		DSN:           dsn,
		FS:            fsys,
		MigrationsDir: migrationsDir,
	})
	if err != nil {
		fail(err)
	}

	if err := run(context.Background(), runner, opts); err != nil {
		fail(fmt.Errorf("migrate %s: %w", opts.Command, err))
	}
}

// selectChain — строка таблицы и встроенная FS цепочки для DSN. Порядок:
// сперва равенство таблицы и встроенных FS (отказ старта), затем имя базы из
// DSN, затем строка с ровно этим именем.
func selectChain(table []byte, fss map[string]fs.FS, dsn string) (migrationchains.Chain, fs.FS, error) {
	rows, err := migrationchains.ParseTable(pointDir, table)
	if err != nil {
		return migrationchains.Chain{}, nil, err
	}
	if err := sameSets(rows, fss); err != nil {
		return migrationchains.Chain{}, nil, err
	}
	db, err := migrationchains.DatabaseOf(dsn)
	if err != nil {
		return migrationchains.Chain{}, nil, fmt.Errorf("точка наката %s: %w", pointDir, err)
	}
	allowed := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Database == db {
			return r, fss[r.Dir], nil
		}
		allowed = append(allowed, r.Database)
	}
	sort.Strings(allowed)
	return migrationchains.Chain{}, nil, fmt.Errorf("точка наката %s: база %q вне таблицы цепочек; "+
		"допустимые: %v — наката нет", pointDir, db, allowed)
}

func sameSets(rows []migrationchains.Chain, fss map[string]fs.FS) error {
	inTable := make([]string, 0, len(rows))
	set := map[string]bool{}
	for _, r := range rows {
		inTable = append(inTable, r.Dir)
		set[r.Dir] = true
	}
	inFS := make([]string, 0, len(fss))
	for dir, f := range fss {
		if f != nil {
			inFS = append(inFS, dir)
		}
	}
	sort.Strings(inTable)
	sort.Strings(inFS)
	equal := len(inTable) == len(inFS)
	for _, d := range inFS {
		if !set[d] {
			equal = false
		}
	}
	if !equal {
		return fmt.Errorf("точка наката %s: каталоги таблицы %v и встроенных FS %v не равны — "+
			"цепочка без FS не накатится, FS без строки не выбирается", pointDir, inTable, inFS)
	}
	return nil
}

// fail подаёт отказ в форме, одной на все точки наката (`Error: <предмет>`), и
// выходит кодом 1.
func fail(err error) {
	migratorcli.ReportError(os.Stderr, err)
	os.Exit(1)
}

// run исполняет разобранную команду. Счёт строк перед сносом живёт внутри
// [migratorrun.Runner.Up].
func run(ctx context.Context, r *migratorrun.Runner, opts migratorcli.Options) error {
	switch opts.Command {
	case migratorcli.CommandUp:
		return r.Up(ctx, opts.Target)
	case migratorcli.CommandDown:
		return r.Down(ctx, opts.Target)
	case migratorcli.CommandStatus:
		return r.Status(ctx, os.Stdout)
	}
	return fmt.Errorf("unhandled command %q", opts.Command)
}
