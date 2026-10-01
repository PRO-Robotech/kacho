// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_foreign_identity_test.go — ПОСАДКА ЛИЧНОСТИ И ЧУЖАЯ СЛУЖБА
// ЛИЧНОСТИ ГОВОРЯТ ОБ ОДНОМ, И СВЯЗЬ ДЕРЖИТ МАШИНА (kacho#2735, стадия S2
// приёмки Ф4д §8; с kacho#1276 — чужой службы в дереве нет вовсе).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Стенд посадки `own` складывался из боевого профиля и накладки. Накладка
// объявляла посадку, но чужие службы личности НЕ выключала — наследовала их
// включёнными снизу. Наблюдаемый исход: стенд, который человека проверяет СВОЕЙ
// полосой, всё равно поднимал чужого поставщика, его базу и его экран входа,
// и они стояли живыми рядом — вторая действующая дверь в ту же систему, о
// которой никто не решал.
//
// Признак на день заведения (kacho `main` @ `f445aaaa554`), цепочка стенда
// `own` = values.prod.yaml + values.own.yaml:
//
//	helm template kacho-umbrella ./helm/umbrella -n kacho \
//	  -f helm/umbrella/values.prod.yaml -f helm/umbrella/values.own.yaml \
//	  | grep -c '^# Source: kacho-umbrella/charts/\(kratos\|hydra\|pg-kratos\|pg-hydra\|kratos-selfservice-ui\)/'
//	    → 30
//
// Сначала чужой стек выключили в базе зонта для всех стендов (#2735, #2777),
// затем сняли физически (#1276): объявления зависимостей, их архивы, их базы
// и экран входа, лежавший в `charts/` без объявления. Отсюда две стороны
// гейта, и обе про одно — посадка и чужая служба обязаны говорить об одном.
//
// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА ДЕРЕВА — ЧУЖОЙ СЛУЖБЫ В ЗОНТЕ НЕТ
//
// Компонент чужой службы, вернувшийся в зонт, — находка сам по себе, без
// всякого стенда: посадки, которая бы его читала, у службы доступа нет
// (kaname#363), а у края она одна на деле — `own`. Флаг выключения тут не
// спасает: условие зависимости, чей путь в значениях не найден, helm читает
// как «включено», и возвращённое объявление подняло бы стек молча.
//
// Прежде у этой стороны была ведомость остатка — компонент, снять который
// пока нельзя, с причиной. Она истекла вместе со своим предметом: снимать
// больше нечего, и запись в ней объявляла бы решённым то, что решать некому.
//
// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА СТЕНДА — ПОСАДКА `external` ПРОВЕРЯТЬ ЧЕЛОВЕКА НЕЧЕМ
//
// Стенд, чьи обе половины ждут внешнего поставщика, в дереве без поставщика
// человека не проверит ничем. Поэтому такой стенд — находка; носителей этой
// стороны в таблице стендов НОЛЬ, и это цель, а не слепота: посадку `own`
// объявляют все стенды. Перепись такой ноль принимает ровно при одном условии
// — на `own` стоят ВСЕ осмотренные стенды, — а способность этой стороны упасть
// доказывает инъекция (TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll).
//
// ─────────────────────────────────────────────────────────────────────────────
// СОСТАВ ЧУЖОГО ВЫВОДИТСЯ ИЗ ДЕРЕВА
//
// Список компонентов выписать здесь нельзя: выписанный разойдётся с Chart.yaml
// молча, и гейт перестанет видеть тот компонент, который переименовали. Он
// выводится: зависимости зонта, чей репозиторий принадлежит поставщику, их
// базы (`pg-<имя>`) и подчарт, лежащий в `charts/` без объявления, чьё `name`
// называет службу поставщика. Число «ноль» здесь — исход, а не «не прочитано»:
// обход печатает, сколько зависимостей и подчартов он осмотрел, и падает на
// пустом обходе. Способность найти вернувшийся компонент доказывает инъекция
// на копии зонта (TestOwnPostureForeignIdentityGate_FindsAComponentReturnedToTheTree).
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// foreignIdentityRepoMark — признак репозитория поставщика чужой службы личности.
const foreignIdentityRepoMark = "ory.sh"

// foreignIdentityChartName — признак подчарта поставщика, лежащего в `charts/`
// без объявления: его `name` называет службу поставщика. Граница слова — начало
// имени либо дефис: без неё признаком стало бы любое имя, где эти буквы стоят
// внутри слова.
var foreignIdentityChartName = regexp.MustCompile(`(?i)(^|-)(kratos|hydra)(-|$)`)

// foreignIdentityComponent — компонент чужой службы личности в зонте и путь
// его флага включения.
type foreignIdentityComponent struct {
	Name string
	Flag []string
	// Undeclared — подчарт из `charts/`, не объявленный зависимостью зонта.
	Undeclared bool
}

