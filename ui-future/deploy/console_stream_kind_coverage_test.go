// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_stream_kind_coverage_test.go — ВИД, ОБЪЯВЛЕННЫЙ ВЛАДЕЛЬЦЕМ, ОБЯЗАН
// БЫТЬ ЛИБО НАЗВАН КОНСОЛЬЮ, ЛИБО ОБЪЯВЛЕН ЕЮ НЕПОКАЗАННЫМ — С ПРИЧИНОЙ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#1558)
//
// Согласие консоли со словарями владельцев держал соседний гейт
// (`console_stream_kind_dictionary_test.go`, #1546), и он судил ОДНО
// направление: вид, названный консолью, объявлен хоть одним журналом дерева.
// Обратное — вид, объявленный владельцем и не названный консолью — он печатал
// переписью и НЕ СУДИЛ. Стороны сходились (16 и 16, поровну по пяти
// владельцам), и держателя у этого совпадения не было: заведи владелец
// семнадцатый вид — числа разошлись бы, и не покраснело бы ничто.
//
// Цена та же, что у #1021 уровнем выше: список остаётся на ОПРОСЕ при живом
// журнале и оплаченной посадке. Отличить от исправной работы нечем — опрос
// возвращается по построению, ошибки нет ни в одном журнале.
//
// ─────────────────────────────────────────────────────────────────────────────
// СУДИТСЯ ОСОЗНАННОСТЬ, А НЕ ПОКРЫТИЕ — И ЭТО РЕШЕНИЕ, А НЕ СМЯГЧЕНИЕ
//
// Консоль НЕ обязана называть каждый вид, и на то три довода, каждый замерен
// (`docs/architecture/subscription-console-kind-coverage.md` — там же цена
// обеих сторон, здесь она не пересказывается):
//
//  1. словарь видов — контракт АРЕНДАТОРУ (публичная ручка, поле `knownKinds`
//     кадра открытия), а не перечень задач консоли;
//  2. требование покрытия завело бы ребро «владелец → консоль»: вид нельзя было
//     бы объявить, не тронув браузер;
//  3. законный непокрытый вид не просто мыслим — под строгим покрытием он
//     НЕИСПОЛНИМ: карта предметов ключуется идентификатором СПЕКИ, а у ресурса,
//     живущего вкладкой карточки, спеки нет вовсе. Гейт, требующий невозможного,
//     краснеет на верном дереве и снимается первым.
//
// Поэтому судится другое: вид, который не назван И не объявлен непоказанным, —
// находка. Решённое молчание законно, молчаливое — нет.
//
// МАШИНА ЭТОТ ВОПРОС ЗА ЧЕЛОВЕКА НЕ РЕШИТ, и это проверено, а не принято на
// веру: вывести спеку из вида нельзя ни в какую сторону. `compute_instance` →
// `compute-instances` держит префикс, `storage_volume` → `volumes` его роняет,
// `nlb_network_load_balancer` → `load-balancers` роняет ещё и среднее слово.
// Значит «есть ли у этого вида страница» спрашивают у человека, а гейт следит
// лишь за тем, чтобы ответ был записан.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВЕДОМОСТЬ ПУСТА, И ГЕЙТ НА НЕЙ ПРОХОДИТ
//
// Пустая ведомость — ЦЕЛЬ, а не поломка: отказ на ней толкал бы держать запись
// ради зелёного. Слепой зоны вперёд она не заводит — пока в ней ничего нет, она
// ничего и не прощает, — и истекает сама в ОБЕ стороны: запись, чей вид карта
// всё же назвала, и запись про вид, которого не объявляет ни один журнал, суть
// находки. Причина у записи обязательна: запись без причины прощает молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА — ЧЕГО ЭТОТ ГЕЙТ НЕ ВИДИТ
//
// Он судит СЛОВАРЬ: объявление `Mapping.Kinds` в коде владельца, то самое,
// которое `Journal.KindDictionary` отдаёт полем `knownKinds`. Множество слов,
// которые производители ФАКТИЧЕСКИ пишут в журнал — в том числе триггеры базы, —
// он не читает: вид, эмитируемый триггером и в `Kinds` не объявленный, ему
// невидим. Держит ли это согласие что-нибудь ещё — здесь не утверждается.
//
// Он также не решает, КОМУ принадлежит непокрытый вид, если один и тот же вид
// объявят два журнала: пары «владелец консоли ↔ журнал» держит соседний гейт
// свойствами 2–4, и второе место об одном предмете разошлось бы с ним молча.

