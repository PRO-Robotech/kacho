// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// edge_assembly_census_test.go — у HTTP-поверхности края ОДИН дом
// (edge_listener.go), и пробы edge_h2_rest_test.go судят именно его (kacho#3125).
//
// Пробы зовут newEdgeHTTPServer и serveEdgeMux — те же функции, что корень.
// Утверждение «проверено то, что работает в бою» держится, только пока корень
// не собирает сервер, не делит порт и не поднимает слушатель мимо них. Перепись
// судит это по ИДЕНТИФИКАТОРАМ разбора, а не по имени переменной: Serve,
// ExternalListener, Match встречаются в корне только внутри своей функции, в
// какой бы форме их ни записали — вызовом, значением метода, через другое имя
// сервера или после переприсвоения слушателя. Формы, которые перепись обязана
// видеть, перечислены в edgeAssemblyInjections и доказаны инъекцией.

// edgeAssemblyHomes — идентификатор → функции edge_listener.go, где ему место.
var edgeAssemblyHomes = map[string][]string{
	// Подъём сервера на слушателе — любой формы.
	"Serve":             {"serveEdgeMux", "serveInternalREST"},
	"ServeTLS":          {"serveEdgeMux", "serveInternalREST"},
	"ListenAndServe":    {"serveEdgeMux", "serveInternalREST"},
	"ListenAndServeTLS": {"serveEdgeMux", "serveInternalREST"},
	// Метки происхождения слушателя.
	"ExternalListener": {"serveEdgeMux"},
	"InternalListener": {"serveInternalREST"},
	// Матчеры мультиплексора.
	"Match":                                   {"splitEdgeCmux"},
	"MatchWithWriters":                        {"splitEdgeCmux"},
	"MatchHeaderFieldSendSettings":            {"splitEdgeCmux"},
	"HTTP2MatchHeaderFieldSendSettings":       {"splitEdgeCmux"},
	"HTTP2MatchHeaderFieldPrefixSendSettings": {"splitEdgeCmux"},
}

const edgeServerBuilder = "newEdgeHTTPServer"

type edgeAssemblyCensus struct {
	files       int
	idents      map[string]int // идентификаторы edgeAssemblyHomes, увиденные в корне
	serverTypes int            // упоминания типа http.Server вне сигнатур
	edgeMuxRefs []string       // места обращения к serveEdgeMux
	decls       map[string]bool
	findings    []string
}

func (c edgeAssemblyCensus) String() string {
	var parts []string
	for _, k := range sortedKeys(c.idents) {
		parts = append(parts, fmt.Sprintf("%s %d", k, c.idents[k]))
	}
	return fmt.Sprintf("файлов %d · %s · тип http.Server вне сигнатур %d · обращений к serveEdgeMux %d · находок %d",
		c.files, strings.Join(parts, " · "), c.serverTypes, len(c.edgeMuxRefs), len(c.findings))
}

// httpImportName — локальное имя пакета net/http в файле либо "".
func httpImportName(f *ast.File) string {
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == "net/http" {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return "http"
		}
	}
	return ""
}

