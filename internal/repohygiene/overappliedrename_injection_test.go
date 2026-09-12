// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// overappliedrename_injection_test.go — доказательство того, что проверка
// переприменённого переименования СПОСОБНА упасть, и того, что она молчит на
// законных близнецах.
//
// # Почему синтетический корень, а не правка дерева
//
// Проверка читает дерево, которое читают и соседние сессии. Внести в него дефект
// ради доказательства значило бы править общее состояние. Разбор вынесен в
// чистую функцию над ПРОИЗВОЛЬНЫМ корнем, и сюда подаётся корень, собранный в
// каталоге прогона.
//
// # Каждая инъекция меняет ровно один факт против контроля
//
// Контроль стоит первым и обязан МОЛЧАТЬ: у него в дереве есть настоящий каталог
// соседней службы, вложенный модуль службы доступа и проза, называющая ОБА
// авторитета верно — координатой дерева и путём модуля. Обе полосы «резолвится»
// на контроле живы, поэтому молчание осей не может быть принято за молчание
// мёртвого правила.
//
// Осей «краснеет» три — по одной на каждый род находки: та же проза с
// переименованным сегментом (авторитет дерева), тот же дефект, записанный путём
// модуля (авторитет объявлений), и пакет ВЛОЖЕННОГО модуля, названный через путь
// родительского. Осей «молчит» шесть, по одной на каждую объявленную полосу, и
// каждая отличается от контроля ровно одним фактом.
//
// # Инъекция обязана ронять ТОЛЬКО проверяемое
//
// Каждая красная ось утверждает ровно одну находку И неизменность соседних
// полос против контроля: иначе красное могло прийти от соседа, а новое правило
// осталось бы вакуумным, не показав этого ничем.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// oarRootWith собирает корень из перечисленных файлов. Файлов ровно столько,
// сколько подано: перепись тогда прямо называет, что прочитано ровно то, что
// подано.
func oarRootWith(t *testing.T, files map[string]string) *treecorpus.Tree {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("подготовка %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
	}
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	return tree
}

// oarBase — общая часть КАЖДОГО корня инъекции: каталог соседней службы (зовётся
// именем платформы), каталог службы доступа (зовётся именем своего продукта) и
// ДВА объявления модуля — родительское в корне и вложенное у службы доступа.
//
// Объявления обязательны, а не декоративны: без них авторитета для полосы «путь
// модуля» не существует вовсе, каждый её токен уходит в «авторитет чужой», и
// молчание оси было бы молчанием мёртвого правила. Вложенное объявление — ещё и
// единственный способ представить род находки «пакет вложенного модуля через
// путь родительского».
//
// Имена служб здесь СИНТЕТИЧЕСКИЕ, и это не косметика. Гейт судит ВСЁ дерево, а
// этот файл — его часть: возьми фикстура настоящее имя службы, дефектная строка
// стала бы находкой о самой пробе, и гейт краснел бы на собственном
// доказательстве. Наблюдалось ровно это — на первом же прогоне после того, как
// файл попал в индекс. Лечится синтетикой, а не полосой-исключением: полоса по
// имени файла прощала бы и настоящий дефект, написанный в пробе.
//
// Пути модулей взяты настоящие: предмет проверки — раскладка «родитель и
// вложенный», а она держится не именами, а тем, что объявлений два и корни у
// них вложены друг в друга.
func oarBase() map[string]string {
	return map[string]string{
		"go.mod":                  "module github.com/PRO-Robotech/kacho\n\ngo 1.26.0\n",
		"services/demoiam/go.mod": "module github.com/PRO-Robotech/kaname\n\ngo 1.26.0\n",
		"services/demo/internal/repo/kacho/pg/repo.go":     "package pg\n",
		"services/demoiam/internal/repo/kaname/pg/repo.go": "package pg\n",
	}
}

// oarWith — контрольная проза ПЛЮС не более одной добавленной строки. Каждая
// инъекция отличается от контроля ровно одним фактом: добавленным токеном.
//
// Контрольная проза несёт ТРИ координаты — соседней службы (именем платформы),
// службы доступа (её собственным именем) и путь модуля службы доступа, который
// резолвится объявлением. Первые две держат живой полосу «резолвится» авторитета
// дерева, третья — ту же полосу авторитета модуля.
func oarWith(extra string) map[string]string {
	files := oarBase()
	prose := "# demo/internal/repo/kacho/pg 312 с\n" +
		"# services/demoiam/internal/repo/kaname/pg/repo.go\n" +
		"# github.com/PRO-Robotech/kaname/internal/repo/kaname\n"
	if extra != "" {
		prose += "# " + extra + "\n"
	}
	files["Makefile"] = prose
	return files
}