package deploy_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// unshownKindsDecl — объявление ведомости непоказанных видов. Ищется по имени
// объявления, а не по позиции: перенос объявления по файлу предметом не является.
var unshownKindsDecl = regexp.MustCompile(`export\s+const\s+UNSHOWN_KINDS\b`)

// unshownKindEntry — запись ведомости целиком. Причина принимается и обычной
// строкой, и шаблонной: длинное объяснение переносят по строкам, а обычный
// литерал TypeScript этого не умеет.
var unshownKindEntry = regexp.MustCompile(
	"\\{\\s*kind\\s*:\\s*\"([^\"]*)\"\\s*,\\s*why\\s*:\\s*(?:\"([^\"]*)\"|`([^`]*)`)\\s*,?\\s*\\}")

// unshownKindField — ЛЮБОЕ объявление вида внутри ведомости, без требований к
// соседнему полю.
//
// Служит счётчиком, а не разбором: число его совпадений сверяется с числом
// разобранных записей, и расхождение — ОТКАЗ. Без такой сверки запись,
// написанная формой, которой [unshownKindEntry] не знает (обратный порядок
// полей, склейка причины из кусков), выпала бы из осмотренного молча — и её вид
// снова стал бы непокрытым НЕЗАМЕТНО, то есть вернулось бы ровно то состояние,
// ради которого гейт заведён.
var unshownKindField = regexp.MustCompile(`kind\s*:\s*"`)

// unshownKind — одна запись ведомости.
type unshownKind struct {
	Kind string
	Why  string
}

// unshownKindsBlockOf вырезает ТЕЛО ведомости из карты предметов.
//
// Вырезается именно блок, а не файл: счётчик `kind:` в файле целиком считал бы
// ещё и записи карты предметов (у них то же поле), и сверка с разбором стала бы
// заведомо ложной — то есть гейт падал бы на верном дереве.
//
// Комментарии снимаются ПЕРЕД поиском: рядом с объявлением стоит закомментированный
// ОБРАЗЕЦ записи, и разбор по сырому тексту прочёл бы его как действующую.
func unshownKindsBlockOf(src string) (string, bool) {
	code := stripSubjectComments(src)
	loc := unshownKindsDecl.FindStringIndex(code)
	if loc == nil {
		return "", false
	}
	// Скобка ищется ПОСЛЕ знака присваивания, а не сразу за именем: в объявлении
	// стоит аннотация типа `readonly UnshownKind[]`, и её скобки идут ПЕРВЫМИ.
	// Разбор, берущий первую скобку, вырезал бы пустое `[]` типа — и на пустой
	// ведомости был бы ЗЕЛЁН ПО НЕВЕРНОЙ ПРИЧИНЕ, а первая же настоящая запись
	// осталась бы невидимой. Поймано инъекцией, а не вычитано.
	assign := -1
	for i := loc[1]; i < len(code); i++ {
		if code[i] == '=' && i+1 < len(code) && code[i+1] != '=' && code[i+1] != '>' {
			assign = i
			break
		}
	}
	if assign < 0 {
		return "", false
	}
	open := strings.IndexByte(code[assign:], '[')
	if open < 0 {
		return "", false
	}
	start := assign + open
	depth := 0
	for i := start; i < len(code); i++ {
		switch code[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return code[start : i+1], true
			}
		}
	}
	return "", false
}

// unshownKindsOf разбирает ведомость и возвращает записи вместе с числом
// ОБЪЯВЛЕННЫХ видов — для сверки разбора с объёмом.
func unshownKindsOf(src string) (entries []unshownKind, declared int, found bool) {
	block, ok := unshownKindsBlockOf(src)
	if !ok {
		return nil, 0, false
	}
	for _, m := range unshownKindEntry.FindAllStringSubmatch(block, -1) {
		why := m[2]
		if why == "" {
			why = m[3]
		}
		entries = append(entries, unshownKind{Kind: m[1], Why: strings.TrimSpace(why)})
	}
	return entries, len(unshownKindField.FindAllString(block, -1)), true
}

