// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rosterreaderexitcode_test.go — гейт «код отказа читателя перечня не теряется
// формой потребления», и его судящая функция.
//
// # Предмет
//
// В дереве есть читатели ПЕРЕЧНЯ, отказывающие ГРОМКО: состав стендов
// (`deploy/tests/helm/stacks.sh`), части продукта (`deploy/scripts/lib/product-names.sh`),
// коллекции набора (`tests/newman/kacholib/stems.sh`). Отказ у каждого — код
// плюс текст причины.
//
// Код отказа доезжает до вызывающего ТОЛЬКО если тот его потребовал. Есть формы
// потребления, в которых он теряется БЕЗУСЛОВНО — не «иногда», не «при
// определённых опциях оболочки», а всегда, потому что так устроен язык:
//
//	for x in $(reader); do …          # код списка `for` не наблюдаем НИКОГДА
//	while read …; done < <(reader)    # подстановка процесса кода не отдаёт
//	while read …; done <<<"$(reader)" # here-string тоже
//	helm … $(reader) …                # код аргумента не читается, читается код helm
//
// В этих формах отказ читателя превращается в ПУСТОЙ либо НЕПОЛНЫЙ перечень при
// нулевом коде. Следствий ДВА, и они разные — оба замерены, ни одно не выведено:
//
//   - НЕПОЛНЫЙ перечень: обход идёт по остатку, страж пустоты его пропускает, и
//     перепись печатает часть как целое. Замер: набор без каталога `collections/`
//     давал `newman_all_stems` КОД 0 и ДВА стема вместо восемнадцати;
//   - ПУСТАЯ величина в аргументе: отказ приписывается ТОМУ, кто её получил.
//     Замер инъекцией одного факта (таблица стендов нечитаема): два гейта из пяти
//     объявляли «helm template отказал … — это УСЛОВИЕ прогона», то есть винили
//     несобранные зависимости вместо непрочитанной таблицы.
//
// Зелёного на пустой цепочке `-f` сегодня не выходит, и это сказано прямо: рендер
// умбреллы без единого `-f` ОТКАЗЫВАЕТ (страж подчарта реестра требует учётных
// данных). Держит это свойство ЧАРТА, а не форма вызова.
//
// # Почему именно эти формы, а не всякая подстановка
//
// Форма `X="$(reader)"` кода тоже не читает, но её отказ ловится `set -e`,
// стражем пустоты у вызывающего либо сверкой числа утверждений. Гейт, судящий и
// её, обязан был бы разбирать область действия `set -e` — и давал бы находки на
// исправном коде. Здесь названы ровно те формы, где защиты НЕТ BY CONSTRUCTION:
// опровергнуть находку нечем, поэтому прощённых записей у гейта ноль.
//
// Граница названа прямо: присваивание подстановкой гейт НЕ судит. Это не
// послабление — это отсутствие предиката, который различал бы защищённое от
// незащищённого без ложных находок.
//
// # Замер, из которого гейт вырос (2026-09-12)
//
// `deploy/tests/helm/stacks.sh` — единственный читатель состава стендов —
// содержал три экземпляра класса сразу, и замер на копии файла без таблицы дал:
// `--table` КОД 2, а `--names` и `--args` КОД 0. `--args` печатал ПУСТО и
// добавлял ВТОРУЮ, ложную причину («стека 's1' в таблице нет — имя переехало»),
// тогда как таблицы не было вовсе.
//
// Цена у `--args`: его вывод идёт в `helm` цепочкой `-f`, и пустая цепочка при
// нулевом коде означает `helm` БЕЗ ЕДИНОГО профиля — отказ, приписанный рендеру
// вместо таблицы (замер выше). Двадцать вызывающих читают этот вывод, и ни один
// не мог отличить «цепочка такая» от «таблица не прочиталась».
//
// Второй экземпляр — `newman_all_stems`: `{ ожидаемые; присутствующие; } | sort -u`.
// Труба доносит код последнего звена, поэтому отказ ОБОИХ слагаемых терялся
// всегда. Замер: набор без каталога `collections/` давал КОД 0 и ДВА стема —
// перечень неполный, а не пустой, поэтому страж «коллекций 0» его пропускал.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// rosterReaderNames — читатели перечня, отказывающие громко. Имя, а не путь:
// функция зовётся и из соседнего файла, и из скрипта, и из рецепта make.
//
// Перечень выведен из дерева по свойству «печатает перечень И умеет вернуть
// ненулевой код», а не выписан по памяти: транзитивное замыкание включает
// производные (`stacks_names` зовёт `stacks_table`, `newman_all_stems` — оба
// слагаемых), потому что отказ основания доезжает до вызывающего через них.
var rosterReaderNames = []string{
	// состав стендов
	"stacks_table", "stacks_names", "stacks_chain", "stacks_args",
	// части продукта
	"product_service_dirs",
	// коллекции набора
	"newman_all_stems", "newman_expected_stems", "newman_present_stems",
}

