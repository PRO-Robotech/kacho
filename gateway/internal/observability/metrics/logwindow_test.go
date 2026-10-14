// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// kacho#2740 — у каждого окна доклада края есть клетка на приборе. Строка
// журнала раз в окно называет ПЕРВОЕ событие окна; клетка — сколько их было с
// запуска, и по её росту видно, держится ли состояние.

type fixedWindow uint64

func (w fixedWindow) Total() uint64 { return uint64(w) }

// windowCells — строки клеток окон на поверхности.
func windowCells(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "kacho_api_gateway_log_window_events_total{") {
			out = append(out, line)
		}
	}
	return out
}

// Перечень окон выводится из кода: каждое поле middleware.LogWindows и окно
// поверхности DPoP — по клетке, с нулём до первого события (и у окна, чья
// полоса не смонтирована: ноль отличается от «клетки нет»).
func TestLogWindows_EveryWindowHasACellWithZeroBeforeTheFirstEvent(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterLogWindows(func() gwmetrics.LogWindowSources { return gwmetrics.LogWindowSources{} })
	cells := windowCells(expose(t, m))
	declared := reflect.TypeOf(middleware.LogWindows{}).NumField() + 1 // + окно поверхности DPoP
	t.Logf("перепись: окон объявлено %d · клеток на поверхности %d", declared, len(cells))
	if len(cells) != declared {
		t.Fatalf("клеток %d, окон %d — у окна нет клетки либо клетка без окна:\n%s",
			len(cells), declared, strings.Join(cells, "\n"))
	}
	for _, c := range cells {
		if !strings.HasSuffix(c, "} 0") {
			t.Errorf("клетка окна до первого события не ноль: %q", c)
		}
	}
}

// Величина клетки — итог окна: событие, насчитанное окном, видно на приборе.
func TestLogWindows_ACellCarriesTheWindowTotal(t *testing.T) {
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterLogWindows(func() gwmetrics.LogWindowSources {
		return gwmetrics.LogWindowSources{
			Auth: middleware.LogWindows{AuthorityRevocationFailure: fixedWindow(7)},
			DPoP: fixedWindow(3),
		}
	})
	body := expose(t, m)
	for _, want := range []string{
		`kacho_api_gateway_log_window_events_total{window="revocation_authority_failure"} 7`,
		`kacho_api_gateway_log_window_events_total{window="auth_methods_unusable_dpop"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("итог окна не дошёл до прибора: ожидалась строка %q", want)
		}
	}
}