// consoleKindCoverageVerdict — находки по каждому судимому свойству.
type consoleKindCoverageVerdict struct {
	// Unjudged — вид объявлен владельцем, картой не назван, ведомостью не объявлен.
	Unjudged []string
	// StaleUnshown — запись ведомости, чей вид карта всё же называет.
	StaleUnshown []string
	// PhantomUnshown — запись ведомости про вид, которого не объявляет ни один журнал.
	PhantomUnshown []string
	// ReasonMissing — запись без причины: прощает молча.
	ReasonMissing []string
	// NamedKinds — сколько РАЗЛИЧНЫХ видов называет карта предметов.
	NamedKinds int
	// CoveredByMap / CoveredByLedger — чем закрыт каждый объявленный вид.
	CoveredByMap    int
	CoveredByLedger int
}

func (v consoleKindCoverageVerdict) empty() bool {
	return len(v.Unjudged) == 0 && len(v.StaleUnshown) == 0 &&
		len(v.PhantomUnshown) == 0 && len(v.ReasonMissing) == 0
}

// judgeConsoleKindCoverage сверяет словари владельцев с картой предметов и ведомостью.
func judgeConsoleKindCoverage(
	dict map[string][]string,
	subjects []consoleStreamSubject,
	ledger []unshownKind,
) consoleKindCoverageVerdict {
	named := map[string]bool{}
	for _, s := range subjects {
		named[s.Kind] = true
	}
	unshown := map[string]string{}
	for _, e := range ledger {
		unshown[e.Kind] = e.Why
	}
	declaredAnywhere := map[string]bool{}
	for _, kinds := range dict {
		for _, k := range kinds {
			declaredAnywhere[k] = true
		}
	}

	var verdict consoleKindCoverageVerdict
	verdict.NamedKinds = len(named)

	for _, journal := range sortedDictKeys(dict) {
		for _, kind := range dict[journal] {
			switch {
			case named[kind]:
				verdict.CoveredByMap++
			case unshown[kind] != "" || hasLedgerEntry(ledger, kind):
				verdict.CoveredByLedger++
			default:
				verdict.Unjudged = append(verdict.Unjudged, fmt.Sprintf(
					"журнал %s объявляет вид %q, которого карта предметов НЕ НАЗЫВАЕТ и "+
						"ведомость непоказанных НЕ ОБЪЯВЛЯЕТ. Список этого ресурса остаётся "+
						"на опросе при живом журнале: поток по нему не открывается вовсе, "+
						"ошибки нет ни в одном журнале, страница работает — отличить от "+
						"исправной работы нечем",
					journal, kind))
			}
		}
	}

	for _, e := range ledger {
		switch {
		case named[e.Kind]:
			verdict.StaleUnshown = append(verdict.StaleUnshown, fmt.Sprintf(
				"ведомость объявляет вид %q непоказанным, а карта предметов его НАЗЫВАЕТ — "+
					"запись потеряла предмет и теперь прощает то, чего нет. Снимите запись",
				e.Kind))
		case !declaredAnywhere[e.Kind]:
			verdict.PhantomUnshown = append(verdict.PhantomUnshown, fmt.Sprintf(
				"ведомость объявляет непоказанным вид %q, которого НЕ ОБЪЯВЛЯЕТ ни один "+
					"журнал дерева — прощать нечего. Либо написание вида устарело вместе со "+
					"словарём владельца, либо запись пережила снятый вид: снимите её",
				e.Kind))
		}
		if e.Why == "" {
			verdict.ReasonMissing = append(verdict.ReasonMissing, fmt.Sprintf(
				"запись ведомости про вид %q не называет причины. Запись без причины "+
					"прощает МОЛЧА — то есть возвращает ровно то состояние, ради которого "+
					"ведомость заведена",
				e.Kind))
		}
	}

	sort.Strings(verdict.Unjudged)
	sort.Strings(verdict.StaleUnshown)
	sort.Strings(verdict.PhantomUnshown)
	sort.Strings(verdict.ReasonMissing)
	return verdict
}

