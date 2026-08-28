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
//   - ПОЗИЦИЯ, ВЫДАННАЯ В ПОРЯДКЕ ФИКСАЦИЙ, — номер штампует триггер писателя,
//     берущий ТРАНЗАКЦИОННУЮ консультативную блокировку ПЕРЕД `nextval`.
//     Блокировка держится до фиксации, поэтому порядок номеров совпадает с
//     порядком фиксаций by construction, и «номер выдан до видимости» неверно.
//     Эта закрытость лежит НА СТОРОНЕ ПИСАТЕЛЯ и в запросе читателя невидима —
//     распознаватель выводит её из ТЕЛА ТРИГГЕРА в миграциях дерева;
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
// # Чем опознаётся ПОРЯДОК ФИКСАЦИЙ — четырьмя признаками, а не словом
//
// Одного вхождения `pg_advisory_xact_lock` в файле мало: судить по слову значило
// бы принять за закрытость блокировку, взятую не там, не так и не тем. Требуются
// все четыре признака сразу, и у каждого — своя инъекция:
//
//  1. тело функции присваивает `NEW.<колонка> := nextval(...)`;
//  2. ТРАНЗАКЦИОННАЯ блокировка (`pg_advisory_xact_lock`) взята ДО этого
//     присваивания — сеансовая (`pg_advisory_lock`) отпускается явно и до
//     фиксации не держится, а взятая ПОСЛЕ не упорядочивает ничего;
//  3. ключ блокировки НЕ ЗАВИСИТ ОТ СТРОКИ (в его аргументе нет `NEW.`/`OLD.`) —
//     ключ, вычисленный из строки, даёт порядок в пределах своей группы, а
//     читатель ведёт ОДИН курсор на всю таблицу;
//  4. функция подвешена триггером `BEFORE` на ту же таблицу, и его перечень
//     событий включает `INSERT` — `AFTER`-триггер `NEW` не меняет, а триггер без
//     `INSERT` оставляет вставленным строкам умолчание колонки, выданное вне
//     блокировки.
//
// Определения читаются В ПОРЯДКЕ МИГРАЦИЙ, и побеждает последнее: `CREATE OR
// REPLACE FUNCTION` без блокировки и `DROP TRIGGER` закрытость СНИМАЮТ. Иначе
// гейт судил бы о состоянии, отменённом более поздней миграцией.
//
// Читается ТОЛЬКО ВОСХОДЯЩАЯ ЧАСТЬ файла (до `-- +goose Down`). Нисходящая к
// живой базе не применяется, а состоит ровно из отмен: миграция, заведшая
// триггер, в своей нисходящей части его же и снимает — и распознаватель,
// читающий файл целиком, объявил бы закрытость снятой. Это не мелочь разбора:
// именно так первая редакция здесь и ошиблась, найдя ноль закрытых колонок при
// живом триггере в дереве.
//
// Оговорка о границе, чтобы её не приняли за общее свойство: карту ТИПОВ колонок
// анализатор по-прежнему собирает по файлу целиком, поэтому в ней остаются
// колонки таблиц, восстановленных нисходящей частью (замер: 16 файлов из 336
// несут `CREATE TABLE` в нисходящей). Это отдельный предмет — он меняет не
// закрытость, а разрешение имён, — и здесь не чинится.
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
	// CommitOrdered — чтения по счётчику БЕЗ верхней границы, закрытые на стороне
	// ПИСАТЕЛЯ: номер штампуется под транзакционной блокировкой, поэтому порядок
	// номеров есть порядок фиксаций. Считается ОТДЕЛЬНЫМ числом: иначе прибавка к
	// молчанию гейта была бы неотличима от расширения слепой зоны.
	CommitOrdered int
	// CommitOrderedColumns — сколько колонок дерева объявлено выдаваемыми в
	// порядке фиксаций. Ноль при непустой схеме означает, что распознаватель
	// триггеров ослеп, а не что таких колонок нет.
	CommitOrderedColumns int
	Allowances           int
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
	// Ordered — позиция выдаётся в порядке фиксаций (закрытость на стороне
	// писателя); Where — координата триггера, из которой это выведено.
	Ordered      bool
	OrderedWhere string
}

