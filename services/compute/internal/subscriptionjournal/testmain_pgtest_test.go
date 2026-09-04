// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
)

// TestMain даёт пакету одну базу с ПОЛНОЙ цепочкой миграций compute.
//
// Цепочка проигрывается целиком, а не подставляется вручную собранной таблицей:
// предмет проб — что объявление журнала сходится с ФАКТИЧЕСКОЙ схемой сервиса, и
// своя копия схемы разошлась бы с ней молча — именно там, где расхождение и
// опасно.
//
// Контейнер поднимается лениво, поэтому под `-short`, где эти пробы пропускают
// себя, он не стоит ничего.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:     "compute_subscription",
		User:     "compute",
		Password: "compute",
		Migrate:  pgtest.Goose(migrations.FS),
	}))
}
