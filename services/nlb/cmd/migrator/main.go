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
// (config.Load, он же читает `KACHO_NLB_REPOSITORY__POSTGRES__URL`). Так одна и
// та же `values.yaml` покрывает оба бинаря (kacho-loadbalancer + kacho-migrator)
// без дублирования.
//
// Флаг `--config` есть ТОЛЬКО у этого сервиса, и это решение, а не остаток:
// nlb читает конфигурацию из смонтированного файла, шесть соседей — из
// окружения. Различие названо в docs/architecture/migrator-cli.md.
package main

import (
	"io/fs"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует "pgx" driver для sql.Open
	"github.com/spf13/cobra"

	"github.com/PRO-Robotech/kacho/pkg/migratorcli"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/migrator"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

const (
	defaultDialect       = "postgres"
	defaultMigrationsDir = "."
)

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
	return root
}

func newUpCmd(opts *rootOptions, migrationsFS fs.FS) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use: "up",
		// Лишний позиционный аргумент — отказ, а не молчаливый накат: у cobra
		// умолчание Args принимает произвольные, и `up 800001` (догадка о том, как
		// задать цель) уезжал накатывать до головы. Версия задаётся --target (#1461).
		Args:  cobra.NoArgs,
		Short: "Apply migrations up to latest (or --target version)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(opts, migrationsFS)
			if err != nil {
				return err
			}
			return r.Up(target)
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
		Args:  cobra.NoArgs,
		Short: "Rollback the most recent migration (or down to --target)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(opts, migrationsFS)
			if err != nil {
				return err
			}
			return r.Down(target)
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
		Args:  cobra.NoArgs,
		Short: "Show migration status (applied / pending)",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := buildRunner(opts, migrationsFS)
			if err != nil {
				return err
			}
			return r.Status(cmd.OutOrStdout())
		},
	}
}

// buildRunner собирает migrator.Runner из persistent-флагов + ENV + config-fallback.
//
// Приоритет DSN один на семь сервисов и живёт в общем пакете:
// --dsn > ENV KACHO_MIGRATOR_DSN > конфигурация сервиса (здесь — config.Load,
// который сам читает `KACHO_NLB_REPOSITORY__POSTGRES__URL` и смонтированный
// `--config`). Своей редакции порядка тут быть не должно: две редакции об одном
// предмете расходятся молча — и разошлись, общей переменной nlb не читал вовсе.
func buildRunner(opts *rootOptions, migrationsFS fs.FS) (*migrator.Runner, error) {
	dialect, err := migrator.ResolveDialect(opts.dialect)
	if err != nil {
		return nil, err
	}

	dsn, err := migratorcli.ResolveDSN(opts.dsn, func() (string, error) {
		cfg, cerr := config.Load(opts.configPath)
		if cerr != nil {
			return "", cerr
		}
		return strings.TrimSpace(cfg.Repository.Postgres.URL), nil
	})
	if err != nil {
		return nil, err
	}

	return migrator.New(migrator.Config{
		Dialect:       dialect,
		DSN:           dsn,
		FS:            migrationsFS,
		MigrationsDir: defaultMigrationsDir,
	})
}
