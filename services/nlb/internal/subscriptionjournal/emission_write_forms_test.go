// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// emission_write_forms_test.go — СТРОКУ ЖУРНАЛА ПИШУТ ТРЕМЯ ФОРМАМИ, И ПЕРЕПИСЬ
// ОБЯЗАНА НАЗЫВАТЬ ВСЕ ТРИ.
//
// # Предмет
//
// Соседние гейты (`TestEveryEmissionNamesTheKindByTheCanonicalConstant`,
// `TestEveryEmissionOfAStatefulKindBuildsTheSamePayload`) судят УЗЕЛ вызова
// `Outbox().Emit` — то есть ровно ОДНУ из трёх законных форм записи строки
// журнала в этом дереве. Две другие для них не существуют:
//
//	форма 1 — вызов на Go (`Outbox().Emit`)      — судится разбором Go;
//	форма 2 — ОПЕРАТОР SQL на Go (`tx.Exec`)     — не судился ничем;
//	форма 3 — ТРИГГЕР базы (`lb_status_recompute`) — не судился ничем.
//
// Перепись при этом молчала об этом молчании: она называла число найденных
// вызовов и не называла числа точек, которых не видела. То есть «двадцать»
// читалось как «все двадцать точек журнала», хотя означало «двадцать точек ОДНОЙ
// формы». Это тот же класс, что `testing.md` §«Гейт на класс» п.7:
// распознаватель, знающий не все законные написания предмета, не даёт ни
// красного, ни зелёного — он МОЛЧИТ, а записанное неизвестной ему формой не
// «разрешено», а НЕ ОСМОТРЕНО (#1568).
//
// # Почему это заведено ДО того, как класс стал дефектом
//
// Сегодня обе невидимые точки законны: у сборщика свободных адресов род
// `DELETED`, у которого состояния не бывает by construction, а согласие триггера
// с формой Go держит сквозная проба
// `TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo`. Законность эта,
// однако, ничем не удержана: следующий оператор на Go либо следующий триггер,
// пишущий род, отличный от снятия, не покраснил бы ни один гейт и не попал бы ни
// в одну перепись.
//
// # Граница разбора названа, а не умолчана
//
// Форму 2 гейт судит ПОЛНОСТЬЮ ТАМ, ГДЕ ИМЯ ТАБЛИЦЫ НАПИСАНО ЛИТЕРАЛОМ: узел
// строкового литерала Go комментария не содержит by construction, поэтому вид и
// род читаются из самого оператора. У той же формы есть разновидность, где имя
// таблицы ПОДСТАВЛЯЕТСЯ форматированием (`INSERT INTO %s`); её предмет разбором
// не разрешается вовсе, поэтому она идёт в перепись отдельной величиной и не
// роняет — граница названа у поля `unresolvable`. У nlb таких операторов ноль, у
// соседних владельцев журнала — восемь.
//
// Форму 3 разбором дерева судить НЕЛЬЗЯ, и это не лень: живое тело триггерной
// функции — последнее из череды переопределений, а прежние лежат в ПРИМЕНЁННЫХ
// миграциях, править которые нельзя (ban #5). Текстовая проверка по миграциям
// краснела бы на собственной истории. Живое тело знает только база — поэтому
// форму 3 судит проба, поднимающая схему
// (`TestEveryTriggerWritingTheJournalIsDeclared` ниже), а перепись формы 3 в
// коротком прогоне печатает «не осмотрено», а не ноль: ноль означал бы «искали и
// не нашли».

// journalStatementPoint — точка записи строки журнала ОПЕРАТОРОМ SQL на Go.
type journalStatementPoint struct {
	pos    string
	kind   string // литерал вида предмета; "" — значение параметризовано
	change string // литерал рода изменения; "" — значение параметризовано
}

// isTransport — оператор, не называющий НИ вида, НИ рода, точкой эмиссии не
// является: это перенос чужого решения (реализация порта `Emit`), а решение
// принято выше и уже осмотрено формой 1.
//
// Различение идёт ПО СУЩЕСТВУ, а не по месту. Исключение «вот в этом файле
// можно» было бы послаблением без срока: оно пережило бы свой предмет молча, а
// параметризованность значений читается из того же оператора и истекает вместе с
// ним.
func (p journalStatementPoint) isTransport() bool { return p.kind == "" && p.change == "" }

