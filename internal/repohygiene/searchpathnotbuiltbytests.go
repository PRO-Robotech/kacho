// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// searchpathnotbuiltbytests.go — разбор проб: кто СОБИРАЕТ клаузу приведения
// схемы в строку соединения.
//
// # Предмет
//
// Схема у каждого сервиса своя (`kacho_iam`, `kacho_vpc`, …). Пока `pgtest`
// отдавал DSN без приведения, всякий, кто писал запрос неквалифицированным
// именем, приписывал клаузу сам — и приписывал тридцать с лишним раз, каждый
// своей копией; копии уже разошлись формой (`const`, `+=`, вычисленный
// разделитель, проверка на удвоение — то есть, то нет).
//
// Цена не в трёх строках, а в том, КАК ВЫГЛЯДИТ ИХ ОТСУТСТВИЕ: запрос уходит в
// `public`, сервер отвечает `relation "regions" does not exist` (SQLSTATE
// 42P01), и отказ неотличим ни от непринятых миграций, ни от неверного имени
// таблицы в продукте. Пропуск наказывается сообщением, посылающим читателя не
// туда, — то есть дефект ПРОБЫ подаётся как дефект ПРОДУКТА.
//
// Предмет принадлежит выдающему базу: `pgtest.Config.SearchPath` объявляет
// приведение один раз на пакет, а пакет со своим контейнером зовёт
// `pgtest.WithSearchPath`. Реализация в дереве одна.
//
// # ВЛАДЕЛЕЦ механизма исключён, и он ВЫВОДИТСЯ, а не выписан
//
// Пакет, объявляющий общую реализацию (`func WithSearchPath`), собирает клаузу
// по определению: его собственные пробы строят ожидаемые строки склейкой, и
// иначе проверить реализацию нечем. Исключение взято НЕ ведомостью путей —
// такая запись пережила бы переезд реализации молча, — а обходом: гейт находит
// каталог, где объявление лежит СЕГОДНЯ.
//
// Объявление обязано быть РОВНО ОДНО. Ноль — предпосылка исчезла, и молчание
// гейта перестало что-либо означать. Больше одного — это ровно тот дефект,
// который гейт и предотвращает: вторая реализация разойдётся с первой молча.
//
// # Что отличает НАХОДКУ от законного упоминания — форма, а не место
//
// Ведомость файлов-исключений здесь не заводится: она была бы вторым местом об
// одном предмете и разошлась бы с деревом молча. Отличие берётся из САМОГО
// КОДА — что с литералом ДЕЛАЮТ:
//
//   - его СКЛЕИВАЮТ со строкой соединения (`dsn + sep + opt`, `dsn += opt`) —
//     это сборка клаузы, находка;
//   - его ПЕРЕДАЮТ как значение для сверки (`require.Contains(t, cfg.DSN(), …)`)
//     либо кладут в таблицу случаев — это утверждение О ПРОДУКТОВОМ DSN, и оно
//     законно: конфигурация сервиса собирает своё приведение сама, и проверять
//     её обязаны.
//
// Одного шага косвенности достаточно и он необходим: канонический вид копии —
// `const optionsParam = "options=…"` и ниже `return dsn + sep + optionsParam`,
// где сам литерал в склейке не участвует.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// SearchPathBuildSite — место, где проба собирает клаузу приведения схемы.
type SearchPathBuildSite struct {
	// File — путь файла от корня дерева.
	File string
	// Line — строка склейки.
	Line int
	// Via — через что литерал попал в склейку: пусто, если напрямую, иначе имя
	// связанного с ним идентификатора.
	Via string
}

// searchPathMarker — признак клаузы в её закодированном виде. Ищется вместе с
// `%3D`, а не по слову `search_path`: слово встречается в прозе комментариев,
// и счёт по нему считал бы объяснение сборкой.
const searchPathMarker = "search_path%3D"

// SearchPathBuildSitesIn разбирает ОДИН файл проб.
//
// Разбор синтаксического дерева, а не текста: по образцу над текстом отличить
// склейку от аргумента сверки нельзя — обе формы содержат один и тот же
// литерал в одной и той же строке.
func SearchPathBuildSitesIn(path, src string) ([]SearchPathBuildSite, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, false
	}

	// Шаг косвенности: имена, связанные с литералом клаузы.
	bound := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ValueSpec:
			for i, name := range v.Names {
				if i < len(v.Values) && isSearchPathLiteral(v.Values[i]) {
					bound[name.Name] = true
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if ok && i < len(v.Rhs) && isSearchPathLiteral(v.Rhs[i]) {
					bound[id.Name] = true
				}
			}
		}
		return true
	})

	carries := func(e ast.Expr) (string, bool) {
		if isSearchPathLiteral(e) {
			return "", true
		}
		if id, ok := e.(*ast.Ident); ok && bound[id.Name] {
			return id.Name, true
		}
		return "", false
	}

	var out []SearchPathBuildSite
	seen := map[int]bool{}
	add := func(pos token.Pos, via string) {
		line := fset.Position(pos).Line
		if seen[line] {
			return
		}
		seen[line] = true
		out = append(out, SearchPathBuildSite{File: path, Line: line, Via: via})
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op != token.ADD {
				return true
			}
			for _, side := range []ast.Expr{v.X, v.Y} {
				if via, ok := carries(side); ok {
					add(v.Pos(), via)
				}
			}
		case *ast.AssignStmt:
			// `dsn += "…options=…"` — склейка без BinaryExpr.
			if v.Tok != token.ADD_ASSIGN {
				return true
			}
			for _, rhs := range v.Rhs {
				if via, ok := carries(rhs); ok {
					add(v.Pos(), via)
				}
			}
		}
		return true
	})
	return out, true
}

// isSearchPathLiteral — строковый литерал, несущий клаузу.
func isSearchPathLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	return strings.Contains(s, searchPathMarker)
}

// SearchPathOwnerName — имя общей реализации приведения схемы.
const SearchPathOwnerName = "WithSearchPath"

// SearchPathOwnerDirs — каталоги, где объявлена общая реализация.
//
// Возвращает каталоги, а не файлы: исключаются пробы ВСЕГО пакета-владельца, а
// они лежат рядом с объявлением.
func SearchPathOwnerDirs(files map[string]string) []string {
	var out []string
	for path, src := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil || fn.Name.Name != SearchPathOwnerName {
				continue
			}
			dir := path
			if i := strings.LastIndexByte(dir, '/'); i >= 0 {
				dir = dir[:i]
			} else {
				dir = "."
			}
			out = append(out, dir)
		}
	}
	sort.Strings(out)
	return out
}
