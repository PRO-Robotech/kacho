// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// TestNotifyCatalogCountsTwoProcessRoots — правило Д74 на дереве: по каталогу
// services/notify два корня процессов (notify, notify-probe), точка наката
// процессом не считается.
func TestNotifyCatalogCountsTwoProcessRoots(t *testing.T) {
	t.Parallel()
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	roots := catalogProcessRoots(tree.SortedFiles())
	for svc, rs := range roots {
		t.Logf("  %-9s корней %d %v", svc, len(rs), rs)
	}
	if got := strings.Join(roots["notify"], ","); got != "notify,notify-probe" {
		t.Fatalf("корни процессов services/notify: %q, ожидались notify,notify-probe", got)
	}
	if len(roots) == 0 {
		t.Fatal("обход пуст: каталогов с корнями процессов ноль")
	}
}

// TestProcessOfFileSplitsOnlyMultiRootCatalogs — файл корня каталога с двумя
// корнями принадлежит корню; каталог с одним корнем — как прежде, целиком.
func TestProcessOfFileSplitsOnlyMultiRootCatalogs(t *testing.T) {
	t.Parallel()
	roots := catalogProcessRoots([]string{
		"services/notify/cmd/notify/main.go",
		"services/notify/cmd/notify-probe/main.go",
		"services/notify/cmd/migrator/main.go",
		"services/vpc/cmd/vpc/main.go",
		"services/vpc/cmd/migrator/main.go",
	})
	cases := map[string]string{
		"services/notify/cmd/notify-probe/diagnostics.go":  "notify/notify-probe",
		"services/notify/cmd/notify-probe/internal/x/x.go": "notify/notify-probe",
		"services/notify/cmd/notify/main.go":               "notify/notify",
		"services/notify/internal/config/config.go":        "notify",
		"services/notify/cmd/migrator/main.go":             "notify",
		"services/vpc/cmd/vpc/main.go":                     "vpc",
		"services/vpc/internal/observability/metrics/x.go": "vpc",
	}
	for rel, want := range cases {
		if got, _ := processOfFile(roots, rel); got != want {
			t.Errorf("%s: процесс %q, ожидался %q", rel, got, want)
		}
	}
	if got := processMetricSegment("notify/notify-probe"); got != "notifyprobe" {
		t.Errorf("сегмент серий notify-probe: %q", got)
	}
	if got := processMetricSegment("vpc"); got != "vpc" {
		t.Errorf("сегмент серий vpc: %q", got)
	}
	// Инъекция «третий корень» — каталог считает три процесса, а не два.
	three := catalogProcessRoots([]string{
		"services/notify/cmd/notify/main.go",
		"services/notify/cmd/notify-probe/main.go",
		"services/notify/cmd/x/main.go",
	})
	if len(three["notify"]) != 3 {
		t.Errorf("третий корень не сосчитан: %v", three["notify"])
	}
}
