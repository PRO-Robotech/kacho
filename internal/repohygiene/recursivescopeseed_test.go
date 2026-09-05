// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// recursivescopeseed_test.go — гейт против рекурсивной цепи, раскрученной на
// НАБОРЕ вместо предмета запроса.
//
// # Предмет
//
// Запрос отдаёт страницу, но берёт в работу весь тип: кандидаты выбираются без
// предела, рекурсивная цепь областей строится для каждого из них, а предел
// применяется последним действием — то есть режет длину ответа, но не работу.
// Ответ при этом не меняется, а стоимость растёт с числом объектов системы. Это
// та же форма, которую норма запрещает на пути видимости: «страница → проверка
// страницы, НИКОГДА не перечисли вселенную → отфильтруй».
//
// У класса ДВЕ стороны, и вторая переживает починку первой:
//
//  1. НЕОГРАНИЧЕННЫЙ ЗАХОД. Нерекурсивная ветвь читает набор без предела, и цепь
//     раскручивается на всём наборе.
//  2. НЕОГРАНИЧЕННОЕ ЧТЕНИЕ ТАБЛИЦЫ ВНУТРИ РЕКУРСИИ. Заход ограничен, но
//     рекурсивная ветвь достаёт таблицу обычным соединением. У вопроса про ОДИН
//     объект внешняя сторона — одна строка, и планировщик ходит указателем; на
//     целой странице он вправе прочитать таблицу целиком, и стоимость снова
//     становится стоимостью набора. Это измерено, а не предположено: после
//     починки первой стороны страница из 50 объектов всё ещё читала все рёбра
//     типа, и кривая осталась растущей.
//
// # Почему гейт, а не разовая правка
//
// Свойство не видно в диффе: предел и раскрутка цепи стоят в разных местах
// запроса, а «здесь есть LIMIT» читается как «страница ограничена». Гейт
// формулирует требование локально — рекурсивная ветвь, посеянная НАБОРОМ,
// обязана нести предел на источнике набора и доставать таблицы соединением вбок
// с пределом, — поэтому следующее такое место краснеет в момент появления.
//
// # Объём и его граница, названная честно
//
// Гейт читает строковые литералы прод-кода ПУТИ ЗАПРОСА — `services/`, `pkg/`,
// `gateway/` — и внутри них исполняемую часть SQL, из которой удалены
// комментарии. Разбор идёт по синтаксическому дереву Go, а не по тексту файла:
// иначе SQL в комментарии Go читался бы как код.
//
// Чего гейт НЕ покрывает, и почему это сказано здесь, а не подразумевается:
//
//   - `tools/` — приборы замера. Они не лежат ни на одном пути запроса (ничто
//     из продукта их не импортирует, см. `services/iam/tools/authzformbench/doc.go`), их
//     стоимость есть предмет отчёта, а не договора с арендатором. Прибор
//     воспроизводит форму продукта своей схемой, у которой нет ограничения на
//     число рёбер объекта, — значит и предел внутри соединения вбок был бы там
//     не доказуемо-неусекающим, а просто числом. Выдавать такой предел за ту же
//     защиту значило бы поставить форму без содержания;
//   - SQL, собранный из кусков на лету, и миграции: у них своя единица разбора,
//     и объявлять их покрытыми было бы обещанием, за которым ничего не стоит.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// recursiveScopeRoots — путь запроса. Перечень объявлен здесь, потому что он и
// есть ОБЪЁМ гейта; печатается в переписи вместе с числом прочитанных файлов.
var recursiveScopeRoots = []string{"services", "pkg", "gateway"}

// TestRecursiveScopeChainCostsTheRequestNotTheSet — свойство дерева.
func TestRecursiveScopeChainCostsTheRequestNotTheSet(t *testing.T) {
	findings, c := collectUnboundedRecursiveScopeSeeds(t, repoRoot(t))

	// Проверка СВОЕЙ предпосылки. Запрет обоснован тем, что в дереве есть
	// рекурсивные запросы; если разбор перестанет их узнавать, «ноль находок»
	// станет означать «ноль прочитанного», и гейт будет зелен навсегда.
	if c.literals == 0 || c.recursive == 0 {
		t.Fatalf("предпосылка гейта не выполнена: литералов с рекурсивным запросом %d, "+
			"рекурсивных выражений %d. Либо разбор перестал их узнавать, либо дерево их "+
			"больше не несёт — в обоих случаях вердикт «чисто» ничего не значит", c.literals, c.recursive)
	}

	for _, f := range findings {
		t.Errorf("%s:%d: выражение %q — %s", f.file, f.line, f.cte, f.why)
	}

	t.Logf("перепись: корни %v; прочитано файлов %d; литералов с рекурсивным запросом %d; "+
		"выражений всего %d, из них рекурсивных %d, посеянных НАБОРОМ %d; находок %d",
		recursiveScopeRoots, c.files, c.literals, c.ctes, c.recursive, c.setSeeded, len(findings))
}

