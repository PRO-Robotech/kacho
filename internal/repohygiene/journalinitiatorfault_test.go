// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/journalfault"
)

// TestJournalInitiatorFaultIsDecidedBeforeTheCheckClass — гейт по дереву
// (kacho#2918, S1A1-c3): в каждом модуле, чья миграция объявляет колонку
// инициатора журнала с ограничением формы, каждая функция, решающая класс
// 23514, спрашивает `pkg/journalfault` раньше этого решения.
//
// Предпосылки проверяются и печатаются переписью:
//   - модулей с журналом инициатора ≥ 1 (обход, не нашедший ни одного, — не
//     «зелёный», а «не выполнилось»);
//   - имя каждого ограничения формы = `<таблица>` + journalfault.ConstraintSuffix —
//     опознание в `pkg/journalfault` держится ровно на этом;
//   - в каждом модуле найдена хотя бы одна функция, решающая 23514
//     (положительный контроль распознавателя).
func TestJournalInitiatorFaultIsDecidedBeforeTheCheckClass(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	// (1) Модули журнала инициатора — из миграций, а не перечнем.
	modules := map[string][]JournalInitiatorConstraint{}
	for _, rel := range rels {
		if !strings.HasPrefix(rel, "services/") || !strings.Contains(rel, "/migrations/") || !strings.HasSuffix(rel, ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- путь из состава дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		for _, c := range ScanJournalInitiatorConstraints(string(body)) {
			svc := strings.SplitN(rel, "/", 3)[1]
			modules[svc] = append(modules[svc], c)
			if c.Constraint != c.Table+journalfault.ConstraintSuffix {
				t.Errorf("%s: ограничение формы инициатора %q на таблице %q не названо %q — "+
					"journalfault.Initiator его не опознает, и 23514 уйдёт отказом по вводу",
					rel, c.Constraint, c.Table, c.Table+journalfault.ConstraintSuffix)
			}
		}
	}
	if len(modules) == 0 {
		t.Fatal("ни одна миграция не объявляет колонку инициатора с ограничением формы — " +
			"предмета нет либо разбор ослеп; вердикта нет")
	}

	// (2) Мапперы модулей.
	var (
		files, funcs int
		perModule    = map[string]int{}
		guarded      int
	)
	for _, rel := range rels {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		parts := strings.SplitN(rel, "/", 3)
		if len(parts) < 3 || parts[0] != "services" {
			continue
		}
		if _, ok := modules[parts[1]]; !ok {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- путь из состава дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		mappers, census, err := ScanJournalFaultMappers(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		files++
		funcs += census.Funcs
		for _, m := range mappers {
			perModule[parts[1]]++
			if m.Guarded {
				guarded++
				continue
			}
			if m.GuardPos != 0 {
				t.Errorf("%s:%d %s: journalfault спрошен на строке %d — ПОСЛЕ решения о 23514; "+
					"отказ журнала по инициатору уйдёт отказом по вводу", m.File, m.Line, m.Func, m.GuardPos)
				continue
			}
			t.Errorf("%s:%d %s решает класс 23514, не спросив journalfault — отказ журнала по "+
				"инициатору (дефект записи сервиса) уйдёт INVALID_ARGUMENT", m.File, m.Line, m.Func)
		}
	}

	names := make([]string, 0, len(modules))
	for n := range modules {
		names = append(names, n)
	}
	sort.Strings(names)
	var lines []string
	for _, n := range names {
		lines = append(lines, n+": ограничений формы "+itoa(len(modules[n]))+", мапперов 23514 "+itoa(perModule[n]))
		if perModule[n] == 0 {
			t.Errorf("модуль %s ведёт журнал инициатора, а ни одной функции, решающей 23514, "+
				"не найдено — распознаватель ослеп либо форма решения сменилась", n)
		}
	}
	t.Logf("перепись: модулей с журналом инициатора %d · файлов Go разобрано %d · функций прочитано %d · "+
		"мапперов 23514 %d, из них с охраной %d\n  %s",
		len(modules), files, funcs, sumInts(perModule), guarded, strings.Join(lines, "\n  "))
}

func sumInts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
