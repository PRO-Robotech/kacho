// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/migrations"
)

// TestMain даёт пакету одну базу с ПОЛНОЙ цепочкой миграций nlb.
//
// Цепочка проигрывается целиком, а не подставляется собранной руками таблицей:
// предмет проб — что объявление журнала сходится с ФАКТИЧЕСКОЙ схемой сервиса.
// Своя копия схемы разошлась бы с ней молча — и разошлась бы именно в именах
// колонок, которые у nlb СВОИ (`resource_type`, `action`), то есть там, где
// ошибка наступает первым запросом в бою, а не сборкой.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{
		Name:     "nlb_subscription",
		User:     "nlb",
		Password: "nlb",
		Migrate:  pgtest.Goose(migrations.FS),
	}))
}
