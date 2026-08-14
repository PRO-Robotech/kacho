// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmancomputedcidrhead_test.go — гейт по коллекциям: адрес, который фикстура
// СОБИРАЕТ выражением, обязан начинаться с законченного поля.
//
// # Предмет
//
// Соседний гейт (`newmansubnetsupernet_test.go`) требует, чтобы у сети был
// объявленный план. Он ничего не говорит о том, попадает ли В этот план адрес,
// который фикстура нарезает, — и не может сказать, когда адрес не записан
// литералом, а собирается выражением из энтропии прогона. Ровно там и живёт
// отказ, который этот гейт закрывает.
//
// Мера, которую выражение обязано соблюсти, одна: **литеральная голова обязана
// заканчиваться разделителем поля** — `:` для IPv6, `.` для IPv4. Тогда первое
// поле адреса задано головой целиком, и членство в плане определяется головой, а
// не шириной приписанного куска. Голова, оборванная В СЕРЕДИНЕ поля, отдаёт первое
// поле на откуп вычислению: приписали одну шестнадцатеричную цифру — вышел один
// адрес, приписали две — совсем другой, и второй может лежать вне плана.
//
// # Почему это не придирка к стилю
//
// Отказ, из которого гейт выведен, выглядел как отказ ПРОДУКТА: подсеть не
// создавалась, а падал не шаг создания, а уборка по незахваченной переменной — то
// есть симптом уезжал от причины на пять шагов. Сеть при этом план объявляла, и
// соседний гейт был законно зелен.
//
// # Область: сгенерированные коллекции, а не питоновские исходники
//
// Исполняет newman коллекции; генератор, отступивший от формы, виден здесь через
// свой продукт. Тот же выбор и по той же причине — в двух соседних гейтах.
package repohygiene

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type computedCidrScan struct {
	carves      int // шагов, режущих подсеть адресом-подстановкой
	resolved    int // из них тех, чьего производителя удалось найти
	computed    int // из них тех, чей адрес именно СОБИРАЕТСЯ (есть конкатенация)
	literal     int // из них тех, чей производитель отдаёт готовый литерал
	planDerived int // из них тех, чей адрес вырезан ИЗ ОПУБЛИКОВАННОГО ПЛАНА
	unresolve   int // подстановок, чей производитель не найден (посев/окружение)
	hits        []string
}

// TestComputedSubnetCidrStartsOnAFieldBoundary — ни одно выражение, собирающее
// адрес подсети, не обрывает литеральную голову в середине поля.
func TestComputedSubnetCidrStartsOnAFieldBoundary(t *testing.T) {
	root := repoRoot(t)

	files := 0
	total := computedCidrScan{}
	forEachCollectionForSubnetSupernet(t, root, func(rel string, body []byte) {
		files++
		res := analyzeComputedCidrHeads(t, rel, body)
		total.carves += res.carves
		total.resolved += res.resolved
		total.computed += res.computed
		total.literal += res.literal
		total.planDerived += res.planDerived
		total.unresolve += res.unresolve
		total.hits = append(total.hits, res.hits...)
	})

	if files == 0 {
		t.Fatalf("гейт не прочитал ни одной коллекции — предпосылка обхода сломана, "+
			"молчание ничего не доказывает (корни: %v)", subnetSupernetScanRoots)
	}
	if total.carves == 0 {
		t.Fatalf("гейт не нашёл ни одного шага, режущего подсеть адресом-подстановкой, "+
			"в %d коллекциях — распознавание предмета сломано, молчание ничего не доказывает", files)
	}
	// Предпосылка РАЗБОРА: производитель подстановки обязан находиться хотя бы у
	// части шагов. Ноль означает, что распознавание выражения сломано, — тогда
	// молчание гейта есть «не смотрел», а не «проверено».
	//
	// А вот НОЛЬ СОБИРАЕМЫХ АДРЕСОВ отказом НЕ является, и это не послабление.
	// Отсутствие склеенных адресов есть та самая цель, к которой правило ведёт
	// (адрес вырезается из опубликованного плана, голова приходит оттуда же);
	// падать на её достижении значило бы принуждать держать плохую конструкцию
	// живой ради зелёного. Способность разобрать выражение доказывают пробы
	// инъекции ниже — на синтетике, которая от состояния дерева не зависит, —
	// а перепись печатает, сколько чего осмотрено.
	if total.resolved == 0 {
		t.Fatalf("гейт не нашёл производителя НИ У ОДНОЙ подстановки в %d коллекциях "+
			"(подстановок: %d) — распознавание производителя сломано; молчание ничего "+
			"не доказывает", files, total.carves)
	}
	t.Logf("осмотрено коллекций: %d; шагов, режущих подсеть подстановкой: %d; "+
		"производитель найден у %d (собирают адрес выражением: %d, отдают готовый литерал: %d, "+
		"вырезают из опубликованного плана: %d); "+
		"производитель НЕ найден (посев/окружение — этим гейтом НЕ покрыты): %d",
		files, total.carves, total.resolved, total.computed, total.literal,
		total.planDerived, total.unresolve)

	if len(total.hits) > 0 {
		sort.Strings(total.hits)
		t.Errorf("найдено %d выражений, собирающих адрес подсети с головой, оборванной "+
			"в середине поля:\n  %s\n\n"+
			"Следствие: первое поле адреса задаётся не головой, а ШИРИНОЙ приписанного "+
			"значения. Одна цифра и две дают адреса из разных блоков, поэтому часть "+
			"значений энтропии уходит за объявленный план сети, и подсеть отвергается "+
			"синхронно. Падает при этом не шаг нарезки, а всё, что стоит на подсети "+
			"дальше по кейсу, — симптом уезжает от причины.\n\n"+
			"Исход: довести голову до разделителя поля ('fd00:' вместо 'fd', '10.' "+
			"вместо '10.1'), чтобы членство в плане определялось головой и не зависело "+
			"от вычисляемой части. Диапазон, случайно спасающий узкую голову, решением "+
			"не является: он объявлен в другом месте выражения и переживёт свою правку.",
			len(total.hits), strings.Join(total.hits, "\n  "))
	}
}

