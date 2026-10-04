// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identityprobefatebeyond_injection_test.go — записи А и Б ведомости судьбы
// проб СПОСОБНЫ упасть и СПОСОБНЫ смолчать (задача #1276, часть 2).
//
// Вход — СИНТЕТИКА: документ, индекс и тексты проб собираются здесь. Имя
// поставщика в этом файле литералом не пишется — оно собирается из единственного
// дома меток (`retiredVendorMarks`) и бренда (`identityVendorBrand`): литерал
// сделал бы этот файл пробой, называющей поставщика, и привязкой под потолком.
//
// Каждая инъекция меняет РОВНО ОДИН факт против законного близнеца, и близнец
// прогоняется первым. Находка проверяется по ТЕКСТУ.
package repohygiene

import (
	"fmt"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетика записи А.

// signRow — строка синтетической таблицы А.
type signRow struct{ file, fate, change, class, held, why string }

// signDoc — документ: законная ведомость трёх проб и таблица А с разбивкой.
// totals == nil — разбивка считается по строкам (законный близнец).
func signDoc(rows []signRow, totals map[string]string, total string) string {
	var b strings.Builder
	b.WriteString(probeFateDoc(probeFateLawfulRows(), nil, ""))
	b.WriteString("\n## Гейты признака\n\n| " + strings.Join(identitySignGateHeader, " | ") + " |\n|---:|---|---|---|---|---|---|\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "| %d | `%s` | %s | %s | %s | %s | %s |\n", i+1, r.file, r.fate, r.change, r.class, r.held, r.why)
	}
	if totals == nil {
		counts := map[string]int{}
		for _, r := range rows {
			counts[r.fate]++
		}
		totals = map[string]string{}
		for _, f := range identityProbeFates {
			totals[f] = fmt.Sprint(counts[f])
		}
		total = fmt.Sprint(len(rows))
	}
	b.WriteString("\n| " + strings.Join(identitySignGateTotals, " | ") + " |\n|---|---:|\n")
	for _, f := range identityProbeFates {
		if v, ok := totals[f]; ok {
			fmt.Fprintf(&b, "| %s | %s |\n", f, v)
		}
	}
	if total != "" {
		fmt.Fprintf(&b, "| **итого** | **%s** |\n", total)
	}
	return b.String()
}

const signPinnedPath = "internal/held/held_test.go"

// signLawfulRows — законный близнец: снятый с преемником, оставленный и
// переписанный.
func signLawfulRows() []signRow {
	return []signRow{
		{"identity_gone_test.go", identityFateRemove, "#2818", "класс отказа", identitySignHeldBySuccessor,
			"`" + probeFateProseTarget + ":7` · `kaname:" + signPinnedPath + "#TestHeld`"},
		{"identity_alpha_test.go", identityFateKeep, identityNoValue, "класс отказа", identitySignHeldInPlace,
			"`deploy/identity_alpha_test.go:10-20`"},
		{"identity_gamma_test.go", identityFateRewrite, "#2818", "класс отказа", identitySignHeldInPlace,
			"`deploy/identity_gamma_test.go:1`"},
	}
}

// signLawfulFacts — индекс законного близнеца: три пробы ведомости и файл
// пиненного модуля с объявленным преемником.
func signLawfulFacts() identitySignFacts {
	return identitySignFacts{
		Tracked: map[string]bool{"deploy/identity_alpha_test.go": true, "deploy/identity_beta_test.go": true,
			"deploy/identity_gamma_test.go": true, probeFateProseTarget: true},
		Pinned: map[string][]string{"kaname:" + signPinnedPath: {"package held", "", "func TestHeld(t *testing.T) {", "}"}},
	}
}

// signJudge — разбор и суд записи А одним вызовом.
func signJudge(t *testing.T, doc string, f identitySignFacts, want int) ([]string, identitySignCensus) {
	t.Helper()
	l, err := parseIdentityProbeFateLedger(doc, "deploy")
	if err != nil {
		t.Fatalf("разбор синтетики отказал: %v", err)
	}
	return judgeIdentitySignGates(l, f, "deploy", "ведомость.md", want)
}

func TestIdentitySignGatesInjection_LawfulRecordIsSilent(t *testing.T) {
	t.Parallel()
	doc := signDoc(signLawfulRows(), nil, "")
	found, c := signJudge(t, doc, signLawfulFacts(), 3)
	if len(found) != 0 {
		t.Fatalf("законная запись дала находки:\n%s", strings.Join(found, "\n"))
	}
	if c.Rows != 3 || c.Absent != 1 || c.Present != 2 || c.Pinned != 1 || c.Coords != 3 ||
		c.ByHeld[identitySignHeldBySuccessor] != 1 || c.ByHeld[identitySignHeldInPlace] != 2 {
		t.Fatalf("перепись законной записи неверна: %s", c)
	}
	// Доводы таблицы А — координаты вне строк ведомости: их разрешает и
	// прибивает якорем гейт ведомости, а не этот суд.
	l, _ := parseIdentityProbeFateLedger(doc, "deploy")
	var seen int
	for _, co := range l.Elsewhere {
		if strings.HasPrefix(co.Path, "deploy/identity_") {
			seen++
		}
	}
	if seen != 2 {
		t.Fatalf("координаты доводов таблицы А не прочитаны координатами документа: %d из 2", seen)
	}
}

func TestIdentitySignGatesInjection_EachDefectIsNamed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(rows []signRow, f *identitySignFacts)
		want   []string
	}{
		{"снятый гейт в индексе", func(_ []signRow, f *identitySignFacts) {
			f.Tracked["deploy/identity_gone_test.go"] = true
		}, []string{"identity_gone_test.go записан снятым", "в индексе есть"}},
		{"снятый без ссылки", func(r []signRow, _ *identitySignFacts) { r[0].why = "довод словами" },
			[]string{"identity_gone_test.go снят, а довод без ссылки"}},
		{"снятый держится пробой на месте", func(r []signRow, _ *identitySignFacts) { r[0].held = identitySignHeldInPlace },
			[]string{"identity_gone_test.go снят", "держится пробой на месте"}},
		{"снятый без задачи", func(r []signRow, _ *identitySignFacts) { r[0].change = identityNoValue },
			[]string{"identity_gone_test.go", "не названа задача, которая его сняла"}},
		{"оставленный снят", func(_ []signRow, f *identitySignFacts) {
			delete(f.Tracked, "deploy/identity_alpha_test.go")
		}, []string{"identity_alpha_test.go записан живым", "в индексе нет"}},
		{"оставленный с изменением", func(r []signRow, _ *identitySignFacts) { r[1].change = "#1" },
			[]string{"identity_alpha_test.go оставлен, а изменение названо"}},
		{"переписанный без задачи", func(r []signRow, _ *identitySignFacts) { r[2].change = identityNoValue },
			[]string{"identity_gamma_test.go", "которая его переписала"}},
		{"живой без координаты в себя", func(r []signRow, _ *identitySignFacts) {
			r[1].why = "`" + probeFateProseTarget + ":7`"
		}, []string{"identity_alpha_test.go", "обязан указывать в сам гейт"}},
		{"живой держится преемником", func(r []signRow, _ *identitySignFacts) { r[2].held = identitySignHeldBySuccessor },
			[]string{"identity_gamma_test.go жив", "судит свой класс сам"}},
		{"класс не назван", func(r []signRow, _ *identitySignFacts) { r[0].class = identityNoValue },
			[]string{"identity_gone_test.go", "класс отказа не назван"}},
		{"ответ вне словаря", func(r []signRow, _ *identitySignFacts) { r[0].held = "прочее" },
			[]string{"identity_gone_test.go", "«чем держится класс» \"прочее\" вне закрытого словаря"}},
		{"файла преемника нет в пине", func(_ []signRow, f *identitySignFacts) { f.Pinned = map[string][]string{} },
			[]string{"kaname:" + signPinnedPath + "#TestHeld", "в пиненном дереве kaname нет"}},
		{"преемник не объявлен", func(_ []signRow, f *identitySignFacts) {
			f.Pinned["kaname:"+signPinnedPath] = []string{"package held", "// TestHeld снят"}
		}, []string{"объявления TestHeld нет"}},
		{"псевдоним вне перечня", func(r []signRow, _ *identitySignFacts) {
			r[0].why = "`foundation:" + signPinnedPath + "#TestHeld`"
		}, []string{"псевдоним модуля \"foundation\"", "вне перечня"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, f := signLawfulRows(), signLawfulFacts()
			tc.mutate(rows, &f)
			found, _ := signJudge(t, signDoc(rows, nil, ""), f, 3)
			probeFateOneFinding(t, found, tc.want...)
		})
	}
}