func (c foreignIdentityComponent) String() string {
	return fmt.Sprintf("%s (%s)", c.Name, strings.Join(c.Flag, "."))
}

// foreignIdentityCensus — объём осмотренного обходом состава.
type foreignIdentityCensus struct {
	Dependencies int // зависимостей зонта прочитано
	Subcharts    int // каталогов подчартов в charts/ прочитано
}

// foreignIdentityComponents — состав чужого в зонте, выведенный из дерева.
func foreignIdentityComponents(t *testing.T) ([]foreignIdentityComponent, foreignIdentityCensus) {
	t.Helper()
	return foreignIdentityComponentsIn(t, umbrellaDir)
}

// foreignIdentityComponentsIn — то же на произвольном каталоге зонта: инъекция
// зовёт ЭТУ ЖЕ функцию на копии, а не свою копию функции.
func foreignIdentityComponentsIn(t *testing.T, dir string) ([]foreignIdentityComponent, foreignIdentityCensus) {
	t.Helper()
	var census foreignIdentityCensus

	chart := readYAML(t, filepath.Join(dir, "Chart.yaml"))
	deps, _ := chart["dependencies"].([]any)
	if len(deps) == 0 {
		t.Fatalf("в %s не прочитано ни одной зависимости — состав чужого взять неоткуда, "+
			"и «чужого нет» здесь неотличимо от «ничего не прочитано»", filepath.Join(dir, "Chart.yaml"))
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
		census.Dependencies++
		n := nameOf(m)
		byName[n] = true
		if repo, _ := m["repository"].(string); strings.Contains(repo, foreignIdentityRepoMark) {
			provider = append(provider, n)
		}
	}
	sort.Strings(provider)

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
	entries, err := os.ReadDir(filepath.Join(dir, "charts"))
	if err != nil {
		t.Fatalf("каталог подчартов не читается: %v — предпосылка исчезла", err)
	}
	for _, e := range entries {
		if !e.IsDir() || byName[e.Name()] {
			continue
		}
		census.Subcharts++
		// Компонент называется `name` СВОЕГО Chart.yaml, а не каталогом: под этим
		// именем helm кладёт его значения, и флаг включения живёт там же. Каталог —
		// наш путь и переименовывается независимо (#2759).
		sub := filepath.Join(dir, "charts", e.Name())
		name := subchartName(t, sub)
		if byName[name] || !foreignIdentityChartName.MatchString(name) {
			continue
		}
		key := flagBearingTopLevelKey(t, filepath.Join(sub, "values.yaml"))
		out = append(out, foreignIdentityComponent{
			Name:       name,
			Flag:       []string{name, key, "enabled"},
			Undeclared: true,
		})
	}
	if census.Subcharts == 0 {
		t.Fatalf("в %s/charts не прочитано ни одного каталога подчарта — экран входа "+
			"поставщика, лежащий там без объявления, искать было негде", dir)
	}
	return out, census
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

// both — обе половины стенда стоят на посадке v.
func (l identityLanding) both(v string) bool { return l.IAM == v && l.Edge == v }

// any — хотя бы одна половина стенда стоит на посадке v.
func (l identityLanding) any(v string) bool { return l.IAM == v || l.Edge == v }

// identityPostureFinding — находка о стенде либо о зонте.
type identityPostureFinding struct {
	Stack  string
	Reason string
	Text   string
}

const (
	foreignInTree         = "компонент чужой службы личности лежит в зонте"
	ownRaisesForeign      = "посадка `own`, а чужая служба личности включена"
	externalRaisesNothing = "посадка `external`, а чужой службы личности нет"
)

// judgeForeignIdentityInTree — ЧИСТЫЙ предикат стороны дерева: каждый
// компонент чужой службы в зонте — находка.
func judgeForeignIdentityInTree(components []foreignIdentityComponent) []identityPostureFinding {
	out := make([]identityPostureFinding, 0, len(components))
	for _, c := range components {
		where := "объявлен зависимостью зонта"
		if c.Undeclared {
			where = "лежит в charts/ без объявления — helm грузит такой подчарт всегда"
		}
		out = append(out, identityPostureFinding{
			Reason: foreignInTree,
			Text: fmt.Sprintf("компонент чужой службы личности %s %s.\n"+
				"Посадки, которая бы его читала, нет: у службы доступа посадка одна — своя "+
				"(kaname#363), у края все стенды таблицы стоят на `own`. Флаг выключения "+
				"его не держит: условие зависимости, чей путь в значениях не найден, helm "+
				"читает как «включено». Снимите компонент (снятие — kacho#1276).", c, where),
		})
	}
	return out
}

// judgeStandIdentity — ЧИСТЫЙ предикат стороны стенда: посадка стенда против
// включённого чужого. Отдельная функция, а не тело проверки: инъекция обязана
// звать ЕЁ ЖЕ, иначе доказывает свойство своей копии.
//
// Стенд, половины которого разошлись, по второй стороне здесь НЕ судится: это
// предмет соседа (helm/umbrella/identity_posture_profiles_test.go), и второй
// вердикт об одном предмете разъехался бы с первым.
func judgeStandIdentity(stack string, p identityLanding, enabled []string) []identityPostureFinding {
	var out []identityPostureFinding
	switch {
	case p.any(landingOwn) && len(enabled) > 0:
		out = append(out, identityPostureFinding{
			Stack:  stack,
			Reason: ownRaisesForeign,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `own` (iam=%s gateway=%s), "+
				"но поднимает чужую службу личности: %s.\n"+
				"Это ВТОРАЯ действующая дверь в ту же систему рядом с нашей полосой.",
				stack, p.IAM, p.Edge, strings.Join(enabled, ", ")),
		})
	case p.both(landingExternal) && len(enabled) == 0:
		out = append(out, identityPostureFinding{
			Stack:  stack,
			Reason: externalRaisesNothing,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `external` обеим половинам, "+
				"но не поднимает НИ ОДНОГО компонента чужой службы личности.\nТакому стенду "+
				"проверять человека нечем: обе половины ждут внешнего поставщика, которого на "+
				"стенде нет, а в дереве его нет вовсе (kacho#1276). Переведите стенд на `own`",
				stack),
		})
	}
	return out
}

