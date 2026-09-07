// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenclaimsinglesource.go — разбор мест, СОБИРАЮЩИХ состав утверждений
// выданного токена (приёмка F2, сценарий F2-42, §2.11).
//
// # Предмет
//
// С этой фазы токен принципалу выдают ДВА пути: обратный вызов прежнего
// провайдера, пока он жив, и наш собственный эндпоинт. Пока перечень утверждений
// и правила их вычисления живут у каждого свои, различие между ними НЕ ЯВЛЯЕТСЯ
// НИЧЬЕЙ НАХОДКОЙ: оно не выражено и потому не может покраснеть. Первая же
// правка одной стороны разойдётся с другой молча — и разойдётся у ПРИНЦИПАЛА,
// чей токен выдан не тем путём.
//
// # Что здесь считается СБОРКОЙ, а что ПОТРЕБЛЕНИЕМ
//
//	claims := map[string]any{"kaname_user_id": …, "kaname_account_id": …}  ← СБОРКА
//	return s.userTokenClaims(row, user, subject, hookCtx)                ← потребление
//
// Потребителей должно быть МНОГО — они и есть цель: второй способ дойти до той
// же сборки, а не второй состав. Находкой является ВТОРАЯ СБОРКА.
//
// # Почему судится ИМЯ КЛЮЧА, а не тип отображения
//
// Предмет — СЛОВАРЬ утверждений, поэтому место опознаётся по именам ключей, а
// тип значений в счёт не идёт: `map[string]string` с теми же именами есть та же
// вторая сборка, переписанная сменой типа. Разбор, судящий по типу, пропустил бы
// её целиком, и обойти его можно было бы одной строкой объявления.
//
// # Почему порог по числу ключей, а не «хоть один»
//
// Префикс утверждений встречается и вне состава — им же названы метрики и поля
// контекста. Место, назвавшее один ключ, состава не объявляет: оно читает или
// правит одно значение. Состав — это перечень, и перечнем он становится с
// нескольких ключей сразу.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **состав, собранный присваиваниями** (`m := map[string]any{}` и дальше
//     `m["kaname_x"] = …` по одному). Разбор читает составной литерал; такая
//     форма даёт литерал без ключей, и её появление видно по счётчику пустых
//     литералов.
//  2. **имя ключа, собранное из частей** или взятое переменной.
//  3. **состав, объявленный в чужом языке** — шаблон, схема, конфигурация.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// ClaimAssembly — место, собирающее состав утверждений.
type ClaimAssembly struct {
	File string
	Line int
	// Func — функция, внутри которой лежит сборка: по номеру строки читатель
	// отказа не поймёт, что именно собирает состав.
	Func string
	// Keys — РАЗНЫЕ ключи состава, названные здесь, по возрастанию.
	Keys []string
}

// ClaimBuilderCall — вызов сборщика состава: то, чем состав ПОТРЕБЛЯЕТСЯ.
type ClaimBuilderCall struct {
	File string
	Line int
	// Func — функция, из которой сборщик вызван. Гейт считает РАЗНЫЕ функции:
	// «обе стороны» — это две точки входа, а не два вызова из одной.
	Func string
	// Callee — имя сборщика.
	Callee string
}

// ClaimCensus — объём осмотренного одним файлом.
type ClaimCensus struct {
	// MapLiterals — составных литералов отображения со строковым ключом
	// прочитано.
	MapLiterals int
	// EmptyMapLiterals — из них без единого ключа. Печатается отдельно: это
	// ровно та форма, в которой состав собирается присваиваниями, и разбор её
	// не видит.
	EmptyMapLiterals int
	// KeyedLiterals — из них с ключами состава.
	KeyedLiterals int
	// Calls — вызовов осмотрено.
	Calls int
}

// ScanClaimAssemblies разбирает один файл и собирает места сборки состава.
//
// prefix — префикс имени ключа состава; minKeys — сколько РАЗНЫХ ключей делают
// место сборкой.
func ScanClaimAssemblies(path string, src []byte, prefix string, minKeys int) (
	[]ClaimAssembly, ClaimCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, ClaimCensus{}, err
	}
	var (
		out    []ClaimAssembly
		census ClaimCensus
	)
	for _, decl := range f.Decls {
		fn, _ := decl.(*ast.FuncDecl)
		enclosing := "уровень пакета"
		if fn != nil {
			enclosing = functionQualifiedName(fn)
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			mt, ok := cl.Type.(*ast.MapType)
			if !ok {
				return true
			}
			if id, ok := mt.Key.(*ast.Ident); !ok || id.Name != "string" {
				return true
			}
			census.MapLiterals++
			keys := map[string]bool{}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				lit, ok := kv.Key.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.HasPrefix(name, prefix) {
					continue
				}
				keys[name] = true
			}
			if len(cl.Elts) == 0 {
				census.EmptyMapLiterals++
			}
			if len(keys) == 0 {
				return true
			}
			census.KeyedLiterals++
			if len(keys) < minKeys {
				return true
			}
			a := ClaimAssembly{File: path, Line: fset.Position(cl.Pos()).Line, Func: enclosing}
			for k := range keys {
				a.Keys = append(a.Keys, k)
			}
			sort.Strings(a.Keys)
			out = append(out, a)
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}

// ScanClaimBuilderCalls разбирает один файл и собирает вызовы сборщиков состава.
func ScanClaimBuilderCalls(path string, src []byte, builders map[string]bool) (
	[]ClaimBuilderCall, ClaimCensus, error,
) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, ClaimCensus{}, err
	}
	var (
		out    []ClaimBuilderCall
		census ClaimCensus
	)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		enclosing := functionQualifiedName(fn)
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			census.Calls++
			var name string
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			case *ast.Ident:
				name = fun.Name
			default:
				return true
			}
			if !builders[name] {
				return true
			}
			out = append(out, ClaimBuilderCall{
				File: path, Line: fset.Position(call.Pos()).Line,
				Func: enclosing, Callee: name,
			})
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, census, nil
}
