// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identitywirekeys.go — разбор мест, где ПРОСТРАНСТВО ИМЁН ЛИЧНОСТИ объявлено
// (приёмка KAN-WIRE-1, сценарии KAN-W2-05 / KAN-W2-06, предмет `ПР-2`).
//
// # Предмет
//
// Личность арендатора едет от края к слушателю службы набором ключей под общей
// приставкой. Пока у набора было ДВА независимых объявления — своё у края и
// своё у фундамента, — переименование одной стороны СОБИРАЛОСЬ ЧИСТО, а
// приёмник, не найдя своих ключей, читал это как «личности нет» и шёл дальше.
// Рассинхрон давал не отказ, а ПОТЕРЮ личности, и заметить его было нечем.
//
// Здесь разбираются две стороны одного предмета:
//
//  1. **где имя ОБЪЯВЛЕНО** — объявление имени вне пакета-объявления есть
//     второе место об одном предмете;
//  2. **чем имя СВЯЗАНО у фундамента** — какие ключи каталога заведены у него
//     своими константами. Каталог называет, какие обязаны быть; расхождение
//     между «объявлено» и «заведено» — тот же рассинхрон, только на шаг раньше
//     провода.
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ, а что употреблением
//
//	const MetaPrincipalID = "x-kacho-principal-id"   ← объявление
//	var keys = []string{"x-kacho-principal-id"}      ← объявление
//	md.Get("x-kacho-principal-id")                   ← УПОТРЕБЛЕНИЕ: не судится
//	t.Fatalf("x-kacho-principal-* не глядя …")       ← ПРОЗА: не судится
//
// Различает УЗЕЛ: судятся значения объявлений (`ValueSpec`), а не всякий
// строковый литерал. Граница не косметическая — за ней лежат два законных
// класса, которые запрет литералов сделал бы невыразимыми: чтение чужого,
// клиентом подделываемого ключа (его надо прочитать, чтобы снять) и текст
// отказа, называющий приставку.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **склейку по частям** — `"x-kacho-" + "principal-id"`. Разбор судит
//     литерал, а не поток значений; такой формы в дереве ноль;
//  2. **объявление внутри функции** — `ValueSpec` берётся отовсюду, включая
//     тела функций, поэтому эта форма как раз видна; невидима форма
//     `k := "x-kacho-principal-id"` (краткое присваивание объявлением не
//     является). Это употребление, и судить его значило бы запретить чтение;
//  3. **точечный импорт** пакета-объявления — обращение пишется голым именем.
//     В дереве такого импорта ноль, и форма пропускается сознательно.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/principalwire"
)

const (
	// IdentityWireOwnerDir — каталог ЕДИНСТВЕННОГО объявления. Имя здесь
	// законно; всюду ещё — находка.
	IdentityWireOwnerDir = "pkg/principalwire/"
	// IdentityWireFundamentDir — каталог фундамента: слой, который читает
	// пересланную личность у слушателя.
	IdentityWireFundamentDir = "pkg/grpcsrv/"
)

// IdentityWireDeclaration — координата места, где имя пространства объявлено.
type IdentityWireDeclaration struct {
	File string
	Line int
	// Const — имя объявленной константы либо переменной; пусто у безымянных
	// элементов составного значения.
	Const string
	// Value — прочитанное значение. Находка без него не чинится.
	Value string
	// Kind — чем значение является: пространство, приставка подсемейства, ключ.
	Kind principalwire.WireNameKind
}

// IdentityWireBinding — константа ФУНДАМЕНТА, связанная с именем каталога.
type IdentityWireBinding struct {
	File string
	Line int
	// Const — имя константы фундамента (`MDKeyPrincipalID` и соседи).
	Const string
	// Ident — имя константы пакета-объявления, к которой она привязана.
	Ident string
}

// IdentityWireCensus — объём осмотренного одним файлом.
type IdentityWireCensus struct {
	Specs      int
	Literals   int
	Selectors  int
	Imports    int
	DotImports int
}

// ScanIdentityWireDeclarations разбирает один исходник и возвращает ВСЕ места,
// где имя пространства личности объявлено, все привязки констант к
// пакету-объявлению и объём осмотренного.
//
// Возвращаются ФАКТЫ, а не вердикт: место в самом пакете-объявлении приходит
// сюда наравне с прочими, и законным его делает вызывающий. Иначе предпосылка
// гейта — «объявление вообще существует» — проверялась бы вторым разбором об
// одном предмете.
func ScanIdentityWireDeclarations(path string, src []byte) (
	decls []IdentityWireDeclaration, bindings []IdentityWireBinding,
	census IdentityWireCensus, err error,
) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, nil, IdentityWireCensus{}, perr
	}

	ownerAliases := map[string]bool{}
	for _, imp := range f.Imports {
		census.Imports++
		p, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil || p != principalwire.ImportPath {
			continue
		}
		name := principalwire.ImportPath[strings.LastIndex(principalwire.ImportPath, "/")+1:]
		if imp.Name != nil {
			switch imp.Name.Name {
			case ".":
				census.DotImports++
				continue
			case "_":
				continue
			default:
				name = imp.Name.Name
			}
		}
		ownerAliases[name] = true
	}

	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		census.Specs++
		for i, v := range vs.Values {
			name := ""
			if i < len(vs.Names) {
				name = vs.Names[i].Name
			}
			for _, lit := range identityWireStringLiterals(v) {
				census.Literals++
				value, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					continue
				}
				kind := principalwire.ClassifyWireName(value)
				if kind == principalwire.WireNameNone {
					continue
				}
				decls = append(decls, IdentityWireDeclaration{
					File: path, Line: fset.Position(lit.Pos()).Line,
					Const: name, Value: value, Kind: kind,
				})
			}
			for _, sel := range identityWireOwnerSelectors(v, ownerAliases) {
				census.Selectors++
				bindings = append(bindings, IdentityWireBinding{
					File: path, Line: fset.Position(sel.Pos()).Line,
					Const: name, Ident: sel.Sel.Name,
				})
			}
		}
		return true
	})
	return decls, bindings, census, nil
}

// identityWireStringLiterals — строковые литералы значения объявления, включая элементы
// составного значения (`[]string{…}`, `map[string]bool{…}`) и склейку.
func identityWireStringLiterals(e ast.Expr) []*ast.BasicLit {
	var out []*ast.BasicLit
	ast.Inspect(e, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			out = append(out, lit)
		}
		return true
	})
	return out
}

// identityWireOwnerSelectors — обращения к пакету-объявлению внутри значения объявления.
func identityWireOwnerSelectors(e ast.Expr, ownerAliases map[string]bool) []*ast.SelectorExpr {
	var out []*ast.SelectorExpr
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && ownerAliases[pkg.Name] {
			out = append(out, sel)
		}
		return true
	})
	return out
}
