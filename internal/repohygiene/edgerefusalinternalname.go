// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// edgerefusalinternalname.go — разбор для гейта «ни один отказ края наружу не
// называет внутренний сервис» (kacho#3029). Отделён от гейта, чтобы инъекция
// подавала ему синтетику, не трогая дерево.
//
// # Предмет
//
// Текст отказа — часть контракта, и он уходит наружу дословно: на REST телом
// `google.rpc.Status`, на gRPC — `message` статуса (REST-мост печатает тот же
// текст). Текст, называющий внутреннюю службу («authz service unavailable»),
// раскрывает арендатору топологию края: из чего он собран и какая его часть
// сейчас лежит. Арендатору это знание ничего не даёт — действие на `503` одно,
// «повторить», — а тому, кто изучает поверхность, даёт карту.
//
// # Что считается текстом отказа — шесть форм записи, узлами разбора
//
// Перечень выведен обходом дерева края (`gateway/`, непроверочные файлы), а не
// по памяти; каждая форма в дереве есть, и гейт падает, если какая-то из них
// исчезла: распознаватель, ни разу не встретивший форму, о ней не проверен.
//
//  1. `status` — второй аргумент `status.Error/Errorf/New/Newf` (строка формата
//     у `…f`);
//  2. `json` — строковое константное выражение, несущее тело
//     `{"code":…,"message":"…"}` (литерал, сцепление литералов и констант);
//  3. `map` — значение ключа `"message"` в составном литерале;
//  4. `field` — значение поля `msg` в составном литерале (запись отказа
//     `refusal{…, msg: …}`);
//  5. `arg` — строковый константный аргумент вызова писателя отказа (имя
//     вызываемого начинается с `write`/`Write`, кроме `Write`, `WriteHeader`,
//     `WriteString`, либо равно `exhausted`): так текст доходит до тела через
//     параметр;
//  6. `httperror` — второй аргумент `http.Error(w, текст, код)`: стандартная
//     библиотека пишет текст телом ответа дословно (форма добрана по находке
//     проверки работы: до неё текст `http.Error` не судился вовсе).
//
// Значение константы разрешается по объявлениям пакета (и `пакет.Имя` — по
// объявлениям всего дерева края), сцепление `+` — складывается. Значение, не
// разрешаемое статически (параметр, вызов), судом не является и считается
// отдельно, по форме: «не разрешено N» — граница, названная вслух. Предпосылка
// гейта — каждая форма встречается в дереве хотя бы одним местом (разрешённым
// либо нет).
//
// # Что считается внутренним именем
//
//   - имя внутренней gRPC-службы (`Internal<…>Service`) — оно не выходит на
//     внешний слушатель by construction (ban #6), и в тексте ему делать нечего;
//   - компонент, названный службой: `<компонент> service|server|backend|database|listener`,
//     где компонент — каталог `services/` либо составная часть края
//     (`authz`, `iam`, `identity`, `kaname`, `openfga`, `fga`, `postgres`,
//     `subscription`: «subscription backend» называет часть края, которая лежит,
//     тогда как поток подписки `/subscription/v1/…` — поверхность контракта, и
//     слово само по себе находкой не является);
//   - имя хранилища или адрес внутреннего слушателя: `openfga`, `postgres`,
//     `pgx`, `:9091`, `.svc`.
//
// Публичные имена не находка: путь `/iam/v1/…`, имя права `vpc.networks.delete`,
// домен `kaname.cloud.iam.v1` в `ErrorInfo` — всё это поверхность контракта.

// EdgeRefusalForms — формы записи текста отказа, в порядке шапки.
var EdgeRefusalForms = []string{"status", "json", "map", "field", "arg", "httperror"}

// EdgeRefusalFinding — текст отказа, называющий внутреннее имя.
type EdgeRefusalFinding struct {
	File, Form, Name, Text string
	Line                   int
}

// EdgeRefusalCensus — объём осмотренного.
type EdgeRefusalCensus struct {
	Files, Unparsed int
	// ByForm — текстов, разрешённых и судимых, по форме.
	ByForm map[string]int
	// UnresolvedByForm — мест формы, чьё значение статически не разрешается
	// (параметр, вызов): форма в дереве есть, текст судом не является.
	UnresolvedByForm map[string]int
	// Unresolved — сумма UnresolvedByForm.
	Unresolved int
}

// EdgeComponentNames — составные части края, которые не являются каталогом
// `services/`, но службой названы быть могут.
var EdgeComponentNames = []string{"authz", "iam", "identity", "kaname", "openfga", "fga", "postgres", "subscription"}

func edgeInternalNamePatterns(components []string) []*regexp.Regexp {
	quoted := make([]string, 0, len(components))
	for _, c := range components {
		quoted = append(quoted, regexp.QuoteMeta(c))
	}
	return []*regexp.Regexp{
		regexp.MustCompile(`\bInternal[A-Z][A-Za-z]*Service\b`),
		regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)[ _-](service|server|backend|database|listener)\b`),
		regexp.MustCompile(`(?i)\b(openfga|postgres|pgx)\b`),
		regexp.MustCompile(`:9091\b|\.svc\b`),
	}
}

var edgeJSONMessage = regexp.MustCompile(`"message":"((?:[^"\\]|\\.)*)"`)

