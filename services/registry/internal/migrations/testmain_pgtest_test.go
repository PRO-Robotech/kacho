// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrations_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"
)

// TestMain даёт пакету один Postgres на все пробы вместо контейнера на пробу.
//
// Migrate не задан: каждая проба журнала берёт собственную ПУСТУЮ базу и сама
// применяет цепочку registry до головы (`openJournalProbeDB`) — так же, как пакеты
// миграций compute, vpc, nlb и storage. Контейнер поднимается лениво: под -short,
// где пробы пропускаются, его нет.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{Name: "registryjournal"}))
}
