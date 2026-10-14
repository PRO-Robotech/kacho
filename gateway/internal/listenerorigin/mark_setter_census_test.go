// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listenerorigin_test

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// mark_setter_census_test.go — перепись экспортированных установщиков метки
// «внутренний» пакета listenerorigin против ведомости разрешённых (kacho#3132).
//
// Гейты сборки края (TestEdgeH2REST_EdgeAssemblyDoesNotLeaveTheRoot,
// TestEdgeH2REST_EdgeAssemblyHasASingleHome) запрещают звать известные имена
// установщиков вне корня, но сам пакет-владелец не судят: новая экспортированная
// функция пакета, ставящая метку (обёртка над WithInternal, свой литерал
// метки), их обходит. Эта перепись держит обратное: множество экспортированных
// имён пакета, через которые метка «внутренний» достижима, равно ведомости
// markSetterLedger — ни больше, ни меньше.
//
// Как опознаётся установщик (пакет проверяется типами, а не текстом):
//
//   - ИСТОЧНИК — объявление пакетного уровня, тело которого называет тип самой
//     метки (markInternal) либо тип соединения, которое ConnContext метит
//     внутренним (internalConn), в ЛЮБОЙ позиции, кроме позиции читателя:
//     составной литерал, объявление переменной, new(...), приведение — всё
//     создаёт значение метки. Позиции читателя — тип в утверждении x.(T) и
//     перечень case переключателя по типу: IsExternal и connOrigin метку только
//     читают и установщиками не являются;
//   - РЕБРО — обращение тела к другому объявлению пакета: вызов либо значение
//     функции, метода, пакетной переменной; обращение к типу пакета ведёт ко
//     всем его методам (значение типа, отданное наружу как интерфейс, исполняет
//     их без явного вызова — так InternalListener достигает Accept);
//     присваивание пакетной переменной в любом теле — ребро от переменной к
//     правой части (подмена значения в init);
//   - УСТАНОВЩИК — объявление, из которого источник достижим; в ведомость
//     сверяются ЭКСПОРТИРОВАННЫЕ имена: функции, пакетные переменные и методы с
//     экспортированным именем (их зовут через интерфейс — net.Listener.Accept).
//
// Исходы: установщик вне ведомости — находка с его именем; запись ведомости,
// которой нет среди установщиков, — находка (запись пережила свой предмет);
// пустой обход либо пакет без типа метки — отказ предпосылки, а не зелёное.
// Перепись печатает объём осмотренного.
//
// Чего перепись НЕ держит: метку, поставленную в обход системы типов пакета
// (unsafe, go:linkname, рефлексия по значению, полученному снаружи), и
// неэкспортированные установщики — их зовёт только сам пакет, а его внешнюю
// поверхность перепись перечисляет целиком.

// markSetterLedger — экспортированные имена пакета, через которые метка
// «внутренний» законно достижима, и почему каждое из них законно.
var markSetterLedger = map[string]string{
	"WithInternal":               "метка на контексте; вне пакета её зов запрещён гейтом сборки края, в пакете — ConnContext",
	"InternalListener":           "обёртка ЕДИНСТВЕННОГО внутреннего слушателя края; вне serveInternalREST запрещена гейтами сборки края",
	"ConnContext":                "перехватчик соединения общего HTTP-сервера; метит внутренним только соединение, прошедшее InternalListener",
	"(*internalListener).Accept": "метод обёртки, достижимый через net.Listener; создаёт соединение с меткой",
}

// markTypes — типы, значение которых И ЕСТЬ метка «внутренний».
var markTypes = []string{"markInternal", "internalConn"}

type markSetterCensus struct {
	files    int
	decls    int
	sources  int
	setters  []string // экспортированные установщики, по алфавиту
	findings []string
}

func (c markSetterCensus) String() string {
	return fmt.Sprintf("файлов %d · объявлений %d · мест создания метки %d · экспортированных установщиков %d · находок %d",
		c.files, c.decls, c.sources, len(c.setters), len(c.findings))
}

