// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// searchpathnotbuiltbytests_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт
// способен упасть и способен смолчать.
//
// Инъекция подаётся НАСТОЯЩИМ входом — исходником пробы той формы, что лежала в
// дереве до правки, — и правит РОВНО ОДИН факт против положительного близнеца:
// что с литералом ДЕЛАЮТ. Литерал в обеих сторонах пары один и тот же, поэтому
// красное не могло прийти от его наличия.
//
// Судит инъекцию ТА ЖЕ функция, что исполняется обходом дерева
// (`SearchPathBuildSitesIn`).
package repohygiene

import (
	"strings"
	"testing"
)

// searchPathInjectionSource — исходник пробы. `%[1]s` — ЕДИНСТВЕННОЕ
// подставляемое место: тело функции, где литерал либо склеивают, либо передают.
const searchPathInjectionSource = `package probe

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/stretchr/testify/require"
)

func TestProbe(t *testing.T) {
	dsn := pgtest.NewDB(t)
%[1]s
	_ = dsn
	_ = require.Contains
}
`

func TestSearchPathGateFallsAndStaysSilentOnItsTwin(t *testing.T) {
	cases := []struct {
		name string
		body string
		// wantFindings — сколько склеек ожидается.
		wantFindings int
		// wantVia — через какое имя литерал попал в склейку (пусто = напрямую).
		wantVia string
		why     string
	}{
		{
			name: "склейка через именованную константу — канонический вид копии",
			body: `	const optionsParam = "options=-c%20search_path%3Dkacho_iam%2Cpublic"
	sep := "?"
	dsn = dsn + sep + optionsParam`,
			wantFindings: 1, wantVia: "optionsParam",
			why: "шаг косвенности необходим: сам литерал в склейке не участвует",
		},
		{
			name:         "склейка литералом напрямую",
			body:         `	dsn = dsn + "&options=-c%20search_path%3Dkacho_storage%2Cpublic"`,
			wantFindings: 1, wantVia: "",
			why: "ОДИН факт против предыдущего: косвенности нет",
		},
		{
			name:         "склейка через `+=`",
			body:         `	dsn += "&options=-c%20search_path%3Dkacho_nlb%2Cpublic"`,
			wantFindings: 1, wantVia: "",
			why: "форма без BinaryExpr: разбор, знающий одну форму, молчал бы на второй",
		},
		{
			name:         "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: литерал передан на сверку",
			body:         `	require.Contains(t, dsn, "options=-c%20search_path%3Dkacho_vpc%2Cpublic")`,
			wantFindings: 0,
			why:          "утверждение о ПРОДУКТОВОМ DSN законно: конфигурация сервиса собирает клаузу сама",
		},
		{
			name: "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: литерал в таблице случаев",
			body: `	cases := []struct{ dsn, want string }{
		{"postgres://u:p@h/d?sslmode=disable&options=-c%20search_path%3Dkacho_vpc", "disable"},
	}
	_ = cases`,
			wantFindings: 0,
			why:          "вход таблицы — данные пробы, а не сборка строки соединения",
		},
		{
			name:         "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: объявленная форма",
			body:         `	dsn = pgtest.WithSearchPath(dsn, "kacho_iam,public")`,
			wantFindings: 0,
			why:          "ровно то, чего гейт добивается: реализация одна на дерево",
		},
		{
			name: "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: клауза названа ПРОЗОЙ",
			body: `	// Приведение схемы (options=-c search_path=kacho_iam,public) объявлено пакетом.
	dsn = dsn + "&pool_max_conns=8"`,
			wantFindings: 0,
			why:          "разбор судит узел, а не текст: иначе гейт краснел бы на собственном объяснении",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := strings.Replace(searchPathInjectionSource, "%[1]s", c.body, 1)
			sites, ok := SearchPathBuildSitesIn("services/probe/x_test.go", src)
			if !ok {
				t.Fatalf("исходник не разобрался — инъекция не доехала до предмета:\n%s", src)
			}
			if len(sites) != c.wantFindings {
				t.Fatalf("находок %d, ожидалось %d (%s)\n%v", len(sites), c.wantFindings, c.why, sites)
			}
			if c.wantFindings == 0 {
				return
			}
			if sites[0].Via != c.wantVia {
				t.Fatalf("находка через %q, ожидалось %q (%s)", sites[0].Via, c.wantVia, c.why)
			}
			if sites[0].File == "" || sites[0].Line == 0 {
				t.Fatalf("находка не называет координату: %+v", sites[0])
			}
		})
	}
	t.Logf("сторон проверено %d", len(cases))
}

// TestSearchPathGateWouldHaveCaughtTheTreeBeforeTheFix — контроль ПРЕДПОСЫЛКИ
// гейта: форма, которая в дереве действительно была, им ловится.
//
// Без этой пробы «находок 0» на сегодняшнем дереве доказывало бы лишь, что
// функция умеет считать до нуля. Здесь подан дословный текст помощника, стоявший
// в десяти файлах дерева до правки.
func TestSearchPathGateWouldHaveCaughtTheTreeBeforeTheFix(t *testing.T) {
	const before = `package probe

import "strings"

func appendSearchPathOptions(dsn string) string {
	const optionsParam = "options=-c%20search_path%3Dkacho_iam%2Cpublic"
	if strings.Contains(dsn, "options=") || strings.Contains(dsn, "options%3D") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + optionsParam
}
`
	sites, ok := SearchPathBuildSitesIn("services/probe/before_test.go", before)
	if !ok {
		t.Fatal("исходник не разобрался — контроль беспредметен")
	}
	if len(sites) != 1 {
		t.Fatalf("находок %d вместо одной: форма, которая в дереве была, гейтом не ловится", len(sites))
	}
	t.Logf("дословный помощник дерева до правки: находка %s:%d через %s",
		sites[0].File, sites[0].Line, sites[0].Via)
}
