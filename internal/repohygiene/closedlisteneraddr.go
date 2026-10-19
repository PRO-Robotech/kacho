// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// closedlisteneraddr.go — гейт класса «выбрал порт — закрыл — отдал» (kacho#3331).
//
// # Класс
//
// Проба открывает слушатель на порту 0, читает у него адрес, закрывает слушатель
// и пользуется адресом ПОСЛЕ закрытия: отдаёт его серверу, процессу, клиенту как
// адрес «остановленной службы». Между закрытием и повторным использованием порт
// свободен для всех: его получает соседний слушатель того же процесса (ядро
// вправе отдать один порт двум подряд таким выборам — так упал близнец домена
// печенья на хуке отправки kacho#3324), проба соседнего пакета или исходящее
// соединение. Вердикт такой пробы — свойство соседства, а не коммита.
//
// Законные формы: адрес берётся у слушателя, который пробе и служит (порт держит
// она сама до конца), либо у удержания `internal/privateloopback` (Hold — адрес,
// который не займёт никто; Lend — адрес, который ядро не выдаёт автовыбором).
//
// # Предикат
//
// В теле одной функции (вложенные литералы функций — отдельные тела):
//
//  1. слушатель присвоен имени из вызова `net.Listen`, `net.ListenTCP` или
//     `tls.Listen`;
//  2. его адрес (`<имя>.Addr()`) присвоен другому имени — носителю адреса;
//  3. `<имя>.Close()` вызван НЕ отложенно (не в `defer`, не внутри литерала
//     функции — `t.Cleanup(func() {…})` закрывает в конце пробы);
//  4. носитель адреса упомянут ПОСЛЕ этого вызова.
//
// Судится узел синтаксического разбора, а не текст: слово в комментарии или
// строке находкой не бывает.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// ClosedListenerAddrFinding — место, где адрес закрытого слушателя ушёл дальше.
type ClosedListenerAddrFinding struct {
	// Pos — координата закрытия «файл:строка».
	Pos string
	// Listener — имя слушателя.
	Listener string
	// Carrier — имя, которое унесло адрес за закрытие.
	Carrier string
}

func (f ClosedListenerAddrFinding) String() string {
	return fmt.Sprintf("%s: слушатель %s закрыт, а его адрес (%s) используется после закрытия — "+
		"порт к этому моменту может держать кто угодно; адрес — у слушателя, который служит "+
		"пробе, либо privateloopback.Hold / privateloopback.Lend", f.Pos, f.Listener, f.Carrier)
}

// ClosedListenerAddrCensus — перепись обхода: сколько прочитано и что найдено.
type ClosedListenerAddrCensus struct {
	Files     int
	Unparsed  int
	Listeners int
	Findings  []ClosedListenerAddrFinding
}

func (c ClosedListenerAddrCensus) String() string {
	return fmt.Sprintf("файлов .go прочитано %d (не разобрано %d) · слушателей `net.Listen`/`tls.Listen` %d · "+
		"мест «взял адрес — закрыл — отдал» %d", c.Files, c.Unparsed, c.Listeners, len(c.Findings))
}

// ClosedListenerAddrsInTree — обход отслеживаемых `.go` под root.
func ClosedListenerAddrsInTree(root string) (ClosedListenerAddrCensus, error) {
	paths, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		return ClosedListenerAddrCensus{}, err
	}
	var c ClosedListenerAddrCensus
	fset := token.NewFileSet()
	for _, p := range paths {
		f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		c.Files++
		if perr != nil {
			c.Unparsed++
			continue
		}
		n, found := closedListenerAddrsInFile(fset, f, strings.TrimPrefix(strings.TrimPrefix(p, root), "/"))
		c.Listeners += n
		c.Findings = append(c.Findings, found...)
	}
	sort.Slice(c.Findings, func(i, j int) bool { return c.Findings[i].Pos < c.Findings[j].Pos })
	return c, nil
}

