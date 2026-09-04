// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
)

// TestMain даёт пакету одну базу с ПОЛНОЙ цепочкой миграций vpc.
//
// Цепочка проигрывается целиком, а не подставляется вручную собранной таблицей:
// предмет проб — что объявление журнала сходится с ФАКТИЧЕСКОЙ схемой сервиса, а
// своя копия схемы разошлась бы с ней молча — именно там, где расхождение и
// опасно. Имена колонок живут в объявлении ЗНАЧЕНИЯМИ, и ошибка в них наступает
// первым запросом в бою, а не сборкой.
//
// Контейнер поднимается лениво, поэтому под `-short`, где эти пробы пропускают
// себя, он не стоит ничего.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:    "vpc_subscription",
		Migrate: pgtest.Goose(migrations.FS),
	}))
}
