// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migratordivergence_test.go — гейт по дереву: каждое различие, которое решение
// объявляет живым, имеет предмет.
//
// Предмет, требования и границы разобраны в шапке migratordivergence.go — здесь
// они не пересказываются. Доказательство способности упасть и смолчать —
// в migratordivergence_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// collectMigratorServiceFacts обходит дерево и собирает факты по каждому
// сервису, у которого есть точка наката. Перечень ВЫЧИСЛЯЕТСЯ обходом, а не
// выписывается: сервис, заведённый завтра, попадает под гейт сам.
func collectMigratorServiceFacts(t *testing.T, root string) []migratorServiceFacts {
	t.Helper()

	paths, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	if err != nil {
		t.Fatalf("корпус дерева не построен: %v", err)
	}

	entries := map[string]migratorEntryFacts{}
	wrappers := map[string]migratorWrapperFacts{}

	for _, path := range paths {
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			t.Fatalf("путь %s не приводится к корню: %v", path, rerr)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		svc := serviceOfMigratorPath(rel)

		switch {
		case strings.HasSuffix(rel, "/cmd/migrator/main.go"):
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("%s не прочитан: %v", rel, rerr)
			}
			f, perr := parseMigratorEntryFacts(rel, string(src))
			if perr != nil {
				t.Fatalf("%v", perr)
			}
			entries[svc] = f

		case strings.Contains(rel, "/internal/apps/migrator/"):
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				t.Fatalf("%s не прочитан: %v", rel, rerr)
			}
			w, perr := parseMigratorWrapperFacts(wrappers[svc], rel, string(src))
			if perr != nil {
				t.Fatalf("%v", perr)
			}
			wrappers[svc] = w
		}
	}

	var names []string
	for svc := range entries {
		names = append(names, svc)
	}
	sort.Strings(names)

	facts := make([]migratorServiceFacts, 0, len(names))
	for _, svc := range names {
		facts = append(facts, migratorServiceFacts{
			Service: svc,
			Entry:   entries[svc],
			Wrapper: wrappers[svc],
		})
	}
	return facts
}

// TestEveryDeclaredMigratorDivergenceStillHasASubject — ведомость различий
// истекает сама.
func TestEveryDeclaredMigratorDivergenceStillHasASubject(t *testing.T) {
	root := repoRoot(t)
	facts := collectMigratorServiceFacts(t, root)

	census := migratorDivergenceCensus{Services: len(facts)}
	for _, f := range facts {
		if f.Wrapper.Present {
			census.Wrappers++
		}
	}

	// Предпосылка: пустой обход означает «ничего не осмотрено», а не «чисто».
	// Точек наката в этом продукте не бывает ноль, и ведомость без строк
	// проверяла бы воздух.
	if census.Services == 0 {
		t.Fatalf("осмотрено НОЛЬ сервисов с точкой наката (%s) — обход пуст, вердикт недействителен", census)
	}

	doc, derr := os.ReadFile(filepath.Join(root, migratorFormDecisionDoc))
	if derr != nil {
		t.Fatalf("решение о форме мигратора не читается (%s): %v — "+
			"ведомость ссылается на него в каждой находке", migratorFormDecisionDoc, derr)
	}

	for _, d := range migratorDeclaredDivergences {
		has := len(d.Subject(facts)) > 0
		if d.Closed == "" {
			census.Live++
			if has {
				census.LiveWithSubject++
			}
			continue
		}
		census.Closed++
		if !has {
			census.ClosedStillGone++
		}
	}

	findings := migratorDivergenceFindings(migratorDeclaredDivergences, facts, string(doc))
	for _, f := range findings {
		t.Errorf("%s", f)
	}

	t.Logf("перепись: %s", census)
	for _, d := range migratorDeclaredDivergences {
		state := "живо"
		if d.Closed != "" {
			state = "снято " + d.Closed
		}
		t.Logf("  %-28s %-40s предмет: %v", d.ID, state, d.Subject(facts))
	}
	if len(findings) == 0 {
		t.Logf("каждая строка ведомости названа в решении; живые имеют предмет, снятые не вернулись")
	}
}
