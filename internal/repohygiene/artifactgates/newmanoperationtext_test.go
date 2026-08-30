// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanoperationtext_test.go — гейт против утверждения о тексте, которое
// СЛАБЕЕ собственного заголовка: полоса операций отвечает текстом, который
// ВЫЧИСЛЯЕТСЯ из дерева, а кейсы проверяли в нём подстроку, приведённую к
// нижнему регистру.
//
// # Почему именно эта полоса, а не все сообщения дерева
//
// «Заголовок обещает дословность» — суждение о естественном языке, машинного
// предиката у него нет (`security.md` §«Механического детектора сборки НЕТ»:
// лексиконный подход над прозой уже проверялся и контроль в обе стороны не
// прошёл). Зато у ОДНОЙ полосы предмет вычислим целиком: арендаторские отказы
// `/operations/{id}` и `/operations/{id}:cancel` производятся двумя местами на
// всё дерево — общим слоем `pkg/operations` и краем `gateway/internal/opsproxy`,
// — и их форматные строки читаются оттуда же, откуда их читает компилятор.
//
// Значит здесь можно требовать не «похоже на текст», а РАВЕНСТВО вычисленному
// тексту. Граница названа прямо, чтобы молчание гейта об остальных сообщениях
// не читалось как их проверка.
//
// # Что именно запрещено и почему это не педантизм
//
//  1. `toLowerCase()` на сообщении полосы. Приведение регистра НЕ РАЗЛИЧАЕТ
//     регистр by construction, поэтому расхождение тона края с тоном владельца
//     не может покраснеть ни в одном прогоне. Так и вышло: край держал свою
//     запись текста «нет такой операции», она разошлась с владельцем регистром
//     одной буквы и прожила всю жизнь трёх кейсов, заведённых ровно ради того,
//     чтобы текст был частью контракта (#1370, #1401). Приведение при этом
//     законно в ОТРИЦАНИИ («сообщение не содержит X») — там оно РАСШИРЯЕТ
//     проверку, — и гейт отрицания не судит.
//  2. Только `include`, без равенства. Вхождение двух слов зеленеет на
//     сообщении о другом ресурсе и на любой добавке вокруг.
//  3. Утверждаемый текст, которого производитель НЕ ПРОИЗВОДИТ. Постоянные
//     части литерала обязаны найтись в каком-нибудь шаблоне полосы; иначе кейс
//     закрепляет формулировку, которой в дереве нет, — и краснеет на верном
//     продукте либо зеленеет на неверном (#1400, тот же класс на стороне
//     документа).
//
// # Что НЕ судится, и это названо, а не умолчано
//
// Утверждение о `Operation.error.message` — отказ ВЛАДЕЛЬЦА ресурса, доехавший
// внутри тела операции. Его текст производит домен (`Region <id> not found`,
// `Volume <id> not found`, …), полоса операций его лишь переносит, и вычислить
// его этими двумя каталогами нельзя. Такие шаги гейт пропускает и считает
// отдельной величиной переписи — иначе «ноль находок» покрывало бы то, чего
// гейт не читал.
package artifactgates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─── производители текстов полосы ────────────────────────────────────────────

// optProducerDirs — где живут производители арендаторской полосы операций.
// Утверждение о дереве, и оно проверяется: каталог обязан существовать и давать
// непустой набор шаблонов.
var optProducerDirs = []string{
	filepath.Join("pkg", "operations"),
	filepath.Join("gateway", "internal", "opsproxy"),
}

// optStatusText — форматная строка отказа. Читается ровно в той форме, в какой
// её читает компилятор: `status.Error(codes.X, "…")` / `status.Errorf(codes.X, "…", …)`.
var optStatusText = regexp.MustCompile(`status\.Errorf?\(\s*codes\.[A-Za-z]+\s*,\s*"((?:[^"\\]|\\.)*)"`)

// optVerb — глагол формата внутри шаблона. Всё, что не глагол, — постоянная
// часть, и она обязана дойти до клиента дословно.
var optVerb = regexp.MustCompile(`%[#+\-0-9.]*[a-zA-Z]`)

// optGoComment — гейт обязан отличать код от комментария: имена текстов стоят и
// в объяснениях рядом, и засчитывать их за производителя значило бы доказывать
// предпосылку её собственным пересказом.
func optGoComment(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "//") {
		return ""
	}
	return line
}

