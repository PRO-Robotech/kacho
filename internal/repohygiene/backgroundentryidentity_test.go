// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBackgroundEntriesForwardANamedIdentity — УК3-31 (замысел issue-2918 §11а,
// CX3D-01 (б), CX3H-02 (в)): фоновый вход не достигает пересылающего вызова
// пишущего метода ни без явного принципала (1), ни с `SystemPrincipal()` (2);
// вход compute `FinishStuckDeletes` по половине (1) — строкой ведомости
// исключений с предикатом снятия. Шапка — `backgroundentryidentity.go`;
// инъекции в обе стороны — `backgroundentryidentity_injection_test.go`.
func TestBackgroundEntriesForwardANamedIdentity(t *testing.T) {
	t.Parallel()
	opts := BackgroundEntryOptions{Root: repoRoot(t)}
	if os.Getenv(foundationTrackerKnob) == "1" {
		var issues []int
		for _, x := range BackgroundEntryExceptions {
			issues = append(issues, x.Issue)
		}
		states, checked, unresolved, cfgErr := resolveFoundationIssueStates(issues)
		if cfgErr != "" {
			t.Fatalf("%s", cfgErr)
		}
		t.Logf("УК3-31: сверка ведомости с трекером: задач %d, сверено %d, не удалось %d", len(issues), checked, unresolved)
		if unresolved > 0 {
			t.Errorf("УК3-31: состояние %d задач ведомости выяснить не удалось — измерение объявлено и не выполнено", unresolved)
		}
		opts.IssueStates = states
	} else {
		t.Logf("УК3-31: сверка ведомости с трекером ВЫКЛЮЧЕНА (ручка %s=1): строк %d, сверено 0; "+
			"решение о закрытой задаче доказано инъекцией без сети", foundationTrackerKnob, len(BackgroundEntryExceptions))
	}
	var log strings.Builder
	findings, census, err := AuditBackgroundEntryIdentity(opts, &log)
	require.NoError(t, err)
	t.Log("\n" + log.String())
	if fails := BackgroundEntryPremiseFailures(census); len(fails) > 0 {
		t.Fatalf("УК3-31: перепись беспредметна:\n  %s", strings.Join(fails, "\n  "))
	}
	for _, f := range findings {
		t.Errorf("УК3-31 %s", f)
	}
}

// TestBackgroundEntriesPremise_TheNamedEntriesAndTheirOwnersAreSeen — предпосылка
// гейта: перечень входов — тот, что назван замыслом; обход каждого входа
// доходит до пересылающего вызова владельца, которого замысел называет (nlb —
// vpc `ReleaseOwnedAddress`; compute — vpc и storage `Detach`). Перестанет
// доходить — половины (1) и (2) замолчат молча; здесь это красное.
func TestBackgroundEntriesPremise_TheNamedEntriesAndTheirOwnersAreSeen(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"nlb.reconcileOne", "compute.FinishStuckDeletes"},
		[]string{BackgroundEntries[0].Key(), BackgroundEntries[1].Key()}, "УК3-31: перечень входов")
	require.Len(t, BackgroundEntries, 2, "УК3-31: перечень входов")
	_, census, err := AuditBackgroundEntryIdentity(BackgroundEntryOptions{Root: repoRoot(t)}, nil)
	require.NoError(t, err)
	want := map[string][]string{
		"nlb.reconcileOne":           {"services/nlb/internal/clients/vpc/internal_address_client.go|ReleaseOwnedAddress"},
		"compute.FinishStuckDeletes": {"services/compute/internal/clients/vpc_nic_client.go|Detach", "services/compute/internal/clients/storage_client.go|Detach"},
	}
	for _, c := range census {
		for _, w := range want[c.Entry] {
			file, method, _ := strings.Cut(w, "|")
			seen := false
			for _, r := range c.Reaches {
				if strings.HasPrefix(r.Pos, file+":") && r.Method == method && r.Write {
					seen = true
				}
			}
			require.True(t, seen, "УК3-31 предпосылка: обход %s не дошёл до пишущего %s в %s (достигнуто: %+v)", c.Entry, method, file, c.Reaches)
		}
	}
}
