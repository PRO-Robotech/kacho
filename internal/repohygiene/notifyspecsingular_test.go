// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notifyspecsingular_test.go — NTF1-A09: валидатор формата шаблона один на три
// места применения, и он — corelib `notify/spec`.
//
// # Предмет
//
// Реализация формата — функция, открывающая или разбирающая файл формата
// шаблона (`notification.yaml`, `body.<locale>.yaml`, `revision.yaml`). Узлы,
// по которым она опознаётся (корпус — notifyspecsingular_corpus_test.go):
//
//   - УПОМИНАНИЕ имени файла формата — строковое константное выражение
//     (литерал, константа пакета, сложение таких), чьё базовое имя —
//     `notification.yaml`, `revision.yaml` либо `body.<…>.yaml`, или сцепление,
//     начатое константой с базовым именем `body.` и законченное `.yaml`;
//   - ОТКРЫТИЕ — `os.Open`, `os.OpenFile`, `os.ReadFile`, `fs.ReadFile`,
//     `ioutil.ReadFile` либо метод `Open`/`ReadFile` значения (файловая система
//     `fs.FS`);
//   - РАЗБОР — `Unmarshal`/`UnmarshalStrict`/`NewDecoder` пакетов YAML и
//     `encoding/json`.
//
// Функция с упоминанием И с открытием или разбором — реализация; литерал
// функции считается за объемлющее объявление. Вызов `text/template` и
// `html/template` реализацией формата не является: в corelib такой вызов есть
// (`quota/refusal.go`, текст отказа) и к формату шаблонов отношения не имеет.
//
// # Чего гейт не видит — и почему это названо
//
// Разбор методом значения (`(*yaml.Node).Decode`) и имя файла, собранное во
// время исполнения из не-константных частей, разбором исходника не
// опознаются. Остаток держится ревью диффа; гейт печатает число упоминаний
// без открытия и разбора, чтобы такая функция была видна хотя бы счётом.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
	"testing"
)

// notifySpecPkg — пакет формата шаблонов фундамента: единственная реализация.
const notifySpecPkg = corelibModulePath + "/notify/spec"

// notifygenPkgPath — генератор постановки: первый из трёх вызывающих.
const notifygenPkgPath = corelibModulePath + "/cmd/notifygen"

// specKnownNames — имена пакетов, чьё имя не равно последнему элементу пути.
var specKnownNames = map[string]string{
	"gopkg.in/yaml.v3":         "yaml",
	"gopkg.in/yaml.v2":         "yaml",
	"sigs.k8s.io/yaml":         "yaml",
	"github.com/goccy/go-yaml": "yaml",
}

// specOpenFuncs — функции пакетов, открывающие или читающие файл.
var specOpenFuncs = map[string]bool{
	"os.Open": true, "os.OpenFile": true, "os.ReadFile": true,
	"io/fs.ReadFile": true, "io/ioutil.ReadFile": true,
}

// specParseFuncs — функции пакетов, разбирающие документ формата.
var specParseFuncs = map[string]bool{
	"gopkg.in/yaml.v3.Unmarshal": true, "gopkg.in/yaml.v3.NewDecoder": true,
	"gopkg.in/yaml.v2.Unmarshal": true, "gopkg.in/yaml.v2.UnmarshalStrict": true, "gopkg.in/yaml.v2.NewDecoder": true,
	"sigs.k8s.io/yaml.Unmarshal": true, "sigs.k8s.io/yaml.UnmarshalStrict": true,
	"github.com/goccy/go-yaml.Unmarshal": true, "github.com/goccy/go-yaml.NewDecoder": true,
	"encoding/json.Unmarshal": true, "encoding/json.NewDecoder": true,
}

// specSensitivePkgs — пакеты открытия и разбора: точечный импорт любого
// делает их вызовы невидимыми селекторному разбору.
var specSensitivePkgs = map[string]bool{
	"os": true, "io/fs": true, "io/ioutil": true, "encoding/json": true,
	"gopkg.in/yaml.v3": true, "gopkg.in/yaml.v2": true, "sigs.k8s.io/yaml": true,
	"github.com/goccy/go-yaml": true,
}

// isFormatFileName — базовое имя файла формата шаблона.
func isFormatFileName(v string) bool {
	b := path.Base(v)
	if b == "notification.yaml" || b == "revision.yaml" {
		return true
	}
	return strings.HasPrefix(b, "body.") && strings.HasSuffix(b, ".yaml") && len(b) > len("body..yaml")
}

// specImpl — функция, опознанная реализацией формата.
type specImpl struct {
	owner, pkg, at string
}

// specReport — исход NTF1-A09 по корпусу.
type specReport struct {
	census           string
	files, funcs     int
	mentions         int // функций с упоминанием имени файла формата
	mentionsNoAccess int // из них без открытия и разбора
	impls            []specImpl
	callers          map[string][]string // пакет-вызывающий → функции, обращающиеся к notify/spec
	findings         []string
}

