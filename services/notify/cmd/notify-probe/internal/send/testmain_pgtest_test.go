// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package send_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/probemigrations"
)

// TestMain выдаёт пакету один Postgres с миграциями, встроенными в бинарь пробы.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "notifyprobesend",
		Migrate: pgtest.Goose(probemigrations.FS),
	}))
}
