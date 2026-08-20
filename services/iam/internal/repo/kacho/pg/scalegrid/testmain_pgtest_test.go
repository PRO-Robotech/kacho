// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package scalegrid_test

// TestMain выдаёт пакету ОДИН Postgres на весь тестовый бинарь; каждая проба
// получает свою базу, клонированную из шаблона, промигрированного один раз.
//
// Заведён затем, что предмет проб этого пакета — ПЛАН, которым прибор читает
// таблицы iam. План нельзя утверждать ни по тексту запроса, ни по числу,
// которое запрос вернул: и то и другое одинаково у формы, читающей таблицу
// целиком, и у формы, идущей указателем по индексу. Спросить его можно только
// у настоящей базы с настоящей схемой.

import (
	"testing"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	"github.com/PRO-Robotech/kacho/services/iam/internal/migrations"
)

func TestMain(m *testing.M) {
	pgtest.Run(m, pgtest.Config{
		Name:    "iam",
		Migrate: pgtest.Goose(migrations.FS),
	})
}