// judgeTemplateFormatSingular — NTF1-A09 по корпусу.
func judgeTemplateFormatSingular(c *pinnedCorpus) specReport {
	r := specReport{census: c.census(), callers: map[string][]string{}}
	consts := packageStringConsts(c)
	for _, f := range c.files {
		r.files++
		names, dot := pinnedImportNames(f.file, specKnownNames)
		for _, d := range dot {
			if specSensitivePkgs[d] || d == notifySpecPkg {
				r.findings = append(r.findings, fmt.Sprintf("%s: точечный импорт %q — вызовы открытия и "+
					"разбора невидимы селекторному разбору; импортируй пакет по имени",
					c.pos(f, f.file.Package), d))
			}
		}
		for _, decl := range f.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			r.funcs++
			owner := funcOwner(f.pkgPath, fd)
			mention, access, callsSpec := false, false, false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr:
					if x.Op != token.ADD {
						return true
					}
					if v, ok := foldString(x, f.pkgPath, names, consts); ok {
						if isFormatFileName(v) {
							mention = true
						}
						return false
					}
					if bodyNameChain(x, f.pkgPath, names, consts) {
						mention = true
					}
					return true
				case *ast.BasicLit, *ast.Ident:
					if v, ok := foldString(x.(ast.Expr), f.pkgPath, names, consts); ok && isFormatFileName(v) {
						mention = true
					}
				case *ast.SelectorExpr:
					if p, _ := selectorTarget(x, names); p == notifySpecPkg {
						callsSpec = true
					}
					if v, ok := foldString(x, f.pkgPath, names, consts); ok && isFormatFileName(v) {
						mention = true
					}
				case *ast.CallExpr:
					if p, n := selectorTarget(x.Fun, names); p != "" {
						if specOpenFuncs[p+"."+n] || specParseFuncs[p+"."+n] {
							access = true
						}
					} else if sel, ok := x.Fun.(*ast.SelectorExpr); ok &&
						(sel.Sel.Name == "Open" || sel.Sel.Name == "ReadFile") {
						access = true
					}
				}
				return true
			})
			if callsSpec && f.pkgPath != notifySpecPkg {
				r.callers[f.pkgPath] = append(r.callers[f.pkgPath], owner)
			}
			if !mention {
				continue
			}
			r.mentions++
			if !access {
				r.mentionsNoAccess++
				continue
			}
			r.impls = append(r.impls, specImpl{owner: owner, pkg: f.pkgPath, at: c.pos(f, fd.Pos())})
		}
	}
	inSpec := 0
	for _, im := range r.impls {
		if im.pkg == notifySpecPkg {
			inSpec++
			continue
		}
		r.findings = append(r.findings, fmt.Sprintf("%s: вторая реализация формата шаблона — %s "+
			"открывает или разбирает файл формата сам, а не зовёт %s (NTF1-A09)", im.at, im.owner, notifySpecPkg))
	}
	if inSpec == 0 {
		r.findings = append(r.findings, fmt.Sprintf("распознаватель не увидел ни одной функции формата в %s — "+
			"образец, ради которого гейт заведён, вне его зрения; молчание о второй реализации ничего не доказывает",
			notifySpecPkg))
	}
	if len(r.callers[notifygenPkgPath]) == 0 {
		r.findings = append(r.findings, fmt.Sprintf("генератор %s не обращается к %s — первое из трёх мест "+
			"применения валидатора не зовёт его", notifygenPkgPath, notifySpecPkg))
	}
	sort.Strings(r.findings)
	return r
}

// bodyNameChain — сцепление «константа с базовым именем body. … ".yaml"»:
// имя файла тела, собранное из локали во время исполнения.
func bodyNameChain(x *ast.BinaryExpr, pkgPath string, names map[string]string, consts map[string]map[string]string) bool {
	var ops []ast.Expr
	var flatten func(e ast.Expr)
	flatten = func(e ast.Expr) {
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			flatten(b.X)
			flatten(b.Y)
			return
		}
		ops = append(ops, e)
	}
	flatten(x)
	if len(ops) < 2 {
		return false
	}
	first, ok := foldString(ops[0], pkgPath, names, consts)
	if !ok || !strings.HasPrefix(path.Base(first+"x"), "body.") {
		return false
	}
	last, ok := foldString(ops[len(ops)-1], pkgPath, names, consts)
	return ok && strings.HasSuffix(last, ".yaml")
}

