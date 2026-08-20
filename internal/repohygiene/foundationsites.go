// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

// Место сборки сервера — единица счёта для возможностей, которые провязываются
// В КОНКРЕТНЫЙ слушатель.
//
// # Почему каталога НЕДОСТАТОЧНО, и это измерено, а не предположено
//
// Первая редакция этой переписи считала клетку по КАТАЛОГУ: разбор всех
// не-тестовых файлов каталога сливался в один набор селекторов, и возможность
// считалась усыновлённой, если её вызывал ХОТЬ КТО-НИБУДЬ внутри. На каталоге с
// одним слушателем это верно. На каталоге с двумя — нет: снятие звена у одного
// из двух серверов оставляет второй, и объединение по каталогу отвечает «несёт»
// за оба.
//
// Замер по дереву: каталогов-слушателей 8, мест сборки сервера 10. Два каталога
// держат по два независимо собранных сервера (край и iam); остальные шесть
// поднимают пару одним замыканием носителя, поэтому у них частичная потеря
// невозможна ПО ПОСТРОЕНИЮ, а не по внимательности.
//
// Инъекция, которой это доказано (`foundationadoption_injection_test.go`,
// `TestFoundationGateNamesTheServerSiteThatDroppedTheWiring`): снятие
// восстановления паники у ВНУТРЕННЕГО сервера iam при целом публичном. По
// каталогу — молчание; по месту сборки — ровно одна находка с координатой.
// Внутренний слушатель без восстановления паники — это `security.md`
// §«AuthN+AuthZ ВЕЗДЕ»: «Internal (:9091) НЕ освобождён».
//
// # Что такое место сборки
//
// Вызов точки входа, СОБИРАЮЩЕЙ сервер: голого конструктора библиотеки либо
// обёртки над ним (`FoundationWrapper`). Вызовы ВНУТРИ каталога обёртки местами
// сборки не считаются — это её внутренности, а её собственное употребление уже
// посчитано у вызывающего. Иначе один слушатель считался бы дважды: один раз в
// точке решения о провязке и второй раз внутри обёртки, где решать нечего.

// FoundationSite — одно место сборки сервера.
type FoundationSite struct {
	// ID — имя в переписи: `<каталог>#<файл>:<имя>`. Строится из имени файла и
	// того, чем связан результат, а НЕ из номера строки: номер сдвигается любой
	// правкой рядом, и запись ведомости, названная им, протухала бы молча.
	ID string
	// Dir — каталог-слушатель, которому место принадлежит.
	Dir string
	// File — путь файла от корня репозитория, Line — строка вызова. Обе — для
	// текста находки: она обязана называть координату, а не только каталог.
	File string
	Line int
	// Entry — селектор точки входа (`grpcsrv.NewServer`, `servicehost.Serve`…).
	Entry string
	// Slice — СРЕЗ ПРОВЯЗКИ этого места: что доезжает до этого сервера, а не до
	// каталога вообще. Как он берётся и чего не умеет — см. sliceAtSite.
	Slice *FoundationScan
}

// FoundationWrapper — каталог, оборачивающий сборку сервера.
//
// Объявляется ПОИМЁННО и с причиной. Объявление имеет два следствия сразу, и
// оба обязаны быть верны: (а) вызовы конструктора ВНУТРИ этого каталога местами
// сборки не считаются; (б) вызов самой обёртки местом сборки СЧИТАЕТСЯ. Одно без
// другого дало бы либо двойной счёт, либо потерю слушателя.
type FoundationWrapper struct {
	// Dir — каталог обёртки от корня репозитория.
	Dir string
	// Entry — селектор, которым обёртку зовут снаружи.
	Entry string
	// Why — зачем обёртка существует, коротко.
	Why string
}

// serverSiteMarkers собирает полный набор точек входа: голые конструкторы
// библиотеки плюс все объявленные обёртки.
//
// Перечень ВЫВОДИТСЯ из набора обёрток, а не выписывается рядом с ним: два
// рукописных списка об одном предмете разошлись бы молча, и разошлись бы они
// именно там, где обёртку завели, а в маркеры вписать забыли, — то есть
// слушатель исчез бы из переписи, выглядя усыновившим всё.
func serverSiteMarkers(base []string, wrappers []FoundationWrapper) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range base {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	for _, w := range wrappers {
		if w.Entry != "" && !seen[w.Entry] {
			seen[w.Entry] = true
			out = append(out, w.Entry)
		}
	}
	sort.Strings(out)
	return out
}

