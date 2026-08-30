// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package artifactgates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// spineBindingCensus — объём осмотренного. Печатается ВСЕГДА: без него «ноль
// находок» неотличимо от «ноль прочитанного». Величины названы ПОРОЗНЬ —
// сколько потребителей осмотрено, сколько из них проверки кейсов, сколько
// проб, сколько обращений к хребту и сколько из них через связывание набора, —
// потому что одно суммарное число скрыло бы ровно тот случай, ради которого
// распознаватель расширяли.
type spineBindingCensus struct {
	consumers  int
	validators int
	probes     int
	callSites  int
	bound      int
}

// Хребет генератора коллекций — функции общего слоя, чья подпись НЕСЁТ
// дескриптор набора (`Emit` либо перечень впрыскиваемого). Именно они и
// стали расти при сведении: подпись меняется в одном месте, а зовущие её
// своими руками остаются со старым числом аргументов.
//
// Перечень выведен из общего слоя, а не из вкуса: это те четыре функции
// `tests/newman/kacholib/gen_shared.py`, у которых первый параметр — решения
// набора.
var reSpineDirectCall = regexp.MustCompile(
	`\.\s*(load_cases_module|case_to_postman|step_to_postman|build_collection)\s*\(`)

// Связывание набора. Приёмник НЕ фиксируется (генератор зовут и `gen`, и
// элементом словаря генераторов), фиксируется дескриптор оркестрации и его
// метод: решения набора живут в нём, и потребитель берёт их оттуда.
var reSpineBoundCall = regexp.MustCompile(
	`_RUN\s*\.\s*(load|case_item|step_item|collection)\s*\(`)

