// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// assertionadmissioncalls.go — разбор обращений к базе, разложенных по функциям
// (приёмка F2, сценарий F2-28).
//
// Предмет разбора — ЧИСЛО обращений к базе внутри одной функции. Допуск
// однократности обязан делать РОВНО ОДНО: «не предъявлялось ли уже» и
// «погасить» неделимы, и неделимыми их делает первичный ключ таблицы, а не
// аккуратность вызывающего. Пара «посмотреть — записать» проходит ВСЕ
// последовательные пробы: окна между чтением и записью при последовательном
// прогоне не существует. Поэтому число вызовов стережёт гейт, а не обзор диффа.
//
// # Почему владелец соединения берётся из ОБЪЯВЛЕНИЯ ТИПА, а не из списка имён
//
// Признак «вызов к базе» нельзя свести к имени метода: `Exec` есть у чего
// угодно, и одноимённый метод счётчика попал бы в находки. Разбор читает
// объявления структур файла и берёт в носители соединения те поля, ЧЕЙ ТИП
// называет драйвер базы (`pgx`, `pgxpool`, `database/sql`). Тогда
// переименование поля исход не меняет, а чужое одноимённое поле находкой не
// становится.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **соединение, приехавшее аргументом** функции, а не полем структуры
//     (`func f(tx pgx.Tx)`). Носители берутся из полей: параметр объявления
//     типа структуры не имеет. В разбираемом файле такой формы нет, и её
//     появление видно по счётчику носителей.
//  2. **косвенный вызов** — метод, положенный в переменную. Разбор судит вызов
//     по месту, а не по потоку значений.
//  3. **обращение через помощника** — функция, которая сама зовёт базу. Число
//     вызовов у ВЫЗЫВАЮЩЕГО останется единицей, а пара «посмотреть — записать»
//     уедет внутрь помощника. Это другой класс, и его стережёт интеграционная
//     проба конкурентного предъявления (F2-24), которая спрашивает исход, а не
//     форму.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// databaseCallVerbs — методы драйвера, каждый из которых есть ОБРАЩЕНИЕ К БАЗЕ.
//
// Перечень закрыт намеренно: метод драйвера, которого здесь нет, обращением не
// считается, и его появление обязано быть решением, а не умолчанием.
var databaseCallVerbs = map[string]bool{
	"Exec":      true,
	"Query":     true,
	"QueryRow":  true,
	"SendBatch": true,
	"CopyFrom":  true,
	"Begin":     true,
	"BeginTx":   true,
	"Acquire":   true,
}

// databaseDriverMarkers — то, чем тип поля выдаёт в себе носителя соединения.
var databaseDriverMarkers = []string{"pgx", "pgxpool", "sql.DB", "sql.Tx", "sql.Conn", "DBTX"}

// DatabaseCallSite — координата обращения к базе.
type DatabaseCallSite struct {
	File string
	Line int
	// Verb — метод драйвера, которым обращение опознано.
	Verb string
	// Handle — поле-носитель соединения, через которое обращение сделано.
	Handle string
}

// FunctionDatabaseCalls — обращения одной функции.
type FunctionDatabaseCalls struct {
	// Name — «Тип.Метод» для метода и «Функция» для обычной функции.
	Name string
	// Line — строка объявления: координата для отказа гейта.
	Line  int
	Calls []DatabaseCallSite
}

// DatabaseCallCensus — объём осмотренного одним файлом.
type DatabaseCallCensus struct {
	// Structs — объявлений структур прочитано.
	Structs int
	// Handles — полей-носителей соединения опознано. Ноль означает, что разбор
	// не нашёл, через что файл ходит в базу: его молчание тогда сказано ни о чём.
	Handles []string
	// Functions — функций и методов осмотрено.
	Functions int
	// Calls — вызовов осмотрено (всех, не только к базе).
	Calls int
	// DBCalls — из них признано обращениями к базе.
	DBCalls int
}

// ScanDatabaseCallsByFunction разбирает один файл и раскладывает обращения к
// базе по функциям.
func ScanDatabaseCallsByFunction(path string, src []byte) (
	map[string]FunctionDatabaseCalls, DatabaseCallCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, DatabaseCallCensus{}, err
	}

	var census DatabaseCallCensus
	handles := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		census.Structs++
		for _, fld := range st.Fields.List {
			if !isDatabaseHandleType(fld.Type) {
				continue
			}
			for _, name := range fld.Names {
				handles[name.Name] = true
			}
		}
		return true
	})
	for h := range handles {
		census.Handles = append(census.Handles, h)
	}
	sort.Strings(census.Handles)

	out := map[string]FunctionDatabaseCalls{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		census.Functions++
		entry := FunctionDatabaseCalls{
			Name: functionQualifiedName(fn),
			Line: fset.Position(fn.Pos()).Line,
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			census.Calls++
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if !databaseCallVerbs[sel.Sel.Name] {
				return true
			}
			handle, ok := databaseHandleOf(sel.X, handles)
			if !ok {
				return true
			}
			entry.Calls = append(entry.Calls, DatabaseCallSite{
				File:   path,
				Line:   fset.Position(call.Pos()).Line,
				Verb:   sel.Sel.Name,
				Handle: handle,
			})
			census.DBCalls++
			return true
		})
		sort.Slice(entry.Calls, func(i, j int) bool { return entry.Calls[i].Line < entry.Calls[j].Line })
		out[entry.Name] = entry
	}
	return out, census, nil
}

// isDatabaseHandleType отвечает, называет ли тип поля драйвер базы.
func isDatabaseHandleType(expr ast.Expr) bool {
	rendered := renderTypeExpr(expr)
	for _, m := range databaseDriverMarkers {
		if strings.Contains(rendered, m) {
			return true
		}
	}
	return false
}

// renderTypeExpr — плоское представление типа: достаточно, чтобы разглядеть в
// нём имя пакета драйвера, и не требует вывода типов.
func renderTypeExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return renderTypeExpr(t.X)
	case *ast.SelectorExpr:
		return renderTypeExpr(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return renderTypeExpr(t.Elt)
	case *ast.IndexExpr:
		return renderTypeExpr(t.X)
	default:
		return ""
	}
}

// databaseHandleOf отвечает, идёт ли вызов через поле-носитель соединения.
//
// Признаются две формы: `r.pool.Exec(...)` (поле приёмника) и `pool.Exec(...)`
// (носитель под своим именем). Всё прочее — чужой одноимённый метод.
func databaseHandleOf(x ast.Expr, handles map[string]bool) (string, bool) {
	switch e := x.(type) {
	case *ast.SelectorExpr:
		if handles[e.Sel.Name] {
			return e.Sel.Name, true
		}
	case *ast.Ident:
		if handles[e.Name] {
			return e.Name, true
		}
	}
	return "", false
}

// functionQualifiedName — «Тип.Метод» либо «Функция».
func functionQualifiedName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := renderTypeExpr(fn.Recv.List[0].Type)
	if recv == "" {
		return fn.Name.Name
	}
	return recv + "." + fn.Name.Name
}
