// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verdictenumeration_test.go — Г6 приёмки R7-1: НА ПУТИ ПРИНЯТИЯ РЕШЕНИЯ НЕТ
// ПЕРЕЧИСЛЕНИЯ.
//
// # Предмет
//
// Вопрос о доступе к ОДНОМУ объекту не вправе стоить размера облака. Чтение
// таблицы, не привязанное ни к предмету запроса, ни к чему-либо, что от него
// выведено, — это перечисление вселенной: ответ тот же, стоимость растёт с
// числом строк в системе, и заметно это только под нагрузкой, то есть в
// продакшне.
//
// Поведенческой пробы для этого мало: она зеленеет на фикстуре, не задевшей ось.
// Свойство — свойство ДЕРЕВА, и держать его обязан обход дерева.
//
// # ЧТО СЧИТАЕТСЯ ПРИВЯЗКОЙ (и почему именно это)
//
// Чтение считается ограниченным, если оно соединено равенством с ЯКОРЕМ:
//
//	· параметром запроса ($N) — предмет вопроса пришёл снаружи;
//	· именованным набором, чья мощность ограничена схемой либо самим запросом
//	  (цепь областей, говорящие, атомы плана, страница кандидатов);
//	· другим чтением, УЖЕ признанным ограниченным, — привязка транзитивна:
//	  выдача, найденная по ограниченной строке субъекта, ограничена вместе с ней.
//
// Замыкание считается до неподвижной точки, а не в один проход: порядок
// соединений в тексте произволен, и однопроходный разбор объявил бы находкой
// то, что привязано соединением ниже по тексту.
//
// # ОБЪЁМ И ЕГО ГРАНИЦА, НАЗВАННАЯ ЧЕСТНО
//
// Гейт разбирает Go по синтаксическому дереву и читает ИСПОЛНЯЕМУЮ часть
// строковых литералов пути вердикта: SQL внутри Go-комментария кодом не
// является, совпадение в SQL-комментарии находкой не является. Собранный из
// кусков на лету SQL и миграции не покрыты — у них своя единица разбора, и
// объявлять их покрытыми было бы обещанием, за которым ничего не стоит.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// verdictAnchors — именованные наборы, чья мощность ограничена не размером
// облака, а схемой либо самим запросом.
//
// Перечень объявлен здесь, потому что он и есть СОДЕРЖАНИЕ запрета: то, что в
// него внесено без основания, немедленно делает гейт зелёным на находке.
var verdictAnchors = map[string]string{
	"scope":        "цепь областей объекта: глубина ограничена проверкой схемы (1..4)",
	"speaker":      "за кого говорит вызывающий: он сам, его группы, подстановка",
	"speaker_pair": "то же парой колонок",
	"fact_atom":    "атомы плана вопроса: раскладка одного вопроса, не таблица",
	"candidate":    "страница перечисления: ограничена своим пределом",
}

type enumFinding struct {
	file, table, alias string
	line               int
}

type enumCensus struct {
	files, literals, reads, bounded int
	tables                          map[string]bool
}

func TestVerdictPathReadsNothingUnbounded(t *testing.T) {
	findings, c := collectUnboundedVerdictReads(t, filepath.Join(repoRoot(t), verdictGlueRoot))

	if c.files == 0 || c.literals == 0 || c.reads == 0 {
		t.Fatalf("предпосылка гейта не выполнена: файлов %d, литералов с SQL %d, чтений таблиц %d. "+
			"«Ноль находок» тогда означает «ноль прочитанного»", c.files, c.literals, c.reads)
	}

	names := make([]string, 0, len(c.tables))
	for n := range c.tables {
		names = append(names, n)
	}
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: файлов %d, литералов с SQL %d, чтений таблиц %d, "+
		"из них привязано %d, различных таблиц упомянуто %d, находок %d",
		c.files, c.literals, c.reads, c.bounded, len(names), len(findings))

	for _, f := range findings {
		t.Errorf("%s:%d: чтение %s (%s) ничем не привязано к предмету запроса\n"+
			"    Ни равенства с параметром, ни соединения с ограниченным набором, ни предела. "+
			"Ответ от этого не меняется, а стоимость растёт с числом строк в системе — заметно "+
			"это только под нагрузкой. Привяжи чтение к цепи областей, к говорящим либо к "+
			"параметру вопроса.", f.file, f.line, f.table, f.alias)
	}
}