// AuditJournalCursorUpperBound судит дерево.
func AuditJournalCursorUpperBound(
	o JournalCursorOptions, log io.Writer,
) ([]JournalCursorFinding, JournalCursorCensus, error) {
	var census JournalCursorCensus
	census.Allowances = len(o.Allow)

	schema, ordered, sqlFiles, err := jcCollectSchema(o.Root, o.SQLRoots)
	if err != nil {
		return nil, census, err
	}
	census.SQLFiles = sqlFiles
	census.Columns = len(schema)
	census.CommitOrderedColumns = len(ordered)

	reads, goFiles, lits, err := jcCollectReads(o.Root, o.GoRoots, schema, ordered)
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
			switch {
			case r.Bounded:
				census.Bounded++
			case r.Ordered:
				census.CommitOrdered++
			}
		}
	}

	_, _ = fmt.Fprintf(log,
		"осмотрено: файлов схемы %d · колонок %d (из них выдаются в порядке фиксаций %d) · "+
			"файлов прод-кода Go %d · литералов %d · очередей с клеймом %d · "+
			"возобновимых чтений %d (по счётчику %d, из них ограничено сверху %d, "+
			"закрыто порядком фиксаций %d) · послаблений %d\n",
		census.SQLFiles, census.Columns, census.CommitOrderedColumns,
		census.GoFiles, census.Literals, census.Claimed,
		census.ResumableReads, census.CounterReads, census.Bounded, census.CommitOrdered,
		census.Allowances)

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
		// Третья закрытость: номер выдан под транзакционной блокировкой, значит
		// порядок номеров есть порядок фиксаций, и голый курсор безопасен. Она
		// лежит на стороне ПИСАТЕЛЯ и выведена из тела триггера, а не из запроса.
		if r.Ordered {
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

// jcCollectSchema собирает объявленные типы колонок из миграций И колонки,
// позиция которых выдаётся в ПОРЯДКЕ ФИКСАЦИЙ.
//
// Второй результат — карта `таблица.колонка` → координата триггера, из которой
// закрытость выведена. Координата идёт в перепись и в текст находки: «закрыто»
// без указания, ЧЕМ закрыто, читатель проверить не может.
func jcCollectSchema(
	root string, sqlRoots []string,
) (schema map[string]string, ordered map[string]string, files int, err error) {
	schema = map[string]string{}
	var paths []string
	for _, r := range sqlRoots {
		got, gerr := collectFiles(filepath.Join(root, r), ".sql")
		if gerr != nil {
			return nil, nil, 0, gerr
		}
		paths = append(paths, got...)
	}
	// Определения читаются В ПОРЯДКЕ МИГРАЦИЙ: `CREATE OR REPLACE FUNCTION` без
	// блокировки и `DROP TRIGGER` закрытость СНИМАЮТ, а без сортировки победило
	// бы то определение, которое обход встретил последним.
	sort.Strings(paths)
	state := jcNewOrderState()
	for _, path := range paths {
		src, rerr := readFileString(path)
		if rerr != nil {
			return nil, nil, 0, rerr
		}
		rel, _ := filepath.Rel(root, path)
		state.apply(jcUpPart(src), filepath.ToSlash(rel))
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
	return schema, state.resolve(), len(paths), nil
}

// jcCollectReads опознаёт возобновимые чтения в строковых литералах прод-кода.
func jcCollectReads(
	root string, goRoots []string, schema, ordered map[string]string,
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
		found, n, err := jcFileReads(path, rel, schema, ordered)
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

func jcFileReads(path, rel string, schema, ordered map[string]string) ([]jcResumableRead, int, error) {
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
		r, ok := jcClassify(text, schema, ordered)
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
func jcClassify(text string, schema, ordered map[string]string) (jcResumableRead, bool) {
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
			if where, yes := ordered[r.Table+"."+strings.ToLower(col)]; yes {
				r.Ordered, r.OrderedWhere = true, where
			}
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

// ─────────────────────────────────────────────────────────────────────────────
// ЗАКРЫТОСТЬ НА СТОРОНЕ ПИСАТЕЛЯ — позиция, выдаваемая в порядке фиксаций.
//
// Распознаватель судит СТРУКТУРУ объявления, а не встреченное слово: одно
// вхождение `pg_advisory_xact_lock` в файле не говорит ни того, что блокировка
// взята перед выдачей номера, ни того, что она вообще относится к этой колонке.
// Четыре признака и их инъекции названы в шапке пакета.

var (
	jcFuncHead = regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([A-Za-z0-9_."]+)\s*\(`)
	jcDollar   = regexp.MustCompile(`\$[A-Za-z0-9_]*\$`)
	// Присваивание позиции: `NEW.<колонка> := nextval(...)`. Знак `=` без
	// двоеточия plpgsql в присваивании тоже принимает, поэтому двоеточие
	// необязательно.
	jcStamp = regexp.MustCompile(`(?is)\bNEW\s*\.\s*([a-z_][a-z0-9_]*)\s*:?=\s*nextval\s*\(`)
	// Блокировка — ТОЛЬКО транзакционная и ТОЛЬКО безусловная. `pg_advisory_lock`
	// сеансовая (до фиксации не держится), `pg_try_advisory_xact_lock` вправе не
	// взять и пойти дальше — ни та, ни другая порядка не дают.
	jcXactLock = regexp.MustCompile(`(?i)\bpg_advisory_xact_lock\s*\(`)
	jcTrigger  = regexp.MustCompile(
		`(?is)CREATE\s+(?:CONSTRAINT\s+)?TRIGGER\s+([A-Za-z0-9_."]+)\s+` +
			`(BEFORE|AFTER|INSTEAD\s+OF)\s+(.*?)\s+ON\s+([A-Za-z0-9_."]+)\b(.*?)` +
			`EXECUTE\s+(?:FUNCTION|PROCEDURE)\s+([A-Za-z0-9_."]+)\s*\(`)
	jcTriggerDrop = regexp.MustCompile(
		`(?is)DROP\s+TRIGGER\s+(?:IF\s+EXISTS\s+)?([A-Za-z0-9_."]+)\s+ON\s+([A-Za-z0-9_."]+)`)
)

// jcStampedTrigger — подвешенный триггер, штампующий позицию.
type jcStampedTrigger struct {
	table    string
	fn       string
	before   bool
	onInsert bool
	where    string
}

// jcOrderState — состояние чтения миграций: последнее определение функции и
// набор подвешенных триггеров. Читается ПОСЛЕДОВАТЕЛЬНО, потому что более
// поздняя миграция вправе закрытость снять.
type jcOrderState struct {
	// stamps — имя функции → колонки, которые она штампует ПОД транзакционной
	// блокировкой с ключом, не зависящим от строки. Пустое множество означает
	// «функция известна и закрытости не даёт» — это НЕ то же, что «функции нет».
	stamps map[string]map[string]bool
	// triggers — (таблица, имя триггера) → подвешенный триггер.
	triggers map[string]jcStampedTrigger
}

func jcNewOrderState() *jcOrderState {
	return &jcOrderState{
		stamps:   map[string]map[string]bool{},
		triggers: map[string]jcStampedTrigger{},
	}
}

// jcEvent — одно объявление с его смещением: порядок ВНУТРИ файла значим не
// меньше порядка файлов (`DROP TRIGGER` и `CREATE TRIGGER` стоят подряд).
type jcEvent struct {
	at    int
	apply func()
}

func (st *jcOrderState) apply(src, rel string) {
	var events []jcEvent

	for _, fn := range jcFunctionBodies(src) {
		name, body := fn.name, fn.body
		at := fn.at
		events = append(events, jcEvent{at: at, apply: func() {
			st.stamps[name] = jcLockedStamps(body)
		}})
	}

	for _, m := range jcTrigger.FindAllStringSubmatchIndex(src, -1) {
		grp := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return src[m[2*i]:m[2*i+1]]
		}
		trg := jcTableName(grp(1))
		timing := strings.ToUpper(strings.Join(strings.Fields(grp(2)), " "))
		events2 := strings.ToUpper(grp(3))
		table := jcTableName(grp(4))
		fn := jcTableName(grp(6))
		at := m[0]
		where := fmt.Sprintf("%s (триггер %s на %s)", rel, trg, table)
		events = append(events, jcEvent{at: at, apply: func() {
			st.triggers[table+"/"+trg] = jcStampedTrigger{
				table:    table,
				fn:       fn,
				before:   timing == "BEFORE",
				onInsert: strings.Contains(events2, "INSERT"),
				where:    where,
			}
		}})
	}

	for _, m := range jcTriggerDrop.FindAllStringSubmatchIndex(src, -1) {
		trg := jcTableName(src[m[2]:m[3]])
		table := jcTableName(src[m[4]:m[5]])
		at := m[0]
		events = append(events, jcEvent{at: at, apply: func() {
			delete(st.triggers, table+"/"+trg)
		}})
	}

	sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
	for _, e := range events {
		e.apply()
	}
}

// resolve — какие колонки выдаются в порядке фиксаций ПОСЛЕ всех миграций.
func (st *jcOrderState) resolve() map[string]string {
	out := map[string]string{}
	for _, tr := range st.triggers {
		if !tr.before || !tr.onInsert {
			continue
		}
		for col := range st.stamps[tr.fn] {
			out[tr.table+"."+col] = tr.where
		}
	}
	return out
}

// jcUpPart — восходящая часть миграции. Нисходящая к живой базе не применяется,
// а её отмены выглядят для линейного чтения как снятие того, что было заведено
// парой строк выше.
func jcUpPart(src string) string {
	if loc := jcGooseDown.FindStringIndex(src); loc != nil {
		return src[:loc[0]]
	}
	return src
}

var jcGooseDown = regexp.MustCompile(`(?i)--\s*\+goose\s+down\b`)

type jcFuncDef struct {
	name string
	body string
	at   int
}

// jcFunctionBodies вырезает тела функций по долларовым кавычкам.
//
// Регулярным выражением это не делается: закрывающий разделитель обязан
// СОВПАДАТЬ с открывающим, а обратных ссылок в здешнем движке нет. Поиск
// парного разделителя строкой честнее приблизительного шаблона: приблизительный
// склеил бы две функции в одно тело и объявил бы закрытой ту, что блокировки не
// берёт.
func jcFunctionBodies(src string) []jcFuncDef {
	var out []jcFuncDef
	for _, m := range jcFuncHead.FindAllStringSubmatchIndex(src, -1) {
		name := jcTableName(src[m[2]:m[3]])
		rest := src[m[1]:]
		d := jcDollar.FindStringIndex(rest)
		if d == nil {
			continue
		}
		tag := rest[d[0]:d[1]]
		body := rest[d[1]:]
		if end := strings.Index(body, tag); end >= 0 {
			body = body[:end]
		}
		out = append(out, jcFuncDef{name: name, body: body, at: m[0]})
	}
	return out
}

// jcLockedStamps — колонки, которые тело функции штампует `nextval`-ом ПОСЛЕ
// транзакционной блокировки с ключом, не зависящим от строки.
//
// Позиция блокировки сравнивается со смещением КАЖДОГО присваивания: блокировка,
// взятая после выдачи номера, не упорядочивает ничего, и «в теле есть и то, и
// другое» закрытостью не является.
func jcLockedStamps(body string) map[string]bool {
	out := map[string]bool{}
	var locks []int
	for _, m := range jcXactLock.FindAllStringIndex(body, -1) {
		arg, ok := jcCallArg(body, m[1])
		if !ok {
			continue
		}
		// Ключ, вычисленный ИЗ СТРОКИ, упорядочивает лишь свою группу, а читатель
		// ведёт один курсор на всю таблицу.
		up := strings.ToUpper(arg)
		if strings.Contains(up, "NEW.") || strings.Contains(up, "OLD.") {
			continue
		}
		locks = append(locks, m[0])
	}
	if len(locks) == 0 {
		return out
	}
	for _, m := range jcStamp.FindAllStringSubmatchIndex(body, -1) {
		col := strings.ToLower(body[m[2]:m[3]])
		for _, at := range locks {
			if at < m[0] {
				out[col] = true
				break
			}
		}
	}
	return out
}

// jcCallArg — текст аргументов вызова, открывающая скобка которого стоит на
// `open-1`. Скобки считаются, поэтому вложенный вызов (`hashtext(...)`) не
// обрывает разбор на своей закрывающей.
func jcCallArg(src string, open int) (string, bool) {
	depth := 1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open:i], true
			}
		}
	}
	return "", false
}