// DiscoverServerSites находит места сборки сервера в каталогах-слушателях.
//
// Каталог, признанный слушателем по улике в своём разборе, обязан дать хотя бы
// одно место: ноль означает, что улика есть, а точки решения о провязке гейт не
// нашёл — например, объявление обёртки поглотило её. Это ОТКАЗ, а не тишина:
// иначе объявлением обёртки можно было бы вывести слушателя из переписи, и он
// выглядел бы усыновившим всё.
func DiscoverServerSites(root string, dirs []string, markers []string,
	wrappers []FoundationWrapper) ([]FoundationSite, error) {

	markerSet := map[string]bool{}
	for _, m := range markers {
		markerSet[m] = true
	}
	fset := token.NewFileSet()
	// Объявления функций пакета — по каталогу файла (в Go каталог и есть пакет).
	// Нужны срезу: величина, доезжающая до места сборки, часто собирается
	// СОСЕДНЕЙ функцией того же пакета, и срез, не умеющий в неё зайти, объявил
	// бы провязку отсутствующей — ложная находка ровно там, где сделано верно.
	pkgFuncs := map[string]map[string]*ast.FuncDecl{}

	var out []FoundationSite
	for _, dir := range dirs {
		before := len(out)
		dirAbs := path.Join(root, dir)
		err := rootedWalk(dirAbs,
			func(rel string) bool {
				return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
			},
			func(abs string, body []byte) error {
				rel := strings.TrimPrefix(strings.TrimPrefix(abs, root), "/")
				if wrapperOwns(rel, wrappers) {
					return nil
				}
				// ParseComments не запрашиваем: упоминание точки входа в
				// объяснении местом сборки не является.
				f, perr := parser.ParseFile(fset, abs, body, parser.SkipObjectResolution)
				if perr != nil {
					return fmt.Errorf("%s: разбор не удался: %w", abs, perr)
				}
				pkgDir := path.Dir(rel)
				if _, ok := pkgFuncs[pkgDir]; !ok {
					fns, ferr := packageFuncs(root, pkgDir, fset)
					if ferr != nil {
						return ferr
					}
					pkgFuncs[pkgDir] = fns
				}
				sites, serr := sitesInFile(fset, f, rel, dir, markerSet, pkgFuncs[pkgDir])
				if serr != nil {
					return serr
				}
				out = append(out, sites...)
				return nil
			})
		if err != nil {
			return nil, err
		}
		if len(out) == before {
			return nil, fmt.Errorf("каталог %s признан слушателем (в его разборе есть улика сборки "+
				"сервера), а мест сборки в нём не найдено ни одного: улика и точка решения о "+
				"провязке разошлись — вероятнее всего, объявление обёртки поглотило слушателя, и "+
				"он выглядел бы усыновившим всё", dir)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// wrapperOwns — лежит ли файл внутри каталога объявленной обёртки.
func wrapperOwns(rel string, wrappers []FoundationWrapper) bool {
	for _, w := range wrappers {
		if w.Dir != "" && (rel == w.Dir || strings.HasPrefix(rel, w.Dir+"/")) {
			return true
		}
	}
	return false
}

// packageFuncs собирает объявления функций одного каталога-пакета.
func packageFuncs(root, pkgDir string, fset *token.FileSet) (map[string]*ast.FuncDecl, error) {
	out := map[string]*ast.FuncDecl{}
	err := rootedWalk(path.Join(root, pkgDir),
		func(rel string) bool {
			// Только сам каталог, без вложенных: вложенный — другой пакет.
			return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") &&
				!strings.Contains(rel, "/")
		},
		func(abs string, body []byte) error {
			f, perr := parser.ParseFile(fset, abs, body, parser.SkipObjectResolution)
			if perr != nil {
				return fmt.Errorf("%s: разбор не удался: %w", abs, perr)
			}
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				// Методы пропускаем: имя метода не именует величину в срезе,
				// и одноимённые методы разных типов слились бы в один.
				if ok && fd.Recv == nil && fd.Name != nil {
					out[fd.Name.Name] = fd
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sitesInFile находит вызовы точек входа в одном файле.
func sitesInFile(fset *token.FileSet, f *ast.File, rel, dir string,
	markerSet map[string]bool, pkgFuncs map[string]*ast.FuncDecl) ([]FoundationSite, error) {

	var out []FoundationSite
	var stack []ast.Node
	var failure error
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		entry := x.Name + "." + sel.Sel.Name
		if !markerSet[entry] {
			return true
		}
		fn := enclosingFunc(stack)
		if fn == nil {
			failure = fmt.Errorf("%s:%d: сервер собирается вне функции — срез провязки для такого "+
				"места построить не из чего", rel, fset.Position(call.Lparen).Line)
			return false
		}
		bound := boundName(stack, call, len(out)+1)
		out = append(out, FoundationSite{
			ID:    dir + "#" + path.Base(rel) + ":" + bound,
			Dir:   dir,
			File:  rel,
			Line:  fset.Position(call.Lparen).Line,
			Entry: entry,
			Slice: sliceAtSite(entry, call, bound, fn, pkgFuncs),
		})
		return true
	})
	if failure != nil {
		return nil, failure
	}
	return out, nil
}

// absorbUses поглощает то, ЧЕМ ОБЁРНУТ уже собранный сервер.
//
// Предмет — возможность, которая ставится не В конструктор, а ПОВЕРХ
// регистрации: `register…(лимитер.Registrar(srv), …)`. В аргументах конструктора
// её нет вовсе, поэтому без этого прохода слушатель, обернувший регистрацию,
// читался бы как не усыновивший — ложная находка ровно там, где сделано верно.
//
// Берётся ТОЛЬКО обёртывающее выражение (`w.Fun` вызова, в чьи аргументы попал
// сервер), а не список аргументов целиком. Разница несущая: аргументы соседних
// вызовов («погасить сервер за срок», «зарегистрировать службы с пулом и
// журналом») втянули бы в срез половину композиционного корня, и срез перестал
// бы различать СОСЕДНИЕ серверы одного каталога — то есть ровно то свойство,
// ради которого единицей счёта выбрано место сборки, а не каталог.
func absorbUses(body *ast.BlockStmt, bound string, absorb func(ast.Node, bool)) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, a := range call.Args {
			if !mentionsIdent(a, bound) {
				continue
			}
			absorb(call.Fun, true)
			break
		}
		return true
	})
}

// mentionsIdent — встречается ли имя внутри выражения.
func mentionsIdent(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// enclosingFunc — ближайшее объявление функции вверх по стеку.
func enclosingFunc(stack []ast.Node) *ast.FuncDecl {
	for i := len(stack) - 1; i >= 0; i-- {
		if fd, ok := stack[i].(*ast.FuncDecl); ok {
			return fd
		}
	}
	return nil
}

// boundName — чем связан результат сборки.
//
// Имя, а не номер строки: правка соседней строки сдвинула бы номер, и запись
// ведомости, названная им, стала бы ложной без единой правки самого места.
// Результат, не связанный ничем, именуется порядковым номером в файле.
func boundName(stack []ast.Node, call *ast.CallExpr, ordinal int) string {
	for i := len(stack) - 1; i >= 0; i-- {
		switch v := stack[i].(type) {
		case *ast.AssignStmt:
			for j, rhs := range v.Rhs {
				if rhs == ast.Expr(call) && j < len(v.Lhs) {
					if id, ok := v.Lhs[j].(*ast.Ident); ok {
						return id.Name
					}
				}
			}
		case *ast.ValueSpec:
			for j, val := range v.Values {
				if val == ast.Expr(call) && j < len(v.Names) {
					return v.Names[j].Name
				}
			}
		}
	}
	return fmt.Sprintf("%d", ordinal)
}

// sliceAtSite — срез провязки: что доезжает ДО ЭТОГО сервера.
//
// # Как берётся
//
// От аргументов вызова назад по употреблениям: величины, поданные на вход,
// разрешаются присваиваниями в теле объемлющей функции, а вызванные функции
// того же пакета поглощаются целиком. Селектор самой точки входа кладётся в срез
// первым — иначе носитель, чьё имя и есть точка входа, не сопоставился бы.
//
// # Чем ограничен — названо, а не умолчано
//
//  1. Срез ПЕРЕОЦЕНИВАЕТ. Поглощая тело соседней функции, он засчитывает всё,
//     что та ставит, даже если на этом пути не ставит. Направление ошибки
//     выбрано осознанно: недооценка дала бы ЛОЖНЫЕ НАХОДКИ ровно там, где
//     провязка сделана верно через соседа, а гейт с ложными находками снимают
//     первым же, кто на них наткнётся.
//  2. Два сервера, собранные ЧЕРЕЗ ОДНУ И ТУ ЖЕ величину, срезом не
//     различаются: у них общий источник, и потеря звена в нём — потеря у обоих.
//     Это не слепота, а верный ответ: различать нечего.
//  3. Область разрешения имён — объемлющая функция и пакет её файла. Величина,
//     приехавшая из другого пакета параметром, разрешается поглощением обёртки,
//     а не сквозным анализом: сквозной потребовал бы графа вызовов всего дерева.
func sliceAtSite(entry string, call *ast.CallExpr, bound string, fn *ast.FuncDecl,
	pkgFuncs map[string]*ast.FuncDecl) *FoundationScan {

	sc := &FoundationScan{Imports: map[string]bool{}, Selects: map[string]bool{}, Calls: map[string]bool{}}
	sc.Selects[entry] = true

	pendingVals := map[string]bool{}  // имена величин — разрешаются в объемлющей функции
	pendingFuncs := map[string]bool{} // имена функций пакета — поглощаются телом
	doneVals := map[string]bool{}
	doneFuncs := map[string]bool{}

	// propagate=false для поглощённых тел: их локальные имена принадлежат ЧУЖОЙ
	// области, и разрешать их присваиваниями объемлющей функции значило бы
	// смешать две области видимости. Практическое следствие ровно одно и оно
	// крупное: без этого разделения общее имя ошибки (`err`) втянуло бы в срез
	// правые части всех присваиваний функции, то есть срез стал бы каталогом.
	absorb := func(n ast.Node, propagate bool) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(x ast.Node) bool {
			switch v := x.(type) {
			case *ast.SelectorExpr:
				if id, ok := v.X.(*ast.Ident); ok {
					sc.Selects[id.Name+"."+v.Sel.Name] = true
					if propagate && !doneVals[id.Name] {
						pendingVals[id.Name] = true
					}
				}
			case *ast.CallExpr:
				if id, ok := v.Fun.(*ast.Ident); ok {
					sc.Calls[id.Name] = true
					if !doneFuncs[id.Name] {
						pendingFuncs[id.Name] = true
					}
					if propagate && !doneVals[id.Name] {
						pendingVals[id.Name] = true
					}
				}
			case *ast.Ident:
				if propagate && !doneVals[v.Name] {
					pendingVals[v.Name] = true
				}
			}
			return true
		})
	}

	for _, a := range call.Args {
		absorb(a, true)
	}

	// Вторая половина среза: что с этим сервером ДЕЛАЮТ.
	//
	// Первая редакция читала только вход конструктора, и это было уже своего
	// предмета. Возможность фундамента доезжает до слушателя двумя разными
	// путями: одни ставятся В сервер (звенья цепочки, пределы транспорта) и
	// видны в его аргументах, другие оборачивают РЕГИСТРАТОР (потолок темпа) и в
	// аргументах не появляются вовсе. Слушатель, обернувший регистрацию, читался
	// как не усыновивший — ложная находка ровно там, где сделано верно.
	//
	// Берутся аргументы вызовов, УПОМИНАЮЩИХ имя сервера, — а не всё тело
	// функции: тело втянуло бы в срез каждую соседнюю величину, и переоценка из
	// оговорённой (поглощение соседней функции) стала бы безграничной.
	if bound != "" {
		absorbUses(fn.Body, bound, absorb)
	}

	for len(pendingVals) > 0 || len(pendingFuncs) > 0 {
		for name := range pendingVals {
			delete(pendingVals, name)
			if doneVals[name] {
				continue
			}
			doneVals[name] = true
			ast.Inspect(fn.Body, func(x ast.Node) bool {
				switch v := x.(type) {
				case *ast.AssignStmt:
					if identInList(v.Lhs, name) {
						for _, rhs := range v.Rhs {
							absorb(rhs, true)
						}
					}
				case *ast.ValueSpec:
					for _, id := range v.Names {
						if id.Name == name {
							for _, val := range v.Values {
								absorb(val, true)
							}
						}
					}
				}
				return true
			})
			break
		}
		for name := range pendingFuncs {
			delete(pendingFuncs, name)
			if doneFuncs[name] {
				continue
			}
			doneFuncs[name] = true
			if fd, ok := pkgFuncs[name]; ok && fd.Body != nil {
				absorb(fd.Body, false)
			}
			break
		}
	}
	return sc
}

// identInList — стоит ли имя среди левых частей присваивания.
func identInList(list []ast.Expr, name string) bool {
	for _, e := range list {
		if id, ok := e.(*ast.Ident); ok && id.Name == name {
			return true
		}
	}
	return false
}