// scopeSeedFinding — одно место, названное координатой.
type scopeSeedFinding struct {
	file string
	line int
	cte  string
	why  string
}

// scopeSeedCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type scopeSeedCensus struct {
	files     int
	literals  int
	ctes      int
	recursive int
	setSeeded int
}

// collectUnboundedRecursiveScopeSeeds — сам обход. Принимает корень дерева,
// чтобы инъекция могла прогнать его по подготовленному дереву.
func collectUnboundedRecursiveScopeSeeds(t *testing.T, root string) ([]scopeSeedFinding, scopeSeedCensus) {
	t.Helper()
	var (
		findings []scopeSeedFinding
		c        scopeSeedCensus
	)
	want := func(rel string) bool {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return false
		}
		// Прибор замера предметом не является — это сказано в шапке гейта. До
		// линии выноса iam оговорка держалась РАСКЛАДКОЙ (приборы лежали в
		// корневом `tools/`, обход шёл по трём другим корням); теперь она
		// держится признаком — [isInstrumentPath].
		return !isInstrumentPath(rel)
	}
	for _, sub := range recursiveScopeRoots {
		dir := filepath.Join(root, sub)
		if err := rootedWalk(dir, want, func(abs string, body []byte) error {
			c.files++
			fs, sub := auditGoFileForRecursiveSQL(root, abs, body)
			c.literals += sub.literals
			c.ctes += sub.ctes
			c.recursive += sub.recursive
			c.setSeeded += sub.setSeeded
			findings = append(findings, fs...)
			return nil
		}); err != nil {
			t.Fatalf("обход %s: %v", dir, err)
		}
	}
	return findings, c
}

// auditGoFileForRecursiveSQL достаёт из файла строковые литералы с рекурсивным
// запросом и судит каждый.
func auditGoFileForRecursiveSQL(root, abs string, body []byte) ([]scopeSeedFinding, scopeSeedCensus) {
	var (
		findings []scopeSeedFinding
		c        scopeSeedCensus
	)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, abs, body, parser.SkipObjectResolution)
	if err != nil {
		// Синтаксис ловит сборка; гейт о нём не судит и молча его не засчитывает —
		// нечитаемый файл не попадает и в перепись литералов.
		return nil, c
	}
	rel, relErr := filepath.Rel(root, abs)
	if relErr != nil {
		rel = abs
	}
	rel = filepath.ToSlash(rel)

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		sql, uerr := strconv.Unquote(lit.Value)
		if uerr != nil || !strings.Contains(sql, "WITH RECURSIVE") {
			return true
		}
		c.literals++
		startLine := fset.Position(lit.Pos()).Line
		fs, sub := auditRecursiveSQL(sql)
		c.ctes += sub.ctes
		c.recursive += sub.recursive
		c.setSeeded += sub.setSeeded
		for _, f := range fs {
			f.file, f.line = rel, startLine+f.line
			findings = append(findings, f)
		}
		return true
	})
	return findings, c
}

