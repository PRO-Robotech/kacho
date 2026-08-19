// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmansubnetsupernet_test.go — гейт по коллекциям: фикстура не режет подсеть в
// сети, у которой нет объявленного адресного плана.
//
// # Предмет
//
// Сеть, не объявившая супернет семейства, подсеть этого семейства НЕ ПРИНИМАЕТ —
// синхронный `INVALID_ARGUMENT`: нарезать не из чего. Значит шаг фикстуры, который
// создаёт сеть телом без блоков и следом режет в ней подсеть с адресом, получает
// отказ — и падает не он один. На подсети стоят адрес, интерфейс, балансировщик,
// поэтому обрушивается фикстурный слой сразу нескольких наборов, а симптом
// («балансировщику не на чем встать») к причине отношения не имеет.
//
// Это ровно тот класс, ради которого гейт и заводится: фикстура, СНИСХОДИТЕЛЬНЕЕ
// продукта, прячет отказ ровно до того дня, когда продукт начинает его давать.
//
// # Что считается фикстурой, а что пробой
//
// Различает ИСХОД, которого шаг ждёт, а не имя шага: шаг, утверждающий отказ
// (4xx/5xx и ни одного 200), — это проба, её предмет и есть отказ. Шаг,
// утверждающий успех либо не утверждающий статуса вовсе, — фикстура: на её успех
// опираются соседние шаги.
//
// Утверждением считается то, что newman ИСПОЛНИТ, и то, что называет ДОПУЩЕННЫЕ
// коды. Поэтому не являются утверждением: отрицание («не 403» — оно запрещает один
// исход из пятисот и о коде не говорит ничего), ветвление петли ожидания
// (`if (pm.response.code === 200 && c < 50)`), число в тексте подписи к падению
// («unexpected 200: …») и закомментированная строка. Разбор — по вызовам
// `pm.expect(`, а не по упоминаниям кода; каждую ось держит в обе стороны
// `newmanrefusalpolarity_injection_test.go`.
//
// Прежняя редакция брала любое трёхзначное число рядом с упоминанием кода ответа и
// потому читала все пять форм одинаково. Следствие двустороннее: шаг, ничего не
// утверждающий, ВЫВОДИЛСЯ из-под наблюдения обоих гейтов, а шаг, приведённый в
// порядок, ПОПАДАЛ под него и мог дать красное, читаемое как регресс, — механизм
// наказывал за починку.
//
// # План может быть объявлен позже создания сети
//
// `:add-cidr-blocks` — законный и named-в-самом-отказе путь вперёд, поэтому кейс,
// создавший сеть без плана и объявивший его следующим шагом, гейта не касается:
// к моменту нарезки план есть. Порядок шагов гейт и читает.
//
// # Оба семейства считаются ПОРОЗНЬ
//
// Продукт проверяет вложенность по одному разу на семейство, поэтому сеть,
// объявившая только `ipv4CidrBlocks`, для подсети v6 плановой НЕ является. Первая
// редакция гейта несла один флаг на оба семейства — слепая зона, доказанная
// инъекцией: тот же кейс, внесённый в дерево, оставлял гейт зелёным, хотя шаг он
// видел и в перепись засчитывал. Обе стороны различителя закреплены
// `TestSubnetSupernetGateSeesFamiliesApart`, а предпосылка («нарезок v6 в дереве
// не нашлось вовсе») роняет прогон, чтобы молчание не читалось как проверка.
//
// # Чего гейт НЕ покрывает, и это названо числом
//
// Родитель, приходящий В КЕЙС ИЗВНЕ (`{{existingNetworkId}}`, `{{seedNetworkA1Id}}`
// — их создают посевные скрипты `deploy/scripts/seed-*.sh` и
// `tests/authz-fixtures/prodseed_*.py`), в коллекции не виден: там только имя
// переменной. Такие шаги гейт считает ОТДЕЛЬНО и печатает их число, чтобы «ноль
// находок» не читалось шире, чем есть, — молчание про них означает «не смотрел», а
// не «проверено».
//
// Само по себе объявление слепой зоны её не закрывает, и инцидент случился именно
// в ней. Теперь эти шаги судит `newmancarveplananchor_test.go`: раз план родителя
// здесь не виден, он требует, чтобы адрес был ВЫВЕДЕН из плана, который посев
// публикует. Разделение предметов остаётся: тот гейт про ПОПАДАНИЕ в план, этот —
// про его НАЛИЧИЕ, и вопрос у каждого один.
//
// # Предмет гейта — СГЕНЕРИРОВАННЫЕ коллекции, а не питоновские исходники
//
// Исполняет newman именно коллекции; генератор, отступивший от формы, виден здесь
// через свой продукт, и обойти гейт правкой мимо генератора нельзя. Тот же выбор и
// по той же причине сделан в `newmanphantomid_test.go`.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// subnetSupernetScanRoots — где лежат коллекции.
var subnetSupernetScanRoots = []string{"services", "gateway"}

