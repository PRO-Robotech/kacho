// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/migrations"
)

// TestMain даёт пакету одну базу с ПОЛНОЙ цепочкой миграций storage.
//
// Цепочка проигрывается целиком, а не подставляется вручную собранной таблицей:
// предмет проб — что объявление журнала сходится с ФАКТИЧЕСКОЙ схемой сервиса, и
// своя копия схемы разошлась бы с ней молча — именно там, где расхождение и
// опасно. Здесь это вдвойне несущее: строку журнала пишет ТРИГГЕР, то есть
// производитель живёт в самой цепочке, а не в Go.
//
// Контейнер поднимается лениво, поэтому под `-short`, где эти пробы пропускают
// себя, он не стоит ничего.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §WithSearchPath.
		SearchPath: "kacho_storage,public",
		Name:       "storage_subscription",
		User:       "storage",
		Password:   "storage",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