func collectUnboundedVerdictReads(t *testing.T, dir string) ([]enumFinding, enumCensus) {
	t.Helper()
	c := enumCensus{tables: map[string]bool{}}
	var out []enumFinding
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("каталог пути вердикта не читается (%s): %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("файл %s: %v", name, err)
		}
		c.files++
		for _, lit := range sqlLiteralsOf(name, body) {
			if !strings.Contains(lit.sql, "kaname.") {
				continue
			}
			c.literals++
			f, cc := auditSQLForEnumeration(name, lit.line, stripSQLLineComments(lit.sql))
			out = append(out, f...)
			c.reads += cc.reads
			c.bounded += cc.bounded
			for n := range cc.tables {
				c.tables[n] = true
			}
		}
	}
	return out, c
}

// tableRead — одно чтение таблицы: где стоит и под каким псевдонимом.
type tableRead struct {
	table, alias string
	line         int
}

// auditSQLForEnumeration — привязка считается ЗАМЫКАНИЕМ до неподвижной точки.
func auditSQLForEnumeration(file string, baseLine int, sql string) ([]enumFinding, enumCensus) {
	c := enumCensus{tables: map[string]bool{}}
	reads := tableReadsOf(sql, baseLine)
	if len(reads) == 0 {
		return nil, c
	}
	// Якоря: имена ограниченных наборов плюс параметр запроса.
	bound := map[string]bool{}
	for name := range verdictAnchors {
		bound[name] = true
		// Псевдонимы ограниченных наборов тоже якоря: `JOIN scope sc` вводит sc.
		for _, a := range aliasesOfSet(sql, name) {
			bound[a] = true
		}
	}
	// Наборы, объявленные В ЭТОМ ЖЕ запросе, — тоже якоря, и это не послабление:
	// КАЖДОЕ чтение таблицы внутри них проверяется отдельно, этим же обходом.
	// Значит набор ограничен ровно тогда, когда ограничены его чтения, и
	// непривязанное чтение внутри него всё равно будет названо — своей строкой,
	// а не спрятано за именем набора.
	for _, name := range declaredSetsOf(sql) {
		bound[name] = true
		for _, a := range aliasesOfSet(sql, name) {
			bound[a] = true
		}
	}
	// Производная таблица вбок, НЕ ЧИТАЮЩАЯ НИ ОДНОЙ ТАБЛИЦЫ СХЕМЫ, — вычисление
	// на строку, а не перечисление: её строки берутся из внешней корреляции и
	// констант, поэтому их число ограничено внешней стороной by construction.
	//
	// ЭТО НЕ ПОСЛАБЛЕНИЕ, и вот почему. Соединение вбок, которое таблицу ЧИТАЕТ
	// (обход цепи областей: `LATERAL (SELECT … FROM kaname.resource_scope_edge
	// pe WHERE pe.object_type = s.s_type …) e`), в якоря НЕ попадает, а его
	// внутреннее чтение по-прежнему проверяется этим же обходом отдельной
	// строкой и привязывается собственным равенством. То есть правило добавляет
	// в якоря ровно то, что якорем и является.
	//
	// Заведено первым экземпляром новой формы (#758): разбор написания субъекта
	// на имя группы вынесен в соединение вбок, и чтение членств привязано к его
	// результату голой колонкой. Прежняя редакция гейта такую привязку не
	// видела — не потому, что её нет, а потому, что имени вычисленного набора
	// неоткуда было взяться.
	for _, a := range computedLateralAliasesOf(sql) {
		bound[a] = true
	}
	for _, r := range reads {
		c.tables[r.table] = true
	}
	c.reads = len(reads)

	// Замыкание: чтение привязано, если равенством связано с якорем либо с уже
	// привязанным чтением. Повторяем, пока множество растёт.
	for grew := true; grew; {
		grew = false
		for _, r := range reads {
			if bound[r.alias] {
				continue
			}
			if boundByEquality(sql, r.alias, bound) {
				bound[r.alias] = true
				grew = true
			}
		}
	}

	var out []enumFinding
	for _, r := range reads {
		if bound[r.alias] {
			c.bounded++
			continue
		}
		out = append(out, enumFinding{file: file, table: r.table, alias: r.alias, line: r.line})
	}
	return out, c
}

