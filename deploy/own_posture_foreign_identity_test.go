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
// Снять сегодня можно не всё: чужой экран входа — единственное, чем человек на
// стенде заходит, пока в консоли нет своей полосы экранов. Гейт, требующий
// «ноль чужого», краснел бы на стенде, который иначе остаётся без церемонии
// входа, — и его бы отключили. Гейт, о таком остатке молчащий, объявил бы
// стенд чистым.
//
// Поэтому остаток ВЕДЁТСЯ: запись ведомости называет стенд, компонент и причину.
// И запись САМОИСТЕКАЕТ — ведомость, называющая компонент, которого на стенде
// уже нет, есть находка: исключению нечего исключать, и оно бы пережило свой
// предмет.
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
	remainderIsStale      = "запись ведомости пережила свой предмет"
	postureUndeclared     = "посадка краю не объявлена ни одним слоем цепочки"
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
// foreignIdentityRemainders — ВЕДОМОСТЬ ОСТАТКА, объявленная для ВСЕХ стендов.
//
// # Почему ключа-стенда больше нет
//
// Ключ был стендом, пока посадку `own` объявлял один стенд из семи. Теперь её
// объявляют все семь, и у ключа-стенда два исхода: либо три записи
// превращаются в двадцать одну, либо шесть стендов дают находки.
//
// Двадцать одна запись — ЦЕНА: один и тот же довод, переписанный семь раз;
// предикат снятия, который придётся удовлетворить семь раз; восьмой стенд,
// молча дающий три находки в день своего заведения; и размер ведомости,
// следящий за числом стендов вместо своего предмета. Но главное — такая
// ведомость СОЛГАЛА БЫ О СВОЁМ ПРЕДМЕТЕ: остаток не есть «этот стенд держит
// чужое хранилище личности», остаток есть «в консоли ещё нет полосы экранов
// входа». Это ОДИН факт о продукте, а не семь о стендах.
//
// Ключ «все стенды» — тоже ЦЕНА, и она называется: теряется различение по
// стендам. Стенд, законно выключивший компонент, больше не отличается от
// прочих, а самоистечение слабеет — запись, переставшая быть при деле на
// шести стендах из семи, останется зелёной из-за седьмого.
//
// Эта цена уплачена ИЗМЕРЕНИЕМ, а не обещанием: самоистечение считает РАДИУС —
// на скольких судимых стендах компонент ещё поднимается, — и печатает его в
// переписи. Сжатие 7 → 1 видно глазом; 7 → 0 есть находка.
//
// Запись САМОИСТЕКАЕТ: ведомость, называющая компонент, которого не поднимает
// ни один судимый стенд, — находка. Исключению нечего исключать.
var foreignIdentityRemainders = []identityRemainder{
	{Component: "kratos", Reason: "хранилище личности чужого экрана входа: " +
		"полосы экранов входа в консоли (`ui-future`) ещё нет, и заход человека на этом " +
		"стенде идёт только через чужой экран. Предикат снятия: консоль обслуживает вход " +
		"своей полосой, и gateway/deploy/login_console_test.go признаёт её церемонией"},
	{Component: "pg-kratos", Reason: "база того же хранилища: выключается вместе с ним, " +
		"не раньше — иначе служба поднимется без данных"},
	{Component: "kratos-selfservice-ui", Reason: "сам чужой экран входа. Снять его сегодня " +
		"значит оставить боевой стенд без церемонии входа; держит это " +
		"gateway/deploy/login_console_test.go"},
}

