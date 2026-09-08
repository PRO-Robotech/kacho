// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_global_defaults_agree_test.go — умолчания службы личности объявлены
// ДВАЖДЫ, и это осознанно; расходиться им нельзя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЙ ДВА
//
// Авторитетное — в умбрелле (`global.kacho.identity`). Только оттуда значения
// доходят до подчарта ПРОВАЙДЕРА, где по ним считается отпечаток содержимого
// настроек: значения `global`, объявленные подчартом, соседям не раздаются —
// это свойство Helm, а не наше решение.
//
// Второе — в самом подчарте kaname, и нужно оно ровно для того, чтобы
// `helm template charts/kaname` рендерился САМ ПО СЕБЕ. На таком рендере
// стоит самопроверка гейта сетевых политик: без умолчаний страж рендера
// отказывает, вывод пуст, и гейт «пропускает» внесённый дефект — перестаёт
// краснеть там, где обязан. Поймано на себе: случай «метка сайдкара
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
// ─────────────────────────────────────────────────────────────────────────────
// ВТОРАЯ ПРОВЕРКА ФАЙЛА: умолчания адреса обратных вызовов
//
// Адрес слушателя хуков прежде выводился ПРЯМО из значений подчарта — из имени
// подчарта и из порта его внутреннего слушателя. В контексте подчарта
// провайдера этих значений нет, поэтому умолчания стали константами, и связь
// «порт полосы = порт слушателя» разорвалась: сменив порт слушателя, полосу
// увели бы в никуда, и это не заметил бы никто — обратные вызовы просто
// перестали бы доходить.
//
// Здесь эта связь восстановлена утверждением: константы обязаны совпадать с
// тем, что подчарт объявляет о себе.
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
					"обязаны быть в ОБОИХ местах: в умбрелле, чтобы дойти до подчарта "+
					"провайдера, и в подчарте, чтобы он рендерился сам по себе",
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

// Умолчания адреса обратных вызовов — те же константы, что стоят в
// `kacho.identity.hooksAuthority`. Выписаны здесь намеренно: проверка сверяет
// ДВЕ независимые записи, и вычислять одну из другой значило бы сверять запись
// с самой собой.
const (
	hooksAuthorityDefaultChartName = "kaname"
	hooksAuthorityDefaultPort      = 9092
)

func TestCallbackAuthorityDefaultsMatchWhatTheSubchartSaysAboutItself(t *testing.T) {
	subchart := readYAML(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"))

	name, _ := subchart["name"].(string)
	if name == "" {
		t.Fatalf("подчарт не объявляет `name` — узел адреса обратных вызовов " +
			"сверять не с чем; «ноль находок» здесь неотличимо от «ноль прочитанного»")
	}
	if name != hooksAuthorityDefaultChartName {
		t.Errorf("узел адреса обратных вызовов собран из %q, а подчарт называет себя %q — "+
			"обратные вызовы уедут на несуществующий Service и перестанут доходить",
			hooksAuthorityDefaultChartName, name)
	}

	svc, _ := subchart["service"].(map[string]any)
	internal, _ := svc["internal"].(map[string]any)
	port, ok := internal["hooksHttpPort"].(int)
	if !ok {
		t.Fatalf("подчарт не объявляет service.internal.hooksHttpPort числом — " +
			"порт полосы сверять не с чем")
	}
	if port != hooksAuthorityDefaultPort {
		t.Errorf("полоса обратных вызовов идёт на порт %d, а слушатель подчарта объявлен "+
			"на %d. Прежде порт полосы ВЫВОДИЛСЯ из этого значения; после переноса "+
			"содержимого в global он стал константой, поэтому связь держит эта проверка",
			hooksAuthorityDefaultPort, port)
	}

	t.Logf("осмотрено: имя подчарта %q, порт слушателя хуков %d", name, port)
}