func TestIdentitySignGatesInjection_CompositionIsJudged(t *testing.T) {
	t.Parallel()
	t.Run("исход вне словаря", func(t *testing.T) {
		rows := signLawfulRows()
		rows[1].fate = "перевести"
		found, _ := signJudge(t, signDoc(rows, map[string]string{identityFateRemove: "1", identityFateKeep: "0",
			identityFateRewrite: "1"}, "3"), signLawfulFacts(), 3)
		probeFateOneFinding(t, found, "identity_alpha_test.go", "исход \"перевести\" вне закрытого словаря")
	})
	t.Run("гейтов меньше, чем назвал признак", func(t *testing.T) {
		found, _ := signJudge(t, signDoc(signLawfulRows(), nil, ""), signLawfulFacts(), 4)
		probeFateSomeFinding(t, found, "решений записано 3", "назвал гейтов 4")
		probeFateSomeFinding(t, found, "итог разбивки решений 3, а гейтов признака 4")
	})
	t.Run("гейт дважды", func(t *testing.T) {
		rows := append(signLawfulRows(), signLawfulRows()[1])
		found, _ := signJudge(t, signDoc(rows, map[string]string{identityFateRemove: "1", identityFateKeep: "1",
			identityFateRewrite: "1"}, "3"), signLawfulFacts(), 3)
		probeFateOneFinding(t, found, "identity_alpha_test.go назван второй раз")
	})
	t.Run("групповая запись", func(t *testing.T) {
		rows := signLawfulRows()
		rows[0].file = "identity_*_test.go"
		found, _ := signJudge(t, signDoc(rows, map[string]string{identityFateRemove: "0", identityFateKeep: "1",
			identityFateRewrite: "1"}, "2"), signLawfulFacts(), 2)
		probeFateOneFinding(t, found, "Групповая запись", "identity_*_test.go")
	})
	t.Run("строка без ячейки", func(t *testing.T) {
		doc := strings.Replace(signDoc(signLawfulRows(), nil, ""), "| класс отказа | "+identitySignHeldBySuccessor+" |",
			"| "+identitySignHeldBySuccessor+" |", 1)
		found, _ := signJudge(t, doc, signLawfulFacts(), 3)
		probeFateSomeFinding(t, found, "строка решения несёт не 7 ячеек")
	})
	t.Run("таблицы нет", func(t *testing.T) {
		found, c := signJudge(t, probeFateDoc(probeFateLawfulRows(), nil, ""), signLawfulFacts(), 17)
		probeFateOneFinding(t, found, "решения по 17 гейтам признака #1276", "не записаны")
		if c.Rows != 0 {
			t.Fatalf("перепись без таблицы не ноль: %s", c)
		}
	})
	t.Run("таблица дважды", func(t *testing.T) {
		doc := signDoc(signLawfulRows(), nil, "") + "\n| " + strings.Join(identitySignGateHeader, " | ") + " |\n"
		found, _ := signJudge(t, doc, signLawfulFacts(), 3)
		probeFateOneFinding(t, found, "таблица решений по гейтам признака стоит 2 раз")
	})
}

