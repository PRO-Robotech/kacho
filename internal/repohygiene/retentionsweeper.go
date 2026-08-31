// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retentionsweeper.go — разбор УБОРЩИКОВ ПО СРОКУ для гейта «объявленный
// уборщик обязан иметь прод-вызывающего» (задача #1292, приёмка
// `services/iam/docs/engineering/acceptance/retention-sweep-has-a-caller.md`,
// сценарии RET-SWP-10…12).
//
// # Предмет
//
// Уборщик, у которого нет вызывающего, — это не «мёртвый код»: это таблица,
// растущая без ограничения, и восемь мест дерева, утверждающих в настоящем
// времени, что сборщик работает. Замер, ради которого гейт написан: в iam было
// объявлено ДВА уборщика по сроку, прод-вызывающих — НОЛЬ у обоих, и у одного
// из них не было даже пробы.
//
// Обзор диффа этого не различает: объявление уборщика выглядит одинаково при
// живом вызывающем и без него, а свойство «вызывающий существует» есть свойство
// ДЕРЕВА. Держать его может только обход дерева.
//
// # Вынесено в НЕ-тестовый файл намеренно
//
// Инъекционная проба зовёт ТОТ ЖЕ разбор, а не свою копию: копия разошлась бы с
// оригиналом молча и доказывала бы способность упасть у кода, который не
// исполняется.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **уборщик, чей оператор СОБРАН ИЗ КУСКОВ** — разбор требует, чтобы
//     удаление и сравнение со временем стояли в ОДНОМ литерале. Склейка
//     литералов функции была написана и снята: она давала два ложных
//     срабатывания на дереве (удаление в одном литерале, слово с `_at` — в
//     соседнем сообщении об ошибке), а инструмент, у которого пятая часть
//     находок ложна, перестают читать. Многострочный оператор при этом виден
//     целиком — сырой литерал есть ОДИН узел, и построчный предикат его бы
//     разорвал (RET-SWP-11). Появление собранного оператора видно по счётчику
//     `Deletes` переписи: удаление признано, уборщиком не стало.
//
//  2. **оператор, собранный В РАНТАЙМЕ** — построителем запросов либо из
//     значения, вычисленного по ходу дела. Литерала в теле функции тогда нет, и
//     собрать текст разбором нельзя by construction.
//
//     Здесь стояло шире — «оператор, приехавший ИЗ ПЕРЕМЕННОЙ», с оговоркой
//     «форм такого рода в дереве нет». Оговорка пережила свой предмет: оператор,
//     объявленный ПАКЕТНОЙ строковой величиной и позванный по имени, — форма
//     живая и обычная, и записаны так были ДВА уборщика дерева
//     (`Store.PurgeExpiredDPoPProofs` шлюза, `TargetDrainRunner.drainOnce`
//     nlb). Оба имели прод-вызывающего, то есть находками не были, — они были
//     НЕВИДИМЫ: ни красного, ни зелёного, молчание. Нашлось это не переписью, а
//     тем, что один из них стоял в ведомости и её запись объявила себя
//     потерявшей предмет с неверной причиной («уборщика в дереве нет вовсе»).
//
//     Поэтому имя пакетного значения ТЕПЕРЬ РАЗБИРАЕТСЯ (см. `packageStrings`),
//     включая склейку `+` из литералов и таких же имён. Цена расширения
//     измерена, а не предположена: осмотренных уборщиков 7 → 9, находок 0 → 0.
//
//     2а. **пакетное значение из СОСЕДНЕГО файла того же пакета** — разбор идёт по
//     одному файлу и чужого файла не видит. Остаток назван, а не спрятан: обе
//     формы дерева объявлены в том же файле, что и уборщик, и появление
//     разнесённой видно по паре счётчиков переписи — `NamedValues` (пакетных
//     строк прочитано) против `Named` (из них признаны уборщиками). Полоса, у
//     которой первое растёт, а второе стоит, и есть эта слепая зона.
//
//  3. **вызов косвенный** — метод, положенный в переменную и вызванный оттуда.
//     Разбор судит вызов по месту, а не по потоку значений.
//
//  4. **однофамилец в ЧУЖОМ каталоге**: вызывающий засчитывается только из
//     СВОЕГО каталога либо из каталога, который его ИМПОРТИРУЕТ. Имя `Reap` в
//     этом дереве носят два разных типа, и без этой границы вызывающий шлюза
//     покрывал бы уборщика iam. Однофамилец внутри ОДНОГО пакета границей не
//     отсекается — это остаток, и он назван.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// RetentionSweeper — функция, чьё тело удаляет строки со сравнением
// колонки-времени.
type RetentionSweeper struct {
	// File — координата объявления.
	File string
	Line int
	// Recv — имя типа получателя («» у обычной функции).
	Recv string
	// Name — имя функции.
	Name string
	// Dir — каталог дерева, в котором она объявлена.
	Dir string
	// SQL — обрывок оператора, по которому она опознана. В отказе он и
	// печатается: находка без предмета неотличима от промаха разбора.
	SQL string
}

