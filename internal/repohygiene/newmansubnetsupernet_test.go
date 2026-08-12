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
// # План может быть объявлен позже создания сети
//
// `:add-cidr-blocks` — законный и named-в-самом-отказе путь вперёд, поэтому кейс,
// создавший сеть без плана и объявивший его следующим шагом, гейта не касается:
// к моменту нарезки план есть. Порядок шагов гейт и читает.
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
// # Предмет гейта — СГЕНЕРИРОВАННЫЕ коллекции, а не питоновские исходники
//
// Исполняет newman именно коллекции; генератор, отступивший от формы, виден здесь
// через свой продукт, и обойти гейт правкой мимо генератора нельзя. Тот же выбор и
// по той же причине сделан в `newmanphantomid_test.go`.
package repohygiene

import (
	"encoding/json"
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
	refusalProbes   int // шагов, чей предмет — отказ (утверждают 4xx/5xx)
	declaredPlans   int // объявлений плана (создание с блоками либо :add-cidr-blocks)
	parentOutOfCase int // фикстур, чей родитель приходит извне кейса — НЕ покрыты
	hits            []string
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
		total.refusalProbes += res.refusalProbes
		total.declaredPlans += res.declaredPlans
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
	t.Logf("осмотрено коллекций: %d; кейсов: %d; шагов, режущих подсеть с адресом: %d; "+
		"из них с родителем ИЗВНЕ кейса (посев/окружение — этим гейтом НЕ покрыты): %d; "+
		"объявлений плана: %d; шагов, утверждающих отказ: %d",
		files, total.cases, total.subnetFixtures, total.parentOutOfCase,
		total.declaredPlans, total.refusalProbes)

	if len(total.hits) > 0 {
		sort.Strings(total.hits)
		t.Errorf("найдено %d фикстур, режущих подсеть в сети без объявленного плана:\n  %s\n\n"+
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
	reStatusAssert = regexp.MustCompile(`pm\.response\.code[^;\n]*`)
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
		planDeclared := false

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
			refusal := expectsRefusal(step)
			if refusal {
				out.refusalProbes++
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
				if planDeclared {
					continue
				}
				if lastPlanless == "" {
					out.parentOutOfCase++
					continue
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
					"» режет подсеть скриптом в сети из шага «"+lastPlanless+
					"», которая плана не объявила")
			case method == "POST" && reAddBlocks.MatchString(strings.TrimSpace(url)):
				if strings.Contains(raw, "CidrBlocks") && !refusal {
					out.declaredPlans++
					planDeclared = true
				}
			case method == "POST" && reNetCreate.MatchString(strings.TrimSpace(url)):
				if refusal {
					continue // проба про сам Network.Create — сеть не появится
				}
				if strings.Contains(raw, "CidrBlocks") {
					out.declaredPlans++
					planDeclared = true
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
				if planDeclared {
					continue
				}
				if lastPlanless == "" {
					out.parentOutOfCase++
					continue
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
					"» добавляет адрес подсети в сети из шага «"+lastPlanless+
					"», которая плана не объявила")
			case method == "POST" && reSubnetCreate.MatchString(strings.TrimSpace(url)):
				if !strings.Contains(raw, "ipv4CidrPrimary") && !strings.Contains(raw, "ipv6CidrPrimary") &&
					!carvesAddressByScript(step) {
					continue
				}
				if refusal {
					continue // предмет пробы — сам отказ, фикстурой она не является
				}
				out.subnetFixtures++
				ref := ""
				if m := reNetworkIDRef.FindStringSubmatch(raw); m != nil {
					ref = m[1]
				}
				fromCase := lastPlanless != "" || planDeclared
				if _, ok := planless[ref]; !ok && !fromCase {
					// Родителя в кейсе нет — он приходит из посева/окружения.
					out.parentOutOfCase++
					continue
				}
				if planDeclared {
					continue
				}
				where := planless[ref]
				if where == "" {
					where = lastPlanless
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
					"» режет подсеть в сети из шага «"+where+"», которая плана не объявила")
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

// expectsRefusal — шаг УТВЕРЖДАЕТ отказ: среди статусов, которые он допускает,
// нет ни одного успешного. Шаг, не утверждающий статуса вовсе, отказом не считается
// — на его успех опираются соседи.
func expectsRefusal(it pmItem) bool {
	script := stepScript(it, "test")
	stmts := reStatusAssert.FindAllString(script, -1)
	if len(stmts) == 0 {
		return false
	}
	sawStatus := false
	for _, s := range stmts {
		for _, m := range reThreeDigit.FindAllStringSubmatch(s, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			sawStatus = true
			if n >= 200 && n < 300 {
				return false
			}
		}
	}
	return sawStatus
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
