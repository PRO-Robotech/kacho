// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// presentation_points_gate_test.go — гейт F4d-28 (Ф3-52): читатель отсечки
// стоит на КАЖДОЙ точке предъявления края.
//
// # Предмет
//
// Точка предъявления — место, где край читает нашу сессию по носителю
// (`ResolveHumanSession`). Ответ о сессии отсечку НЕ применяет (Р7): её
// спрашивают отдельно (`SessionCutoffOf`) и сравнивают на крае. Значит точка
// предъявления без вопроса об отсечке — сессия, которую принудительный выход
// администратора не гасит на этом пути. Сегодня точек две — полоса личности и
// маршрут «кто я»; они ВЛОЖЕНЫ (Д13), и гейт считает точки, а не вызовы.
//
// # Что судится
//
// Функция, чьё тело зовёт `ResolveHumanSession`, — точка. Она несёт читателя,
// если `SessionCutoffOf` зовётся в её теле либо в теле функции ТОГО ЖЕ пакета,
// достижимой из неё прямыми вызовами (полоса спрашивает через
// `sessionCutoffCheck`, обработчик — через `sessionRevoked`). Законный близнец —
// читатель отсечки ВНЕ точки предъявления: уборщик открытых потоков
// (`streamrevocation`) читает отсечку, не читая сессии, и точкой не является.
// Глаголы формы точками тоже не являются: их запрос проходит полосу личности
// ДО ретрансляции, а ретранслятор читателя не несёт по построению (Ф3-52).
//
// Судится дерево синтаксиса; перепись печатает «точек N · несущих читателя M»;
// пустой обход — отказ. Способность упасть доказана инъекцией на синтетике.
package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	presentationCall = "ResolveHumanSession"
	cutoffCall       = "SessionCutoffOf"
)

// presentationPoint — одна точка предъявления и вердикт по ней.
type presentationPoint struct {
	pkg, fn, pos string
	hasReader    bool
}

// judgePresentationPoints — судья над разобранными файлами: пакет → файлы.
func judgePresentationPoints(fset *token.FileSet, byPkg map[string]map[string]*ast.File) (points []presentationPoint, filesRead int) {
	for pkg, files := range byPkg {
		// Граф прямых вызовов внутри пакета: имя функции → имена вызванных функций
		// (селекторы и голые идентификаторы), плюс где стоит нужный вызов.
		type fnInfo struct {
			calls    map[string]bool
			presents bool
			reads    bool
			pos      token.Pos
		}
		fns := map[string]*fnInfo{}
		for _, f := range files {
			filesRead++
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				info := &fnInfo{calls: map[string]bool{}, pos: fd.Pos()}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					var name string
					switch fun := call.Fun.(type) {
					case *ast.SelectorExpr:
						name = fun.Sel.Name
					case *ast.Ident:
						name = fun.Name
					}
					switch name {
					case presentationCall:
						info.presents = true
					case cutoffCall:
						info.reads = true
					default:
						info.calls[name] = true
					}
					return true
				})
				fns[fd.Name.Name] = info
			}
		}
		// Достижимость читателя из точки — по прямым вызовам того же пакета.
		var reachesReader func(name string, seen map[string]bool) bool
		reachesReader = func(name string, seen map[string]bool) bool {
			info, ok := fns[name]
			if !ok || seen[name] {
				return false
			}
			seen[name] = true
			if info.reads {
				return true
			}
			for callee := range info.calls {
				if reachesReader(callee, seen) {
					return true
				}
			}
			return false
		}
		for name, info := range fns {
			if !info.presents {
				continue
			}
			points = append(points, presentationPoint{
				pkg: pkg, fn: name, pos: fset.Position(info.pos).String(),
				hasReader: reachesReader(name, map[string]bool{}),
			})
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].pos < points[j].pos })
	return points, filesRead
}

// parseGatewayInternal разбирает все не-тестовые Go-файлы под gateway/internal.
func parseGatewayInternal(t *testing.T) (*token.FileSet, map[string]map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	byPkg := map[string]map[string]*ast.File{}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		dir := filepath.Dir(p)
		if byPkg[dir] == nil {
			byPkg[dir] = map[string]*ast.File{}
		}
		byPkg[dir][p] = f
		return nil
	})
	if err != nil {
		t.Fatalf("обход gateway/internal: %v", err)
	}
	return fset, byPkg
}

