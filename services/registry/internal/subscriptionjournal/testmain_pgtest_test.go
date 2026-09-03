// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// TestMain даёт пакету одну базу с ПОЛНОЙ цепочкой миграций реестра.
//
// Цепочка проигрывается целиком, а не подставляется вручную собранной таблицей:
// предмет проб — что объявление журнала сходится с ФАКТИЧЕСКОЙ схемой сервиса, а
// своя копия схемы разошлась бы с ней молча — именно там, где расхождение и
// опасно. Тем же прогоном проверяется, что триггеры эмиссии действительно висят
// на таблице реестров: на подставной схеме их не было бы вовсе.
//
// Контейнер поднимается лениво, поэтому под `-short`, где эти пробы пропускают
// себя, он не стоит ничего.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		// Приведение схемы — ОДИН раз на пакет, у выдающего базу.
		// Прежде его приписывал каждый вызывающий своей копией; забывший
		// получал `relation … does not exist` — отказ, читающийся как дефект
		// продукта. Довод целиком — `internal/pgtest` §WithSearchPath.
		SearchPath: "kacho_registry,public",
		Name:       "registry_subscription",
		User:       "registry",
		Password:   "registry",
		Migrate:    pgtest.Goose(migrations.FS),
	}))
}
