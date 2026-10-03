// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// TestMain — пробы пакета, собирающие конфигурацию загрузчиком, видят ручки Р4
// приёмки KA1 в величинах профилей, как процесс на любом стенде. Умолчания в
// загрузчике нет; отказ без объявления — предмет TestCallBudget* ниже и
// KA1-20/21 (gateway/cmd/api-gateway).
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

// Три состояния ручки, два текста отказа: «не объявлено» и «объявлено, но не
// годится» лечатся по-разному.
func TestCallBudgetDistinguishesUndeclaredFromUnfit(t *testing.T) {
	const k = config.KnobIdentityCallBudget
	if _, err := config.ParseCallBudget(k, "", false); err == nil ||
		!strings.Contains(err.Error(), k+" не объявлено") {
		t.Fatalf("необъявленная ручка: %v", err)
	}
	for _, raw := range []string{"", "abc", "0s", "-1s"} {
		_, err := config.ParseCallBudget(k, raw, true)
		if err == nil || !strings.Contains(err.Error(), k+" объявлено, но не годится") ||
			!strings.Contains(err.Error(), `"`+raw+`"`) || strings.Contains(err.Error(), "не объявлено") {
			t.Errorf("%q: отказ обязан сказать «объявлено, но не годится» и привести значение: %v", raw, err)
		}
	}
	// Близнецы: годные величины читаются как есть.
	for raw, want := range map[string]time.Duration{"1s": time.Second, "250ms": 250 * time.Millisecond, "30s": 30 * time.Second} {
		got, err := config.ParseCallBudget(k, raw, true)
		if err != nil || got != want {
			t.Errorf("%q: получено %v, %v; ожидалось %v", raw, got, err, want)
		}
	}
}

// Загрузчик читает обе ручки из окружения процесса.
func TestCallBudgetsReachTheConfig(t *testing.T) {
	t.Setenv(config.KnobIdentityCallBudget, "1500ms")
	t.Setenv(config.KnobBackendCallBudget, "300ms")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IdentityCallBudget != 1500*time.Millisecond || cfg.BackendCallBudget != 300*time.Millisecond {
		t.Fatalf("ручки не дошли до конфигурации: %v / %v", cfg.IdentityCallBudget, cfg.BackendCallBudget)
	}
}
