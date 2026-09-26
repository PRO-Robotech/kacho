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
// ОСТАТОК НАЗЫВАЕТСЯ, А НЕ ПРЯЧЕТСЯ
//
// Компонент чужой службы, который на стенде посадки `own` снять пока нельзя,
// ВЕДЁТСЯ ведомостью: запись называет стенд, компонент и причину. И запись
// САМОИСТЕКАЕТ — ведомость, называющая компонент, которого на стенде уже нет,
// есть находка: исключению нечего исключать, и оно бы пережило свой предмет.
//
// Сегодня ведомость ПУСТА, и это исход, а не упущение (#2735, #2777): чужой
// стек выключен в базе зонта для всех стендов, и записи стенда `own`
// (поставщик, его база, его экран входа) истекли вместе со своим предметом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОБЕ СТОРОНЫ, А НЕ ОДНА
//
// Проверка, ловящая только «own с включённым чужим», зеленела бы на стенде,
// который выключил чужую службу, оставшись на посадке `external`, — то есть на
// стенде, которому проверять человека НЕЧЕМ. Поэтому у гейта две находки, и обе
// про одно: посадка и флаги обязаны говорить об одном.
//
// Носителей второй стороны в таблице стендов сегодня НОЛЬ, и это цель #2735, а
// не слепота: посадку `own` объявляют все стенды. Поэтому перепись такой ноль
// принимает ровно при одном условии — на `own` стоят ВСЕ осмотренные стенды, —
// а способность второй стороны упасть доказывает инъекция
// (TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll): та же
// функция суждения, стенд `external` без поставщика.
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

// both — обе половины стенда стоят на посадке v.
func (l identityLanding) both(v string) bool { return l.IAM == v && l.Edge == v }

// any — хотя бы одна половина стенда стоит на посадке v.
func (l identityLanding) any(v string) bool { return l.IAM == v || l.Edge == v }

// identityPostureFinding — находка о стенде.
type identityPostureFinding struct {
	Stack  string
	Reason string
	Text   string
}

const (
	ownRaisesForeign      = "посадка `own`, а чужая служба личности включена"
	externalRaisesNothing = "посадка `external`, а чужой службы личности нет"
	remainderIsStale      = "запись ведомости пережила свой предмет"
)

// identityRemainder — ОСТАТОК, снять который сегодня нельзя, с причиной.
type identityRemainder struct {
	Component string
	Reason    string
}

// foreignIdentityRemainders — ВЕДОМОСТЬ ОСТАТКА по стендам.
//
// Запись здесь — не послабление, а объявление: «этот компонент чужой службы
// стоит на этом стенде намеренно, вот почему, и вот чем держится предикат его
// снятия». Запись, чей компонент на стенде уже выключен, — находка: она
// объявляла бы решённым то, что решать больше нечего.
var foreignIdentityRemainders = map[string][]identityRemainder{}

// judgeStandIdentity — ЧИСТЫЙ предикат: посадка стенда против включённого
// чужого и против ведомости остатка. Отдельная функция, а не тело проверки:
// инъекция обязана звать ЕЁ ЖЕ, иначе доказывает свойство своей копии.
//
// Стенд, половины которого разошлись, по второй стороне здесь НЕ судится: это
// предмет соседа (helm/umbrella/identity_posture_profiles_test.go), и второй
// вердикт об одном предмете разъехался бы с первым.
func judgeStandIdentity(stack string, p identityLanding, enabled []string, remainders []identityRemainder) []identityPostureFinding {
	onStand := map[string]bool{}
	for _, c := range enabled {
		onStand[c] = true
	}
	decided := map[string]string{}
	var out []identityPostureFinding

	for _, r := range remainders {
		// САМОИСТЕЧЕНИЕ. Запись об остатке, которого на стенде нет, — находка:
		// либо компонент уже снят и ведомость пережила свой предмет, либо его
		// переименовали и ведомость перестала его узнавать.
		if !onStand[r.Component] || !p.any(landingOwn) {
			out = append(out, identityPostureFinding{
				Stack:  stack,
				Reason: remainderIsStale,
				Text: fmt.Sprintf("ведомость остатка называет компонент %q стенда %q, "+
					"а на стенде его нет (посадка iam=%s gateway=%s, включено: %s).\n"+
					"Исключению нечего исключать: запись объявляет решённым то, что решать "+
					"больше нечего, и переживёт любой следующий разбор. Снимите её.",
					r.Component, stack, p.IAM, p.Edge, joinOrNone(enabled)),
			})
			continue
		}
		decided[r.Component] = r.Reason
	}

	var undecided []string
	for _, c := range enabled {
		if _, ok := decided[c]; !ok {
			undecided = append(undecided, c)
		}
	}

	switch {
	case p.any(landingOwn) && len(undecided) > 0:
		out = append(out, identityPostureFinding{
			Stack:  stack,
			Reason: ownRaisesForeign,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `own` (iam=%s gateway=%s), "+
				"но поднимает чужую службу личности, о которой не решал никто: %s.\n"+
				"Это ВТОРАЯ действующая дверь в ту же систему рядом с нашей полосой: накладка "+
				"посадки чужие службы не выключает — наследует их включёнными из слоя под "+
				"собой.\nИсходов два: объявить флаг ложью в том слое, который объявил посадку "+
				"(выключение в боевом слое сняло бы чужую службу и у стендов, которые на ней "+
				"стоят по решению), либо внести компонент в ведомость остатка с причиной и "+
				"предикатом снятия.\nРешённый остаток этого стенда: %s",
				stack, p.IAM, p.Edge, strings.Join(undecided, ", "), joinOrNone(decidedComponentNames(decided))),
		})
	case p.both(landingExternal) && len(enabled) == 0:
		out = append(out, identityPostureFinding{
			Stack:  stack,
			Reason: externalRaisesNothing,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `external` обеим половинам, "+
				"но не поднимает НИ ОДНОГО компонента чужой службы личности.\nТакому стенду "+
				"проверять человека нечем: обе половины ждут внешнего поставщика, которого на "+
				"стенде нет. Исходов два: перевести стенд на `own` либо вернуть флаги",
				stack),
		})
	}
	return out
}

