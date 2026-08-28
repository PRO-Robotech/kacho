// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalcursorupperbound.go — анализатор «читатель журнала не продвигает
// позицию по ГОЛОМУ НОМЕРУ».
//
// # Предмет — не файл, а ДИСЦИПЛИНА ЧТЕНИЯ
//
// Номер строки журнала выдаёт счётчик на ВСТАВКЕ, а строка становится видимой на
// ФИКСАЦИИ. Порядок номеров и порядок фиксаций поэтому независимы: читатель,
// продвинувший курсор за номер, который ещё не стал видимым, эту строку не
// получит НИКОГДА — перечитывание идёт строго «больше курсора», и возобновление
// с сохранённой позиции воспроизводит ту же дыру. Сходиться тут нечему.
//
// Гарантия и её цена объявлены один раз —
// `docs/architecture/journal-position-settled-watermark.md`; исполнение —
// `pkg/subscription/watermark.go`. Этот анализатор держит СЛЕДСТВИЕ: чтобы
// граница была не только написана, но и соблюдена всеми читателями.
//
// # Чем опознаётся возобновимое чтение
//
// Тремя признаками в ОДНОМ тексте запроса: восходящий порядок по некоторому
// выражению, нижняя граница по ТОМУ ЖЕ выражению против связанного параметра, и
// отсутствие клейма.
//
// Направление — НЕОБЯЗАТЕЛЬНОЕ: `ASC` есть умолчание стандарта, и `ORDER BY seq`
// означает ровно то же, что `ORDER BY seq ASC`. Требовать написанное слово
// значило бы судить ФОРМУ ЗАПИСИ, а не запрос, — и молчать на той же дыре,
// записанной короче. Обе стороны сравнения тоже переставимы (`col > $1` и
// `$1 < col`), а включающая граница (`>=`) от исключающей не отличается: обе
// продвигают позицию.
//
// # Что выведено из предмета BY CONSTRUCTION, а не перечнем
//
//   - НИСХОДЯЩАЯ выборка — курсора «дальше позиции» не выражает;
//   - ОЧЕРЕДЬ С КЛЕЙМОМ (`FOR UPDATE`) — курсор мимо строки не двигается,
//     пропущенная незакоммиченная будет забрана следующим проходом;
//   - ПОЗИЦИЯ, КОТОРУЮ НЕ ВЫДАЁТ БАЗА, — крокфордов `text`-идентификатор, метка
//     времени, десятичное: счётчика на вставке у них нет вовсе, поэтому «номер
//     выдан раньше видимости» к ним неприменимо. Тип берётся из СХЕМЫ ДЕРЕВА, а
//     не из имени колонки и не из списка исключений.
//
// Оговорка о клейме: на сегодняшнем дереве эта ветка не исключает НИ ОДНОГО
// запроса (перепись печатает «очередей с клеймом 0») — здешние очереди
// упорядочены не по одной колонке и под первые два признака не подпадают.
// Ветка написана как разграничение и проверена инъекцией, но предмета в дереве
// у неё пока нет; читать «клейм разграничивается» как «клейм встречается»
// нельзя.
//
// # Чего распознаватель НЕ ЛОВИТ — названо, а не умолчано
//
// Он читает ОДИН строковый литерал, поэтому слеп к двум законным записям того же
// запрещённого чтения:
//
//   - ЗАПРОС, СКЛЕЕННЫЙ ИЗ НЕСКОЛЬКИХ ЛИТЕРАЛОВ (порядок в одном, граница в
//     другом);
//   - ПОРЯДОК ПО НОМЕРУ ВЫРАЖЕНИЯ в списке выборки (`ORDER BY 1`) — связать
//     номер с колонкой надёжно нельзя там, где список собран из `%s`.
//
// Обе слепые зоны зафиксированы пробой
// (`TestJournalCursorGateDeclaresItsBlindSpots`): научит кто-нибудь
// распознаватель — проба покраснеет и заставит поправить эту шапку. Молчаливая
// слепота тем и опасна, что неотличима от отсутствия предмета.
//
// # Что вне обхода
//
// Пробы (`_test.go`) — несут синтетические фикстуры строковым литералом;
// сгенерённые стабы (`pkg/api/`) — запросов не содержат вовсе. Корни обхода
// перечисляет вызывающий; на сегодняшнем дереве `tools/` в них НЕ ВХОДИТ —
// предмета там нет (`ORDER BY` в его прод-коде ноль), но и наблюдения тоже нет,
// и это сказано здесь, а не подразумевается.
//
// # Неизвестный вход — ЯВНЫЙ ОТКАЗ
//
// Колонка, объявленного типа которой в схемах дерева нет, даёт находку
// `КОЛОНКА-НЕОПОЗНАНА`: молчание означало бы «счётчика не нашли, значит его
// нет». Чинится такая находка ПОПОЛНЕНИЕМ СПИСКА ТИПОВ либо расширением корней
// схемы — но НИКОГДА записью в ведомости послаблений: послабление прощает
// нарушение, а здесь нарушения нет, есть неполнота наблюдения. Это разные
// предметы, и подмена одного другим гасит наблюдение навсегда.
//
// # Ведомость послаблений и её самоистечение
//
// Чтение, законно живущее с голым номером, стоит в ведомости с причиной и
// предикатом снятия. Запись, которой в дереве больше нечего исключать, — САМА
// НАХОДКА: иначе она переживёт свой фикс и разрешит следующему завести такое же
// чтение под тем же оправданием.
//
// Падает анализатор на ПУСТОМ ОБХОДЕ: ноль файлов прод-кода либо ноль колонок
// схемы — тогда «ноль находок» неотличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Виды находок.
const (
	JournalCursorBareNumber        = "КУРСОР-ПО-ГОЛОМУ-НОМЕРУ"
	JournalCursorUnresolvedColumn  = "КОЛОНКА-НЕОПОЗНАНА"
	JournalCursorAllowanceNoReason = "ПОСЛАБЛЕНИЕ-БЕЗ-ПРИЧИНЫ"
	JournalCursorAllowanceStale    = "ПОСЛАБЛЕНИЕ-БЕЗ-ПРЕДМЕТА"
)