func TestOverAppliedRenameScanIsProvenByInjection(t *testing.T) {
	t.Parallel()
	control, controlFindings, err := scanOverAppliedRename(oarRootWith(t, oarWith("")))
	if err != nil {
		t.Fatalf("контроль: обход: %v", err)
	}
	t.Logf("контроль: %s", control)
	if len(controlFindings) != 0 {
		t.Fatalf("контроль обязан молчать, а дал находки: %v", controlFindings)
	}
	if control.modulesDeclared != 2 {
		t.Fatalf("контроль беспредметен: объявлений модуля %d вместо двух, значит раскладка "+
			"«родитель и вложенный» не собрана и род находки о вложенном модуле "+
			"непредставим. Перепись: %s", control.modulesDeclared, control)
	}
	if control.resolved == 0 {
		t.Fatalf("контроль беспредметен: полоса «резолвится» пуста, значит правило не "+
			"исполнялось ни разу и молчание осей ничего не докажет. Перепись: %s", control)
	}
	if control.moduleJudged == 0 {
		t.Fatalf("контроль беспредметен: полосой модуля не судим ни один токен, значит "+
			"новое правило вакуумно, и его молчание на близнецах ничего не докажет. "+
			"Перепись: %s", control)
	}

	band := func(c overAppliedCensus, name string) int {
		switch name {
		case "moduleForeign":
			return c.moduleForeign
		case "moduleOwnSegment":
			return c.moduleOwnSegment
		case "shortAnchor":
			return c.shortAnchor
		case "anchorUnresolved":
			return c.anchorUnresolved
		case "resolved":
			return c.resolved
		case "moduleJudged":
			return c.moduleJudged
		}
		return -1
	}

	cases := []struct {
		name     string
		extra    string
		red      bool
		wantIn   string
		wantAlso string
		wantWhy  string
	}{
		{
			name:   "ОСЬ (дерево): тот же токен с переименованным сегментом — находка с координатой",
			extra:  "demo/internal/repo/kaname/pg 312 с",
			red:    true,
			wantIn: "demo/internal/repo/kaname/pg",
		},
		{
			name:     "ОСЬ (модуль): ТОТ ЖЕ дефект, записанный путём модуля — находка, а не слепая зона",
			extra:    "github.com/PRO-Robotech/kacho/services/demo/internal/repo/kaname/pg",
			red:      true,
			wantIn:   "github.com/PRO-Robotech/kacho/services/demo/internal/repo/kaname/pg",
			wantAlso: "services/demo/internal/repo",
		},
		{
			name:     "ОСЬ (модуль): пакет ВЛОЖЕННОГО модуля через путь родительского — находка",
			extra:    "github.com/PRO-Robotech/kacho/services/demoiam/internal/repo/kaname",
			red:      true,
			wantIn:   "github.com/PRO-Robotech/kacho/services/demoiam/internal/repo/kaname",
			wantAlso: "github.com/PRO-Robotech/kaname",
		},
		{
			name:    "близнец: перечисление служб через косую черту — молчит (короткий якорь)",
			extra:   "единый пол безопасности, идентичный kaname/geo/demo/registry.",
			wantWhy: "shortAnchor",
		},
		{
			name:    "близнец: голый owner/repo — молчит (авторитет не наш, модуль не объявлен)",
			extra:   "PRO-Robotech/kaname",
			wantWhy: "moduleForeign",
		},
		{
			name:    "близнец: сегмент — имя самого объявленного модуля — молчит",
			extra:   "github.com/PRO-Robotech/kaname/internal/other",
			wantWhy: "moduleOwnSegment",
		},
		{
			name:    "близнец: веб-адрес GitHub — молчит той же полосой, без перечня глаголов",
			extra:   "github.com/PRO-Robotech/kaname/issues/2224",
			wantWhy: "moduleOwnSegment",
		},
		{
			name:    "близнец: якорь не резолвится вовсе — молчит (токен путём дерева не является)",
			extra:   "services/nosuch/internal/apps/kaname/api/gate.go",
			wantWhy: "anchorUnresolved",
		},
		{
			name:    "близнец: синтетика чужой инъекции путём модуля — молчит той же полосой",
			extra:   "github.com/PRO-Robotech/kacho/services/nosuch/internal/apps/kaname/config",
			wantWhy: "anchorUnresolved",
		},
		{
			name:    "близнец: ЕЩЁ ОДИН свой каталог службы доступа — молчит (резолвится)",
			extra:   "services/demoiam/internal/repo/kaname/pg",
			wantWhy: "resolved",
		},
		{
			name:    "близнец: путь СОБСТВЕННОГО модуля службы — молчит (резолвится объявлением)",
			extra:   "github.com/PRO-Robotech/kaname/internal/repo/kaname/pg",
			wantWhy: "moduleJudged",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			census, findings, err := scanOverAppliedRename(oarRootWith(t, oarWith(tc.extra)))
			if err != nil {
				t.Fatalf("обход: %v", err)
			}
			t.Logf("перепись: %s", census)

			if tc.red {
				if len(findings) != 1 {
					t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
				}
				got := findings[0].String()
				if !strings.Contains(got, tc.wantIn) {
					t.Fatalf("находка не называет координату %q: %s", tc.wantIn, got)
				}
				if tc.wantAlso != "" && !strings.Contains(got, tc.wantAlso) {
					t.Fatalf("находка не называет %q — читатель не узнает, чем именно "+
						"координата не резолвится: %s", tc.wantAlso, got)
				}
				if !strings.Contains(got, "Makefile:4") {
					t.Fatalf("находка не называет файл и строку: %s", got)
				}
				for _, b := range []string{"moduleForeign", "moduleOwnSegment", "shortAnchor",
					"anchorUnresolved", "resolved"} {
					if band(census, b) != band(control, b) {
						t.Fatalf("инъекция задела соседнюю полосу: %s %d против %d на "+
							"контроле — красное могло прийти не от проверяемого факта",
							b, band(census, b), band(control, b))
					}
				}
				return
			}

			if len(findings) != 0 {
				t.Fatalf("законный близнец объявлен находкой: %v", findings)
			}
			if got, was := band(census, tc.wantWhy), band(control, tc.wantWhy); got <= was {
				t.Fatalf("молчание не объяснено: полоса %s не приросла (%d против %d на "+
					"контроле), то есть добавленный токен до её правила не дошёл. Перепись: %s",
					tc.wantWhy, got, was, census)
			}
		})
	}
}

