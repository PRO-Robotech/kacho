// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package publicauthzcensus

// index.go — разбор одного обслуживающего пакета и обход его вызовов.
//
// # Почему обход, а не чтение файла обработчика
//
// Вопрос о доступе задаёт не транспортный метод, а use-case, который он зовёт:
// у проекта дверь стоит в GetProjectUseCase.Execute, а обработчик — три строки
// переадресации. Проверка, читающая только тело обработчика, объявила бы дырой
// каждый RPC, чья дверь стоит там, где ей и положено стоять по раскладке слоёв.
//
// # Почему обход внутрипакетный, а не сквозной
//
// Обработчик и его use-case лежат в ОДНОМ пакете (api/project несёт и
// handler.go, и get.go) — это раскладка, а не совпадение. Поэтому обхода внутри
// пакета достаточно, а полный граф по модулю потребовал бы проверки типов всего
// дерева и своей зависимости. Граница названа честно: вопрос, вынесенный в
// сосед­ний пакет и не помеченный здесь, уйдёт в «не разрешилось» — третью
// категорию, — а не в норму и не в находку.
//
// # Приёмник разрешается по ТИПУ, а не по имени поля
//
// Имя поля `get` встречается в нескольких структурах пакета, и общий словарь
// «поле → тип» склеил бы разные use-case'ы. Приёмник берётся у объявления
// метода (`func (h *Handler) …` → Handler), поэтому `h.get` разрешается в поле
// ИМЕННО Handler'а.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// pkgIndex — разобранный обслуживающий пакет.
type pkgIndex struct {
	// funcs — функции уровня пакета по имени.
	funcs map[string]*ast.FuncDecl
	// methods — методы по ключу «Тип.Метод».
	methods map[string]*ast.FuncDecl
	// fields — поля структур: «Тип.поле» → имя типа поля (без указателя и пакета).
	fields map[string]string
	// handlerTypes — типы, на которых висят методы, названные как RPC. Обычно
	// один Handler; перечень выведен, а не выписан, потому что у части служб
	// обработчик назван иначе.
	handlerTypes map[string]bool
}

// indexPackage разбирает каталог пакета и возвращает индекс и число
// разобранных не-тестовых файлов.
func indexPackage(dir string) (*pkgIndex, int, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, notTestFile, parser.SkipObjectResolution)
	if err != nil {
		return nil, 0, err
	}
	idx := &pkgIndex{
		funcs:        map[string]*ast.FuncDecl{},
		methods:      map[string]*ast.FuncDecl{},
		fields:       map[string]string{},
		handlerTypes: map[string]bool{},
	}
	files := 0
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files++
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil || len(d.Recv.List) == 0 {
						idx.funcs[d.Name.Name] = d
						continue
					}
					recv, ok := receiverType(d.Recv.List[0].Type)
					if !ok {
						continue
					}
					idx.methods[recv+"."+d.Name.Name] = d
					if strings.Contains(recv, "Handler") {
						idx.handlerTypes[recv] = true
					}
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts, isTS := spec.(*ast.TypeSpec)
						if !isTS {
							continue
						}
						st, isStruct := ts.Type.(*ast.StructType)
						if !isStruct {
							continue
						}
						for _, field := range st.Fields.List {
							typeName, okT := bareTypeName(field.Type)
							if !okT {
								continue
							}
							for _, nm := range field.Names {
								idx.fields[ts.Name.Name+"."+nm.Name] = typeName
							}
						}
					}
				}
			}
		}
	}
	return idx, files, nil
}