// JournalCursorAllowance — одно послабление.
type JournalCursorAllowance struct {
	File    string
	Column  string
	Because string
}

// JournalCursorOptions — вход анализатора.
type JournalCursorOptions struct {
	Root     string
	GoRoots  []string
	SQLRoots []string
	Allow    []JournalCursorAllowance
}

// JournalCursorCensus — объём осмотренного. Печатается ВСЕГДА.
type JournalCursorCensus struct {
	SQLFiles       int
	Columns        int
	GoFiles        int
	Literals       int
	ResumableReads int
	Claimed        int
	CounterReads   int
	Bounded        int
	Allowances     int
}

// JournalCursorFinding — одна находка.
type JournalCursorFinding struct {
	Kind  string
	Where string
	What  string
}

func (f JournalCursorFinding) String() string {
	return fmt.Sprintf("[%s] %s — %s", f.Kind, f.Where, f.What)
}

var (
	// Направление — НЕОБЯЗАТЕЛЬНОЕ и попадает в захват: `ASC` есть умолчание
	// стандарта, и `ORDER BY seq` — та же восходящая выборка, записанная короче.
	// Требовать слово `ASC` значило бы судить форму записи, а не запрос.
	jcOrderBy = regexp.MustCompile(`(?is)ORDER\s+BY\s+([A-Za-z_0-9.%\[\]]+)(?:\s+(ASC|DESC))?`)
	jcFrom    = regexp.MustCompile(`(?is)\bFROM\s+([A-Za-z_0-9."%\[\]]+)`)
	jcClaim   = regexp.MustCompile(`(?is)FOR\s+UPDATE`)
	jcCreate  = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z0-9_."]+)\s*\((.*?)\n\s*\)`)
	jcColLine = regexp.MustCompile(`(?i)^([a-z_][a-z0-9_]*)\s+([a-z][a-z0-9 ]*)`)
)

// jcCounterTypes — типы, у которых номер выдаёт БАЗА на вставке. Только у них
// порядок номеров и порядок фиксаций независимы, а значит только они несут класс.
var jcCounterTypes = []string{"bigserial", "bigint", "serial", "integer", "int", "smallint"}

