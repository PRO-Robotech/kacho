// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_foreign_identity_test.go — ПОСАДКА ЛИЧНОСТИ И ФЛАГИ ЧУЖИХ СЛУЖБ
// СВЯЗАНЫ, И СВЯЗЬ ДЕРЖИТ МАШИНА (kacho#2735, стадия S2 приёмки Ф4д §8).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Стенд посадки `own` складывается из боевого профиля и накладки. Накладка
// объявляет посадку, но чужие службы личности НЕ выключает — наследует их
// включёнными снизу. Наблюдаемый исход: стенд, который человека проверяет СВОЕЙ
// полосой, всё равно поднимает чужого поставщика, его базу и его экран входа,
// и они стоят живыми рядом — вторая действующая дверь в ту же систему, о которой
// никто не решал.
//
// Признак на день заведения (kacho `main` @ `f445aaaa554`), цепочка стенда
// `own` = values.prod.yaml + values.own.yaml:
//
//	helm template kacho-umbrella ./helm/umbrella -n kacho \
//	  -f helm/umbrella/values.prod.yaml -f helm/umbrella/values.own.yaml \
//	  | grep -c '^# Source: kacho-umbrella/charts/\(kratos\|hydra\|pg-kratos\|pg-hydra\|kratos-selfservice-ui\)/'
//	    → 30
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ОДНА ПРАВКА ПРОФИЛЯ
//
// Выключить флаги — работа на один раз; связь «посадка ↔ флаги» после неё не
// держится ничем. Четыре стража настроек чужой службы открываются условием
// «стенд поднимает чужую службу» — на стенде, где она выключена, они законно
// молчат, и ПОВТОРНОЕ включение чужой службы рядом с посадкой `own` не дало бы
// ни одного красного: каждый страж честно проверил бы то, что сам объявил.
// Здесь судится ровно то, чего не судит ни один из них, — согласие посадки и
// флагов.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОБЕ СТОРОНЫ, А НЕ ОДНА
//
// Проверка, ловящая только «own с включённым чужим», зеленела бы на стенде,
// который выключил чужую службу, оставшись на посадке `external`, — то есть на
// стенде, которому проверять человека НЕЧЕМ. Поэтому у гейта две находки, и обе
// про одно: посадка и флаги обязаны говорить об одном.
//
// ─────────────────────────────────────────────────────────────────────────────
// СОСТАВ ЧУЖОГО ВЫВОДИТСЯ ИЗ ДЕРЕВА
//
// Список компонентов выписать здесь нельзя: выписанный разойдётся с Chart.yaml
// молча, и гейт перестанет видеть тот компонент, который переименовали. Он
// выводится: зависимости зонта, чей репозиторий принадлежит поставщику, их
// базы (`pg-<имя>`) и подчарт экрана входа, лежащий в `charts/` без объявления.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// foreignIdentityRepoMark — признак репозитория поставщика чужой службы личности.
const foreignIdentityRepoMark = "ory.sh"

// foreignIdentityComponent — компонент чужой службы личности и путь его флага
// включения в слитых значениях стенда.
type foreignIdentityComponent struct {
	Name string
	Flag []string
}

func (c foreignIdentityComponent) String() string {
	return fmt.Sprintf("%s (%s)", c.Name, strings.Join(c.Flag, "."))
}

