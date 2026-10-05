// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_global_defaults_agree_test.go — умолчания службы личности объявлены
// ДВАЖДЫ, и это осознанно; расходиться им нельзя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЙ ДВА
//
// Авторитетное — в умбрелле (`global.kacho.identity`): его читают подчарт службы
// доступа и стражи рендера зонта. Второе — в самом подчарте kaname, и нужно оно
// ровно для того, чтобы `helm template charts/kaname` рендерился САМ ПО СЕБЕ. На
// таком рендере стоит самопроверка гейта сетевых политик: без умолчаний страж
// рендера отказывает, вывод пуст, и гейт «пропускает» внесённый дефект —
// перестаёт краснеть там, где обязан. Поймано на себе: случай «метка сайдкара
// безусловна» стал ПРОПУСКАТЬ ровно после переноса значений в `global`.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТО ОПАСНО И ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ
//
// Два места об одном предмете расходятся молча, а копия подчарта ПЕРЕКРЫВАЕТСЯ
// умбреллой — то есть правка в ней не даёт никакого наблюдаемого эффекта на
// стенде и выглядит применённой. Поэтому: каждый ключ, объявленный подчартом,
// обязан существовать в умбрелле и совпадать с ним ЗНАЧЕНИЕМ.
//
// Обратное включение НЕ требуется: умбрелла вправе объявить больше — лишнее
// подчарту для одиночного рендера не нужно.
//
// Вторая проверка файла — умолчания адреса обратных вызовов поставщика
// личности — снята вместе с их читателем (kacho#2818): слушателя обратных
// вызовов у службы нет (kaname#363), и копия подчарта этого узла больше не
// несёт.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// identityGlobalsPath — узел значений, о согласии которого идёт речь.
var identityGlobalsPath = []string{"global", "kacho", "identity"}

// flatten раскладывает дерево значений в плоские пары «путь → значение».
// Сравнивать надо ЛИСТЬЯ: сравнение поддеревьев целиком объявило бы
// расхождением любой лишний ключ умбреллы, а он законен.
func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, sub := range t {
			flatten(prefix+"."+k, sub, out)
		}
	default:
		out[prefix] = fmt.Sprintf("%v", t)
	}
}

func TestIdentityGlobalDefaultsOfTheSubchartAgreeWithTheUmbrella(t *testing.T) {
	umbrella := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	subchart := readYAML(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"))

	pick := func(tree map[string]any, where string) map[string]any {
		cur := any(tree)
		for _, p := range identityGlobalsPath {
			m, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("%s: узел %q не разбирается как отображение — "+
					"сверять нечего, и это отказ, а не успех", where, p)
			}
			cur, ok = m[p]
			if !ok {
				t.Fatalf("%s: не объявлен узел %s — умолчания службы личности "+
					"обязаны быть в ОБОИХ местах: в умбрелле, авторитетно, и в подчарте, "+
					"чтобы он рендерился сам по себе",
					where, filepath.Join(identityGlobalsPath...))
			}
		}
		m, _ := cur.(map[string]any)
		return m
	}

	u := map[string]string{}
	s := map[string]string{}
	flatten("", pick(umbrella, "values.yaml умбреллы"), u)
	flatten("", pick(subchart, "values.yaml подчарта kaname"), s)
	if len(s) == 0 || len(u) == 0 {
		t.Fatalf("листьев прочитано: у умбреллы %d, у подчарта %d — пустой результат "+
			"НЕ означает «всё хорошо»", len(u), len(s))
	}

	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		uv, ok := u[k]
		if !ok {
			t.Errorf("подчарт объявляет %s%s, а умбрелла — нет. Копия подчарта "+
				"ПЕРЕКРЫВАЕТСЯ умбреллой, поэтому такое значение не действует ни "+
				"на одном стенде и при этом выглядит применённым",
				filepath.Join(identityGlobalsPath...), k)
			continue
		}
		if uv != s[k] {
			t.Errorf("значение %s%s разошлось: умбрелла %q, подчарт %q. "+
				"Действует умбрелла; правка в подчарте наблюдаемого эффекта не даёт",
				filepath.Join(identityGlobalsPath...), k, uv, s[k])
		}
	}

	t.Logf("осмотрено листьев: у умбреллы %d, у подчарта %d; сверено %d",
		len(u), len(s), len(keys))
}