type pmScript struct {
	Listen string `json:"listen"`
	Script struct {
		Exec []string `json:"exec"`
	} `json:"script"`
}

type pmItem struct {
	Name    string     `json:"name"`
	Item    []pmItem   `json:"item"`
	Event   []pmScript `json:"event"`
	Request *struct {
		Method string          `json:"method"`
		URL    json.RawMessage `json:"url"`
		Body   *struct {
			Raw string `json:"raw"`
		} `json:"body"`
	} `json:"request"`
}

type pmCollection struct {
	Item []pmItem `json:"item"`
}

type subnetSupernetScan struct {
	cases           int
	subnetFixtures  int // шагов-фикстур, режущих подсеть с адресом
	carvesV4        int // из них режущих адрес семейства v4
	carvesV6        int // из них режущих адрес семейства v6 (шаг может резать оба)
	refusalProbes   int // шагов, чей предмет — отказ (утверждают 4xx/5xx)
	negationOnly    int // шагов, чьи утверждения о коде — ТОЛЬКО отрицания («не 403»)
	noStatusAssert  int // шагов, не утверждающих о коде ничего
	declaredPlans   int // объявлений плана (создание с блоками либо :add-cidr-blocks)
	declaredV4      int // из них объявивших план семейства v4
	declaredV6      int // из них объявивших план семейства v6
	parentOutOfCase int // фикстур, чей родитель приходит извне кейса — НЕ покрыты
	hits            []string
}

// planFamilies — какие семейства адресного плана объявлены/нарезаны. Гейт первой
// редакции нёс ОДИН флаг «план объявлен» на оба семейства, и это была слепая зона,
// доказанная инъекцией, а не вычитанная: сеть, объявившая только `ipv4CidrBlocks`,
// засчитывалась плановой и для подсети v6, которую продукт отвергает синхронно.
// Семейства независимы у продукта (`eachWithinSupernet` зовётся по одному на каждое),
// значит и здесь они обязаны считаться порознь.
type planFamilies struct{ v4, v6 bool }

func (p planFamilies) any() bool { return p.v4 || p.v6 }

// covers — план покрывает ВСЕ семейства, которые режет шаг. Шаг двух семейств
// требует обоих планов: продукт откажет по первому же непокрытому.
func (p planFamilies) covers(need planFamilies) bool {
	return (!need.v4 || p.v4) && (!need.v6 || p.v6)
}

// missing — человекочитаемое имя семейства, которого не хватает (для координаты).
func (p planFamilies) missing(need planFamilies) string {
	switch {
	case need.v4 && !p.v4 && need.v6 && !p.v6:
		return "IPv4 и IPv6"
	case need.v4 && !p.v4:
		return "IPv4"
	default:
		return "IPv6"
	}
}

// familiesIn — какие семейства называет текст (тело запроса либо скрипт шага).
// Читается имя ПОЛЯ контракта, а не подстрока адреса: `ipv6CidrPrimary` и
// `ipv6CidrBlocks` — единственные две формы, которыми приходит адрес v6.
func familiesIn(text string) planFamilies {
	return planFamilies{
		v4: strings.Contains(text, "ipv4Cidr"),
		v6: strings.Contains(text, "ipv6Cidr"),
	}
}

