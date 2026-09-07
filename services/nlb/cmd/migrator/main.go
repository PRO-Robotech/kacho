// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command migrator — отдельный binary мигратора схемы kacho-nlb.
//
// + /: отдельный CLI use-case = отдельный binary;
// миграции — НЕ subcommand основного сервиса. Диалект резолвится через
// [migrator.Dialect] interface (postgres — единственный поддерживаемый; интерфейс
// оставляет место под будущие диалекты без if-ветвей в Runner'е).
//
// Subcommands:
//
//	kacho-migrator up      [--target <version>]
//	kacho-migrator down    [--target <version>]
//	kacho-migrator status
//
// # Глагола `create` здесь НЕТ — и это решение, а не пропуск (#566)
//
// Он был и выдавал `goose.Create` без `SetSequential`. Имя миграции в этом
// дереве пишет АВТОР: форма номера — решение, а инструмент принял бы его молча
// и всегда одинаково.
//
// Действующая форма — метка времени заведения `YYYYMMDDHHMMSS_<что_делает>.sql`
// (`date -u +%Y%m%d%H%M%S`). Объявлена она в ОДНОМ месте и здесь НЕ
// переписывается: docs/architecture/migration-version-namespace.md. Своей
// редакции у справки быть не должно — две редакции об одном предмете расходятся
// молча, и расходились (#1026).
//
// Global flags:
//
//	--dialect oneof<postgres>                 (default postgres)
//	--dsn     <connection-string>             (или ENV / --config fallback)
//	--config  /etc/kacho-nlb/config.yaml      (если --dsn пуст — читает
//	                                          repository.postgres.url из YAML)
//
// Источник DSN — приоритет: --dsn > ENV `KACHO_MIGRATOR_DSN` > --config
// (`config.MigrateDSN`, она же читает `KACHO_NLB_REPOSITORY__POSTGRES__URL`).
// Так одна и та же `values.yaml` покрывает оба бинаря (kacho-loadbalancer +
// kacho-migrator) без дублирования.
//
// # Конфигурацию читает СВОЯ дверь, и это решение
//
// Накат спрашивает у конфигурации то, чем пользуется сам: адрес базы и режим
// шифрования до неё. Страж посадки СЛУЖБЫ (mTLS рёбер, круг отправителей чужой
// личности, объявление домена величин, фильтр списков) на него не наложен — у
// процесса, чья работа `goose up`, нет ни одного из этих предметов, и отказ по
// ним называл бы оператору не тот. Страж службы при этом не ослаблен ни на
// байт: он стоит там же, где стоял, — за `config.Load`, которой поднимается
// `kacho-loadbalancer`. Разбор конфигурации у обеих дверей ОДИН.
//
// Флаг `--config` есть ТОЛЬКО у этого сервиса, и это решение, а не остаток:
// nlb читает конфигурацию из смонтированного файла, шесть соседей — из
// окружения. Различие названо в docs/architecture/migrator-cli.md.
package main

import (
	"io/fs"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует "pgx" driver для sql.Open
	"github.com/spf13/cobra"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli/cobraargs"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/pkg/migratorrun"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

const (
	defaultDialect       = "postgres"
	defaultMigrationsDir = "."
)

// serviceName — чью цепочку применяет эта точка наката. Живой счёт строк перед
// сносом называет им то, что стережёт, поэтому безымянной точки наката не
// бывает: предусловия отказывают ей в старте.
const serviceName = "nlb"

// dsnExtraSources — чем ЭТА служба заполняет DSN СВЕРХ двух общих (`--dsn` и
// KACHO_MIGRATOR_DSN), в порядке убывания приоритета. Два общих здесь НЕ
// перечисляются намеренно: их печатает сам общий пакет, поэтому умолчать
// источник, который перебивает названные, нельзя by construction. Ровно это и
// случилось однажды — текст отказа называл третий источник и умалчивал второй.
var dsnExtraSources = []string{"KACHO_NLB_REPOSITORY__POSTGRES__URL", "config repository.postgres.url (--config)"}

// rootOptions — shared параметры всех subcommand'ов (persistent flags).
type rootOptions struct {
	dialect    string
	dsn        string
	configPath string
}

func main() {
	if err := newRootCmd(migrations.FS).Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd собирает дерево команд. Вынесено в отдельный конструктор,
// чтобы main_test.go мог инстанцировать без os.Exit. migrationsFS принимается
// параметром: в production — `internal/migrations.FS`, в тестах — пустая
// `fstest.MapFS{}` (проверяем только парсинг args).
func newRootCmd(migrationsFS fs.FS) *cobra.Command {
	opts := &rootOptions{}

	root := &cobra.Command{
		Use:   "kacho-migrator",
		Short: "Управление миграциями БД сервиса kacho-nlb",
		Long: "kacho-migrator — отдельный CLI для управления миграциями БД сервиса kacho-nlb:\n" +
			"применение (up), откат (down), статус (status).\n\n" +
			"Новая миграция заводится РУКОЙ: internal/migrations/YYYYMMDDHHMMSS_<что>.sql\n" +
			"(метка времени заведения: date -u +%Y%m%d%H%M%S).\n" +
			"Подробности — docs/architecture/migration-version-namespace.md.",
		SilenceUsage: true,
		// Пустая командная строка — ОТКАЗ, а не успех (#1461). Cobra при корне без
		// исполнения печатает помощь и выходит успехом; прямая форма отвечает
		// отказом. Скрипт или init-контейнер, потерявший аргумент, объявлялся бы
		// выполнившим накат — успех на невыполненной работе.
		//
		// Отказ по неизвестной подкоманде производит общий пакет — тем же текстом,
		// что и прямая форма, и с перечнем известных подкоманд.
		Args: cobraargs.OnlyKnownCommands,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			// Сентинел общий: своя редакция того же текста разошлась бы молча.
			return migratorcli.ErrNoCommand
		},
	}
	root.PersistentFlags().StringVar(&opts.dialect, "dialect", defaultDialect,
		"SQL dialect (postgres)")
	root.PersistentFlags().StringVar(&opts.dsn, "dsn", "",
		"database DSN; if empty — read ENV "+migratorcli.EnvDSN+", then config.yaml")
	root.PersistentFlags().StringVar(&opts.configPath, "config", "",
		"path to kacho-nlb config.yaml (fallback DSN source)")

	root.AddCommand(
		newUpCmd(opts, migrationsFS),
		newDownCmd(opts, migrationsFS),
		newStatusCmd(opts, migrationsFS),
	)
	// Дополнения оболочки cobra доводит сама; у прямой формы такой команды нет и
	// не будет. Перечень команд читает оператор — значит он тоже поверхность.
	cobraargs.HideShellCompletion(root)
	return root
}

func newUpCmd(opts *rootOptions, migrationsFS fs.FS) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use: "up",
		// Лишний позиционный аргумент — отказ, а не молчаливый накат: у cobra
		// умолчание Args принимает произвольные, и `up 800001` (догадка о том, как
		// задать цель) уезжал накатывать до головы. Версия задаётся --target (#1461).
		// Текст отказа производит общий пакет — один на семь.
		Args:  cobraargs.NoExtraArguments,
		Short: "Apply migrations up to latest (or --target version)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(opts, migrationsFS)
			if err != nil {
				return err
			}
			return r.Up(cmd.Context(), target)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "stop at this version (inclusive); default — latest")
	return cmd
}

