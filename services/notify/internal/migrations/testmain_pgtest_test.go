// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/migrations"
)

// TestMain выдаёт пакету один Postgres; шаблон базы — та самая цепочка, которую
// встраивает точка наката (строка kacho_notify в chains.yaml). Пробы схемы
// получают клон шаблона (NewDB), проба наката — пустую базу (NewEmptyDB).
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "notifymig",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