// TestSubnetFixtureNeverCarvesFromAPlanlessNetwork — ни одна фикстура не режет
// подсеть с адресом в сети, которая к этому моменту плана не объявила.
func TestSubnetFixtureNeverCarvesFromAPlanlessNetwork(t *testing.T) {
	root := repoRoot(t)

	files := 0
	total := subnetSupernetScan{}
	forEachCollectionForSubnetSupernet(t, root, func(rel string, body []byte) {
		files++
		res := analyzeSubnetSupernetFixtures(t, rel, body)
		total.cases += res.cases
		total.subnetFixtures += res.subnetFixtures
		total.carvesV4 += res.carvesV4
		total.carvesV6 += res.carvesV6
		total.refusalProbes += res.refusalProbes
		total.negationOnly += res.negationOnly
		total.noStatusAssert += res.noStatusAssert
		total.declaredPlans += res.declaredPlans
		total.declaredV4 += res.declaredV4
		total.declaredV6 += res.declaredV6
		total.parentOutOfCase += res.parentOutOfCase
		total.hits = append(total.hits, res.hits...)
	})

	if files == 0 {
		t.Fatalf("гейт не прочитал ни одной коллекции в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", subnetSupernetScanRoots)
	}
	if total.subnetFixtures == 0 {
		t.Fatalf("гейт не нашёл ни одного шага, режущего подсеть с адресом, в %d коллекциях — "+
			"распознавание предмета сломано, молчание ничего не доказывает", files)
	}
	if total.declaredPlans == 0 {
		t.Fatalf("гейт не распознал ни одного объявления адресного плана в %d коллекциях — "+
			"а именно его наличие он и требует; молчание ничего не доказывает", files)
	}
	// Предпосылка РАЗЛИЧИТЕЛЯ: гейт отделяет фикстуру от пробы по ожидаемому исходу.
	// Если проб отказа не распознано ни одной, различитель на живых данных не
	// исполнялся, и «фикстурой» он мог посчитать что угодно.
	if total.refusalProbes == 0 {
		t.Fatalf("гейт не распознал ни одного шага, утверждающего отказ, в %d коллекциях — "+
			"различитель «фикстура против пробы» не исполнился ни разу; молчание ничего "+
			"не доказывает", files)
	}
	// Предпосылка РАЗЛИЧИТЕЛЯ СЕМЕЙСТВ. Требование «план того же семейства» проверяемо
	// ровно тогда, когда в дереве есть что различать: если ни одной нарезки v6 не
	// найдено, различитель на живых данных не исполнялся и его молчание — «не
	// смотрел», а не «проверено». Ровно этим гейт и был слеп в первой редакции.
	if total.carvesV6 == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОЙ нарезки семейства IPv6 в %d коллекциях (нарезок v4: %d) — "+
			"различитель семейств не исполнился ни разу; молчание ничего не доказывает",
			files, total.carvesV4)
	}
	if total.declaredV6 == 0 {
		t.Fatalf("гейт не распознал НИ ОДНОГО объявления плана семейства IPv6 в %d коллекциях "+
			"(объявлений v4: %d) — а именно его наличие он и требует от нарезки v6; "+
			"молчание ничего не доказывает", files, total.declaredV4)
	}
	t.Logf("осмотрено коллекций: %d; кейсов: %d; шагов, режущих подсеть с адресом: %d "+
		"(из них семейства v4: %d, семейства v6: %d); "+
		"из них с родителем ИЗВНЕ кейса (посев/окружение — этим гейтом НЕ покрыты, их "+
		"держит TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan): %d; "+
		"объявлений плана: %d (v4: %d, v6: %d); "+
		"шагов, УТВЕРЖДАЮЩИХ отказ (из-под наблюдения выведены — их предмет и есть "+
		"отказ): %d; шагов, чьи утверждения о коде — ТОЛЬКО отрицания («не 403»): %d "+
		"(о коде не утверждают ничего ⇒ судятся как фикстуры; их переписывание в "+
		"утверждения-пары — предмет отдельной задачи, и НОЛЬ здесь законен); шагов "+
		"без утверждений о коде вовсе: %d",
		files, total.cases, total.subnetFixtures, total.carvesV4, total.carvesV6,
		total.parentOutOfCase, total.declaredPlans, total.declaredV4, total.declaredV6,
		total.refusalProbes, total.negationOnly, total.noStatusAssert)

	if len(total.hits) > 0 {
		sort.Strings(total.hits)
		t.Errorf("найдено %d фикстур, режущих подсеть в сети без объявленного плана ТОГО ЖЕ "+
			"семейства:\n  %s\n\n"+
			"Следствие: сеть без супернета семейства подсеть этого семейства не принимает "+
			"(sync INVALID_ARGUMENT — нарезать не из чего), поэтому падает не только этот шаг, "+
			"но и всё, что стоит на подсети: адрес, интерфейс, балансировщик. Симптом при этом "+
			"уезжает далеко от причины.\n\n"+
			"Исход: объявить план у сети — блоками в теле создания либо шагом "+
			":add-cidr-blocks ДО нарезки. Ослаблять утверждения кейса нельзя: фикстура "+
			"приводится к продукту, а не продукт к фикстуре.",
			len(total.hits), strings.Join(total.hits, "\n  "))
	}
}

// ---- разбор ----

var (
	reSubnetCreate = regexp.MustCompile(`/vpc/v1/subnets\s*$`)
	reNetCreate    = regexp.MustCompile(`/vpc/v1/networks\s*$`)
	reAddBlocks    = regexp.MustCompile(`/vpc/v1/networks/[^/]+:add-cidr-blocks\s*$`)
	reNetworkIDRef = regexp.MustCompile(`"networkId"\s*:\s*"\{\{([^}]+)\}\}"`)
	reThreeDigit   = regexp.MustCompile(`\b([1-5]\d{2})\b`)

	// Две формы нарезки, которые первая редакция гейта НЕ видела, — обе найдены
	// не чтением, а прогоном сквозных проб: гейт был зелен, а e2e красен.
	//
	//  1. блок добавляется ПОЗЖЕ, отдельным глаголом на самой подсети. Создание
	//     подсети без адреса законно, поэтому шаг создания проверку проходил, а
	//     адрес появлялся шагом, которого разбор не рассматривал вовсе;
	//  2. тело запроса собирается СКРИПТОМ (пачка параллельных созданий в пробе
	//     состязания): у шага пустое тело и посторонний адрес, а настоящий запрос
	//     живёт в тексте скрипта.
	//
	// Обе формы режут адрес из плана сети ровно так же, как обычное создание, и
	// на сети без плана получают тот же синхронный отказ.
	reSubnetAddBlocks  = regexp.MustCompile(`/vpc/v1/subnets/[^/]+:add-cidr-blocks\s*$`)
	reScriptSubnetPost = regexp.MustCompile(`/vpc/v1/subnets['"` + "`" + `\s]`)
)