// jcOpaqueTypes — типы, у которых счётчика на вставке нет вовсе: значение
// назначено до записи, поэтому «номер выдан раньше видимости» неприменимо.
// jcOpaqueTypes — типы, у которых счётчика на вставке нет: значение назначено до
// записи либо не является выданным номером. Сюда же время и десятичное: курсор по
// метке времени — законная постраничная выборка, а не журнальная позиция.
//
// Список ПОПОЛНЯЕТСЯ здесь, а не обходится записью в ведомости послаблений:
// послабление прощает нарушение, а неполный список даёт НЕ ТУ находку
// (`КОЛОНКА-НЕОПОЗНАНА` вместо законного молчания) — это разные предметы.
var jcOpaqueTypes = []string{
	"text", "uuid", "varchar", "char", "character", "citext",
	"timestamptz", "timestamp", "date", "time", "interval",
	"numeric", "decimal", "real", "double", "boolean", "bool", "jsonb", "json", "bytea", "inet", "cidr",
}

// jcResumableRead — одно опознанное возобновимое чтение.
type jcResumableRead struct {
	File    string
	Line    int
	Table   string
	Column  string
	Bounded bool
	Counter bool
	Known   bool
	Claimed bool
}

// AuditJournalCursorUpperBound судит дерево.
func AuditJournalCursorUpperBound(
	o JournalCursorOptions, log io.Writer,
) ([]JournalCursorFinding, JournalCursorCensus, error) {
	var census JournalCursorCensus
	census.Allowances = len(o.Allow)

	schema, sqlFiles, err := jcCollectSchema(o.Root, o.SQLRoots)
	if err != nil {
		return nil, census, err
	}
	census.SQLFiles = sqlFiles
	census.Columns = len(schema)

	reads, goFiles, lits, err := jcCollectReads(o.Root, o.GoRoots, schema)
	if err != nil {
		return nil, census, err
	}
	census.GoFiles = goFiles
	census.Literals = lits
	for _, r := range reads {
		if r.Claimed {
			census.Claimed++
			continue
		}
		census.ResumableReads++
		if r.Counter {
			census.CounterReads++
			if r.Bounded {
				census.Bounded++
			}
		}
	}

	_, _ = fmt.Fprintf(log,
		"осмотрено: файлов схемы %d · колонок %d · файлов прод-кода Go %d · литералов %d · "+
			"очередей с клеймом %d · возобновимых чтений %d (по счётчику %d, из них ограничено сверху %d) · послаблений %d\n",
		census.SQLFiles, census.Columns, census.GoFiles, census.Literals,
		census.Claimed, census.ResumableReads, census.CounterReads, census.Bounded, census.Allowances)

	if census.GoFiles == 0 || census.Columns == 0 {
		return nil, census, fmt.Errorf(
			"обход пуст: файлов прод-кода Go %d, колонок схемы %d — «ноль находок» неотличимо от «ноль прочитанного»",
			census.GoFiles, census.Columns)
	}

	allowed := make(map[string]JournalCursorAllowance, len(o.Allow))
	var findings []JournalCursorFinding
	for _, a := range o.Allow {
		if a.Because == "" {
			findings = append(findings, JournalCursorFinding{
				Kind:  JournalCursorAllowanceNoReason,
				Where: a.File + ":" + a.Column,
				What: "послабление без причины и предиката снятия: закрыть глаз стало бы дешевле, " +
					"чем ограничить чтение сверху",
			})
			continue
		}
		allowed[filepath.ToSlash(a.File)+":"+strings.ToLower(a.Column)] = a
	}

	used := map[string]bool{}
	for _, r := range reads {
		if r.Claimed {
			continue
		}
		key := r.File + ":" + strings.ToLower(r.Column)

		// Неизвестный вход — ЯВНЫЙ ОТКАЗ. Колонка, которой нет ни в одной схеме
		// дерева, не может быть объявлена безопасной по умолчанию: молчание тут
		// означало бы «счётчика не нашли, значит его нет».
		if !r.Known {
			if _, ok := allowed[key]; ok {
				used[key] = true
				continue
			}
			findings = append(findings, JournalCursorFinding{
				Kind:  JournalCursorUnresolvedColumn,
				Where: fmt.Sprintf("%s:%d", r.File, r.Line),
				What: fmt.Sprintf(
					"возобновимое чтение по колонке %q таблицы %q: объявленного типа в схемах дерева нет, "+
						"поэтому счётчик от непрозрачного идентификатора неотличим — а от этого зависит, несёт ли чтение класс",
					r.Column, r.Table),
			})
			continue
		}
		if !r.Counter || r.Bounded {
			continue
		}
		if _, ok := allowed[key]; ok {
			used[key] = true
			continue
		}
		findings = append(findings, JournalCursorFinding{
			Kind:  JournalCursorBareNumber,
			Where: fmt.Sprintf("%s:%d", r.File, r.Line),
			What: fmt.Sprintf(
				"чтение продвигает позицию по голому номеру: колонка %q таблицы %q — счётчик, выдаваемый на вставке, "+
					"а верхней границы у запроса нет; строка, чей номер выдан до фиксации, за курсор не вернётся никогда",
				r.Column, r.Table),
		})
	}

	// Самоистечение ведомости: запись, которой больше нечего исключать, — сама
	// находка, иначе она переживёт свой фикс и разрешит следующему завести
	// такое же чтение под тем же оправданием.
	for key, a := range allowed {
		if used[key] {
			continue
		}
		findings = append(findings, JournalCursorFinding{
			Kind:  JournalCursorAllowanceStale,
			Where: key,
			What: fmt.Sprintf(
				"чтения по голому номеру по этой координате в дереве нет, а послабление осталось (%s)",
				a.Because),
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Where < findings[j].Where
	})
	return findings, census, nil
}

// jcCollectSchema собирает объявленные типы колонок из миграций.
func jcCollectSchema(root string, sqlRoots []string) (map[string]string, int, error) {
	schema := map[string]string{}
	var files []string
	for _, r := range sqlRoots {
		got, err := collectFiles(filepath.Join(root, r), ".sql")
		if err != nil {
			return nil, 0, err
		}
		files = append(files, got...)
	}
	for _, path := range files {
		src, err := readFileString(path)
		if err != nil {
			return nil, 0, err
		}
		for _, m := range jcCreate.FindAllStringSubmatch(src, -1) {
			table := jcTableName(m[1])
			for _, line := range strings.Split(m[2], "\n") {
				line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ","))
				cm := jcColLine.FindStringSubmatch(line)
				if cm == nil {
					continue
				}
				col := strings.ToLower(cm[1])
				switch col {
				case "constraint", "primary", "unique", "foreign", "check", "exclude", "like":
					continue
				}
				key := table + "." + col
				if _, seen := schema[key]; !seen {
					schema[key] = strings.TrimSpace(strings.ToLower(cm[2]))
				}
			}
		}
	}
	return schema, len(files), nil
}