// declName — имя объявления в форме ведомости: F, (*T).M либо T.M.
func declName(obj types.Object) string {
	fn, ok := obj.(*types.Func)
	if !ok {
		return obj.Name()
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil {
		return fn.Name()
	}
	recv := sig.Recv().Type()
	if p, isPtr := recv.(*types.Pointer); isPtr {
		return "(*" + p.Elem().(*types.Named).Obj().Name() + ")." + fn.Name()
	}
	return recv.(*types.Named).Obj().Name() + "." + fn.Name()
}

// sharedTypeEnv — один набор позиций и один импортёр исходников на все
// переписи процесса: импортёр помнит типизированную стандартную библиотеку, и
// каждая инъекция не типизирует её заново.
var sharedTypeEnv = sync.OnceValues(func() (*token.FileSet, types.Importer) {
	fset := token.NewFileSet()
	return fset, importer.ForCompiler(fset, "source", nil)
})

// censusMarkSetters типизирует файлы пакета и сверяет его экспортированные
// установщики метки с ведомостью.
func censusMarkSetters(files map[string]string, ledger map[string]string) (markSetterCensus, error) {
	var c markSetterCensus
	fset, imp := sharedTypeEnv()
	var parsed []*ast.File
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, err := parser.ParseFile(fset, n, files[n], parser.SkipObjectResolution)
		if err != nil {
			return c, err
		}
		parsed = append(parsed, f)
	}
	c.files = len(parsed)
	if c.files == 0 {
		return c, fmt.Errorf("предпосылка: не-тестовых файлов пакета 0 — проверять нечего, это не зелёное")
	}

	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check("listenerorigin", fset, parsed, info)
	if err != nil {
		return c, fmt.Errorf("типизация пакета: %w", err)
	}

	isMarkType := map[types.Object]bool{}
	for _, name := range markTypes {
		obj := pkg.Scope().Lookup(name)
		if _, ok := obj.(*types.TypeName); !ok {
			return c, fmt.Errorf("предпосылка: в пакете нет типа метки %s — перепись сверяет не тот предмет", name)
		}
		isMarkType[obj] = true
	}

	// methodsOf — методы типа пакета (значения и указателя), объявленные в пакете.
	methodsOf := func(tn *types.TypeName) []types.Object {
		var out []types.Object
		for _, t := range []types.Type{tn.Type(), types.NewPointer(tn.Type())} {
			ms := types.NewMethodSet(t)
			for i := 0; i < ms.Len(); i++ {
				if m := ms.At(i).Obj(); m.Pkg() == pkg {
					out = append(out, m)
				}
			}
		}
		return out
	}

	edges := map[types.Object]map[types.Object]bool{}
	source := map[types.Object]bool{}
	addEdge := func(from, to types.Object) {
		if edges[from] == nil {
			edges[from] = map[types.Object]bool{}
		}
		edges[from][to] = true
	}
	isNode := func(obj types.Object) bool {
		if obj == nil || obj.Pkg() != pkg {
			return false
		}
		switch o := obj.(type) {
		case *types.Func:
			return true
		case *types.Var:
			return o.Parent() == pkg.Scope()
		}
		return false
	}

	// scan — тело объявления from: источники, рёбра и присваивания пакетным переменным.
	scan := func(from types.Object, body ast.Node) {
		reader := map[*ast.Ident]bool{}
		markReaders := func(n ast.Node) {
			ast.Inspect(n, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					reader[id] = true
				}
				return true
			})
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.TypeAssertExpr:
				if x.Type != nil {
					markReaders(x.Type)
				}
			case *ast.TypeSwitchStmt:
				for _, s := range x.Body.List {
					for _, e := range s.(*ast.CaseClause).List {
						markReaders(e)
					}
				}
			}
			return true
		})
		ast.Inspect(body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || !isNode(info.Uses[id]) {
						continue
					}
					v := info.Uses[id]
					for _, rhs := range x.Rhs {
						ast.Inspect(rhs, func(m ast.Node) bool {
							if rid, ok := m.(*ast.Ident); ok && isNode(info.Uses[rid]) {
								addEdge(v, info.Uses[rid])
							}
							return true
						})
					}
				}
			case *ast.Ident:
				obj := info.Uses[x]
				if obj == nil || obj.Pkg() != pkg || reader[x] {
					return true
				}
				if tn, ok := obj.(*types.TypeName); ok {
					if isMarkType[tn] {
						source[from] = true
						c.sources++
					}
					for _, m := range methodsOf(tn) {
						addEdge(from, m)
					}
					return true
				}
				if isNode(obj) {
					addEdge(from, obj)
				}
			}
			return true
		})
	}

	var decls []types.Object
	for _, f := range parsed {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				obj := info.Defs[d.Name]
				decls = append(decls, obj)
				if d.Body != nil {
					scan(obj, d.Body)
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, s := range d.Specs {
					vs := s.(*ast.ValueSpec)
					for _, id := range vs.Names {
						obj := info.Defs[id]
						if obj == nil {
							continue
						}
						decls = append(decls, obj)
						if vs.Type != nil {
							scan(obj, vs.Type)
						}
						for _, v := range vs.Values {
							scan(obj, v)
						}
					}
				}
			}
		}
	}
	c.decls = len(decls)

	// Замыкание: установщик — объявление, из которого источник достижим.
	setter := map[types.Object]bool{}
	for o := range source {
		setter[o] = true
	}
	for changed := true; changed; {
		changed = false
		for from, tos := range edges {
			if setter[from] {
				continue
			}
			for to := range tos {
				if setter[to] {
					setter[from] = true
					changed = true
					break
				}
			}
		}
	}

	found := map[string]bool{}
	for _, o := range decls {
		if setter[o] && o.Exported() {
			found[declName(o)] = true
		}
	}
	for name := range found {
		c.setters = append(c.setters, name)
		if _, ok := ledger[name]; !ok {
			c.findings = append(c.findings, fmt.Sprintf("экспортированный установщик метки «внутренний» %s вне ведомости markSetterLedger", name))
		}
	}
	sort.Strings(c.setters)
	for name := range ledger {
		if !found[name] {
			c.findings = append(c.findings, fmt.Sprintf("запись ведомости %s без предмета: метку через неё больше не поставить — снять запись", name))
		}
	}
	sort.Strings(c.findings)
	return c, nil
}

