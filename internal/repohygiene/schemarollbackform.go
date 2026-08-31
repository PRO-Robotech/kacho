// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schemarollbackform.go — инвентарь миграций, после которых ПРЕЖНИЙ образ
// обслуживать схему уже не может.
//
// # Предмет: откат объявлен штатным и неисполним по схеме
//
// Мигратор идёт при КАЖДОМ раскате, поэтому штатный откат выкатки возвращает
// прежний образ НА НОВУЮ СХЕМУ. Схему он не возвращает: секция `Down` на этом
// пути не исполняется вовсе — её запускает только отдельный глагол мигратора,
// которого откат поставки не зовёт. Отсюда следствие, которое надо назвать
// вслух, потому что оно обратно ожидаемому: НАЛИЧИЕ секции отката у миграции
// ничего не говорит о выполнимости отката образа. Она про другой путь.
//
// Значит выполнимость решает одно: осталась ли в схеме та колонка, которую
// прежний образ читает или пишет. Три формы отнимают её, и все три — в
// Up-секции:
//
//	DROP COLUMN     прежний образ ВЫБИРАЕТ снятую колонку   → отказ на первом чтении
//	RENAME COLUMN   прежний образ выбирает прежнее имя      → то же самое
//	SET NOT NULL    прежний образ ВСТАВЛЯЕТ без неё         → отказ на первой записи
//
// # Почему этого не ловило ничто
//
// Машинно необратимость в этом дереве объявляет ровно один механизм —
// `dropguard`, — и его распознаватель знает `DROP TABLE` и больше ничего
// (`internal/dropguard/inventory.go`, `dropTableRe`). Снятие КОЛОНКИ ему не
// нарушение, а НЕВИДИМОСТЬ: он о такой форме не спрашивает.
//
// Замер на 1b0592608 (перепись печатает те же числа на каждом прогоне, поэтому
// устаревание здесь видно, а не подразумевается): миграций 354, отнимают
// колонку 27 файлов — DROP COLUMN 17 файлов / 54 вхождения, SET NOT NULL 8/11,
// RENAME COLUMN 2/5. Объявлено из них было НОЛЬ.
//
// Форма, о которой распознаватель не знает, не край и не редкость: всё
// записанное в ней оказывается вне наблюдения — не красным и не зелёным, а
// молчанием. Поэтому гейт заводится на ФОРМУ, а не на файл.
//
// # Граница с соседними механизмами — названа, чтобы не завести второе место
//
// `DROP TABLE` этот гейт НЕ судит: его предмет принадлежит `dropguard`, там он
// объявляется вместе с числом строк, которые снятие уничтожает. Дублировать
// его здесь значило бы завести два места об одном предмете, и разошлись бы они
// молча. Здесь — ровно остаток: то, что `dropguard` не видит.
//
// Снятие ограничения, индекса, триггера, умолчания, функции и
// последовательности предметом не является: прежний образ читает и пишет
// КОЛОНКИ, а перечисленное его обращений к ним не отменяет.
//
// # Как объявляется решение
//
// Запрет #5 запрещает править применённую миграцию, поэтому у файла, лежавшего
// в дереве до заведения гейта, признака в себе быть не может — его решение
// живёт в счётной ведомости [schemaRollbackBaselineFile]. Ведомость хранит
// ТОЧНОЕ число вхождений, а не потолок: потолок не краснеет никогда и потому не
// истекает. Запись, у которой предмета больше нет, — находка.
//
// Новая миграция ведомости не получает: её признак стоит В НЕЙ САМОЙ строкой
//
//	-- +kacho point-of-no-return: <чем это ломает прежний образ>
//
// Признак читается из СЫРОЙ Up-секции, а формы — из исполняемой: признак сам
// является комментарием, и в исполняемой части его нет by construction.
//
// # ГРАНИЦА, названная честно: что этот гейт НЕ доказывает
//
// Он производитель отказа на КОММИТЕ, а не на откате. Он делает точку
// невозврата машинночитаемой — и не отвергает прежний образ, вставший на новую
// схему: читателя у объявления на пути старта сегодня нет вовсе (перепись
// прод-кода по версии схемы даёт ноль). Это остаток задачи #1690, а не её
// закрытие, и следствие заведено отдельно (#1734: слот готовности не
// спрашивает о версии схемы, поэтому такой под объявляется готовым).
//
// Он судит ИЗМЕНЕНИЕ СХЕМЫ, а не код: снята ли колонка, которую прежний образ
// действительно читает, он не знает и знать не может — прежний образ ему не
// дан. Поэтому находка означает «откат образа через эту версию под вопросом», а
// не «сломается наверняка»; решение принимает человек и записывает его
// признаком или строкой ведомости.
//
// Перечень форм ЗАКРЫТ и составлен по дереву, а не по памяти: сужение типа
// (`ALTER COLUMN ... TYPE`) в Up-секциях дерева на момент заведения не
// встречалось ни разу, поэтому в перечень не внесено — форма без предмета дала
// бы утверждение, которое не может ни покраснеть, ни позеленеть. Появится —
// вносится вместе со своей инъекцией.
package repohygiene

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/schemaguard"
)

