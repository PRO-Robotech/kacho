// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// rowclass_gate_test.go — гейт пакета `deliver`: колонку `class` строки ленты
// (`ClaimedNotification.Class` / `GetClass`) в коде пакета читает ровно ОДНО
// место — сравнение клетки 2 (`class_mismatch`, замысел З22, CX1-52 (а)).
// Клетки 3–10, сетка и рендер берут класс из сборки (`tmpl.Class`) через
// [Resolved], в котором колонки нет.
//
// Гейт судит идентичность, а не текст: чтение — это выборка поля или метода
// типа `corelib.notify.ClaimedNotification` по данным проверки типов (все
// законные формы записи: `row.Class`, `row.GetClass()`, значение метода
// `row.GetClass`, выражение метода `(*T).GetClass`). Слово в комментарии и в
// строковом литерале чтением не является. Доступ отражением (`ProtoReflect`)
// законной формой чтения колонки в этом пакете не считается и гейтом не
// видится — сказано прямо, чтобы молчание не читалось шире.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/feed"
)

const notifyAPIPath = "github.com/PRO-Robotech/corelib/api/corelib/notify"

// listed — пакет в выводе `go list -json`.
type listed struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
	CgoFiles   []string
	ImportMap  map[string]string
	Error      *struct{ Err string }
}

// goList — `go list -export -deps -json` по шаблонам из каталога dir.
func goList(t *testing.T, dir string, patterns ...string) []listed {
	t.Helper()
	args := append([]string{"list", "-export", "-deps", "-json"}, patterns...)
	cmd := exec.Command("go", args...) // #nosec G204 -- аргументы — константы пробы
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: go list %v: %v\n%s", patterns, err, errb.String())
	}
	var pkgs []listed
	dec := json.NewDecoder(&out)
	for {
		var p listed
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: разбор вывода go list: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// checker — проверка типов исходников на данных экспорта зависимостей.
type checker struct {
	fset    *token.FileSet
	exports map[string]string
	imports map[string]string
}

func newChecker(pkgs []listed) *checker {
	c := &checker{fset: token.NewFileSet(), exports: map[string]string{}, imports: map[string]string{}}
	for _, p := range pkgs {
		if p.Export != "" {
			c.exports[p.ImportPath] = p.Export
		}
	}
	return c
}

func (c *checker) check(t *testing.T, path string, files map[string]string, importMap map[string]string) *types.Info {
	t.Helper()
	var parsed []*ast.File
	for _, name := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(c.fset, name, files[name], parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: разбор %s: %v", name, err)
		}
		parsed = append(parsed, f)
	}
	gc := importer.ForCompiler(c.fset, "gc", func(p string) (io.ReadCloser, error) {
		exp, ok := c.exports[p]
		if !ok {
			return nil, fmt.Errorf("данных экспорта %s нет", p)
		}
		return os.Open(exp) // #nosec G304 -- путь из вывода go list
	})
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: importerFunc(func(p string) (*types.Package, error) {
		if mapped, ok := importMap[p]; ok {
			p = mapped
		}
		return gc.Import(p)
	})}
	if _, err := conf.Check(path, c.fset, parsed, info); err != nil {
		t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: типы %s: %v", path, err)
	}
	return info
}

