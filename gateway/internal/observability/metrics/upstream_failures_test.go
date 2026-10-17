// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/gateway/internal/upstreamfailure"
)

// kacho#3221 — клетка причины отказа соседу существует нулём для КАЖДОГО
// объявленного соседа и каждой причины до первого события; перечень причин
// выводится из пакета учёта, а не выписывается здесь.
func TestUpstreamFailures_EveryCellExistsWithZeroBeforeTheFirstEvent(t *testing.T) {
	upstreams := gwmetrics.UpstreamFailureUpstreamLabels()
	tally := upstreamfailure.NewTally(upstreams)
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterUpstreamFailures(tally.Snapshot)
	body := expose(t, m)
	present := 0
	for _, u := range upstreams {
		for _, c := range upstreamfailure.Causes() {
			line := `kacho_api_gateway_upstream_call_failures_total{cause="` + string(c) + `",upstream="` + u + `"} 0`
			if !strings.Contains(body, line) {
				t.Errorf("клетки нет с нулём: %s", line)
				continue
			}
			present++
		}
	}
	if present == 0 {
		t.Fatal("клеток 0 — перепись пуста")
	}
	t.Logf("перепись: соседей %d · причин %d · клеток на поверхности %d", len(upstreams), len(upstreamfailure.Causes()), present)
}

// Неразрешённый адрес и остановленная служба — РАЗНЫЕ клетки одного соседа.
func TestUpstreamFailures_UnresolvedAndStoppedAreDifferentCells(t *testing.T) {
	tally := upstreamfailure.NewTally([]string{"geo"})
	tally.Record("geo", status.Error(codes.Unavailable, "no children to pick from"))
	tally.Record("geo", status.Error(codes.Unavailable, "connection error: desc = \"transport: Error while dialing: connect: connection refused\""))
	tally.Record("geo", status.Error(codes.Unavailable, "connection error: desc = \"transport: Error while dialing: connect: connection refused\""))
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterUpstreamFailures(tally.Snapshot)
	body := expose(t, m)
	for _, want := range []string{
		`kacho_api_gateway_upstream_call_failures_total{cause="unresolved",upstream="geo"} 1`,
		`kacho_api_gateway_upstream_call_failures_total{cause="unavailable",upstream="geo"} 2`,
		`kacho_api_gateway_upstream_call_failures_total{cause="deadline",upstream="geo"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("на приборе нет %s", want)
		}
	}
}

// Словарь соседей поверхности — ключи карты адресов корня, ни больше ни меньше.
func TestUpstreamFailures_UpstreamLabelsAreTheBackendAddressKeys(t *testing.T) {
	keys := config.Config{}.BackendAddrs()
	labels := gwmetrics.UpstreamFailureUpstreamLabels()
	if len(keys) == 0 {
		t.Fatal("ключей адресов 0 — сверять не с чем")
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if _, ok := keys[l]; !ok {
			t.Errorf("метка соседа %q не ключ адреса корня", l)
		}
		seen[l] = true
	}
	for k := range keys {
		if !seen[k] {
			t.Errorf("ключ адреса %q без метки — его отказы не выйдут на поверхность", k)
		}
	}
	t.Logf("перепись: ключей адресов %d · меток соседа %d", len(keys), len(labels))
}

// Словарь причин поверхности — причины пакета учёта, в том же порядке.
func TestUpstreamFailures_CauseLabelsAreTheTallyCauses(t *testing.T) {
	labels := gwmetrics.UpstreamFailureCauseLabels()
	causes := upstreamfailure.Causes()
	if len(labels) != len(causes) || len(causes) == 0 {
		t.Fatalf("меток причины %d, причин учёта %d", len(labels), len(causes))
	}
	for i := range causes {
		if labels[i] != string(causes[i]) {
			t.Errorf("метка %d = %q, причина учёта %q", i, labels[i], causes[i])
		}
	}
}

// Сосед, которого нет в словаре поверхности, меткой не становится.
func TestUpstreamFailures_ADataKeyDoesNotBecomeALabel(t *testing.T) {
	tally := upstreamfailure.NewTally([]string{"tenant-42"})
	tally.Record("tenant-42", status.Error(codes.Unavailable, "x"))
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterUpstreamFailures(tally.Snapshot)
	if body := expose(t, m); strings.Contains(body, "tenant-42") {
		t.Fatal("ключ из данных стал меткой")
	}
}