// carvesAddressByScript — тело запроса собрано скриптом шага: адрес подсети
// задаётся не полем тела, а строкой в тексте. Возвращает true, когда скрипт
// одновременно называет путь создания подсети и якорный адрес.
func carvesAddressByScript(it pmItem) bool {
	script := stepScript(it, "test") + "\n" + stepScript(it, "prerequest")
	if !reScriptSubnetPost.MatchString(script) {
		return false
	}
	return strings.Contains(script, "ipv4CidrPrimary") || strings.Contains(script, "ipv6CidrPrimary")
}

// countCarveFamilies — перепись нарезок по семействам. Считается ФАКТ нарезки
// каждого семейства, а не шаг: дуальный шаг режет оба и обязан требовать оба плана.
func countCarveFamilies(out *subnetSupernetScan, need planFamilies) {
	if need.v4 {
		out.carvesV4++
	}
	if need.v6 {
		out.carvesV6++
	}
}

// declareFamilies — то же для объявлений плана.
func declareFamilies(out *subnetSupernetScan, have *planFamilies, got planFamilies) {
	if got.v4 {
		out.declaredV4++
		have.v4 = true
	}
	if got.v6 {
		out.declaredV6++
		have.v6 = true
	}
}

func analyzeSubnetSupernetFixtures(t *testing.T, rel string, body []byte) subnetSupernetScan {
	t.Helper()
	var coll pmCollection
	if err := json.Unmarshal(body, &coll); err != nil {
		t.Fatalf("%s: коллекция не разбирается: %v — файл не может быть ни засчитан "+
			"в перепись, ни молча пропущен", rel, err)
	}

	out := subnetSupernetScan{}
	for _, c := range coll.Item {
		out.cases++
		group := c.Item
		if len(group) == 0 {
			group = []pmItem{c}
		}
		// Идентификаторы сетей, СОЗДАННЫХ в этом кейсе без плана, и те, чей план
		// уже объявлен. Ключ — имя переменной, в которую шаг публикует networkId;
		// когда шаг её не публикует, работает признак «последняя созданная».
		planless := map[string]string{} // переменная → координата шага-создателя
		lastPlanless := ""
		// Планы считаются ПОСЕМЕЙНО: сеть, объявившая только v4, для подсети v6
		// плановой не является (продукт зовёт проверку по одному разу на семейство).
		planDeclared := planFamilies{}

		for _, step := range flattenItems(group, nil) {
			if step.Request == nil {
				continue
			}
			url := rawURL(step.Request.URL)
			raw := ""
			if step.Request.Body != nil {
				raw = step.Request.Body.Raw
			}
			method := step.Request.Method
			// Перепись по КЛАССАМ, а не по одному флагу: «шаг ждёт отказа»,
			// «шаг утверждает только отрицанием» и «шаг не утверждает о коде
			// ничего» — три разных состояния, и слипшись они дают ровно тот
			// дефект, ради которого различитель переписан. Числа печатает гейт.
			class := classifyStepStatus(step)
			refusal := class == stepSaysRefusal
			switch class {
			case stepSaysRefusal:
				out.refusalProbes++
			case stepSaysNegationOnly:
				out.negationOnly++
			case stepSaysNothing:
				out.noStatusAssert++
			}

			switch {
			case method == "POST" && carvesAddressByScript(step):
				// Тело собрано скриптом: адрес шага стоит на ЧУЖОМ пути (пачка
				// параллельных созданий адресуется на путь сетей), поэтому по
				// адресу такой шаг не опознаётся вовсе. Ветвь стоит ПЕРВОЙ, иначе
				// шаг был бы засчитан созданием сети — то есть не нарезкой, а её
				// родителем, и предмет исчез бы дважды.
				if refusal {
					continue
				}
				out.subnetFixtures++
				need := familiesIn(stepScript(step, "test") + "\n" + stepScript(step, "prerequest"))
				countCarveFamilies(&out, need)
				if planDeclared.covers(need) {
					continue
				}
				if lastPlanless == "" {
					out.parentOutOfCase++
					continue
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
					"» режет подсеть скриптом в сети из шага «"+lastPlanless+
					"», которая не объявила план "+planDeclared.missing(need))
			case method == "POST" && reAddBlocks.MatchString(strings.TrimSpace(url)):
				if strings.Contains(raw, "CidrBlocks") && !refusal {
					out.declaredPlans++
					declareFamilies(&out, &planDeclared, familiesIn(raw))
				}
			case method == "POST" && reNetCreate.MatchString(strings.TrimSpace(url)):
				if refusal {
					continue // проба про сам Network.Create — сеть не появится
				}
				if strings.Contains(raw, "CidrBlocks") {
					out.declaredPlans++
					declareFamilies(&out, &planDeclared, familiesIn(raw))
					// Сеть, объявившая план ОДНОГО семейства, для другого остаётся
					// беспланой: её имя обязано попасть в набор «без плана», иначе
					// нарезка второго семейства не найдёт координаты своего родителя.
					for _, v := range publishedVars(step) {
						planless[v] = step.Name
					}
					lastPlanless = step.Name
					continue
				}
				for _, v := range publishedVars(step) {
					planless[v] = step.Name
				}
				lastPlanless = step.Name
			case method == "POST" && reSubnetAddBlocks.MatchString(strings.TrimSpace(url)):
				// Адрес приходит на подсеть отдельным глаголом — нарезка та же,
				// и на сети без плана она отвергается так же.
				if !strings.Contains(raw, "CidrBlocks") || refusal {
					continue
				}
				out.subnetFixtures++
				need := familiesIn(raw)
				countCarveFamilies(&out, need)
				if planDeclared.covers(need) {
					continue
				}
				if lastPlanless == "" {
					out.parentOutOfCase++
					continue
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
					"» добавляет адрес подсети в сети из шага «"+lastPlanless+
					"», которая не объявила план "+planDeclared.missing(need))
			case method == "POST" && reSubnetCreate.MatchString(strings.TrimSpace(url)):
				if !strings.Contains(raw, "ipv4CidrPrimary") && !strings.Contains(raw, "ipv6CidrPrimary") &&
					!carvesAddressByScript(step) {
					continue
				}
				if refusal {
					continue // предмет пробы — сам отказ, фикстурой она не является
				}
				out.subnetFixtures++
				need := familiesIn(raw)
				if !need.any() {
					need = familiesIn(stepScript(step, "test") + "\n" + stepScript(step, "prerequest"))
				}
				countCarveFamilies(&out, need)
				ref := ""
				if m := reNetworkIDRef.FindStringSubmatch(raw); m != nil {
					ref = m[1]
				}
				fromCase := lastPlanless != "" || planDeclared.any()
				if _, ok := planless[ref]; !ok && !fromCase {
					// Родителя в кейсе нет — он приходит из посева/окружения.
					out.parentOutOfCase++
					continue
				}
				if planDeclared.covers(need) {
					continue
				}
				where := planless[ref]
				if where == "" {
					where = lastPlanless
				}
				if where == "" {
					// Родитель кейсу известен только объявленным планом ДРУГОГО
					// семейства: координаты шага-создателя нет, но предмет есть.
					where = "<создан вне кейса>"
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
					"» режет подсеть в сети из шага «"+where+"», которая не объявила план "+
					planDeclared.missing(need))
			}
		}
	}
	return out
}

