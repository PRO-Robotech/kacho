// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package protofieldreaders строит индекс чтений полей: какое поле какого
// СООБЩЕНИЯ прочитано в прод-коде.
//
// ЗАЧЕМ ТИП, А НЕ ИМЯ. Предикат «поле публичного запроса обязано иметь читателя»
// (`api-conventions.md`, «Принято-и-проигнорировано — ЗАПРЕЩЕНО») до этого искал
// читателя ПО ИМЕНИ геттера в пределах домена. Имя между сообщениями одного
// домена не уникально (`filter`, `name`, `labels`, `page_size` объявлены
// десятками сообщений), поэтому найденный читатель мог принадлежать ДРУГОМУ
// сообщению — и поле объявлялось читаемым, не будучи им. На дереве такой слабой
// атрибуцией закрывалось 755 полей из 826, то есть девять десятых вердикта
// держалось на совпадении имён.
//
// Здесь получатель вызова резолвится `go/types`: для каждого `x.GetFoo()` и
// `x.Foo` берётся ТИП `x`, и чтение записывается на пару (тип, поле). Совпадение
// имён перестаёт что-либо закрывать by construction: `ListSubnetsRequest.Filter`
// и `ListUsedAddressesRequest.Filter` — разные ключи индекса.
//
// ПРЕДПОСЫЛКА, КОТОРУЮ ИНДЕКС ПРОВЕРЯЕТ САМ. Типы берутся из export-данных
// (`go list -deps -export`), то есть предполагается, что дерево СОБИРАЕТСЯ.
// Пакет, который не разобрался или не протипизировался, — это пакет, чьи чтения
// НЕВИДИМЫ, а значит его поля стали бы ложными находками. Такой пакет попадает в
// `Index.Errors`, и вызывающий обязан считать индекс негодным, а не «пустым».
//
// Тесты (`_test.go`) НЕ читаются намеренно: правило требует читателя в
// ПРОД-коде — тестовый читатель не делает поле применённым.
package protofieldreaders

import (
	"encoding/json"
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
	"sort"
	"strings"
)

// DefaultPatterns — деревья прод-кода, где законно живёт читатель поля запроса:
// сами сервисы, край (grpc-gateway + authz-middleware) и общий repo-root
// `internal/`. Совпадает с деревьями, которые обходит вызывающий предикат.
var DefaultPatterns = []string{"./services/...", "./gateway/...", "./internal/..."}

// Package — один протипизированный пакет прод-кода и его непроверочные файлы
// (пути относительно корня репозитория).
type Package struct {
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

// Index — результат обхода. Ключ `Reads` — "<пакет типа>|<Тип>|<ПолеGo>",
// значение — пакеты прод-кода, в которых это чтение встретилось.
type Index struct {
	Patterns []string            `json:"patterns"`
	Packages []Package           `json:"packages"`
	Reads    map[string][]string `json:"reads"`
	// SkippedNoProdFiles — пакеты, у которых нет ни одного непроверочного файла
	// (сплошь `_test.go` либо всё отсечено build-тегом). Читателя в них нет by
	// construction, но объём осмотренного обязан называть и их: «ноль находок»
	// должно быть отличимо от «ноль прочитанного».
	SkippedNoProdFiles []string `json:"skipped_no_prod_files"`
	// Errors — предпосылка не выполнена: эти пакеты НЕ протипизированы, их чтения
	// невидимы. Непустой список делает индекс негодным целиком.
	Errors []string `json:"errors"`
}

// FileCount — сколько непроверочных файлов прочитано (перепись, а не вердикт).
func (ix *Index) FileCount() int {
	n := 0
	for _, p := range ix.Packages {
		n += len(p.Files)
	}
	return n
}

type listPkg struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
	DepOnly    bool
}

