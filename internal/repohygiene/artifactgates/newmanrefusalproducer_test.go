// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanrefusalproducer_test.go — гейт против утверждения о тексте отказа, у
// которого в дереве НЕТ ПРОИЗВОДИТЕЛЯ ВОВСЕ.
//
// # Предмет
//
// Тон сообщений — часть контракта (`api-conventions.md` §Error-format): тексты
// стабильны и меняются осознанно. Но ОСОЗНАННАЯ смена тона обнаруживалась
// только прогоном стенда: кейс продолжает пинить прежний текст, продукт отдаёт
// новый, и расходятся они молча до 25-минутного прогона.
//
// Наблюдалось (#1742): правка развела слитый отказ по ссылке на две
// противоположные полосы — «ссылаемого нет» лечится созданием, «ещё
// используется» освобождением, — а два сквозных кейса продолжали утверждать
// прежний общий текст и покраснели НА СТЕНДЕ, а не на сборке.
//
// # Что гейт судит, и чего он НЕ судит
//
// Он судит СОВПАДЕНИЕ СТРОК, а не правильность тона. Осознанная смена текста
// остаётся законной — она просто перестаёт быть невидимой до прогона: автор
// правит кейс тем же изменением, каким правит продукт. Гейт не имеет мнения о
// том, каким текст ДОЛЖЕН быть; он утверждает лишь, что утверждаемый текст
// кто-то в дереве производит.
//
// # Чем он отличается от соседа по тону
//
// `newmanrefusaltone_test.go` судит утверждения, приводящие регистр, и на
// литерале, у которого производителя нет вовсе, ЯВНО молчит — «это предмет
// соседнего гейта, не этого». Соседа не было; это он.
//
// # Почему население — только объявленные помощником утверждения
//
// Находка обязана быть ДОКАЗАТЕЛЬСТВОМ, а не суждением. «Литерал не найден у
// производителя» само по себе доказательством не является: литералом в кейсе
// бывает имя переменной окружения, имя права, поле запроса, отрендеренный текст
// с подставленным значением — у всего этого производителя нет и быть не должно.
// Перепись 2026-08-31 по всем утверждениям о `message`: 743 литерала, из них без
// производителя 61 — и подавляющее большинство законны. Гейт, у которого столько
// ложных находок, отключают первым (`testing.md` §«Гейт на класс», п. 2б).
//
// Поэтому мерка здесь — ОБЪЯВЛЕНИЕ САМОГО АВТОРА, ровно как у видов 2-3 соседа:
// судятся утверждения, собранные общим слоем `assert_refusal_message` /
// `assert_refusal_message_contains`, чьи шапки прямо говорят, что вход — «текст
// владельца дословно». Автор объявил, что это текст продукта; если продукт его
// не производит, утверждение не может пройти НИ ПРИ КАКОМ ответе.
//
// # Обе формы записи известны — иначе половина населения была бы невидима
//
// Помощник порождает `.to.eql(` (равенство) и `.to.have.string(` (вхождение:
// сообщение доезжает с хвостом, который статически не вычисляется). Форма, о
// которой распознаватель не знает, — не находка и не её отсутствие, это
// невидимость (`testing.md` §«Гейт на класс», п. 7). Обе доказаны инъекцией, и
// перепись печатает их порознь: ноль у одной означает ослепший распознаватель.
//
// # Совпадение считается ПЕРЕСЕЧЕНИЕМ ОБРАЗЦОВ, а не вхождением и не долей
//
// Объявленный текст несёт места подстановки окружения (`net-dup-{{runId}}`), а
// текст производителя — глаголы формата (`Network with name %s already exists`).
// Прямое вхождение поэтому не годится ни в одну сторону.
//
// ДОЛЯ ПОКРЫТИЯ ТОЖЕ НЕ ГОДИТСЯ, и это измерено, а не предположено: кейс вправе
// написать подставляемое значение ЛИТЕРАЛОМ, когда оно постоянно
// (`User usr00000000000000zzz not found` против `User %s not found`). Тогда доля
// падает до 43 % при полном совпадении по существу — то есть мерка объявляла бы
// находку на исправном кейсе.
//
// Гейт спрашивает иначе: есть ли у ВЛАДЕЛЬЦА этой коллекции производитель, чей
// образец ПЕРЕСЕКАЕТСЯ с объявленным, — то есть существует ли сообщение, которое
// оба описывают. Проверяется в обе стороны, потому что формы помощника разные:
//
//   - равенство (`.to.eql`) — образец производителя обязан покрыть объявленный
//     текст ЦЕЛИКОМ (глаголы формата поглощают подставленное значение);
//   - вхождение (`.to.have.string`) — объявленный текст обязан найтись ВНУТРИ
//     текста производителя (сообщение доезжает с хвостом, который статически не
//     вычисляется: накопитель нарушений, перечень помех).
//
// # ИСХОДОВ ТРИ, А НЕ ДВА — и третий назван числом, а не спрятан
//
// «Образец не пересёкся» ещё не доказывает, что производителя нет: текст бывает
// собран ИЗ АРГУМЕНТОВ (`fmt.Errorf("%w: %s %s not found", err, "region", id)` —
// слово «region» приезжает значением, а не шаблоном), и бывает произведён вне
// нашего дерева (`Not Found` даёт библиотека края, а не сервис). Обе формы
// статически не вычисляются НИ ПРИ КАКОМ распознавателе.
//
// Поэтому находкой объявляется только доказанное: образец не пересёкся И у
// владельца нет ничего похожего — ближайший производитель покрывает меньше
// rpNearestFloor постоянной части объявленного текста. Всё, что между, — исход
// «не установлено»: он печатается ОТДЕЛЬНЫМ числом с перечнем, вердикта не
// выносит и под маску не уходит. Смешать его с находкой значило бы завести
// проверку, у которой две трети находок ложные, — такую отключают первой.
//
// ПРОИЗВОДИТЕЛЬ БЕЗ ПОСТОЯННОЙ ЧАСТИ В СЧЁТ НЕ ИДЁТ. Шаблон вида `%w: %s`
// описывает ЛЮБОЕ сообщение и покрывал бы всё, обнуляя гейт. Поэтому
// производитель участвует, только если несёт постоянный кусок не короче
// rpMinAnchor знаков. Порог назван числом и закреплён инъекцией.
//
// # Диагностика — часть свойства
//
// Находка печатает ближайшего производителя и его долю покрытия, потому что
// починка иначе требует догадок: автор должен видеть, ЧЕМ продукт заменил текст,
// а не только что текста нет (`testing.md` §«Гейт на класс», п. 8).
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

