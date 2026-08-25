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
//  2. **оператор, приехавший из переменной или построителя запросов** — литерала
//     в теле функции тогда нет вовсе. Форм такого рода в дереве нет; их
//     появление видно по счётчику распознанных уборщиков в переписи.
//  3. **вызов косвенный** — метод, положенный в переменную и вызванный оттуда.
//     Разбор судит вызов по месту, а не по потоку значений.
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
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		census.Functions++

		var literals []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.Literals++
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				v = lit.Value
			}
			literals = append(literals, v)
			return true
		})

		matched := ""
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
			continue
		}
		census.Sweepers++
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