// foreignIdentityComponents — состав чужого, выведенный из дерева.
func foreignIdentityComponents(t *testing.T) []foreignIdentityComponent {
	t.Helper()

	chart := readYAML(t, filepath.Join(umbrellaDir, "Chart.yaml"))
	deps, _ := chart["dependencies"].([]any)
	if len(deps) == 0 {
		t.Fatalf("в %s не прочитано ни одной зависимости — состав чужого взять неоткуда",
			filepath.Join(umbrellaDir, "Chart.yaml"))
	}

	// Имя, под которым зависимость видна значениям, — это alias, если он есть.
	nameOf := func(m map[string]any) string {
		if alias, _ := m["alias"].(string); strings.TrimSpace(alias) != "" {
			return alias
		}
		name, _ := m["name"].(string)
		return name
	}

	var provider []string
	byName := map[string]bool{}
	for _, d := range deps {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		n := nameOf(m)
		byName[n] = true
		if repo, _ := m["repository"].(string); strings.Contains(repo, foreignIdentityRepoMark) {
			provider = append(provider, n)
		}
	}
	sort.Strings(provider)
	if len(provider) == 0 {
		t.Fatalf("среди зависимостей зонта нет ни одной с репозиторием %q — "+
			"поставщик переехал либо признак перестал его узнавать; «чужого не найдено» "+
			"здесь неотличимо от «чужое не прочитано»", foreignIdentityRepoMark)
	}

	out := make([]foreignIdentityComponent, 0, 2*len(provider)+1)
	for _, n := range provider {
		out = append(out, foreignIdentityComponent{Name: n, Flag: []string{n, "enabled"}})
		// База компонента объявлена алиасом `pg-<имя>` — включается отдельным
		// флагом и без него остаётся стоять при выключенной службе.
		if db := "pg-" + n; byName[db] {
			out = append(out, foreignIdentityComponent{Name: db, Flag: []string{db, "enabled"}})
		}
	}

	// Подчарты, лежащие в `charts/` БЕЗ объявления зависимостью: helm грузит их
	// всегда, и флаг у них свой, внутри собственного узла значений.
	entries, err := os.ReadDir(filepath.Join(umbrellaDir, "charts"))
	if err != nil {
		t.Fatalf("каталог подчартов не читается: %v — предпосылка исчезла", err)
	}
	for _, e := range entries {
		if !e.IsDir() || byName[e.Name()] {
			continue
		}
		var belongs bool
		for _, n := range provider {
			if strings.HasPrefix(e.Name(), n+"-") {
				belongs = true
			}
		}
		if !belongs {
			continue
		}
		key := flagBearingTopLevelKey(t, filepath.Join(umbrellaDir, "charts", e.Name(), "values.yaml"))
		out = append(out, foreignIdentityComponent{
			Name: e.Name(),
			Flag: []string{e.Name(), key, "enabled"},
		})
	}
	return out
}

// flagBearingTopLevelKey — узел верхнего уровня файла значений подчарта, несущий
// ключ включения. Читается, а не выписывается: выписанный разошёлся бы с
// шаблоном молча, и гейт перестал бы видеть флаг, не сказав об этом.
func flagBearingTopLevelKey(t *testing.T, path string) string {
	t.Helper()
	tree := readYAML(t, path)
	var found []string
	for k, v := range tree {
		node, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := node["enabled"]; ok {
			found = append(found, k)
		}
	}
	sort.Strings(found)
	if len(found) != 1 {
		t.Fatalf("%s несёт %d узлов верхнего уровня с ключом `enabled` (%s) — путь флага "+
			"включения выводится неоднозначно", path, len(found), strings.Join(found, ", "))
	}
	return found[0]
}

// standPosture — что стенд объявил каждой половине.
type standPosture struct {
	IAM  string
	Edge string
}

func (p standPosture) both(v string) bool { return p.IAM == v && p.Edge == v }
func (p standPosture) any(v string) bool  { return p.IAM == v || p.Edge == v }

// identityPostureFinding — находка о стенде.
type identityPostureFinding struct {
	Stack  string
	Reason string
	Text   string
}

const (
	ownRaisesForeign      = "посадка `own`, а чужая служба личности включена"
	externalRaisesNothing = "посадка `external`, а чужой службы личности нет"
)

// judgeStandIdentity — ЧИСТЫЙ предикат: посадка стенда против включённого
// чужого. Отдельная функция, а не тело проверки: инъекция обязана звать ЕЁ ЖЕ,
// иначе доказывает свойство своей копии.
//
// Стенд, половины которого разошлись, здесь НЕ судится: это предмет соседа
// (helm/umbrella/identity_posture_profiles_test.go), и второй вердикт об одном
// предмете разъехался бы с первым.
func judgeStandIdentity(stack string, p standPosture, enabled []string) *identityPostureFinding {
	switch {
	case p.any("own") && len(enabled) > 0:
		return &identityPostureFinding{
			Stack:  stack,
			Reason: ownRaisesForeign,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `own` (iam=%s gateway=%s), "+
				"но поднимает чужую службу личности: %s.\nЭто ВТОРАЯ действующая дверь в ту же "+
				"систему рядом с нашей полосой, и её никто не решал: накладка посадки чужие "+
				"службы не выключает — наследует их включёнными из слоя под собой.\n"+
				"Исход один: объявить флаги ложью в том слое, который объявил посадку, — "+
				"выключение в боевом слое сняло бы чужую службу и у стендов, которые на ней "+
				"стоят по решению",
				stack, p.IAM, p.Edge, strings.Join(enabled, ", ")),
		}
	case p.both("external") && len(enabled) == 0:
		return &identityPostureFinding{
			Stack:  stack,
			Reason: externalRaisesNothing,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `external` обеим половинам, "+
				"но не поднимает НИ ОДНОГО компонента чужой службы личности.\nТакому стенду "+
				"проверять человека нечем: обе половины ждут внешнего поставщика, которого на "+
				"стенде нет. Исходов два: перевести стенд на `own` либо вернуть флаги",
				stack),
		}
	}
	return nil
}

