// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestJournaledWritesGoThroughTheHelper — УК3-27 (замысел issue-2918, З4): всякая
// запись, способная породить строку журнала подписки модуля, идёт транзакцией
// помощника `journaltx`; инициатор выставляется локально к транзакции и только
// им; личность компонента — таблица фоновых путей (Д116). Правила (а)–(д) — шапка
// `journaledwrites.go`. Инъекции в обе стороны — `journaledwrites_injection_test.go`.
func TestJournaledWritesGoThroughTheHelper(t *testing.T) {
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditJournaledWrites(JournaledWriteOptions{Root: repoRoot(t)}, &log)
	require.NoError(t, err)
	t.Log("\n" + log.String())
	if fails := JournaledWritePremiseFailures(census); len(fails) > 0 {
		t.Fatalf("УК3-27: перепись беспредметна:\n  %s", strings.Join(fails, "\n  "))
	}
	for _, f := range findings {
		t.Errorf("УК3-27 %s", f)
	}
}

// TestJournaledWritesPremise_TheFiveModulesAndTheNamedTablesAreSeen — предпосылка
// гейта: обход видит пять подключённых модулей, а вывод журналируемых таблиц —
// таблицы, которые замысел (З4) называет журналируемыми. Если вывод перестанет
// их видеть, правило (б) замолчит о них молча; здесь это красное.
func TestJournaledWritesPremise_TheFiveModulesAndTheNamedTablesAreSeen(t *testing.T) {
	t.Parallel()
	_, census, err := AuditJournaledWrites(JournaledWriteOptions{Root: repoRoot(t)}, nil)
	require.NoError(t, err)
	var names []string
	for _, m := range census.Modules {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	require.Equal(t, []string{"compute", "nlb", "registry", "storage", "vpc"}, names,
		"предпосылка: подключённые модули — каталоги с internal/subscriptionjournal")

	want := map[string][]string{
		"storage":  {"storage_outbox", "volumes", "snapshots", "images", "volume_attachments"},
		"registry": {"registry_resource_journal", "registries"},
		"nlb":      {"nlb_outbox", "listeners"},
		"vpc":      {"vpc_outbox", "subnets"},
		"compute":  {"compute_outbox"},
	}
	for mod, tables := range want {
		m, ok := census.Module(mod)
		require.True(t, ok, mod)
		got := map[string]bool{}
		for _, tb := range m.JournaledTables {
			got[tb] = true
		}
		for _, tb := range tables {
			require.True(t, got[tb], "предпосылка: %s — таблица %s не выведена журналируемой (выведено: %v)", mod, tb, m.JournaledTables)
		}
	}
}

// TestJournaledComponentPairsAreTheDeclaredBackgroundPaths — Д116: фоновые пути,
// пишущие журнал модуля, — таблица пар (модуль, процесс), и только они. Таблица
// названа решением дословно; у каждой пары в живом дереве ровно один вызов
// `AsComponent` (правило (д) основного теста), здесь — состав таблицы и то, что
// перепись видит вызывающего каждой пары.
func TestJournaledComponentPairsAreTheDeclaredBackgroundPaths(t *testing.T) {
	t.Parallel()
	want := []JournaledComponentPair{
		{Module: "storage", Service: "storage", Role: "reconciler"},
		{Module: "nlb", Service: "nlb", Role: "free-ip-runner"},
		{Module: "nlb", Service: "nlb", Role: "target-drain-runner"},
		{Module: "registry", Service: "registry", Role: "orphan-sweep"},
	}
	require.Equal(t, want, JournaledComponentPairs, "Д116: таблица фоновых путей")
	_, census, err := AuditJournaledWrites(JournaledWriteOptions{Root: repoRoot(t)}, nil)
	require.NoError(t, err)
	for _, p := range want {
		m, ok := census.Module(p.Module)
		require.True(t, ok, p.Module)
		n := 0
		for _, c := range m.AsComponent {
			if strings.HasSuffix(c, "("+p.Service+", "+p.Role+")") {
				n++
			}
		}
		require.Equal(t, 1, n, "Д116: вызывающих пары (%s, %s) в дереве: %d — %v", p.Service, p.Role, n, m.AsComponent)
	}
}