type importerFunc func(string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// classReads — координаты чтений колонки class строки ленты: выборки поля
// `Class` и метода `GetClass` типа ClaimedNotification.
func classReads(fset *token.FileSet, info *types.Info) []string {
	var out []string
	for sel, s := range info.Selections {
		obj := s.Obj()
		if obj.Pkg() == nil || obj.Pkg().Path() != notifyAPIPath {
			continue
		}
		if obj.Name() != "Class" && obj.Name() != "GetClass" {
			continue
		}
		recv := s.Recv()
		if p, ok := recv.(*types.Pointer); ok {
			recv = p.Elem()
		}
		named, ok := recv.(*types.Named)
		if !ok || named.Obj().Name() != "ClaimedNotification" {
			continue
		}
		out = append(out, fset.Position(sel.Sel.Pos()).String())
	}
	slices.Sort(out)
	return out
}

// classReadFinding — находка гейта: nil, если чтение ровно одно.
func classReadFinding(files int, reads []string) error {
	switch {
	case files == 0:
		return fmt.Errorf("пакет deliver: непробных файлов 0 — читать нечего, клетки 2 нет")
	case len(reads) == 0:
		return fmt.Errorf("колонку class строки не читает ни одно место: сравнения клетки 2 (class_mismatch) нет")
	case len(reads) > 1:
		return fmt.Errorf("колонку class строки читают %d мест, ждали 1 (сравнение клетки 2): %s", len(reads), strings.Join(reads, "; "))
	}
	return nil
}

// TestRowClassIsReadByOneLine — гейт на дереве: пакет `deliver`.
func TestRowClassIsReadByOneLine(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	pkgs := goList(t, wd, ".")
	var self *listed
	for i := range pkgs {
		if pkgs[i].Dir == wd {
			self = &pkgs[i]
		}
	}
	if self == nil || self.Error != nil {
		t.Fatalf("пакет deliver не найден выводом go list в %s", wd)
	}
	if len(self.CgoFiles) > 0 {
		t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: пакет несёт cgo-файлы")
	}
	files := map[string]string{}
	for _, name := range self.GoFiles {
		b, err := os.ReadFile(filepath.Join(self.Dir, name)) // #nosec G304 -- путь из вывода go list
		if err != nil {
			t.Fatalf("гейт НЕ ИСПОЛНЯЛСЯ: %v", err)
		}
		files[name] = string(b)
	}
	c := newChecker(pkgs)
	var reads []string
	if len(files) > 0 {
		reads = classReads(c.fset, c.check(t, self.ImportPath, files, self.ImportMap))
	}
	t.Logf("перепись: непробных файлов %d, чтений колонки class %d: %v", len(files), len(reads), reads)
	if err := classReadFinding(len(files), reads); err != nil {
		t.Fatal(err)
	}
}

// TestRowClassGateInjection — инъекция в обе стороны на синтетике: дефект
// краснеет и называет координаты; законный близнец той же формы молчит;
// пустой обход зелёным не бывает. Не зависит от дерева пакета.
func TestRowClassGateInjection(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	c := newChecker(goList(t, wd, notifyAPIPath))
	const head = "package syn\n\nimport notifyv1 \"" + notifyAPIPath + "\"\n\n"
	cases := []struct {
		name  string
		body  string
		reads int
		red   bool
	}{
		{"одно чтение полем — молчит", "func cell2(r *notifyv1.ClaimedNotification, c notifyv1.NotificationClass) bool { return r.Class != c }\n", 1, false},
		{"одно чтение методом — молчит", "func cell2(r *notifyv1.ClaimedNotification, c notifyv1.NotificationClass) bool { return r.GetClass() != c }\n", 1, false},
		{"слово в комментарии и строке — не чтение", "// r.Class и r.GetClass() — слова\nvar s = \"r.GetClass()\"\nfunc cell2(r *notifyv1.ClaimedNotification, c notifyv1.NotificationClass) bool { return r.Class != c }\n", 1, false},
		{"второе чтение полем — красный", "func cell2(r *notifyv1.ClaimedNotification, c notifyv1.NotificationClass) bool { return r.Class != c }\nfunc net(r *notifyv1.ClaimedNotification) notifyv1.NotificationClass { return r.Class }\n", 2, true},
		{"второе чтение значением метода — красный", "func cell2(r *notifyv1.ClaimedNotification, c notifyv1.NotificationClass) bool { return r.Class != c }\nfunc net(r *notifyv1.ClaimedNotification) func() notifyv1.NotificationClass { return r.GetClass }\n", 2, true},
		{"второе чтение выражением метода — красный", "func cell2(r *notifyv1.ClaimedNotification, c notifyv1.NotificationClass) bool { return r.Class != c }\nvar get = (*notifyv1.ClaimedNotification).GetClass\n", 2, true},
		{"чтения нет — красный", "func cell2(r *notifyv1.ClaimedNotification) string { return r.GetTemplate() }\n", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := c.check(t, "example.invalid/syn", map[string]string{"syn.go": head + tc.body}, nil)
			reads := classReads(c.fset, info)
			if len(reads) != tc.reads {
				t.Fatalf("чтений %d (%v), ждали %d", len(reads), reads, tc.reads)
			}
			err := classReadFinding(1, reads)
			if (err != nil) != tc.red {
				t.Fatalf("находка %v, ждали красный=%v", err, tc.red)
			}
			if tc.red && tc.reads > 1 && !strings.Contains(err.Error(), "syn.go:") {
				t.Fatalf("находка не называет координаты: %v", err)
			}
		})
	}
	if err := classReadFinding(0, nil); err == nil {
		t.Fatal("пустой обход дал зелёный")
	}
}

// TestResolvedCarriesNoClassColumn — в описании строки после клеток 1–2 нет
// колонки class: ни поля класса ленты, ни сырой строки, его несущей.
func TestResolvedCarriesNoClassColumn(t *testing.T) {
	rt := reflect.TypeOf(Resolved{})
	if rt.NumField() == 0 {
		t.Fatal("Resolved без полей: шаблон, атрибуты и адресат не описаны")
	}
	banned := map[reflect.Type]string{
		reflect.TypeOf(notifyv1.NotificationClass(0)):        "класс ленты",
		reflect.TypeOf(feed.Class("")):                       "класс ленты (feed.Class)",
		reflect.TypeOf((*notifyv1.ClaimedNotification)(nil)): "сырая строка ленты",
	}
	for i := range rt.NumField() {
		f := rt.Field(i)
		if what, bad := banned[f.Type]; bad {
			t.Fatalf("поле Resolved.%s — %s (%v): колонка class проходит дальше клетки 2", f.Name, what, f.Type)
		}
	}
	t.Logf("перепись: полей Resolved %d", rt.NumField())
}