func censusEdgeAssembly(fset *token.FileSet, files []*ast.File, variants int) edgeAssemblyCensus {
	c := edgeAssemblyCensus{files: len(files), idents: map[string]int{}, decls: map[string]bool{}}
	for _, f := range files {
		httpName := httpImportName(f)
		for _, decl := range f.Decls {
			owner := ""
			skip := map[*ast.Ident]bool{}
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil {
				owner = fd.Name.Name
				c.decls[owner] = true
				skip[fd.Name] = true
			}
			// Сигнатуры функций вправе называть тип сервера: параметр — не сборка.
			var sigs []ast.Node
			ast.Inspect(decl, func(n ast.Node) bool {
				if ft, ok := n.(*ast.FuncType); ok {
					sigs = append(sigs, ft)
				}
				return true
			})
			inSig := func(n ast.Node) bool {
				for _, s := range sigs {
					if n.Pos() >= s.Pos() && n.End() <= s.End() {
						return true
					}
				}
				return false
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.SelectorExpr:
					if x, ok := n.X.(*ast.Ident); ok && httpName != "" && x.Name == httpName && n.Sel.Name == "Server" && !inSig(n) {
						c.serverTypes++
						if owner != edgeServerBuilder {
							c.findings = append(c.findings, fmt.Sprintf("%s: http.Server собирается в %q, а не в %s — пробы края такой сервер не судят",
								fset.Position(n.Pos()), owner, edgeServerBuilder))
						}
					}
				case *ast.Ident:
					if skip[n] {
						return true
					}
					if n.Name == "serveEdgeMux" {
						c.edgeMuxRefs = append(c.edgeMuxRefs, fset.Position(n.Pos()).String())
					}
					homes, ok := edgeAssemblyHomes[n.Name]
					if !ok {
						return true
					}
					c.idents[n.Name]++
					for _, h := range homes {
						if owner == h {
							return true
						}
					}
					c.findings = append(c.findings, fmt.Sprintf("%s: %s в %q, а место ему — %s: этот слушатель пробы края не судят",
						fset.Position(n.Pos()), n.Name, owner, strings.Join(homes, ", ")))
				}
				return true
			})
		}
	}
	if len(c.edgeMuxRefs) != variants {
		c.findings = append(c.findings, fmt.Sprintf("обращений к serveEdgeMux %d (%s), вариантов внешнего слушателя под пробой %d — слушатель заведён без пробы либо проба без слушателя",
			len(c.edgeMuxRefs), strings.Join(c.edgeMuxRefs, "; "), variants))
	}
	return c
}

// Предпосылка переписи: дома существуют и в них есть то, что перепись
// разрешает только им. Иначе «находок 0» значило бы «смотреть было не на что».
func checkEdgeAssemblyPremise(t *testing.T, c edgeAssemblyCensus) {
	t.Helper()
	for _, fn := range []string{"serveEdgeMux", "serveInternalREST", "splitEdgeCmux", edgeServerBuilder} {
		if !c.decls[fn] {
			t.Fatalf("в пакете корня нет функции %s — перепись судит пустоту", fn)
		}
	}
	for _, id := range []string{"Serve", "ExternalListener", "InternalListener", "Match", "MatchWithWriters"} {
		if c.idents[id] == 0 {
			t.Fatalf("в пакете корня не встречено ни одного %s — перепись судит пустоту", id)
		}
	}
	if c.serverTypes == 0 {
		t.Fatal("в пакете корня не собирается ни одного http.Server — перепись судит пустоту")
	}
}

func TestEdgeH2REST_EdgeAssemblyHasASingleHome(t *testing.T) {
	fset, files := parseRootPackage(t)
	c := censusEdgeAssembly(fset, files, len(probedEdgeListeners))
	t.Logf("перепись пакета корня: %s · вариантов под пробой %d", c, len(probedEdgeListeners))
	checkEdgeAssemblyPremise(t, c)
	if len(c.findings) > 0 {
		t.Fatalf("сборка края мимо своего дома:\n%s", strings.Join(c.findings, "\n"))
	}
}

