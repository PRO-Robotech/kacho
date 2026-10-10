// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// edge_assembly_outside_root_test.go — сборка края не выносится за пакет корня
// молча (kacho#3125).
//
// Перепись edge_assembly_census_test.go судит ТОЛЬКО пакет корня. Сборку,
// вынесенную в соседний пакет модуля gateway (мультиплексор, деление порта
// своими матчерами, REST-половина без фильтра, метка «внешний» и Serve за
// обёрткой), она не видит, и пробы edge_h2_rest_test.go такую сборку тоже не
// судят. Этот гейт держит вход в такой вынос: в не-тестовых файлах дерева
// gateway вне пакета корня нет ни одного обращения —
//
//   - к пакету cmux, кроме типа соединения MuxConn: ни мультиплексора (New, тип
//     CMux), ни матчеров (Any, HTTP1Fast, HTTP2, HTTP2MatchHeaderField*SendSettings
//     и любых других имён пакета). Без этих имён вне корня нельзя ни создать
//     мультиплексор, ни принять его параметром, ни поделить им порт;
//   - к меткам происхождения слушателя listenerorigin.ExternalListener и
//     listenerorigin.InternalListener (в самом пакете listenerorigin — к ним вне
//     объявления);
//   - к прямой метке «внутренний» на контексте listenerorigin.WithInternal (в
//     самом пакете listenerorigin она законна: её зовёт ConnContext).
//
// Других экспортированных способов поставить метку у пакета нет: WithExternal
// не существует, а ConnContext метит только соединение, прошедшее обёртку
// InternalListener либо ExternalListener (типы соединений неэкспортированы),
// поэтому без запрещённых здесь обёрток метки он не ставит. Что этот перечень
// полон, держит не этот гейт, а перепись пакета-владельца (см. «Чего гейт НЕ
// держит» ниже).
//
// Обращение — любое упоминание узлом разбора: вызов, значение функции, тип,
// переприсвоение. Импорт этих пакетов точкой вне корня — тоже находка: такую
// форму гейт по имени не опознаёт. Парная половина — в переписи корня: фабрику
// newEdgeCmux корень зовёт ровно столько раз, сколько вариантов слушателя под
// пробой, так что мультиплексор, отданный из корня наружу, — находка и там.
//
// Чего гейт НЕ держит (сказано, чтобы не читать больше, чем есть):
//   - тестовые файлы: они собирают свои мультиплексоры законно и в бой не идут;
//   - обход через другие детали — свой разбор HTTP/2 поверх net.Listener,
//     http2.Server.ServeConn, своё деление порта без cmux; такой обход виден
//     только ревью;
//   - обращение к мультиплексору, не называющее пакет cmux ни одним именем
//     (рефлексия по значению, полученному из корня как any): его видно только
//     ревью;
//   - соседние модули: дерево — каталог gateway, другие продукты монорепо не
//     обходятся;
//   - сам пакет listenerorigin: внутри него метку ставят и без экспортированных
//     имён (литерал markInternal{}, соединение internalConn, зов WithInternal).
//     Что из этого выходит наружу, держит перепись пакета-владельца
//     TestListenerOriginMarkSettersMatchTheLedger
//     (gateway/internal/listenerorigin/mark_setter_census_test.go):
//     экспортированные имена пакета, через которые метка «внутренний»
//     достижима, равны её ведомости — новый установщик (обёртка над
//     WithInternal, свой литерал, переменная-функция, метод) краснеет со своим
//     именем, запись ведомости без предмета — тоже. Поведение самих
//     установщиков держат пробы listenerorigin_test.go.
//
// Перечисленные формы выноса — через cmux и метки — поэтому требуют осознанной
// правки этого гейта (и переписи корня вслед за ним), а не проходят зелёным;
// формы из перечня выше гейт не судит.

// edgeAssemblyPartsRule — какие имена пакета вне корня запрещены: перечень
// banned либо, если задан allowed, все имена пакета, кроме перечисленных.
// premise — имена, которые распознаватель обязан увидеть в пакете корня.
type edgeAssemblyPartsRule struct {
	banned  []string
	allowed []string
	premise []string
}

func (r edgeAssemblyPartsRule) bans(name string) bool {
	if r.allowed != nil {
		for _, a := range r.allowed {
			if a == name {
				return false
			}
		}
		return true
	}
	for _, b := range r.banned {
		if b == name {
			return true
		}
	}
	return false
}

