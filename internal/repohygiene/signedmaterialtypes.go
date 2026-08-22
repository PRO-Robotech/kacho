// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// signedmaterialtypes.go — разбор объявлений строкового значения константой
// (приёмка F2, сценарий F2-36).
//
// # Предмет
//
// С задачи #898 один издатель работает с ДВУМЯ видами подписанного: он
// выпускает токен доступа и принимает утверждение клиента. Объявленный тип —
// один из трёх независимых признаков, которыми эти виды разделены, и ни один из
// трёх не назначен единственным.
//
// Пока значения живут по своим пакетам, их РАЗЛИЧИЕ не является ничьей
// находкой: оно не выражено и потому не может покраснеть. Совпади они по
// недосмотру — одно из двух направлений разделения перестало бы работать молча,
// а положительный путь остался бы зелёным.
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ
//
//	const TokenTypeAccess = "at+jwt"     ← объявление: значение заведено здесь
//	if hdr.Typ != "at+jwt" { … }         ← УПОТРЕБЛЕНИЕ: значение сравнивается
//
// Разбор берёт спецификации `const` и `var`, а не всякий литерал: сравнение с
// величиной вторым её объявлением не является, и гейт, считающий литералы,
// запретил бы её употреблять.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **значение, собранное из частей** (`"at" + "+jwt"`, значение переменной).
//     Разбор читает литералы, а не вычисляет выражения.
//  2. **значение, объявленное в чужом языке** — конфигурация, чарт, схема.
//     Разбор ходит по Go-исходникам.
//  3. **поле структуры со значением по умолчанию**, заданным не константой:
//     объявлением константы оно не является, и его повтор — другой класс.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
)

// StringValueDeclaration — объявление строкового значения константой.
type StringValueDeclaration struct {
	File string
	Line int
	// Name — идентификатор, которым значение названо.
	Name string
	// Value — само значение.
	Value string
	// Kind — `const` либо `var`: величина, объявленная переменной, может быть
	// переприсвоена, и это стоит видеть в отказе.
	Kind string
}

// StringValueCensus — объём осмотренного одним файлом.
type StringValueCensus struct {
	// ValueSpecs — спецификаций `const`/`var` прочитано.
	ValueSpecs int
	// StringConstants — из них объявляющих строковый литерал.
	StringConstants int
	// Matches — из них объявляющих одно из искомых значений.
	Matches int
}

// ScanDeclaredStringValues разбирает один файл и собирает объявления искомых
// строковых значений.
func ScanDeclaredStringValues(path string, src []byte, values []string) (
	[]StringValueDeclaration, StringValueCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, StringValueCensus{}, err
	}
	want := map[string]bool{}
	for _, v := range values {
		want[v] = true
	}

	var (
		out    []StringValueDeclaration
		census StringValueCensus
	)
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			census.ValueSpecs++
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				census.StringConstants++
				if !want[val] {
					continue
				}
				census.Matches++
				out = append(out, StringValueDeclaration{
					File:  path,
					Line:  fset.Position(name.Pos()).Line,
					Name:  name.Name,
					Value: val,
					Kind:  gd.Tok.String(),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}
