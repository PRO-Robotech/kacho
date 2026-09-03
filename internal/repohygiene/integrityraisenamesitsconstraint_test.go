// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// integrityraisenamesitsconstraint_test.go — держатель: живой производитель
// класса `integrity_constraint_violation` называет связь.
//
// Предмет, разбор «живого» и довод против числа в комментарии — в шапке
// `integrityraisenamesitsconstraint.go`; здесь они не пересказываются.
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `integrityraisenamesitsconstraint_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// integrityMigrationsGlob — где живут миграции сервисов.
//
// Обход идёт по ВСЕМ сервисам, а не по одному: правило про имя связи общее —
// отображение отказов каждого сервиса ключуется именем, — и гейт, суженный до
// того сервиса, где класс нашли, молчал бы у следующего.
const integrityMigrationsGlob = "services/*/internal/migrations/*.sql"

func TestLiveIntegrityRaiseNamesItsConstraint(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var all []IntegrityRaiseSite
	migrations, withClass := 0, 0
	for rel := range tt.files {
		ok, err := filepath.Match(integrityMigrationsGlob, rel)
		if err != nil || !ok {
			continue
		}
		migrations++
		body, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса СВОЕГО репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		sites := IntegrityRaiseSitesIn(rel, string(body))
		if len(sites) > 0 {
			withClass++
		}
		all = append(all, sites...)
	}

	live := LiveIntegrityRaiseSites(all)
	sort.Slice(live, func(i, j int) bool {
		if live[i].File != live[j].File {
			return live[i].File < live[j].File
		}
		return live[i].Line < live[j].Line
	})

	inDown, named := 0, 0
	for _, s := range all {
		if s.InDownBranch {
			inDown++
		}
	}
	var findings []string
	for _, s := range live {
		if s.NamesConstraint {
			named++
			continue
		}
		findings = append(findings, fmt.Sprintf(
			"  %s:%d функция %q поднимает класс целостности БЕЗ `CONSTRAINT` — "+
				"отображение отказов выберет общую полосу, и потребитель этой не различит",
			s.File, s.Line, s.Function))
	}

	t.Logf("миграций осмотрено %d; из них поднимают класс целостности %d; "+
		"операторов RAISE с этим классом %d (в ветви отката %d, замещённых поздним определением %d); "+
		"живых производителей %d, из них называют связь %d",
		migrations, withClass, len(all), inDown, len(all)-inDown-len(live), len(live), named)

	if migrations == 0 {
		t.Fatal("миграций не найдено: обход пуст, и вердикт беспредметен")
	}
	if len(live) == 0 {
		t.Fatal("живых производителей класса целостности в дереве нет — предпосылка гейта исчезла, " +
			"и его молчание перестало что-либо означать: снимите утверждение вместе с предметом " +
			"либо переведите на признак, который дерево производит")
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("%d живых производител(ей) класса целостности не называют связь:\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}
