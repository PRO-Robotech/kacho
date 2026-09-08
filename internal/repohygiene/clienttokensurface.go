// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttokensurface.go — на КАКОЙ объявленной поверхности зарегистрирован
// путь эндпоинта (приёмка F2, сценарий F2-45).
//
// # Что здесь стережётся и почему обзора диффа недостаточно
//
// Регистрация пути на муксе — одна строка, и по ней не видно, куда этот мукс
// уедет. Досягаемость решается не здесь, а на полсотни строк ниже, в записи
// поверхности: тот же самый обработчик объявляется внешним либо внутренним.
// Строка регистрации при этом читается одинаково в обоих случаях.
//
// Поэтому разбор связывает ДВА места: место, где путь регистрируется, и место,
// где объявляется досягаемость несущей его поверхности. Перечень поверхностей
// ВЫВОДИТСЯ из объявлений корня, а не выписывается: утверждение о единственном
// маршруте оставалось бы зелёным, уедь второй не туда.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **регистрация через значение-переменную** (`p := clienttokenhttp.TokenPath;
//     mux.Handle(p, h)`). Разбор судит вызов по месту, а не по потоку значений;
//     такая форма попадает в перепись как НЕРАЗОБРАННАЯ, а не молча исчезает.
//  2. **обёртка над муксом** (`wrap(mux).Handle(...)`) — получатель вызова не
//     идентификатор, связать его с поверхностью нечем. Тоже перепись.
//  3. **досягаемость, вычисленная в рантайме** — запись поверхности, у которой
//     `Reach` не константа пакета, а выражение. Разбор объявляет такую запись
//     НЕОБЪЯВЛЕННОЙ и считает её находкой: досягаемость, которую нельзя
//     прочитать глазами, нельзя и проверить.
//
// Первые две — границы инструмента, и они печатаются числом. Третья — находка.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SurfaceRegistration — один зарегистрированный путь вместе с досягаемостью
// поверхности, на которую он в итоге уехал.
type SurfaceRegistration struct {
	// Path — как путь назван в исходнике: `<пакет>.<константа>`.
	Path string
	// Mux — идентификатор, на котором вызван Handle.
	Mux string
	// SurfaceName — имя объявленной поверхности; пусто, если связать не удалось.
	SurfaceName string
	// Reach — объявленная досягаемость: `ReachExternal` / `ReachClusterInternal`.
	Reach string
	// Pos — координата регистрации.
	Pos string
}

// SurfaceCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного».
type SurfaceCensus struct {
	Files         int
	Surfaces      int
	Registrations int
	// Unlinked — регистрации, которые не удалось связать с поверхностью. Это
	// граница инструмента, и она печатается, а не молчит.
	Unlinked int
}

// ScanSurfaceRegistrations разбирает композиционный корень и связывает каждую
// регистрацию пути с досягаемостью несущей его поверхности.
//
// Возвращает ещё и findings — записи поверхностей, чья досягаемость объявлена
// не константой: их нельзя прочитать, значит нельзя и проверить.
func ScanSurfaceRegistrations(dir string) (regs []SurfaceRegistration, census SurfaceCensus, findings []string, err error) {
	fset := token.NewFileSet()

	// Каталог обходится сам, а не разборщиком каталогов: тот связывает файлы с
	// пакетами, не читая условий сборки, и о такой границе объявляет сам. Здесь
	// предмет — файлы композиционного корня, и связывать их с пакетами не надо.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, census, nil, fmt.Errorf("repohygiene: состав %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	// handlerToSurface — идентификатор, отданный полем Handler, → объявленная
	// поверхность.
	handlerToSurface := map[string]SurfaceRegistration{}
	// alias — присваивание `left = right` между идентификаторами: мукс уезжает
	// в переменную обработчика, и связать одно с другим можно только так.
	alias := map[string]string{}

	var files []*ast.File
	for _, n := range names {
		f, perr := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if perr != nil {
			return nil, census, nil, fmt.Errorf("repohygiene: разбор %s: %w", n, perr)
		}
		files = append(files, f)
	}
	census.Files = len(files)

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isSurfaceLiteral(lit) {
				return true
			}
			census.Surfaces++
			var handler, reach, name string
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, _ := kv.Key.(*ast.Ident)
				if key == nil {
					continue
				}
				switch key.Name {
				case "Handler":
					if id, ok := kv.Value.(*ast.Ident); ok {
						handler = id.Name
					}
				case "Reach":
					sel, ok := kv.Value.(*ast.SelectorExpr)
					if !ok {
						// Досягаемость, которую нельзя прочитать глазами,
						// нельзя и проверить.
						findings = append(findings, fmt.Sprintf(
							"%s: досягаемость поверхности объявлена не константой пакета", fset.Position(kv.Pos())))
						return true
					}
					reach = sel.Sel.Name
				case "Name":
					if bl, ok := kv.Value.(*ast.BasicLit); ok {
						name = strings.Trim(bl.Value, `"`)
					}
				}
			}
			if handler != "" {
				handlerToSurface[handler] = SurfaceRegistration{SurfaceName: name, Reach: reach}
			}
			return true
		})

		ast.Inspect(f, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}
			l, lok := as.Lhs[0].(*ast.Ident)
			r, rok := as.Rhs[0].(*ast.Ident)
			if lok && rok {
				alias[r.Name] = l.Name
			}
			return true
		})
	}

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Handle" {
				return true
			}
			mux, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			pathSel, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				// Путь, поданный переменной либо литералом: связать его с
				// объявленной константой нечем.
				census.Registrations++
				census.Unlinked++
				return true
			}
			pkgIdent, ok := pathSel.X.(*ast.Ident)
			if !ok {
				census.Registrations++
				census.Unlinked++
				return true
			}
			census.Registrations++

			reg := SurfaceRegistration{
				Path: pkgIdent.Name + "." + pathSel.Sel.Name,
				Mux:  mux.Name,
				Pos:  fset.Position(call.Pos()).String(),
			}
			// Мукс мог уехать в переменную обработчика — идём по цепочке
			// присваиваний, но не бесконечно: цикл в присваиваниях возможен, и
			// зависать на нём проверка не вправе.
			cur := mux.Name
			for hop := 0; hop < 8; hop++ {
				if s, ok := handlerToSurface[cur]; ok {
					reg.SurfaceName, reg.Reach = s.SurfaceName, s.Reach
					break
				}
				next, ok := alias[cur]
				if !ok || next == cur {
					break
				}
				cur = next
			}
			if reg.Reach == "" {
				census.Unlinked++
			}
			regs = append(regs, reg)
			return true
		})
	}
	sort.Slice(regs, func(i, j int) bool { return regs[i].Pos < regs[j].Pos })
	return regs, census, findings, nil
}

// isSurfaceLiteral отвечает, является ли составной литерал записью поверхности.
//
// Судит по ИМЕНИ ТИПА, а не по составу полей: литерал с теми же полями и другим
// типом поверхностью не является, а поверхность с новым полем ею быть не
// перестаёт.
func isSurfaceLiteral(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Surface"
}

// ExternalReach — имя константы досягаемости «извне кластера».
const ExternalReach = "ReachExternal"

// RegistrationsOf отбирает регистрации названного пути.
func RegistrationsOf(regs []SurfaceRegistration, path string) []SurfaceRegistration {
	var out []SurfaceRegistration
	for _, r := range regs {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// CompositionRootDir — каталог композиционного корня iam.
func CompositionRootDir(repoRoot string) string {
	return filepath.Join(repoRoot, "services", "iam", "cmd", "kaname")
}