// rosterReaderRe — чтение читателя перечня: либо имя функции оболочки, либо
// вызов скрипта состава стендов как исполняемого файла.
var rosterReaderRe = regexp.MustCompile(
	`\b(?:` + strings.Join(rosterReaderNames, "|") + `)\b` +
		`|stacks\.sh"?\s+--(?:names|table|chain|args)\b`)

// lossyForms — формы, в которых код подстановки теряется БЕЗУСЛОВНО.
//
// Каждая проверяется на строке, из которой уже вырезан комментарий оболочки:
// все три формы встречаются В ОБЪЯСНЕНИЯХ этого самого класса (в шапке
// `stacks.sh`, в шапке `stems.sh`, в этом файле), и проверка по подстроке
// краснела бы на собственном обосновании.
var lossyForms = []struct {
	Name string
	Re   *regexp.Regexp
}{
	{"список for", regexp.MustCompile(`\bfor\s+[A-Za-z_][A-Za-z0-9_]*\s+in\s+[^;]*\$\(`)},
	{"подстановка процесса", regexp.MustCompile(`<\s*\(`)},
	{"here-string с подстановкой", regexp.MustCompile(`<<<\s*"?\$\(`)},
	// Имя инструмента обязано стоять в позиции КОМАНДЫ, а не где угодно в строке.
	// Без якоря выражение ловило `helm` внутри ПУТИ (`tests/helm/stacks.sh`,
	// `./helm/umbrella`) и внутри собственного текста отказа: первый же прогон дал
	// три находки, и все три были ложными по референту — предикат не отличал имя
	// команды от имени каталога.
	//
	// `render` в перечне не случаен: это идиома ЭТОГО каталога («отрендерить
	// названный стек»), и первый прогон по настоящему тексту до починки показал,
	// что без неё форма в `networkpolicy-egress-test.sh` не находится вовсе.
	{"аргумент helm/kubectl", regexp.MustCompile(
		`(?:^|[;|&(]|\t|\bthen\b|\bdo\b|\belse\b)[ \t]*(?:helm|helm_try|render|kubectl)[ \t][^\n]*\$\(`)},
}

// RosterReaderFinding — одно место, где код отказа читателя перечня теряется.
type RosterReaderFinding struct {
	File string
	Line int
	Form string
	Text string
}

func (f RosterReaderFinding) String() string {
	return fmt.Sprintf("%s:%d — %s: код отказа читателя перечня теряется. %s",
		f.File, f.Line, f.Form, strings.TrimSpace(f.Text))
}

// RosterReaderCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного».
type RosterReaderCensus struct {
	FilesRead      int // файлов, чей текст прочитан
	LinesRead      int // строк прочитано
	FilesMentioned int // файлов, упомянувших хотя бы одного читателя перечня
	Consumptions   int // строк-потреблений (комментарии вырезаны)
	Findings       int
}

// AuditRosterReaderExitCodes — судящая функция.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию: проба, повторяющая логику
// гейта, доказывала бы свойство копии.
//
// `texts` — отображение «путь → содержимое». Пустой вход НЕ означает «находок
// нет»: вызывающий обязан отказать на пустом обходе, и проба это требует.
func AuditRosterReaderExitCodes(texts map[string]string) ([]RosterReaderFinding, RosterReaderCensus) {
	var (
		findings []RosterReaderFinding
		cen      RosterReaderCensus
	)
	paths := make([]string, 0, len(texts))
	for p := range texts {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		body := texts[path]
		cen.FilesRead++
		mentioned := false
		for i, raw := range strings.Split(body, "\n") {
			cen.LinesRead++
			// Комментарий вырезается ТЕМ ЖЕ разборщиком, что у соседнего гейта
			// кодов возврата конвейера: второе выражение об одном предмете
			// разошлось бы молча.
			line := stripShellComment(raw)
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !rosterReaderRe.MatchString(line) {
				continue
			}
			mentioned = true
			cen.Consumptions++
			for _, f := range lossyForms {
				if !f.Re.MatchString(line) {
					continue
				}
				// Форма найдена — но читатель обязан быть ВНУТРИ подстановки,
				// а не рядом с ней: `helm … $ARGS` с заранее прочитанной
				// величиной законен, и находка на нём была бы ложной.
				if !readerInsideSubstitution(line) {
					continue
				}
				findings = append(findings, RosterReaderFinding{
					File: path, Line: i + 1, Form: f.Name, Text: line,
				})
				cen.Findings++
				break
			}
		}
		if mentioned {
			cen.FilesMentioned++
		}
	}
	return findings, cen
}

