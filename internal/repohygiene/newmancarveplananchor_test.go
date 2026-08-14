// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmancarveplananchor_test.go — гейт по коллекциям: шаг, режущий подсеть в сети,
// СОЗДАННОЙ ВНЕ КЕЙСА, берёт адрес из плана, который эта сеть объявила, — а не
// выводит его сам.
//
// # Почему этот гейт отдельный
//
// Соседний `newmansubnetsupernet_test.go` требует, чтобы у родителя план БЫЛ, и
// честно печатает, чего не покрывает: родителя, приходящего в кейс извне
// (`{{existingNetworkId}}` — его создаёт посев), в коллекции не видно, там только
// имя переменной. На день заведения этого гейта в той строке стояло число 70 —
// именно столько шагов оставались вне всякой проверки. Инцидент случился ровно
// там.
//
// # Предмет
//
// Наличие плана и ПОПАДАНИЕ В НЕГО — разные требования, и второе отказывает
// отдельно: `subnet CIDR <X> is not within any network CIDR block`. Кейс, который
// собирает адрес сам (из хеша прогона, случайного числа, склейки строк), попадает
// в план или мимо в зависимости от того, ЧЕЙ посев создал сеть и какой хеш выпал
// прогону, — то есть проходит через раз и по причине, не названной нигде.
//
// Наблюдалось: набор nlb собирал адрес v6 склейкой `'fd' + число.toString(16)`.
// Текстовый префикс — не числовой: когда число укладывалось в один шестнадцатеричный
// разряд, первый хекстет получался трёхразрядным и уезжал за пределы плана. Мимо
// одного плана адрес уходил на 7.5% прогонов, мимо второго (посев того же набора для
// другой посадки объявляет план у́же) — на всех. Отказ прилетал шагу нарезки, а
// падали за ним шаги, которые нарезки не делали: не захвачен идентификатор подсети —
// не отправлены запросы, которые его адресуют.
//
// # Требование
//
// Адрес такого шага обязан быть ВЫВЕДЕН ИЗ ПЛАНА: посев публикует объявленный им
// план переменными окружения `existingNetworkV4Plan` / `existingNetworkV6Plan`, а
// кейс режет внутри. Тогда посев волен менять план, и кейс следует за ним сам —
// вместо второго места об одном предмете, из которых верно одно.
//
// # Что гейт считает выведением
//
// ВЫЗОВ чтения переменной плана того же семейства в скрипте кейса, а не упоминание
// её имени: комментарий, объясняющий вывод, выведением не является. Комментарии из
// скрипта снимаются до сопоставления — иначе гейт зеленел бы на объяснении вместо
// исполнения (`testing.md` §«Гейт читает исполняемую часть, а не текст»).
package repohygiene

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Канонические имена, которыми посев публикует объявленный им адресный план.
// Названы здесь, потому что гейт именно их и требует: читатель отказа обязан
// узнать, что́ ему завести, не выясняя это по чужим файлам.
const (
	publishedPlanV4 = "existingNetworkV4Plan"
	publishedPlanV6 = "existingNetworkV6Plan"
)

type carveAnchorScan struct {
	cases      int
	outOfCase  int // шагов-нарезок, чей родитель создан вне кейса
	anchored   int // из них взявших адрес из опубликованного плана
	inCase     int // нарезок с родителем В кейсе — предмет соседнего гейта
	hits       []string
	sawPlanGet bool // распознаватель чтения плана срабатывал хотя бы раз
}

// TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan — ни один шаг, режущий подсеть
// в сети из посева, не выводит адрес сам.
func TestOutOfCaseCarveTakesItsCidrFromThePublishedPlan(t *testing.T) {
	root := repoRoot(t)

	files := 0
	total := carveAnchorScan{}
	forEachCollectionForSubnetSupernet(t, root, func(rel string, body []byte) {
		files++
		res := analyzeCarvePlanAnchoring(t, rel, body)
		total.cases += res.cases
		total.outOfCase += res.outOfCase
		total.anchored += res.anchored
		total.inCase += res.inCase
		total.sawPlanGet = total.sawPlanGet || res.sawPlanGet
		total.hits = append(total.hits, res.hits...)
	})

	if files == 0 {
		t.Fatalf("гейт не прочитал ни одной коллекции — предпосылка обхода сломана, "+
			"молчание ничего не доказывает (корни: %v)", subnetSupernetScanRoots)
	}
	// Предпосылка ПРЕДМЕТА: гейт судит только шаги с родителем извне кейса. Если их
	// не нашлось ни одного, он не рассмотрел ничего, и его молчание означает «не
	// смотрел». Ровно этим числом соседний гейт и объявляет свою слепую зону.
	if total.outOfCase == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОГО шага, режущего подсеть в сети из посева, "+
			"в %d коллекциях (нарезок с родителем в кейсе: %d) — распознавание предмета "+
			"сломано, молчание ничего не доказывает", files, total.inCase)
	}

	t.Logf("осмотрено коллекций: %d; кейсов: %d; шагов, режущих подсеть в сети ИЗ ПОСЕВА: %d; "+
		"из них берут адрес из опубликованного плана (%s / %s): %d; "+
		"нарезок с родителем В кейсе (предмет TestSubnetFixtureNeverCarvesFromAPlanlessNetwork): %d",
		files, total.cases, total.outOfCase, publishedPlanV4, publishedPlanV6,
		total.anchored, total.inCase)

	if len(total.hits) > 0 {
		sort.Strings(total.hits)
		t.Errorf("найдено %d шагов, режущих подсеть в сети из посева адресом, который "+
			"выведен не из её плана:\n  %s\n\n"+
			"Следствие: адрес попадает в объявленный план или мимо него в зависимости от "+
			"того, чей посев создал сеть и какой хеш выпал прогону. Мимо — синхронный отказ "+
			"«subnet CIDR ... is not within any network CIDR block», идентификатор подсети не "+
			"захвачен, и падают шаги, которые нарезки не делали: они адресуют несостоявшуюся "+
			"фикстуру.\n\n"+
			"Исход: посев публикует объявленный им план переменными %s / %s, кейс режет "+
			"ВНУТРИ него (в наборе nlb — общий помощник `carve_cidr_pre` в scripts/gen.py). "+
			"Прошивать адрес под конкретный посев нельзя: это второе место об одном "+
			"предмете, и разойдутся они молча.",
			len(total.hits), strings.Join(total.hits, "\n  "), publishedPlanV4, publishedPlanV6)
		return
	}
	// Предпосылка РАСПОЗНАВАТЕЛЯ — проверяется, только когда находок нет: до починки
	// красным обязан быть сам дефект, а не рассказ о сломанном распознавателе.
	if total.anchored == 0 {
		t.Fatalf("ни одна из %d нарезок в сети из посева не распознана как берущая адрес "+
			"из плана — распознаватель чтения плана не подтверждён на живых данных, "+
			"поэтому «ноль находок» тут значит «не смотрел»", total.outOfCase)
	}
}

// ---- разбор ----

var (
	// Вызов чтения переменной плана. Именно ВЫЗОВ: голое упоминание имени
	// (в том числе в строке отказа) выведением не является.
	rePlanGetV4 = regexp.MustCompile(`pm\.(?:environment|variables|globals)\.get\(\s*['"` + "`" + `]` + publishedPlanV4 + `['"` + "`" + `]`)
	rePlanGetV6 = regexp.MustCompile(`pm\.(?:environment|variables|globals)\.get\(\s*['"` + "`" + `]` + publishedPlanV6 + `['"` + "`" + `]`)

	reWholeVar  = regexp.MustCompile(`^\{\{([A-Za-z0-9_]+)\}\}$`)
	reEnvSetVar = regexp.MustCompile(`pm\.environment\.set\(\s*['"` + "`" + `]([A-Za-z0-9_]+)['"` + "`" + `]`)
)

// carveCidrFields — поля контракта, которыми на подсеть приходит адрес.
var carveCidrFields = map[string]bool{
	"ipv4CidrPrimary": true, "ipv6CidrPrimary": true,
	"ipv4CidrBlocks": true, "ipv6CidrBlocks": true,
}

