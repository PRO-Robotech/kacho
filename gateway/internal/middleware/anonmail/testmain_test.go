// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"
)

// TestMain выдаёт пакету один Postgres на пробы хранилища postgres (сервер
// один, база у каждой пробы своя). Migrate нет намеренно: схему накатывает
// хранилище однократности края при построении (`idempotencypg.New`) — так же,
// как в корне края, где пул ограничителя строится ПОСЛЕ него.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{Name: "gwanon"}))
}
