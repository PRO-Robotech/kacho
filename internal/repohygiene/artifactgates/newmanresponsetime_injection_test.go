// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"strings"
	"testing"
)

// TestNewmanResponseTimeInjection_BudgetAssertIsFound — инъекция в обе стороны:
// утверждение времени ответа в источнике кейса и в порождённой коллекции —
// находки с координатой; комментарий о времени ответа и утверждение кода
// ответа — законные близнецы, молчат.
func TestNewmanResponseTimeInjection_BudgetAssertIsFound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, rel, body string
		want            int
	}{
		{"утверждение в источнике кейса", "services/vpc/tests/newman/scripts/gen.py",
			`x = "pm.test('t', () => pm.expect(pm.response.responseTime).to.be.below(300));"`, 1},
		{"утверждение в коллекции", "services/vpc/tests/newman/collections/a.postman_collection.json",
			`        "pm.test('t', () => pm.expect(pm.response.responseTime).to.be.below(500));",`, 1},
		{"комментарий Python о времени ответа", "services/vpc/tests/newman/scripts/gen.py",
			`    # pm.response.responseTime — бюджет живёт в нагрузочном наборе`, 0},
		{"комментарий JS о времени ответа", "tests/newman/kacholib/x.js",
			`// pm.response.responseTime не судится`, 0},
		{"утверждение кода ответа", "services/vpc/tests/newman/scripts/gen.py",
			`x = "pm.test('s', () => pm.expect(pm.response.code).to.eql(200));"`, 0},
	}
	for _, c := range cases {
		var census responseTimeCensus
		got := responseTimeFindings(c.rel, []byte(c.body), &census)
		if len(got) != c.want {
			t.Errorf("%s: находок %d, ждали %d: %v", c.name, len(got), c.want, got)
			continue
		}
		if c.want > 0 && !strings.HasPrefix(got[0], c.rel+":1:") {
			t.Errorf("%s: находка без координаты строки: %q", c.name, got[0])
		}
		if census.files != 1 || census.lines != 1 {
			t.Errorf("%s: перепись файлов %d, строк %d — ждали 1 и 1", c.name, census.files, census.lines)
		}
	}
	for _, rel := range []string{"services/vpc/tests/k6/resource_ops.js", "services/vpc/internal/x.go",
		"services/vpc/tests/newman/docs/CASES-INDEX.md"} {
		if isNewmanSuiteFile(rel) {
			t.Errorf("%s: не файл набора newman, а обход его взял", rel)
		}
	}
	if !isNewmanSuiteFile("services/vpc/tests/newman/cases/network.py") ||
		!isNewmanSuiteFile("tests/newman/kacholib/gen_shared.py") {
		t.Error("файл набора newman выпал из обхода")
	}
}