// stripJSComments — снять `//`-комментарии, не трогая содержимое строковых литералов.
// Гейт обязан читать исполняемую часть: без этого объяснение вывода в комментарии
// засчиталось бы за сам вывод.
func stripJSComments(src string) string {
	var b strings.Builder
	var quote rune
	esc := false
	rs := []rune(src)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if quote != 0 {
			b.WriteRune(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == quote:
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			b.WriteRune(c)
			continue
		}
		if c == '/' && i+1 < len(rs) && rs[i+1] == '/' {
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			b.WriteRune('\n')
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// planReadersInCase — какие семейства плана кейс ЧИТАЕТ, и в каких переменных
// публикует результат. Ключ — имя переменной, значение — прочитанные семейства.
func planReadersInCase(steps []pmItem) (map[string]planFamilies, bool) {
	out := map[string]planFamilies{}
	saw := false
	for _, s := range steps {
		code := stripJSComments(stepScript(s, "prerequest") + "\n" + stepScript(s, "test"))
		got := planFamilies{v4: rePlanGetV4.MatchString(code), v6: rePlanGetV6.MatchString(code)}
		if !got.any() {
			continue
		}
		saw = true
		for _, m := range reEnvSetVar.FindAllStringSubmatch(code, -1) {
			cur := out[m[1]]
			cur.v4 = cur.v4 || got.v4
			cur.v6 = cur.v6 || got.v6
			out[m[1]] = cur
		}
	}
	return out, saw
}

// carvedValues — адреса, которые шаг кладёт в подсеть, по семействам.
func carvedValues(raw string) map[string][]string {
	out := map[string][]string{}
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &body) != nil {
		return out
	}
	for k, v := range body {
		if !carveCidrFields[k] {
			continue
		}
		fam := "v4"
		if strings.HasPrefix(k, "ipv6") {
			fam = "v6"
		}
		var one string
		if json.Unmarshal(v, &one) == nil {
			out[fam] = append(out[fam], one)
			continue
		}
		var many []string
		if json.Unmarshal(v, &many) == nil {
			out[fam] = append(out[fam], many...)
		}
	}
	return out
}

func analyzeCarvePlanAnchoring(t *testing.T, rel string, body []byte) carveAnchorScan {
	t.Helper()
	var coll pmCollection
	if err := json.Unmarshal(body, &coll); err != nil {
		t.Fatalf("%s: коллекция не разбирается: %v — файл не может быть ни засчитан "+
			"в перепись, ни молча пропущен", rel, err)
	}

	out := carveAnchorScan{}
	for _, c := range coll.Item {
		out.cases++
		group := c.Item
		if len(group) == 0 {
			group = []pmItem{c}
		}
		steps := flattenItems(group, nil)

		// Сети, созданные В ЭТОМ кейсе: их план виден здесь же и его держит
		// соседний гейт. Предмет этого — только родитель ИЗВНЕ.
		inCaseNets := map[string]bool{}
		anyInCaseNet := false
		for _, s := range steps {
			if s.Request == nil || s.Request.Method != "POST" {
				continue
			}
			if !reNetCreate.MatchString(strings.TrimSpace(rawURL(s.Request.URL))) {
				continue
			}
			if expectsRefusal(s) {
				continue // сеть не появится — родителем она не станет
			}
			anyInCaseNet = true
			for _, v := range publishedVars(s) {
				inCaseNets[v] = true
			}
		}

		readers, saw := planReadersInCase(steps)
		out.sawPlanGet = out.sawPlanGet || saw

		for _, step := range steps {
			if step.Request == nil || step.Request.Method != "POST" {
				continue
			}
			url := strings.TrimSpace(rawURL(step.Request.URL))
			if !reSubnetCreate.MatchString(url) && !reSubnetAddBlocks.MatchString(url) {
				continue
			}
			if expectsRefusal(step) {
				continue // предмет пробы — сам отказ
			}
			raw := ""
			if step.Request.Body != nil {
				raw = step.Request.Body.Raw
			}
			values := carvedValues(raw)
			if len(values) == 0 {
				continue // шаг адреса не несёт — резать нечего
			}
			ref := ""
			if m := reNetworkIDRef.FindStringSubmatch(raw); m != nil {
				ref = m[1]
			}
			// Родитель считается «в кейсе», если кейс вообще создавал сеть (тогда
			// её план виден здесь же и его держит соседний гейт) либо если тело
			// называет опубликованную кейсом переменную. `:add-cidr-blocks` сети
			// в теле не называет — родителя наследует от подсети кейса, поэтому
			// первого условия достаточно и для него.
			if anyInCaseNet || inCaseNets[ref] {
				out.inCase++
				continue
			}
			out.outOfCase++

			bad := []string{}
			for fam, vals := range values {
				for _, v := range vals {
					if anchoredToPlan(v, fam, readers) {
						continue
					}
					bad = append(bad, fam+"="+v)
				}
			}
			if len(bad) == 0 {
				out.anchored++
				continue
			}
			sort.Strings(bad)
			out.hits = append(out.hits, rel+" :: "+c.Name+" :: шаг «"+step.Name+
				"» режет "+strings.Join(bad, ", ")+" в сети «{{"+ref+"}}» из посева, "+
				"не прочитав её плана")
		}
	}
	return out
}

// ---- инъекция в обе стороны ----

// carveShape — одна и та же форма кейса: сеть НЕ создаётся (родитель из посева),
// шаг режет подсеть адресом из переменной, которую готовит его же pre-request.
// Меняется ровно одно — откуда pre-request берёт префикс.
func carveShape(producer string) []byte {
	return []byte(`{"item":[{"name":"C — carve in a seeded network","item":[
      {"name":"prov-subnet","request":{"method":"POST","url":{"raw":"{{baseUrl}}/vpc/v1/subnets"},
        "body":{"raw":"{\"networkId\":\"{{existingNetworkId}}\",\"ipv6CidrPrimary\":\"{{subCidr6}}\"}"}},
        "event":[
          {"listen":"prerequest","script":{"exec":[` + producer + `]}},
          {"listen":"test","script":{"exec":[
            "pm.test('status 200', () => pm.expect(pm.response.code).to.eql(200));"]}}]}
    ]}]}`)
}

// TestCarvePlanAnchorGateRedOnInjectedDefect — адрес, выведённый мимо плана,
// краснит гейт И называет координату.
func TestCarvePlanAnchorGateRedOnInjectedDefect(t *testing.T) {
	got := analyzeCarvePlanAnchoring(t, "injected.json", carveShape(
		`"pm.environment.set('subCidr6', 'fd' + (10 + (Date.now() % 80)).toString(16) + ':1::/64');"`))

	if got.outOfCase != 1 {
		t.Fatalf("шаг не распознан как нарезка в сети из посева: outOfCase=%d — "+
			"на неразобранном входе находка ничего не доказывает", got.outOfCase)
	}
	if len(got.hits) != 1 {
		t.Fatalf("дефект не найден: hits=%v", got.hits)
	}
	if !strings.Contains(got.hits[0], "prov-subnet") || !strings.Contains(got.hits[0], "subCidr6") {
		t.Errorf("находка обязана назвать шаг и адрес, получено: %q", got.hits[0])
	}
}

// TestCarvePlanAnchorGateSilentOnLawfulSameShape — та же форма, но префикс взят из
// опубликованного плана. Без этой стороны гейт запрещал бы нарезку как таковую и
// был бы снят первым же ложным срабатыванием.
func TestCarvePlanAnchorGateSilentOnLawfulSameShape(t *testing.T) {
	got := analyzeCarvePlanAnchoring(t, "lawful.json", carveShape(
		`"var __p = pm.environment.get('`+publishedPlanV6+`');",`+
			`"pm.environment.set('subCidr6', __carve6(__p));"`))

	if got.outOfCase != 1 {
		t.Fatalf("шаг не распознан: outOfCase=%d — молчание на неразобранном входе "+
			"ничего не доказывает", got.outOfCase)
	}
	if !got.sawPlanGet {
		t.Fatalf("распознаватель чтения плана не сработал на входе, который его содержит — " +
			"молчание гейта объясняется сломанным распознавателем, а не законностью входа")
	}
	if got.anchored != 1 {
		t.Fatalf("законная нарезка не засчитана выведенной из плана: anchored=%d", got.anchored)
	}
	if len(got.hits) != 0 {
		t.Errorf("гейт сработал на законной конструкции той же формы: %v", got.hits)
	}
}

// TestCarvePlanAnchorGateReadsCodeNotComment — упоминание плана в КОММЕНТАРИИ
// выведением не является.
//
// Без этого гейт зеленел бы на объяснении вместо исполнения: комментарий, который
// пишут как раз тогда, когда рядом делают исключение, — самый вероятный вход
// (`testing.md` §«Гейт читает исполняемую часть, а не текст»).
func TestCarvePlanAnchorGateReadsCodeNotComment(t *testing.T) {
	got := analyzeCarvePlanAnchoring(t, "commented.json", carveShape(
		`"// адрес согласован с pm.environment.get('`+publishedPlanV6+`') вручную",`+
			`"pm.environment.set('subCidr6', 'fd00:1::/64');"`))

	if got.outOfCase != 1 {
		t.Fatalf("шаг не распознан: outOfCase=%d", got.outOfCase)
	}
	if got.sawPlanGet {
		t.Fatalf("распознаватель засчитал КОММЕНТАРИЙ за чтение плана — гейт читает " +
			"текст, а не исполняемую часть")
	}
	if len(got.hits) != 1 {
		t.Fatalf("адрес, согласованный «вручную», обязан быть находкой: hits=%v", got.hits)
	}
}

// anchoredToPlan — адрес выведен из опубликованного плана своего семейства.
func anchoredToPlan(value, fam string, readers map[string]planFamilies) bool {
	m := reWholeVar.FindStringSubmatch(value)
	if m == nil {
		// Адрес прошит в кейсе литералом: план сети из посева здесь неизвестен, и
		// совпадение с ним — совпадение, а не свойство.
		return false
	}
	got := readers[m[1]]
	if fam == "v6" {
		return got.v6
	}
	return got.v4
}
