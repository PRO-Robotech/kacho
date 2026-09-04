// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// peerprosverb.go — разбор прозы полос ответа соседа (`peer.Prose`).
//
// Предмет. Носитель `pkg/peer` подставляет идентификатор чужого ресурса в текст
// ТОЛЬКО тех полос, которые о нём что-либо утверждают (`Outcome.NamesResource`).
// Значит проза каждого поля обязана нести ровно столько глаголов, сколько её
// полоса заполнит: `Missing` и `State` — один, `Unavailable` — ни одного,
// непрозрачная форма (`Opaque: true`) — ни одного ни в одном поле.
//
// Расхождение не роняет сборку и не ловится `go vet`: формат приходит в
// форматтер значением поля, а не константой на месте вызова. Наружу оно выходит
// служебным мусором в контрактном тексте — `%!(EXTRA string=<id>)` при лишнем
// аргументе, `%!s(MISSING)` при недостающем, — то есть арендатор получает
// сломанный текст, а в первом случае ещё и названный идентификатор чужого
// ресурса, который полоса называть не собиралась.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: строка `"%s"` в
// комментарии, объясняющем эту же дисциплину, текстовому поиску неотличима от
// самой прозы.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// peerProseScanRoots — область обхода. Каталог, которого здесь нет, гейтом не
// покрыт; отсутствие каталога — отказ, а не тихий пропуск.
var peerProseScanRoots = []string{"pkg", "services", "gateway"}

// peerProseCensus — объём осмотренного. Печатается всегда, чтобы «ноль находок»
// было отличимо от «ноль прочитанного».
type peerProseCensus struct {
	Files      int
	Literals   int
	Values     int
	Unresolved []string
}

// peerProseFinding — расхождение прозы с полосой.
type peerProseFinding struct {
	Where string
	What  string
}

// verbCount — сколько глаголов форматтера несёт строка. Удвоенный процент —
// экранированный литерал, глаголом не является.
func verbCount(s string) int {
	return strings.Count(s, "%") - 2*strings.Count(s, "%%")
}

// peerProseExpect — сколько глаголов вправе нести поле.
var peerProseExpect = map[string]int{
	"Missing":     1,
	"State":       1,
	"Unavailable": 0,
}

// auditPeerProse обходит дерево и сверяет прозу каждого литерала `peer.Prose` с
// тем, что подставит носитель.
//
// Значение, не являющееся строковым литералом, разрешается как строковая
// константа ТОГО ЖЕ пакета; неразрешимое попадает в перепись поимённо и находкой
// не считается — гейт обязан называть границу своего зрения, а не делать вид,
// что её нет.
func auditPeerProse(root string) ([]peerProseFinding, peerProseCensus, error) {
	var paths []string
	for _, sub := range peerProseScanRoots {
		base := filepath.Join(root, sub)
		// Состав берётся у ИНДЕКСА git, а не у диска: обход диска прочитал бы
		// игнорируемое (рабочие копии агентов, распаковки чартов, сборочные
		// каталоги фронтенда), и вердикт стал бы свойством рабочего каталога, а
		// не коммита. Отсутствие каталога — отказ, а не тихий пропуск: область
		// обхода гейта тогда сломана.
		under, err := treecorpus.UnderWithSuffix(base, ".go")
		if err != nil {
			return nil, peerProseCensus{}, fmt.Errorf("состав %s: %w", sub, err)
		}
		paths = append(paths, under...)
	}
	return auditPeerProseFiles(root, paths)
}

