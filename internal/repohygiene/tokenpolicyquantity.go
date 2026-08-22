// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokenpolicyquantity.go — разбор объявлений длительности (приёмка F2,
// сценарий F2-22).
//
// # Предмет
//
// Величина политики токенов — допуск расхождения часов и потолок длительности
// утверждения — обязана быть объявлена ЧИСЛОМ ровно один раз. Второе
// объявление не расходится с первым сразу: оно расходится при первой же правке
// одной стороны, и расходится там, где расхождение не видно, потому что обе
// величины по отдельности выглядят разумными.
//
// Цена расхождения у этих двух величин названа приёмкой прямо: потолок
// длительности участвует В ДВУХ расчётах — он ограничивает само утверждение и
// он же задаёт верхнюю границу жизни строки погашения. Разойдясь, они делают
// строку либо короче утверждения (повтор становится законным), либо длиннее
// (хранилище растёт без границы).
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ ЧИСЛОМ
//
//	ClockSkew = 60 * time.Second        ← объявление ЧИСЛОМ
//	grace     = MaxTTL + CacheCeiling   ← объявление ВЫРАЖЕНИЕМ: величина не
//	                                      выбрана, а выведена
//	skewSeconds = 60                    ← целое: длительностью не является
//
// Разбор различает эти три случая и печатает счётчик неразобранных: «величина
// объявлена один раз» обязано быть отличимо от «второе объявление разбор не
// прочитал».
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **величина, приехавшая настройкой** (переменная окружения, поле профиля
//     развёртывания). Это другой класс — «объявлено без читателя», и его
//     стережёт свой гейт.
//  2. **величина, сложенная ТОЛЬКО из других имён** (`grace = MaxTTL +
//     Ceiling`): единицы длительности она не называет, поэтому объявлением
//     длительности не признаётся вовсе. Вторым объявлением ЧИСЛОМ она и не
//     является — величина не выбрана, а выведена, — но и в счётчик
//     неразобранных не попадает: перепись говорит о том, что разбор увидел.
//     Величина, у которой единица названа, а слагаемое выведено
//     (`MaxTTL + 15*time.Minute`), в перепись попадает НЕРАЗОБРАННОЙ.
//  3. **величина, лежащая в чужом языке** — конфигурация, чарт, схема. Разбор
//     ходит по Go-исходникам.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"time"
)

// DurationDeclaration — объявление длительности.
type DurationDeclaration struct {
	File string
	Line int
	// Name — идентификатор, которым величина названа.
	Name string
	// Expr — выражение значения в плоском виде: нужно тексту отказа, потому что
	// по одному числу читатель не поймёт, из чего оно сложено.
	Expr string
	// Nanos — разобранное значение. Осмысленно только при Resolved.
	Nanos int64
	// Resolved — значение разобрано как ЧИСЛО. Ложь означает, что величина
	// выведена из других либо записана формой, которой разбор не знает.
	Resolved bool
}

// DurationCensus — объём осмотренного одним файлом.
type DurationCensus struct {
	// ValueSpecs — объявлений `const`/`var` прочитано.
	ValueSpecs int
	// Durations — из них признано объявлениями длительности.
	Durations int
	// Unresolved — из них таких, чьё значение разбором не сведено к числу.
	// Печатается отдельно: молчание при ненулевом счётчике сказано о меньшем,
	// чем кажется.
	Unresolved int
}

// ScanDurationDeclarations разбирает один файл и собирает объявления
// длительности.
func ScanDurationDeclarations(path string, src []byte) ([]DurationDeclaration, DurationCensus, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, DurationCensus{}, err
	}
	var (
		out    []DurationDeclaration
		census DurationCensus
	)
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		census.ValueSpecs++
		for i, name := range vs.Names {
			if i >= len(vs.Values) {
				continue
			}
			value := vs.Values[i]
			if !mentionsDurationUnit(value) {
				continue
			}
			census.Durations++
			d := DurationDeclaration{
				File: path,
				Line: fset.Position(name.Pos()).Line,
				Name: name.Name,
				Expr: renderDurationExpr(value),
			}
			if nanos, ok := resolveDurationExpr(value); ok {
				d.Nanos, d.Resolved = nanos, true
			} else {
				census.Unresolved++
			}
			out = append(out, d)
		}
		return true
	})
	return out, census, nil
}

// mentionsDurationUnit отвечает, называет ли выражение единицу длительности.
//
// Признак намеренно синтаксический: `60 * time.Second` есть длительность, а
// `60` — целое, и различие это несущее. Настройка «в секундах» вторым
// объявлением величины не является: она названа не числом длительности, а
// числом, к которому единицу приложит читатель.
func mentionsDurationUnit(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "time" {
			return true
		}
		if _, ok := durationUnits[sel.Sel.Name]; ok {
			found = true
		}
		return true
	})
	return found
}

// durationUnits — единицы длительности стандартной библиотеки.
var durationUnits = map[string]time.Duration{
	"Nanosecond":  time.Nanosecond,
	"Microsecond": time.Microsecond,
	"Millisecond": time.Millisecond,
	"Second":      time.Second,
	"Minute":      time.Minute,
	"Hour":        time.Hour,
}

// resolveDurationExpr сводит выражение к числу наносекунд.
//
// Признаются только те формы, в которых величина ВЫБРАНА: единица, произведение
// целого на единицу и сумма таких произведений. Величина, сложенная из ДРУГИХ
// ИМЕНОВАННЫХ величин, числом не объявлена — она выведена, и её повтор
// принадлежит другому классу.
func resolveDurationExpr(expr ast.Expr) (int64, bool) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return resolveDurationExpr(e.X)
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok || pkg.Name != "time" {
			return 0, false
		}
		unit, ok := durationUnits[e.Sel.Name]
		if !ok {
			return 0, false
		}
		return int64(unit), true
	case *ast.BasicLit:
		if e.Kind != token.INT {
			return 0, false
		}
		v, err := strconv.ParseInt(e.Value, 0, 64)
		if err != nil {
			return 0, false
		}
		return v, true
	case *ast.BinaryExpr:
		l, lok := resolveDurationExpr(e.X)
		r, rok := resolveDurationExpr(e.Y)
		if !lok || !rok {
			return 0, false
		}
		switch e.Op {
		case token.MUL:
			return l * r, true
		case token.ADD:
			return l + r, true
		case token.SUB:
			return l - r, true
		default:
			return 0, false
		}
	default:
		return 0, false
	}
}

// renderDurationExpr — плоское представление выражения для текста отказа.
func renderDurationExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return "(" + renderDurationExpr(e.X) + ")"
	case *ast.SelectorExpr:
		return renderDurationExpr(e.X) + "." + e.Sel.Name
	case *ast.Ident:
		return e.Name
	case *ast.BasicLit:
		return e.Value
	case *ast.BinaryExpr:
		return renderDurationExpr(e.X) + " " + e.Op.String() + " " + renderDurationExpr(e.Y)
	case *ast.CallExpr:
		var args []string
		for _, a := range e.Args {
			args = append(args, renderDurationExpr(a))
		}
		return renderDurationExpr(e.Fun) + "(" + strings.Join(args, ", ") + ")"
	default:
		return "<выражение>"
	}
}