// standIdentityCensus — объём осмотренного.
type standIdentityCensus struct {
	Stacks     int
	Own        int
	External   int
	Mixed      int
	Components int
}

func (c standIdentityCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · на посадке own %d · на посадке external %d · "+
		"половины разошлись %d · компонентов чужой службы личности в дереве %d",
		c.Stacks, c.Own, c.External, c.Mixed, c.Components)
}

// TestOwnPostureRaisesNoForeignIdentityService — посадка и флаги говорят об одном.
func TestOwnPostureRaisesNoForeignIdentityService(t *testing.T) {
	stacks := deployStacks(t)
	components := foreignIdentityComponents(t)

	// Умолчания половин живут в профилях ПОДЧАРТОВ: стенд, посадку не
	// объявивший, получает их, а не пустоту.
	iamDefault := postureDefaultOf(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"),
		"config", "authn", "identityProvider")
	edgeDefault := postureDefaultOf(t, filepath.Join("..", "gateway", "deploy", "values.yaml"),
		"authn", "identityProvider")
	if iamDefault == "" || edgeDefault == "" {
		t.Fatalf("умолчание посадки не прочитано у одной из половин (iam=%q gateway=%q) — "+
			"вердикт о стендах, посадку не объявивших, был бы вынесен неизвестно о чём",
			iamDefault, edgeDefault)
	}

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	census := standIdentityCensus{Stacks: len(names), Components: len(components)}
	var findings []identityPostureFinding

	for _, name := range names {
		merged, _, _ := mergedValuesOfStack(t, stacks[name])

		p := standPosture{
			IAM:  stringAt(merged, iamDefault, "kaname", "config", "authn", "identityProvider"),
			Edge: stringAt(merged, edgeDefault, "api-gateway", "authn", "identityProvider"),
		}

		var enabled []string
		for _, c := range components {
			if v, ok := lookup(merged, c.Flag...); ok {
				if on, isBool := v.(bool); isBool && on {
					enabled = append(enabled, c.Name)
				}
			}
		}

		switch {
		case p.both("own"):
			census.Own++
		case p.both("external"):
			census.External++
		default:
			census.Mixed++
		}

		t.Logf("  %s: iam=%s gateway=%s · чужого включено %d (%s)",
			name, p.IAM, p.Edge, len(enabled), strings.Join(enabled, ", "))

		if f := judgeStandIdentity(name, p, enabled); f != nil {
			findings = append(findings, *f)
		}
	}

	t.Logf("перепись: %s · находок %d", census, len(findings))

	// Предпосылка обхода, обе стороны. Ноль стендов на любой из посадок значит,
	// что гейт судит ПУСТОТУ по этой оси и молчал бы о вернувшемся дефекте.
	if census.Own == 0 {
		t.Fatalf("ни один стенд не стоит на посадке `own` (осмотрено %d) — гейт судил бы "+
			"пустоту: «находок ноль» здесь неотличимо от «нечего было проверять»", census.Stacks)
	}
	if census.External == 0 {
		t.Fatalf("ни один стенд не стоит на посадке `external` (осмотрено %d) — вторая "+
			"сторона предиката осталась без единого носителя", census.Stacks)
	}
	if census.Components < 2 {
		t.Fatalf("состав чужой службы личности выведен из дерева как %d компонент(ов) — "+
			"признак перестал их узнавать", census.Components)
	}

	for _, f := range findings {
		t.Errorf("%s: %s", f.Reason, f.Text)
	}
}

// stringAt — строка по пути либо умолчание.
func stringAt(tree map[string]any, def string, path ...string) string {
	v, ok := lookup(tree, path...)
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// postureDefaultOf — значение по пути в файле значений подчарта.
func postureDefaultOf(t *testing.T, path string, keys ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- путь собственного дерева
	if err != nil {
		t.Fatalf("профиль %s не читается: %v", path, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("профиль %s не разбирается как YAML: %v", path, err)
	}
	v, ok := lookup(tree, keys...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
