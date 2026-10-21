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
//  1. слушатель — имя, которому присвоен вызов `net.Listen`, `net.ListenTCP`
//     или `tls.Listen` (присваивание `:=`/`=` либо объявление `var`);
//  2. носитель адреса — всякое имя, которому присвоено выражение с
//     `<слушатель>.Addr()` либо с другим носителем того же слушателя
//     (`a := l.Addr(); s := a.String()`); присваивание полю или элементу
//     (`x.f = l.Addr()…`, `x[i] = …`) делает носителем корень цели — `x`;
//  3. закрытие — `<слушатель>.Close()`, вызванный НЕ отложенно (не в `defer`,
//     не внутри литерала функции: `t.Cleanup(func() {…})` закрывает в конце);
//  4. находка — после закрытия упомянут носитель либо снова прочитан
//     `<слушатель>.Addr()`.
//
// Закрытие в блоке, который тут же покидает ФУНКЦИЮ (`return`, `panic`,
// `t.Fatal…`, `t.FailNow`, `t.Skip…`, `os.Exit`), судит только остаток ЭТОГО
// блока: путь, на котором слушатель закрыт, до упоминаний за блоком не доходит
// (закрытие в ветке ошибки). `break`, `continue` и `goto` выходом не считаются:
// они уводят управление лишь из цикла или `switch`, и за ним путь доходит до
// упоминаний (цикл подбора порта `for { …; _ = l.Close(); break }; return addr`).
//
// Судится узел синтаксического разбора, а не текст: слово в комментарии или
// строке находкой не бывает. Файл, который не разобрался, — отказ гейта: он
// судит дерево целиком, и неразобранный файл с дефектом иначе был бы зелёным.
//
// # Граница обхода — формы, которых предикат НЕ видит
//
//   - закрытие вспомогательной функцией (`closeQuietly(l)`, `mustClose(t, l)`):
//     судится только вызов метода `Close` у самого слушателя;
//   - закрытие в сразу вызванном литерале (`func() { _ = l.Close() }()`):
//     литерал — отдельное тело, в нём слушатель не объявлен;
//   - слушатель, полученный иначе: `net.ListenPacket`/`net.ListenUDP` (UDP),
//     `(&net.ListenConfig{}).Listen`, `httptest`, помощник, возвращающий
//     слушатель, — имя не опознаётся слушателем;
//   - адрес, ушедший через параметр в другую функцию, которая закрывает слушатель
//     сама: тела функций судятся порознь;
//   - адрес, переложенный аргументом функции, которая не метод
//     (`u := fmt.Sprintf("http://%s", addr)`): результат такого вызова носителем
//     не считается (иначе носителем стал бы и `err := use(addr)`), находка есть,
//     только если после закрытия упомянут сам `addr`.
//
// Вне границы класс держат перепись и ревью; расширение границы — новой формой с
// инъекцией дефекта и законного близнеца.

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
	// Carrier — что унесло адрес за закрытие: имя носителя либо `<слушатель>.Addr()`.
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
	Unparsed  []string
	Listeners int
	Findings  []ClosedListenerAddrFinding
}

func (c ClosedListenerAddrCensus) String() string {
	return fmt.Sprintf("файлов .go прочитано %d (не разобрано %d) · слушателей `net.Listen`/`tls.Listen` %d · "+
		"мест «взял адрес — закрыл — отдал» %d", c.Files, len(c.Unparsed), c.Listeners, len(c.Findings))
}

