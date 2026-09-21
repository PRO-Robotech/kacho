// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// access_log_wiring_test.go — гейт: журнал доступа стоит СНАРУЖИ полосы прав,
// и уровень печати процесса таков, что записи полосы видны.
//
// # Зачем
//
// Звено, стоящее ЗА полосой прав, не видит ни одного запроса, который полоса
// отвергла сама: она отвечает и не зовёт следующего. Пока журнал доступа стоял
// внутри, отказ края не оставлял НИ ОДНОЙ строки — а это ровно тот класс
// событий, ради которого приходят в разбор происшествия.
//
// Довод не нов и в этом же файле выписан для НАТИВНОЙ полосы: измеритель
// задержки ставится первым, то есть самым внешним, потому что «стоя за звеном
// прав, он оставил бы неизмеренным каждый отказ». У полосы HTTP измерителя нет
// ни одного, поэтому единственная запись о запросе — журнал доступа, и место
// ему то же.
//
// # Почему по синтаксическому дереву
//
// Порядок в этой цепочке выражается порядком присваиваний: каждое следующее
// ОБОРАЧИВАЕТ предыдущее, поэтому «позже в файле» значит «снаружи». Упоминание
// в комментарии за присваивание не считается — отсюда разбор, а не поиск по
// тексту. Техника та же, что у соседнего гейта потолка тела.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parsedMainForWiring — разобранный композиционный корень.
func parsedMainForWiring(t *testing.T) (*ast.File, *token.FileSet) {
	t.Helper()
	root := gatewayTreeRootForWiring(t)
	rel := "cmd/api-gateway/main.go"
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("чтение %s: %v", rel, err)
	}
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, rel, body, parser.ParseComments)
	if parseErr != nil {
		t.Fatalf("разбор %s: %v", rel, parseErr)
	}
	return file, fset
}

// TestHTTPAccessLogIsOutsideTheRightsLane — журнал доступа применяется ПОЗЖЕ
// (то есть снаружи) полосы прав.
//
// Что делать, если гейт сработал:
//
//  1. журнала нет вовсе -> добавить middleware.HTTPAccessLog в цепочку;
//  2. он есть, но раньше полосы прав -> перенести присваивание ПОСЛЕ неё:
//     иначе каждый отказ полосы остаётся без единой строки;
//  3. цепочка перестала собираться накоплением в одну переменную -> уточнить
//     распознавание ниже, а не снимать требование.
func TestHTTPAccessLogIsOutsideTheRightsLane(t *testing.T) {
	file, fset := parsedMainForWiring(t)

	var accessLogPos, authzPos int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "HTTPAccessLog":
			accessLogPos = int(call.Pos())
		case "HTTP":
			if x, isIdent := sel.X.(*ast.Ident); isIdent && x.Name == "authzMW" {
				authzPos = int(call.Pos())
			}
		}
		return true
	})

	// Премиса в обе стороны: оба звена вообще присутствуют. Ноль означал бы, что
	// гейт сравнивает отсутствующее с отсутствующим и молчит на пустом месте.
	if accessLogPos == 0 {
		t.Fatal("в цепочке HTTP нет middleware.HTTPAccessLog — записи о запросе не оставляет ничто")
	}
	if authzPos == 0 {
		t.Fatal("в цепочке HTTP нет authzMW.HTTP — предмета у гейта нет")
	}

	t.Logf("перепись: журнал доступа %s · полоса прав %s",
		fset.Position(token.Pos(accessLogPos)), fset.Position(token.Pos(authzPos)))

	if accessLogPos < authzPos {
		t.Errorf("журнал доступа (%s) применяется РАНЬШЕ полосы прав (%s), то есть стоит ВНУТРИ неё.\n"+
			"Полоса прав отвечает сама и следующего не зовёт, поэтому каждый её отказ — в том числе "+
			"отказ «этот слушатель такого не обслуживает» — не оставляет ни одной строки журнала. "+
			"Перенести присваивание HTTPAccessLog ПОСЛЕ authzMW.HTTP.",
			fset.Position(token.Pos(accessLogPos)), fset.Position(token.Pos(authzPos)))
	}
}

// TestProcessLogLevelIsTheOneTheProbesAssume — уровень печати процесса пришпилен
// к Info.
//
// Гейт держит не «Info — правильный уровень», а СВЯЗЬ: поведенческие пробы
// (restmux/external_refusal_existence_test.go) проверяют видимость записей с
// порогом Info, потому что его ставит корень. Сменится уровень здесь —
// покраснеет этот гейт, а не молча разойдётся смысл их зелёного.
//
// Ручки у уровня нет, и это отдельный, НЕ закрываемый здесь предмет: фундамент
// даёт `observability.NewSloggerLevel`, край им не пользуется и строит логгер
// сам. Гейт фиксирует то, что есть, и не выдаёт это за решение.
func TestProcessLogLevelIsTheOneTheProbesAssume(t *testing.T) {
	file, fset := parsedMainForWiring(t)

	found := ""
	pos := token.NoPos
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent || key.Name != "Level" {
			return true
		}
		sel, isSel := kv.Value.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if x, isPkg := sel.X.(*ast.Ident); isPkg && x.Name == "slog" {
			found = sel.Sel.Name
			pos = kv.Pos()
		}
		return true
	})

	if found == "" {
		t.Fatal("в корне не найдено присваивание Level для обработчика журнала — " +
			"порог печати выяснять нечем, и предположение проб о видимости записей ничем не держится")
	}
	t.Logf("перепись: уровень печати процесса %s (%s)", found, fset.Position(pos))
	if found != "LevelInfo" {
		t.Errorf("порог печати процесса стал %s, а поведенческие пробы видимости записей "+
			"построены на Info. Согласовать обе стороны одним изменением.", found)
	}
}

