// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmancodebrancharm_test.go — гейт против ветки по коду ответа, которую допуск
// ТОГО ЖЕ шага исключает, притом что обе исполняются на одном и том же ответе.
//
// # Предмет
//
// Шаг, у которого допуск объявляет `oneOf([403, 404])`, а рядом — БЕЗУСЛОВНАЯ
// ветка `if (pm.response.code === 200)`, утверждает про один ответ две
// несовместимые вещи: при 200 допуск краснеет, а ветка отчитывается зелёным.
// По отчёту нельзя сказать, какое из двух выражает намерение автора, и чинят
// такое обычно ослаблением допуска — то есть ровно тем, что запрещено
// (`testing.md` §«E2E НИКОГДА не пропускаются»).
//
// # Почему гейт обязан читать ВЕТВИ, а не совпадение чисел
//
// Господствующая и ЗАКОННАЯ форма в этом корпусе — таблица решений:
//
//	if (pm.response.code === 200) { …страница пуста… }
//	else { pm.expect(pm.response.code).to.be.oneOf([403, 404]); }
//
// Здесь ветка и допуск живут в РАЗНЫХ ветвях одного `if`/`else` и на одном
// ответе не исполняются никогда: при 200 допуск не выполняется вовсе. У каждой
// полосы свой производитель (пустая страница фильтра прав против отказа гейта
// края), и каждая полоса что-то утверждает.
//
// Замер на `release/deforking-2` @ 3e8c90fe0: коллекций 93, шагов 9224, веток по
// коду ответа 574, допусков 718. Пар «ветка вне допуска» — **254**, и все они
// раскладываются на законные: противоположная ветвь `if`/`else` — **203**,
// полоса повтора — **51**. Настоящих противоречий — **НОЛЬ**. Гейт, считающий
// совпадение чисел без разбора ветвей, дал бы 254 находки из 254 ложных;
// инструмент с такой долей перестают читать, и вместе с ним перестают читать
// настоящую находку.
//
// # Ноль — это ЗАМЕР, а не предположение, и он опроверг постановку задачи
//
// Задача #1459 называла **7** противоречий из 90 кандидатов и перечисляла их
// поимённо. Все семь — ровно те `if`/`else`, что перечислены выше: предикат
// задачи сверял коды из ветки с составом допуска ТОГО ЖЕ шага, но структуру
// ветвей не разбирал, а при `if (200) {…} else {oneOf([403,404])}` допуск на
// ответе 200 НЕ ИСПОЛНЯЕТСЯ ВОВСЕ. Утверждение «при 200 допуск краснеет, а
// ветка отчитывается зелёным» неверно для каждого из семи.
//
// Гейт поэтому заведён не под находку, а под КЛАСС: предмет реален (шаг,
// утверждающий два несовместимых исхода на одном ответе, чинят ослаблением
// допуска — тем самым, что запрещено), экземпляров сегодня ноль, и ноль этот
// обязан остаться проверяемым. Предпосылка `exclusiveArms == 0` стережёт
// обратную сторону: ослепни разбор ветвей — законная таблица решений поехала
// бы в находки, и гейт краснел бы на верном корпусе.
//
// # Что НЕ судится, и это названо, а не умолчано
//
//   - ветвь, несущая переход (`setNextRequest`), — законная полоса повтора:
//     ответ, на который она смотрит, до утверждений не доходит;
//   - шаг без допуска по коду ответа — сравнивать не с чем; такие шаги
//     считаются отдельной величиной переписи, чтобы «ноль находок» не
//     покрывало того, чего гейт не читал;
//   - ветвь по ЧУЖОМУ полю (`j.code`, `j.error.code`) — не предмет: допуск
//     этого шага говорит про HTTP-код края, а не про код внутри тела.
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

var (
	// Ветвь по коду ОТВЕТА КРАЯ. Условие читается целиком, чтобы ветвь по чужому
	// полю (`j.code === 9`) сюда не попадала.
	rcbBranch = regexp.MustCompile(`\bif\s*\(([^{]*?)\)\s*\{`)
	rcbCode   = regexp.MustCompile(`pm\.response\.code\s*===\s*(\d{3})`)
	rcbAdmOp  = regexp.MustCompile(`\.to\.(?:be\.)?(?:oneOf|eql|equal)\(`)
	rcbOneOf  = regexp.MustCompile(`\.to\.(?:be\.)?oneOf\(\s*\[([^\]]*)\]`)
	rcbEql    = regexp.MustCompile(`\.to\.(?:eql|equal)\(\s*(\d{3})\s*\)`)
	rcbInt    = regexp.MustCompile(`\d{3}`)
	rcbJump   = regexp.MustCompile(`setNextRequest`)
)