// optTemplates — шаблоны текстов полосы, ВЫЧИСЛЕННЫЕ переписью производителей.
// Второе значение — покрытые каталоги: каталог, которого перепись не нашла,
// означает, что полоса переехала, а гейт судит по половине производителей.
func optTemplates(root string, goFiles []string) (map[string]bool, map[string]bool, error) {
	out := map[string]bool{}
	covered := map[string]bool{}
	for _, rel := range goFiles {
		dir := ""
		for _, d := range optProducerDirs {
			if strings.HasPrefix(rel, d+string(filepath.Separator)) {
				dir = d
				break
			}
		}
		if dir == "" || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		covered[dir] = true
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		for _, raw := range strings.Split(string(b), "\n") {
			for _, m := range optStatusText.FindAllStringSubmatch(optGoComment(raw), -1) {
				out[optExpandQuoted(m[1])] = true
			}
		}
	}
	return out, covered, nil
}

// optExpandQuoted раскрывает `%q` в `"%s"`: клиент видит НЕ глагол, а кавычки,
// которые тот добавляет. Гейт, сверяющий литерал кейса с сырым `%q`, объявил бы
// находкой верное ожидание `invalid operation id "<X>"` — то есть краснел бы на
// починенном кейсе. Раскрытие — свойство языка форматирования, а не вкус: `%q`
// на строке всегда даёт двойные кавычки.
func optExpandQuoted(tmpl string) string {
	return strings.ReplaceAll(tmpl, "%q", `"%s"`)
}

