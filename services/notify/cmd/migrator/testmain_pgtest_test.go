// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"
)

// TestMain выдаёт пакету один Postgres для пробы точки на живой базе. Базы
// пробы пустые (pgtest.NewEmptyDB): накат цепочки пробы делает сама точка.
// Процесс-помощник (TestHelperRunsMain) контейнера не поднимает — базу ему
// называет DSN родителя.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{Name: "notifymigrator"}))
}
