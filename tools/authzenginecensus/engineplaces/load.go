// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package engineplaces

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

// ПОЧЕМУ ЭТОТ МЕХАНИЗМ ОБХОДА, А НЕ `go/packages` — решение названо, потому что
// оба ответа защитимы и отвечают на РАЗНЫЕ вопросы.
//
//	`golang.org/x/tools/go/packages` отвечает на вопрос «загрузи мне пакеты со
//	всем, что попрошу, ДАЖЕ если дерево не собирается»: ошибки складываются в
//	поле результата, обход продолжается, и вызывающий волен их не прочитать.
//	Это удобно для правщика кода и опасно для переписи: необойдённое дерево
//	выглядит чистой переписью.
//
//	`go list -deps -export` + `go/importer` + `types.Config.Check` отвечает на
//	вопрос «протипизируй по УЖЕ СОБРАННЫМ export-данным»: пакет, который не
//	собрался, export-данных не имеет и типизацию не проходит — он попадает в
//	`Census.Errors`, и перепись объявляет себя негодной ЦЕЛИКОМ.
//
// Выбран второй, по трём причинам, каждая проверяема:
//
//  1. он уже живёт в дереве (`tools/unreadfieldaudit/protofieldreaders`) — один
//     инструмент, один способ отказать, одна привычка читать его вывод;
//  2. `golang.org/x/tools` стоит в `go.mod` КОСВЕННОЙ зависимостью; `go/packages`
//     сделал бы её прямой — правка `go.mod` ради обхода, который дерево уже умеет;
//  3. предпосылка «дерево собирается» у него не подразумевается, а ПРОВЕРЯЕТСЯ:
//     это ровно то условие, без которого «мест 0» неотличимо от «пакет не
//     загрузился».
//
// Цена решения названа честно: перепись требует собранных export-данных, то есть
// первый прогон на холодном кэше стоит полной сборки дерева. Это не побочный
// эффект, а предмет: перепись, которую можно снять с несобирающегося дерева,
// утверждала бы о нём то, чего не читала.
//
// # ОДИН УНИВЕРСУМ ТИПОВ — иначе перепись занижает МОЛЧА
//
// Пакеты СВОЕГО модуля типизируются из исходников в ОДИН проход, в порядке
// зависимостей, и каждый следующий получает уже протипизированные предыдущие.
// Иначе один и тот же импортированный тип существует в двух экземплярах —
// проверенном из исходника и поднятом из export-данных, — `types.Identical` на
// них ложно, и `types.Implements` возвращает ЛОЖЬ для порта, в сигнатурах
// которого стоит тип дома движка.
//
// Это не теория: первая редакция прибора так и была написана и не увидела
// `authzcascade.Relations` — порт, чей собственный godoc объявляет
// «Satisfied by *clients.OpenFGAHTTPClient», — потеряв ДЕСЯТЬ настоящих мест
// решения. Перепись при этом выглядела ЧИЩЕ, то есть ошибалась в сторону,
// которую не видно. `go list -deps` печатает пакеты в порядке пост-обхода
// («пакет назван только после всех своих зависимостей»), и этот порядок здесь
// используется как топологический.

// listPkg — то, что нужно от `go list` и ничего сверх.
type listPkg struct {
	ImportPath string
	Dir        string
	Export     string
	GoFiles    []string
	DepOnly    bool
	Module     *listMod
	Error      *listErr
}

type listMod struct {
	Path string
	Main bool
}

type listErr struct {
	Err string
}