// jcCollectReads опознаёт возобновимые чтения в строковых литералах прод-кода.
func jcCollectReads(
	root string, goRoots []string, schema map[string]string,
) ([]jcResumableRead, int, int, error) {
	var files []string
	for _, r := range goRoots {
		got, err := collectFiles(filepath.Join(root, r), ".go")
		if err != nil {
			return nil, 0, 0, err
		}
		files = append(files, got...)
	}

	var reads []jcResumableRead
	goFiles, lits := 0, 0
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		// Пробы несут синтетические фикстуры строковым литералом, сгенерённые
		// стабы запросов не содержат вовсе.
		if strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "pkg/api/") {
			continue
		}
		goFiles++
		found, n, err := jcFileReads(path, rel, schema)
		if err != nil {
			return nil, 0, 0, err
		}
		lits += n
		reads = append(reads, found...)
	}
	sort.Slice(reads, func(i, j int) bool {
		if reads[i].File != reads[j].File {
			return reads[i].File < reads[j].File
		}
		return reads[i].Line < reads[j].Line
	})
	return reads, goFiles, lits, nil
}

func jcFileReads(path, rel string, schema map[string]string) ([]jcResumableRead, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("разбор %s: %w", rel, err)
	}
	var out []jcResumableRead
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		count++
		text, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			text = lit.Value
		}
		r, ok := jcClassify(text, schema)
		if !ok {
			return true
		}
		r.File = rel
		r.Line = fset.Position(lit.Pos()).Line
		out = append(out, r)
		return true
	})
	return out, count, nil
}

