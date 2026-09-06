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
// ОБХОД ИДЁТ ПО МОДУЛЯМ. Дерево несёт больше одного Go-модуля, а `go list` в
// каталог со СВОИМ `go.mod` не спускается by construction — значит читатель,
// записанный в вынесенной службе, был бы не «ненайденным», а НЕВИДИМЫМ. Перечень
// модулей выводится из дерева, перепись называет каждый порознь (`Index.Modules`),
// и пустой обход запрошенного дерева — отказ, а не пустой индекс.
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
	"regexp"
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

// Module — один Go-МОДУЛЬ, обойдённый индексом, и его перепись.
//
// ЗАЧЕМ ОТДЕЛЬНАЯ ЕДИНИЦА. Дерево несёт больше одного модуля, и `go list` в
// модуль со СВОИМ `go.mod` не спускается by construction. Пока перепись знала
// только «пакетов N, файлов M», обход одного дерева был неотличим от обхода
// обоих: суммарное число росло от чего угодно. Модуль называется порознь именно
// затем, чтобы «ноль прочитанного» у одного из деревьев было видно.
type Module struct {
	// Path — путь модуля из его `go.mod`.
	Path string `json:"path"`
	// Dir — каталог модуля относительно корня обхода ("." для корневого).
	Dir string `json:"dir"`
	// Patterns — пакетные шаблоны, с которыми модуль обойдён (уже относительно
	// его собственного корня).
	Patterns []string `json:"patterns"`
	Packages int      `json:"packages"`
	Files    int      `json:"files"`
}