// ─── узлы `global`, которые объявляет ТОЛЬКО зонтик (NTF-1 D2; замысел З28, CX1-112)
//
// Перечень узлов, а не одна константа: `identity` — согласие объявления подчарта
// kaname с зонтиком по значению (выше); `notifications` и `spiffe` — объявлений в
// `values.yaml` подчартов НОЛЬ. Умолчание `global`, объявленное подчартом, видно
// лишь этому подчарту: сосед и зонтик видят «нет ключа», и
// `global.kacho.notifications.modules.kaname.enabled: false` в
// `charts/kaname/values.yaml` при молчащем зонтике выключило бы kaname, сохранив
// его в перечне источников notify. Одиночные рендеры подчартов получают эти узлы
// слоем своей пробы (нога notify без зонтика) либо обёрткой (одиночный рендер
// kaname).

// umbrellaOnlyGlobalNodes — узлы `global.kacho`, которых подчарт не объявляет.
var umbrellaOnlyGlobalNodes = []string{"notifications", "spiffe"}

// subchartValuesFiles — значения подчартов: `charts/*/values.yaml` зонтика и
// `values.yaml` чарта notify.
func subchartValuesFiles(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "values.yaml"))
	files = append(files, filepath.Join(notifyChartDir, "values.yaml"))
	sort.Strings(files)
	return files
}

// umbrellaOnlyGlobalFindings — объявления узлов umbrellaOnlyGlobalNodes в файлах.
func umbrellaOnlyGlobalFindings(t *testing.T, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		vals := readYAML(t, f)
		for _, node := range umbrellaOnlyGlobalNodes {
			if _, ok := lookup(vals, "global", "kacho", node); ok {
				out = append(out, fmt.Sprintf("%s объявляет global.kacho.%s — узел объявляет только зонтик (CX1-112)", f, node))
			}
		}
	}
	return out
}

func TestUmbrellaOnlyGlobalNodesAreNotDeclaredBySubcharts(t *testing.T) {
	files := subchartValuesFiles(t)
	if len(files) < 2 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файлов значений подчартов %d — обход пуст либо сломан", len(files))
	}
	findings := umbrellaOnlyGlobalFindings(t, files)
	for _, f := range findings {
		t.Error(f)
	}
	t.Logf("узлы: identity (согласие по значению), %v (объявлений в подчартах 0); осмотрено файлов значений подчартов %d; находок %d",
		umbrellaOnlyGlobalNodes, len(files), len(findings))
}

// TestUmbrellaOnlyGlobalNodesGateFindsASubchartDeclaration — инъекция: ключ
// переопределения kaname в значениях подчарта → находка с путём файла; близнец —
// тот же ключ в values.yaml зонтика (файл не подчарта) → молчание.
func TestUmbrellaOnlyGlobalNodesGateFindsASubchartDeclaration(t *testing.T) {
	dir := t.TempDir()
	body := []byte("global:\n  kacho:\n    notifications:\n      modules:\n        kaname:\n          enabled: false\n")
	inj := filepath.Join(dir, "charts", "kaname", "values.yaml")
	if err := os.MkdirAll(filepath.Dir(inj), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inj, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if f := umbrellaOnlyGlobalFindings(t, []string{inj}); len(f) != 1 || !strings.Contains(f[0], inj) {
		t.Errorf("инъекция «переопределение kaname в значениях подчарта»: находки %v — ждали одну с путём %s", f, inj)
	}
	twin := filepath.Join(umbrellaDir, "values.yaml")
	if _, ok := lookup(readYAML(t, twin), "global", "kacho", "notifications", "enabled"); !ok {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнецу нечего судить — в %s нет global.kacho.notifications.enabled", twin)
	}
	files := subchartValuesFiles(t)
	for _, f := range files {
		if f == twin {
			t.Errorf("перечень файлов подчартов несёт values.yaml зонтика — близнец судил бы зонтик как подчарт")
		}
	}
	t.Logf("инъекция → находка с путём; близнец — узел в values.yaml зонтика, файл вне перечня подчартов (%d файлов)", len(files))
}
