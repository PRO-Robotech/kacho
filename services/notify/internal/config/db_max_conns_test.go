// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// db_max_conns_test.go — ширина пула базы notify объявлена ручкой, а не
// умолчанием драйвера (NTF-1, полоса D6; гейт развёртывания
// deploy/pool_fits_database_test.go: «пул × реплики ≤ max_connections» судит
// только объявленную ширину). Умолчание pgx — max(4, число ядер УЗЛА): его
// никто не выбирал, и оно меняется от смены узла без единой правки дерева.
//
// Ручка `notify.db.maxConns` (KACHO_NOTIFY_DB_MAX_CONNS) без умолчания:
// не задана — отказ старта с именем ручки; вне [1..100] — отказ с границей;
// в границе — пул собирается ровно этой ширины (`pool_max_conns` строки
// соединения, которую разбирает corelib db.NewPool).

import (
	"net/url"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

const (
	dbMaxConnsEnv  = "KACHO_NOTIFY_DB_MAX_CONNS"
	dbMaxConnsKnob = "notify.db.maxConns"
)

func TestConfig_DBMaxConnsWithoutValueRefusesStart(t *testing.T) {
	useFixture(t, map[string]*string{dbMaxConnsEnv: nil})
	requireOnlyRefusal(t, start(t), dbMaxConnsKnob, "не задана")
}

func TestConfig_DBMaxConnsOutOfBoundRefusesStart(t *testing.T) {
	for _, v := range []string{"0", "101", "-1"} {
		t.Run(v, func(t *testing.T) {
			useFixture(t, map[string]*string{dbMaxConnsEnv: str(v)})
			requireOnlyRefusal(t, start(t), dbMaxConnsKnob, "[1..100]")
		})
	}
}

// TestConfig_DBMaxConnsReachesThePoolDSN — близнец: значение в границе стартует,
// и строка соединения пула несёт ровно его; края границы приняты.
func TestConfig_DBMaxConnsReachesThePoolDSN(t *testing.T) {
	for _, v := range []string{"1", "7", "100"} {
		t.Run(v, func(t *testing.T) {
			useFixture(t, map[string]*string{dbMaxConnsEnv: str(v)})
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("загрузка: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("старт с %s=%s отвергнут: %v", dbMaxConnsEnv, v, err)
			}
			u, err := url.Parse(cfg.DSN())
			if err != nil {
				t.Fatalf("строка соединения не разбирается: %v", err)
			}
			q := u.Query()
			if got := q.Get("pool_max_conns"); got != v {
				t.Fatalf("pool_max_conns строки соединения = %q, ручка = %s", got, v)
			}
			if got := q.Get("sslmode"); got != "require" {
				t.Fatalf("sslmode строки соединения = %q — ширина пула сместила режим канала", got)
			}
		})
	}
}
