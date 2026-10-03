// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// TestMain — пробы пакета, собирающие конфигурацию загрузчиком (`config.Load`),
// видят ручки Р4 приёмки KA1 в величинах профилей, как процесс на любом стенде:
// умолчания в загрузчике нет.
func TestMain(m *testing.M) {
	for k, v := range map[string]string{
		config.KnobIdentityCallBudget: "1s",
		config.KnobBackendCallBudget:  "30s",
	} {
		if _, ok := os.LookupEnv(k); !ok {
			_ = os.Setenv(k, v)
		}
	}
	os.Exit(m.Run())
}