func TestPresentationPoints_F3_52_EveryPointCarriesTheCutoffReader(t *testing.T) {
	fset, byPkg := parseGatewayInternal(t)
	points, filesRead := judgePresentationPoints(fset, byPkg)
	if filesRead == 0 {
		t.Fatal("прочитано 0 файлов — обход пуст")
	}
	if len(points) == 0 {
		t.Fatalf("точек предъявления 0 при %d прочитанных файлах — читателя сессии у края нет, и молчание гейта ничего не утверждает", filesRead)
	}
	with := 0
	for _, p := range points {
		if !p.hasReader {
			t.Errorf("точка предъявления %s (%s) читает сессию и НЕ спрашивает отсечку — принудительный выход на этом пути не действует (F4d-28)", p.fn, p.pos)
			continue
		}
		with++
	}
	t.Logf("перепись: файлов прочитано %d · точек предъявления %d · несущих читателя %d", filesRead, len(points), with)
	// Сегодня точек ДВЕ — полоса личности и «кто я» (Ф3-52). Число названо
	// потому, что третья точка есть предмет решения, а не побочный эффект.
	if len(points) != 2 {
		names := make([]string, 0, len(points))
		for _, p := range points {
			names = append(names, p.fn+" ("+p.pos+")")
		}
		t.Fatalf("точек предъявления %d, ожидалось 2 (полоса личности и «кто я»): %v", len(points), names)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в обе стороны — синтетика.

func judgeSyntheticPoints(t *testing.T, src string) []presentationPoint {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("синтетика не разбирается: %v", err)
	}
	points, _ := judgePresentationPoints(fset, map[string]map[string]*ast.File{"p": {"p.go": f}})
	return points
}

const pointsFixture = `package p
type R interface{ ResolveHumanSession(string) (string, bool, error) }
type C interface{ SessionCutoffOf(string) (int, bool, error) }
type lane struct{ r R; c C }
func (l lane) check(id string) bool { _, f, _ := l.c.SessionCutoffOf(id); return f }
func (l lane) lane(b string) bool { s, _, _ := l.r.ResolveHumanSession(b); return l.check(s) }
func (l lane) whoami(b string) bool { s, _, _ := l.r.ResolveHumanSession(b); _, f, _ := l.c.SessionCutoffOf(s); return f }
func (l lane) sweeper(id string) bool { _, f, _ := l.c.SessionCutoffOf(id); return f }
%s
`

// Инъекция: третья точка без вопроса об отсечке — красное С КООРДИНАТОЙ.
func TestPresentationPointsGate_Injection_AThirdPointWithoutTheReaderIsNamed(t *testing.T) {
	src := strings.Replace(pointsFixture, "%s",
		"func (l lane) relay(b string) bool { _, f, _ := l.r.ResolveHumanSession(b); return f }", 1)
	points := judgeSyntheticPoints(t, src)
	if len(points) != 3 {
		t.Fatalf("точек %d, ожидалось 3", len(points))
	}
	var bad []string
	for _, p := range points {
		if !p.hasReader {
			bad = append(bad, p.fn+" "+p.pos)
		}
	}
	if len(bad) != 1 || !strings.HasPrefix(bad[0], "relay ") || !strings.Contains(bad[0], "p.go:") {
		t.Fatalf("внесённая точка без читателя не названа координатой: %v", bad)
	}
}

// Близнец: читатель отсечки вне точки предъявления (уборщик) — молчит; две
// законные точки (прямой и транзитивный читатель) — несут читателя.
func TestPresentationPointsGate_Twin_AReaderOutsideAPointIsSilent(t *testing.T) {
	points := judgeSyntheticPoints(t, strings.Replace(pointsFixture, "%s", "", 1))
	if len(points) != 2 {
		t.Fatalf("точек %d, ожидалось 2 (уборщик точкой не является)", len(points))
	}
	for _, p := range points {
		if !p.hasReader {
			t.Fatalf("законная точка %s объявлена без читателя — ложная находка", p.fn)
		}
	}
}

func TestPresentationPointsGate_EmptyWalkIsARefusal(t *testing.T) {
	points := judgeSyntheticPoints(t, "package p\n")
	if len(points) != 0 {
		t.Fatalf("на пустом дереве точек %d", len(points))
	}
	// Отказ на пустом обходе производит сам гейт (t.Fatal при нуле точек);
	// судья честно отдаёт ноль, и это утверждается здесь, чтобы пустота не
	// сошла за «все точки несут читателя».
}
