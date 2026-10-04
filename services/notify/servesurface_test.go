// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// servesurface_test.go — NTF1-G19 (перепись поверхности) и УК23: у notify нет
// входящего глагола, и это сверяется с ВЕДОМОСТЬЮ корней
// (servesurface_ledger.go), а не с константой.
//
// # Что судится
//
// Не-тестовые файлы пакетов kacho под `services/notify/` — кроме
// `cmd/notify-probe` и пакетов, достижимых ТОЛЬКО из его корня (фикстурный
// источник стенда, а не шлюз). В них — вызовы, чья цель по идентичности
// объекта (`go/types`, а не текст):
//
//   - `google.golang.org/grpc.NewServer`;
//   - точка входа фундамента, поднимающая gRPC-сервер. Перечень НЕ пишется по
//     памяти: он выводится из пина corelib — замыкание вызывающих
//     `grpc.NewServer` по не-тестовым файлам пакетов corelib, достижимых из
//     notify; экспортируемые функции замыкания — точки входа. Новая точка
//     входа фундамента входит в перечень сама;
//   - функция `Register…Server` сгенерированного стаба (первый параметр —
//     `grpc.ServiceRegistrar`).
//
// Каждый такой вызов судится по ведомости корня, достигающего пакета: сервис
// регистрации — в строке корня; подъём сервера — только у корня с непустой
// строкой. Корень `cmd/*` без строки ведомости — находка; строка без корня —
// находка (самоистечение).
//
// # Чего разбор не видит — и как это сказано
//
// Вызов через значение-функцию или метод интерфейса статически не
// разрешается. Перепись печатает их число отдельной строкой: появление такого
// вызова заметно по ней, а исход держит перепись портов пода (G19, первая
// половина сценария).
//
// Фундамент в предмет не входит, и это не послабление: notify импортирует его
// законно (самоотчёт — `servicecontract` → `grpcsrv`). Судится ВЫЗОВ из кода
// notify, а не достижимость по импорту.
package notify_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify"
)

const (
	notifyPkgPrefix  = "github.com/PRO-Robotech/kacho/services/notify"
	notifyProbeRoot  = notifyPkgPrefix + "/cmd/notify-probe"
	corelibPrefix    = "github.com/PRO-Robotech/corelib/"
	grpcNewServer    = "google.golang.org/grpc.NewServer"
	grpcRegistrarArg = "google.golang.org/grpc.ServiceRegistrar"
)

// listedPkg — пакет вывода `go list -json`.
type listedPkg struct {
	ImportPath string
	Dir        string
	Name       string
	GoFiles    []string
	CgoFiles   []string
	Export     string
	DepOnly    bool
	Imports    []string
	ImportMap  map[string]string
	Error      *struct{ Err string }
}

// repoRootOf — корень репозитория (каталог с go.mod модуля kacho).
func repoRootOf(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("корень репозитория: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %s без go.mod", root)
	}
	return root
}

// goListDeps — `go list -deps` по шаблонам в дереве root; overlay — файлы,
// добавленные поверх дерева (путь от корня → содержимое): инъекция настоящим
// входом без правки дерева.
func goListDeps(t *testing.T, root string, overlay map[string]string, export bool, patterns ...string) []listedPkg {
	t.Helper()
	args := []string{"list", "-e", "-deps",
		"-json=ImportPath,Dir,Name,GoFiles,CgoFiles,Export,DepOnly,Imports,ImportMap,Error"}
	if export {
		args = append(args, "-export")
	}
	if len(overlay) > 0 {
		dir := t.TempDir()
		repl := map[string]string{}
		i := 0
		for rel, src := range overlay {
			p := filepath.Join(dir, fmt.Sprintf("overlay%d.go", i))
			i++
			if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
				t.Fatalf("оверлей: %v", err)
			}
			repl[filepath.Join(root, filepath.FromSlash(rel))] = p
		}
		body, _ := json.Marshal(map[string]any{"Replace": repl})
		f := filepath.Join(dir, "overlay.json")
		if err := os.WriteFile(f, body, 0o600); err != nil {
			t.Fatalf("оверлей: %v", err)
		}
		args = append(args, "-overlay", f)
	}
	args = append(args, patterns...)
	cmd := exec.Command("go", args...) // #nosec G204 -- argv собран из констант пробы
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	var pkgs []listedPkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPkg
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: разбор вывода go list: %v", err)
		}
		if p.Error != nil {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: пакет %s: %s", p.ImportPath, p.Error.Err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: go list по %v не дал ни одного пакета", patterns)
	}
	return pkgs
}