// auditRecursiveSQL разбирает ОДИН литерал и судит его выражения.
//
// Строка находки — СМЕЩЕНИЕ от начала литерала: координату собирает вызывающий,
// который один знает, где литерал начался.
func auditRecursiveSQL(sql string) ([]scopeSeedFinding, scopeSeedCensus) {
	var (
		findings []scopeSeedFinding
		c        scopeSeedCensus
	)
	code := stripSQLLineComments(sql)
	idx := strings.Index(code, "WITH RECURSIVE")
	if idx < 0 {
		return nil, c
	}
	ctes := parseCTEList(code[idx+len("WITH RECURSIVE"):])
	c.ctes = len(ctes)
	byName := make(map[string]sqlCTE, len(ctes))
	names := make(map[string]bool, len(ctes))
	for _, e := range ctes {
		byName[e.name] = e
		names[e.name] = true
	}
	for _, e := range ctes {
		if !mentionsWord(e.body, e.name) {
			continue // не рекурсивное: собственного имени в теле нет
		}
		c.recursive++
		seed, step := splitAtFirstTopLevelUnion(e.body)
		seedTable, _ := readsTableOutsideBoundedLateral(seed, names)
		seedSets := referencedCTEs(seed, byName)
		if !seedTable && len(seedSets) == 0 {
			// Заход — одна строка из доводов запроса. Раскручивать нечего, и
			// требовать предела не от чего: законный близнец, а не послабление.
			continue
		}
		c.setSeeded++
		line := offsetLine(code, idx+strings.Index(code[idx:], e.name))

		if seedTable && !boundsOutput(seed) {
			findings = append(findings, scopeSeedFinding{line: line, cte: e.name,
				why: "заход читает таблицу БЕЗ предела: цепь раскрутится на всём наборе, а предел " +
					"в конце запроса ограничит длину ответа, но не работу. Предел обязан стоять на " +
					"источнике кандидатов — до раскрутки цепи, а не после"})
		}
		for _, ref := range seedSets {
			refTable, _ := readsTableOutsideBoundedLateral(ref.body, names)
			if refTable && !boundsOutput(ref.body) {
				findings = append(findings, scopeSeedFinding{line: line, cte: e.name,
					why: "заход посеян набором " + ref.name + ", а тот читает таблицу БЕЗ предела: " +
						"цепь раскрутится на всём наборе, и предел в конце ограничит длину ответа, " +
						"но не работу. Предел обязан стоять на источнике кандидатов, до раскрутки"})
			}
		}
		if tbl, name := readsTableOutsideBoundedLateral(step, names); tbl {
			findings = append(findings, scopeSeedFinding{line: line, cte: e.name,
				why: "рекурсивная ветвь достаёт таблицу " + name + " обычным соединением. На заходе " +
					"из ОДНОЙ строки это безразлично, а на целой странице планировщик вправе " +
					"прочитать таблицу целиком — и стоимость страницы снова станет стоимостью " +
					"набора. Доступ к таблице обязан идти соединением вбок с пределом"})
		}
	}
	return findings, c
}

// sqlCTE — одно именованное выражение запроса.
type sqlCTE struct {
	name string
	body string
}

// parseCTEList разбирает список выражений после WITH RECURSIVE до начала
// основного запроса.
func parseCTEList(s string) []sqlCTE {
	var out []sqlCTE
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		name, rest := takeIdentAt(s)
		if name == "" {
			return out
		}
		rest = strings.TrimLeft(rest, " \t\r\n")
		if strings.HasPrefix(rest, "(") { // список колонок
			_, after, ok := takeParens(rest)
			if !ok {
				return out
			}
			rest = strings.TrimLeft(after, " \t\r\n")
		}
		if !strings.HasPrefix(rest, "AS") {
			return out
		}
		rest = rest[len("AS"):]
		// Между AS и телом законно стоит MATERIALIZED / NOT MATERIALIZED.
		for {
			rest = strings.TrimLeft(rest, " \t\r\n")
			kw, after := takeIdentAt(rest)
			if kw != "MATERIALIZED" && kw != "NOT" {
				break
			}
			rest = after
		}
		body, after, ok := takeParens(rest)
		if !ok {
			return out
		}
		out = append(out, sqlCTE{name: name, body: body})
		rest = strings.TrimLeft(after, " \t\r\n")
		if !strings.HasPrefix(rest, ",") {
			return out
		}
		s = rest[1:]
	}
}

// takeIdentAt снимает идентификатор, стоящий РОВНО в начале строки.
func takeIdentAt(s string) (string, string) {
	i := 0
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '.' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// takeParens снимает сбалансированную скобочную группу с начала строки.
func takeParens(s string) (body, rest string, ok bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(s, "(") {
		return "", s, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
	}
	return "", s, false
}

// splitAtFirstTopLevelUnion делит тело на нерекурсивную и рекурсивную ветви.
func splitAtFirstTopLevelUnion(body string) (seed, step string) {
	depth := 0
	for i := 0; i+5 <= len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 || body[i:i+5] != "UNION" {
			continue
		}
		if i > 0 && isIdentByte(body[i-1]) {
			continue
		}
		return body[:i], body[i+5:]
	}
	return body, ""
}