// rpMinAnchor — какой длины постоянный кусок обязан нести шаблон производителя,
// чтобы вообще участвовать в сверке.
//
// Без порога шаблон `%w: %s` («любое сообщение») покрывал бы каждый объявленный
// текст, и гейт стал бы вакуумным. Четыре знака — самое короткое осмысленное
// слово тона этого дерева; величина закреплена инъекцией с обеих сторон.
const rpMinAnchor = 4

// rpNearestFloor — насколько близким обязан быть ближайший производитель, чтобы
// отсутствие пересечения перестало быть доказательством.
//
// Обе стороны измерены на дереве: снятый текст инцидента
// («referenced resource not found or still in use») не имеет у iam ничего ближе
// 23 % — его куски находит лишь общее « not found»; а два законных утверждения,
// чей текст собран из аргументов либо произведён библиотекой края, имеют 65 % и
// 100 %. Порог разводит их и закреплён инъекцией с обеих сторон.
const rpNearestFloor = 0.5

// rpCorpusFor — чьими текстами вправе отвечать шаг этой коллекции.
//
// КРАЙ — ОСОБЫЙ СЛУЧАЙ, И ЕГО НАДО НАЗВАТЬ. Коллекции `gateway/` гоняют ВСЕ
// домены через край: их шаг вправе получить отказ любого сервиса. Общее правило
// владельца (`rtOwner` → «*» для всего вне `services/`) дало бы им корпус из
// одного лишь фундамента, и КАЖДОЕ их утверждение о доменном тексте стало бы
// находкой. Наблюдалось на первом же прогоне: отказ, который производит
// `services/iam`, был объявлен непроизводимым в коллекции края.
func rpCorpusFor(byOwner map[string]map[string]bool, rel string) rtCorpus {
	if rtOwner(rel) != rtSharedOwner {
		return rtCorpusFor(byOwner, rtOwner(rel))
	}
	set := map[string]bool{}
	for _, texts := range byOwner {
		for t := range texts {
			set[t] = true
		}
	}
	names := make([]string, 0, len(set))
	for t := range set {
		names = append(names, t)
	}
	sort.Strings(names)
	blob := strings.Join(names, "\x00")
	return rtCorpus{blob: blob, low: strings.ToLower(blob), n: len(names)}
}

