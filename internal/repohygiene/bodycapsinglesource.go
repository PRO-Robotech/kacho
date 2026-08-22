// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bodycapsinglesource.go — разбор объявлений потолка тела запроса (приёмка F2,
// сценарий F2-12).
//
// Потолок обязан быть РЕАЛИЗОВАН ровно в одном месте дерева. Реализаций бывает
// несколько, и различие между ними НЕ ВЫРАЖЕНО: каждая по отдельности выглядит
// исполненной, а первая же правка одной из них до остальных не доезжает — и не
// доезжает молча. Замер, из-за которого пакет-владелец заведён: реализаций было
// ТРИ, и объявленную длину проверяла ОДНА.
//
// # Что здесь считается РЕАЛИЗАЦИЕЙ, а что ПОТРЕБИТЕЛЕМ
//
//	http.MaxBytesReader(w, r.Body, n)   ← реализация: ставит ограничитель сама
//	httpbody.Cap(w, r, n)              ← ПОТРЕБИТЕЛЬ: зовёт единственную реализацию
//
// Потребителей сколько угодно — они и есть цель. Находкой является ВТОРАЯ
// реализация, а не второе употребление.
//
// # Почему вызов приводится к ПОЛНОМУ ПУТИ ИМПОРТА, а не к имени пакета
//
// Имя пакета в исходнике задаёт вызывающий: `nethttp "net/http"` и
// `myhttp "example.com/http"` пишутся одинаково коротко и означают разное.
// Разбор читает объявления импортов файла и приводит вызов к паре
// «путь импорта + имя функции»; тогда псевдоним на исход не влияет, а чужой
// одноимённый помощник не становится находкой.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **точечный импорт** (`import . "net/http"`) — вызов пишется голым
//     `MaxBytesReader(...)`, псевдонима у него нет, и привести его к пути
//     импорта нечем. В дереве такого импорта ноль (проверено предикатом ниже),
//     и разбор пропускает такие импорты сознательно.
//  2. **косвенный вызов** — значение функции, положенное в переменную или поле
//     (`limiter := http.MaxBytesReader; limiter(...)`). Разбор судит вызов по
//     месту, а не по потоку значений.
//  3. **своя реализация БЕЗ этой функции** — ограничитель, написанный руками
//     над `io.LimitReader`. Это другой класс: он не «вторая копия потолка», а
//     потолок без второго слоя, и его стережёт проба самого пакета-владельца.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// BodyCapSite — координата вызова в дереве.
type BodyCapSite struct {
	File string
	Line int
	// Callee — пара «путь импорта + имя функции»: то, чем вызов опознаётся
	// независимо от псевдонима пакета в исходнике.
	Callee string
}

// BodyCapCensus — объём осмотренного одним файлом.
type BodyCapCensus struct {
	// Imports — объявлений импорта прочитано.
	Imports int
	// Calls — вызовов осмотрено.
	Calls int
	// DotImports — точечных импортов встречено. Печатается отдельно, потому что
	// это ровно та форма, которой разбор не видит: молчание при ненулевом
	// счётчике сказано о меньшем, чем кажется.
	DotImports int
}

// ScanBodyCapCalls разбирает один файл и делит вызовы на реализации потолка и
// его потребителей.
//
// impl и consumer задаются парой «путь импорта + имя функции» (`net/http.MaxBytesReader`).
func ScanBodyCapCalls(path string, src []byte, impl, consumer string) (
	impls, consumers []BodyCapSite, census BodyCapCensus, err error,
) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, nil, BodyCapCensus{}, perr
	}

	aliases := map[string]string{}
	for _, imp := range f.Imports {
		census.Imports++
		p := bodyCapUnquote(imp.Path.Value)
		if p == "" {
			continue
		}
		name := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			name = p[i+1:]
		}
		if imp.Name != nil {
			if imp.Name.Name == "." {
				census.DotImports++
				continue
			}
			if imp.Name.Name == "_" {
				continue
			}
			name = imp.Name.Name
		}
		aliases[name] = p
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		census.Calls++
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, ok := aliases[pkgIdent.Name]
		if !ok {
			return true
		}
		site := BodyCapSite{
			File:   path,
			Line:   fset.Position(call.Pos()).Line,
			Callee: importPath + "." + sel.Sel.Name,
		}
		switch site.Callee {
		case impl:
			impls = append(impls, site)
		case consumer:
			consumers = append(consumers, site)
		}
		return true
	})

	sort.Slice(impls, func(i, j int) bool { return impls[i].Line < impls[j].Line })
	sort.Slice(consumers, func(i, j int) bool { return consumers[i].Line < consumers[j].Line })
	return impls, consumers, census, nil
}

// bodyCapUnquote — путь импорта без кавычек. Отдельной функцией, чтобы разбор не
// зависел от того, чем именно закавычен путь в исходнике.
func bodyCapUnquote(quoted string) string {
	if len(quoted) < 2 {
		return ""
	}
	return strings.Trim(quoted, "\"`")
}
