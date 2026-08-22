// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package main — отдельный binary `kacho-migrator`: CLI управления миграциями
// схемы БД compute (goose поверх embed `internal/migrations`).
//
//	kacho-migrator up      # прокатить все pending-миграции
//	kacho-migrator down    # откатить последнюю миграцию
//	kacho-migrator status  # показать применённые/pending
//
// Отдельная точка сборки (зеркалит kacho-vpc / kacho-iam): serve-binary
// `kacho-compute` больше НЕ несёт embed-миграции и деструктивный `migrate down`
// (least-privilege — runtime-образ не может менять схему live-БД). Миграции
// гоняет отдельный one-shot init-container/Job с этим бинарём.
//
// DSN берётся из того же config.Load() (viper/env), что и serve — одно
// helm-values задаёт БД-параметры для обоих бинарей.
package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // регистрирует "pgx" driver для sql.Open
	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/pkg/dbready"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: kacho-migrator {up|down|status}")
	}
	direction := os.Args[1]

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("goose dialect: %v", err)
	}
	db, err := sql.Open("pgx", cfg.MigrateDSN())
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

	var gooseErr error
	switch direction {
	case "up":
		// ПРОПУЩЕННЫЕ МИГРАЦИИ ПРИНИМАЮТСЯ, и это не послабление, а следствие схемы
		// нумерации. Номер у нас — «задача × 1000 + порядок», и он НЕ хронологичен by
		// construction: задача закрывается не по порядку номеров, и файл `708001` появляется в
		// дереве позже, чем `800001`. База, накатившая больший номер раньше, при
		// обновлении видит «пропущенную миграцию перед текущей версией» и отказывает —
		// служба не стартует вовсе.
		//
		// Замер на момент правки: таких пар в дереве 22, во ВСЕХ семи сервисах.
		// Конвейер их не видит by construction — он всегда поднимает чистую базу, где
		// пропущенных нет; воспроизводится только на обновлении развёрнутой.
		//
		// Приём пропущенной означает ПРИМЕНИТЬ её, а не пропустить; порядок внутри
		// одной задачи (`NNN001` до `NNN002`) goose сохраняет независимо от опции.
		gooseErr = goose.Up(db, ".", goose.WithAllowMissing())
	case "down":
		gooseErr = goose.Down(db, ".")
	case "status":
		gooseErr = goose.Status(db, ".")
	default:
		log.Fatalf("unknown command %q (usage: kacho-migrator {up|down|status})", direction)
	}
	if gooseErr != nil {
		log.Fatalf("migrate %s: %v", direction, gooseErr)
	}
}