func flattenItems(items []pmItem, acc []pmItem) []pmItem {
	for _, it := range items {
		if len(it.Item) > 0 {
			acc = flattenItems(it.Item, acc)
			continue
		}
		acc = append(acc, it)
	}
	return acc
}

func rawURL(m json.RawMessage) string {
	if len(m) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m, &s); err == nil {
		return s
	}
	var o struct {
		Raw string `json:"raw"`
	}
	_ = json.Unmarshal(m, &o)
	return o.Raw
}

func stepScript(it pmItem, listen string) string {
	var b strings.Builder
	for _, e := range it.Event {
		if e.Listen != listen {
			continue
		}
		for _, l := range e.Script.Exec {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ---- различитель «проба отказа против фикстуры» ----
//
// Шаг относится к пробам отказа по тому, что он УТВЕРЖДАЕТ, а не по тому, какие
// числа встречаются в его тексте. Прежняя редакция брала любое трёхзначное число
// рядом с упоминанием кода ответа, поэтому читала одинаково три разные вещи:
// утверждение отказа (`.to.equal(403)`), его ОТРИЦАНИЕ (`.to.not.equal(403)`,
// ALLOW-полоса — она о коде не утверждает ничего) и управляющий поток петли
// ожидания (`if (pm.response.code === 200 && c < 50)`). Плюс числа из текста
// сообщения («unexpected 200: …») и из закомментированных строк.
//
// Следствие было двусторонним, и вторая сторона хуже первой: шаг, ничего не
// утверждающий, ВЫВОДИЛСЯ из-под наблюдения обоих гейтов, а шаг, приведённый в
// порядок, ПОПАДАЛ под него и мог дать красное, читаемое как регресс. То есть
// механизм наказывал за починку и учил возвращать слабую форму.
//
// Каждая ось разбора — полярность, комментарий, строковый литерал, управляющий
// поток, перенос строки в цепочке — закреплена В ОБЕ СТОРОНЫ отдельным файлом
// `newmanrefusalpolarity_injection_test.go`: без красной стороны различитель
// ничего не требует, без зелёной он снимается первым ложным срабатыванием.

// jsExecutablePart — исходник шага без комментариев и без СОДЕРЖИМОГО строковых
// литералов (кавычки остаются, чтобы форма выражения не поехала).
//
// Отличается от `stripJSComments` предметом, а не аккуратностью: тому нужен текст
// ВНУТРИ кавычек (он ищет имя переменной плана), а этому нужно, чтобы числа из
// подписи к падению не читались как допущенные коды. Поэтому это две функции, а не
// одна с флагом.
func jsExecutablePart(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			} else {
				i = len(src)
			}
		case c == '\'' || c == '"' || c == '`':
			q := c
			b.WriteByte(q)
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == q || (q != '`' && src[i] == '\n') {
					break
				}
				i++
			}
			b.WriteByte(q)
			if i < len(src) && src[i] == q {
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// matchingParen — индекс закрывающей скобки к открывающей в позиции open.
func matchingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// firstTopLevelArg — первый аргумент вызова (текст до запятой ВЕРХНЕГО уровня).
func firstTopLevelArg(args string) string {
	depth := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return args[:i]
			}
		}
	}
	return args
}

