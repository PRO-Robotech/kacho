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
// соседней службы и проза, называющая его верно. Ось «краснеет» одна — та же
// проза с переименованным сегментом. Осей «молчит» четыре, по одной на каждую
// объявленную полосу, и каждая отличается от контроля ровно одним фактом.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
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
// именем платформы) и каталог службы доступа (зовётся именем своего продукта).
// Обе половины нужны, иначе «молчит» было бы неотличимо от «нечего резолвить».
//
// Имена служб здесь СИНТЕТИЧЕСКИЕ, и это не косметика. Гейт судит ВСЁ дерево, а
// этот файл — его часть: возьми фикстура настоящее имя службы, дефектная строка
// стала бы находкой о самой пробе, и гейт краснел бы на собственном
// доказательстве. Наблюдалось ровно это — на первом же прогоне после того, как
// файл попал в индекс. Лечится синтетикой, а не полосой-исключением: полоса по
// имени файла прощала бы и настоящий дефект, написанный в пробе.
func oarBase() map[string]string {
	return map[string]string{
		"services/demo/internal/repo/kacho/pg/repo.go":     "package pg\n",
		"services/demoiam/internal/repo/kaname/pg/repo.go": "package pg\n",
	}
}

// oarWith — контрольная проза ПЛЮС не более одной добавленной строки. Каждая
// инъекция отличается от контроля ровно одним фактом: добавленным токеном.
//
// Контрольная проза несёт ДВЕ координаты — соседней службы (именем платформы) и
// службы доступа (её собственным именем). Обе резолвятся, поэтому полоса
// «резолвится» на контроле ЖИВА, и её молчание на осях не может быть принято за
// молчание мёртвого правила.
func oarWith(extra string) map[string]string {
	files := oarBase()
	prose := "# demo/internal/repo/kacho/pg 312 с\n" +
		"# services/demoiam/internal/repo/kaname/pg/repo.go\n"
	if extra != "" {
		prose += "# " + extra + "\n"
	}
	files["Makefile"] = prose
	return files
}

func TestOverAppliedRenameScanIsProvenByInjection(t *testing.T) {
	control, controlFindings, err := scanOverAppliedRename(oarRootWith(t, oarWith("")))
	if err != nil {
		t.Fatalf("контроль: обход: %v", err)
	}
	t.Logf("контроль: %s", control)
	if len(controlFindings) != 0 {
		t.Fatalf("контроль обязан молчать, а дал находки: %v", controlFindings)
	}
	if control.resolved == 0 {
		t.Fatalf("контроль беспредметен: полоса «резолвится» пуста, значит правило не "+
			"исполнялось ни разу и молчание осей ничего не докажет. Перепись: %s", control)
	}

	band := func(c overAppliedCensus, name string) int {
		switch name {
		case "moduleForm":
			return c.moduleForm
		case "shortAnchor":
			return c.shortAnchor
		case "anchorUnresolved":
			return c.anchorUnresolved
		case "resolved":
			return c.resolved
		}
		return -1
	}

	cases := []struct {
		name    string
		extra   string
		red     bool
		wantIn  string
		wantWhy string
	}{
		{
			name:   "ОСЬ: тот же токен с переименованным сегментом — находка с координатой",
			extra:  "demo/internal/repo/kaname/pg 312 с",
			red:    true,
			wantIn: "demo/internal/repo/kaname/pg",
		},
		{
			name:    "близнец: перечисление служб через косую черту — молчит (короткий якорь)",
			extra:   "единый пол безопасности, идентичный kaname/geo/demo/registry.",
			wantWhy: "shortAnchor",
		},
		{
			name:    "близнец: путь модуля Go — молчит (резолвится модулем, не деревом)",
			extra:   "github.com/PRO-Robotech/kacho/services/demo/internal/repo/kaname/pg",
			wantWhy: "moduleForm",
		},
		{
			name:    "близнец: якорь не резолвится вовсе — молчит (токен путём дерева не является)",
			extra:   "services/nosuch/internal/apps/kaname/api/gate.go",
			wantWhy: "anchorUnresolved",
		},
		{
			name:    "близнец: ЕЩЁ ОДИН свой каталог службы доступа — молчит (резолвится)",
			extra:   "services/demoiam/internal/repo/kaname/pg",
			wantWhy: "resolved",
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
				if !strings.Contains(got, "Makefile:3") {
					t.Fatalf("находка не называет файл и строку: %s", got)
				}
				if census.resolved != control.resolved {
					t.Fatalf("инъекция задела соседнюю полосу: «резолвится» %d против %d на "+
						"контроле — красное могло прийти не от проверяемого факта",
						census.resolved, control.resolved)
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