type rcbFinding struct {
	collection, title, step string
	branchCode              int
	allowed                 []int
}

func (f rcbFinding) String() string {
	return fmt.Sprintf("%s :: %s / %s — ветка `if (pm.response.code === %d)` исполняется на том же "+
		"ответе, что и допуск %v этого шага, и допуск кода %d НЕ содержит: при %d допуск краснеет, "+
		"а ветка отчитывается зелёным. Исход один из трёх: %d входит в допуск (и тогда ветка — его "+
		"пин) · ветка снята · ветвь оформлена переходом",
		f.collection, f.title, f.step, f.branchCode, f.allowed, f.branchCode, f.branchCode, f.branchCode)
}

type rcbCensus struct {
	collections, steps int
	branches           int
	admissions         int
	// exclusiveArms — пар «ветка вне допуска», где допуск лежит в
	// ПРОТИВОПОЛОЖНОЙ ветви того же `if`/`else`: на одном ответе не исполняются
	// никогда, это таблица решений, а не противоречие.
	exclusiveArms int
	// retryLane — пар, где шаг несёт переход: законная полоса повтора.
	retryLane int
	// stepsWithoutAdmission — шаги с веткой, но без допуска по коду ответа:
	// сравнивать не с чем.
	stepsWithoutAdmission int
}

// rcbSpan — полуинтервал в тексте скрипта.
type rcbSpan struct{ from, to int }

func (s rcbSpan) holds(pos int) bool { return pos >= s.from && pos <= s.to }

// rcbMatchBrace — конец блока, начинающегося фигурной скобкой в позиции open.
// Возвращает -1, если блок не закрыт: разбор такого шага прекращается, а не
// достраивается догадкой.
func rcbMatchBrace(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

type rcbArm struct {
	code       int
	then, els  rcbSpan
	hasElse    bool
	conditionA string
}

// rcbArms — ветви по коду ответа со своими then/else.
func rcbArms(src string) []rcbArm {
	var out []rcbArm
	for _, loc := range rcbBranch.FindAllStringSubmatchIndex(src, -1) {
		cond := src[loc[2]:loc[3]]
		m := rcbCode.FindStringSubmatch(cond)
		if m == nil {
			continue
		}
		code := 0
		if _, err := fmt.Sscanf(m[1], "%d", &code); err != nil {
			continue
		}
		open := loc[1] - 1
		end := rcbMatchBrace(src, open)
		if end < 0 {
			continue
		}
		arm := rcbArm{code: code, then: rcbSpan{open, end}, conditionA: cond}
		// `else {` сразу за закрывающей скобкой — с поправкой на пробелы и перевод строки.
		rest := src[end+1:]
		if em := regexp.MustCompile(`^\s*else\s*\{`).FindStringIndex(rest); em != nil {
			eopen := end + 1 + em[1] - 1
			eend := rcbMatchBrace(src, eopen)
			if eend > 0 {
				arm.els = rcbSpan{eopen, eend}
				arm.hasElse = true
			}
		}
		out = append(out, arm)
	}
	return out
}

type rcbAdmission struct {
	pos   int
	codes map[int]bool
}

// rcbAdmissions — допуски ПО КОДУ ОТВЕТА. Разбор идёт по операторам (`;`), а не
// по строкам: `pm.expect(pm.response.code)` и `.to.be.oneOf([…])` в этом корпусе
// сплошь и рядом стоят на РАЗНЫХ строках, и построчный распознаватель не увидел
// бы ровно ту форму, которой записано большинство допусков.
func rcbAdmissions(src string) []rcbAdmission {
	var out []rcbAdmission
	offset := 0
	for _, stmt := range strings.Split(src, ";") {
		start := offset
		offset += len(stmt) + 1
		if !strings.Contains(stmt, "pm.response.code") || !rcbAdmOp.MatchString(stmt) {
			continue
		}
		codes := map[int]bool{}
		if m := rcbOneOf.FindStringSubmatch(stmt); m != nil {
			for _, n := range rcbInt.FindAllString(m[1], -1) {
				v := 0
				if _, err := fmt.Sscanf(n, "%d", &v); err == nil {
					codes[v] = true
				}
			}
		}
		if m := rcbEql.FindStringSubmatch(stmt); m != nil {
			v := 0
			if _, err := fmt.Sscanf(m[1], "%d", &v); err == nil {
				codes[v] = true
			}
		}
		if len(codes) == 0 {
			continue
		}
		pos := start + rcbAdmOp.FindStringIndex(stmt)[0]
		out = append(out, rcbAdmission{pos: pos, codes: codes})
	}
	return out
}

// auditCodeBranchArm — весь разбор одним входом, чтобы инъекция гоняла ТУ ЖЕ
// функцию, а не свою копию логики.
func auditCodeBranchArm(root string, cols []string) ([]rcbFinding, rcbCensus, error) {
	var findings []rcbFinding
	var cen rcbCensus

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

		var walk func(items []nmItem, title string)
		walk = func(items []nmItem, title string) {
			for _, it := range items {
				if it.isFolder() {
					next := title
					if next == "" {
						next = it.Name
					}
					walk(it.Item, next)
					continue
				}
				cen.steps++
				findings = append(findings, rcbAuditStep(rel, title, it, &cen)...)
			}
		}
		walk(col.Item, "")
	}
	return findings, cen, nil
}