// ClosedListenerAddrsInSource — тот же предикат над текстом одного файла (для
// инъекций: синтетика, а не живая запись дерева).
func ClosedListenerAddrsInSource(name, src string) (listeners int, findings []ClosedListenerAddrFinding, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, nil, err
	}
	n, found := closedListenerAddrsInFile(fset, f, name)
	return n, found, nil
}

func closedListenerAddrsInFile(fset *token.FileSet, f *ast.File, rel string) (int, []ClosedListenerAddrFinding) {
	listeners := 0
	var out []ClosedListenerAddrFinding
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
		case *ast.FuncLit:
			body = fn.Body
		}
		if body == nil {
			return true
		}
		k, found := closedListenerAddrsInBody(fset, body, rel)
		listeners += k
		out = append(out, found...)
		return true
	})
	return listeners, out
}

// closedListenerAddrsInBody судит одно тело, не заходя во вложенные литералы
// функций: их обойдёт closedListenerAddrsInFile как отдельные тела.
func closedListenerAddrsInBody(fset *token.FileSet, body *ast.BlockStmt, rel string) (int, []ClosedListenerAddrFinding) {
	listenerVars := map[string]bool{}
	carriers := map[string]string{} // носитель → слушатель
	type closing struct {
		listener string
		pos      token.Pos
	}
	var closes []closing
	type use struct {
		name string
		pos  token.Pos
	}
	var uses []use

	inspectOwn(body, func(n ast.Node, deferred bool) {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if len(v.Rhs) == 1 && len(v.Lhs) >= 1 && isListenCall(v.Rhs[0]) {
				if id, ok := v.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
					listenerVars[id.Name] = true
				}
				return
			}
			for _, rhs := range v.Rhs {
				if l := addrOfListener(rhs, listenerVars); l != "" {
					for _, lhs := range v.Lhs {
						if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
							carriers[id.Name] = l
						}
					}
				}
			}
		case *ast.CallExpr:
			if deferred {
				return
			}
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" && len(v.Args) == 0 {
				if id, ok := sel.X.(*ast.Ident); ok && listenerVars[id.Name] {
					closes = append(closes, closing{listener: id.Name, pos: v.Pos()})
				}
			}
		case *ast.Ident:
			uses = append(uses, use{name: v.Name, pos: v.Pos()})
		}
	})

	var out []ClosedListenerAddrFinding
	for _, c := range closes {
		for carrier, l := range carriers {
			if l != c.listener {
				continue
			}
			for _, u := range uses {
				if u.name == carrier && u.pos > c.pos {
					p := fset.Position(c.pos)
					out = append(out, ClosedListenerAddrFinding{
						Pos: fmt.Sprintf("%s:%d", rel, p.Line), Listener: l, Carrier: carrier,
					})
					break
				}
			}
		}
	}
	return len(listenerVars), out
}

// inspectOwn обходит тело, не заходя во вложенные литералы функций, и сообщает,
// стоит ли узел под `defer`.
func inspectOwn(body *ast.BlockStmt, fn func(n ast.Node, deferred bool)) {
	var walk func(n ast.Node, deferred bool)
	walk = func(n ast.Node, deferred bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch v := m.(type) {
			case nil:
				return false
			case *ast.FuncLit:
				return false
			case *ast.DeferStmt:
				if m != n {
					walk(v.Call, true)
					return false
				}
			}
			fn(m, deferred)
			return true
		})
	}
	walk(body, false)
}

func isListenCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name + "." + sel.Sel.Name {
	case "net.Listen", "net.ListenTCP", "tls.Listen":
		return true
	}
	return false
}

// addrOfListener — имя слушателя, чей `Addr()` стоит в выражении, или "".
func addrOfListener(e ast.Expr, listeners map[string]bool) string {
	found := ""
	ast.Inspect(e, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Addr" || len(call.Args) != 0 {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && listeners[id.Name] {
			found = id.Name
		}
		return true
	})
	return found
}