// ---- разбор ----

var (
	// Подстановка в поле адреса подсети: "ipv6CidrPrimary": "{{_zcV6Cidr}}".
	reCidrFieldVar = regexp.MustCompile(`"(ipv[46]Cidr(?:Primary|Blocks))"\s*:\s*\[?\s*"\{\{([^}]+)\}\}"`)
	// Производитель значения: pm.environment.set('_zcV6Cidr', <выражение до конца строки>).
	reCidrProducer = regexp.MustCompile(`pm\.(?:environment|variables|collectionVariables)\.set\(\s*'([^']+)'\s*,\s*(.+)$`)
	// Местное имя и его выражение: `var __v6 = __carve6(pm.environment.get('…'));`.
	// Нужно, чтобы отличить адрес, ВЫРЕЗАННЫЙ ИЗ ПЛАНА, от адреса, собранного из
	// собственной энтропии: у первого литеральной головы нет по построению.
	reCidrLocalAssign = regexp.MustCompile(`(?:var|let|const)\s+(\w+)\s*=\s*(.+)$`)
	// Голое имя как всё выражение производителя: `pm.environment.set('_v4', __v4);`.
	reCidrBareIdent = regexp.MustCompile(`^(\w+)\s*\)\s*;?\s*$`)
)

// planDerivedExpr — выражение производителя отдаёт значение, вырезанное из
// опубликованного плана: либо читает план само, либо возвращает имя, которому это
// значение присвоено выше по кейсу.
func planDerivedExpr(expr string, planDerived map[string]bool) bool {
	code := stripJSComments(expr)
	if rePlanGetV4.MatchString(code) || rePlanGetV6.MatchString(code) {
		return true
	}
	if m := reCidrBareIdent.FindStringSubmatch(strings.TrimSpace(code)); m != nil {
		return planDerived[m[1]]
	}
	return false
}

// headOfExpression — литеральная голова выражения и признак того, что за ней
// что-то приписывается. Возвращает («», false), когда выражение начинается не со
// строкового литерала (тогда голова не задана вовсе — это отдельный случай, и он
// тоже нарушение: план не определён ничем).
func headOfExpression(expr string) (head string, concatenated bool, ok bool) {
	e := strings.TrimSpace(expr)
	if len(e) == 0 || e[0] != '\'' {
		return "", false, false
	}
	end := strings.Index(e[1:], "'")
	if end < 0 {
		return "", false, false
	}
	head = e[1 : 1+end]
	rest := strings.TrimSpace(e[1+end+1:])
	// Приписывание — знак «+» сразу за литералом. Готовый литерал завершается
	// закрывающей скобкой вызова.
	concatenated = strings.HasPrefix(rest, "+")
	return head, concatenated, true
}

