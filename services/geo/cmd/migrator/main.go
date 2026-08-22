// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Command kacho-migrator — раннер DB-миграций kacho-geo (схема kacho_geo).
// Отдельный бинарь от API-сервера; запускается deploy-init-контейнером до старта
// основного pod.
//
//	kacho-migrator up|down|status
//
// DSN: флаг --dsn, иначе KACHO_MIGRATOR_DSN, иначе config kacho-geo (KACHO_GEO_*).
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует database/sql-драйвер "pgx"
	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/dbready"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/geo/internal/migrations"
)

func main() {
	dsnFlag := flag.String("dsn", "", "database DSN (else KACHO_MIGRATOR_DSN, else KACHO_GEO_* config)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		log.Fatal("usage: kacho-migrator [--dsn <dsn>] {up|down|status}")
	}
	direction := args[0]

	dsn := resolveDSN(*dsnFlag)

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("goose dialect: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

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

	var gerr error
	switch direction {
	case "up":
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
		gerr = goose.Up(db, ".", goose.WithAllowMissing())
	case "down":
		gerr = goose.Down(db, ".")
	case "status":
		gerr = goose.Status(db, ".")
	default:
		log.Fatalf("unknown migrate direction: %s (up|down|status)", direction)
	}
	if gerr != nil {
		log.Fatalf("migrate %s: %v", direction, gerr)
	}
}

// resolveDSN выбирает DSN: флаг --dsn > env KACHO_MIGRATOR_DSN > config kacho-geo.
func resolveDSN(flagDSN string) string {
	if flagDSN != "" {
		return flagDSN
	}
	if env := os.Getenv("KACHO_MIGRATOR_DSN"); env != "" {
		return env
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config (for DSN): %v", err)
	}
	return cfg.MigrateDSN()
}
