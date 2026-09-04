// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migratorapply_test

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestMain выдаёт пакету ОДИН Postgres на все семь точек наката.
//
// Шаблон намеренно пуст (`Migrate` не задан): предмет доказательства — накат,
// поэтому каждая точка получает ПУСТУЮ базу и накатывает цепочку сама. Заполнить
// шаблон чужим накатом значило бы доказывать на уже накатанном — то есть не
// доказывать ничего.
//
// Контейнер поднимается лениво, поэтому прогон под кратким режимом, где все пробы
// пропускаются, не платит за него ничего.
func TestMain(m *testing.M) {
	os.Exit(pgtest.Run(m, pgtest.Config{Name: "migratorapply"}))
}