// computedLateralAliasesOf — псевдонимы соединений вбок, не читающих таблиц.
//
// Тело берётся по балансу скобок от `LATERAL (`; если в нём есть имя таблицы
// схемы, псевдоним НЕ возвращается — такое соединение вбок судится как обычное
// чтение.
func computedLateralAliasesOf(sql string) []string {
	const marker = "LATERAL ("
	var out []string
	for off := 0; ; {
		i := strings.Index(strings.ToUpper(sql[off:]), marker)
		if i < 0 {
			return out
		}
		open := off + i + len(marker) - 1
		depth, end := 0, -1
		for j := open; j < len(sql); j++ {
			switch sql[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			return out
		}
		off = end + 1
		if strings.Contains(sql[open:end], "kaname.") {
			continue
		}
		if a := aliasAfter(sql[end+1:]); a != "" {
			out = append(out, a)
		}
	}
}

// tableReadsOf — чтения таблиц схемы: имя и введённый псевдоним.
func tableReadsOf(sql string, baseLine int) []tableRead {
	const marker = "kaname."
	var out []tableRead
	for off := 0; ; {
		i := strings.Index(sql[off:], marker)
		if i < 0 {
			return out
		}
		at := off + i
		off = at + len(marker)
		// ЧТЕНИЕМ считается только то, что стоит за FROM или JOIN. Имя таблицы
		// в реестре («у этого типа метки лежат вот здесь») чтением не является
		// и находкой быть не может: там оно значение, а не запрос.
		if !precededByReadKeyword(sql, at) {
			continue
		}
		table, rest := takeIdentAt(sql[off:])
		if table == "" {
			continue
		}
		alias := aliasAfter(rest)
		if alias == "" {
			alias = table
		}
		out = append(out, tableRead{
			table: table, alias: alias,
			line: baseLine + strings.Count(sql[:at], "\n"),
		})
	}
}

// aliasAfter — псевдоним сразу за именем таблицы, если он есть.
func aliasAfter(rest string) string {
	i := skipSpace(rest, 0)
	word, _ := takeIdentAt(rest[i:])
	switch strings.ToUpper(word) {
	case "", "ON", "WHERE", "JOIN", "LEFT", "INNER", "CROSS", "UNION", "GROUP",
		"ORDER", "LIMIT", "SET", "VALUES", "AS", "USING", "AND", "OR", "SELECT", "FROM":
		return ""
	}
	return word
}

// aliasesOfSet — под какими псевдонимами в запрос входит именованный набор.
func aliasesOfSet(sql, name string) []string {
	var out []string
	for off := 0; ; {
		i := strings.Index(sql[off:], name)
		if i < 0 {
			return out
		}
		at := off + i
		off = at + len(name)
		if at > 0 && isIdentByte(sql[at-1]) {
			continue
		}
		if a := aliasAfter(sql[off:]); a != "" {
			out = append(out, a)
		}
	}
}

// boundByEquality — есть ли равенство, связывающее псевдоним с якорем.
//
// Читается по СТРОКЕ соединения: `bs.subject_type = sp.s_type`, `b.id = bs.binding_id`,
// `f.object_type = $2`. Обе стороны рассматриваются, потому что порядок в
// равенстве произволен.
func boundByEquality(sql, alias string, bound map[string]bool) bool {
	for _, line := range strings.Split(sql, "\n") {
		if !strings.Contains(line, alias+".") {
			continue
		}
		// Вхождение в ограниченный набор привязывает так же, как равенство:
		// `ON n.subject IN ('group:' || gm.group_id, …)` берёт члены НАЗВАННЫХ
		// групп, а не всех групп системы.
		if i := strings.Index(strings.ToUpper(line), " IN "); i >= 0 {
			for b := range bound {
				if strings.Contains(line[:i], b+".") {
					return true
				}
			}
		}
		if !strings.Contains(line, "=") {
			continue
		}
		for _, part := range strings.Split(line, "=") {
			p := strings.TrimSpace(part)
			if strings.Contains(p, "$") {
				// Параметр рядом с колонкой этого псевдонима на той же строке.
				if strings.Contains(line, alias+".") {
					return true
				}
			}
			for b := range bound {
				if strings.Contains(p, b+".") && strings.Contains(line, alias+".") {
					return true
				}
			}
		}
	}
	return false
}

// sqlLiteral — строковый литерал прод-кода вместе с его строкой в файле.
type sqlLiteral struct {
	sql  string
	line int
}

// sqlLiteralsOf — литералы, разобранные ПО СИНТАКСИЧЕСКОМУ ДЕРЕВУ Go.
//
// Не по тексту файла: SQL, стоящий в Go-комментарии, кодом не является, и
// читать его как код значило бы краснеть на объяснении запрета.
//
// СКЛЕЙКА РАЗБИРАЕТСЯ ЦЕЛИКОМ. Кусок запроса, собранный конкатенацией, сам по
// себе не несёт ни параметров, ни предикатов — они в соседних слагаемых, — и
// судить его отдельно значило бы объявлять находкой каждый второй фрагмент.
// Поэтому выражение сложения сводится в ОДИН псевдо-литерал, а слагаемые, не
// являющиеся литералами, подставляются как `$?`: их значение неизвестно, но
// известно, что там стоит ВЫЧИСЛЯЕМОЕ — то есть привязка, а не таблица.
//
// Граница названа честно: если слагаемое подставит имя таблицы, гейт увидит
// `$?` и промолчит. Именно поэтому реестр имён таблиц («у этого типа метки
// лежат вот здесь») закрыт своим гейтом словарей, а не этим.
func sqlLiteralsOf(name string, body []byte) []sqlLiteral {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, 0)
	if err != nil {
		return nil
	}
	// Литералы, поглощённые склейкой, отдельно не рассматриваются.
	consumed := map[ast.Node]bool{}
	var out []sqlLiteral
	ast.Inspect(file, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || be.Op != token.ADD {
			return true
		}
		text, any := flattenConcat(be, consumed)
		if any {
			out = append(out, sqlLiteral{sql: text, line: fset.Position(be.Pos()).Line})
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || consumed[lit] {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		out = append(out, sqlLiteral{sql: s, line: fset.Position(lit.Pos()).Line})
		return true
	})
	return out
}

// flattenConcat — свести выражение сложения строк в один текст.
func flattenConcat(e ast.Expr, consumed map[ast.Node]bool) (string, bool) {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "$?", false
		}
		l, okl := flattenConcat(v.X, consumed)
		r, okr := flattenConcat(v.Y, consumed)
		return l + r, okl || okr
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "$?", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "$?", false
		}
		consumed[v] = true
		return s, true
	default:
		// Не литерал: значение неизвестно, но известно, что оно ВЫЧИСЛЯЕМОЕ.
		return "$?", false
	}
}

