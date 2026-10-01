// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/migrations"
)

// TestMain выдаёт пакету один Postgres: база пробы — ТЕМИ миграциями, которые
// встроены в бинарь (журнал подписки и лента, записанная notifygen init), а не
// своей схемой пробы.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "notifyprobe",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