var (
	// Утверждение, собранное общим слоем: обе формы одного помощника и ОБА
	// КОНВЕРТА. Синхронный отказ приезжает полем `message` верхнего уровня,
	// отказ асинхронной мутации — вложенным `error.message` конверта `Operation`
	// (`api-conventions.md`: мутации возвращают `Operation`), и общий слой
	// выносит его в `opMsg`. Конверт, о котором распознаватель не знает, делает
	// каждое утверждение о нём не находкой и не её отсутствием, а НЕВИДИМЫМ
	// (`testing.md` §«Гейт на класс», п. 7) — и завёл бы ровно ту слепую зону,
	// ради которой заведён #1748. Перепись печатает конверты порознь.
	rpAssert = regexp.MustCompile(
		`pm\.expect\(\s*(?:j\.message|opMsg)\s*,\s*JSON\.stringify\(j\)\s*\)\.to\.(eql|have\.string)\(`)
	// Тот же разбор, но отвечает на другой вопрос: КАКОЙ конверт прочитан.
	// Нужен для переписи — ноль у одного конверта означает ослепший
	// распознаватель, а не чистое дерево.
	rpOpEnvelope = regexp.MustCompile(`pm\.expect\(\s*opMsg\s*,`)
	// Кусок объявленного текста и чтение окружения на его месте.
	// ОБЕ ФОРМЫ КАВЫЧЕК ОБЯЗАТЕЛЬНЫ. Сериализатор общего слоя (`js_str`) берёт
	// двойные кавычки, как только сам текст несёт апостроф, — а он его несёт у
	// целого вида отказов: `invalid membership id 'not-an-id'`. Распознаватель,
	// знающий одни одинарные, вычитывал бы из такого утверждения ВНУТРЕННИЙ
	// апострофированный кусок вместо всего текста и объявлял бы находку на
	// исправном кейсе. Наблюдалось на первом прогоне этого гейта; форма, о
	// которой распознаватель не знает, — не находка и не её отсутствие, это
	// невидимость (`testing.md` §«Гейт на класс», п. 7).
	rpChunk  = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"`)
	rpEnvGet = regexp.MustCompile(`pm\.environment\.get\(`)
	rpVerb   = regexp.MustCompile(`%[#+\-0-9.]*[a-zA-Z]`)
)