// auditPeerProseFiles — ядро разбора: та же логика, но состав приходит списком.
//
// Разделение нужно ради проб инъекции: синтетическое дерево во временном
// каталоге репозиторием не является, спрашивать у него индекс нечего. Обе
// стороны гоняют ЭТУ функцию — проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
func auditPeerProseFiles(root string, paths []string) ([]peerProseFinding, peerProseCensus, error) {
	var (
		findings []peerProseFinding
		census   peerProseCensus
	)

	// Строковые константы собираются по каталогам: пакет — это каталог, и
	// значение, пришедшее в литерал именем, чаще всего объявлено рядом.
	consts := map[string]map[string]string{}
	files := map[string][]string{}

	for _, path := range paths {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil, census, fmt.Errorf("путь %s вне дерева %s: %w", path, root, relErr)
		}
		if strings.HasPrefix(rel, "pkg/api/") || strings.Contains(rel, "mock") {
			continue
		}
		dir := filepath.Dir(path)
		files[dir] = append(files[dir], path)
	}

	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	dirs := make([]string, 0, len(files))
	for dir := range files {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		sort.Strings(files[dir])
		consts[dir] = map[string]string{}
		for _, path := range files[dir] {
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return nil, census, fmt.Errorf("разбор %s: %w", path, err)
			}
			parsed[path] = f
			census.Files++
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i >= len(vs.Values) {
							continue
						}
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if v, err := strconv.Unquote(lit.Value); err == nil {
								consts[dir][name.Name] = v
							}
						}
					}
				}
			}
		}
	}

	for _, dir := range dirs {
		callArgs := collectPackageCallArgs(parsed, files[dir])
		for _, path := range files[dir] {
			f := parsed[path]
			rel, _ := filepath.Rel(root, path)
			enclosing := map[*ast.CompositeLit]*ast.FuncDecl{}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					if cl, ok := n.(*ast.CompositeLit); ok && isPeerProseType(cl.Type) {
						enclosing[cl] = fn
					}
					return true
				})
			}
			ast.Inspect(f, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || !isPeerProseType(cl.Type) {
					return true
				}
				census.Literals++
				opaque := false
				for _, elt := range cl.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if ok && key.Name == "Opaque" {
						if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
							opaque = true
						}
					}
				}
				for _, elt := range cl.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					want, tracked := peerProseExpect[key.Name]
					if !tracked {
						continue
					}
					if opaque {
						want = 0
					}
					texts := peerProseStrings(kv.Value, consts[dir], enclosing[cl], callArgs)
					pos := fset.Position(kv.Pos())
					where := fmt.Sprintf("%s:%d поле %s", rel, pos.Line, key.Name)
					if len(texts) == 0 {
						census.Unresolved = append(census.Unresolved, where)
						continue
					}
					census.Values += len(texts)
					for _, text := range texts {
						if got := verbCount(text); got != want {
							findings = append(findings, peerProseFinding{
								Where: where,
								What: fmt.Sprintf("глаголов форматтера %d, полоса заполнит %d — текст %q "+
									"выйдет к арендатору сломанным (лишний аргумент печатается как "+
									"%%!(EXTRA …), недостающий — как %%!s(MISSING))", got, want, text),
							})
						}
					}
				}
				return true
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].Where < findings[j].Where })
	sort.Strings(census.Unresolved)
	return findings, census, nil
}

// isPeerProseType — литерал именно носителя полос, а не одноимённой структуры
// чужого пакета.
func isPeerProseType(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "peer" && sel.Sel.Name == "Prose"
}

// collectPackageCallArgs — аргументы всех вызовов функций пакета по имени.
//
// Нужно затем, что проза сплошь и рядом собирается обёрткой: `func zoneLane(o
// peer.Outcome, id, unavailable string)` кладёт в литерал СВОЙ параметр, и без
// шага к вызывающим гейт видел бы имя, а не текст. Тогда обёртка становилась бы
// способом пронести глагол мимо гейта — то есть послаблением, которого никто не
// объявлял.
func collectPackageCallArgs(parsed map[string]*ast.File, paths []string) map[string][][]ast.Expr {
	out := map[string][][]ast.Expr{}
	for _, path := range paths {
		f := parsed[path]
		if f == nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				out[id.Name] = append(out[id.Name], call.Args)
			}
			return true
		})
	}
	return out
}

// peerProseStrings — все тексты, которыми поле может оказаться: сам литерал,
// строковая константа пакета либо — если поле кладёт параметр объемлющей
// функции — каждый аргумент, с которым эту функцию в пакете зовут.
//
// Пустой результат означает «гейту не видно»; такое попадает в перепись
// поимённо, а не выдаётся за отсутствие дефекта.
func peerProseStrings(e ast.Expr, consts map[string]string, fn *ast.FuncDecl, calls map[string][][]ast.Expr) []string {
	if s, ok := peerProseString(e, consts); ok {
		return []string{s}
	}
	id, ok := e.(*ast.Ident)
	if !ok || fn == nil || fn.Type.Params == nil {
		return nil
	}
	pos, found := -1, false
	idx := 0
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name.Name == id.Name {
				pos, found = idx, true
			}
			idx++
		}
		if len(field.Names) == 0 {
			idx++
		}
	}
	if !found {
		return nil
	}
	var out []string
	for _, args := range calls[fn.Name.Name] {
		if pos >= len(args) {
			return nil
		}
		s, ok := peerProseString(args[pos], consts)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

// peerProseString — значение поля как строка: литерал либо строковая константа
// того же пакета. Второй результат ложен, когда значение вычисляется в рантайме
// (параметр, поле, вызов) — такое гейту не видно, и он это называет.
func peerProseString(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, okL := peerProseString(v.X, consts)
		r, okR := peerProseString(v.Y, consts)
		if !okL || !okR {
			return "", false
		}
		return l + r, true
	}
	return "", false
}