// schemaRollbackBaselineFile — счётная ведомость файлов, лежавших в дереве до
// заведения гейта. Путь назван, а не выведен: ведомость — предмет решения, а не
// свойство дерева.
const schemaRollbackBaselineFile = "internal/repohygiene/schema-rollback-baseline.txt"

// pointOfNoReturnMarker — признак в самой миграции.
//
// ВЛАДЕЛЕЦ ТОКЕНА — ПРОД-КОД (`pkg/schemaguard`), а не этот гейт, и это не
// косметика. У объявления теперь ДВА читателя: гейт, требующий признака на
// коммите, и страж старта, снимающий под из ротации на несовместимой схеме
// (задача #1734). Две копии токена разошлись бы молча — и разошлись бы там, где
// расхождение не видно: автор прочитал бы в подсказке гейта одно, а страж
// старта искал бы другое, то есть проверка была бы зелена, а защита мертва.
//
// Общий источник снимает предмет расхождения by construction: приведение
// невозможно обойти, потому что символ один.
const pointOfNoReturnMarker = schemaguard.PointOfNoReturnMarker

// schemaRollbackForm — форма, отнимающая у прежнего образа колонку.
type schemaRollbackForm struct {
	Name string
	Re   *regexp.Regexp
	Why  string
}

// schemaRollbackForms — ВСЕ формы, которые гейт судит. Перечень закрытый и
// объявлен один раз: инъекция доказывает способность упасть по КАЖДОЙ из них
// отдельно, иначе молчание одной неотличимо от её отсутствия.
var schemaRollbackForms = []schemaRollbackForm{
	{"DROP COLUMN", regexp.MustCompile(`(?is)\bDROP\s+COLUMN\b`),
		"прежний образ выбирает снятую колонку — отказ на первом чтении"},
	{"RENAME COLUMN", regexp.MustCompile(`(?is)\bRENAME\s+COLUMN\b`),
		"прежний образ выбирает прежнее имя — отказ на первом чтении"},
	{"SET NOT NULL", regexp.MustCompile(`(?is)\bSET\s+NOT\s+NULL\b`),
		"прежний образ вставляет строку без этой колонки — отказ на первой записи"},
}

// schemaRollbackCensus — объём осмотренного. Отдельное утверждение, а не
// примечание: «ноль находок» обязано быть отличимо от «ноль прочитанного», а
// разложение по формам делает видимой именно ту слепоту, ради которой гейт
// заведён, — форму, о которой распознаватель не спрашивает.
type schemaRollbackCensus struct {
	Files      int // файлов миграций осмотрено
	WithForm   int // из них отнимают колонку
	Marked     int // объявлены признаком в себе
	Baselined  int // объявлены ведомостью
	Undeclared int // не объявлены ничем
	ByForm     map[string]int
}