// standIdentityCensus — объём осмотренного.
type standIdentityCensus struct {
	Stacks       int
	Own          int
	External     int
	Mixed        int
	Dependencies int
	Subcharts    int
	Components   int
}

func (c standIdentityCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · на посадке own %d · на посадке external %d · "+
		"половины разошлись %d · зависимостей зонта прочитано %d · подчартов charts/ без "+
		"объявления прочитано %d · компонентов чужой службы личности в зонте %d",
		c.Stacks, c.Own, c.External, c.Mixed, c.Dependencies, c.Subcharts, c.Components)
}

// TestOwnPostureRaisesNoForeignIdentityService — посадка и чужая служба говорят
// об одном: чужой службы в зонте нет, и ни один стенд её не ждёт.
func TestOwnPostureRaisesNoForeignIdentityService(t *testing.T) {
	stacks := deployStacks(t)
	components, walked := foreignIdentityComponents(t)
	umbrellaBase := readFileForTest(t, filepath.Join(umbrellaDir, "values.yaml"))

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	census := standIdentityCensus{
		Stacks: len(names), Dependencies: walked.Dependencies,
		Subcharts: walked.Subcharts, Components: len(components),
	}
	findings := judgeForeignIdentityInTree(components)

	for _, name := range names {
		merged, _, _ := mergedValuesOfStack(t, stacks[name])

		// Посадку читает ЕДИНСТВЕННЫЙ читатель пакета (identityLandingOfChain) —
		// тот же, что отбирает стенды для стражей личности, — из тех же слоёв,
		// из которых выше сложены флаги: значения зонта, затем профили цепочки.
		texts := []string{umbrellaBase}
		for _, prof := range stacks[name] {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		p := identityLandingOfChain(t, texts)
		if !p.lands() {
			t.Fatalf("стенд %q: посадка не прочитана ни у цепочки, ни у баз подчартов — вердикт о "+
				"нём был бы вынесен неизвестно о чём", name)
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
		case p.both(landingOwn):
			census.Own++
		case p.both(landingExternal):
			census.External++
		default:
			census.Mixed++
		}

		t.Logf("  %s: iam=%s gateway=%s · чужого включено %d (%s)",
			name, p.IAM, p.Edge, len(enabled), strings.Join(enabled, ", "))

		findings = append(findings, judgeStandIdentity(name, p, enabled)...)
	}

	t.Logf("перепись: %s · находок %d", census, len(findings))

	// Предпосылка обхода. Ноль стендов на посадке `own` значит, что гейт судит
	// ПУСТОТУ и молчал бы о вернувшемся дефекте.
	if census.Own == 0 {
		t.Fatalf("ни один стенд не стоит на посадке `own` (осмотрено %d) — гейт судил бы "+
			"пустоту: «находок ноль» здесь неотличимо от «нечего было проверять»", census.Stacks)
	}
	// Сторона стенда без носителей законна ТОЛЬКО как цель: все стенды на `own`.
	if census.External == 0 && census.Own != census.Stacks {
		t.Fatalf("ни один стенд не стоит на посадке `external`, и не все стоят на `own` "+
			"(осмотрено %d, на own %d, половины разошлись %d) — сторона стенда осталась без "+
			"носителя не потому, что её предмет исчерпан", census.Stacks, census.Own, census.Mixed)
	}
	if census.External == 0 {
		t.Logf("сторона стенда (посадка `external` без поставщика) носителей не имеет: "+
			"на `own` стоят все %d стендов — её способность упасть держит инъекция "+
			"TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll", census.Stacks)
	}

	for _, f := range findings {
		t.Errorf("%s: %s", f.Reason, f.Text)
	}
}