// edgeAssemblyPartsOutsideRoot — импорт → правило имён, которым вне корня не место.
var edgeAssemblyPartsOutsideRoot = map[string]edgeAssemblyPartsRule{
	// Тип соединения MuxConn читает linktls (состояние TLS под мультиплексором).
	"github.com/soheilhy/cmux": {
		allowed: []string{"MuxConn"},
		premise: []string{"New", "Any", "CMux"},
	},
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin": {
		banned:  []string{"ExternalListener", "InternalListener", "WithInternal"},
		premise: []string{"ExternalListener", "InternalListener"},
	},
}

// listenerOriginOwnBanned — имена, которые и внутри пакета listenerorigin
// зовутся только в своём объявлении. WithInternal сюда не входит: её зовёт
// ConnContext того же пакета, это и есть законная установка метки.
var listenerOriginOwnBanned = []string{"ExternalListener", "InternalListener"}

// edgeOutsideRootFile — разобранный файл и его путь относительно каталога gateway.
type edgeOutsideRootFile struct {
	rel  string
	file *ast.File
}

type edgeOutsideRootCensus struct {
	files    int
	importer map[string]int // путь импорта → файлов, импортирующих его
	refs     int            // обращений к запрещённым именам (находок по ним)
	findings []string
}

func (c edgeOutsideRootCensus) String() string {
	var parts []string
	for _, k := range sortedKeys(c.importer) {
		parts = append(parts, fmt.Sprintf("импортируют %s %d", k[strings.LastIndex(k, "/")+1:], c.importer[k]))
	}
	return fmt.Sprintf("файлов %d · %s · обращений к деталям сборки %d · находок %d",
		c.files, strings.Join(parts, " · "), c.refs, len(c.findings))
}

func censusEdgeOutsideRoot(fset *token.FileSet, files []edgeOutsideRootFile) edgeOutsideRootCensus {
	c := edgeOutsideRootCensus{files: len(files), importer: map[string]int{}}
	for _, ef := range files {
		f := ef.file
		local := map[string]edgeAssemblyPartsRule{} // локальное имя пакета → правило
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			rule, ok := edgeAssemblyPartsOutsideRoot[path]
			if !ok {
				continue
			}
			c.importer[path]++
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "." {
				c.findings = append(c.findings, fmt.Sprintf("%s: %s импортирован точкой — гейт не опознаёт обращения к его деталям по имени",
					fset.Position(imp.Pos()), path))
				continue
			}
			local[name] = rule
		}
		// В самом пакете listenerorigin метки зовутся без имени пакета.
		var ownNames map[string]bool
		if f.Name.Name == "listenerorigin" {
			ownNames = map[string]bool{}
			for _, n := range listenerOriginOwnBanned {
				ownNames[n] = true
			}
		}
		declared := map[*ast.Ident]bool{}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				declared[fd.Name] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				x, ok := n.X.(*ast.Ident)
				if !ok {
					return true
				}
				if rule, ok := local[x.Name]; ok && rule.bans(n.Sel.Name) {
					c.refs++
					c.findings = append(c.findings, fmt.Sprintf("%s: %s.%s вне пакета корня (%s) — сборку края здесь не судят ни перепись корня, ни пробы края",
						fset.Position(n.Pos()), x.Name, n.Sel.Name, ef.rel))
				}
			case *ast.Ident:
				if ownNames != nil && ownNames[n.Name] && !declared[n] {
					c.refs++
					c.findings = append(c.findings, fmt.Sprintf("%s: %s вне объявления в пакете listenerorigin (%s) — метку ставит только корень",
						fset.Position(n.Pos()), n.Name, ef.rel))
				}
			}
			return true
		})
	}
	return c
}

// gatewayDir — каталог модуля gateway (пакет корня — gateway/cmd/api-gateway).
func gatewayDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("каталог gateway не вычисляется: %v", err)
	}
	if filepath.Base(dir) != "gateway" {
		t.Fatalf("ожидали каталог gateway двумя уровнями выше пакета корня, а это %s", dir)
	}
	return dir
}