func (c schemaRollbackCensus) String() string {
	names := make([]string, 0, len(c.ByForm))
	for _, f := range schemaRollbackForms {
		if n := c.ByForm[f.Name]; n > 0 {
			names = append(names, fmt.Sprintf("%s=%d", f.Name, n))
		}
	}
	by := "—"
	if len(names) > 0 {
		by = strings.Join(names, ", ")
	}
	return fmt.Sprintf(
		"миграций осмотрено %d · отнимают колонку %d (%s) · объявлено признаком %d · ведомостью %d · НЕ объявлено %d",
		c.Files, c.WithForm, by, c.Marked, c.Baselined, c.Undeclared)
}

// schemaRollbackSource — исходник миграции, поданный разбору. Разбор ОДИН на
// настоящее дерево и на инъекцию, поэтому фикстура не может оказаться
// снисходительнее того, что судит дерево.
type schemaRollbackSource struct {
	Rel  string
	Body string
}

// scanSchemaRollbackForms — какие формы и сколько раз стоят в Up-секции.
//
// Формы ищутся в ИСПОЛНЯЕМОЙ части: комментарии забелены общим разбором
// (`migrationUpSection`), а строковые литералы — [sqlBlankStrings]. Без второго
// прозаическое сообщение об ошибке, содержащее слова формы, читалось бы как
// сама форма; такие сообщения в дереве есть.
func scanSchemaRollbackForms(body string) map[string]int {
	exec := sqlBlankStrings(migrationUpSection(body))
	out := map[string]int{}
	for _, f := range schemaRollbackForms {
		if n := len(f.Re.FindAllString(exec, -1)); n > 0 {
			out[f.Name] = n
		}
	}
	return out
}

// hasPointOfNoReturnMarker — признак читается из СЫРОЙ Up-секции: он сам
// комментарий, и в исполняемой части его нет by construction. Пустое
// обоснование признаком не считается — иначе токен становится печатью, которую
// ставят не читая.
//
// РАСПОЗНАВАТЕЛЬ ОДИН на гейт и на стража старта (`pkg/schemaguard`): гейт
// требует признака на коммите, страж читает его в развёрнутом образе, и
// вопрос у них ОДИН И ТОТ ЖЕ. Своя копия разбора отвечала бы на него иначе —
// и заметить это было бы неоткуда: обе стороны зелены поодиночке.
func hasPointOfNoReturnMarker(body string) bool {
	return schemaguard.DeclaresPointOfNoReturn(body)
}