// TestEveryJournalWriteFormIsAccountedFor — ФОРМЫ ЗАПИСИ СЧИТАЮТСЯ РАЗДЕЛЬНО, И
// ОПЕРАТОР НА GO СУДИТСЯ.
//
// # Что утверждается
//
//  1. перепись печатает формы РАЗДЕЛЬНО — «вызовом N · оператором M · триггером
//     K», чтобы ноль по форме означал «искали и не нашли», а не «не искали»;
//  2. оператор SQL на Go, называющий вид или род, — ТОЧКА ЭМИССИИ, и род её
//     обязан быть снятием. Любой другой род — находка: такая точка идёт мимо
//     порта `Emit`, а значит мимо словаря констант (соседний гейт вида и рода) и
//     мимо общего строителя нагрузки (соседний гейт нагрузки). Вид объявлен
//     несущим состояние, поэтому одна частичная точка делает ложным ВЕСЬ вид.
//
// # Чего проверка НЕ утверждает
//
// Она не судит нагрузку оператора: собрана ли она общим строителем, из SQL не
// видно. Именно поэтому род, отличный от снятия, здесь ЗАПРЕЩЁН, а не проверен:
// у снятия состояния не бывает, и судить нечего.
func TestEveryJournalWriteFormIsAccountedFor(t *testing.T) {
	calls := inspectEmissions(t, func(emission) {})
	stmts := inspectJournalStatements(t)

	var points, transports []journalStatementPoint
	for _, p := range stmts.points {
		if p.isTransport() {
			transports = append(transports, p)
			continue
		}
		points = append(points, p)
	}

	t.Logf("перепись форм записи строки журнала %s:", Table)
	t.Logf("  форма 1 — вызовом на Go: точек %d (файлов Go осмотрено %d)",
		calls.emitsSeen, calls.filesRead)
	t.Logf("  форма 2 — оператором SQL на Go: точек %d, переносов %d "+
		"(файлов Go осмотрено %d, операторов найдено %d; операторов с ФОРМАТИРУЕМЫМ именем "+
		"таблицы %d — их предмет разбором не разрешается, см. поле unresolvable)",
		len(points), len(transports), stmts.filesRead, len(stmts.points), stmts.unresolvable)
	t.Logf("  форма 3 — триггером базы: НЕ ОСМОТРЕНО этой проверкой — живое тело функции " +
		"знает только база; судит TestEveryTriggerWritingTheJournalIsDeclared")
	for _, p := range points {
		t.Logf("    %s — вид %q, род %q", p.pos, p.kind, p.change)
	}
	for _, p := range transports {
		t.Logf("    %s — перенос (ни вида, ни рода не называет)", p.pos)
	}

	if calls.filesRead == 0 || stmts.filesRead == 0 {
		t.Fatal("не осмотрено ни одного файла — проверка беспредметна, а не пройдена")
	}
	if calls.emitsSeen == 0 {
		t.Fatal("форма 1 не найдена ни разу — предмета у переписи нет. Если эмиссия переехала, " +
			"проверка обязана покраснеть, а не молча одобрить любое дерево")
	}
	// Форма 2 существует в дереве: реализация порта `Emit` сама есть оператор SQL.
	// Ноль здесь означал бы, что распознаватель формы перестал её узнавать, — и
	// тогда молчание по ней ничего не стоит.
	if len(stmts.points) == 0 {
		t.Fatal("форма 2 не найдена ни разу, а она в дереве есть заведомо: реализация порта " +
			"`Emit` сама пишет строку оператором. Значит распознаватель формы её больше не " +
			"узнаёт, и его молчание ничего не утверждает")
	}

	for _, p := range points {
		if p.change == deletedChangeWord() {
			continue
		}
		t.Errorf("%s: строка журнала пишется ОПЕРАТОРОМ SQL на Go, вид %q, род %q.\n"+
			"Такая точка идёт мимо порта `Emit`, а значит мимо словаря констант и мимо общего "+
			"строителя нагрузки: ни один из соседних гейтов её не судит, и их перепись её не "+
			"видит. Род, отличный от снятия (%q), здесь запрещён — у снятия состояния не "+
			"бывает by construction, а у прочих родов вид объявлен несущим ПОЛНОЕ состояние, и "+
			"одна частичная точка делает ложным ВЕСЬ вид.\n"+
			"Исходов два: эмитить через порт `Emit` — либо, если оператор нужен именно здесь, "+
			"завести ему сквозную пробу согласия с формой Go, как у триггера, и назвать её "+
			"здесь", p.pos, p.kind, p.change, deletedChangeWord())
	}
}