// rpDeclared — объявленный текст: постоянные куски автора, разделённые местом
// подстановки. Место подстановки несёт `\x01` — знак, которого нет ни в одном
// тексте, поэтому склейки соседних кусков в несуществующую фразу не бывает.
func rpDeclared(expr string) (string, bool) {
	var b strings.Builder
	rest := expr
	saw := false
	for rest != "" {
		lit := rpChunk.FindStringIndex(rest)
		env := rpEnvGet.FindStringIndex(rest)
		switch {
		case lit != nil && (env == nil || lit[0] < env[0]):
			m := rpChunk.FindStringSubmatch(rest[lit[0]:])
			q := m[1]
			if q == "" {
				q = m[2]
			}
			b.WriteString(rpUnquote(q))
			saw = true
			rest = rest[lit[1]:]
		case env != nil:
			b.WriteString("\x01")
			// Дочитываем до конца вызова, чтобы имя переменной не попало в текст.
			rest = rest[env[1]:]
			if i := strings.Index(rest, ")"); i >= 0 {
				rest = rest[i+1:]
			} else {
				rest = ""
			}
		default:
			rest = ""
		}
	}
	return b.String(), saw
}

func rpUnquote(s string) string {
	return strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`, `\n`, "\n", `\t`, "\t").Replace(s)
}

// rpConstLen — длина постоянной части объявленного текста.
func rpConstLen(decl string) int {
	return len(strings.ReplaceAll(decl, "\x01", ""))
}

// rpChunks — постоянные куски шаблона: то, что подстановка закрыть не может.
func rpChunks(tmpl string) []string {
	var out []string
	for _, c := range rpVerb.Split(tmpl, -1) {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// rpAnchored — несёт ли шаблон постоянный кусок, достаточный, чтобы о чём-то
// утверждать.
func rpAnchored(tmpl string) bool {
	for _, c := range rpChunks(tmpl) {
		if len(strings.TrimSpace(c)) >= rpMinAnchor {
			return true
		}
	}
	return false
}

// rpPattern — образец из шаблона: постоянные куски дословно, места подстановки
// свободны. `fill` — знак, которым закрываются места подстановки ПРОТИВОПОЛОЖНОЙ
// стороны, чтобы образец мог их поглотить.
func rpPattern(tmpl, wildcard string) *regexp.Regexp {
	var b strings.Builder
	for i, c := range rpVerb.Split(tmpl, -1) {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(strings.ReplaceAll(c, wildcard, "\x02")))
	}
	// Место подстановки окружения объявленного текста — тоже свободное место.
	return regexp.MustCompile("(?s)" + strings.ReplaceAll(b.String(), regexp.QuoteMeta("\x02"), ".*"))
}

// rpIntersects — описывают ли объявленный текст и шаблон производителя хотя бы
// одно общее сообщение.
//
// `contains` разводит две формы помощника: при равенстве образец производителя
// обязан покрыть объявленное ЦЕЛИКОМ, при вхождении объявленное обязано найтись
// внутри текста производителя.
func rpIntersects(decl, producer string, contains bool) bool {
	if !rpAnchored(producer) {
		return false
	}
	if contains {
		// Объявленное — окно в сообщении владельца: ищем его образец внутри
		// шаблона производителя, чьи глаголы закрыты свободным знаком.
		declPat := rpPattern(strings.ReplaceAll(decl, "\x01", "%s"), "")
		return declPat.MatchString(rpVerb.ReplaceAllString(producer, "\x02"))
	}
	prodPat := regexp.MustCompile("^(?s)" + strings.TrimPrefix(
		rpPattern(producer, "\x01").String(), "(?s)") + "$")
	return prodPat.MatchString(strings.ReplaceAll(decl, "\x01", "\x02"))
}

// rpNearest — ближайший производитель для ДИАГНОСТИКИ: наибольшая доля
// постоянной части объявленного текста, уложенная его кусками по порядку.
// Вердикта не выносит — находка, называющая симптом вместо причины, посылает
// читателя искать не там (`testing.md` §«Гейт на класс», п. 8).
func rpNearest(decl string, corpus rtCorpus) (string, float64) {
	total := rpConstLen(decl)
	if total == 0 {
		return "", 0
	}
	low := strings.ToLower(decl)
	best, bestCover := "", 0.0
	for _, p := range strings.Split(corpus.blob, "\x00") {
		pos, covered := 0, 0
		for _, c := range rpChunks(strings.ToLower(p)) {
			i := strings.Index(low[pos:], c)
			if i < 0 {
				continue
			}
			covered += len(c)
			pos += i + len(c)
		}
		if covered > total {
			covered = total
		}
		if f := float64(covered) / float64(total); f > bestCover {
			best, bestCover = p, f
		}
	}
	return best, bestCover
}

type rpFinding struct {
	collection, step, declared, nearest string
	nearestCover                        float64
}

func (f rpFinding) String() string {
	near := "ни один производитель владельца не совпадает с ним даже частично"
	if f.nearest != "" {
		near = fmt.Sprintf("ближе всего производитель %q (общего — %.0f%% постоянной части)",
			f.nearest, f.nearestCover*100)
	}
	return fmt.Sprintf("%s :: %s\n      объявлен текст владельца %q — производителя нет; %s",
		f.collection, f.step, f.declared, near)
}

type rpCensus struct {
	collections, steps int
	// unproven — утверждения, чей образец не пересёкся, но у владельца есть
	// похожий текст: доказательства нет ни в одну сторону. Печатается числом и
	// перечнем, вердикта не выносит.
	unproven []rpFinding
	// eqlAsserts / containsAsserts — обе формы помощника порознь: ноль у одной
	// означает ослепший распознаватель, а не чистое дерево.
	eqlAsserts, containsAsserts int
	// syncEnvelope / opEnvelope — та же перепись по КОНВЕРТУ: `message` верхнего
	// уровня против `error.message` конверта операции. Ноль у одного означает,
	// что распознаватель этого конверта не читает.
	syncEnvelope, opEnvelope int
}

func (c rpCensus) declared() int { return c.eqlAsserts + c.containsAsserts }

// auditRefusalProducer — весь разбор одним входом, чтобы инъекция гоняла ТУ ЖЕ
// функцию, а не свою копию логики.
func auditRefusalProducer(root string, cols []string,
	corpusOf func(collection string) rtCorpus) ([]rpFinding, rpCensus, error) {
	var findings []rpFinding
	var cen rpCensus

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
		corpus := corpusOf(rel)

		var walk func(items []nmItem)
		walk = func(items []nmItem) {
			for _, it := range items {
				if it.isFolder() {
					walk(it.Item)
					continue
				}
				cen.steps++
				for _, ev := range it.Event {
					if ev.Listen != "test" {
						continue
					}
					for _, raw := range ev.Script.Exec {
						line := slpStripJSComment(raw)
						loc := rpAssert.FindStringSubmatchIndex(line)
						if loc == nil {
							continue
						}
						if line[loc[2]:loc[3]] == "eql" {
							cen.eqlAsserts++
						} else {
							cen.containsAsserts++
						}
						if rpOpEnvelope.MatchString(line) {
							cen.opEnvelope++
						} else {
							cen.syncEnvelope++
						}
						decl, saw := rpDeclared(line[loc[1]:])
						if !saw || rpConstLen(decl) == 0 {
							continue
						}
						contains := line[loc[2]:loc[3]] != "eql"
						matched := false
						for _, p := range strings.Split(corpus.blob, "\x00") {
							if rpIntersects(decl, p, contains) {
								matched = true
								break
							}
						}
						if matched {
							continue
						}
						best, cover := rpNearest(decl, corpus)
						f := rpFinding{
							collection: rel, step: it.Name,
							declared: strings.ReplaceAll(decl, "\x01", "…"),
							nearest:  best, nearestCover: cover,
						}
						if cover >= rpNearestFloor {
							cen.unproven = append(cen.unproven, f)
							continue
						}
						findings = append(findings, f)
					}
				}
			}
		}
		walk(col.Item)
	}
	return findings, cen, nil
}

func TestNewmanAssertedRefusalTextHasAProducer(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — из ИНДЕКСА git: под корнем лежат рабочие копии агентов и
	// распаковки отчётов прогонов, и вердикт по ним был бы свойством чужого
	// рабочего каталога, а не коммита.
	tt := newTrackedTree(t, root)

	byOwner, err := rtProducers(root, optGoFiles(tt))
	if err != nil {
		t.Fatal(err)
	}
	// Предпосылка первая: производители прочитаны. Пустой набор означает
	// ослепший распознаватель, и тогда КАЖДОЕ утверждение дерева — находка.
	if len(byOwner[rtSharedOwner]) == 0 {
		t.Fatal("в общем фундаменте (pkg/, gateway/) не найдено НИ ОДНОГО текста отказа — " +
			"распознаватель производителей ослеп; чинить надо гейт, а не выходить успехом")
	}

	cols := optCollections(tt)
	findings, cen, err := auditRefusalProducer(root, cols, func(rel string) rtCorpus {
		return rpCorpusFor(byOwner, rel)
	})
	if err != nil {
		t.Fatal(err)
	}

	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	// Предпосылка вторая: обе формы помощника видны. Ноль у любой означает «ноль
	// прочитанного», а не «ноль находок».
	if cen.eqlAsserts == 0 || cen.containsAsserts == 0 {
		t.Fatalf("в %d шагах утверждений общего слоя о тексте отказа: равенством %d, вхождением %d — "+
			"ноль у любой формы означает ослепший распознаватель, а не чистое дерево",
			cen.steps, cen.eqlAsserts, cen.containsAsserts)
	}
	// Предпосылка третья: читаются ОБА конверта. Отказ мутации приезжает
	// вложенным `error.message`, и распознаватель, знающий один лишь `message`
	// верхнего уровня, объявлял бы «производителя нет» у целого конверта — то
	// есть молчал бы там, где предмет и живёт (#1748).
	if cen.syncEnvelope == 0 || cen.opEnvelope == 0 {
		t.Fatalf("в %d шагах утверждений по конвертам: синхронный %d, конверт операции %d — "+
			"ноль у любого означает, что этот конверт распознаватель не читает вовсе",
			cen.steps, cen.syncEnvelope, cen.opEnvelope)
	}

	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, fmt.Sprintf("%s:%d", o, len(byOwner[o])))
	}
	sort.Strings(owners)
	var unproven strings.Builder
	for _, f := range cen.unproven {
		fmt.Fprintf(&unproven, "\n    %s", f)
	}
	t.Logf("осмотрено: коллекций %d, шагов %d; утверждений общего слоя о тексте отказа %d "+
		"(равенством %d, вхождением %d; конверт синхронный %d, операции %d); из них "+
		"доказательства нет ни в одну сторону у %d "+
		"(текст собран из аргументов либо произведён вне дерева — исход «не установлено», "+
		"а не находка).%s\nПроизводителей по владельцам: %v",
		cen.collections, cen.steps, cen.declared(), cen.eqlAsserts, cen.containsAsserts,
		cen.syncEnvelope, cen.opEnvelope, len(cen.unproven), unproven.String(), owners)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "утверждений о тексте отказа, у которого нет производителя: %d\n\n", len(findings))
		b.WriteString("Автор объявил этот текст текстом владельца (`assert_refusal_message`),\n")
		b.WriteString("а продукт его не производит — значит утверждение не может пройти НИ ПРИ\n")
		b.WriteString("КАКОМ ответе, и узналось бы это только 25-минутным прогоном стенда.\n")
		b.WriteString("Чинится в cases/*.py набора: приведи текст к тому, что продукт отдаёт\n")
		b.WriteString("сегодня; коллекции затем перегенерируются scripts/gen.py своего набора.\n")
		b.WriteString("Гейт судит СОВПАДЕНИЕ СТРОК, а не правильность тона: осознанная смена\n")
		b.WriteString("текста законна и требует лишь правки кейса тем же изменением.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}