// Qualified — «Тип.Метод» либо «Функция», для текста отказа.
func (s RetentionSweeper) Qualified() string {
	if s.Recv == "" {
		return s.Name
	}
	return s.Recv + "." + s.Name
}

// RetentionSweeperCensus — объём осмотренного одним файлом.
type RetentionSweeperCensus struct {
	// Functions — функций осмотрено.
	Functions int
	// Literals — строковых литералов прочитано.
	Literals int
	// Deletes — из них назвали удаление строк (без учёта сравнения со временем).
	Deletes int
	// Sweepers — из них признаны уборщиками ПО СРОКУ.
	Sweepers int
	// NamedValues — пакетных строковых значений файла прочитано.
	//
	// Объём ВТОРОЙ полосы разбора. Печатается рядом с `Named`, потому что одно
	// число не различает «полоса пуста» и «полоса не читалась».
	NamedValues int
	// Named — из уборщиков признаны по ИМЕНИ пакетного значения, а не по
	// литералу в теле.
	Named int
}

// deleteRe — оператор удаляет строки.
var deleteRe = regexp.MustCompile(`(?is)\bdelete\b.*\bfrom\b`)

// timeColumnRe — операнд сравнения называет колонку-время либо часы базы.
//
// Признак закрыт намеренно: «колонка, чьё имя оканчивается на `_at` или
// `_before`» плюс имена часов базы. Корзины «прочее» нет — колонка, которой
// здесь не назвали, есть незакрытый вопрос, а не пятый способ назвать время.
var timeColumnRe = regexp.MustCompile(`(?is)(\b\w*(_at|_before)\b|\bnow\s*\(\s*\)|\bcurrent_timestamp\b)`)

// comparisonRe — в операторе есть сравнение.
var comparisonRe = regexp.MustCompile(`(<=|>=|<|>)`)

// IsTimedDelete отвечает, удаляет ли оператор строки по сравнению со временем.
func IsTimedDelete(sql string) bool {
	if !deleteRe.MatchString(sql) {
		return false
	}
	return comparisonRe.MatchString(sql) && timeColumnRe.MatchString(sql)
}

// ScanRetentionSweepers разбирает один файл и возвращает объявленных в нём
// уборщиков по сроку.
//
// Литерал читается ЦЕЛИКОМ, а не построчно: два уборщика дерева объявляют
// оператор в несколько строк, и построчный предикат увидел бы `DELETE FROM t` и
// `WHERE expires_at <= now()` порознь, не признав ни одну строку уборкой
// (RET-SWP-11).
//
// # Две законные формы записи оператора, а не одна
//
// Уборщик называет свой оператор либо ЛИТЕРАЛОМ в теле, либо ИМЕНЕМ пакетного
// строкового значения того же файла. Обе формы в этом дереве живые, обе
// разбираются, и порядок между ними — литерал вперёд: по нему берётся текст для
// отказа, а находка без предмета неотличима от промаха разбора.
func ScanRetentionSweepers(path, dir string, src []byte) ([]RetentionSweeper, RetentionSweeperCensus, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, RetentionSweeperCensus{}, err
	}

	var (
		out    []RetentionSweeper
		census RetentionSweeperCensus
	)
	named := packageStrings(f)
	census.NamedValues = len(named)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		census.Functions++

		var literals []string
		var names []string
		// Правая часть селектора — ПОЛЕ или МЕТОД, а не имя пакетного значения:
		// `s.q` есть `q` чужого типа. Она отмечается и пропускается ПОИМЁННО, а
		// не отсечением ветки: под селектором лежат и литералы
		// (`db.Exec(ctx, "…").Scan(…)`), и обход, обрывающийся здесь, терял бы
		// их — первая редакция этой правки так и сделала, и перепись сказала об
		// этом сама: литералов 28210 → 27971, удалений 84 → 75.
		fields := map[*ast.Ident]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				census.Literals++
				v, uerr := strconv.Unquote(x.Value)
				if uerr != nil {
					v = x.Value
				}
				literals = append(literals, v)
			case *ast.SelectorExpr:
				fields[x.Sel] = true
			case *ast.Ident:
				if !fields[x] {
					names = append(names, x.Name)
				}
			}
			return true
		})

		matched := ""
		viaName := false
		for _, v := range literals {
			if deleteRe.MatchString(v) {
				census.Deletes++
			}
			if IsTimedDelete(v) {
				matched = v
				break
			}
		}
		if matched == "" {
			for _, id := range names {
				v, ok := named[id]
				if !ok || !IsTimedDelete(v) {
					continue
				}
				matched, viaName = v, true
				break
			}
		}
		if matched == "" {
			continue
		}
		census.Sweepers++
		if viaName {
			census.Named++
		}
		recv := ""
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recv = receiverName(fn.Recv.List[0].Type)
		}
		out = append(out, RetentionSweeper{
			File: path,
			Line: fset.Position(fn.Pos()).Line,
			Recv: recv,
			Name: fn.Name.Name,
			Dir:  dir,
			SQL:  condenseSQL(matched),
		})
	}
	return out, census, nil
}

