// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package migrator — бизнес-логика отдельного бинаря cmd/migrator: обертка над
// goose, которую дергает cmd/migrator/main.go (cobra). Вынесена в свой бинарь,
// чтобы миграции не были subcommand'ом основного сервиса.
//
// # Dialect
//
// Per-dialect логика инкапсулирована в [Dialect]-интерфейсе (`dialect.go` +
// `postgres.go`); Runner — тонкая обертка, которая:
//   - валидирует Config до обращения к goose (friendly-error на FS==nil и т.п. —
//     иначе goose упадет где-нибудь в недрах с малопонятным сообщением);
//   - проксирует Up/Down/Status на Dialect-impl.
//
// Это позволяет добавлять новые диалекты без if-ветвей в Runner.
//
// Embed FS (`internal/migrations/*.sql`) принимается параметром Config.FS, чтобы
// runner не тянул прямой импорт `internal/migrations`: зависимость
// одно-направленная (cmd/migrator → internal/apps/migrator + internal/migrations),
// а сам `internal/apps/migrator` ни к чему vpc-specific не привязан.
package migrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/PRO-Robotech/kacho/internal/dropguard"
)

// Config — параметры одного запуска runner'а. Заполняется cmd/migrator/main.go
// из cobra-флагов / ENV / viper-config.
type Config struct {
	// Service — имя сервиса, чью цепочку применяет этот runner. Обязательное:
	// без него живой счёт строк перед сносом не может назвать, что он стережёт,
	// а безымянный отказ в логе init-контейнера некому адресовать.
	Service string
	// Dialect — резолвленный диалект (через [NewDialect]).
	Dialect Dialect
	// DSN — строка подключения для sql.Open(<driver>, DSN).
	// Тот же DSN, что использует kacho-vpc для pgxpool (без pool_max_conns
	// — sql.Open его не понимает, см. config.MigrateDSN). У cobra-front'а
	// поле читается из --dsn / ENV KACHO_MIGRATOR_DSN.
	DSN string
	// FS — embed.FS с .sql миграциями (передается cmd/migrator/main.go из
	// internal/migrations.FS). Так runner не зависит напрямую от vpc-specific
	// пакета и переиспользуется любой сервис, если будет нужно.
	FS fs.FS
	// MigrationsDir — путь внутри FS, где лежат .sql файлы. Для embed.FS
	// корня — ".".
	MigrationsDir string
}

// Validate проверяет минимально необходимые поля перед обращением к диалекту.
func (c Config) Validate() error {
	if c.Service == "" {
		return errors.New("service is empty (the live drop preflight would have nothing to name)")
	}
	if c.Dialect == nil {
		return errors.New("dialect is not set")
	}
	if c.Dialect.Spec().Name == "" {
		return errors.New("dialect spec.Name is empty")
	}
	if c.DSN == "" {
		return errors.New("dsn is empty (set --dsn or KACHO_MIGRATOR_DSN)")
	}
	if c.FS == nil {
		return errors.New("migrations FS is nil")
	}
	if c.MigrationsDir == "" {
		return errors.New("migrations dir is empty")
	}
	return nil
}

// Runner — высокоуровневая обертка над [Dialect]. Один экземпляр на жизнь
// процесса, методы безопасны для последовательных вызовов; concurrent
// использование не предполагается (cobra гоняет одну команду за раз).
type Runner struct {
	cfg Config
}

// New собирает Runner; cfg валидируется здесь же.
func New(cfg Config) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Runner{cfg: cfg}, nil
}

// Up прогоняет миграции вверх — сперва СОСЧИТАВ строки в таблицах, которые
// уронят ещё не применённые миграции (см. preflightDrops), и лишь затем
// делегируя в Dialect-impl. Ненулевой счёт прекращает применение.
func (r *Runner) Up(target string) error {
	ctx := context.Background()
	// СЧЁТ ПЕРЕД СНОСОМ — здесь, а не в вызывающем, и это единственное место.
	// Шаг, который зовут отдельной строкой, однажды не позовут; отсюда его не
	// обойти, не обойдя сам Up. Ненулевой счёт возвращает ошибку — миграции не
	// применяются вовсе.
	if err := r.preflightDrops(ctx); err != nil {
		return err
	}
	return r.cfg.Dialect.Up(ctx, r.cfg.DSN, r.cfg.FS, r.cfg.MigrationsDir, target)
}

// preflightDrops считает на ЖИВОЙ базе строки в каждой таблице, которую уронит
// ещё не применённая миграция.
//
// Измеряющий гейт в internal/migrations отвечает на другой вопрос — сколько сеет
// наша собственная цепочка, проигранная в пустую базу. Что записал арендатор, не
// знает ни один контейнер; узнать это можно только здесь и только сейчас, пока
// строки ещё существуют.
//
// Соединение своё и закрывается сразу: держать его до конца применения незачем, а
// dbready внутри openPgxDB заодно даёт барьер готовности до первого вопроса.
func (r *Runner) preflightDrops(ctx context.Context) error {
	db, err := openPgxDB(ctx, r.cfg.DSN, r.cfg.Dialect.Spec())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return dropguard.Gate(ctx, db, r.cfg.Service, r.cfg.FS, os.Stderr)
}

// Down откатывает миграции. Делегирует в Dialect-impl.
func (r *Runner) Down(target string) error {
	return r.cfg.Dialect.Down(context.Background(), r.cfg.DSN, r.cfg.FS, r.cfg.MigrationsDir, target)
}

// Status печатает примененные/непримененные миграции.
func (r *Runner) Status(out io.Writer) error {
	return r.cfg.Dialect.Status(context.Background(), r.cfg.DSN, r.cfg.FS, r.cfg.MigrationsDir, out)
}

// parseTargetVersion — goose использует int64 для версии (timestamp или
// 4-digit prefix файла). Принимаем строку с CLI, чтобы пользователь мог
// написать "0010" как в имени файла; конвертация — fmt.Sscanf вместо
// strconv.ParseInt для устойчивости к leading zeros.
//
// Helper используется postgres.go.
func parseTargetVersion(s string) (int64, error) {
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0, fmt.Errorf("parse target version %q: %w", s, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("target version must be non-negative, got %d", v)
	}
	return v, nil
}
