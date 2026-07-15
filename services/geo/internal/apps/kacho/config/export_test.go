// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"os"

	corecfg "github.com/PRO-Robotech/kacho/pkg/config"
)

// LoadInto — test-only хелпер (компилируется исключительно в _test.go, не
// попадает в production-бинарь и в публичный API пакета): выставляет переданные
// env-переменные на время вызова и грузит тем же путём LoadPrefixed, что и Load
// (по выходу восстанавливает env). Экспортируется во внешний тест-пакет
// config_test через export_test.go-идиому. Мутирует process-global env
// (не concurrency-safe) — поэтому вне production-пути.
func LoadInto(c *Config, env map[string]string) error {
	saved := make(map[string]*string, len(env))
	for k, v := range env {
		if prev, ok := os.LookupEnv(k); ok {
			saved[k] = &prev
		} else {
			saved[k] = nil
		}
		_ = os.Setenv(k, v)
	}
	defer func() {
		for k, prev := range saved {
			if prev == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *prev)
			}
		}
	}()
	return corecfg.LoadPrefixed(envPrefix, c)
}