// edgeAssemblyInjections — формы обхода, которые перепись обязана видеть.
// Каждая — настоящий исходник, дописанный к корню отдельным файлом.
var edgeAssemblyInjections = []struct {
	name, src, want string
}{
	{
		name: "I8a: внешний слушатель через переменную",
		src: `func inj(m cmux.CMux, httpSrv *http.Server) {
	ext := listenerorigin.ExternalListener(m.Match(cmux.Any()))
	_ = httpSrv.Serve(ext)
}`,
		want: "ExternalListener в \"inj\"",
	},
	{
		name: "I8b: значение метода Serve",
		src: `func inj(l net.Listener, httpSrv *http.Server) {
	serve := httpSrv.Serve
	_ = serve(l)
}`,
		want: "Serve в \"inj\"",
	},
	{
		name: "I8c: сервер под другим именем",
		src: `func inj(l net.Listener, httpSrv *http.Server) {
	srv2 := httpSrv
	_ = srv2.Serve(l)
}`,
		want: "Serve в \"inj\"",
	},
	{
		name: "I8d: третий слушатель через serveEdgeMux",
		src: `func inj(m cmux.CMux, g *grpc.Server, s *http.Server) {
	_ = serveEdgeMux(m, g, s, nil)
}`,
		want: "обращений к serveEdgeMux 3",
	},
	{
		name: "I8e: переприсвоение слушателя после splitEdgeCmux",
		src: `func inj(m cmux.CMux) net.Listener {
	_, httpL := splitEdgeCmux(m)
	httpL = m.Match(cmux.Any())
	return httpL
}`,
		want: "Match в \"inj\"",
	},
	{
		name: "сервер собран литералом мимо newEdgeHTTPServer",
		src: `func inj(h http.Handler) *http.Server {
	return &http.Server{Handler: h}
}`,
		want: "http.Server собирается в \"inj\"",
	},
	{
		name: "сервер собран new мимо newEdgeHTTPServer",
		src: `func inj() *http.Server {
	return new(http.Server)
}`,
		want: "http.Server собирается в \"inj\"",
	},
	{
		name: "матчер cmux мимо splitEdgeCmux",
		src: `func inj(m cmux.CMux) net.Listener {
	return m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
}`,
		want: "MatchWithWriters в \"inj\"",
	},
}

// Законный близнец той же формы: сервер параметром, его Shutdown, ServeMux,
// обращение к общему серверу — перепись молчит.
const edgeAssemblyTwin = `func twin(ctx context.Context, srv *http.Server) *http.ServeMux {
	_ = srv.Shutdown(ctx)
	return http.NewServeMux()
}`

func parseInjected(t *testing.T, fset *token.FileSet, body string) *ast.File {
	t.Helper()
	src := "package main\n\nimport (\n\t\"context\"\n\t\"net\"\n\t\"net/http\"\n\n\t\"github.com/soheilhy/cmux\"\n\t\"google.golang.org/grpc\"\n\n\t\"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin\"\n)\n\nvar (\n\t_ context.Context\n\t_ net.Listener\n\t_ http.Handler\n\t_ cmux.CMux\n\t_ *grpc.Server\n\t_ = listenerorigin.ConnContext\n)\n\n" + body + "\n"
	f, err := parser.ParseFile(fset, "injected.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("инъекция не разбирается: %v", err)
	}
	return f
}

func TestEdgeH2REST_EdgeAssemblyCensusSeesEveryBypassForm(t *testing.T) {
	fset, files := parseRootPackage(t)
	control := censusEdgeAssembly(fset, files, len(probedEdgeListeners))
	if len(control.findings) != 0 {
		t.Fatalf("контроль не чист — инъекции судить не с чем: %v", control.findings)
	}

	t.Run("законный близнец молчит", func(t *testing.T) {
		c := censusEdgeAssembly(fset, append(append([]*ast.File{}, files...), parseInjected(t, fset, edgeAssemblyTwin)), len(probedEdgeListeners))
		if len(c.findings) != 0 {
			t.Fatalf("перепись покраснела на законной форме: %v", c.findings)
		}
	})
	for _, inj := range edgeAssemblyInjections {
		t.Run(inj.name, func(t *testing.T) {
			c := censusEdgeAssembly(fset, append(append([]*ast.File{}, files...), parseInjected(t, fset, inj.src)), len(probedEdgeListeners))
			for _, f := range c.findings {
				if strings.Contains(f, inj.want) {
					t.Logf("находка: %s", f)
					return
				}
			}
			t.Fatalf("обход не найден: ждали находку со словами %q, получили %v", inj.want, c.findings)
		})
	}
}
