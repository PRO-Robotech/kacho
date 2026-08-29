// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import "strings"

import "testing"

// TestSingleConnDSNCarriesNoPoolParameter — строка ОДИНОЧНОГО соединения не несёт
// параметров пула.
//
// Предмет измеряется, а не подразумевается: `pool_max_conns` не является
// libpq-параметром, поэтому разбор строки его НЕ отвергает — он уезжает серверу
// runtime-параметром в стартовом пакете, и Postgres отвечает FATAL уже на
// подключении. Со стороны это тихо: процесс поднят, глагол выставлен, а каждая
// подписка отвечает «источник недоступен» и никогда ничем иным.
func TestSingleConnDSNCarriesNoPoolParameter(t *testing.T) {
	cfg := Config{
		DBHost: "h", DBPort: "5432", DBUser: "u", DBPassword: "p",
		DBName: "kacho_compute", DBSSLMode: "require", DBMaxConns: 10,
	}

	// Проверка СОБСТВЕННОЙ предпосылки: утверждение ниже осмысленно ровно пока
	// пуловая строка этот параметр действительно несёт. Перестанет нести —
	// проба обязана сказать это, а не молча зеленеть.
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
