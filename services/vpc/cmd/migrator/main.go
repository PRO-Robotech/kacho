// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — отдельный binary `kacho-migrator`: CLI управления миграциями БД.
// `cmd/vpc/main.go` обслуживает только `serve`, миграции вынесены сюда отдельной
// точкой сборки. API в стиле `goose`:
//
//	kacho-migrator up [--target <version>]
//	kacho-migrator down [--target <version>]
//	kacho-migrator status
//
// # Глагола `create` здесь НЕТ — и это решение, а не пропуск (#566)
//
// Он был и выдавал `goose.Create` без `SetSequential`, то есть имя с 14-значной
// меткой времени (`20260817042704_имя.sql`). Файлов такой формы в дереве ноль:
// глаголом ни разу не пользовались, а если бы воспользовались — гейт
// пространства номеров отверг бы результат, потому что метка времени превышает
// номер всякой возможной задачи на три порядка.
//
// Новая миграция называется рукой: `<задача><порядковый:3>_<что_делает>.sql`
// (например `539001_...`), номер задачи — тот же, что в имени ветки. Выводить
// его инструментом нечем и незачем: он уже известен автору, а установленный
// инструмент работает ровно у тех, кто его установил. Разбор —
// docs/architecture/migration-version-namespace.md.
//
// Флаги верхнего уровня:
//
//	--dialect postgres                    (default; продукт Postgres-only)
//	--dsn     <connection-string>         (или ENV KACHO_MIGRATOR_DSN)
//
// Помимо ENV KACHO_MIGRATOR_DSN, для удобства dev-стенда (тот же набор переменных,
// что и у kacho-vpc) поддерживается fallback: если --dsn пуст и
// KACHO_MIGRATOR_DSN пуст, читаем `config.Load()` (viper/YAML-config) и берем
// `cfg.MigrateDSN()`. Так одно helm-values задает БД-параметры для обоих binary,
// не дублируя DSN. Явно переданный --dsn перекрывает vpc-config.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует "pgx" driver для sql.Open
	"github.com/spf13/cobra"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/migrator"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
)

const (
	defaultDialect       = "postgres"
	defaultMigrationsDir = "."
	envDSN               = "KACHO_MIGRATOR_DSN"
)

// rootOptions — shared параметры всех subcommand'ов, накапливаются persistent-флагами.
type rootOptions struct {
	dialect string
	dsn     string
}

func main() {
	if err := newRootCmd(migrations.FS).Execute(); err != nil {
		// cobra сам печатает текст ошибки + usage; нам остается только exit-code.
		// Не пишем еще раз — было бы дублирование.
		os.Exit(1)
	}
}

// newRootCmd собирает дерево команд. Вынесено в отдельный конструктор,
// чтобы main_test.go мог инстанцировать и парсить args без os.Exit.
// migrationsFS принимается параметром: в production — `internal/migrations.FS`,
// в тестах — пустая `fstest.MapFS{}` (нам важно проверить парсинг args).
func newRootCmd(migrationsFS fs.FS) *cobra.Command {
	opts := &rootOptions{}

	root := &cobra.Command{
		Use:   "kacho-migrator",
		Short: "Управление миграциями БД сервиса kacho-vpc",
		Long: "kacho-migrator — отдельный CLI для управления миграциями БД сервиса kacho-vpc:\n" +
			"применение (up), откат (down), статус (status).\n\n" +
			"Новая миграция заводится РУКОЙ: internal/migrations/<задача><порядковый:3>_<что>.sql\n" +
			"(например 539001_add_route_table.sql; номер задачи — тот же, что в имени ветки).\n" +
			"Подробности — docs/architecture/migration-version-namespace.md.",
		SilenceUsage: true, // не показывать usage на runtime-ошибках (только на parse-ошибках)
	}
	root.PersistentFlags().StringVar(&opts.dialect, "dialect", defaultDialect,
		"SQL dialect (postgres)")
	root.PersistentFlags().StringVar(&opts.dsn, "dsn", "",
		"database DSN; if empty — read ENV "+envDSN+", then fall back to kacho-vpc config (envconfig)")

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
		Use:   "up",
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
		Use:   "down",
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
		Use:   "status",
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
// Источник DSN — приоритет: --dsn flag > ENV KACHO_MIGRATOR_DSN > viper/YAML-config
// (config.Load → cfg.MigrateDSN). Так одно helm-values покрывает оба binary,
// и можно явно перекрыть `--dsn` для cross-DB-инструментов и ad-hoc запусков.
func buildRunner(opts *rootOptions, migrationsFS fs.FS) (*migrator.Runner, error) {
	dialect, err := migrator.NewDialect(opts.dialect)
	if err != nil {
		return nil, err
	}

	dsn := strings.TrimSpace(opts.dsn)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv(envDSN))
	}
	if dsn == "" {
		// Fallback к vpc-config: тот же набор DB-параметров (host/port/user/name/sslmode).
		// config.Load() возвращает Config без ошибки даже без пароля — пароль
		// подставляется через repository.postgres.password-from-env либо напрямую в DSN;
		// здесь fallback фейлится лишь при реальной ошибке загрузки config.
		cfg, cerr := config.Load(os.Getenv("KACHO_VPC_CONFIG_PATH"))
		if cerr != nil {
			return nil, fmt.Errorf("dsn unset (--dsn / %s) and vpc config load failed: %w", envDSN, cerr)
		}
		dsn = cfg.MigrateDSN()
	}

	return migrator.New(migrator.Config{
		Dialect:       dialect,
		DSN:           dsn,
		FS:            migrationsFS,
		MigrationsDir: defaultMigrationsDir,
	})
}