// packageSources — не-тестовые исходники пакета listenerorigin.
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("чтение каталога пакета: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(n))
		if err != nil {
			t.Fatalf("чтение %s: %v", n, err)
		}
		out[n] = string(b)
	}
	return out
}

// TestListenerOriginMarkSettersMatchTheLedger — живой пакет: экспортированные
// установщики метки «внутренний» ровно те, что в ведомости.
func TestListenerOriginMarkSettersMatchTheLedger(t *testing.T) {
	c, err := censusMarkSetters(packageSources(t), markSetterLedger)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("перепись: %s · установщики %v", c, c.setters)
	if c.sources == 0 {
		t.Fatalf("предпосылка: мест создания метки 0 при непустом пакете — распознаватель источника слеп (%s)", c)
	}
	for _, f := range c.findings {
		t.Error(f)
	}
}

// withExtra — живой пакет плюс синтетический файл.
func withExtra(t *testing.T, src string) map[string]string {
	t.Helper()
	files := packageSources(t)
	files["zz_injected.go"] = "package listenerorigin\n\n" + src
	return files
}

func ledgerPlus(extra ...string) map[string]string {
	out := map[string]string{}
	for k, v := range markSetterLedger {
		out[k] = v
	}
	for _, e := range extra {
		out[e] = "синтетика пробы"
	}
	return out
}