// typedPkg — пакет, разобранный и проверенный типами.
type typedPkg struct {
	path  string
	files []*ast.File
	names []string // путь файла от корня репозитория
	info  *types.Info
}

// typeCheck проверяет типами пакет lp; зависимости — по данным экспорта
// `go list -export`. Содержимое файла оверлея берётся из overlay.
func typeCheck(t *testing.T, fset *token.FileSet, root string, lp listedPkg, exports map[string]string,
	overlay map[string]string) *typedPkg {
	t.Helper()
	if len(lp.CgoFiles) > 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: пакет %s несёт cgo-файлы — разбор без cgo их не видит", lp.ImportPath)
	}
	tp := &typedPkg{path: lp.ImportPath, info: &types.Info{
		Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}}
	for _, name := range lp.GoFiles {
		abs := filepath.Join(lp.Dir, name)
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = abs
		}
		rel = filepath.ToSlash(rel)
		src, err := sourceOf(abs, rel, overlay)
		if err != nil {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: чтение %s: %v", abs, err)
		}
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: разбор %s: %v", abs, err)
		}
		tp.files = append(tp.files, f)
		tp.names = append(tp.names, rel)
	}
	gc := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		exp, ok := exports[path]
		if !ok || exp == "" {
			return nil, fmt.Errorf("данных экспорта %s нет", path)
		}
		return os.Open(exp) // #nosec G304 -- путь из вывода go list
	})
	conf := types.Config{Importer: importerFunc(func(path string) (*types.Package, error) {
		if mapped, ok := lp.ImportMap[path]; ok {
			path = mapped
		}
		return gc.Import(path)
	})}
	if _, err := conf.Check(lp.ImportPath, fset, tp.files, tp.info); err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: типы пакета %s: %v", lp.ImportPath, err)
	}
	return tp
}

// sourceOf — содержимое файла: из оверлея, если файл подменён, иначе с диска.
func sourceOf(abs, rel string, overlay map[string]string) ([]byte, error) {
	if body, ok := overlay[rel]; ok {
		return []byte(body), nil
	}
	return os.ReadFile(abs) // #nosec G304 -- путь из вывода go list
}

type importerFunc func(string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// calleeOf — цель вызова по идентичности: статическая функция (полное имя и
// объект) либо признак вызова через значение-функцию или метод интерфейса.
func calleeOf(info *types.Info, call *ast.CallExpr) (fn *types.Func, dynamic bool) {
	fun := ast.Unparen(call.Fun)
	switch x := fun.(type) {
	case *ast.IndexExpr:
		fun = x.X
	case *ast.IndexListExpr:
		fun = x.X
	}
	var obj types.Object
	switch x := fun.(type) {
	case *ast.Ident:
		obj = info.Uses[x]
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[x]; ok {
			obj = sel.Obj()
		} else {
			obj = info.Uses[x.Sel]
		}
	case *ast.FuncLit:
		return nil, false
	default:
		return nil, true
	}
	switch o := obj.(type) {
	case *types.Func:
		if sig, ok := o.Type().(*types.Signature); ok && sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) {
			return nil, true
		}
		return o, false
	case *types.Var:
		return nil, true
	}
	return nil, false
}

// enclosingOwner — полное имя объявления, внутри которого стоит позиция.
func enclosingOwner(info *types.Info, f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || pos < fd.Pos() || pos >= fd.End() {
			continue
		}
		if fn, ok := info.Defs[fd.Name].(*types.Func); ok {
			return fn.FullName()
		}
		return fd.Name.Name
	}
	return "<инициализатор пакета>"
}

var registerStub = regexp.MustCompile(`^Register(\w+)Server$`)

// stubService — имя сервиса, если fn — регистрация сгенерированного стаба.
func stubService(fn *types.Func) (string, bool) {
	m := registerStub.FindStringSubmatch(fn.Name())
	if m == nil {
		return "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Params().Len() == 0 {
		return "", false
	}
	if sig.Params().At(0).Type().String() != grpcRegistrarArg {
		return "", false
	}
	return m[1], true
}

// surfaceReport — исход переписи поверхности notify.
type surfaceReport struct {
	subjectPkgs, files, calls, dynamic int
	corelibPkgs                        int
	entryPoints                        []string
	roots                              []string
	excluded                           []string
	findings                           []string
}

