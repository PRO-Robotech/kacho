// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
)

// Гейт «отображаемое имя принципала не кладут на провод сырым».
//
// Имя — единственное значение семейства `x-kacho-`, приходящее свободным вводом
// пользователя. Значение обычного metadata-ключа gRPC обязано быть печатаемым
// ASCII; иначе транспорт отвергает ВЕСЬ вызов кодом Internal, не доходя до
// обработчика, — то есть пользователь с кириллическим именем теряет доступ к API
// целиком, а не «имя не отображается» (#873).
//
// Гейт заведён потому, что писателей этого ключа несколько (пути Bearer, Kratos,
// служебной учётки, DPoP плюс распространение между сервисами), и правка одного
// из них молча не доезжала до остальных: «здесь закодировали» было неотличимо от
// «здесь забыли» ничем, кроме чтения всех мест подряд.
//
// Признак читает РАЗОБРАННЫЙ исходник, а не текст: имя ключа стоит и в
// комментариях, объясняющих эту же защиту, и гейт по подстроке краснел бы на
// собственном объяснении.

// displayKeyMarkers — по чему узнаётся, что аргумент называет ключ отображаемого
// имени. Идентификаторы констант — в любом из трёх поверхностных видов (заголовок
// голый, заголовок мостовой, metadata-ключ), плюс сама строка ключа на случай,
// если её однажды впишут литералом.
var displayKeyMarkers = []string{
	"PrincipalDisplay",
	"principalDisplay",
	"x-kacho-principal-display-name",
}

// displayValueProducers — законные способы получить значение этого ключа.
// Первый применяют там, где значение ВПЕРВЫЕ попадает под ключ (обратим,
// экранирует процент); второй — в точке пересылки, где значение уже под ключом
// (идемпотентен, поэтому не портит уже закодированное).
var displayValueProducers = []string{
	"EncodePrincipalDisplayName",
	"EnsurePrincipalDisplayNameWireSafe",
}

// keyValueWriters — вызовы, у которых аргументы идут парами «ключ, значение».
var keyValueWriters = map[string]bool{
	"Set": true, "Append": true, "Add": true,
	"Pairs": true, "AppendToOutgoingContext": true,
}

// RawDisplayNameWrite — место, где значение кладут под ключ отображаемого имени
// без кодирования.
type RawDisplayNameWrite struct {
	Line int
	Expr string
}

// findRawDisplayNameWrites разбирает исходник и возвращает записи под ключ
// отображаемого имени, чьё значение не прошло через кодек.
//
// Возвращает также число осмотренных записей под этот ключ — чтобы «ноль
// находок» было отличимо от «ключ в файле не встретился».
func findRawDisplayNameWrites(filename string, src []byte) (finds []RawDisplayNameWrite, writes int, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: разбор: %w", filename, err)
	}

	text := func(n ast.Node) string {
		var b bytes.Buffer
		if perr := printer.Fprint(&b, fset, n); perr != nil {
			return ""
		}
		return b.String()
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := displayCalleeName(call.Fun)
		if !keyValueWriters[name] {
			return true
		}
		// Пары «ключ, значение» начинаются с первого аргумента у Set/Append/Add/
		// Pairs и со второго у AppendToOutgoingContext (первый там — ctx).
		start := 0
		if name == "AppendToOutgoingContext" {
			start = 1
		}
		for i := start; i+1 < len(call.Args); i += 2 {
			keyTxt := text(call.Args[i])
			if !namesDisplayKey(keyTxt) {
				continue
			}
			writes++
			if valueIsSafe(text(call.Args[i+1])) {
				continue
			}
			finds = append(finds, RawDisplayNameWrite{
				Line: fset.Position(call.Args[i+1].Pos()).Line,
				Expr: text(call),
			})
		}
		return true
	})
	return finds, writes, nil
}

func displayCalleeName(fun ast.Expr) string {
	switch e := fun.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.Ident:
		return e.Name
	}
	return ""
}

func namesDisplayKey(expr string) bool {
	for _, m := range displayKeyMarkers {
		if strings.Contains(expr, m) {
			return true
		}
	}
	return false
}

// valueIsSafe — значение годно, если оно прошло через кодек либо является
// строковым литералом из печатаемого ASCII (константу транспорт примет, и она
// не может стать кириллицей от пользовательского ввода).
func valueIsSafe(expr string) bool {
	for _, p := range displayValueProducers {
		if strings.Contains(expr, p) {
			return true
		}
	}
	if lit, err := strconv.Unquote(strings.TrimSpace(expr)); err == nil {
		for i := 0; i < len(lit); i++ {
			if lit[i] < 0x20 || lit[i] > 0x7E {
				return false
			}
		}
		return true
	}
	return false
}