// packageStrings — строковые значения, объявленные НА УРОВНЕ ПАКЕТА в этом же
// файле, уже собранные в текст.
//
// # Почему `const` И `var`
//
// Предмет — «оператор объявлен отдельно от функции», а не «объявлен
// неизменяемым». Величина, положенная в `var`, уходит в базу тем же способом, и
// различать их значило бы завести слепую зону шириной в одно ключевое слово.
//
// # Почему склейка здесь разбирается, а в теле функции — нет
//
// Склейка ЛИТЕРАЛОВ ФУНКЦИИ была написана и снята: она давала два ложных
// срабатывания (удаление в одном литерале, слово с `_at` — в соседнем сообщении
// об ошибке), потому что в теле рядом лежат куски, друг к другу не относящиеся.
// У объявления величины такого соседства нет by construction: складывается ровно
// то, из чего эта величина состоит, — поэтому `dpopPurgeSQL` вида
// «`DELETE …` + предикат срока» читается целиком и без домысла.
func packageStrings(f *ast.File) map[string]string {
	exprs := map[string]ast.Expr{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, nm := range vs.Names {
				if i >= len(vs.Values) || nm.Name == "_" {
					continue
				}
				exprs[nm.Name] = vs.Values[i]
			}
		}
	}
	out := make(map[string]string, len(exprs))
	for name, e := range exprs {
		if v, ok := resolveString(e, exprs, 0); ok {
			out[name] = v
		}
	}
	return out
}

// resolveString — текст строкового выражения, собираемого из литералов и имён,
// объявленных здесь же.
//
// Глубина ограничена намеренно: перечень имён приходит из того же файла и
// замкнуться сам на себя может (`const a = b`, `const b = a` компилятор не
// пропустит, но разбор не компилятор и обязан кончаться при любом входе).
func resolveString(e ast.Expr, exprs map[string]ast.Expr, depth int) (string, bool) {
	if depth > 8 {
		return "", false
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(x.Value)
		if err != nil {
			v = x.Value
		}
		return v, true
	case *ast.ParenExpr:
		return resolveString(x.X, exprs, depth+1)
	case *ast.Ident:
		inner, ok := exprs[x.Name]
		if !ok {
			return "", false
		}
		return resolveString(inner, exprs, depth+1)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := resolveString(x.X, exprs, depth+1)
		if !lok {
			return "", false
		}
		r, rok := resolveString(x.Y, exprs, depth+1)
		if !rok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// ScanMethodCallNames возвращает имена методов, вызванных в файле, вместе с
// именем объемлющей функции.
//
// Имя объемлющей функции нужно, чтобы вызов уборщика ИЗ САМОГО СЕБЯ (рекурсия,
// обёртка того же имени) не засчитывался вызывающим: иначе уборщик, зовущий
// себя, объявил бы себя провязанным.
func ScanMethodCallNames(path string, src []byte) (map[string][]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		enclosing := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			enclosing = receiverName(fn.Recv.List[0].Type) + "." + enclosing
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					out[sel.Sel.Name] = append(out[sel.Sel.Name], enclosing)
				}
			case *ast.SelectorExpr:
				// Метод, переданный ЗНАЧЕНИЕМ (`Sweep: repo.Reap`), — тоже
				// вызывающий: реестр уборки провязывает уборщиков именно так,
				// и без этой формы гейт объявил бы находкой живую провязку.
				out[x.Sel.Name] = append(out[x.Sel.Name], enclosing)
			case *ast.Ident:
				// ТРЕТЬЯ законная форма: вызов внутри СВОЕГО пакета идёт без
				// селектора (`NewJournalSweeper(db, j, log)`), потому что имя
				// не квалифицируется. Она столь же законна и столь же
				// распространена, а без неё уборщик, объявленный и провязанный
				// в одном пакете, оказывается не находкой, а НЕВИДИМКОЙ:
				// гейт молчит о нём, пока его вообще не зовут, и краснеет,
				// когда зовут правильно. Наблюдалось на первом уборщике общего
				// фундамента — провязка была живой, вердикт обратным.
				out[x.Name] = append(out[x.Name], enclosing)
			}
			return true
		})
	}
	return out, nil
}

// condenseSQL — оператор в одну строку, чтобы отказ гейта читался.
func condenseSQL(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 120
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