func rcbAuditStep(rel, title string, it nmItem, cen *rcbCensus) []rcbFinding {
	var lines []string
	for _, ev := range it.Event {
		if ev.Listen == "test" {
			for _, l := range ev.Script.Exec {
				lines = append(lines, slpStripJSComment(l))
			}
		}
	}
	src := strings.Join(lines, "\n")

	arms := rcbArms(src)
	if len(arms) == 0 {
		return nil
	}
	cen.branches += len(arms)

	adms := rcbAdmissions(src)
	if len(adms) == 0 {
		cen.stepsWithoutAdmission++
		return nil
	}
	cen.admissions += len(adms)
	jump := rcbJump.MatchString(src)

	var out []rcbFinding
	for _, arm := range arms {
		for _, a := range adms {
			if a.codes[arm.code] {
				continue
			}
			if arm.hasElse && arm.els.holds(a.pos) {
				cen.exclusiveArms++
				continue
			}
			if jump {
				cen.retryLane++
				continue
			}
			allowed := make([]int, 0, len(a.codes))
			for c := range a.codes {
				allowed = append(allowed, c)
			}
			sort.Ints(allowed)
			out = append(out, rcbFinding{
				collection: rel, title: title, step: it.Name,
				branchCode: arm.code, allowed: allowed,
			})
		}
	}
	return out
}

func TestNewmanCodeBranchLivesInsideItsStepAdmission(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	cols := optCollections(tt)
	findings, cen, err := auditCodeBranchArm(root, cols)
	if err != nil {
		t.Fatal(err)
	}

	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	// Предпосылки: оба распознавателя живы. Ноль здесь означает «ноль
	// прочитанного», а не «ноль находок».
	if cen.branches == 0 {
		t.Fatalf("в %d шагах не найдено НИ ОДНОЙ ветки по коду ответа — распознаватель ветвей ослеп", cen.steps)
	}
	if cen.admissions == 0 {
		t.Fatalf("при %d ветках не найдено НИ ОДНОГО допуска по коду ответа — распознаватель допуска ослеп; "+
			"в этом корпусе допуск сплошь записан оператором в несколько строк", cen.branches)
	}
	if cen.exclusiveArms == 0 {
		t.Fatalf("ни одной пары «ветка вне допуска» в противоположной ветви if/else при %d ветках — "+
			"разбор ветвей ослеп, и тогда законная таблица решений попала бы в находки", cen.branches)
	}

	t.Logf("осмотрено: коллекций %d, шагов %d; веток по коду ответа %d, допусков по коду ответа %d; "+
		"пар «ветка вне допуска» разложено так: противоположная ветвь if/else %d (таблица решений — "+
		"на одном ответе не исполняются никогда), полоса повтора %d (шаг несёт переход); шагов с "+
		"веткой, но БЕЗ допуска %d (сравнивать не с чем — не проверяются)",
		cen.collections, cen.steps, cen.branches, cen.admissions,
		cen.exclusiveArms, cen.retryLane, cen.stepsWithoutAdmission)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "веток по коду ответа вне допуска ТОГО ЖЕ шага, исполняемых на том же ответе: %d\n\n", len(findings))
		b.WriteString("Ослаблением допуска это НЕ чинится: шаг обязан утверждать один исход,\n")
		b.WriteString("а не два несовместимых. Чинится в cases/*.py набора; коллекции затем\n")
		b.WriteString("перегенерируются scripts/gen.py своего набора.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}