// String — перепись для журнала пробы.
func (r specReport) String() string {
	pkgs := make([]string, 0, len(r.callers))
	for p := range r.callers {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	var b strings.Builder
	fmt.Fprintf(&b, "NTF1-A09: %s · файлов %d · функций %d · с упоминанием файла формата %d (из них без "+
		"открытия и разбора %d) · реализаций %d · находок %d", r.census, r.files, r.funcs, r.mentions,
		r.mentionsNoAccess, len(r.impls), len(r.findings))
	for _, im := range r.impls {
		fmt.Fprintf(&b, "\n  реализация %s (%s)", im.owner, im.at)
	}
	fmt.Fprintf(&b, "\n  вызывающих %s: пакетов %d", notifySpecPkg, len(pkgs))
	for _, p := range pkgs {
		fmt.Fprintf(&b, "\n    %s: %s", p, strings.Join(r.callers[p], ", "))
	}
	return b.String()
}

// TestNTF1A09_TemplateFormatHasOneImplementation — NTF1-A09 по деревьям
// corelib, kacho, kaname на пинах go.mod: реализация формата одна —
// `notify/spec`, генератор её зовёт.
//
// Гейт сборки notify (полоса N9) и notify на старте (полоса N5) в дереве ещё
// не существуют: их вызов печатается переписью вызывающих, а утверждение «зовёт»
// для них несут пробы своих полос (G17, G03). Утверждать здесь вызов, которого
// нет, значило бы краснеть на отсутствии предмета, а не на дефекте.
func TestNTF1A09_TemplateFormatHasOneImplementation(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	c := pinnedTreesCorpus(t, root, "corelib", "kacho", "kaname")
	if len(c.broken) > 0 {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: файлов, не прошедших разбор, %d: %s", len(c.broken), c.broken[0])
	}
	r := judgeTemplateFormatSingular(c)
	t.Log(r.String())
	if r.files == 0 || r.funcs == 0 {
		t.Fatalf("пустой обход — не вердикт: %s", r.String())
	}
	for _, f := range r.findings {
		t.Errorf("NTF1-A09 · %s", f)
	}
}

// injectedCorpus — корпус дерева с добавленным файлом kacho rel.
func injectedCorpus(t *testing.T, base *pinnedCorpus, rel, src string) *pinnedCorpus {
	t.Helper()
	f, err := parser.ParseFile(base.fset, "kacho:"+rel, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("инъекция не разобралась: %v", err)
	}
	out := &pinnedCorpus{fset: base.fset, versions: base.versions, perTree: map[string]int{}}
	for k, v := range base.perTree {
		out.perTree[k] = v
	}
	out.files = append(append([]pinnedGoFile(nil), base.files...), pinnedGoFile{
		tree: "kacho", rel: rel, pkgPath: kachoModulePath + "/" + path.Dir(rel), file: f,
	})
	out.perTree["kacho"]++
	return out
}

// TestNTF1A09Injection — инъекции в дерево notify настоящим входом корпуса.
// Каждая порча меняет ровно один факт против близнеца того же вида.
func TestNTF1A09Injection(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := pinnedTreesCorpus(t, root, "corelib", "kacho", "kaname")
	control := judgeTemplateFormatSingular(base)
	if len(control.findings) != 0 {
		t.Fatalf("контроль не чист — инъекция не отличима от дерева: %v", control.findings)
	}
	const rel = "services/notify/internal/render/zz_a09_inject.go"
	cases := []struct {
		name, src string
		red       bool
	}{
		{"порча: notification.yaml открыт os.ReadFile и разобран yaml", `package render
import (
	"os"
	"gopkg.in/yaml.v3"
)
func loadOwn(dir string) (map[string]any, error) {
	b, err := os.ReadFile(dir + "/notification.yaml")
	if err != nil { return nil, err }
	var m map[string]any
	return m, yaml.Unmarshal(b, &m)
}`, true},
		{"порча: body.<locale>.yaml сцеплением, чтение fs.ReadFile", `package render
import "io/fs"
const bodyPrefix = "body."
func bodyOf(fsys fs.FS, loc string) ([]byte, error) {
	return fs.ReadFile(fsys, bodyPrefix + loc + ".yaml")
}`, true},
		{"порча: revision.yaml константой, разбор json", `package render
import "encoding/json"
const revFile = "revision.yaml"
func rev(b []byte) (string, error) {
	var v struct{ R int }
	_ = revFile
	return revFile, json.Unmarshal(b, &v)
}`, true},
		{"близнец: имя файла передано в notify/spec, сами не открываем", `package render
import (
	"io/fs"
	"github.com/PRO-Robotech/corelib/notify/spec"
)
func viaSpec(fsys fs.FS) (spec.Set, error) {
	return spec.ReadSetOnly(fsys, "notification.yaml")
}`, false},
		{"близнец: text/template без файла формата", `package render
import "text/template"
func refusal() (*template.Template, error) {
	return template.New("refusal").Parse("{{.}}")
}`, false},
		{"близнец: открытие файла, не являющегося файлом формата", `package render
import "os"
func other() ([]byte, error) { return os.ReadFile("manifest.yaml") }`, false},
	}
	for _, tc := range cases {
		r := judgeTemplateFormatSingular(injectedCorpus(t, base, rel, tc.src))
		got := strings.Join(r.findings, "\n")
		switch {
		case tc.red && len(r.findings) == 0:
			t.Errorf("%s: гейт молчит — вторая реализация не найдена", tc.name)
		case tc.red && !strings.Contains(got, "kacho:"+rel+":"):
			t.Errorf("%s: находка без координаты инъекции: %s", tc.name, got)
		case tc.red && !strings.Contains(got, "вторая реализация формата шаблона"):
			t.Errorf("%s: находка называет не предмет: %s", tc.name, got)
		case !tc.red && len(r.findings) != 0:
			t.Errorf("%s: законный близнец объявлен находкой: %s", tc.name, got)
		}
		t.Logf("%s → находок %d: %s", tc.name, len(r.findings), got)
	}
}