// optConstParts — постоянные части шаблона: то, что клиент увидит дословно.
func optConstParts(tmpl string) []string {
	var out []string
	for _, p := range optVerb.Split(tmpl, -1) {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// ─── разбор шага ─────────────────────────────────────────────────────────────

var (
	optOpsURL = regexp.MustCompile(`/operations/`)
	// Утверждение о сообщении ВЕРХНЕГО УРОВНЯ. Форма `error.message` исключается
	// отдельно (см. шапку): её текст производит владелец ресурса, не полоса.
	optTopMessage = regexp.MustCompile(`(?:pm\.response\.json\(\)|\bj\b|\bjj\b|\b_j\b|\bm\b)\s*(?:&&\s*[\w.]+\s*)?\.?\s*message|\bmessage\b`)
	optErrMessage = regexp.MustCompile(`error\s*(?:&&|\|\||\))?[^;]{0,40}?\.message|\.error\.message|error\s*\|\|\s*\{\}\)\.message`)
	optAssertOn   = regexp.MustCompile(`\.to\.(?:include|contain|eql|equal|satisfy)\(`)
	optNegation   = regexp.MustCompile(`\.to\.not\.`)
	optLower      = regexp.MustCompile(`toLowerCase\(\)`)
	optEquality   = regexp.MustCompile(`\.to\.(?:eql|equal)\(`)
	optJSLiteral  = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"`)
)

type optFinding struct {
	collection, casePath, step, why string
}

func (f optFinding) String() string {
	return fmt.Sprintf("%s :: %s / %s — %s", f.collection, f.casePath, f.step, f.why)
}

type optCensus struct {
	collections, steps, opsSteps int
	opsWithTopMessage            int
	opsWithOwnerErrorOnly        int
}

// optStepBinds — переменные, в которые шаг положил сообщение верхнего уровня
// (`const m = pm.response.json().message`). Без них утверждение `pm.expect(m)`
// не опознаётся, и половина шагов стала бы гейту невидима — та самая слепая
// зона, которая от «ноль находок» неотличима.
var optBind = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*([^;]*)`)

// auditOperationLaneText — весь разбор одним входом, чтобы инъекция гоняла ТУ ЖЕ
// функцию, а не свою копию логики.
func auditOperationLaneText(root string, cols []string, templates map[string]bool) ([]optFinding, optCensus, error) {
	var findings []optFinding
	var cen optCensus

	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		var walk func(items []nmItem, path []string)
		walk = func(items []nmItem, path []string) {
			for _, it := range items {
				if it.isFolder() {
					walk(it.Item, append(path, it.Name))
					continue
				}
				cen.steps++
				if !optOpsURL.MatchString(it.rawURL()) {
					continue
				}
				cen.opsSteps++

				var lines []string
				for _, ev := range it.Event {
					if ev.Listen == "test" {
						lines = append(lines, ev.Script.Exec...)
					}
				}
				f, kind := auditOperationLaneStep(lines, templates)
				switch kind {
				case optKindTop:
					cen.opsWithTopMessage++
				case optKindOwnerOnly:
					cen.opsWithOwnerErrorOnly++
				}
				for _, why := range f {
					findings = append(findings, optFinding{
						collection: rel, casePath: strings.Join(path, " / "),
						step: it.Name, why: why,
					})
				}
			}
		}
		walk(col.Item, nil)
	}
	return findings, cen, nil
}

type optKind int

const (
	optKindNone optKind = iota
	optKindTop
	optKindOwnerOnly
)

// auditOperationLaneStep судит ОДИН шаг и возвращает причины находок и вид шага.
func auditOperationLaneStep(lines []string, templates map[string]bool) ([]string, optKind) {
	// Переменные, несущие сообщение владельца (`j.error.message`): утверждение о
	// них — не предмет этого гейта.
	ownerVars := map[string]bool{}
	topVars := map[string]bool{}
	for _, raw := range lines {
		line := slpStripJSComment(raw)
		for _, m := range optBind.FindAllStringSubmatch(line, -1) {
			switch {
			case optErrMessage.MatchString(m[2]):
				ownerVars[m[1]] = true
			case optTopMessage.MatchString(m[2]):
				topVars[m[1]] = true
			}
		}
	}

	var why []string
	kind := optKindNone
	sawOwner := false
	for _, raw := range lines {
		line := slpStripJSComment(raw)
		if !optAssertOn.MatchString(line) || optNegation.MatchString(line) {
			continue
		}
		// Про чьё сообщение утверждение?
		isOwner := optErrMessage.MatchString(line)
		for v := range ownerVars {
			if regexp.MustCompile(`\bexpect\(\s*` + regexp.QuoteMeta(v) + `\b`).MatchString(line) {
				isOwner = true
			}
		}
		if isOwner {
			sawOwner = true
			continue
		}
		isTop := optTopMessage.MatchString(line)
		for v := range topVars {
			if regexp.MustCompile(`\bexpect\(\s*` + regexp.QuoteMeta(v) + `\b`).MatchString(line) {
				isTop = true
			}
		}
		if !isTop {
			continue
		}
		kind = optKindTop

		if optLower.MatchString(line) {
			why = append(why, "утверждение о сообщении полосы приведено к нижнему регистру — "+
				"расхождение тона по регистру не может покраснеть НИКОГДА (`toLowerCase` не различает "+
				"регистр by construction). Текст полосы вычисляется из "+strings.Join(optProducerDirs, " и ")+
				": утверждай его дословно")
		}
		if !optEquality.MatchString(line) {
			why = append(why, "утверждается ВХОЖДЕНИЕ подстроки, а не равенство: текст полосы известен "+
				"целиком, поэтому `to.eql` — тот же труд и другое утверждение. Вхождение зеленеет и на "+
				"сообщении о другом ресурсе, и на любой добавке вокруг")
		}
		// Постоянные части утверждаемого текста обязаны найтись у производителя.
		//
		// Литералы берутся ТОЛЬКО из аргумента сравнения, а не из всей строки:
		// имя `pm.test('…')` и пояснение второго аргумента `expect(v, '…')` —
		// проза для человека, у неё производителя нет и быть не должно. Гейт,
		// читающий их наравне с утверждаемым текстом, краснел бы на собственном
		// объяснении — тот же класс, который он ловит.
		for _, lit := range optJSLiteral.FindAllStringSubmatch(optComparedPart(line), -1) {
			s := lit[1]
			if s == "" {
				s = lit[2]
			}
			s = strings.TrimSpace(s)
			// Короткое — имя утверждения, ключ окружения, склейка: о тексте не говорит.
			if len(s) < 8 || !strings.Contains(s, " ") {
				continue
			}
			if optLiteralHasProducer(s, templates) {
				continue
			}
			why = append(why, fmt.Sprintf(
				"утверждаемый текст %q не производит НИ ОДИН шаблон полосы (%d шаблонов из %v) — "+
					"кейс закрепляет формулировку, которой в дереве нет: он либо краснеет на верном "+
					"продукте, либо зеленеет на неверном",
				s, len(templates), optProducerDirs))
		}
	}
	if kind == optKindNone && sawOwner {
		kind = optKindOwnerOnly
	}
	return why, kind
}

// optTemplateRe — шаблон как образец с подстановкой: глагол формата означает
// «любое значение», постоянные части обязаны совпасть дословно.
func optTemplateRe(tmpl string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString(`(?i)^`)
	last := 0
	for _, loc := range optVerb.FindAllStringIndex(tmpl, -1) {
		b.WriteString(regexp.QuoteMeta(tmpl[last:loc[0]]))
		b.WriteString(`.*`)
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(tmpl[last:]))
	b.WriteString(`$`)
	return regexp.MustCompile(b.String())
}

// optComparedPart — хвост строки, начиная с оператора сравнения. Всё, что до
// него, — имя пробы и пояснение отказа, то есть речь к человеку.
func optComparedPart(line string) string {
	loc := optAssertOn.FindStringIndex(line)
	if loc == nil {
		return ""
	}
	return line[loc[0]:]
}

// optLiteralHasProducer — есть ли у литерала производитель. Литерал сверяется с
// ПОСТОЯННЫМИ частями шаблонов: `operation %s not found` даёт «operation » и
// « not found», и утверждение вправе нести любую из них целиком либо весь текст
// с подставленным глаголом.
func optLiteralHasProducer(lit string, templates map[string]bool) bool {
	low := strings.ToLower(lit)
	for tmpl := range templates {
		lt := strings.ToLower(tmpl)
		if strings.Contains(lt, low) {
			return true
		}
		// Литерал может нести УЖЕ ПОДСТАВЛЕННОЕ значение — так пишется ожидание
		// там, где идентификатор стоит в пути литералом, а не переменной
		// окружения. Форма законна и распространена; распознаватель, её не
		// знающий, объявил бы находкой верное ожидание, а всё записанное в ней
		// осталось бы вне наблюдения (`testing.md` §«Гейт на класс», п. 7).
		if optTemplateRe(tmpl).MatchString(lit) {
			return true
		}
		// Литерал может быть склейкой хвоста и головы вокруг подстановки —
		// тогда каждая его непустая часть обязана найтись в шаблоне.
		ok := true
		for _, p := range optConstParts(lit) {
			if !strings.Contains(lt, strings.ToLower(strings.TrimSpace(p))) {
				ok = false
				break
			}
		}
		if ok && len(optConstParts(lit)) > 0 {
			return true
		}
	}
	return false
}

// optGoFiles — отслеживаемые .go дерева. Вынесено функцией, чтобы её же звала
// проба предпосылки: перепись, собранная другим способом, доказывала бы
// свойство своей копии.
func optGoFiles(tt *trackedTree) []string {
	out := make([]string, 0, len(tt.files))
	for rel := range tt.files {
		if strings.HasSuffix(rel, ".go") {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func optCollections(tt *trackedTree) []string {
	var out []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

func TestOperationLaneMessageIsAssertedVerbatim(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — из ИНДЕКСА git: под корнем лежат рабочие копии агентов и
	// распаковки отчётов прогонов, и вердикт по ним был бы свойством чужого
	// рабочего каталога, а не коммита.
	tt := newTrackedTree(t, root)

	templates, covered, err := optTemplates(root, optGoFiles(tt))
	if err != nil {
		t.Fatal(err)
	}
	// Предпосылка первая: каталог, который перепись назвала и не нашла, означает
	// переехавшую полосу — тогда «текст вычислен» перестаёт быть замером.
	for _, d := range optProducerDirs {
		if !covered[d] {
			t.Fatalf("в индексе git нет ни одного не-тестового .go под %s — утверждение о том, "+
				"где живёт полоса операций, пережило свой предмет", d)
		}
	}
	// Предпосылка вторая: шаблоны непусты. Пустой набор означает ослепший
	// распознаватель, и тогда всякий текст кейса «не имеет производителя».
	if len(templates) == 0 {
		t.Fatal("в производителях полосы операций не найдено НИ ОДНОГО текста отказа — " +
			"распознаватель ослеп; чинить надо гейт, а не выходить успехом")
	}

	cols := optCollections(tt)
	findings, cen, err := auditOperationLaneText(root, cols, templates)
	if err != nil {
		t.Fatal(err)
	}

	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	if cen.opsSteps == 0 {
		t.Fatalf("в %d шагах не найдено НИ ОДНОГО шага полосы операций — гейт ничего не осматривает", cen.steps)
	}
	if cen.opsWithTopMessage == 0 {
		t.Fatalf("шагов полосы %d, из них с утверждением о СОБСТВЕННОМ сообщении полосы 0 — "+
			"распознаватель утверждения ослеп: «ноль находок» здесь означало бы «ноль прочитанного»",
			cen.opsSteps)
	}

	names := make([]string, 0, len(templates))
	for tmpl := range templates {
		names = append(names, tmpl)
	}
	sort.Strings(names)
	t.Logf("осмотрено: коллекций %d, шагов %d; шагов полосы операций %d, из них утверждают о "+
		"СОБСТВЕННОМ сообщении полосы %d, только о сообщении владельца ресурса %d (не судятся — "+
		"их текст производит домен, а не полоса). Шаблонов полосы %d, вычислены переписью %v: %q",
		cen.collections, cen.steps, cen.opsSteps, cen.opsWithTopMessage, cen.opsWithOwnerErrorOnly,
		len(templates), optProducerDirs, names)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "утверждений о тексте полосы операций, которые слабее собственного заголовка: %d\n\n", len(findings))
		b.WriteString("Текст этой полосы известен ЦЕЛИКОМ — он вычисляется из её производителей, —\n")
		b.WriteString("поэтому «содержит два слова в нижнем регистре» здесь не утверждение, а его вид.\n")
		b.WriteString("Чинится в cases/*.py набора: равенство вычисленному тексту, без приведения\n")
		b.WriteString("регистра; коллекции затем перегенерируются scripts/gen.py.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}
