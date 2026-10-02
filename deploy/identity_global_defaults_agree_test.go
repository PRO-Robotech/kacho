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
	"path/filepath"
	"sort"
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