// judgeStandIdentity — ЧИСТЫЙ предикат: посадка стенда против включённого
// чужого и против ведомости остатка. Отдельная функция, а не тело проверки:
// инъекция обязана звать ЕЁ ЖЕ, иначе доказывает свойство своей копии.
//
// Стенд, половины которого разошлись, по второй стороне здесь НЕ судится: это
// предмет соседа (helm/umbrella/identity_posture_profiles_test.go), и второй
// вердикт об одном предмете разъехался бы с первым.
func judgeStandIdentity(stack string, p standPosture, enabled []string, remainders []identityRemainder) []identityPostureFinding {
	decided := map[string]string{}
	for _, r := range remainders {
		decided[r.Component] = r.Reason
	}
	var out []identityPostureFinding

	var undecided []string
	for _, c := range enabled {
		if _, ok := decided[c]; !ok {
			undecided = append(undecided, c)
		}
	}

	switch {
	case p.any("own") && len(undecided) > 0:
		out = append(out, identityPostureFinding{
			Stack:  stack,
			Reason: ownRaisesForeign,
			Text: fmt.Sprintf("стенд %q объявил посадку личности `own` (iam=%s gateway=%s), "+
				"но поднимает чужую службу личности, о которой не решал никто: %s.\n"+
				"Это ВТОРАЯ действующая дверь в ту же систему рядом с нашей полосой: накладка "+
				"посадки чужие службы не выключает — наследует их включёнными из слоя под "+
				"собой.\nИсходов два: объявить флаг ложью в том слое, который объявил посадку, "+
				"либо внести компонент в ведомость остатка с причиной и предикатом снятия.\n"+
				"Решённый остаток: %s",
				stack, p.IAM, p.Edge, strings.Join(undecided, ", "), joinOrNone(decidedComponentNames(decided))),
		})
	case p.both("external") && len(enabled) == 0:
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

// judgeRemainderLedger — САМОИСТЕЧЕНИЕ ведомости, судимое ПО ДЕРЕВУ.
//
// Пока ключом была пара «стенд + компонент», истечение судилось на стенде.
// Ведомость объявлена для ВСЕХ стендов, поэтому и истекает она по всем: запись
// при деле, пока компонент поднимает хоть один судимый стенд.
//
// РАДИУС возвращается вторым значением и печатается перепиской: он и есть та
// цена, которую платит ключ «все стенды». Без него сжатие предмета с семи
// стендов до одного прошло бы молча, и ведомость выглядела бы прежней.
func judgeRemainderLedger(
	ledger []identityRemainder, raisedOn map[string][]string,
) (findings []identityPostureFinding, radius map[string]int) {
	radius = map[string]int{}
	for _, r := range ledger {
		stands := raisedOn[r.Component]
		radius[r.Component] = len(stands)
		if len(stands) > 0 {
			continue
		}
		findings = append(findings, identityPostureFinding{
			Stack:  "(все стенды)",
			Reason: remainderIsStale,
			Text: fmt.Sprintf("ведомость остатка называет компонент %q, которого не поднимает "+
				"НИ ОДИН судимый стенд.\nИсключению нечего исключать: запись объявляет "+
				"решённым то, что решать больше нечего, и переживёт любой следующий разбор. "+
				"Снимите её вместе с её предметом.", r.Component),
		})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Text < findings[j].Text })
	return findings, radius
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
	Undeclared int
	Components int
	Remainders int
}

func (c standIdentityCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · классифицировано %d · на посадке own %d · "+
		"на посадке external %d · половины разошлись %d · посадку не объявили %d · "+
		"компонентов чужой службы личности в дереве %d · записей ведомости остатка %d",
		c.Stacks, c.Own+c.External+c.Mixed+c.Undeclared, c.Own, c.External, c.Mixed,
		c.Undeclared, c.Components, c.Remainders)
}