// assertionChain — цепочка утверждения, стоящая после `pm.expect(…)`: до конца
// выражения (`;`), до закрывающей скобки объемлющего вызова или до конца строки.
//
// Пробелы и переносы ПЕРЕД цепочкой пропускаются: в дереве есть вызовы, у которых
// аргументы заняли две строки, а `.to.…` встал на третьей. Разбор, начинавший счёт
// с переноса, такое утверждение не читал вовсе — и молча возвращал пробу отказа в
// фикстуры. Свойство держит `TestRefusalDiscriminatorReadsChainsBrokenAcrossLines`;
// без пропуска оно краснеет.
func assertionChain(s string, from int) string {
	for from < len(s) && (s[from] == ' ' || s[from] == '\t' || s[from] == '\n' || s[from] == '\r') {
		from++
	}
	depth := 0
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return s[from:i]
			}
			depth--
		case ';', '\n':
			if depth == 0 {
				return s[from:i]
			}
		}
	}
	return s[from:]
}

// statusAssertion — одно утверждение шага о коде ответа.
type statusAssertion struct {
	negated bool  // в цепочке есть `.not.` — сказано, каким код НЕ бывает
	codes   []int // коды-литералы, которые утверждение называет
}

var (
	reExpectCall = regexp.MustCompile(`pm\.expect\s*\(`)
	reCodeToken  = regexp.MustCompile(`pm\.response\.code\b`)
)

func threeDigitCodes(s string) []int {
	var out []int
	for _, m := range reThreeDigit.FindAllStringSubmatch(s, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// statusAssertionsIn — утверждения шага о коде ответа.
//
// Читается ровно то, что newman исполнит: разбор идёт по вызовам `pm.expect(`, а не
// по упоминаниям кода, поэтому петля ожидания и любая другая ветвь управляющего
// потока сюда не попадают by construction — они о коде НЕ УТВЕРЖДАЮТ, они по нему
// ветвятся.
//
// Две формы записи, обе живут в дереве: субъект — код (`pm.expect(code).to.eql(404)`)
// и субъект — набор (`pm.expect([403,404]).to.include(code)`). Утверждение, чьи коды
// названы не литералом (переменной), допущенных кодов не сообщает и в разбор не идёт.
func statusAssertionsIn(script string) []statusAssertion {
	code := jsExecutablePart(script)
	var out []statusAssertion
	for _, loc := range reExpectCall.FindAllStringIndex(code, -1) {
		open := loc[1] - 1
		closing := matchingParen(code, open)
		if closing < 0 {
			continue
		}
		subject := firstTopLevelArg(code[open+1 : closing])
		chain := assertionChain(code, closing+1)
		var lits []int
		switch {
		case reCodeToken.MatchString(subject):
			lits = threeDigitCodes(chain)
		case reCodeToken.MatchString(chain):
			lits = threeDigitCodes(subject)
		default:
			continue
		}
		if len(lits) == 0 {
			continue
		}
		out = append(out, statusAssertion{negated: strings.Contains(chain, ".not."), codes: lits})
	}
	return out
}

// stepStatusClass — чем шаг является для гейта.
type stepStatusClass int

const (
	// stepSaysNothing — шаг о коде не утверждает ничего (либо утверждает только
	// отрицанием). Соседи опираются на его успех ⇒ он фикстура и судится по существу.
	stepSaysNothing stepStatusClass = iota
	// stepSaysNegationOnly — все утверждения шага о коде отрицательные.
	stepSaysNegationOnly
	// stepSaysSuccess — среди допущенных кодов есть успешный.
	stepSaysSuccess
	// stepSaysRefusal — шаг утверждает отказ: допущенные коды названы и успешного
	// среди них нет. Предмет такого шага — сам отказ, фикстурой он не является.
	stepSaysRefusal
)

func classifyStepStatus(it pmItem) stepStatusClass {
	as := statusAssertionsIn(stepScript(it, "test"))
	if len(as) == 0 {
		return stepSaysNothing
	}
	sawPositive := false
	for _, a := range as {
		if a.negated {
			continue
		}
		sawPositive = true
		for _, n := range a.codes {
			if n >= 200 && n < 300 {
				return stepSaysSuccess
			}
		}
	}
	if !sawPositive {
		return stepSaysNegationOnly
	}
	return stepSaysRefusal
}

// expectsRefusal — шаг УТВЕРЖДАЕТ отказ: среди кодов, которые он допускает, нет ни
// одного успешного. Шаг, не утверждающий статуса вовсе, и шаг, утверждающий только
// отрицанием («не 403»), отказом не считаются — на их успех опираются соседи.
func expectsRefusal(it pmItem) bool {
	return classifyStepStatus(it) == stepSaysRefusal
}

// publishedVars — имена переменных окружения, в которые шаг публикует свой ответ.
// Ими следующий шаг и адресует созданную сеть.
var rePublish = regexp.MustCompile(`pm\.environment\.set\(\s*'([^']+)'`)

func publishedVars(it pmItem) []string {
	var out []string
	for _, m := range rePublish.FindAllStringSubmatch(stepScript(it, "test"), -1) {
		out = append(out, m[1])
	}
	return out
}

// ---- обход ----

func forEachCollectionForSubnetSupernet(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range subnetSupernetScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("каталог %s не найден (%v) — область обхода гейта сломана", sub, err)
		}
		err := rootedWalk(base, func(rel string) bool {
			return strings.Contains(filepath.ToSlash(rel), "/tests/newman/collections/") &&
				strings.HasSuffix(rel, ".json")
		}, func(abs string, body []byte) error {
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				return relErr
			}
			fn(filepath.ToSlash(rel), body)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", sub, err)
		}
	}
}

