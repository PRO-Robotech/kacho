// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/migrations"
)

// TestMain выдаёт пакету один Postgres: база шлюза — ТЕМИ миграциями, которые
// встроены в точку наката (цепочка строки kacho_notify в chains.yaml), а не
// своей схемой пробы. Пробы без базы (ведро, пауза, инвариант сетки) контейнера
// не поднимают: pgtest поднимает его на первом NewDB.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "notify",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