// deletedChangeWord — слово рода «снятие» так, как оно лежит в колонке журнала.
//
// Берётся у СЛОВАРЯ журнала, а не выписывается: слово хранилища объявлено один
// раз, и второе его написание разошлось бы с первым молча — ровно тот класс,
// который стережёт соседний гейт словаря.
func deletedChangeWord() string {
	for word, change := range Journal().Mapping.Changes {
		if change.String() == "DELETED" {
			return word
		}
	}
	return ""
}

type journalStatementCensus struct {
	filesRead int
	points    []journalStatementPoint
	// unresolvable — операторы вставки, чьё ИМЯ ТАБЛИЦЫ подставляется
	// форматированием (`INSERT INTO %s`). Разрешить его разбором нельзя, поэтому
	// про такой оператор не известно даже того, пишет ли он журнал.
	//
	// Величина ПЕЧАТАЕТСЯ, но НЕ РОНЯЕТ, и это граница, а не недосмотр: ронять на
	// всяком форматируемом операторе значило бы краснеть на верном коде, который
	// к журналу отношения не имеет, — а гейт, краснеющий на верном коде,
	// отключают первым. У nlb таких операторов сегодня НОЛЬ, и ноль этот означает
	// «искали и не нашли»: у соседних владельцев журнала форма живая (реестр
	// пишет так семь операторов, машины — один), то есть она законна в этом
	// дереве и появиться здесь может.
	//
	// Класс сработал на самом замере, которым эта проверка обосновывалась:
	// предикат по литеральному имени таблицы дал у машин НОЛЬ писателей журнала
	// при живом писателе. Распознаватель, знающий не все законные написания
	// предмета, не даёт ни красного, ни зелёного — он молчит, и молчал он о
	// целом владельце.
	unresolvable int
}

// inspectJournalStatements обходит НЕ-ТЕСТОВОЕ дерево сервиса и находит каждый
// строковый литерал Go, содержащий вставку в таблицу журнала.
//
// Обход идёт по ВСЕМУ сервису, а не по каталогу use-case: оператор — форма
// низкоуровневая, и живёт она как раз вне `apps` (реализация порта — в `repo`,
// сборщик свободных адресов — в `jobs`). Сузить обход до `apps` значило бы
// завести слепую зону ровно там, где форма и встречается.
//
// Комментарий сюда попасть не может by construction: узел `*ast.BasicLit` несёт
// значение литерала, а комментарии в дерево разбора Go отдельной ветвью не
// входят. Поэтому объяснения этого файла — включая приведённые в них операторы —
// проверку не краснят.
func inspectJournalStatements(t *testing.T) journalStatementCensus {
	t.Helper()

	root := filepath.Join("..", "..")
	res := journalStatementCensus{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("%s не разобран: %v — гейт судит по узлам, и неосмотренный файл его "+
				"молчания не оправдывает", path, parseErr)
		}
		res.filesRead++
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, uErr := strconv.Unquote(lit.Value)
			if uErr != nil {
				return true
			}
			res.unresolvable += countUnresolvableInserts(text)
			for _, ins := range insertsInto(text, Table) {
				kind, change := ins.literalOf(Journal().Storage.KindColumn),
					ins.literalOf(Journal().Storage.ChangeColumn)
				res.points = append(res.points, journalStatementPoint{
					pos:    fset.Position(lit.Pos()).String(),
					kind:   kind,
					change: change,
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s не завершён: %v", root, err)
	}
	return res
}

// sqlInsert — разобранная вставка: колонки и соответствующие им значения.
type sqlInsert struct {
	cols   []string
	values []string
}

// literalOf возвращает СТРОКОВЫЙ ЛИТЕРАЛ, стоящий против названной колонки, либо
// "" когда значение параметризовано (`$1`) или колонки в операторе нет.
func (s sqlInsert) literalOf(col string) string {
	for i, c := range s.cols {
		if !strings.EqualFold(c, col) || i >= len(s.values) {
			continue
		}
		v := strings.TrimSpace(s.values[i])
		if len(v) >= 2 && v[0] == '\'' {
			if end := strings.IndexByte(v[1:], '\''); end >= 0 {
				return v[1 : 1+end]
			}
		}
		return ""
	}
	return ""
}

// insertsInto находит в тексте оператора все вставки в названную таблицу и
// разбирает их перечень колонок и перечень значений.
func insertsInto(text, table string) []sqlInsert {
	var out []sqlInsert
	low := strings.ToLower(text)
	needle := "insert into " + strings.ToLower(table)
	for at := 0; ; {
		i := strings.Index(low[at:], needle)
		if i < 0 {
			return out
		}
		pos := at + i + len(needle)
		at = pos
		cols, after := parenList(text, pos)
		if cols == nil {
			continue
		}
		lowAfter := strings.ToLower(text[after:])
		v := strings.Index(lowAfter, "values")
		if v < 0 {
			// `INSERT ... SELECT` — значения приходят выборкой, литералов у
			// колонок нет. Точка эмиссии это или перенос, из оператора не видно;
			// такой формы в дереве сегодня нет, и объявлять её разобранной было
			// бы ложью. Пропуск НАЗВАН переписью: колонки есть, значений нет.
			out = append(out, sqlInsert{cols: cols})
			continue
		}
		vals, next := parenList(text, after+v+len("values"))
		out = append(out, sqlInsert{cols: cols, values: vals})
		if next > at {
			at = next
		}
	}
}

// parenList читает ближайший скобочный перечень, начиная с pos, и делит его по
// запятым ВЕРХНЕГО уровня (вложенные скобки и строковые литералы не режутся).
// Возвращает элементы и позицию сразу за закрывающей скобкой.
func parenList(text string, pos int) ([]string, int) {
	open := strings.IndexByte(text[pos:], '(')
	if open < 0 {
		return nil, pos
	}
	i := pos + open + 1
	depth, inStr, start := 1, false, i
	var out []string
	for ; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\'':
			inStr = !inStr
		case inStr:
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return append(out, strings.TrimSpace(text[start:i])), i + 1
			}
		case c == ',' && depth == 1:
			out = append(out, strings.TrimSpace(text[start:i]))
			start = i + 1
		}
	}
	return nil, pos
}