// TestOwnPostureRaisesNoForeignIdentityService — посадка и флаги говорят об одном.
func TestOwnPostureRaisesNoForeignIdentityService(t *testing.T) {
	stacks := deployStacks(t)
	components := foreignIdentityComponents(t)

	// ПОДСТАНОВКА УМОЛЧАНИЯ ЗДЕСЬ БОЛЬШЕ НЕ ДЕЛАЕТСЯ ЗА КРАЙ.
	//
	// Стояло: «умолчания половин живут в профилях подчартов, стенд, посадку не
	// объявивший, получает их, а не пустоту» — и обход подставлял умолчание
	// края, вынося вердикт о значении, которого стенд не называл. Умолчание у
	// чарта края снято (посадка решает три места провязки, и наследованное
	// значение означает «не выбирали»), поэтому подставлять стало нечего:
	// необъявленная посадка края здесь ОСТАЁТСЯ ПУСТОЙ и роняет прогон ниже
	// вместе с именем стенда — тем же вердиктом, что у стража старта.
	//
	// У службы прав умолчание в вендоренной копии чарта ещё есть, и оно
	// читается: снятие копии — предмет владельца чарта
	// (deploy/helm/umbrella/identity_posture_profiles_test.go, ведомость
	// остатков).
	iamDefault := postureDefaultOf(t, filepath.Join(umbrellaDir, "charts", "kaname", "values.yaml"),
		"config", "authn", "identityProvider")
	if iamDefault == "" {
		t.Fatalf("умолчание посадки службы прав не прочитано (iam=%q) — вердикт о стендах, "+
			"посадку не объявивших, был бы вынесен неизвестно о чём", iamDefault)
	}
	edgeDefault := postureDefaultOf(t, filepath.Join("..", "gateway", "deploy", "values.yaml"),
		"authn", "identityProvider")
	if edgeDefault != "" {
		t.Errorf("базовый профиль чарта края снова объявляет посадку (%q) — умолчание решает "+
			"за стенд, и «не объявлено» перестаёт наступать где бы то ни было", edgeDefault)
	}

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	census := standIdentityCensus{
		Stacks:     len(names),
		Components: len(components),
		Remainders: len(foreignIdentityRemainders),
	}
	var findings []identityPostureFinding
	// Радиус ведомости: на каких судимых стендах компонент ещё поднимается.
	raisedOn := map[string][]string{}

	for _, name := range names {
		merged, _, _ := mergedValuesOfStack(t, stacks[name])

		p := standPosture{
			IAM:  stringAt(merged, iamDefault, "kaname", "config", "authn", "identityProvider"),
			Edge: stringAt(merged, edgeDefault, "api-gateway", "authn", "identityProvider"),
		}

		if strings.TrimSpace(p.Edge) == "" {
			findings = append(findings, identityPostureFinding{
				Stack:  name,
				Reason: postureUndeclared,
				Text: fmt.Sprintf("стенд %q посадку КРАЮ не объявил ни одним слоем цепочки "+
					"(служба прав: %s).\nУмолчания у чарта края нет намеренно, поэтому край "+
					"на этом стенде ОТКАЖЕТ В СТАРТЕ с именем ручки. Объявите посадку тем "+
					"слоем, который решает о стенде", name, p.IAM),
			})
			census.Undeclared++
			continue
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

		// Радиус считается ТОЛЬКО по судимым стендам: компонент на стенде, о
		// котором гейт не высказывается, записи ведомости не оправдывает.
		if p.any("own") {
			for _, c := range enabled {
				raisedOn[c] = append(raisedOn[c], name)
			}
		}

		findings = append(findings, judgeStandIdentity(name, p, enabled, foreignIdentityRemainders)...)
	}

	stale, radius := judgeRemainderLedger(foreignIdentityRemainders, raisedOn)
	findings = append(findings, stale...)
	for _, r := range foreignIdentityRemainders {
		t.Logf("  ведомость %s: радиус %d из %d судимых стендов (%s)",
			r.Component, radius[r.Component], census.Own+census.Mixed,
			joinOrNone(raisedOn[r.Component]))
	}

	t.Logf("перепись: %s · находок %d", census, len(findings))

	// Предпосылка обхода, обе стороны. Ноль стендов на любой из посадок значит,
	// что гейт судит ПУСТОТУ по этой оси и молчал бы о вернувшемся дефекте.
	if census.Own == 0 {
		t.Fatalf("ни один стенд не стоит на посадке `own` (осмотрено %d) — гейт судил бы "+
			"пустоту: «находок ноль» здесь неотличимо от «нечего было проверять»", census.Stacks)
	}
	// ВТОРАЯ СТОРОНА ПРЕДИКАТА ЖИВА, НО НОСИТЕЛЯ У НЕЁ БОЛЬШЕ НЕТ — И ЭТО ЦЕЛЬ.
	//
	// Здесь стоял отказ: «ни один стенд не стоит на посадке `external` — вторая
	// сторона предиката осталась без единого носителя». Он был фикстурой,
	// привязанной к СНИМАЕМОМУ предмету, и истёк вместе с ним: ноль стендов на
	// `external` есть ровно то, ради чего эпик и шёл. Проба, падающая на
	// ДОСТИЖЕНИИ СВОЕЙ ЦЕЛИ, свойства не держит — её снимают, а не чинят.
	//
	// Правило `externalRaisesNothing` при этом НЕ снято: стенд, выключивший
	// чужую службу и оставшийся на `external`, проверять человека нечем, и
	// вернуться это состояние может завтра. Способность этой ветви УПАСТЬ
	// доказана не живым носителем, а СИНТЕТИКОЙ — доказательство, опирающееся
	// на живой носитель, исчезло бы вместе с починкой:
	// `own_posture_foreign_identity_injection_test.go`,
	// TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll
	// (дефект краснит) и его законный близнец там же.
	if census.External == 0 {
		t.Logf("на посадке `external` не стоит ни один стенд из %d — это ЦЕЛЬ, а не отказ. "+
			"Ветвь о таком стенде жива и падать умеет; доказано синтетикой "+
			"(TestOwnPostureForeignIdentityGate_FindsTheStandWithNoProviderAtAll)",
			census.Stacks)
	}

	// ЧТО ЭТА ПРЕДПОСЫЛКА ДЕРЖАЛА НА САМОМ ДЕЛЕ — что обход КОГО-ТО судил.
	// «Находок ноль» обязано быть отличимо от «ни один стенд не классифицирован».
	if classified := census.Own + census.External + census.Mixed + census.Undeclared; classified != census.Stacks {
		t.Fatalf("классифицировано %d стендов из %d — обход потерял стенды молча, и "+
			"«находок ноль» сказано не обо всех", classified, census.Stacks)
	}
	if census.Components < 2 {
		t.Fatalf("состав чужой службы личности выведен из дерева как %d компонент(ов) — "+
			"признак перестал их узнавать", census.Components)
	}
	// Ведомость стендами больше не ключуется — она объявлена для ВСЕХ, — поэтому
	// проверки «называет стенд, которого нет» здесь нет: называть стенд ей
	// нечем. Её предмет судят две другие оси: компонент обязан быть в выведенном
	// составе (инъекционный файл) и обязан подниматься хоть одним судимым
	// стендом (`judgeRemainderLedger`, радиус выше).

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