// TestRequestHeaderCapIsADecision — предел строки запроса и заголовков объявлен
// ЯВНО, а не взят умолчанием библиотеки.
//
// # Зачем
//
// Этот предел — потолок того, сколько байт запросчик БЕЗ удостоверения может
// заставить край принять и записать: путь из строки запроса уезжает в журнал, а
// журнал — общий ресурс, который чинят не быстрее, чем он кончается. Умолчание
// `net/http` — мегабайт, и оно ничьим решением не является: его никто не
// выбирал под этот край и никто не пересмотрит, когда изменится посадка.
//
// Гейт не судит ВЕЛИЧИНУ — он требует, чтобы она была названа. Величина
// обсуждается там, где объявлена, и рядом с доводом.
func TestRequestHeaderCapIsADecision(t *testing.T) {
	file, fset := parsedMainForWiring(t)

	found := false
	pos := token.NoPos
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent || key.Name != "MaxHeaderBytes" {
			return true
		}
		found = true
		pos = kv.Pos()
		return true
	})

	if !found {
		t.Error("сервер края не задаёт MaxHeaderBytes — действует умолчание net/http в мегабайт. " +
			"Это потолок того, сколько байт незасвидетельствованный запросчик заставит край " +
			"принять и записать в журнал; он обязан быть РЕШЕНИЕМ с доводом, а не умолчанием " +
			"библиотеки, которое никто не выбирал и никто не пересмотрит.")
		return
	}
	t.Logf("перепись: предел строки запроса объявлен (%s)", fset.Position(pos))
}

// prependedInterceptors возвращает элементы ВНЕШНЕГО довеска цепи звеньев —
// того самого `append([]T{…}, <цепь>...)`, который ставит звено ПЕРВЫМ, то есть
// самым внешним.
//
// На нативной полосе порядок задаётся не позицией в файле, а позицией в срезе:
// цепь копится присваиваниями сверху вниз, но один довесок ставится СПЕРЕДИ.
// Поэтому гейт ищет именно его, а не сравнивает координаты, как на полосе HTTP.
func prependedInterceptors(t *testing.T, file *ast.File, chainVar string) []string {
	t.Helper()
	var names []string
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		fn, isIdent := call.Fun.(*ast.Ident)
		if !isIdent || fn.Name != "append" {
			return true
		}
		// `append(lit, chain...)` — многоточие помечает САМ вызов, а не
		// отдельным узлом в аргументах.
		if !call.Ellipsis.IsValid() {
			return true
		}
		tail, isTail := call.Args[1].(*ast.Ident)
		if !isTail || tail.Name != chainVar {
			return true
		}
		lit, isLit := call.Args[0].(*ast.CompositeLit)
		if !isLit {
			return true
		}
		found = true
		for _, el := range lit.Elts {
			c, isCall := el.(*ast.CallExpr)
			if !isCall {
				continue
			}
			if sel, isSel := c.Fun.(*ast.SelectorExpr); isSel {
				names = append(names, sel.Sel.Name)
			}
		}
		return true
	})
	if !found {
		t.Fatalf("в корне не найден внешний довесок цепи %s — порядок звеньев выяснять нечем, "+
			"и молчание гейта пусто", chainVar)
	}
	return names
}

// TestNativeAccessLogIsOutsideTheRefusingLinks — ТОТ ЖЕ КЛАСС, что и на полосе
// HTTP, и он обязан быть закрыт на ОБЕИХ.
//
// На нативной полосе журнал доступа дописывался в цепь ПОСЛЕДНИМ, а первый
// элемент там самый внешний, — значит журнал стоял ВНУТРИ всех отказных
// звеньев: личности, привязки удостоверения, допуска по темпу, отказа по
// маршруту и решения о правах. Отказ любого из них не оставлял записи.
//
// Довод здесь тот же, что уже выписан в корне для измерителя задержки: стоя за
// звеном прав, он оставил бы неизмеренным каждый отказ. Измеритель по этому
// доводу поставлен первым, а журнал — нет; одно и то же рассуждение применено к
// одному звену из двух.
func TestNativeAccessLogIsOutsideTheRefusingLinks(t *testing.T) {
	file, _ := parsedMainForWiring(t)

	for _, lane := range []struct{ chainVar, accessLog string }{
		{"grpcUnaryInterceptors", "UnaryAccessLog"},
		{"grpcStreamInterceptors", "StreamAccessLog"},
	} {
		outer := prependedInterceptors(t, file, lane.chainVar)
		t.Logf("перепись: внешний довесок %s несёт %d звено(ьев): %s",
			lane.chainVar, len(outer), strings.Join(outer, ", "))

		carries := false
		for _, n := range outer {
			if n == lane.accessLog {
				carries = true
			}
		}
		if !carries {
			t.Errorf("%s не стоит во внешнем довеске цепи %s (там: %s) — значит он дописан в "+
				"цепь и стоит ВНУТРИ отказных звеньев. Отказ по личности, по привязке "+
				"удостоверения, по темпу, по маршруту и по правам не оставляет записи: "+
				"ровно тот класс, что закрыт на полосе HTTP.",
				lane.accessLog, lane.chainVar, strings.Join(outer, ", "))
		}
	}
}