func (r surfaceReport) String() string {
	return fmt.Sprintf("NTF1-G19: корней %d %v · пакетов в предмете %d (исключено как корень пробы %d) · "+
		"файлов %d · вызовов %d · пакетов corelib в замыкании %d · точки входа фундамента %v · находок %d\n"+
		"  вызовов через значение-функцию или метод интерфейса (цель статически не видна): %d",
		len(r.roots), r.roots, r.subjectPkgs, len(r.excluded), r.files, r.calls, r.corelibPkgs, r.entryPoints,
		len(r.findings), r.dynamic)
}

// auditServeSurface — перепись поверхности notify по дереву root (с
// оверлеем overlay) и ведомости ledger.
func auditServeSurface(t *testing.T, root string, overlay map[string]string, ledger map[string][]string) surfaceReport {
	t.Helper()
	listing := goListDeps(t, root, overlay, true, "./services/notify/...")
	byPath := map[string]listedPkg{}
	exports := map[string]string{}
	for _, p := range listing {
		byPath[p.ImportPath] = p
		exports[p.ImportPath] = p.Export
	}
	var r surfaceReport

	// Корни и достижимость пакетов notify из каждого.
	reach := map[string]map[string]bool{} // корень → пакеты notify
	for _, p := range listing {
		if p.Name != "main" || !strings.HasPrefix(p.ImportPath, notifyPkgPrefix+"/cmd/") {
			continue
		}
		seen := map[string]bool{}
		var walk func(string)
		walk = func(ip string) {
			if seen[ip] {
				return
			}
			seen[ip] = true
			for _, imp := range byPath[ip].Imports {
				if imp == notifyPkgPrefix || strings.HasPrefix(imp, notifyPkgPrefix+"/") {
					walk(imp)
				}
			}
		}
		walk(p.ImportPath)
		reach[p.ImportPath] = seen
	}
	rootRel := func(ip string) string { return strings.TrimPrefix(ip, notifyPkgPrefix+"/") }
	for ip := range reach {
		if ip == notifyProbeRoot {
			continue
		}
		r.roots = append(r.roots, rootRel(ip))
		if _, ok := ledger[rootRel(ip)]; !ok {
			r.findings = append(r.findings, fmt.Sprintf("корень без записи: services/notify/%s — процесс под "+
				"services/notify/cmd/ не назван в ведомости поверхности (servesurface_ledger.go, УК23)", rootRel(ip)))
		}
	}
	sort.Strings(r.roots)
	for rel := range ledger {
		if _, ok := reach[notifyPkgPrefix+"/"+rel]; !ok {
			r.findings = append(r.findings, fmt.Sprintf("строка ведомости без корня: %s — корня процесса с таким "+
				"путём под services/notify нет; запись пережила свой предмет", rel))
		}
	}

	// Пакеты предмета: notify вне корня пробы и не достижимые только из него.
	var subject []listedPkg
	for _, p := range listing {
		if p.DepOnly || !(p.ImportPath == notifyPkgPrefix || strings.HasPrefix(p.ImportPath, notifyPkgPrefix+"/")) {
			continue
		}
		if p.ImportPath == notifyProbeRoot || strings.HasPrefix(p.ImportPath, notifyProbeRoot+"/") {
			r.excluded = append(r.excluded, p.ImportPath)
			continue
		}
		byProbe, byOther := reach[notifyProbeRoot][p.ImportPath], false
		for ip, seen := range reach {
			if ip != notifyProbeRoot && seen[p.ImportPath] {
				byOther = true
			}
		}
		if byProbe && !byOther {
			r.excluded = append(r.excluded, p.ImportPath)
			continue
		}
		subject = append(subject, p)
	}
	r.subjectPkgs = len(subject)

	fset := token.NewFileSet()

	// Точки входа фундамента: замыкание вызывающих grpc.NewServer.
	callers := map[string][]string{} // цель → вызывающие объявления
	exported := map[string]bool{}
	for _, p := range listing {
		if !strings.HasPrefix(p.ImportPath, corelibPrefix) {
			continue
		}
		r.corelibPkgs++
		tp := typeCheck(t, fset, root, p, exports, nil)
		for _, f := range tp.files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				owner, ok := tp.info.Defs[fd.Name].(*types.Func)
				if !ok {
					continue
				}
				exported[owner.FullName()] = owner.Exported()
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if fn, _ := calleeOf(tp.info, call); fn != nil {
						callers[fn.FullName()] = append(callers[fn.FullName()], owner.FullName())
					}
					return true
				})
			}
		}
	}
	closure := map[string]bool{}
	queue := []string{grpcNewServer}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range callers[cur] {
			if !closure[c] {
				closure[c] = true
				queue = append(queue, c)
			}
		}
	}
	entry := map[string]bool{grpcNewServer: true}
	for fn := range closure {
		if exported[fn] {
			entry[fn] = true
			r.entryPoints = append(r.entryPoints, fn)
		}
	}
	sort.Strings(r.entryPoints)

	// Вызовы в пакетах предмета.
	for _, p := range subject {
		tp := typeCheck(t, fset, root, p, exports, overlay)
		var rootsOf []string
		for ip, seen := range reach {
			if ip != notifyProbeRoot && seen[p.ImportPath] {
				rootsOf = append(rootsOf, rootRel(ip))
			}
		}
		sort.Strings(rootsOf)
		for i, f := range tp.files {
			r.files++
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				r.calls++
				fn, dynamic := calleeOf(tp.info, call)
				if dynamic {
					r.dynamic++
					return true
				}
				if fn == nil {
					return true
				}
				at := fmt.Sprintf("%s:%d", tp.names[i], fset.Position(call.Pos()).Line)
				owner := enclosingOwner(tp.info, f, call.Pos())
				if svc, ok := stubService(fn); ok {
					for _, rt := range orNoRoot(rootsOf) {
						if !ledgerServes(ledger[rt], svc) {
							r.findings = append(r.findings, fmt.Sprintf("%s: сервис вне ведомости — %s регистрирует "+
								"%s (%s), а строка корня %s его не несёт (NTF1-G19)", at, owner, svc, fn.FullName(), rt))
						}
					}
					return true
				}
				if entry[fn.FullName()] {
					for _, rt := range orNoRoot(rootsOf) {
						if len(ledger[rt]) == 0 {
							r.findings = append(r.findings, fmt.Sprintf("%s: подъём gRPC-сервера — %s зовёт %s, а "+
								"строка корня %s пуста: у notify нет входящего глагола (NTF1-G19)", at, owner,
								fn.FullName(), rt))
						}
					}
				}
				return true
			})
		}
	}
	sort.Strings(r.findings)
	return r
}