// TestListenerOriginMarkSetterCensusSeesEveryForm — инъекции: каждая законная
// форма установщика вне ведомости краснеет и называет себя; читатели метки и
// установщик, внесённый в ведомость, молчат; запись без предмета краснеет.
func TestListenerOriginMarkSetterCensusSeesEveryForm(t *testing.T) {
	red := []struct {
		name, src, want string
	}{
		{"обёртка над WithInternal", `import "context"
func Mark(ctx context.Context) context.Context { return WithInternal(ctx) }`, "Mark"},
		{"свой литерал метки", `import "context"
func Tag(ctx context.Context) context.Context { return context.WithValue(ctx, originKey{}, markInternal{}) }`, "Tag"},
		{"переменная типа метки", `import "context"
func Tag(ctx context.Context) context.Context { var m markInternal; return context.WithValue(ctx, originKey{}, m) }`, "Tag"},
		{"соединение с меткой", `import "net"
func Wrap(c net.Conn) net.Conn { return &internalConn{Conn: c} }`, "Wrap"},
		{"через неэкспортированного посредника", `import "context"
func hop(ctx context.Context) context.Context { return WithInternal(ctx) }
func Mark(ctx context.Context) context.Context { return hop(ctx) }`, "Mark"},
		{"экспортированная переменная-функция", `var Mark = WithInternal`, "Mark"},
		{"переменная, подменённая в init", `import "context"
var Mark func(context.Context) context.Context
func init() { Mark = WithInternal }`, "Mark"},
		{"метод экспортированного типа", `import "context"
type Marker struct{}
func (Marker) Mark(ctx context.Context) context.Context { return WithInternal(ctx) }`, "Marker.Mark"},
		{"значение функции в замыкании", `import "context"
func Marker() func(context.Context) context.Context { return func(ctx context.Context) context.Context { return WithInternal(ctx) } }`, "Marker"},
		{"обёртка над InternalListener", `import "net"
func Admin(l net.Listener) net.Listener { return InternalListener(l) }`, "Admin"},
	}
	for _, tc := range red {
		t.Run("красное/"+tc.name, func(t *testing.T) {
			c, err := censusMarkSetters(withExtra(t, tc.src), markSetterLedger)
			if err != nil {
				t.Fatal(err)
			}
			want := "экспортированный установщик метки «внутренний» " + tc.want + " вне ведомости"
			hit := false
			for _, f := range c.findings {
				if strings.Contains(f, want) {
					hit = true
				}
			}
			if !hit || len(c.findings) != 1 {
				t.Fatalf("ожидалась ровно одна находка %q, получено %v (%s)", want, c.findings, c)
			}
		})
	}

	silent := []struct {
		name, src string
		ledger    map[string]string
	}{
		{"читатель утверждением типа", `import "context"
func IsInternal(ctx context.Context) bool { _, ok := ctx.Value(originKey{}).(markInternal); return ok }`, markSetterLedger},
		{"читатель переключателем по типу", `import "net"
func IsInternalConn(c net.Conn) bool { switch c.(type) { case *internalConn: return true }; return false }`, markSetterLedger},
		{"установщик в ведомости", `import "context"
func Mark(ctx context.Context) context.Context { return WithInternal(ctx) }`, ledgerPlus("Mark")},
		{"метка «внешний»", `import "context"
func External(ctx context.Context) context.Context { return context.WithValue(ctx, originKey{}, markExternal{}) }`, markSetterLedger},
	}
	for _, tc := range silent {
		t.Run("молчание/"+tc.name, func(t *testing.T) {
			c, err := censusMarkSetters(withExtra(t, tc.src), tc.ledger)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.findings) != 0 {
				t.Fatalf("законный близнец обязан молчать, находки: %v (%s)", c.findings, c)
			}
		})
	}

	t.Run("красное/запись без предмета", func(t *testing.T) {
		c, err := censusMarkSetters(packageSources(t), ledgerPlus("Gone"))
		if err != nil {
			t.Fatal(err)
		}
		if len(c.findings) != 1 || !strings.Contains(c.findings[0], "запись ведомости Gone без предмета") {
			t.Fatalf("запись без предмета обязана быть находкой, получено %v", c.findings)
		}
	})

	t.Run("предпосылка/пустой обход", func(t *testing.T) {
		if _, err := censusMarkSetters(map[string]string{}, markSetterLedger); err == nil || !strings.Contains(err.Error(), "проверять нечего") {
			t.Fatalf("пустой обход обязан быть отказом предпосылки, получено %v", err)
		}
	})

	t.Run("предпосылка/нет типа метки", func(t *testing.T) {
		files := map[string]string{"a.go": "package listenerorigin\n\nfunc F() {}\n"}
		if _, err := censusMarkSetters(files, map[string]string{}); err == nil || !strings.Contains(err.Error(), "нет типа метки") {
			t.Fatalf("пакет без типа метки обязан быть отказом предпосылки, получено %v", err)
		}
	})
}
