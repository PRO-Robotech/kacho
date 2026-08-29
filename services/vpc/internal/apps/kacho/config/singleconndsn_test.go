// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"
	"testing"
)

// TestSingleConnDSNCarriesNoPoolParameter — строка ОДИНОЧНОГО соединения не несёт
// параметров пула.
//
// Предмет измеряется, а не подразумевается: `pool_max_conns` не является
// libpq-параметром, поэтому разбор строки его НЕ отвергает — он уезжает серверу
// runtime-параметром в стартовом пакете, и Postgres отвечает FATAL уже на
// подключении. Со стороны это тихо: процесс поднят, глагол выставлен, а каждая
// подписка отвечает «источник недоступен» и никогда ничем иным.
func TestSingleConnDSNCarriesNoPoolParameter(t *testing.T) {
	cfg := Config{}
	cfg.Repository.Postgres.URL = "postgres://u:p@h:5432/kacho_vpc"
	cfg.Repository.Postgres.SSLMode = "require"
	cfg.Repository.Postgres.MaxConns = 10

	// Проверка СОБСТВЕННОЙ предпосылки: утверждение ниже осмысленно ровно пока
	// пуловая строка этот параметр действительно несёт.
	if pooled := cfg.DSN(); !strings.Contains(pooled, "pool_max_conns") {
		t.Fatalf("предпосылка не выполнена: пуловая строка больше не несёт pool_max_conns (%q) — "+
			"утверждение ниже стало бы вакуумным", pooled)
	}

	single := cfg.SingleConnDSN()
	if single == "" {
		t.Fatal("строка одиночного соединения пуста: подписка не подключится вовсе")
	}
	if strings.Contains(single, "pool_") {
		t.Fatalf("строка одиночного соединения несёт параметр пула: %q — "+
			"вне пула это неизвестный PG-параметр и FATAL при подключении", single)
	}
}