// precededByReadKeyword — стоит ли имя таблицы в позиции ЧТЕНИЯ.
//
// Имя таблицы в реестре («у этого типа метки лежат вот здесь») чтением не
// является: там оно значение, а не запрос, и находкой быть не может.
func precededByReadKeyword(sql string, at int) bool {
	head := strings.ToUpper(strings.TrimRight(sql[:at], " \t\n"))
	// Запятая — тоже соединение, и она НЕ мелочь: чтение, приписанное через
	// запятую, ускользало бы от гейта молча, то есть ровно тем способом, каким
	// перечисление и возвращается. Найдено саморевью, а не прогоном: на дереве
	// такой формы сегодня нет, и «ноль находок» её отсутствия не доказывало.
	for _, kw := range []string{"FROM", "JOIN", ","} {
		if strings.HasSuffix(head, kw) {
			return true
		}
	}
	return false
}

// declaredSetsOf — имена наборов, объявленных в самом запросе (`имя(...) AS (`).
//
// Разбор по ФОРМЕ объявления, а не по перечню: перечень разошёлся бы с запросом
// молча, и первый же новый набор стал бы находкой без предмета.
func declaredSetsOf(sql string) []string {
	var out []string
	up := strings.ToUpper(sql)
	for off := 0; ; {
		i := strings.Index(up[off:], " AS (")
		if i < 0 {
			return out
		}
		at := off + i
		off = at + len(" AS (")
		head := strings.TrimRight(sql[:at], " \t\n")
		// Перед `AS (` может стоять список колонок — снимем его.
		if strings.HasSuffix(head, ")") {
			if d := strings.LastIndexByte(head, '('); d > 0 {
				head = strings.TrimRight(head[:d], " \t\n")
			}
		}
		j := len(head)
		for j > 0 && isIdentByte(head[j-1]) {
			j--
		}
		if name := head[j:]; name != "" {
			out = append(out, name)
		}
	}
}