// pythonCode — исполняемая часть модуля python: без комментариев и без
// строковых литералов.
//
// ЗАЧЕМ. Гейт обязан судить то, что код ДЕЛАЕТ, а не то, что о нём написано:
// имена хребта стоят и в шапках самих потребителей, и в объяснениях рядом с
// вызовами, — распознаватель по сырому тексту краснел бы на собственном
// объяснении. Тройные кавычки здесь несут прозу (шапка модуля, шапка пробы),
// одинарные — тексты сообщений; ни то, ни другое вызовом не является.
//
// Замена НА ПРОБЕЛ, а не на пустоту: иначе `a"""x"""(` склеилось бы в вызов,
// которого в исходнике нет.
func pythonCode(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	const (
		plain = iota
		inComment
		inString
	)
	state := plain
	var delim string
	i := 0
	for i < len(src) {
		switch state {
		case plain:
			if src[i] == '#' {
				state = inComment
				out.WriteByte(' ')
				i++
				continue
			}
			if d := quoteAt(src, i); d != "" {
				state, delim = inString, d
				for range d {
					out.WriteByte(' ')
				}
				i += len(d)
				continue
			}
			out.WriteByte(src[i])
			i++
		case inComment:
			if src[i] == '\n' {
				state = plain
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
			i++
		case inString:
			// Экранированный знак внутри литерала закрывающим не бывает.
			if src[i] == '\\' && i+1 < len(src) {
				out.WriteString("  ")
				i += 2
				continue
			}
			if strings.HasPrefix(src[i:], delim) {
				state = plain
				for range delim {
					out.WriteByte(' ')
				}
				i += len(delim)
				continue
			}
			if src[i] == '\n' {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
			i++
		}
	}
	return out.String()
}

// quoteAt — открывающий разделитель литерала в позиции i, либо пусто.
// Тройной пробуется ПЕРВЫМ: иначе `"""` прочиталось бы как пустая строка `""`
// плюс открывающая кавычка, и вся шапка модуля осталась бы кодом.
func quoteAt(src string, i int) string {
	for _, d := range []string{`"""`, "'''", `"`, "'"} {
		if strings.HasPrefix(src[i:], d) {
			return d
		}
	}
	return ""
}

// auditNewmanSpineBinding — судящая функция гейта.
//
// Выделена, чтобы инъекция гоняла ЕЁ, а не свою копию: проба, повторяющая
// логику гейта, доказывала бы свойство копии.
//
// Находкой считается обращение к функции хребта НАПРЯМУЮ: такой вызов обязан
// назвать решения набора первым аргументом — и тем самым заводит ВТОРОЕ место,
// где эти решения записаны, а разойдётся оно молча.
func auditNewmanSpineBinding(consumers map[string]string, kinds map[string]string) ([]string, spineBindingCensus) {
	cen := spineBindingCensus{consumers: len(consumers)}

	rels := make([]string, 0, len(consumers))
	for rel := range consumers {
		rels = append(rels, rel)
		switch kinds[rel] {
		case "проверка кейсов":
			cen.validators++
		case "проба":
			cen.probes++
		}
	}
	sort.Strings(rels)

	var findings []string
	for _, rel := range rels {
		code := pythonCode(consumers[rel])

		bound := reSpineBoundCall.FindAllStringSubmatch(code, -1)
		cen.callSites += len(bound)
		cen.bound += len(bound)

		var direct []string
		for _, m := range reSpineDirectCall.FindAllStringSubmatch(code, -1) {
			cen.callSites++
			direct = append(direct, m[1])
		}
		if len(direct) == 0 {
			continue
		}
		sort.Strings(direct)
		findings = append(findings, fmt.Sprintf(
			"%s (%s) — зовёт хребет генератора напрямую, минуя связывание набора: %s",
			rel, kinds[rel], strings.Join(direct, ", ")))
	}
	return findings, cen
}

// Потребитель генератора newman берёт форму коллекции ТЕМ ЖЕ связыванием, что
// генерация.
//
// ПРЕДМЕТ. Модуль кейсов исполняется с впрыснутыми именами набора; шаг, кейс и
// коллекция собираются решениями набора (`Emit`). Пока потребитель — проверка
// кейсов или регрессионная проба — зовёт функцию хребта своими руками, эти
// решения записаны ДВАЖДЫ, и расходятся они на первой же смене подписи.
//
// ЦЕНА ИЗМЕРЕНА, А НЕ ПРЕДПОЛОЖЕНА, И ИЗМЕРЕНА ДВАЖДЫ.
//
//   - Сведение хребта генератора (#1379) сделало перечень впрыскиваемого
//     обязательным параметром загрузчика. Генераторы его передают; ШЕСТЬ
//     проверок кейсов — нет, и каждая стала отвечать `TypeError: … missing 1
//     required positional argument` на ЛЮБОЙ вход. Тогда гейт и завёлся.
//   - Гейт закрыл экземпляр, а не класс: он смотрел ТОЛЬКО на `validate-cases.py`
//     и только на загрузчик. Регрессионные пробы вокруг оснастки — тот же
//     потребитель того же хребта — остались вне наблюдения, и после того же
//     сведения ДВАДЦАТЬ ВОСЕМЬ проб перестали исполняться (#1536): четыре файла,
//     восемь мест вызова, четыре различные функции хребта. Это не красный
//     вердикт, а «не выполнилось», поданное как красное: слот занят, вердикта
//     нет ни у одной из них.
//
// ПОЧЕМУ ЭТОТ ГЕЙТ СВЕРХ ПРОГОНЩИКА ПРОБ. `run-python-probes.py` ловит ИСХОД —
// `TypeError` на прогоне; этот гейт ловит ПРИЧИНУ и называет её словами: обход
// связывания. Диагностика — часть свойства, а не украшение: по «TypeError»
// читатель идёт чинить пробу, по находке отсюда — чинить обход. И гейт краснеет
// на обходе ДО того, как подпись сменилась, то есть до поломки.
//
// ЧЕГО ГЕЙТ НЕ СУДИТ. Он не требует, чтобы связывание называлось определённым
// именем, и не проверяет, ЧТО в него связано: это решение набора. Предмет здесь
// один — что решения записаны в ОДНОМ месте, а потребитель берёт их оттуда.
func TestNewmanConsumersReachTheSpineThroughTheSuiteBinding(t *testing.T) {
	root := repoRoot(t)

	// Состав — из индекса git, а не обходом диска: под корнем лежат рабочие
	// копии агентов, и обход по диску сделал бы вердикт свойством чужого
	// каталога — в обе стороны.
	tt := newTrackedTree(t, root)

	consumers := map[string]string{}
	kinds := map[string]string{}
	for rel := range tt.files {
		if !strings.Contains(rel, "tests/newman/scripts/") {
			continue
		}
		base := filepath.Base(rel)
		var kind string
		switch {
		case base == "validate-cases.py":
			kind = "проверка кейсов"
		case strings.HasSuffix(base, "_test.py"):
			kind = "проба"
		default:
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса git этого модуля
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		consumers[rel] = string(b)
		kinds[rel] = kind
	}
	if len(consumers) == 0 {
		t.Fatalf("предпосылка гейта не выполняется: потребителей генератора newman в индексе\n" +
			"НОЛЬ — либо файлы переименованы, либо обход смотрит не туда; чинить надо\n" +
			"гейт, а не молча выходить успехом.")
	}

	findings, cen := auditNewmanSpineBinding(consumers, kinds)

	if cen.callSites == 0 {
		t.Fatalf("предпосылка гейта не выполняется: осмотрено потребителей %d (проверок %d,\n"+
			"проб %d), обращений к хребту НОЛЬ. Либо потребители перестали звать хребет,\n"+
			"либо распознаватель не знает формы вызова — в обоих случаях это отказ, а не\n"+
			"молчание: гейт, потерявший предмет, вечнозелен.",
			cen.consumers, cen.validators, cen.probes)
	}

	t.Logf("осмотрено потребителей генератора newman: %d (проверок кейсов %d, проб %d); "+
		"обращений к хребту %d, из них через связывание набора %d",
		cen.consumers, cen.validators, cen.probes, cen.callSites, cen.bound)

	if len(findings) > 0 {
		t.Fatalf("потребитель зовёт хребет генератора СВОИМИ руками, а не связыванием набора.\n"+
			"Тогда решения набора записаны в двух местах, и расходятся они молча: генерация\n"+
			"остаётся зелёной, а потребитель перестаёт исполняться ВОВСЕ — его отказ читается\n"+
			"как находка о кейсах либо как красный вердикт пробы. Чинится вызовом связывания,\n"+
			"объявленного в gen.py рядом с самими решениями (`gen._RUN.load`, `_RUN.case_item`,\n"+
			"`_RUN.step_item`, `_RUN.collection`):\n  %s",
			strings.Join(findings, "\n  "))
	}
}