// parseSchemaRollbackBaseline — разбор ведомости. Строка:
//
//	<путь>|<форма>|<вхождений>
//
// Пустые строки и строки, начинающиеся с `#`, — разбор и обоснование.
func parseSchemaRollbackBaseline(text string) (map[schemaRollbackKey]int, []string) {
	out := map[schemaRollbackKey]int{}
	var malformed []string
	for i, line := range strings.Split(text, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		parts := strings.Split(s, "|")
		if len(parts) != 3 {
			malformed = append(malformed, fmt.Sprintf("строка %d: %q — ожидалось <путь>|<форма>|<вхождений>", i+1, s))
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil || n <= 0 {
			malformed = append(malformed, fmt.Sprintf("строка %d: %q — число вхождений не разобрано", i+1, s))
			continue
		}
		out[schemaRollbackKey{File: strings.TrimSpace(parts[0]), Form: strings.TrimSpace(parts[1])}] = n
	}
	return out, malformed
}

// schemaRollbackKey — предмет одной записи ведомости.
type schemaRollbackKey struct {
	File string
	Form string
}

// schemaRollbackFinding — находка. Без координаты находка не действие, поэтому
// она несёт файл, форму и число вхождений.
type schemaRollbackFinding struct {
	Kind string // "не объявлено" | "ведомость разошлась" | "предмета больше нет"
	Key  schemaRollbackKey
	Have int
	Want int
}

func (f schemaRollbackFinding) String() string {
	switch f.Kind {
	case "не объявлено":
		why := ""
		for _, sf := range schemaRollbackForms {
			if sf.Name == f.Key.Form {
				why = sf.Why
			}
		}
		return fmt.Sprintf(
			"%s: Up-секция несёт %s (%d раз) — %s.\n"+
				"    После этой версии ОТКАТ ОБРАЗА неисполним: секция Down на пути отката "+
				"поставки не исполняется, схема остаётся новой.\n"+
				"    Объяви это в самой миграции строкой в Up-секции:\n"+
				"        %s <чем это ломает прежний образ>\n"+
				"    Применённую миграцию править нельзя (запрет #5) — если файл уже накатан, "+
				"строка идёт в %s видом «%s|%s|%d».",
			f.Key.File, f.Key.Form, f.Have, why,
			pointOfNoReturnMarker, schemaRollbackBaselineFile, f.Key.File, f.Key.Form, f.Have)
	case "ведомость разошлась":
		return fmt.Sprintf(
			"%s: ведомость объявляет %s %d раз(а), в файле — %d.\n"+
				"    Ведомость хранит ТОЧНОЕ число, а не потолок: потолок не краснеет "+
				"никогда и потому не истекает. Приведи запись к факту.",
			f.Key.File, f.Key.Form, f.Want, f.Have)
	default: // "предмета больше нет"
		return fmt.Sprintf(
			"%s: ведомость объявляет %s, а в файле этой формы нет (файла может не быть вовсе).\n"+
				"    Исключение, которому нечего исключать, — находка: сними запись из %s.",
			f.Key.File, f.Key.Form, schemaRollbackBaselineFile)
	}
}

// findSchemaRollbackFindings — разбор состава дерева. Вход — исходники
// миграций и текст ведомости; выхода два: перепись и находки.
func findSchemaRollbackFindings(srcs []schemaRollbackSource, baseline map[schemaRollbackKey]int) (schemaRollbackCensus, []schemaRollbackFinding) {
	census := schemaRollbackCensus{ByForm: map[string]int{}}
	seen := map[schemaRollbackKey]bool{}
	var out []schemaRollbackFinding

	sorted := append([]schemaRollbackSource(nil), srcs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Rel < sorted[j].Rel })

	for _, s := range sorted {
		// Имя файла разбирает та же регулярка, что и остальные инвентари
		// (`migrationversionnamespace.go`). Второго разбора одного имени в
		// дереве не заводим: два разбора разошлись бы молча.
		if !migrationVersionFileRe.MatchString(path.Base(s.Rel)) {
			continue
		}
		census.Files++
		forms := scanSchemaRollbackForms(s.Body)
		if len(forms) == 0 {
			continue
		}
		census.WithForm++
		marked := hasPointOfNoReturnMarker(s.Body)
		if marked {
			census.Marked++
		}
		names := make([]string, 0, len(forms))
		for n := range forms {
			names = append(names, n)
		}
		sort.Strings(names)
		fileBaselined := false
		for _, n := range names {
			census.ByForm[n] += forms[n]
			key := schemaRollbackKey{File: s.Rel, Form: n}
			seen[key] = true
			want, inBaseline := baseline[key]
			switch {
			case marked:
				// признак в себе — решение записано у предмета
			case inBaseline && want == forms[n]:
				fileBaselined = true
			case inBaseline:
				out = append(out, schemaRollbackFinding{Kind: "ведомость разошлась", Key: key, Have: forms[n], Want: want})
			default:
				out = append(out, schemaRollbackFinding{Kind: "не объявлено", Key: key, Have: forms[n]})
			}
		}
		if fileBaselined {
			census.Baselined++
		}
		if !marked && !fileBaselined {
			census.Undeclared++
		}
	}

	// Самоистечение: запись, у которой предмета больше нет.
	stale := make([]schemaRollbackKey, 0)
	for k := range baseline {
		if !seen[k] {
			stale = append(stale, k)
		}
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].File != stale[j].File {
			return stale[i].File < stale[j].File
		}
		return stale[i].Form < stale[j].Form
	})
	for _, k := range stale {
		out = append(out, schemaRollbackFinding{Kind: "предмета больше нет", Key: k})
	}
	return census, out
}