// endsOnFieldBoundary — голова заканчивается разделителем поля своего семейства.
func endsOnFieldBoundary(head string) bool {
	if head == "" {
		return false
	}
	last := head[len(head)-1]
	return last == ':' || last == '.'
}

func analyzeComputedCidrHeads(t *testing.T, rel string, body []byte) computedCidrScan {
	t.Helper()
	var coll pmCollection
	if err := json.Unmarshal(body, &coll); err != nil {
		t.Fatalf("%s: коллекция не разбирается: %v — файл не может быть ни засчитан "+
			"в перепись, ни молча пропущен", rel, err)
	}

	out := computedCidrScan{}
	for _, c := range coll.Item {
		group := c.Item
		if len(group) == 0 {
			group = []pmItem{c}
		}
		steps := flattenItems(group, nil)

		// Производители видны из любого скрипта кейса: значение ставится в окружение
		// и живёт до шага-потребителя. Собираем их по всему кейсу, а не только у
		// самого шага, — иначе гейт объявил бы «производитель не найден» там, где он
		// стоит шагом выше.
		producers := map[string]string{}
		// Имена, значение которых ВЫРЕЗАНО ИЗ ОПУБЛИКОВАННОГО ПЛАНА. У такого адреса
		// литеральной головы нет и быть не должно: голова приходит из плана, который
		// объявил посев, — то есть ровно из того места, куда это правило и ведёт.
		// Требовать головы здесь значило бы объявить находкой единственный законный
		// исход, и первым же таким срабатыванием гейт был бы снят.
		planDerived := map[string]bool{}
		for _, st := range steps {
			for _, line := range strings.Split(stepScript(st, "prerequest")+"\n"+stepScript(st, "test"), "\n") {
				code := stripJSComments(line)
				if m := reCidrLocalAssign.FindStringSubmatch(code); m != nil &&
					(rePlanGetV4.MatchString(m[2]) || rePlanGetV6.MatchString(m[2])) {
					planDerived[m[1]] = true
				}
				if m := reCidrProducer.FindStringSubmatch(line); m != nil {
					producers[m[1]] = m[2]
				}
			}
		}

		for _, st := range steps {
			if st.Request == nil || st.Request.Method != "POST" || st.Request.Body == nil {
				continue
			}
			url := strings.TrimSpace(rawURL(st.Request.URL))
			if !reSubnetCreate.MatchString(url) && !reSubnetAddBlocks.MatchString(url) {
				continue
			}
			for _, m := range reCidrFieldVar.FindAllStringSubmatch(st.Request.Body.Raw, -1) {
				field, varName := m[1], m[2]
				out.carves++
				expr, found := producers[varName]
				if !found {
					out.unresolve++
					continue
				}
				out.resolved++
				if planDerivedExpr(expr, planDerived) {
					out.planDerived++
					continue
				}
				head, concat, ok := headOfExpression(expr)
				if !ok {
					out.computed++
					out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+st.Name+
						"» берёт "+field+" из «"+varName+"», чьё выражение не начинается "+
						"со строкового литерала — головы у адреса нет вовсе")
					continue
				}
				if !concat {
					out.literal++
					continue
				}
				out.computed++
				if endsOnFieldBoundary(head) {
					continue
				}
				out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+st.Name+
					"» берёт "+field+" из «"+varName+"»: голова «"+head+
					"» оборвана в середине поля")
			}
		}
	}
	return out
}

// ---- инъекция в обе стороны ----

// TestComputedCidrHeadGateRedOnInjectedDefect — голова, оборванная в середине
// поля, краснит гейт И называет координату (кейс, шаг, переменную, саму голову).
func TestComputedCidrHeadGateRedOnInjectedDefect(t *testing.T) {
	got := analyzeComputedCidrHeads(t, "injected.json", []byte(`{"item":[{
      "name":"ZC-X — carve a v6 subnet from computed entropy",
      "item":[
        {"name":"prov-v6","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv6CidrPrimary\":\"{{_v6}}\"}"}},
          "event":[{"listen":"prerequest","script":{"exec":[
            "var __h = 3;",
            "pm.environment.set('_v6', 'fd' + (10 + (__h % 80)).toString(16) + ':1::/64');"]}},
            {"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]}
      ]}]}`))

	if got.carves != 1 || got.resolved != 1 {
		t.Fatalf("предмет не распознан: carves=%d resolved=%d", got.carves, got.resolved)
	}
	if len(got.hits) != 1 {
		t.Fatalf("дефект не найден: hits=%v", got.hits)
	}
	h := got.hits[0]
	for _, want := range []string{"ZC-X", "prov-v6", "_v6", "fd", "ipv6CidrPrimary"} {
		if !strings.Contains(h, want) {
			t.Errorf("гейт обязан назвать %q в координате, получено: %q", want, h)
		}
	}
}