// Refusals — отказы гейта по переписи: предпосылка обхода, неразобранные файлы,
// находки. Пусто — зелёный.
func (c ClosedListenerAddrCensus) Refusals() []string {
	var out []string
	if c.Files == 0 || c.Listeners == 0 {
		out = append(out, "предпосылка обхода не выполнена — проверять нечего: "+c.String())
	}
	for _, u := range c.Unparsed {
		out = append(out, "файл не разобран — гейт судит дерево целиком, и дефект в нём был бы зелёным: "+u)
	}
	for _, f := range c.Findings {
		out = append(out, f.String())
	}
	return out
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
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		c.Files++
		if perr != nil {
			c.Unparsed = append(c.Unparsed, fmt.Sprintf("%s: %v", rel, perr))
			continue
		}
		n, found := closedListenerAddrsInFile(fset, f, rel)
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

// listenerBody — факты одного тела в порядке исходника.
type listenerBody struct {
	listeners map[string]bool
	carriers  map[string]string // носитель → слушатель
	closes    []listenerEvent   // закрытия
	addrReads []listenerEvent   // чтения <слушатель>.Addr()
	uses      []listenerEvent   // упоминания имён (name — имя)
	blocks    []stmtBlock       // блоки, завершающиеся сразу
}

type listenerEvent struct {
	name string
	pos  token.Pos
}

type stmtBlock struct {
	pos, end token.Pos
}

// closedListenerAddrsInBody судит одно тело, не заходя во вложенные литералы
// функций: их обойдёт closedListenerAddrsInFile как отдельные тела.
func closedListenerAddrsInBody(fset *token.FileSet, body *ast.BlockStmt, rel string) (int, []ClosedListenerAddrFinding) {
	b := &listenerBody{listeners: map[string]bool{}, carriers: map[string]string{}}
	inspectOwn(body, func(n ast.Node, deferred bool) {
		switch v := n.(type) {
		case *ast.AssignStmt:
			b.assign(v.Lhs, v.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(v.Names))
			for i, id := range v.Names {
				lhs[i] = id
			}
			b.assign(lhs, v.Values)
		case *ast.BlockStmt:
			if v != body && terminates(v.List) {
				b.blocks = append(b.blocks, stmtBlock{pos: v.Pos(), end: v.End()})
			}
		case *ast.CaseClause:
			if terminates(v.Body) {
				b.blocks = append(b.blocks, stmtBlock{pos: v.Pos(), end: v.End()})
			}
		case *ast.CommClause:
			if terminates(v.Body) {
				b.blocks = append(b.blocks, stmtBlock{pos: v.Pos(), end: v.End()})
			}
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || len(v.Args) != 0 {
				return
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || !b.listeners[id.Name] {
				return
			}
			switch sel.Sel.Name {
			case "Close":
				if !deferred {
					b.closes = append(b.closes, listenerEvent{name: id.Name, pos: v.Pos()})
				}
			case "Addr":
				b.addrReads = append(b.addrReads, listenerEvent{name: id.Name, pos: v.Pos()})
			}
		case *ast.Ident:
			b.uses = append(b.uses, listenerEvent{name: v.Name, pos: v.Pos()})
		}
	})
	return len(b.listeners), b.findings(fset, rel)
}

// assign — присваивание либо объявление: заводит слушателя или носителя.
func (b *listenerBody) assign(lhs, rhs []ast.Expr) {
	if len(rhs) == 1 && len(lhs) >= 1 && isListenCall(rhs[0]) {
		if id, ok := lhs[0].(*ast.Ident); ok && id.Name != "_" {
			b.listeners[id.Name] = true
		}
		return
	}
	for _, r := range rhs {
		l := b.addrSource(r)
		if l == "" {
			continue
		}
		for _, target := range lhs {
			if root := assignTargetRoot(target); root != "" && root != "_" {
				b.carriers[root] = l
			}
		}
	}
}

// addrSource — слушатель, чей адрес несёт выражение: прямое `<слушатель>.Addr()`
// либо упоминание носителя. Пусто — выражение адреса не несёт.
//
// В аргументы вызова, который не метод (`use(addr)`, `fmt.Sprintf(…, addr)`),
// разбор не заходит: результат такого вызова — не адрес (`err := use(addr)`), и
// носителем его считать — ложная находка на упоминании `err` после закрытия.
// Цепочка методов носителя (`a.String()`) адрес несёт.
func (b *listenerBody) addrSource(e ast.Expr) string {
	found := ""
	ast.Inspect(e, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		switch v := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok {
				return false // не метод: аргументы адреса дальше не несут
			}
			if sel.Sel.Name == "Addr" && len(v.Args) == 0 {
				if id, ok := sel.X.(*ast.Ident); ok && b.listeners[id.Name] {
					found = id.Name
					return false
				}
			}
			// Метод: адрес несёт получатель цепочки, а не аргументы.
			if l := b.addrSource(sel.X); l != "" {
				found = l
			}
			return false
		case *ast.Ident:
			if l, ok := b.carriers[v.Name]; ok {
				found = l
			}
		}
		return true
	})
	return found
}

func (b *listenerBody) findings(fset *token.FileSet, rel string) []ClosedListenerAddrFinding {
	var out []ClosedListenerAddrFinding
	for _, c := range b.closes {
		end := token.Pos(-1) // до конца тела
		for _, blk := range b.blocks {
			if blk.pos <= c.pos && c.pos < blk.end && (end < 0 || blk.end < end) {
				end = blk.end
			}
		}
		inScope := func(p token.Pos) bool { return p > c.pos && (end < 0 || p < end) }
		carrier := ""
		for _, r := range b.addrReads {
			if r.name == c.name && inScope(r.pos) {
				carrier = c.name + ".Addr()"
				break
			}
		}
		if carrier == "" {
			for _, u := range b.uses {
				if l, ok := b.carriers[u.name]; ok && l == c.name && inScope(u.pos) {
					carrier = u.name
					break
				}
			}
		}
		if carrier != "" {
			out = append(out, ClosedListenerAddrFinding{
				Pos: fmt.Sprintf("%s:%d", rel, fset.Position(c.pos).Line), Listener: c.name, Carrier: carrier,
			})
		}
	}
	return out
}

// terminates — последний оператор списка уводит управление из блока.
func terminates(list []ast.Stmt) bool {
	if len(list) == 0 {
		return false
	}
	switch s := list[len(list)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			return fn.Name == "panic"
		case *ast.SelectorExpr:
			switch fn.Sel.Name {
			case "Fatal", "Fatalf", "FailNow", "Skip", "Skipf", "SkipNow", "Exit":
				return true
			}
		}
	}
	return false
}

// assignTargetRoot — корень цели присваивания: `x` у `x`, `x.f`, `x.f.g`, `x[i]`, `*x`.
func assignTargetRoot(e ast.Expr) string {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v.Name
		case *ast.SelectorExpr:
			e = v.X
		case *ast.IndexExpr:
			e = v.X
		case *ast.StarExpr:
			e = v.X
		case *ast.ParenExpr:
			e = v.X
		default:
			return ""
		}
	}
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