func golist(root string, patterns []string) ([]listPkg, error) {
	args := []string{"list", "-e", "-deps", "-export",
		"-json=ImportPath,Dir,Export,GoFiles,DepOnly,Module,Error"}
	args = append(args, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list в %s: %w: %s", root, err, stderr.String())
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []listPkg
	for {
		var p listPkg
		if derr := dec.Decode(&p); derr == io.EOF {
			break
		} else if derr != nil {
			return nil, fmt.Errorf("разбор вывода go list: %w", derr)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// loadedPkg — один протипизированный пакет своего модуля.
type loadedPkg struct {
	Path  string
	Dir   string
	Files []string // пути относительно корня репозитория
	Syn   []*ast.File
	Info  *types.Info
	Pkg   *types.Package
	// Requested — пакет назван шаблоном (а не подтянут как зависимость):
	// места собираются только с них, чтобы единица счёта совпадала с тем, о чём
	// перепись объявила.
	Requested bool
}

// loadResult — итог обхода: что протипизировано и что осмотреть не удалось.
type loadResult struct {
	Own       []*loadedPkg // пакеты своего модуля, названные шаблоном
	All       []*loadedPkg // все протипизированные из исходников (один универсум)
	Requested int          // сколько пакетов своего модуля назвал шаблон
	Skipped   []string     // без единого непроверочного файла
	Errors    []string     // предпосылка не выполнена
	FileSet   *token.FileSet
	root      string
}

// sourceFirstImporter — импортёр, который для пакетов СВОЕГО модуля отдаёт уже
// протипизированный из исходников экземпляр, а для внешних — export-данные.
// Именно он держит один универсум типов.
type sourceFirstImporter struct {
	fromSource map[string]*types.Package
	fallback   types.Importer
}

func (si *sourceFirstImporter) Import(path string) (*types.Package, error) {
	if p, ok := si.fromSource[path]; ok {
		return p, nil
	}
	return si.fallback.Import(path)
}

// load типизирует своё дерево по export-данным зависимостей.
//
// Проверочные файлы (`_test.go`) НЕ читаются НАМЕРЕННО: предмет переписи —
// прод-места. Это объявленная граница, а не пропуск, и она печатается.
func load(root string, patterns []string) (*loadResult, error) {
	pkgs, err := golist(root, patterns)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("go list не вернул ни одного пакета по %v — обход пуст, "+
			"перепись не утверждает ничего", patterns)
	}

	exports := make(map[string]string, len(pkgs))
	for i := range pkgs {
		if pkgs[i].Export != "" {
			exports[pkgs[i].ImportPath] = pkgs[i].Export
		}
	}
	fset := token.NewFileSet()
	fallback := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("нет export-данных для %q", path)
		}
		// #nosec G304 -- путь берётся из карты экспортов, заполненной выводом `go list`
		// о СОБСТВЕННОМ дереве репозитория; внешнего ввода на этом пути нет.
		return os.Open(f)
	})
	imp := &sourceFirstImporter{fromSource: map[string]*types.Package{}, fallback: fallback}

	res := &loadResult{FileSet: fset}
	for i := range pkgs {
		p := &pkgs[i]
		mine := p.Module != nil && p.Module.Main
		if !mine {
			continue
		}
		if !p.DepOnly {
			res.Requested++
		}
		if p.Error != nil && p.Error.Err != "" {
			res.Errors = append(res.Errors, p.ImportPath+": go list: "+p.Error.Err)
			continue
		}
		if len(p.GoFiles) == 0 {
			if !p.DepOnly {
				res.Skipped = append(res.Skipped, p.ImportPath)
			}
			continue
		}
		lp := &loadedPkg{Path: p.ImportPath, Dir: p.Dir, Requested: !p.DepOnly}
		failed := false
		for _, gf := range p.GoFiles {
			abs := filepath.Join(p.Dir, gf)
			f, perr := parser.ParseFile(fset, abs, nil, parser.ParseComments)
			if perr != nil {
				res.Errors = append(res.Errors, p.ImportPath+": разбор: "+perr.Error())
				failed = true
				break
			}
			lp.Syn = append(lp.Syn, f)
			if rel, rerr := filepath.Rel(root, abs); rerr == nil {
				lp.Files = append(lp.Files, filepath.ToSlash(rel))
			} else {
				lp.Files = append(lp.Files, filepath.ToSlash(abs))
			}
		}
		if failed {
			continue
		}
		lp.Info = &types.Info{
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Types:      map[ast.Expr]types.TypeAndValue{},
		}
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
		tp, _ := cfg.Check(p.ImportPath, fset, lp.Syn, lp.Info)
		if terr != nil {
			res.Errors = append(res.Errors, p.ImportPath+": типизация: "+terr.Error())
			continue
		}
		lp.Pkg = tp
		imp.fromSource[p.ImportPath] = tp
		sort.Strings(lp.Files)
		res.All = append(res.All, lp)
		if lp.Requested {
			res.Own = append(res.Own, lp)
		}
	}
	sort.Slice(res.Own, func(i, j int) bool { return res.Own[i].Path < res.Own[j].Path })
	sort.Slice(res.All, func(i, j int) bool { return res.All[i].Path < res.All[j].Path })
	sort.Strings(res.Skipped)
	sort.Strings(res.Errors)
	return res, nil
}