// parseGatewayOutsideRoot разбирает не-тестовые .go файлы каталога gateway, кроме
// пакета корня.
func parseGatewayOutsideRoot(t *testing.T) (*token.FileSet, []edgeOutsideRootFile) {
	t.Helper()
	root := gatewayDir(t)
	rootPkg := filepath.Join(root, "cmd", "api-gateway")
	fset := token.NewFileSet()
	var files []edgeOutsideRootFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case path == rootPkg:
				return filepath.SkipDir
			case d.Name() == "testdata" || d.Name() == "vendor" || d.Name() == "node_modules" || (strings.HasPrefix(d.Name(), ".") && path != root):
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		f, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("%s не разбирается: %w", rel, perr)
		}
		files = append(files, edgeOutsideRootFile{rel: rel, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева gateway: %v", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return fset, files
}

// Предпосылка: обход прочитал файлы, среди них — импортёры обоих пакетов (иначе
// «находок 0» значило бы «ни одного файла, где они могли бы быть»), и тот же
// распознаватель на пакете корня ВИДИТ все запрещённые вне корня имена.
func checkEdgeOutsideRootPremise(t *testing.T, outside edgeOutsideRootCensus) {
	t.Helper()
	if outside.files == 0 {
		t.Fatal("обход дерева gateway вне корня не прочитал ни одного файла — гейт судит пустоту")
	}
	for path := range edgeAssemblyPartsOutsideRoot {
		if outside.importer[path] == 0 {
			t.Fatalf("вне корня ни один файл не импортирует %s — распознавание по имени импорта не на чем проверить", path)
		}
	}
	fset, rootFiles := parseRootPackage(t)
	wrapped := make([]edgeOutsideRootFile, 0, len(rootFiles))
	for _, f := range rootFiles {
		wrapped = append(wrapped, edgeOutsideRootFile{rel: "cmd/api-gateway/" + fset.Position(f.Pos()).Filename, file: f})
	}
	inRoot := censusEdgeOutsideRoot(fset, wrapped)
	for _, rule := range edgeAssemblyPartsOutsideRoot {
		for _, n := range rule.premise {
			seen := false
			for _, f := range inRoot.findings {
				if strings.Contains(f, "."+n+" ") {
					seen = true
					break
				}
			}
			if !seen {
				t.Fatalf("распознаватель не видит %s в пакете корня, где оно есть — его молчание вне корня ничего не значит", n)
			}
		}
	}
}

func TestEdgeH2REST_EdgeAssemblyDoesNotLeaveTheRoot(t *testing.T) {
	fset, files := parseGatewayOutsideRoot(t)
	c := censusEdgeOutsideRoot(fset, files)
	t.Logf("перепись дерева gateway вне пакета корня: %s", c)
	checkEdgeOutsideRootPremise(t, c)
	if len(c.findings) > 0 {
		t.Fatalf("детали сборки края вне пакета корня:\n%s", strings.Join(c.findings, "\n"))
	}
}

// edgeOutsideRootInjections — формы выноса сборки, которые гейт обязан видеть.
// Каждая — настоящий исходник соседнего пакета модуля gateway.
var edgeOutsideRootInjections = []struct {
	name, rel, src, want string
}{
	{
		name: "B2: сборка края обёрткой в соседнем пакете",
		rel:  "internal/edgewrap/wrap.go",
		src: `package edgewrap

import (
	"net"
	"net/http"

	"github.com/soheilhy/cmux"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

func Serve(l net.Listener, srv *http.Server) error {
	m := cmux.New(l)
	go func() { _ = srv.Serve(listenerorigin.ExternalListener(m.Match(cmux.Any()))) }()
	return m.Serve()
}
`,
		want: "cmux.Any вне пакета корня (internal/edgewrap/wrap.go)",
	},
	{
		name: "метка «внешний» значением функции под своим именем импорта",
		rel:  "internal/edgewrap/mark.go",
		src: `package edgewrap

import (
	"net"

	lo "github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

var mark = lo.ExternalListener

func Mark(l net.Listener) net.Listener { return mark(l) }
`,
		want: "lo.ExternalListener вне пакета корня",
	},
	{
		name: "метка «внутренний» в соседнем пакете",
		rel:  "internal/edgewrap/internal.go",
		src: `package edgewrap

import (
	"net"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

func Admin(l net.Listener) net.Listener { return listenerorigin.InternalListener(l) }
`,
		want: "listenerorigin.InternalListener вне пакета корня",
	},
	{
		name: "мультиплексор без Any в соседнем пакете",
		rel:  "internal/edgewrap/mux.go",
		src: `package edgewrap

import (
	"net"

	"github.com/soheilhy/cmux"
)

func Mux(l net.Listener) cmux.CMux { return cmux.New(l) }
`,
		want: "cmux.New вне пакета корня",
	},
	{
		name: "E1: деление порта cmux-ом в соседнем пакете без New и Any",
		rel:  "internal/edgeextra/run.go",
		src: `package edgeextra

import (
	"net/http"

	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
)

func Run(m cmux.CMux, g *grpc.Server, srv *http.Server) {
	grpcL := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	restL := m.Match(cmux.HTTP1Fast(), cmux.HTTP2())
	go func() { _ = srv.Serve(restL) }()
	go func() { _ = g.Serve(grpcL) }()
	_ = m.Serve()
}
`,
		want: "cmux.HTTP2MatchHeaderFieldSendSettings вне пакета корня (internal/edgeextra/run.go)",
	},
	{
		name: "мультиплексор параметром без единого матчера cmux",
		rel:  "internal/edgeextra/param.go",
		src: `package edgeextra

import "github.com/soheilhy/cmux"

func Serve(m cmux.CMux) error { return m.Serve() }
`,
		want: "cmux.CMux вне пакета корня (internal/edgeextra/param.go)",
	},
	{
		name: "импорт точкой",
		rel:  "internal/edgewrap/dot.go",
		src: `package edgewrap

import (
	"net"

	. "github.com/soheilhy/cmux"
)

func Mux(l net.Listener) CMux { return New(l) }
`,
		want: "импортирован точкой",
	},
	{
		name: "N1: метка «внутренний» напрямую в ConnContext соседнего пакета",
		rel:  "internal/edgewrap/wrap.go",
		src: `package edgewrap

import (
	"context"
	"net"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

func ConnContext(ctx context.Context, c net.Conn) context.Context {
	return listenerorigin.WithInternal(ctx)
}
`,
		want: "listenerorigin.WithInternal вне пакета корня (internal/edgewrap/wrap.go)",
	},
	{
		name: "метка «внутренний» значением функции под своим именем импорта",
		rel:  "internal/edgewrap/mark2.go",
		src: `package edgewrap

import lo "github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"

var markInternal = lo.WithInternal
`,
		want: "lo.WithInternal вне пакета корня",
	},
	{
		name: "метка внутри пакета listenerorigin вне объявления",
		rel:  "internal/listenerorigin/wrap.go",
		src: `package listenerorigin

import "net"

func Both(l net.Listener) net.Listener { return ExternalListener(InternalListener(l)) }
`,
		want: "ExternalListener вне объявления в пакете listenerorigin",
	},
}

// Законный близнец той же формы: тип соединения cmux (как в linktls), чтение
// метки, метод Any у чужого значения, объявление меток в их пакете.
var edgeOutsideRootTwins = []struct{ rel, src string }{
	{
		rel: "internal/edgewrap/twin.go",
		src: `package edgewrap

import (
	"context"
	"net"

	"github.com/soheilhy/cmux"

	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

type anyer struct{}

func (anyer) Any() bool { return true }

func Twin(ctx context.Context, c net.Conn) bool {
	_, isMux := c.(*cmux.MuxConn)
	var a anyer
	return isMux && a.Any() && listenerorigin.OnExternalListener(ctx)
}
`,
	},
	{
		rel: "internal/listenerorigin/twin.go",
		src: `package listenerorigin

import "net"

func ExternalListener(l net.Listener) net.Listener { return l }

func InternalListener(l net.Listener) net.Listener { return l }
`,
	},
	{
		// Метку ставит сам её пакет: ConnContext зовёт WithInternal в
		// listenerorigin.go, и пакет-владелец вправе звать её где угодно.
		rel: "internal/listenerorigin/twin_withinternal.go",
		src: `package listenerorigin

import "context"

func markForAdmin(ctx context.Context) context.Context { return WithInternal(ctx) }
`,
	},
}

func parseOutsideRootInjected(t *testing.T, fset *token.FileSet, rel, src string) edgeOutsideRootFile {
	t.Helper()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("инъекция %s не разбирается: %v", rel, err)
	}
	return edgeOutsideRootFile{rel: rel, file: f}
}

