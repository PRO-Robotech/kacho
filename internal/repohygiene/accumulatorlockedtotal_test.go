// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/token"
	"sort"
)

// accumulatorlockedtotal_test.go — вторая разновидность накопителя для переписи
// [TestDeclaredAccumulatorsHaveANonTestReader]: итог ПОД ЗАМКОМ (kacho#2740).
//
// # Почему разновидность вторая, а не исключение первой
//
// Перепись ключевалась на признаках атомарного накопителя: поле `atomic.*` и
// экспортируемый слепок, читающий его `.Load()`. Докладчик окна журнала края
// (`gateway/internal/middleware/introspection_failure_report.go`) держит итог
// иначе — целым полем под `sync.Mutex` — и отдаёт его неэкспортируемым методом
// прямо в строку журнала. Признаков первой разновидности у него нет ни одного,
// поэтому «ноль находок» означал здесь «целый вид не прочитан»: строка журнала
// называет ПЕРВОЕ событие окна, а продолжительность состояния не видна нигде.
//
// # ЧТО СЧИТАЕТСЯ ИТОГОМ ПОД ЗАМКОМ — предикат, а не список
//
// Структурный тип, у которого есть поле `sync.Mutex`/`sync.RWMutex` (именованное
// или встроенное) и целое поле F, такое что:
//
//   - F увеличивается через получателя (`r.F++` либо `r.F += …`) в методе,
//     берущем замок того же получателя (`r.mu.Lock()` либо встроенный `r.Lock()`):
//     величина копится на горячем пути многими горутинами;
//   - F никогда не присваивается и не уменьшается (`r.F = …`, `r.F--`, `-=`):
//     это итог «с запуска», а не состояние «сколько подряд сейчас» — у
//     состояния нуль значит другое, и двусмыслицы гейта у него нет;
//   - F ОТДАЁТСЯ результатом хотя бы одного метода (`return r.F`,
//     `return uint64(r.F)`, `return Stats{N: r.F}`): величина выходит из типа.
//     Номер последовательности, уходящий ключом карты, величиной не является.
//
// Отдающие методы — и экспортируемые, и нет: именно неэкспортируемый отдающий
// метод и есть признак разновидности (итог видит только свой пакет).
//
// # ТОЛЬКО КОД, ПОДНИМАЕМЫЙ ПРОЦЕССОМ
//
// Судится носитель из пакета, достижимого импортами от какого-нибудь `package
// main` дерева. Дублёры портов (`portmock`, `repomock`) лежат в не-тестовых
// файлах и держат счёт вызовов под замком законно: его читает проба, а в
// процесс платформы дублёр не попадает. Требовать от них клетки на приборе
// значило бы требовать прибора у кода, который никогда не поднимается. Вычет
// выведен из графа импортов, а не из имени каталога, и напечатан переписью
// отдельным числом.
//
// # ЧТО СЧИТАЕТСЯ КЛЕТКОЙ
//
// То же требование, что у первой разновидности ([readerOf]): ссылка на ЛЮБОЙ
// отдающий метод из не-тестового пакета, который импортирует объявителя.
// Неэкспортируемый метод снаружи не назвать by construction — поэтому докладчик,
// чей итог уходит только в свою строку журнала, остаётся находкой, пока у него
// не появится экспортируемый слепок, читаемый поверхностью сбора.

// integerTypes — целые типы, способные нести итог.
var integerTypes = map[string]bool{
	"int": true, "int64": true, "int32": true, "uint": true, "uint64": true, "uint32": true,
}

// lockedTypeShape — поля одного структурного типа, существенные для предиката.
type lockedTypeShape struct {
	where     string          // файл:строка объявления типа
	pkg       string          // имя пакета
	mutexes   map[string]bool // имена полей-замков
	embedded  bool            // замок встроен (`sync.Mutex` без имени)
	integers  map[string]bool // целые поля
	increased map[string]bool // F увеличивается под замком получателя
	written   map[string]bool // F присваивается или уменьшается — состояние, не итог
	handOuts  map[string]map[string]bool
}

// isSyncLock — `sync.Mutex` либо `sync.RWMutex`.
func isSyncLock(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "sync" && (sel.Sel.Name == "Mutex" || sel.Sel.Name == "RWMutex")
}

// recvField — имя поля, если выражение есть `<recv>.<поле>`.
func recvField(e ast.Expr, recv string) (string, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != recv {
		return "", false
	}
	return sel.Sel.Name, true
}

