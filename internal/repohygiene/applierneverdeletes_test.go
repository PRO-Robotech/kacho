// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// applierneverdeletes_test.go — держатель Г3: применитель ролей модуля НЕ
// удаляет строку роли (сценарий MOD-RD-15).
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `applierneverdeletes_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// applierPackageDir — каталог применителя.
	applierPackageDir = "services/iam/internal/apps/kacho/moduleroles/"
	// applierCensusFloor — файлов пакета, ниже которого обход беспредметен.
	applierCensusFloor = 2
)

// applierDeleteFindings — предикат находки. Тот же зовёт инъекция.
func applierDeleteFindings(sites []ApplierDeleteSite) []string {
	var out []string
	for _, s := range sites {
		out = append(out, fmt.Sprintf("%s:%d  [%s] %s", s.File, s.Line, s.Kind, s.What))
	}
	sort.Strings(out)
	return out
}

// TestMODRD15ApplierNeverDeletesARoleRow — сам гейт.
func TestMODRD15ApplierNeverDeletesARoleRow(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasPrefix(rel, applierPackageDir) || !strings.HasSuffix(rel, ".go") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed          int
		methods, lits   int
		comments        int
		sites           []ApplierDeleteSite
		portDeclaresAny bool
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		s, census, err := ScanApplierDeletes(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		methods += census.InterfaceMethods
		lits += census.StringLiterals
		comments += census.Comments
		sites = append(sites, s...)
		if census.InterfaceMethods > 0 {
			portDeclaresAny = true
		}
	}

	t.Logf("перепись: файлов пакета %s разобрано %d, методов интерфейсов прочитано %d, "+
		"строковых литералов %d, комментариев %d, находок %d",
		applierPackageDir, parsed, methods, lits, comments, len(sites))

	if parsed < applierCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — применитель "+
			"переехал либо снят, и гейт стережёт каталог, которого больше нет",
			parsed, applierCensusFloor)
	}
	// Предпосылка: порт вообще ОБЪЯВЛЕН. Ноль методов означает, что судить
	// нечего, и молчание гейта было бы сказано ни о чём.
	if !portDeclaresAny || methods == 0 {
		t.Fatalf("в пакете применителя не прочитано ни одного метода интерфейса — порта нет, "+
			"и первая ось гейта беспредметна (прочитано файлов %d)", parsed)
	}
	// Предпосылка второй оси: комментарии в пакете ЕСТЬ. Различение «код против
	// текста» без них ничего не различает.
	if comments == 0 {
		t.Fatalf("в пакете применителя ноль комментариев — вторая ось гейта не отличает " +
			"литерал от прозы, потому что прозы нет")
	}

	if findings := applierDeleteFindings(sites); len(findings) > 0 {
		t.Fatalf("применитель способен удалить строку роли — %d место(а):\n  %s\n\n"+
			"Роль с выдачами удалить нельзя: `access_bindings_role_fk … ON DELETE RESTRICT` "+
			"отвергнет операцию, и применитель встанет на первой же роли, которой кто-то "+
			"пользуется. А если бы не отверг — каскад унёс бы селекторы, проекцию глаголов "+
			"и проекцию сегментов МОЛЧА.\n"+
			"Снятие роли — предмет задачи #1913, и «снять и положить» вместо приведения "+
			"его не заменяет. Форма снятия выбрана: строка ПОМЕЧАЕТСЯ снятой, а не "+
			"удаляется — services/iam/docs/engineering/architecture/role-withdrawal-is-a-mark.md", len(findings), strings.Join(findings, "\n  "))
	}
}