func TestIdentitySignGatesInjection_TotalsThatDisagreeAreAFinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		totals map[string]string
		total  string
		want   []string
	}{
		{"число исхода", map[string]string{identityFateRemove: "2", identityFateKeep: "1", identityFateRewrite: "1"}, "3",
			[]string{"«снять 2»", "строк с этим исходом 1"}},
		{"исход без строки", map[string]string{identityFateRemove: "1", identityFateKeep: "1"}, "3",
			[]string{"нет строки исхода", identityFateRewrite}},
		{"итога нет", map[string]string{identityFateRemove: "1", identityFateKeep: "1", identityFateRewrite: "1"}, "",
			[]string{"нет итоговой строки"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, _ := signJudge(t, signDoc(signLawfulRows(), tc.totals, tc.total), signLawfulFacts(), 3)
			probeFateOneFinding(t, found, tc.want...)
		})
	}
}

// Объявление символа выводится из вида файла; вида, которого разбор не знает,
// — нет ответа, а не «объявлен».
func TestIdentitySignGatesInjection_PinnedDeclarationForms(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path string
		text []string
		want bool
		err  bool
	}{
		{"a_test.go", []string{"func TestHeld(t *testing.T) {"}, true, false},
		{"a_test.go", []string{"// func TestHeld(t *testing.T) {", "func TestHeldToo(t *testing.T) {"}, false, false},
		{"a_test.py", []string{"def test_held():"}, false, false},
		{"b_test.py", []string{"    def TestHeld(self):"}, true, false},
		{"c.sh", []string{"TestHeld() {"}, false, true},
	} {
		got, err := pinnedDeclares(tc.path, tc.text, "TestHeld")
		if got != tc.want || (err != nil) != tc.err {
			t.Fatalf("%s %q: объявлено=%v ошибка=%v, ожидалось %v/%v", tc.path, tc.text, got, err, tc.want, tc.err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Распознаватель имени и формы пробы.

// vendorName — имя службы поставщика из единственного дома меток.
func vendorName() string { return retiredVendorMarks[0] }

// titled — слово с прописной первой буквой.
func titled(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func TestIdentityVendorNameRecogniser_KnowsTheFormsAndTheirTwins(t *testing.T) {
	t.Parallel()
	brand := identityVendorBrand
	cases := []struct {
		line string
		want bool
	}{
		// имя службы — у единственного дома меток
		{"http://kacho-umbrella-" + vendorName() + "-public.kacho.svc:80", true},
		{"const x = " + titled(vendorName()) + "Issuer", true},
		{"setH" + vendorName()[1:] + "ted(true) // " + vendorName() + "te", false},
		// бренд отдельным словом
		{"location ^~ /." + brand + "/idp/public/ {", true},
		{"values.fe3455-" + brand + ".yaml", true},
		{strings.ToUpper(brand) + "_BAND = re.compile(...)", true},
		{"// " + titled(brand) + " stores keep their own DSN", true},
		{"def " + brand + "_lanes(conf):", true},
		// законные близнецы той же формы
		{"memory story Directory category factory", false},
		{"import \"github.com/" + brand + "/fosite\"", false},
		// цена границы названа: бренд прописными, продолженный прописной
		{strings.ToUpper(brand) + "X", true},
	}
	for _, tc := range cases {
		if got := identityVendorNamedIn(tc.line); got != tc.want {
			t.Errorf("identityVendorNamedIn(%q) = %v, ожидалось %v", tc.line, got, tc.want)
		}
	}
}

func TestIdentityBeyondForm_KnowsEveryProbeForm(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"deploy/a_test.go":                            identityFormGoTest,
		"ui-future/host/src/a.test.tsx":               identityFormTSTest,
		"ui-future/scripts/a.test.mjs":                identityFormTSTest,
		"ui-future/e2e/specs/a.spec.ts":               identityFormSpec,
		"deploy/tests/helm/a-test.sh":                 identityFormDeploy,
		"deploy/tests/conformance/x/run.sh":           identityFormDeploy,
		"tests/authz-fixtures/seed.py":                identityFormTests,
		"internal/repohygiene/testdata/x/a.sh.before": identityFormTestdata,
		"ui-future/shared/src/test/address.ts":        identityFormSrcTest,
		"scripts/hooks/a-test.sh":                     identityFormScript,
		".github/scripts/a_test.py":                   identityFormScript,
		".github/scripts/test_a.py":                   identityFormScript,
		"scripts/a.bats":                              identityFormScript,
		// не пробы
		"deploy/tests/helm/README.md":         "",
		"docs/architecture/a.md":              "",
		"deploy/helm/umbrella/values.yaml":    "",
		"ui-future/shared/src/lib/a.ts":       "",
		"internal/repohygiene/gate.go":        "",
		"ui-future/e2e/specs/fixtures.ts":     "",
		"deploy/tests/helm/README.mdx":        "",
		"services/iam/internal/a_testdata.go": "",
	} {
		if got := identityBeyondForm(path); got != want {
			t.Errorf("identityBeyondForm(%q) = %q, ожидалось %q", path, got, want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Синтетика записи Б.

// beyondRow — строка синтетической таблицы Б.
type beyondRow struct{ path, fate, done, frag, why string }

// beyondDoc — законная ведомость и таблица Б с разбивкой; totals == nil —
// разбивка по строкам.
func beyondDoc(rows []beyondRow, totals map[identityBeyondKey]string, total string) string {
	var b strings.Builder
	b.WriteString(probeFateDoc(probeFateLawfulRows(), nil, ""))
	b.WriteString("\n## Вне ведомости\n\n| " + strings.Join(identityBeyondHeader, " | ") + " |\n|---:|---|---|---|---|---|\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "| %d | `%s` | %s | %s | %s | %s |\n", i+1, r.path, r.fate, r.done, r.frag, r.why)
	}
	if totals == nil {
		totals = map[identityBeyondKey]string{}
		counts := map[identityBeyondKey]int{}
		for _, r := range rows {
			counts[identityBeyondKey{r.fate, r.done}]++
		}
		for _, k := range identityBeyondCells {
			totals[k] = fmt.Sprint(counts[k])
		}
		total = fmt.Sprint(len(rows))
	}
	b.WriteString("\n| " + strings.Join(identityBeyondTotals, " | ") + " |\n|---|---|---:|\n")
	for _, k := range identityBeyondCells {
		if v, ok := totals[k]; ok {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", k.Fate, k.Done, v)
		}
	}
	if total != "" {
		fmt.Fprintf(&b, "| **итого** | — | **%s** |\n", total)
	}
	return b.String()
}

// Файлы синтетического дерева записи Б.
const (
	beyondGuard   = "deploy/guard_test.go"
	beyondAddress = "ui-future/x/src/test/address.ts"
	beyondPending = "deploy/tests/helm/pending-test.sh"
	beyondClean   = "deploy/tests/helm/clean-test.sh"
	beyondGone    = "deploy/tests/helm/gone-test.sh"
	beyondLater   = "deploy/tests/helm/later-test.sh"
)

// beyondTexts — тексты законного близнеца. Пробу ведомости, прозу и слова,
// которые на имя только похожи, популяция не берёт.
func beyondTexts() map[string]string {
	return map[string]string{
		beyondGuard:                        "package deploy_test\n\nvar banned = \"" + vendorName() + "-public\"\n",
		beyondAddress:                      "export const retired = \"/." + identityVendorBrand + "/\";\n",
		beyondPending:                      "#!/usr/bin/env bash\n# строка о " + titled(vendorName()) + " в прозе\n",
		beyondClean:                        "#!/usr/bin/env bash\n# переписана\n",
		beyondLater:                        "#!/usr/bin/env bash\n# снимается вместе с предметом\n",
		"deploy/identity_alpha_test.go":    "var x = \"" + vendorName() + "\"\n",
		"docs/x.md":                        vendorName() + "\n",
		"internal/y/memory_test.go":        "// memory story\nimport \"github.com/" + identityVendorBrand + "/fosite\"\n",
		"deploy/helm/umbrella/values.yaml": vendorName() + ":\n  enabled: false\n",
	}
}

// beyondFacts — факты из текстов: индекс — все тексты.
func beyondFacts(texts map[string]string) identityBeyondFacts {
	tracked := map[string]bool{}
	for p := range texts {
		tracked[p] = true
	}
	return identityBeyondFactsOf(tracked, texts, "deploy")
}

func beyondLawfulRows() []beyondRow {
	return []beyondRow{
		{beyondGuard, identityFateKeep, identityNoValue, "`" + vendorName() + "-public`", "страж возврата"},
		{beyondAddress, identityFateKeep, identityNoValue, "`/." + identityVendorBrand + "/`", "страж возврата"},
		{beyondPending, identityFateRewrite, identityBeyondPending, "`" + titled(vendorName()) + "`", "проза — #1276"},
		{beyondClean, identityFateRewrite, identityBeyondDone, identityNoValue, "#2818"},
		{beyondGone, identityFateRemove, identityBeyondDone, identityNoValue, "снята с предметом #2818"},
		{beyondLater, identityFateRemove, identityBeyondPending, identityNoValue, "уходит с предметом #1276"},
	}
}

// beyondJudge — разбор и суд записи Б одним вызовом.
func beyondJudge(t *testing.T, doc string, f identityBeyondFacts) ([]string, identityBeyondCensus) {
	t.Helper()
	l, err := parseIdentityProbeFateLedger(doc, "deploy")
	if err != nil {
		t.Fatalf("разбор синтетики отказал: %v", err)
	}
	return judgeIdentityBeyond(l, f, "deploy", "ведомость.md")
}

func TestIdentityBeyondInjection_LawfulRecordIsSilent(t *testing.T) {
	t.Parallel()
	doc := beyondDoc(beyondLawfulRows(), nil, "")
	found, c := beyondJudge(t, doc, beyondFacts(beyondTexts()))
	if len(found) != 0 {
		t.Fatalf("законная запись дала находки:\n%s", strings.Join(found, "\n"))
	}
	// Популяция — три пробы: проба ведомости, проза, профиль и слова-близнецы
	// в неё не входят.
	if c.Population != 3 || c.Rows != 6 || c.Fragments != 3 || c.Read != 6 {
		t.Fatalf("перепись законной записи неверна: %s", c)
	}
	// Фрагменты таблицы Б координатами документа не читаются: иначе фрагмент,
	// похожий на координату, требовал бы якоря.
	l, _ := parseIdentityProbeFateLedger(doc, "deploy")
	for _, co := range l.Elsewhere {
		if strings.Contains(co.Raw, "pending-test.sh") || strings.Contains(co.Raw, "guard_test.go") {
			t.Fatalf("строка таблицы Б прочитана координатой документа: %s", co.Raw)
		}
	}
}

func TestIdentityBeyondInjection_EachDefectIsNamed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(rows []beyondRow, texts map[string]string, drop map[string]bool)
		want   []string
	}{
		{"проба без строки", func(_ []beyondRow, texts map[string]string, _ map[string]bool) {
			texts["deploy/extra_test.go"] = "// " + vendorName() + "\n"
		}, []string{"deploy/extra_test.go называет поставщика (строки :1)", "строки о нём"}},
		{"снятая в индексе", func(_ []beyondRow, texts map[string]string, _ map[string]bool) {
			texts[beyondGone] = "#!/usr/bin/env bash\n"
		}, []string{beyondGone + " записана снятой, а в индексе есть"}},
		{"ждущая снятия уже снята", func(_ []beyondRow, _ map[string]string, drop map[string]bool) {
			drop[beyondLater] = true
		}, []string{beyondLater + " записана живой", "в индексе её нет"}},
		{"оставленной — исполнение", func(r []beyondRow, _ map[string]string, _ map[string]bool) { r[0].done = identityBeyondDone },
			[]string{beyondGuard, "исполнение \"да\" при исходе \"оставить\""}},
		{"ждущая переписи уже чиста", func(r []beyondRow, texts map[string]string, _ map[string]bool) {
			texts[beyondPending] = "#!/usr/bin/env bash\n"
			r[2].frag = identityNoValue
		}, []string{beyondPending + " ждёт переписи, а поставщика больше не называет"}},
		{"фрагмент не записан", func(r []beyondRow, _ map[string]string, _ map[string]bool) { r[0].frag = identityNoValue },
			[]string{beyondGuard + " называет поставщика (строка 3)", "фрагмент имени не записан"}},
		{"фрагмент ушёл из пробы", func(r []beyondRow, _ map[string]string, _ map[string]bool) { r[1].frag = "`/retired/`" },
			[]string{beyondAddress, "фрагмента «/retired/» нет", "строки :1"}},
		{"фрагмент у чистой пробы", func(r []beyondRow, _ map[string]string, _ map[string]bool) { r[3].frag = "`переписана`" },
			[]string{beyondClean + " поставщика не называет", "пишите «—»"}},
		{"не проба", func(r []beyondRow, _ map[string]string, _ map[string]bool) {
			r[4].path = "deploy/helm/umbrella/values.yaml"
		}, []string{"deploy/helm/umbrella/values.yaml — не проба"}},
		{"проба ведомости", func(r []beyondRow, _ map[string]string, _ map[string]bool) {
			r[4].path = "deploy/identity_alpha_test.go"
		}, []string{"deploy/identity_alpha_test.go — проба самой ведомости"}},
		{"групповая запись", func(r []beyondRow, _ map[string]string, _ map[string]bool) {
			r[4].path = "deploy/tests/helm/*-test.sh"
		}, []string{"не один путь от корня", "Групповая"}},
		{"довод без задачи", func(r []beyondRow, _ map[string]string, _ map[string]bool) {
			r[5].why = "уходит с предметом"
		},
			[]string{beyondLater, "довод не называет задачу"}},
		{"оставленная без довода", func(r []beyondRow, _ map[string]string, _ map[string]bool) { r[0].why = "" },
			[]string{beyondGuard + " — довод пуст"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, texts, drop := beyondLawfulRows(), beyondTexts(), map[string]bool{}
			tc.mutate(rows, texts, drop)
			f := beyondFacts(texts)
			for p := range drop {
				delete(f.Tracked, p)
			}
			// Разбивка — законного близнеца: инъекция меняет строку, а не число.
			totals := map[identityBeyondKey]string{}
			counts := map[identityBeyondKey]int{}
			for _, r := range beyondLawfulRows() {
				counts[identityBeyondKey{r.fate, r.done}]++
			}
			for _, k := range identityBeyondCells {
				totals[k] = fmt.Sprint(counts[k])
			}
			if tc.name == "оставленной — исполнение" {
				// строка с незаконной клеткой в разбивку не идёт — её единица
				// вычтена, чтобы находка была одна и о строке
				totals[identityBeyondKey{identityFateKeep, identityNoValue}] = "1"
			}
			found, _ := beyondJudge(t, beyondDoc(rows, totals, "6"), f)
			if tc.name == "не проба" || tc.name == "проба ведомости" || tc.name == "групповая запись" {
				// строка отвергнута до счёта: разбивка и итог расходятся с ней — это
				// вторая находка той же строки, а не соседняя.
				probeFateSomeFinding(t, found, tc.want...)
				return
			}
			probeFateOneFinding(t, found, tc.want...)
		})
	}
}

func TestIdentityBeyondInjection_CompositionIsJudged(t *testing.T) {
	t.Parallel()
	t.Run("исход вне словаря", func(t *testing.T) {
		rows := beyondLawfulRows()
		rows[0].fate = "перевести"
		found, _ := beyondJudge(t, beyondDoc(rows, map[identityBeyondKey]string{
			{identityFateRemove, identityBeyondDone}: "1", {identityFateRemove, identityBeyondPending}: "1",
			{identityFateRewrite, identityBeyondDone}: "1", {identityFateRewrite, identityBeyondPending}: "1",
			{identityFateKeep, identityNoValue}: "1"}, "6"), beyondFacts(beyondTexts()))
		probeFateOneFinding(t, found, beyondGuard, "исход \"перевести\" вне закрытого словаря")
	})
	t.Run("проба дважды", func(t *testing.T) {
		rows := append(beyondLawfulRows(), beyondLawfulRows()[0])
		found, _ := beyondJudge(t, beyondDoc(rows, nil, ""), beyondFacts(beyondTexts()))
		probeFateSomeFinding(t, found, beyondGuard+" назван второй раз")
	})
	t.Run("разбивка расходится", func(t *testing.T) {
		totals := map[identityBeyondKey]string{}
		for _, k := range identityBeyondCells {
			totals[k] = "1"
		}
		found, _ := beyondJudge(t, beyondDoc(beyondLawfulRows(), totals, "6"), beyondFacts(beyondTexts()))
		probeFateOneFinding(t, found, "«оставить · — — 1»", "строк с этим исходом 2")
	})
	t.Run("итога нет", func(t *testing.T) {
		totals := map[identityBeyondKey]string{}
		for _, k := range identityBeyondCells {
			totals[k] = "1"
		}
		totals[identityBeyondKey{identityFateKeep, identityNoValue}] = "2"
		found, _ := beyondJudge(t, beyondDoc(beyondLawfulRows(), totals, ""), beyondFacts(beyondTexts()))
		probeFateOneFinding(t, found, "нет итоговой строки")
	})
	t.Run("таблицы нет", func(t *testing.T) {
		found, c := beyondJudge(t, probeFateDoc(probeFateLawfulRows(), nil, ""), beyondFacts(beyondTexts()))
		probeFateSomeFinding(t, found, "судьба проб вне ведомости", "таких проб в индексе 3")
		for _, p := range []string{beyondGuard, beyondAddress, beyondPending} {
			probeFateSomeFinding(t, found, p+" называет поставщика")
		}
		if len(found) != 4 || c.Rows != 0 {
			t.Fatalf("без таблицы ожидались 4 находки и ноль строк, получено %d, %s:\n%s", len(found), c,
				strings.Join(found, "\n"))
		}
	})
}

// Цель снятия — популяция ноль и в записи только исполненное: гейт молчит и
// печатает ноль, а не падает.
func TestIdentityBeyondInjection_EmptyPopulationIsTheGoal(t *testing.T) {
	t.Parallel()
	texts := map[string]string{beyondClean: "#!/usr/bin/env bash\n"}
	rows := []beyondRow{
		{beyondClean, identityFateRewrite, identityBeyondDone, identityNoValue, "#2818"},
		{beyondGone, identityFateRemove, identityBeyondDone, identityNoValue, "#2818"},
	}
	found, c := beyondJudge(t, beyondDoc(rows, nil, ""), beyondFacts(texts))
	if len(found) != 0 || c.Population != 0 || c.Read != 1 {
		t.Fatalf("популяция ноль при исполненной записи: находок %d, перепись %s\n%s", len(found), c,
			strings.Join(found, "\n"))
	}
	if !strings.Contains(c.String(), "называют поставщика 0") {
		t.Fatalf("перепись не называет ноль словами: %s", c)
	}
}