func TestEdgeH2REST_EdgeAssemblyOutsideRootGateSeesEveryForm(t *testing.T) {
	fset, files := parseGatewayOutsideRoot(t)
	control := censusEdgeOutsideRoot(fset, files)
	if len(control.findings) != 0 {
		t.Fatalf("контроль не чист — инъекции судить не с чем: %v", control.findings)
	}
	for _, tw := range edgeOutsideRootTwins {
		t.Run("законный близнец молчит: "+tw.rel, func(t *testing.T) {
			c := censusEdgeOutsideRoot(fset, append(append([]edgeOutsideRootFile{}, files...), parseOutsideRootInjected(t, fset, tw.rel, tw.src)))
			if len(c.findings) != 0 {
				t.Fatalf("гейт покраснел на законной форме: %v", c.findings)
			}
		})
	}
	for _, inj := range edgeOutsideRootInjections {
		t.Run(inj.name, func(t *testing.T) {
			c := censusEdgeOutsideRoot(fset, append(append([]edgeOutsideRootFile{}, files...), parseOutsideRootInjected(t, fset, inj.rel, inj.src)))
			for _, f := range c.findings {
				if strings.Contains(f, inj.want) {
					t.Logf("находка: %s", f)
					return
				}
			}
			t.Fatalf("вынос не найден: ждали находку со словами %q, получили %v", inj.want, c.findings)
		})
	}
}