// ---- инъекция в обе стороны ----

// TestSubnetSupernetGateRedOnInjectedDefect — фикстура без плана краснит гейт И
// называет координату (кейс и шаг).
func TestSubnetSupernetGateRedOnInjectedDefect(t *testing.T) {
	got := analyzeSubnetSupernetFixtures(t, "injected.json", []byte(`{"item":[{
      "name":"SUB-X — carve without a plan",
      "item":[
        {"name":"pre-net","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/networks"},
          "body":{"raw":"{\"projectId\":\"p\",\"name\":\"n\"}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
            "pm.environment.set('netId', pm.response.json().metadata.networkId);"]}}]},
        {"name":"pre-sub","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"10.1.0.0/24\"}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]}
      ]}]}`))

	if got.subnetFixtures != 1 {
		t.Fatalf("фикстура не распознана: subnetFixtures=%d", got.subnetFixtures)
	}
	if len(got.hits) != 1 {
		t.Fatalf("дефект не найден: hits=%v", got.hits)
	}
	if !strings.Contains(got.hits[0], "pre-sub") || !strings.Contains(got.hits[0], "pre-net") ||
		!strings.Contains(got.hits[0], "SUB-X") {
		t.Errorf("гейт обязан назвать кейс, режущий шаг и сеть без плана, получено: %q", got.hits[0])
	}
}