// referencedCTEs — какие из известных выражений упоминает эта ветвь.
func referencedCTEs(branch string, byName map[string]sqlCTE) []sqlCTE {
	var out []sqlCTE
	for n, e := range byName {
		if mentionsWord(branch, n) {
			out = append(out, e)
		}
	}
	return out
}

// readsTableOutsideBoundedLateral — достаёт ли ветвь НАСТОЯЩУЮ таблицу, минуя
// соединение вбок с пределом.
//
// «Настоящая» определяется структурно, а не по имени схемы: за FROM/JOIN стоит
// идентификатор, который не есть ни выражение этого же запроса, ни вызов
// функции. Предикат по префиксу имени молча выдавал бы за чистые все запросы,
// чьи таблицы названы иначе, — то есть «ноль находок» означало бы «ноль
// узнанного».
//
// Соединение вбок, несущее СВОЙ предел, из рассмотрения выводится: доступ через
// него ограничен строкой внешней стороны и потому не растёт с набором.
// Соединение вбок БЕЗ предела планировщик волен развернуть в обычное — такое
// остаётся под вопросом.
func readsTableOutsideBoundedLateral(branch string, cteNames map[string]bool) (bool, string) {
	i := 0
	for {
		w, after := scanNextWord(branch, i)
		if w == "" {
			return false, ""
		}
		i = after
		if up := strings.ToUpper(w); up != "FROM" && up != "JOIN" {
			continue
		}
		j := skipSpace(branch, i)
		if j >= len(branch) {
			return false, ""
		}
		if branch[j] == '(' { // подзапрос — читаем его теми же правилами
			i = j + 1
			continue
		}
		w2, _ := takeIdentAt(branch[j:])
		if w2 == "" {
			continue
		}
		after2 := j + len(w2)
		if strings.EqualFold(w2, "LATERAL") {
			body, rest, ok := takeParens(branch[after2:])
			if ok && boundsOutput(body) {
				k := skipSpace(branch, len(branch)-len(rest))
				_, alias := takeIdentAt(branch[k:]) // псевдоним соединения
				i = k + len(alias)
				continue
			}
			i = after2
			continue
		}
		if cteNames[w2] {
			i = after2
			continue
		}
		if k := skipSpace(branch, after2); k < len(branch) && branch[k] == '(' {
			i = after2 // вызов функции (unnest, generate_series), а не таблица
			continue
		}
		return true, w2
	}
}

// scanNextWord находит следующий идентификатор начиная с i.
func scanNextWord(s string, i int) (string, int) {
	for i < len(s) && !isIdentByte(s[i]) {
		i++
	}
	if i >= len(s) {
		return "", i
	}
	w, _ := takeIdentAt(s[i:])
	return w, i + len(w)
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	return i
}

// mentionsWord — есть ли слово целиком (не часть идентификатора).
func mentionsWord(s, word string) bool {
	_, ok := findWord(s, word)
	return ok
}

// boundsOutput — ограничивает ли предел ВЫДАЧУ этой ветви.
//
// Только предел на своём уровне скобок. Предел во вложенном подзапросе
// ограничивает его, а не ветвь: принять его за границу значило бы объявить
// ограниченным набор, который таковым не стал, — форма без содержания ровно в
// том месте, ради которого гейт написан.
func boundsOutput(s string) bool {
	i, ok := findWord(s, "LIMIT")
	for ok {
		if parenDepthAt(s, i) == 0 {
			return true
		}
		j, more := findWord(s[i+1:], "LIMIT")
		i, ok = i+1+j, more
	}
	return false
}

// parenDepthAt — глубина скобок в точке.
func parenDepthAt(s string, off int) int {
	depth := 0
	for i := 0; i < off && i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth
}

// findWord — позиция слова целиком.
func findWord(s, word string) (int, bool) {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return 0, false
		}
		j += i
		before := j == 0 || !isIdentByte(s[j-1])
		k := j + len(word)
		after := k >= len(s) || !isIdentByte(s[k])
		if before && after {
			return j, true
		}
		i = j + 1
	}
}

// offsetLine — сколько переводов строки до смещения.
func offsetLine(s string, off int) int {
	if off < 0 || off > len(s) {
		return 0
	}
	return strings.Count(s[:off], "\n")
}

// stripSQLLineComments убирает комментарии SQL.
//
// Иначе гейт читал бы объяснение защиты как саму защиту: слово LIMIT в
// комментарии, разбирающем этот же класс, сделало бы запрет зелёным на снятом
// пределе — ровно тот класс, который гейт и ловит.
func stripSQLLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
