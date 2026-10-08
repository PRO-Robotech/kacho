// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/migrations"
)

// TestMain выдаёт пакету один Postgres; шаблон базы — та цепочка kacho_notify,
// которую встраивает точка наката (chains.yaml), а не своя схема пробы:
// notify-api и notify-sender работают над одной базой (замысел З1).
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "notifyapi",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