func newDownCmd(opts *rootOptions, migrationsFS fs.FS) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use: "down",
		// Лишний позиционный аргумент — отказ, а не молчаливый накат: у cobra
		// умолчание Args принимает произвольные, и `up 800001` (догадка о том, как
		// задать цель) уезжал накатывать до головы. Версия задаётся --target (#1461).
		// Текст отказа производит общий пакет — один на семь.
		Args:  cobraargs.NoExtraArguments,
		Short: "Rollback the most recent migration (or down to --target)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(opts, migrationsFS)
			if err != nil {
				return err
			}
			return r.Down(cmd.Context(), target)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "rollback down to this version (inclusive); default — one step back")
	return cmd
}

func newStatusCmd(opts *rootOptions, migrationsFS fs.FS) *cobra.Command {
	return &cobra.Command{
		Use: "status",
		// Лишний позиционный аргумент — отказ, а не молчаливый накат: у cobra
		// умолчание Args принимает произвольные, и `up 800001` (догадка о том, как
		// задать цель) уезжал накатывать до головы. Версия задаётся --target (#1461).
		// Текст отказа производит общий пакет — один на семь.
		Args:  cobraargs.NoExtraArguments,
		Short: "Show migration status (applied / pending)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(opts, migrationsFS)
			if err != nil {
				return err
			}
			return r.Status(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// buildRunner собирает накат из persistent-флагов + ENV + config-fallback.
//
// Приоритет DSN один на семь сервисов и живёт в общем пакете:
// --dsn > ENV KACHO_MIGRATOR_DSN > конфигурация сервиса (здесь —
// `config.MigrateDSN`, она же читает `KACHO_NLB_REPOSITORY__POSTGRES__URL` и
// смонтированный `--config`). Своей редакции порядка тут быть не должно: две
// редакции об одном предмете расходятся молча — и разошлись, общей переменной
// nlb не читал вовсе.
//
// Третий источник читает дверь ТОЧКИ НАКАТА, а не дверь службы: посадка
// процесса, который служит, накат не связывает, а шифрование до собственной
// базы — связывает. Границу называет сама `config.MigrateDSN`.
func buildRunner(opts *rootOptions, migrationsFS fs.FS) (*migratorrun.Runner, error) {
	// ДИАЛЕКТ СВЕРЯЕТСЯ ПЕРВЫМ, и это порядок, а не стиль. Общий накат сверяет
	// его тоже — но уже приняв DSN, а до DSN лежит загрузка конфигурации службы.
	// Оператор, назвавший несуществующий диалект, получал бы тогда отказ
	// КОНФИГУРАЦИИ: длинный, про совсем другое и не называющий причины, по
	// которой запуск отвергнут. У прямой четвёрки этот порядок держит сам разбор
	// (migratorcli.Parse отвергает диалект до всего прочего); cobra такой
	// проверки не делает, поэтому здесь она стоит явно. Функция та же — двух
	// редакций одного текста не заводится.
	if _, err := migratorcli.ResolveDialectSpec(opts.dialect); err != nil {
		return nil, err
	}

	// Запасная конфигурация читается ДВЕРЬЮ ТОЧКИ НАКАТА, а не дверью службы.
	// Разбор у них один; различается то, ЧТО каждая спрашивает: накат — адрес
	// своей базы и шифрование до неё, служба — свою посадку целиком. Почему
	// именно так и что при этом НЕ снимается — `config.MigrateDSN`.
	dsn, err := migratorcli.ResolveDSN(opts.dsn, func() (string, error) {
		return config.MigrateDSN(opts.configPath)
	})
	if err != nil {
		return nil, err
	}

	return migratorrun.New(migratorrun.Config{
		Service:         serviceName,
		Dialect:         opts.dialect,
		DSN:             dsn,
		FS:              migrationsFS,
		MigrationsDir:   defaultMigrationsDir,
		DSNExtraSources: dsnExtraSources,
	})
}