// Index — результат обхода. Ключ `Reads` — "<пакет типа>|<Тип>|<ПолеGo>",
// значение — пакеты прод-кода, в которых это чтение встретилось.
type Index struct {
	Patterns []string            `json:"patterns"`
	Modules  []Module            `json:"modules"`
	Packages []Package           `json:"packages"`
	Reads    map[string][]string `json:"reads"`
	// Discriminated — ключ "<пакет типа>|<Тип>", значение — пакеты прод-кода, где
	// по этому типу РАЗЛИЧАЮТ ветку (`switch x.(type)` / `x.(*T)`).
	//
	// Отдельная карта, а не запись в `Reads`, потому что это другой факт: читается
	// не значение поля, а то, КАКАЯ ветка `oneof` выбрана. Для члена с пустой
	// полезной нагрузкой (`message X {}`) это единственно возможное чтение —
	// обращаться в нём не к чему, и без этой карты такой член объявлялся бы
	// «принятым и выброшенным» при живом и единственно возможном читателе.
	//
	// КОНСТРУИРОВАНИЕ СЮДА НЕ ПОПАДАЕТ. Составной литерал `&pb.M_Foo{…}` на пути
	// ответа — запись, а не чтение запроса; засчитать её значило бы закрывать поле
	// запроса собственным выводом сервиса.
	Discriminated map[string][]string `json:"discriminated"`
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
//
// ОБХОД ИДЁТ ПО МОДУЛЯМ, А НЕ ОДНИМ `go list` В КОРНЕ, и это не оптимизация.
// `go list` в каталог, у которого есть СВОЙ `go.mod`, не спускается by
// construction: он принадлежит другому модулю. Пока обход был один, всякое
// чтение поля, записанное в вынесенном сервисе, оказывалось не «непрочитанным»
// и не «находкой», а НЕВИДИМЫМ — предикат молчал о нём в обе стороны. Замер на
// 04feea5aa4: `go list ./services/...` из корня возвращал 0 пакетов под
// services/iam при 136 пакетах в самом модуле, и все 268 полей публичного
// запроса домена уезжали в полосу «RPC-НЕ-РЕАЛИЗОВАН» — корзину, которая ничего
// не утверждает.
//
// Перечень модулей ВЫВОДИТСЯ из дерева (`go.mod` под обойденными каталогами), а
// не выписывается: рукописный список унаследовал бы ту же немоту при следующем
// вынесенном сервисе, и заметить это было бы снова некому.
//
// У КАЖДОГО МОДУЛЯ СВОИ EXPORT-ДАННЫЕ. Вложенный модуль резолвит платформу
// ОПУБЛИКОВАННОЙ версией из кэша модулей, корневой — своим деревом; один и тот
// же путь пакета указывает у них на разные файлы экспорта. Общая карта экспортов
// сделала бы типизацию зависящей от порядка обхода, поэтому импортёр строится на
// каждый модуль заново.
func Build(patterns ...string) (*Index, error) {
	if len(patterns) == 0 {
		patterns = DefaultPatterns
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	walks, err := planWalks(cwd, patterns)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	ix := &Index{
		Patterns:      patterns,
		Reads:         map[string][]string{},
		Discriminated: map[string][]string{},
	}
	readers := map[string]map[string]bool{}
	discs := map[string]map[string]bool{}
	// Сколько пакетов принёс КАЖДЫЙ шаблон вызывающего. Единица счёта — шаблон, а
	// не модуль: вызывающий просил дерево, и пустым обходом надо звать пустое
	// ДЕРЕВО, а не пустой модуль внутри него (законный случай — сервис, целиком
	// вынесенный в свой модуль: корневой обход того же шаблона даёт ноль, а дерево
	// прочитано).
	byPattern := map[string]int{}
	// Пакет обрабатывается ОДИН раз, даже если его принесли два перекрывающихся
	// шаблона: иначе его чтения удвоились бы в переписи, а `Packages` понесли бы
	// дубликаты.
	seenPkg := map[string]bool{}
	// Перепись ведётся по МОДУЛЮ (каталогу), а не по обходу: три шаблона одного
	// корневого модуля — это один модуль, обойдённый трижды, и называть его тремя
	// значило бы завышать число деревьев.
	byModuleDir := map[string]int{}

	for _, w := range walks {
		pkgs, lerr := golist(filepath.Join(cwd, w.dir), w.patterns)
		if lerr != nil {
			return nil, fmt.Errorf("go list в %s: %w", w.dir, lerr)
		}
		exports := make(map[string]string, len(pkgs))
		for _, p := range pkgs {
			if p.Export != "" {
				exports[p.ImportPath] = p.Export
			}
		}
		imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			f, ok := exports[path]
			if !ok {
				return nil, fmt.Errorf("нет export-данных для %q", path)
			}
			// #nosec G304 -- путь берётся из карты экспортов, заполненной выводом `go list`
			// о СОБСТВЕННОМ дереве репозитория; внешнего ввода на этом пути нет.
			return os.Open(f)
		})

		idx, ok := byModuleDir[w.dir]
		if !ok {
			ix.Modules = append(ix.Modules, Module{Path: w.path, Dir: w.dir})
			idx = len(ix.Modules) - 1
			byModuleDir[w.dir] = idx
		}
		mod := &ix.Modules[idx]
		mod.Patterns = append(mod.Patterns, w.patterns...)
		for _, p := range pkgs {
			if p.DepOnly {
				continue
			}
			if len(p.GoFiles) == 0 {
				ix.SkippedNoProdFiles = append(ix.SkippedNoProdFiles, p.ImportPath)
				continue
			}
			for _, orig := range w.origin {
				byPattern[orig]++
			}
			if seenPkg[p.ImportPath] {
				continue
			}
			seenPkg[p.ImportPath] = true
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
			info := &types.Info{
				Selections: map[*ast.SelectorExpr]*types.Selection{},
				// Types нужен, чтобы резолвить ТИП из ветки переключателя и из
				// тип-ассершена: там нет селектора, значит и Selections пуст.
				Types: map[ast.Expr]types.TypeAndValue{},
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
			_, _ = cfg.Check(p.ImportPath, fset, syn, info)
			if terr != nil {
				ix.Errors = append(ix.Errors, p.ImportPath+": типизация: "+terr.Error())
				continue
			}
			sort.Strings(rel)
			ix.Packages = append(ix.Packages, Package{Path: p.ImportPath, Files: rel})
			mod.Packages++
			mod.Files += len(rel)
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
			for _, f := range syn {
				for _, expr := range discriminatedTypes(f) {
					tp, tn, ok := namedOf(info.Types[expr].Type)
					if !ok {
						continue
					}
					key := tp + "|" + tn
					if discs[key] == nil {
						discs[key] = map[string]bool{}
					}
					discs[key][p.ImportPath] = true
				}
			}
		}
	}

	// ПУСТОЙ ОБХОД — ОТКАЗ, и единица здесь ШАБЛОН, а не суммарное число: обход,
	// не принёсший ни одного пакета по запрошенному дереву, ничего о нём не
	// утверждает, а суммарный ноль скрывал бы это за пакетами соседнего дерева.
	for _, pat := range patterns {
		if byPattern[pat] == 0 {
			return nil, fmt.Errorf("по шаблону %q не прочитано ни одного пакета "+
				"(обойдено модулей: %d) — обход пуст, индекс о нём ничего не утверждает",
				pat, len(ix.Modules))
		}
	}

	for k, v := range readers {
		ix.Reads[k] = sortedKeys(v)
	}
	for k, v := range discs {
		ix.Discriminated[k] = sortedKeys(v)
	}
	sort.Slice(ix.Packages, func(i, j int) bool { return ix.Packages[i].Path < ix.Packages[j].Path })
	sort.Slice(ix.Modules, func(i, j int) bool { return ix.Modules[i].Dir < ix.Modules[j].Dir })
	sort.Strings(ix.SkippedNoProdFiles)
	sort.Strings(ix.Errors)
	return ix, nil
}

// moduleWalk — один Go-модуль и шаблоны, с которыми он обходится (уже
// относительно его собственного корня).
type moduleWalk struct {
	dir      string // каталог модуля относительно корня обхода ("." — корневой)
	path     string // путь модуля из его go.mod
	patterns []string
	origin   []string // шаблоны вызывающего, которые этот обход покрывает
}

// planWalks — какие модули обойти и с какими шаблонами.
//
// Для каждого шаблона вызывающего берётся его буквальный каталог, и:
//   - шаблон отдаётся ВЛАДЕЛЬЦУ этого каталога — ближайшему модулю на пути вверх
//     (для `./services/iam/...` владелец — сам вынесенный модуль, и `go list` в
//     корне на такой шаблон отвечает отказом «directory prefix … does not contain
//     main module»);
//   - каждый модуль СТРОГО ПОД этим каталогом обходится своим `./...`.
//
// Шаблон, не начинающийся с "./", вложенных модулей не ищет и уходит корневому
// обходу как есть: это форма не про каталог дерева (`all`, путь пакета), и
// выводить из неё каталог значило бы угадывать.
func planWalks(cwd string, patterns []string) ([]moduleWalk, error) {
	order := []string{}
	byKey := map[string]*moduleWalk{}
	add := func(dir, path, pattern, origin string) {
		key := dir + "\x00" + pattern
		w, ok := byKey[key]
		if !ok {
			w = &moduleWalk{dir: dir, path: path}
			byKey[key] = w
			order = append(order, key)
		}
		if len(w.patterns) == 0 {
			w.patterns = []string{pattern}
		}
		for _, o := range w.origin {
			if o == origin {
				return
			}
		}
		w.origin = append(w.origin, origin)
	}

	rootPath, err := modulePath(filepath.Join(cwd, "go.mod"))
	if err != nil {
		return nil, err
	}
	for _, pat := range patterns {
		if !strings.HasPrefix(pat, "./") && pat != "." {
			add(".", rootPath, pat, pat)
			continue
		}
		d := literalDir(pat)
		owner, ownerPath, oerr := ownerModule(cwd, d, rootPath)
		if oerr != nil {
			return nil, oerr
		}
		rel, rerr := filepath.Rel(owner, d)
		if rerr != nil {
			return nil, rerr
		}
		add(owner, ownerPath, patternIn(rel, pat), pat)

		nested, nerr := nestedModulesUnder(cwd, d, owner)
		if nerr != nil {
			return nil, nerr
		}
		for _, m := range nested {
			add(m.dir, m.path, "./...", pat)
		}
	}
	out := make([]moduleWalk, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out, nil
}

// literalDir — буквальный каталог шаблона: часть до первого `...`.
func literalDir(pattern string) string {
	p := strings.TrimPrefix(pattern, "./")
	if i := strings.Index(p, "..."); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "."
	}
	return p
}

// patternIn — тот же шаблон, но относительно корня модуля-владельца.
func patternIn(rel, orig string) string {
	rel = filepath.ToSlash(rel)
	dots := strings.HasSuffix(orig, "...")
	switch {
	case rel == "." && dots:
		return "./..."
	case rel == ".":
		return "."
	case dots:
		return "./" + rel + "/..."
	default:
		return "./" + rel
	}
}

// ownerModule — ближайший модуль на пути вверх от dir, не выше корня обхода.
func ownerModule(cwd, dir, rootPath string) (string, string, error) {
	for cur := filepath.Clean(dir); ; cur = filepath.Dir(cur) {
		if cur == "." || cur == string(filepath.Separator) {
			return ".", rootPath, nil
		}
		p, err := modulePath(filepath.Join(cwd, cur, "go.mod"))
		if err != nil {
			return "", "", err
		}
		if p != "" {
			return cur, p, nil
		}
	}
}

type nestedModule struct{ dir, path string }

// nestedModulesUnder — модули, чей `go.mod` лежит СТРОГО под dir (и не является
// модулем-владельцем самого dir).
func nestedModulesUnder(cwd, dir, owner string) ([]nestedModule, error) {
	var out []nestedModule
	base := filepath.Join(cwd, dir)
	err := filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() != "go.mod" {
			return nil
		}
		rel, rerr := filepath.Rel(cwd, filepath.Dir(p))
		if rerr != nil {
			return rerr
		}
		if rel == "." || rel == owner {
			return nil
		}
		mp, merr := modulePath(p)
		if merr != nil {
			return merr
		}
		if mp == "" {
			return nil
		}
		out = append(out, nestedModule{dir: rel, path: mp})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out, nil
}

var reModuleDecl = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// modulePath — путь модуля из go.mod; пустая строка, если файла нет.
func modulePath(goMod string) (string, error) {
	// #nosec G304 -- путь собирается из корня обхода и каталогов СОБСТВЕННОГО дерева.
	b, err := os.ReadFile(goMod)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	m := reModuleDecl.FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf("%s не объявляет module — обход не может назвать модуль", goMod)
	}
	return string(m[1]), nil
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

func sortedKeys(m map[string]bool) []string {
	l := make([]string, 0, len(m))
	for k := range m {
		l = append(l, k)
	}
	sort.Strings(l)
	return l
}

// discriminatedTypes — выражения-типы, по которым код РАЗЛИЧАЕТ ветку значения:
// ветки `switch x.(type)` и тип-ассершен `x.(*T)`.
//
// Составной литерал `&pb.M_Foo{…}` сюда НЕ попадает by construction: он не
// выражение-тип ни в одной из этих двух форм. Различие принципиальное —
// конструирование ветки на пути ответа не является чтением поля запроса.
//
// `case nil:` пропускается: у него нет именованного типа, и «ветка не выбрана» —
// не член oneof.
func discriminatedTypes(f *ast.File) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TypeAssertExpr:
			if x.Type != nil { // `x.(type)` вне переключателя невозможен
				out = append(out, x.Type)
			}
		case *ast.TypeSwitchStmt:
			for _, stmt := range x.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				out = append(out, cc.List...)
			}
		}
		return true
	})
	return out
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

func golist(dir string, patterns []string) ([]listPkg, error) {
	args := append([]string{
		"list", "-deps", "-export",
		"-json=ImportPath,Dir,Export,GoFiles,DepOnly",
	}, patterns...)
	// #nosec G204 -- программа фиксирована (`go`), переменная часть — шаблоны пакетов,
	// которые задаёт сам инструмент анализа, а не внешний вызывающий.
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
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