// hasLedgerEntry — назван ли вид ведомостью вообще, пусть и без причины.
//
// Отделено от причины намеренно: запись без причины обязана дать ОДНУ находку
// («причины нет»), а не две («причины нет» и «вид не судим») — вторая посылала бы
// читателя чинить не то.
func hasLedgerEntry(ledger []unshownKind, kind string) bool {
	for _, e := range ledger {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// TestEveryKindTheOwnerDeclaresIsNamedOrDeclaredUnshown — обратное направление
// согласия консоли со словарями владельцев.
func TestEveryKindTheOwnerDeclaresIsNamedOrDeclaredUnshown(t *testing.T) {
	root := repoRootFromTest(t)

	raw, err := os.ReadFile(filepath.Join(root, consoleSubjectsRel)) // #nosec G304 -- корень этого дерева
	if err != nil {
		t.Fatalf("карта предметов консоли %s не читается (%v) — сверять нечего", consoleSubjectsRel, err)
	}
	subjects := consoleStreamSubjectsOf(string(raw))
	ledger, declaredLedgerFields, ledgerFound := unshownKindsOf(string(raw))
	dict := journalDictionaries(t, root)

	declared := 0
	perOwner := make([]string, 0, len(dict))
	for _, journal := range sortedDictKeys(dict) {
		declared += len(dict[journal])
		perOwner = append(perOwner, fmt.Sprintf("%s %d", path.Base(path.Dir(path.Dir(journal))), len(dict[journal])))
	}

	verdict := judgeConsoleKindCoverage(dict, subjects, ledger)

	t.Logf("осмотрено: владельцев журнала %d, видов объявлено %d (%s); карта предметов "+
		"(%s) называет различных видов %d; ведомость непоказанных: записей %d",
		len(dict), declared, strings.Join(perOwner, " · "),
		consoleSubjectsRel, verdict.NamedKinds, len(ledger))
	t.Logf("объявленные виды закрыты: картой %d, ведомостью %d, НЕ СУДИМО %d",
		verdict.CoveredByMap, verdict.CoveredByLedger, len(verdict.Unjudged))

	// Премисы, а не вежливость: ноль прочитанного с любой стороны делает молчание
	// гейта неотличимым от «нарушений нет».
	if len(dict) == 0 {
		t.Fatalf("владельцев журнала найдено 0 по образцу %s — вердикт беспредметен",
			filepath.ToSlash(journalKindsGlob))
	}
	if declared == 0 {
		t.Fatal("владельцы не объявляют ни одного вида — покрывать нечего, и молчание " +
			"проверки не является утверждением о согласии")
	}
	if len(subjects) == 0 {
		t.Fatalf("%s не называет ни одной пары владелец+вид — прочитано ноль", consoleSubjectsRel)
	}
	if !ledgerFound {
		t.Fatalf("%s не объявляет UNSHOWN_KINDS — ведомость непоказанных видов не найдена. "+
			"Без неё законный исход («вида не показываем, вот почему») выразить НЕЧЕМ, и "+
			"гейт требовал бы покрытия, невыполнимого для вида без спеки", consoleSubjectsRel)
	}
	if declaredLedgerFields != len(ledger) {
		t.Fatalf("ведомость %s объявляет вид %d раз, а разобрано записей %d — %d записей "+
			"написаны формой, которой разбор не знает, и вид в них НИКТО не судит. Почини "+
			"распознаватель, а не ведомость: невидимая запись — не редкость, а слепая зона, "+
			"и «ноль находок» в ней означает «ноль прочитанного»",
			consoleSubjectsRel, declaredLedgerFields, len(ledger),
			declaredLedgerFields-len(ledger))
	}

	for _, text := range verdict.Unjudged {
		t.Errorf("%s: %s.\nИсходов ДВА, и оба законны: назвать вид в STREAM_SUBJECTS "+
			"(страница на общем хуке списка оживает от ОДНОЙ строки) либо объявить его в "+
			"UNSHOWN_KINDS с причиной — если страницы списка у ресурса нет вовсе. Чего "+
			"нельзя — оставить как есть: решение принимает человек, машина вывести спеку "+
			"из вида не может (см. шапку)", consoleSubjectsRel, text)
	}
	for _, text := range verdict.StaleUnshown {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
	for _, text := range verdict.PhantomUnshown {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
	for _, text := range verdict.ReasonMissing {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
}