// TestSubnetSupernetGateSeesFamiliesApart — различитель семейств, доказанный
// инъекцией в ОБЕ стороны на одной и той же форме шага.
//
// Первая редакция гейта несла один флаг «план объявлен» на оба семейства, и это
// была не описка, а слепая зона с последствием: сеть, объявившая только
// `ipv4CidrBlocks`, засчитывалась плановой и для подсети v6 — которую продукт
// отвергает синхронно. Слепота найдена не чтением: тот же кейс, внесённый в дерево,
// оставлял гейт зелёным, хотя шаг он ВИДЕЛ и в перепись засчитывал.
//
// Обе стороны обязательны. Без красной гейт ничего не требует; без зелёной он
// запрещал бы нарезку v4 в сети, объявившей план только v4, — и был бы снят первым
// же ложным срабатыванием.
func TestSubnetSupernetGateSeesFamiliesApart(t *testing.T) {
	const shape = `{"item":[{"name":"F — %s carve on a %s-only plan","item":[
      {"name":"mk-net","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/networks"},
        "body":{"raw":"{\"name\":\"n\",\"%sCidrBlocks\":[\"%s\"]}"}},
        "event":[{"listen":"test","script":{"exec":[
          "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
          "pm.environment.set('netId', pm.response.json().metadata.networkId);"]}}]},
      {"name":"mk-sub","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
        "body":{"raw":"{\"networkId\":\"{{netId}}\",\"%sCidrPrimary\":\"%s\"}"}},
        "event":[{"listen":"test","script":{"exec":[
          "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]}
    ]}]}`

	build := func(planFam, planCIDR, subFam, subCIDR string) []byte {
		return []byte(fmt.Sprintf(shape, subFam, planFam, planFam, planCIDR, subFam, subCIDR))
	}

	// КРАСНАЯ сторона: план только v4, режется v6.
	red := analyzeSubnetSupernetFixtures(t, "families-red.json",
		build("ipv4", "10.0.0.0/8", "ipv6", "fd00:1::/64"))
	if red.carvesV6 != 1 || red.declaredV6 != 0 {
		t.Fatalf("вход не разобран: carvesV6=%d declaredV6=%d — на неразобранном входе "+
			"ни находка, ни молчание ничего не доказывают", red.carvesV6, red.declaredV6)
	}
	if len(red.hits) != 1 {
		t.Fatalf("нарезка v6 в сети с планом ТОЛЬКО v4 обязана быть находкой, получено: %v", red.hits)
	}
	if !strings.Contains(red.hits[0], "IPv6") {
		t.Errorf("находка обязана назвать НЕДОСТАЮЩЕЕ семейство, получено: %q", red.hits[0])
	}

	// ЗЕЛЁНАЯ сторона, случай 1: та же форма, но режется семейство объявленного плана.
	sameFam := analyzeSubnetSupernetFixtures(t, "families-green-v4.json",
		build("ipv4", "10.0.0.0/8", "ipv4", "10.1.0.0/24"))
	if sameFam.carvesV4 != 1 {
		t.Fatalf("вход не разобран: carvesV4=%d", sameFam.carvesV4)
	}
	if len(sameFam.hits) != 0 {
		t.Fatalf("гейт сработал на законной нарезке v4 при плане v4: %v", sameFam.hits)
	}

	// ЗЕЛЁНАЯ сторона, случай 2: v6 при объявленном плане v6 — то же зеркально.
	v6ok := analyzeSubnetSupernetFixtures(t, "families-green-v6.json",
		build("ipv6", "fd00::/8", "ipv6", "fd00:1::/64"))
	if v6ok.carvesV6 != 1 || v6ok.declaredV6 != 1 {
		t.Fatalf("вход не разобран: carvesV6=%d declaredV6=%d", v6ok.carvesV6, v6ok.declaredV6)
	}
	if len(v6ok.hits) != 0 {
		t.Fatalf("гейт сработал на законной нарезке v6 при плане v6: %v", v6ok.hits)
	}
}

// TestSubnetSupernetGateSilentOnLawfulSameShape — три законные конструкции ТОЙ ЖЕ
// формы, и все три сняты с дерева, а не выдуманы: план объявлен в теле создания;
// план объявлен позже, шагом `:add-cidr-blocks`; шаг УТВЕРЖДАЕТ отказ, то есть он
// проба, а не фикстура.
//
// Без этой стороны гейт запрещал бы создание сети без плана как таковое — и первым
// же ложным срабатыванием (на кейсе, чей предмет и есть отказ) был бы снят.
func TestSubnetSupernetGateSilentOnLawfulSameShape(t *testing.T) {
	got := analyzeSubnetSupernetFixtures(t, "lawful.json", []byte(`{"item":[
      {"name":"A — plan declared in the create body","item":[
        {"name":"pre-net","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/networks"},
          "body":{"raw":"{\"name\":\"n\",\"ipv4CidrBlocks\":[\"10.0.0.0/8\"]}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
            "pm.environment.set('netId', pm.response.json().metadata.networkId);"]}}]},
        {"name":"pre-sub","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"10.1.0.0/24\"}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]}
      ]},
      {"name":"B — plan declared later, via :add-cidr-blocks","item":[
        {"name":"mk-net-planless","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/networks"},
          "body":{"raw":"{\"name\":\"n\"}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));",
            "pm.environment.set('netId', pm.response.json().metadata.networkId);"]}}]},
        {"name":"cr-sub-no-plan","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"10.1.0.0/24\"}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 400', () => pm.expect(pm.response.code).to.eql(400));"]}}]},
        {"name":"declare-plan","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/networks/{{netId}}:add-cidr-blocks"},
          "body":{"raw":"{\"ipv4CidrBlocks\":[\"10.0.0.0/8\"]}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]},
        {"name":"mk-sub","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"10.1.0.0/24\"}"}},
          "event":[{"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]}
      ]}]}`))

	if got.declaredPlans != 2 {
		t.Fatalf("объявления плана не распознаны: declaredPlans=%d, ожидалось 2", got.declaredPlans)
	}
	if got.refusalProbes != 1 {
		t.Fatalf("проба отказа не распознана: refusalProbes=%d, ожидалась 1 — "+
			"различитель «фикстура против пробы» не отработал", got.refusalProbes)
	}
	if got.subnetFixtures != 2 {
		t.Fatalf("фикстуры не распознаны: subnetFixtures=%d, ожидалось 2 — молчание "+
			"на неразобранном входе ничего не доказывает", got.subnetFixtures)
	}
	if len(got.hits) != 0 {
		t.Fatalf("гейт сработал на законной конструкции той же формы: %v", got.hits)
	}
}