// joinOrNone — перечень либо прямое слово о пустоте: пустая строка в тексте
// находки читается как «здесь ничего не подставилось».
func joinOrNone(v []string) string {
	if len(v) == 0 {
		return "ничего"
	}
	return strings.Join(v, ", ")
}

func decidedComponentNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// standIdentityCensus — объём осмотренного.
type standIdentityCensus struct {
	Stacks     int
	Own        int
	External   int
	Mixed      int
	Components int
	Remainders int
}

func (c standIdentityCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · на посадке own %d · на посадке external %d · "+
		"половины разошлись %d · компонентов чужой службы личности в дереве %d · "+
		"записей ведомости остатка %d",
		c.Stacks, c.Own, c.External, c.Mixed, c.Components, c.Remainders)
}

// TestOwnPostureRaisesNoForeignIdentityService — посадка и флаги говорят об одном.
func TestOwnPostureRaisesNoForeignIdentityService(t *testing.T) {
	stacks := deployStacks(t)
	components := foreignIdentityComponents(t)
	umbrellaBase := readFileForTest(t, filepath.Join(umbrellaDir, "values.yaml"))

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	census := standIdentityCensus{Stacks: len(names), Components: len(components)}
	for _, rs := range foreignIdentityRemainders {
		census.Remainders += len(rs)
	}
	var findings []identityPostureFinding

	for _, name := range names {
		merged, _, _ := mergedValuesOfStack(t, stacks[name])

		// Посадку читает ЕДИНСТВЕННЫЙ читатель пакета (identityLandingOfChain) —
		// тот же, что отбирает стенды для стражей личности, — из тех же слоёв,
		// из которых выше сложены флаги: значения зонта, затем профили цепочки.
		// Умолчание подчарта он подставляет, когда о посадке молчит вся цепочка,
		// а не каждая половина порознь; для этого гейта это одно и то же, потому
		// что объявить одну половину без второй профиль не может — это отказ
		// TestIdentityPostureHalvesOfAProfileAgree (helm/umbrella).
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

		findings = append(findings, judgeStandIdentity(name, p, enabled, foreignIdentityRemainders[name])...)
	}

	t.Logf("перепись: %s · находок %d", census, len(findings))

	// Предпосылка обхода, обе стороны. Ноль стендов на любой из посадок значит,
	// что гейт судит ПУСТОТУ по этой оси и молчал бы о вернувшемся дефекте.
	if census.Own == 0 {
		t.Fatalf("ни один стенд не стоит на посадке `own` (осмотрено %d) — гейт судил бы "+
			"пустоту: «находок ноль» здесь неотличимо от «нечего было проверять»", census.Stacks)
	}
	// Вторая сторона без носителей законна ТОЛЬКО как цель: все стенды на `own`.
	// Любой иной ноль (стенды с разошедшимися половинами и ни одного `external`)
	// значит, что вторую сторону судить не по чему, и об этом сказано вслух.
	if census.External == 0 && census.Own != census.Stacks {
		t.Fatalf("ни один стенд не стоит на посадке `external`, и не все стоят на `own` "+
			"(осмотрено %d, на own %d, половины разошлись %d) — вторая сторона предиката "+
			"осталась без носителя не потому, что её предмет исчерпан", census.Stacks,
			census.Own, census.Mixed)
	}
	if census.External == 0 {
		t.Logf("вторая сторона (посадка `external` без поставщика) носителей не имеет: "+
			"на `own` стоят все %d стендов — её способность упасть держит инъекция "+
			"TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll", census.Stacks)
	}
	if census.Components < 2 {
		t.Fatalf("состав чужой службы личности выведен из дерева как %d компонент(ов) — "+
			"признак перестал их узнавать", census.Components)
	}
	// Ведомость, называющая стенд, которого в таблице состава нет, судит
	// несуществующее и молчала бы об этом.
	for stack := range foreignIdentityRemainders {
		if _, ok := stacks[stack]; !ok {
			t.Errorf("ведомость остатка называет стенд %q, которого в таблице состава нет", stack)
		}
	}

	for _, f := range findings {
		t.Errorf("%s: %s", f.Reason, f.Text)
	}
}