// TestOverAppliedRenameRefusesAnEmptyWalk — обход без единого файла есть ОТКАЗ,
// а не тихий успех: иначе «находок ноль» неотличимо от «прочитано ноль».
func TestOverAppliedRenameRefusesAnEmptyWalk(t *testing.T) {
	t.Parallel()
	tree := oarRootWith(t, map[string]string{})
	_, _, err := scanOverAppliedRename(tree)
	if err == nil {
		t.Fatal("пустой обход обязан быть отказом, а вернулся успех")
	}
	if !strings.Contains(err.Error(), "беспредметен") {
		t.Fatalf("отказ не называет причину: %v", err)
	}
}

// TestOverAppliedRenameSelfExpiresWhenTheNameLeavesCoordinates — предпосылка
// гейта: если имя продукта ушло из координат вовсе, перепись обязана дать ноль,
// и гейт дерева просит себя перечитать (Fatal), а не зеленеет.
func TestOverAppliedRenameSelfExpiresWhenTheNameLeavesCoordinates(t *testing.T) {
	t.Parallel()
	tree := oarRootWith(t, map[string]string{
		"services/demo/internal/repo/kacho/pg/repo.go": "package pg\n",
		"Makefile": "# demo/internal/repo/kacho/pg 312 с\n",
	})
	census, findings, err := scanOverAppliedRename(tree)
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("на дереве без имени продукта находок быть не может: %v", findings)
	}
	if census.tokensWithName != 0 {
		t.Fatalf("перепись насчитала %d токенов с сегментом %q там, где его нет: %s",
			census.tokensWithName, standaloneProductSegment, census)
	}
}

// TestOverAppliedRenameModuleAuthorityComesFromTheTree — авторитет полосы «путь
// модуля» берётся из ОБЪЯВЛЕНИЙ того же дерева, а не выписан константой. Убери
// объявления — полоса обязана целиком уйти в «авторитет чужой», а не молча
// начать судить своим умолчанием.
//
// Без этой пробы «модуль объявлен» и «модуль подразумевается» неотличимы: на
// настоящем дереве объявления есть всегда, поэтому подмена авторитета
// константой прошла бы зелёной.
func TestOverAppliedRenameModuleAuthorityComesFromTheTree(t *testing.T) {
	t.Parallel()
	with := oarWith("github.com/PRO-Robotech/kacho/services/demo/internal/repo/kaname/pg")

	withDecl, withFindings, err := scanOverAppliedRename(oarRootWith(t, with))
	if err != nil {
		t.Fatalf("обход с объявлениями: %v", err)
	}
	if len(withFindings) != 1 {
		t.Fatalf("с объявлениями ожидалась одна находка, получено %d: %v",
			len(withFindings), withFindings)
	}

	without := map[string]string{}
	for rel, body := range with {
		if strings.HasSuffix(rel, "go.mod") {
			continue
		}
		without[rel] = body
	}
	noDecl, noFindings, err := scanOverAppliedRename(oarRootWith(t, without))
	if err != nil {
		t.Fatalf("обход без объявлений: %v", err)
	}
	t.Logf("с объявлениями: %s", withDecl)
	t.Logf("без объявлений: %s", noDecl)

	if noDecl.modulesDeclared != 0 {
		t.Fatalf("объявления сняты, а перепись насчитала %d — авторитет взят не из дерева: %s",
			noDecl.modulesDeclared, noDecl)
	}
	if len(noFindings) != 0 {
		t.Fatalf("без объявлений модуля судить нечем, а находки есть: %v", noFindings)
	}
	if noDecl.moduleJudged != 0 {
		t.Fatalf("без объявлений полосой модуля судим %d токен(ов) — значит путь модуля "+
			"резолвится выписанной константой, а не объявлением дерева: %s",
			noDecl.moduleJudged, noDecl)
	}
	if noDecl.moduleForeign <= withDecl.moduleForeign {
		t.Fatalf("без объявлений полоса «авторитет чужой» не приросла (%d против %d) — "+
			"пути модуля ушли не туда, и молчание объяснено неверно: %s",
			noDecl.moduleForeign, withDecl.moduleForeign, noDecl)
	}
}