// processDirs — каталоги пакетов, достижимые импортами от `package main`.
func processDirs(facts []goFileFacts) map[string]bool {
	importsOf := map[string]map[string]bool{}
	var queue []string
	reached := map[string]bool{}
	for _, f := range facts {
		if importsOf[f.dir] == nil {
			importsOf[f.dir] = map[string]bool{}
		}
		for dir := range f.imports {
			importsOf[f.dir][dir] = true
		}
		if f.pkgName == "main" && !reached[f.dir] {
			reached[f.dir] = true
			queue = append(queue, f.dir)
		}
	}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		for next := range importsOf[dir] {
			if !reached[next] {
				reached[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reached
}

// collectLockedTotals — носители итога под замком в коде, поднимаемом процессом.
func collectLockedTotals(facts []goFileFacts) []accumulator {
	accs, _ := collectLockedTotalsCensus(facts)
	return accs
}

// collectLockedTotalsCensus — то же плюс число носителей, вычтенных как не
// попадающие в процесс (дублёры для проб).
func collectLockedTotalsCensus(facts []goFileFacts) (out []accumulator, outsideProcess int) {
	shapes := map[string]*lockedTypeShape{} // «каталог.тип»
	for _, f := range facts {
		for _, decl := range f.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				shape := &lockedTypeShape{
					where: f.rel + ":" + fmtLine(f.fset, ts.Pos()), pkg: f.pkgName,
					mutexes: map[string]bool{}, integers: map[string]bool{},
					increased: map[string]bool{}, written: map[string]bool{},
					handOuts: map[string]map[string]bool{},
				}
				for _, fld := range st.Fields.List {
					switch {
					case isSyncLock(fld.Type) && len(fld.Names) == 0:
						shape.embedded = true
					case isSyncLock(fld.Type):
						for _, nm := range fld.Names {
							shape.mutexes[nm.Name] = true
						}
					default:
						if id, ok := fld.Type.(*ast.Ident); ok && integerTypes[id.Name] {
							for _, nm := range fld.Names {
								shape.integers[nm.Name] = true
							}
						}
					}
				}
				if (len(shape.mutexes) > 0 || shape.embedded) && len(shape.integers) > 0 {
					shapes[f.dir+"."+ts.Name.Name] = shape
				}
			}
		}
	}
	if len(shapes) == 0 {
		return nil, 0
	}

	for _, f := range facts {
		for _, decl := range f.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 || fd.Body == nil {
				continue
			}
			typeName := receiverTypeName(fd.Recv.List[0].Type)
			shape := shapes[f.dir+"."+typeName]
			if shape == nil {
				continue
			}
			recv := fd.Recv.List[0].Names[0].Name
			locks, increments := false, map[string]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch s := n.(type) {
				case *ast.CallExpr:
					sel, ok := s.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Lock" {
						return true
					}
					if mu, ok := recvField(sel.X, recv); ok && shape.mutexes[mu] {
						locks = true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == recv && shape.embedded {
						locks = true
					}
				case *ast.IncDecStmt:
					if fld, ok := recvField(s.X, recv); ok && shape.integers[fld] {
						if s.Tok == token.INC {
							increments[fld] = true
						} else {
							shape.written[fld] = true
						}
					}
				case *ast.AssignStmt:
					for _, lhs := range s.Lhs {
						fld, ok := recvField(lhs, recv)
						if !ok || !shape.integers[fld] {
							continue
						}
						if s.Tok == token.ADD_ASSIGN {
							increments[fld] = true
						} else {
							shape.written[fld] = true
						}
					}
				case *ast.ReturnStmt:
					for _, res := range s.Results {
						ast.Inspect(res, func(m ast.Node) bool {
							if e, ok := m.(ast.Expr); ok {
								if fld, ok := recvField(e, recv); ok && shape.integers[fld] {
									if shape.handOuts[fld] == nil {
										shape.handOuts[fld] = map[string]bool{}
									}
									shape.handOuts[fld][fd.Name.Name] = true
								}
							}
							return true
						})
					}
				}
				return true
			})
			if locks {
				for fld := range increments {
					shape.increased[fld] = true
				}
			}
		}
	}

	inProcess := processDirs(facts)
	for key, shape := range shapes {
		methods := map[string]bool{}
		var totals []string
		for fld := range shape.increased {
			if shape.written[fld] || len(shape.handOuts[fld]) == 0 {
				continue
			}
			totals = append(totals, fld)
			for m := range shape.handOuts[fld] {
				methods[m] = true
			}
		}
		if len(totals) == 0 {
			continue
		}
		dir, typ := splitTypeKey(key)
		if !inProcess[dir] {
			outsideProcess++
			continue
		}
		sort.Strings(totals)
		handOuts := make([]string, 0, len(methods))
		for m := range methods {
			handOuts = append(handOuts, m)
		}
		sort.Strings(handOuts)
		accessor := ""
		for i, m := range handOuts {
			if i > 0 {
				accessor += "|"
			}
			accessor += m
		}
		out = append(out, accumulator{
			dir: dir, pkg: shape.pkg, typ: typ, accessor: accessor,
			where: shape.where, handOuts: handOuts, totals: totals,
		})
	}
	return out, outsideProcess
}

// splitTypeKey — «каталог.тип» обратно в пару. Каталог может содержать точки
// (`gateway/internal/foo.v2`), имя типа — нет, поэтому делится ПОСЛЕДНЯЯ точка.
func splitTypeKey(key string) (dir, typ string) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '.' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}