// orNoRoot — корни пакета; пакет, не достижимый ни из одного корня, судится
// под меткой «без корня» (строки ведомости у него нет — любой вызов находка).
func orNoRoot(roots []string) []string {
	if len(roots) == 0 {
		return []string{"<без корня>"}
	}
	return roots
}

// ledgerServes — сервис svc (короткое имя) в строке ведомости (полные имена).
func ledgerServes(row []string, svc string) bool {
	for _, full := range row {
		if full == svc || strings.HasSuffix(full, "."+svc) {
			return true
		}
	}
	return false
}

// requireSurfaceCensus — предпосылки: обход непуст, перечень точек входа
// выведен и несёт известную точку фундамента.
func requireSurfaceCensus(t *testing.T, r surfaceReport) {
	t.Helper()
	if r.subjectPkgs == 0 || r.files == 0 || r.calls == 0 || len(r.roots) == 0 {
		t.Fatalf("пустой обход — не вердикт: %s", r)
	}
	if r.corelibPkgs == 0 || len(r.entryPoints) == 0 {
		t.Fatalf("пустой перечень точек входа фундамента — не вердикт: %s", r)
	}
	if !contains(r.entryPoints, "github.com/PRO-Robotech/corelib/grpcsrv.NewServer") {
		t.Fatalf("выведенный перечень точек входа не несёт grpcsrv.NewServer — замыкание вызывающих "+
			"grpc.NewServer сломано: %s", r)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestNTF1G19_NotifyServesNoInboundVerb — перепись поверхности notify по дереву
// против ведомости корней.
func TestNTF1G19_NotifyServesNoInboundVerb(t *testing.T) {
	t.Parallel()
	root := repoRootOf(t)
	r := auditServeSurface(t, root, nil, notify.ServeSurfaceLedger)
	t.Log(r.String())
	t.Logf("исключено из предмета (корень пробы и пакеты только его): %v", r.excluded)
	requireSurfaceCensus(t, r)
	for _, f := range r.findings {
		t.Errorf("NTF1-G19 · %s", f)
	}
}

// TestNTF1G19Injection — инъекции оверлеем поверх настоящего дерева.
func TestNTF1G19Injection(t *testing.T) {
	t.Parallel()
	root := repoRootOf(t)
	if control := auditServeSurface(t, root, nil, notify.ServeSurfaceLedger); len(control.findings) != 0 {
		t.Fatalf("контроль не чист — инъекция не отличима от дерева: %v", control.findings)
	}
	cases := []struct {
		name    string
		overlay map[string]string
		ledger  map[string][]string
		want    string // пусто — близнец молчит
		at      string
	}{
		{
			name: "порча: grpcsrv.NewServer в cmd/notify",
			overlay: map[string]string{"services/notify/cmd/notify/zz_g19_inject.go": `package main

import "github.com/PRO-Robotech/corelib/grpcsrv"

func g19Inject() { _ = grpcsrv.NewServer() }
`},
			ledger: notify.ServeSurfaceLedger,
			want:   "подъём gRPC-сервера", at: "services/notify/cmd/notify/zz_g19_inject.go:5",
		},
		{
			name: "порча: grpc.NewServer напрямую во внутреннем пакете notify",
			overlay: map[string]string{"services/notify/internal/config/zz_g19_inject.go": `package config

import "google.golang.org/grpc"

func g19Inject() { _ = grpc.NewServer() }
`},
			ledger: notify.ServeSurfaceLedger,
			want:   "подъём gRPC-сервера", at: "services/notify/internal/config/zz_g19_inject.go:5",
		},
		{
			name: "порча: регистрация стаба в cmd/notify",
			overlay: map[string]string{"services/notify/cmd/notify/zz_g19_inject.go": `package main

import (
	"google.golang.org/grpc"
	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
)

func g19Inject(s *grpc.Server) { notifyv1.RegisterInternalNotificationFeedServiceServer(s, nil) }
`},
			ledger: notify.ServeSurfaceLedger,
			want:   "сервис вне ведомости", at: "services/notify/cmd/notify/zz_g19_inject.go:8",
		},
		{
			name: "порча: корень без записи ведомости",
			overlay: map[string]string{"services/notify/cmd/zz-g19-root/main.go": `package main

func main() {}
`},
			ledger: notify.ServeSurfaceLedger,
			want:   "корень без записи: services/notify/cmd/zz-g19-root",
		},
		{
			name:    "порча: строка ведомости без корня",
			ledger:  withRow(notify.ServeSurfaceLedger, "cmd/zz-gone", nil),
			want:    "строка ведомости без корня: cmd/zz-gone",
			overlay: nil,
		},
		{
			name: "близнец: регистрация сервиса, названного строкой корня",
			overlay: map[string]string{"services/notify/cmd/notify/zz_g19_inject.go": `package main

import (
	"google.golang.org/grpc"
	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
)

func g19Inject(s *grpc.Server) { notifyv1.RegisterInternalNotificationFeedServiceServer(s, nil) }
`},
			ledger: withRow(notify.ServeSurfaceLedger, "cmd/notify",
				[]string{"corelib.notify.InternalNotificationFeedService"}),
		},
		{
			name: "близнец: самоотчёт через servicecontract (импорт grpcsrv), без вызова точки входа",
			overlay: map[string]string{"services/notify/cmd/notify/zz_g19_inject.go": `package main

import "github.com/PRO-Robotech/corelib/grpcsrv"

func g19Twin() string { return grpcsrv.ServiceIdentityNotApplicable }
`},
			ledger: notify.ServeSurfaceLedger,
		},
	}
	for _, tc := range cases {
		r := auditServeSurface(t, root, tc.overlay, tc.ledger)
		got := strings.Join(r.findings, "\n")
		switch {
		case tc.want == "" && len(r.findings) != 0:
			t.Errorf("%s: законный близнец объявлен находкой: %s", tc.name, got)
		case tc.want != "" && !strings.Contains(got, tc.want):
			t.Errorf("%s: находки %q нет: %s", tc.name, tc.want, got)
		case tc.at != "" && !strings.Contains(got, tc.at):
			t.Errorf("%s: находка без координаты %s: %s", tc.name, tc.at, got)
		}
		t.Logf("%s → находок %d: %s", tc.name, len(r.findings), got)
	}
}

// withRow — копия ведомости с заменённой строкой.
func withRow(ledger map[string][]string, rel string, row []string) map[string][]string {
	out := make(map[string][]string, len(ledger)+1)
	for k, v := range ledger {
		out[k] = v
	}
	out[rel] = row
	return out
}
