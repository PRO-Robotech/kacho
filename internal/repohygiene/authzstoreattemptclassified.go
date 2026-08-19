// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// Разбор «каждое обращение к хранилищу прав объявляет ПРИЧИНУ своего исхода»,
// вынесенный из гейта, чтобы проба инъекции могла подать сюда синтетический
// вход и доказать, что разбор умеет и краснеть, и молчать.
//
// ПРЕДМЕТ (#720). Обращение к хранилищу прав отвечает вызывающему одним и тем
// же — ошибкой. Пока причина не названа ОТДЕЛЬНЫМ значением, «хранилища нет»,
// «хранилище молчит» и «соединение из пула оказалось мёртвым» неразличимы
// снаружи: их приходится восстанавливать чтением журнала построчно, уже после
// того как отказ истолкован. На прогоне с одним отказом из 736 запросов это
// означает искать одну строку среди тысяч.
//
// Причина здесь не только для наблюдения: она РЕШАЕТ, имеет ли повтор смысл.
// Мёртвое соединение из пула повторяется на свежем; лежащее и молчащее
// хранилище — нет, иначе закрытый отказ перестаёт укладываться в объявленный
// бюджет ровно тогда, когда хранилищу и так плохо. Поэтому вызов транспорта без
// классификации — не стилистика, а ветка, принимающая решение вслепую.
//
// Гейт читает ИСПОЛНЯЕМОЕ (дерево разбора), а не текст: имена
// `fgaHTTPClient`/`classifyFGAAttempt` стоят и в этой шапке, и в комментариях
// адаптера, поэтому проверка по подстроке краснела бы на собственном
// объяснении.

// AuthzStoreTransportSelector — переменная общего HTTP-клиента к хранилищу прав.
// Вызов `.Do(…)` именно на ней и есть обращение, которое обязано быть
// классифицировано.
const AuthzStoreTransportSelector = "fgaHTTPClient"

// AuthzStoreClassifier — функция, объявляющая исход попытки.
const AuthzStoreClassifier = "classifyFGAAttempt"

// AuthzStoreCallFinding — одно обращение к транспорту хранилища прав, чья
// объемлющая функция исход попытки не классифицирует.
type AuthzStoreCallFinding struct {
	File string
	Line int
	Func string
}

// AuthzStoreCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного».
type AuthzStoreCensus struct {
	Files      int
	Calls      int
	Classified int
}

// FindUnclassifiedAuthzStoreCalls разбирает исходники (имя → содержимое) и
// возвращает обращения к транспорту хранилища прав, чья объемлющая функция не
// зовёт классификатор.
//
// Объемлющей считается ближайшая функция — объявление или литерал: повтор и
// наблюдение живут ровно там, где стоит вызов, и «классификатор где-то в файле»
// свойства не даёт.
func FindUnclassifiedAuthzStoreCalls(sources map[string]string) ([]AuthzStoreCallFinding, AuthzStoreCensus, error) {
	var (
		findings []AuthzStoreCallFinding
		census   AuthzStoreCensus
	)
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, sources[name], 0)
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w", name, err)
		}
		census.Files++

		// Стек объемлющих функций: у каждой — своё «звала ли классификатор».
		type frame struct {
			name       string
			classifies bool
			calls      []AuthzStoreCallFinding
		}
		var stack []frame

		flush := func(f frame) {
			census.Calls += len(f.calls)
			if f.classifies {
				census.Classified += len(f.calls)
				return
			}
			findings = append(findings, f.calls...)
		}

		var walk func(n ast.Node) bool
		walk = func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				stack = append(stack, frame{name: node.Name.Name})
				if node.Body != nil {
					ast.Inspect(node.Body, walk)
				}
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				flush(top)
				return false
			case *ast.FuncLit:
				owner := "func literal"
				if len(stack) > 0 {
					owner = stack[len(stack)-1].name + " (литерал)"
				}
				stack = append(stack, frame{name: owner})
				ast.Inspect(node.Body, walk)
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				flush(top)
				return false
			case *ast.CallExpr:
				if len(stack) == 0 {
					return true
				}
				cur := &stack[len(stack)-1]
				if isIdentCall(node, AuthzStoreClassifier) {
					cur.classifies = true
				}
				if isTransportDo(node, AuthzStoreTransportSelector) {
					pos := fset.Position(node.Pos())
					cur.calls = append(cur.calls, AuthzStoreCallFinding{
						File: name, Line: pos.Line, Func: cur.name,
					})
				}
				return true
			}
			return true
		}
		ast.Inspect(file, walk)
	}
	return findings, census, nil
}

// isIdentCall — вызов функции с данным именем (`classifyFGAAttempt(…)`).
func isIdentCall(call *ast.CallExpr, name string) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == name
}

// isTransportDo — вызов `<selector>.Do(…)`, где selector — простое имя.
// Метод на другом получателе (`c.do(…)`, `r.inner.Do(…)`) сюда НЕ попадает:
// у них своя классификация, и считать их находкой значило бы ловить форму.
func isTransportDo(call *ast.CallExpr, selector string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Do" {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	return ok && recv.Name == selector
}
