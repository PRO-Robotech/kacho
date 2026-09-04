// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package idempotencypg_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestMain выдаёт пакету ОДИН Postgres на все пробы (санкционированный
// поставщик: сервер один, база у каждой пробы своя).
//
// Migrate нет намеренно: схему накатывает САМО хранилище при построении, и это
// часть предмета проб — реплика, стартовавшая на пустой базе, обязана прийти в
// рабочее состояние сама. Проба, получившая заранее мигрированную базу, об этом
// свойстве не сказала бы ничего.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{Name: "gwidem"}))
}