// readerInsideSubstitution — стоит ли читатель перечня ВНУТРИ `$( … )`.
//
// Без этого различения гейт краснел бы на законной форме `helm … $ARGS`, где
// величина прочитана строкой выше вместе со своим кодом: имя читателя там
// встречается только в комментарии, а комментарий уже вырезан — но в строке
// `args="$(stacks_args …)" || fatal` подстановка есть, и она ЗАКОННА.
// Поэтому одного `$(` мало; нужен читатель именно в его границах.
func readerInsideSubstitution(line string) bool {
	for idx := 0; ; {
		start, open := nextSubstitutionOpen(line, idx)
		if start < 0 {
			return false
		}
		_ = open
		depth, end := 0, -1
		for i := start + 1; i < len(line); i++ {
			switch line[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		inner := line[start+2:]
		if end > start {
			inner = line[start+2 : end]
		}
		if rosterReaderRe.MatchString(inner) {
			return true
		}
		idx = start + 2
	}
}

// nextSubstitutionOpen — ближайшее открытие подстановки, начиная с idx.
//
// Форм ТРИ, и первый прогон по настоящему тексту до починки показал, зачем:
// проверка, знавшая только `$(`, не нашла `done < <(newman_all_stems …)` вовсе —
// подстановка процесса пишется `<(`, а не `$(`. Форма, о которой выражение не
// знает, даёт не красное и не зелёное, а МОЛЧАНИЕ.
func nextSubstitutionOpen(line string, idx int) (pos int, form string) {
	best, bestForm := -1, ""
	for _, f := range []string{"$(", "<(", ">("} {
		if i := strings.Index(line[idx:], f); i >= 0 {
			if best < 0 || idx+i < best {
				best, bestForm = idx+i, f
			}
		}
	}
	return best, bestForm
}

// rosterReaderTexts — текст файлов дерева, в которых читатель перечня вообще
// может встретиться.
//
// Состав берётся из ИНДЕКСА git, а не с диска: под деревом лежат распакованные
// чужие чарты, сборочные каталоги и рабочие копии полос — обход диска судил бы
// чужой текст и краснел на исправном дереве.
//
// Файлы Go и фикстуры исключены by construction и по разным причинам: в Go текст
// формы живёт СТРОКОЙ-ожиданием у соседних проб (гейт, судящий их, краснел бы на
// собственных фикстурах дерева), а `testdata/` — синтетический вход других
// гейтов, где дефект обязан присутствовать.
func rosterReaderTexts(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	texts := map[string]string{}
	for _, rel := range tree.SortedFiles() {
		switch {
		case strings.HasSuffix(rel, ".go"):
			continue
		case strings.Contains(rel, "testdata/"):
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			// Файл индекса, которого нет на диске, — «не выполнилось», а не
			// «находок нет»: перепись без него была бы уже́ней объявленного.
			t.Fatalf("%s: %v — обход неполон, вердикт был бы о меньшем дереве", rel, err)
		}
		texts[rel] = string(b)
	}
	if len(texts) == 0 {
		t.Fatal("из индекса не прочитано ни одного файла — «ноль находок» означало бы «ноль прочитанного»")
	}
	return texts
}

// TestRosterReaderExitCodeIsNotLostByTheConsumingForm — ни одно место дерева не
// потребляет читатель перечня в форме, теряющей его код БЕЗУСЛОВНО.
func TestRosterReaderExitCodeIsNotLostByTheConsumingForm(t *testing.T) {
	t.Parallel()
	if treecorpus.RunResultWillBeCached() {
		t.Fatal(treecorpus.CachedVerdictRefusal())
	}
	findings, cen := AuditRosterReaderExitCodes(rosterReaderTexts(t))

	// Пустой обход — ОТКАЗ, а не чистота, и порядок ветвей несущий: страж стоит
	// РАНЬШЕ вердикта. Читателей перечня в дереве ЕСТЬ — три библиотеки и их
	// производные, — поэтому ноль упоминаний означает сломанный предикат.
	if cen.FilesMentioned == 0 {
		t.Fatal("ни один файл дерева не упомянул читателя перечня — предикат обхода сломан")
	}
	if cen.Consumptions == 0 {
		t.Fatal("строк-потреблений ноль при непустом обходе — комментарии вырезаны слишком широко")
	}

	for _, f := range findings {
		t.Errorf("%s", f)
	}
	t.Logf("перепись: файлов прочитано %d, строк %d; упомянувших читателя перечня файлов %d; "+
		"строк-потреблений %d; находок %d",
		cen.FilesRead, cen.LinesRead, cen.FilesMentioned, cen.Consumptions, cen.Findings)
}