// narrowsPage отвечает, сужает ли путь обслуживания страницу построчно.
//
// Спрашивается ТОЛЬКО у полосы `scope_filtered`: там дверь единичного вопроса
// не задаёт намеренно, и авторизация принадлежит обработчику.
//
// Второй результат — разрешился ли метод вообще. false означает «не
// выполнилось»: метода с таким именем на обработчике нет, и утверждать по нему
// что-либо нельзя ни в одну сторону — ни что дверь есть, ни что её нет.
func (p *pkgIndex) narrowsPage(rpcMethod string) (string, bool) {
	var entry *ast.FuncDecl
	var recv string
	for t := range p.handlerTypes {
		if fn, ok := p.methods[t+"."+rpcMethod]; ok {
			entry, recv = fn, t
			break
		}
	}
	if entry == nil {
		return "", false
	}
	return p.walk(entry, recv, map[string]bool{}, 0), true
}

const maxDepth = 12

// walk обходит тело функции и всё, что из неё достижимо внутри пакета, и
// возвращает имя найденного вызова сужения страницы. Пустая строка означает,
// что не найдено ни одного.
func (p *pkgIndex) walk(fn *ast.FuncDecl, recv string, seen map[string]bool, depth int) string {
	if fn == nil || fn.Body == nil || depth > maxDepth {
		return ""
	}
	evidence := ""
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if evidence != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, isQualified := qualifiedCallName(call.Fun); isQualified {
			if form, hit := pageNarrowingCalls[name]; hit {
				evidence = name + " (" + form + ")"
				return false
			}
		}
		// Третья форма: прямой вопрос к порту отношений.
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel &&
			sel.Sel.Name == relationPortQuestion && len(call.Args) == relationPortArgs {
			evidence = "порт отношений: Check(ctx, субъект, отношение, объект)"
			return false
		}
		if next, nrecv, okNext := p.resolveCall(call.Fun, recv); okNext {
			key := nrecv + "." + funcKey(next)
			if !seen[key] {
				seen[key] = true
				if ev := p.walk(next, nrecv, seen, depth+1); ev != "" {
					evidence = ev
					return false
				}
			}
		}
		return true
	})
	return evidence
}

func funcKey(fn *ast.FuncDecl) string {
	if fn == nil {
		return ""
	}
	return fn.Name.Name
}

// resolveCall разрешает вызов в объявление внутри пакета.
//
// Разбираются две формы, покрывающие раскладку слоёв этого дерева:
//
//	f(...)            — функция уровня пакета;
//	h.поле.Метод(...) — метод use-case'а, лежащего в поле приёмника.
func (p *pkgIndex) resolveCall(fun ast.Expr, recv string) (*ast.FuncDecl, string, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		if fn, ok := p.funcs[f.Name]; ok {
			return fn, recv, true
		}
	case *ast.SelectorExpr:
		// h.поле.Метод(...)
		inner, ok := f.X.(*ast.SelectorExpr)
		if !ok {
			// приёмник.Метод(...) — метод того же типа.
			if ident, isIdent := f.X.(*ast.Ident); isIdent && recv != "" {
				_ = ident
				if fn, hit := p.methods[recv+"."+f.Sel.Name]; hit {
					return fn, recv, true
				}
			}
			return nil, "", false
		}
		if _, isIdent := inner.X.(*ast.Ident); !isIdent {
			return nil, "", false
		}
		fieldType, hit := p.fields[recv+"."+inner.Sel.Name]
		if !hit {
			return nil, "", false
		}
		if fn, okM := p.methods[fieldType+"."+f.Sel.Name]; okM {
			return fn, fieldType, true
		}
	}
	return nil, "", false
}

// qualifiedCallName возвращает имя вида «пакет.Функция» для вызова через
// селектор пакета.
func qualifiedCallName(fun ast.Expr) (string, bool) {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name + "." + sel.Sel.Name, true
}

// receiverType достаёт имя типа приёмника из *T или T.
func receiverType(t ast.Expr) (string, bool) {
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	ident, ok := t.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// bareTypeName достаёт имя типа поля, снимая указатель. Типы из чужих пакетов
// (селектор) не возвращаются: их объявления в этом пакете нет, и обходить
// нечего.
func bareTypeName(t ast.Expr) (string, bool) {
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	ident, ok := t.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}