// FindEdgeRefusalInternalNames судит тексты отказов края. sources — путь от
// корня → текст непроверочного файла Go; components — компоненты, которые
// не могут быть названы службой (каталоги `services/` плюс EdgeComponentNames).
func FindEdgeRefusalInternalNames(sources map[string]string, components []string) ([]EdgeRefusalFinding, EdgeRefusalCensus) {
	census := EdgeRefusalCensus{ByForm: map[string]int{}, UnresolvedByForm: map[string]int{}}
	unresolved := func(form string) { census.UnresolvedByForm[form]++; census.Unresolved++ }
	patterns := edgeInternalNamePatterns(components)
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	paths := make([]string, 0, len(sources))
	for rel := range sources {
		if !strings.HasPrefix(rel, "gateway/") || strings.HasSuffix(rel, "_test.go") || IsTestHarnessPath(rel) {
			continue
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		f, err := parser.ParseFile(fset, rel, sources[rel], 0)
		if err != nil {
			census.Unparsed++
			continue
		}
		census.Files++
		files[rel] = f
	}
	consts := edgeStringConsts(files)

	var findings []EdgeRefusalFinding
	judge := func(rel string, pos token.Pos, form, text string) {
		census.ByForm[form]++
		for _, p := range patterns {
			if m := p.FindString(text); m != "" {
				findings = append(findings, EdgeRefusalFinding{File: rel, Line: fset.Position(pos).Line, Form: form, Name: m, Text: text})
				return
			}
		}
	}
	for _, rel := range paths {
		f := files[rel]
		if f == nil {
			continue
		}
		dir := path.Dir(rel)
		eval := func(e ast.Expr) (string, bool) { return consts.eval(dir, e) }
		inner := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				if v.Op == token.ADD {
					inner[v.X], inner[v.Y] = true, true
				}
				if !inner[v] {
					if s, ok := eval(v); ok {
						for _, m := range edgeJSONMessage.FindAllStringSubmatch(s, -1) {
							judge(rel, v.Pos(), "json", m[1])
						}
					}
				}
			case *ast.BasicLit:
				if v.Kind == token.STRING && !inner[v] {
					if s, err := strconv.Unquote(v.Value); err == nil {
						for _, m := range edgeJSONMessage.FindAllStringSubmatch(s, -1) {
							judge(rel, v.Pos(), "json", m[1])
						}
					}
				}
			case *ast.CallExpr:
				switch {
				case edgeSelOf(v.Fun, "status", "Error", "Errorf", "New", "Newf") && len(v.Args) >= 2:
					if s, ok := eval(v.Args[1]); ok {
						judge(rel, v.Args[1].Pos(), "status", s)
					} else {
						unresolved("status")
					}
				case edgeSelOf(v.Fun, "http", "Error") && len(v.Args) >= 2:
					if s, ok := eval(v.Args[1]); ok {
						judge(rel, v.Args[1].Pos(), "httperror", s)
					} else {
						unresolved("httperror")
					}
				case edgeRefusalWriterCall(v):
					for _, a := range v.Args {
						if s, ok := eval(a); ok {
							judge(rel, a.Pos(), "arg", s)
						}
					}
				}
			case *ast.KeyValueExpr:
				switch k := v.Key.(type) {
				case *ast.BasicLit:
					if k.Kind == token.STRING && k.Value == `"message"` {
						if s, ok := eval(v.Value); ok {
							judge(rel, v.Value.Pos(), "map", s)
						} else {
							unresolved("map")
						}
					}
				case *ast.Ident:
					if k.Name == "msg" {
						if s, ok := eval(v.Value); ok {
							judge(rel, v.Value.Pos(), "field", s)
						} else {
							unresolved("field")
						}
					}
				}
			}
			return true
		})
	}
	return findings, census
}

// edgeRefusalWriterCall — вызов писателя отказа, через параметр которого текст
// доходит до тела.
func edgeRefusalWriterCall(c *ast.CallExpr) bool {
	var name string
	switch f := c.Fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
	default:
		return false
	}
	switch name {
	case "Write", "WriteHeader", "WriteString":
		return false
	case "exhausted":
		return true
	}
	return strings.HasPrefix(name, "write") || strings.HasPrefix(name, "Write")
}

// edgeConstTable — строковые константы края: по каталогу пакета и по
// квалифицированному имени `пакет.Имя`.
type edgeConstTable struct {
	byDir  map[string]map[string]ast.Expr
	byQual map[string]ast.Expr
	dirOf  map[string]string // квалифицированное имя → каталог объявления
}

func edgeStringConsts(files map[string]*ast.File) edgeConstTable {
	t := edgeConstTable{byDir: map[string]map[string]ast.Expr{}, byQual: map[string]ast.Expr{}, dirOf: map[string]string{}}
	for rel, f := range files {
		dir := path.Dir(rel)
		if t.byDir[dir] == nil {
			t.byDir[dir] = map[string]ast.Expr{}
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				vs := s.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						t.byDir[dir][n.Name] = vs.Values[i]
						q := f.Name.Name + "." + n.Name
						t.byQual[q] = vs.Values[i]
						t.dirOf[q] = dir
					}
				}
			}
		}
	}
	return t
}

func (t edgeConstTable) eval(dir string, e ast.Expr) (string, bool) {
	return t.evalDepth(dir, e, 0)
}

func (t edgeConstTable) evalDepth(dir string, e ast.Expr, depth int) (string, bool) {
	if depth > 16 {
		return "", false
	}
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.ParenExpr:
		return t.evalDepth(dir, v.X, depth+1)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := t.evalDepth(dir, v.X, depth+1)
		if !ok {
			return "", false
		}
		r, ok := t.evalDepth(dir, v.Y, depth+1)
		return l + r, ok
	case *ast.Ident:
		if x, ok := t.byDir[dir][v.Name]; ok {
			return t.evalDepth(dir, x, depth+1)
		}
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			q := id.Name + "." + v.Sel.Name
			if x, ok := t.byQual[q]; ok {
				return t.evalDepth(t.dirOf[q], x, depth+1)
			}
		}
	}
	return "", false
}