// TestComputedCidrHeadGateSilentOnLawfulSameShape — четыре законные конструкции
// ТОЙ ЖЕ формы, и все сняты с дерева, а не выдуманы: голова доведена до точки
// (v4); голова доведена до двоеточия (v6); производитель отдаёт готовый литерал;
// адрес приходит из посева, и производителя в кейсе нет вовсе.
//
// Без этой стороны гейт запрещал бы вычисление адреса как таковое — и первым же
// ложным срабатыванием на законной v4-нарезке был бы снят.
func TestComputedCidrHeadGateSilentOnLawfulSameShape(t *testing.T) {
	got := analyzeComputedCidrHeads(t, "lawful.json", []byte(`{"item":[
      {"name":"A — v4 head reaches the dot","item":[
        {"name":"mk-v4","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"{{_v4}}\"}"}},
          "event":[{"listen":"prerequest","script":{"exec":[
            "var __o = 17;",
            "pm.environment.set('_v4', '10.' + __o + '.' + (__o + 1) + '.0/24');"]}}]}
      ]},
      {"name":"B — v6 head reaches the colon","item":[
        {"name":"mk-v6","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv6CidrPrimary\":\"{{_v6}}\"}"}},
          "event":[{"listen":"prerequest","script":{"exec":[
            "var __h = 3;",
            "pm.environment.set('_v6', 'fd00:' + (__h % 80).toString(16) + ':1::/64');"]}}]}
      ]},
      {"name":"C — producer hands over a ready literal","item":[
        {"name":"mk-lit","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv6CidrPrimary\":\"{{_lit}}\"}"}},
          "event":[{"listen":"prerequest","script":{"exec":[
            "pm.environment.set('_lit', 'fd00:dead:beef::/64');"]}}]}
      ]},
      {"name":"D — the address comes from the seed, no producer in the case","item":[
        {"name":"mk-seeded","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{netId}}\",\"ipv4CidrPrimary\":\"{{seedCidr}}\"}"}}}
      ]},
      {"name":"E — the address is carved from the published plan","item":[
        {"name":"mk-carved","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
          "body":{"raw":"{\"networkId\":\"{{existingNetworkId}}\",\"ipv6CidrPrimary\":\"{{_carved}}\"}"}},
          "event":[{"listen":"prerequest","script":{"exec":[
            "var __v6 = __carve6(pm.environment.get('existingNetworkV6Plan'));",
            "pm.environment.set('_carved', __v6);"]}}]}
      ]}]}`))

	if got.carves != 5 {
		t.Fatalf("предмет не распознан: carves=%d, ожидалось 5 — молчание на неразобранном "+
			"входе ничего не доказывает", got.carves)
	}
	// Адрес, вырезанный из плана, литеральной головы не несёт И нести не должен:
	// голова приходит из плана. Без этой стороны гейт объявлял бы находкой
	// единственный законный исход — и был бы снят первым же срабатыванием на нём.
	if got.planDerived != 1 {
		t.Fatalf("нарезка из опубликованного плана не распознана: planDerived=%d, "+
			"ожидалась 1", got.planDerived)
	}
	if got.computed != 2 {
		t.Fatalf("собираемых адресов распознано %d, ожидалось 2 — разбор выражения не "+
			"отработал", got.computed)
	}
	if got.literal != 1 {
		t.Fatalf("готовый литерал не распознан: literal=%d, ожидался 1 — различитель "+
			"«собирается против готового» не отработал", got.literal)
	}
	if got.unresolve != 1 {
		t.Fatalf("адрес из посева не распознан: unresolve=%d, ожидался 1", got.unresolve)
	}
	if len(got.hits) != 0 {
		t.Fatalf("гейт сработал на законной конструкции той же формы: %v", got.hits)
	}
}