// jcClassify — опознаёт ВОЗОБНОВИМОЕ ЧТЕНИЕ и разрешает тип его позиции.
//
// Возобновимое чтение — это три признака в ОДНОМ тексте запроса: восходящий
// порядок по некоторому выражению, нижняя граница по ТОМУ ЖЕ выражению против
// связанного параметра, и отсутствие клейма. Клейм (`FOR UPDATE`) выводит запрос
// из предмета by construction: очередь с клеймом курсор мимо строки не двигает —
// пропущенная незакоммиченная строка будет забрана следующим проходом.
func jcClassify(text string, schema map[string]string) (jcResumableRead, bool) {
	om := jcOrderBy.FindStringSubmatch(text)
	if om == nil {
		return jcResumableRead{}, false
	}
	// Нисходящая выборка курсор «дальше позиции» не выражает: предмета нет.
	if strings.EqualFold(om[2], "DESC") {
		return jcResumableRead{}, false
	}
	col := om[1]
	if !jcHasLowerBound(text, col) {
		return jcResumableRead{}, false
	}
	if jcClaim.MatchString(text) {
		// Очередь с клеймом ПРОЧИТАНА и осознанно выведена из предмета: курсор
		// мимо строки не двигается, пропущенная незакоммиченная будет забрана
		// следующим проходом. Считаем её отдельно — молчание на непрочитанном
		// ничего не доказывает.
		return jcResumableRead{Column: col, Claimed: true}, true
	}

	r := jcResumableRead{
		Column:  col,
		Bounded: jcHasUpperBound(text, col),
	}

	// Параметризованный журнал: имя таблицы и колонки приходят из объявленной
	// формы (`subscription.Storage`), где позиция объявлена счётчиком.
	if strings.Contains(col, "%") {
		r.Table, r.Counter, r.Known = "(параметризован)", true, true
		return r, true
	}

	fm := jcFrom.FindStringSubmatch(text)
	if fm == nil {
		return r, true
	}
	r.Table = jcTableName(fm[1])
	typ, ok := schema[r.Table+"."+strings.ToLower(col)]
	if !ok {
		return r, true
	}
	// Сравниваем ПЕРВОЕ СЛОВО объявления, а не префикс строки: `strings.HasPrefix`
	// опознал бы `interval` счётчиком по слову `int`, и колонка длительности
	// попала бы в предмет, ничего общего с ним не имея.
	head := typ
	if i := strings.IndexAny(head, " \t("); i >= 0 {
		head = head[:i]
	}
	r.Known = true
	for _, t := range jcCounterTypes {
		if head == t {
			r.Counter = true
			return r, true
		}
	}
	for _, t := range jcOpaqueTypes {
		if head == t {
			return r, true
		}
	}
	r.Known = false
	return r, true
}

// jcHasLowerBound / jcHasUpperBound — граница «снизу» и «сверху» в ОБЕИХ
// законных записях сравнения: и `col > $1`, и `$1 < col`. Операнды в SQL
// переставимы, поэтому предикат, знающий один порядок, пропускает тот же запрос,
// записанный наоборот.
//
// Включающая граница (`>=`) от исключающей (`>`) здесь НЕ отличается: обе
// продвигают позицию, разница лишь в том, отдаётся ли строка курсора повторно.
func jcHasLowerBound(text, col string) bool {
	q := regexp.QuoteMeta(col)
	return regexp.MustCompile(q+`\s*>=?\s*\$\d`).MatchString(text) ||
		regexp.MustCompile(`\$\d\s*<=?\s*`+q).MatchString(text)
}

func jcHasUpperBound(text, col string) bool {
	q := regexp.QuoteMeta(col)
	return regexp.MustCompile(q+`\s*<=?\s*\$\d`).MatchString(text) ||
		regexp.MustCompile(`\$\d\s*>=?\s*`+q).MatchString(text)
}

func jcTableName(raw string) string {
	raw = strings.ReplaceAll(raw, `"`, "")
	if i := strings.LastIndex(raw, "."); i >= 0 {
		raw = raw[i+1:]
	}
	return strings.ToLower(raw)
}