// Build обходит указанные пакетные шаблоны (по умолчанию — DefaultPatterns) и
// возвращает индекс чтений. Запускать из корня репозитория: пути в `Packages`
// считаются относительно текущего каталога.
func Build(patterns ...string) (*Index, error) {
	if len(patterns) == 0 {
		patterns = DefaultPatterns
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	pkgs, err := golist(patterns)
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("go list не вернул ни одного пакета по %v — обход пуст, "+
			"индекс ничего не утверждает", patterns)
	}

	exports := make(map[string]string, len(pkgs))
	for _, p := range pkgs {
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("нет export-данных для %q", path)
		}
		// #nosec G304 -- путь берётся из карты экспортов, заполненной выводом `go list`
		// о СОБСТВЕННОМ дереве репозитория; внешнего ввода на этом пути нет.
		return os.Open(f)
	})

	ix := &Index{Patterns: patterns, Reads: map[string][]string{}}
	readers := map[string]map[string]bool{}

	for _, p := range pkgs {
		if p.DepOnly {
			continue
		}
		if len(p.GoFiles) == 0 {
			ix.SkippedNoProdFiles = append(ix.SkippedNoProdFiles, p.ImportPath)
			continue
		}
		rel := make([]string, 0, len(p.GoFiles))
		syn := make([]*ast.File, 0, len(p.GoFiles))
		failed := false
		for _, gf := range p.GoFiles {
			abs := filepath.Join(p.Dir, gf)
			f, perr := parser.ParseFile(fset, abs, nil, 0)
			if perr != nil {
				ix.Errors = append(ix.Errors, p.ImportPath+": разбор: "+perr.Error())
				failed = true
				break
			}
			syn = append(syn, f)
			if r, rerr := filepath.Rel(cwd, abs); rerr == nil {
				rel = append(rel, r)
			} else {
				rel = append(rel, abs)
			}
		}
		if failed {
			continue
		}
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		var terr error
		cfg := &types.Config{
			Importer: imp,
			Error: func(e error) {
				if terr == nil {
					terr = e
				}
			},
			DisableUnusedImportCheck: true,
		}
		_, _ = cfg.Check(p.ImportPath, fset, syn, info)
		if terr != nil {
			ix.Errors = append(ix.Errors, p.ImportPath+": типизация: "+terr.Error())
			continue
		}
		sort.Strings(rel)
		ix.Packages = append(ix.Packages, Package{Path: p.ImportPath, Files: rel})
		for sel, s := range info.Selections {
			field, ok := readField(sel, s)
			if !ok {
				continue
			}
			tp, tn, ok := namedOf(s.Recv())
			if !ok {
				continue
			}
			key := tp + "|" + tn + "|" + field
			if readers[key] == nil {
				readers[key] = map[string]bool{}
			}
			readers[key][p.ImportPath] = true
		}
	}

	for k, v := range readers {
		l := make([]string, 0, len(v))
		for r := range v {
			l = append(l, r)
		}
		sort.Strings(l)
		ix.Reads[k] = l
	}
	sort.Slice(ix.Packages, func(i, j int) bool { return ix.Packages[i].Path < ix.Packages[j].Path })
	sort.Strings(ix.SkippedNoProdFiles)
	sort.Strings(ix.Errors)
	return ix, nil
}

// readField — какое ПОЛЕ сообщения читает этот селектор.
//
// Две формы, обе настоящие: сгенерированный геттер `x.GetFoo()` и обращение к
// полю структуры `x.Foo`. Третья форма — имя поля строкой (рефлексивные читатели:
// known-set маски, whitelist фильтра, `from_request_field` каталога прав) — сюда
// НЕ попадает by construction: у строки нет получателя, а значит нет и типа, по
// которому её можно приписать сообщению. Её взвешивает вызывающий и обязан
// объявлять отдельной, слабой корзиной.
func readField(sel *ast.SelectorExpr, s *types.Selection) (string, bool) {
	switch s.Kind() {
	case types.FieldVal:
		return sel.Sel.Name, true
	case types.MethodVal:
		n := sel.Sel.Name
		if !strings.HasPrefix(n, "Get") || len(n) == len("Get") {
			return "", false
		}
		return n[len("Get"):], true
	default:
		return "", false
	}
}

// namedOf — именованный тип получателя без указателей и алиасов.
func namedOf(t types.Type) (pkgPath, name string, ok bool) {
	for {
		switch x := t.(type) {
		case *types.Pointer:
			t = x.Elem()
		case *types.Alias:
			t = types.Unalias(x)
		case *types.Named:
			o := x.Obj()
			if o.Pkg() == nil {
				return "", "", false
			}
			return o.Pkg().Path(), o.Name(), true
		default:
			return "", "", false
		}
	}
}

func golist(patterns []string) ([]listPkg, error) {
	args := append([]string{
		"list", "-deps", "-export",
		"-json=ImportPath,Dir,Export,GoFiles,DepOnly",
	}, patterns...)
	// #nosec G204 -- программа фиксирована (`go`), переменная часть — шаблоны пакетов,
	// которые задаёт сам инструмент анализа, а не внешний вызывающий.
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(out)
	var pkgs []listPkg
	var decErr error
	for {
		var p listPkg
		if err := dec.Decode(&p); err != nil {
			if err != io.EOF {
				decErr = err
			}
			break
		}
		pkgs = append(pkgs, p)
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	if decErr != nil {
		return nil, decErr
	}
	return pkgs, nil
}