// logFormsNotJudgedHere — строка переписи, которую печатает КАЖДЫЙ гейт разбора
// Go рядом со своим числом найденных вызовов.
//
// # Зачем она
//
// Число найденных вызовов, стоящее в переписи ОДНО, читается как «столько точек у
// журнала». Означает оно «столько точек ОДНОЙ формы»: две другие формы разбор Go
// не видит by construction, и его молчание по ним неотличимо от «их нет».
// Величины теперь ДВЕ, и вторая названа: сколько точек пишут журнал иначе и чем
// они судятся.
//
// # Почему производитель один
//
// Строка собирается здесь, а не пишется в каждом гейте: два места об одном
// предмете разошлись бы молча — и разошлись бы именно там, где расхождение не
// видно, потому что оба печатают правдоподобное число.
func logFormsNotJudgedHere(t *testing.T) {
	t.Helper()
	stmts := inspectJournalStatements(t)
	points := 0
	for _, p := range stmts.points {
		if !p.isTransport() {
			points++
		}
	}
	t.Logf("  этим разбором НЕ судятся ещё две формы записи строки журнала: "+
		"оператором SQL на Go — точек %d (судит TestEveryJournalWriteFormIsAccountedFor) · "+
		"триггером базы — судит TestEveryTriggerWritingTheJournalIsDeclared по ЖИВОЙ схеме "+
		"(разбор текста миграций тут невозможен: прежние переопределения лежат в применённых "+
		"миграциях, и проверка краснела бы на собственной истории)", points)
}

// countUnresolvableInserts считает вставки, чьё имя таблицы подставляется
// форматированием: разрешить их предмет разбором нельзя.
//
// Величина идёт в перепись и не роняет — обоснование у поля `unresolvable`.
func countUnresolvableInserts(text string) int {
	const marker = "insert into "
	low, n := strings.ToLower(text), 0
	for at := 0; ; {
		i := strings.Index(low[at:], marker)
		if i < 0 {
			return n
		}
		at += i + len(marker)
		rest := strings.TrimLeft(low[at:], " \t\n\r")
		if strings.HasPrefix(rest, "%s") || strings.HasPrefix(rest, "%q") {
			n++
		}
	}
}
