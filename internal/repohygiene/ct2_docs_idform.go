// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_docs_idform.go — анализатор «форма идентификатора в клиентской странице —
// та, которую ЧЕКАНИТ код».
//
// # Предмет
//
// Платформа несёт ДВЕ формы идентификатора, и выбирает между ними не читатель,
// а генератор:
//
//   - слитная  — `ids.NewID(prefix)` → `<prefix><17 крокфорд>`, prefix ровно 3;
//   - дефисная — `ids.NewHyphenID(prefix)` → `<prefix>-<17 крокфорд>`, prefix 2..3.
//
// Страница, показывающая идентификатор в форме, которой владелец не чеканит,
// даёт вызывающему строку, которой в природе нет. Скопировав её, он не получает
// внятного отказа о написании: разбор `validate.ResourceID` судит ТОЛЬКО
// префикс и тело внутри не смотрит, поэтому чужая форма проходит проверку и
// умирает дальше — промахом по несуществующему ресурсу либо отказом
// предусловия у соседа. То есть цена ошибки — круг запроса и отказ, который
// говорит не о том.
//
// Замер на день заведения (kacho#1641): в клиентских страницах storage — 18
// идентификаторов дефисной формы при слитной чеканке (`vol`/`snp`/`img`/`sop`),
// в страницах compute — 5 (`prj`, `img`). Проза при этом была ВЕРНА:
// `services/storage/docs/content/api/overview.mdx` объявляет «`vol` + 17
// символов crockford-base32». Два места об одном предмете, и неверным было то,
// которое вызывающий копирует.
//
// # Что судит анализатор
//
// Словарь ВЫВОДИТСЯ из дерева, а не выписывается:
//
//  1. по всем не-тестовым `.go` собираются вызовы `ids.NewID` / `ids.NewHyphenID`
//     И ТРАНЗИТИВНО — вызовы всякой функции, передающей свой параметр в уже
//     известную чеканящую (поиск до неподвижной точки). Без этого шага
//     идентификатор ОПЕРАЦИИ остался бы вне суда: он чеканится через посредника,
//     куда префикс приезжает параметром;
//  2. аргумент резолвится до строкового литерала — сам литерал, константа того
//     же пакета (`keyPrefix`), либо константа чужого (`domain.PrefixVolume`,
//     `ids.PrefixMachineTypeHyphen`) через список импортов ФАЙЛА. Резолв идёт по
//     разбору, а не по имени: `PrefixImage` объявлен ДВАЖДЫ — `fd8` в `pkg/ids`
//     и `img` в домене storage, и выбор между ними решает импорт вызывающего;
//  3. получается отображение «префикс → множество чеканимых форм».
//
// Затем в клиентских страницах судимых доменов ищутся токены обеих форм, и
// написанная форма сверяется с чеканимой.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. ПРЕФИКС, КОТОРОГО НЕТ В СЛОВАРЕ, не судится. Аргумент, не сводимый к
//     литералу ни прямо, ни транзитивно (значение из базы, поле структуры),
//     оставляет свой префикс неизвестным, и токен с ним пропускается — молчание
//     здесь означает «не знаю», а не «верно». Перепись печатает и позиции
//     неразрешённых аргументов, и перечень неизвестных префиксов, поэтому
//     слепая зона видна числом, а не подразумевается.
//
//  2. ТЕЛО идентификатора не судится: анализатор смотрит на форму (есть ли
//     разделитель), а не на энтропию. Пример с телом из семнадцати законных
//     знаков остаётся примером.
//
//  3. СУФФИКС ПОСЛЕ идентификатора не судится — и это НЕ упущение, а отказ от
//     негодного предиката. Двоеточие после идентификатора несёт в этом дереве
//     ДВА разных смысла: суффикс-действие REST (`/volumes/vol…:changeDiskType`,
//     конвенция продукта) и тег образа (`img…:stable`, форма, которой владелец
//     не адресует). Лексически они неразличимы, поэтому проверка «идентификатор,
//     за которым двоеточие» краснела бы на законной конвенции — а проверку,
//     краснеющую на верном коде, отключают первой.
//
//  4. ДОМЕН, НЕ НАЗВАННЫЙ В `JudgedDomains`, не судится. На день заведения
//     граница была реальной — судились два домена из восьми; #1723 её снял:
//     остальные шесть перемерены, находок в них НОЛЬ, и теперь судятся все.
//     Перечень выписан, а не выведен обходом `services/*`: домен без клиентской
//     документации обязан быть виден как решение, а не как пропуск обхода.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных исходников, пустой словарь префиксов, ноль страниц либо ноль
// рассуженных токенов — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Формы идентификатора. Значения — то, что печатается в находке.
const (
	idFormConcat = "слитная"
	idFormHyphen = "дефисная"
)

// DocsIDFormOptions — вход анализатора.
type DocsIDFormOptions struct {
	// Root — корень дерева.
	Root string
	// ModulePath — путь модуля; по нему импорт сводится к каталогу дерева.
	ModulePath string
	// JudgedDomains — каталоги, чьи клиентские страницы судятся, относительно
	// Root. Перечень объявлен явно: он граница полосы, а не свойство дерева.
	JudgedDomains []string
}

// DocsIDFormCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type DocsIDFormCensus struct {
	GoFiles         int
	ConstLiterals   int
	MintCalls       int
	MintUnresolved  int
	UnresolvedArgs  []string
	Prefixes        int
	PrefixForms     []string
	DocFiles        int
	TokensSeen      int
	Judged          int
	UnknownPrefix   int
	UnknownPrefixes []string
	// MintedAt — префикс → позиции вызовов чеканки, его производящих. Нужно
	// потребителю словаря: находка о префиксе без координаты не восстанавливает
	// следующий шаг.
	MintedAt map[string][]string
}

// DocsIDFormFinding — один идентификатор, написанный не в той форме.
type DocsIDFormFinding struct {
	File    string
	Line    int
	Token   string
	Prefix  string
	Written string
	Minted  []string
}

func (f DocsIDFormFinding) String() string {
	return fmt.Sprintf("%s:%d: `%s` — форма %s, а префикс `%s` чеканится в форме %s",
		f.File, f.Line, f.Token, f.Written, f.Prefix, strings.Join(f.Minted, "/"))
}

var (
	// docsIDFormHyphenRe — дефисная форма: 2..3 знака префикса, дефис, тело.
	docsIDFormHyphenRe = regexp.MustCompile(`\b([a-z]{2,3})-([0-9a-hjkmnp-tv-z]{17})\b`)
	// docsIDFormConcatRe — слитная форма. Ровно 3 знака префикса: `NewID`
	// требует ровно три и на другой длине паникует, поэтому слитного
	// двухзнакового идентификатора не бывает by construction.
	docsIDFormConcatRe = regexp.MustCompile(`\b([a-z]{3})([0-9a-hjkmnp-tv-z]{17})\b`)
)

// AuditDocsIDForm выносит вердикт о дереве.
func AuditDocsIDForm(opts DocsIDFormOptions, log io.Writer) ([]DocsIDFormFinding, DocsIDFormCensus, error) {
	var census DocsIDFormCensus

	minted, err := docsIDFormMintMap(opts, &census)
	if err != nil {
		return nil, census, err
	}
	census.Prefixes = len(minted)
	for p, forms := range minted {
		census.PrefixForms = append(census.PrefixForms, p+":"+strings.Join(docsIDFormSorted(forms), "+"))
	}
	sort.Strings(census.PrefixForms)
	sort.Strings(census.UnresolvedArgs)

	pages, err := docsIDFormPages(opts)
	if err != nil {
		return nil, census, err
	}
	census.DocFiles = len(pages)

	unknown := map[string]bool{}
	var findings []DocsIDFormFinding

	for _, rel := range pages {
		// #nosec G304 -- путь получен обходом дерева документации ЭТОГО репозитория, не извне
		raw, err := os.ReadFile(filepath.Join(opts.Root, rel))
		if err != nil {
			return nil, census, fmt.Errorf("%s: %w", rel, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, m := range docsIDFormHyphenRe.FindAllStringSubmatch(line, -1) {
				findings = docsIDFormJudge(findings, minted, unknown, &census, rel, i+1, m[0], m[1], idFormHyphen)
			}
			for _, m := range docsIDFormConcatRe.FindAllStringSubmatch(line, -1) {
				findings = docsIDFormJudge(findings, minted, unknown, &census, rel, i+1, m[0], m[1], idFormConcat)
			}
		}
	}

	for p := range unknown {
		census.UnknownPrefixes = append(census.UnknownPrefixes, p)
	}
	sort.Strings(census.UnknownPrefixes)

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: исходников Go %d · строковых констант %d · вызовов чеканки %d "+
				"(из них аргумент не сведён к литералу: %d %v) · префиксов в словаре %d %v · "+
				"судимые домены %v · страниц %d · токенов встречено %d · рассужено %d · "+
				"префикс неизвестен, НЕ судится: %d %v\n",
			census.GoFiles, census.ConstLiterals, census.MintCalls,
			census.MintUnresolved, census.UnresolvedArgs,
			census.Prefixes, census.PrefixForms,
			opts.JudgedDomains, census.DocFiles, census.TokensSeen, census.Judged,
			census.UnknownPrefix, census.UnknownPrefixes)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Token < findings[j].Token
	})
	return findings, census, nil
}

// docsIDFormJudge выносит вердикт об ОДНОМ токене.
func docsIDFormJudge(
	findings []DocsIDFormFinding,
	minted map[string]map[string]bool,
	unknown map[string]bool,
	census *DocsIDFormCensus,
	rel string, line int, token, prefix, written string,
) []DocsIDFormFinding {
	census.TokensSeen++
	forms, ok := minted[prefix]
	if !ok {
		// Неизвестный префикс — слепая зона, а не «верно».
		unknown[prefix] = true
		census.UnknownPrefix++
		return findings
	}
	census.Judged++
	if forms[written] {
		return findings
	}
	return append(findings, DocsIDFormFinding{
		File: rel, Line: line, Token: token, Prefix: prefix,
		Written: written, Minted: docsIDFormSorted(forms),
	})
}

func docsIDFormSorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// docsIDFormMintMap строит отображение «префикс → чеканимые формы» разбором
// дерева.
//
// Резолв ТРАНЗИТИВНЫЙ, и это не роскошь. Прямых вызовов `ids.NewID` хватает
// ресурсам, но идентификатор ОПЕРАЦИИ чеканится через посредника
// (`operations.NewFromContext(ctx, domain.PrefixOperation, …)` → … →
// `ids.NewID(domainPrefix)`), где префикс приезжает ПАРАМЕТРОМ. Распознаватель,
// знающий одну лишь прямую форму, оставлял бы такие префиксы неизвестными — то
// есть НЕ судил бы их вовсе, молча: на дне заведения это было 9 токенов из 62
// (`sop`, `epd`), и среди них ровно те, ради которых гейт заводился.
//
// Поэтому множество «чеканящих функций» ищется до неподвижной точки: функция,
// передающая СВОЙ параметр в уже известную чеканящую функцию, сама становится
// чеканящей по этому параметру. Проходов не больше docsIDFormMaxRounds —
// граница названа, чтобы обход не мог не сойтись.
func docsIDFormMintMap(opts DocsIDFormOptions, census *DocsIDFormCensus) (map[string]map[string]bool, error) {
	// known — «чеканящие функции»: каталог.имя#индекс-параметра → форма.
	known := map[string]string{
		"pkg/ids.NewID#0":       idFormConcat,
		"pkg/ids.NewHyphenID#0": idFormHyphen,
	}
	minted := map[string]map[string]bool{}
	sites := map[string]bool{}      // позиции разрешённых вызовов — счёт без дублей по проходам
	unresolved := map[string]bool{} // позиции вызовов, чей аргумент не сведён к литералу
	mintedAt := map[string]bool{}   // «префикс\x00позиция» — дедупликация по проходам

	consts, err := docsIDFormConsts(opts, census)
	if err != nil {
		return nil, err
	}

	for round := 0; round < docsIDFormMaxRounds; round++ {
		changed, err := docsIDFormRound(opts, consts, known, minted, sites, unresolved, mintedAt)
		if err != nil {
			return nil, err
		}
		if !changed {
			break
		}
	}

	census.MintedAt = map[string][]string{}
	for k := range mintedAt {
		i := strings.IndexByte(k, 0)
		census.MintedAt[k[:i]] = append(census.MintedAt[k[:i]], k[i+1:])
	}
	for p := range census.MintedAt {
		sort.Strings(census.MintedAt[p])
	}
	census.MintCalls = len(sites) + len(unresolved)
	census.MintUnresolved = len(unresolved)
	for pos := range unresolved {
		census.UnresolvedArgs = append(census.UnresolvedArgs, pos)
	}
	return minted, nil
}

// docsIDFormMaxRounds — потолок числа проходов поиска неподвижной точки.
const docsIDFormMaxRounds = 8

// docsIDFormConsts собирает строковые константы дерева: каталог → имя → литерал.
// Резолв идёт по КАТАЛОГУ, а не по голому имени: `PrefixImage` объявлен дважды —
// `fd8` в `pkg/ids` и `img` в домене storage, и выбор решает импорт вызывающего.
func docsIDFormConsts(opts DocsIDFormOptions, census *DocsIDFormCensus) (map[string]map[string]string, error) {
	consts := map[string]map[string]string{}
	// aliases — константа, объявленная ЧЕРЕЗ ДРУГУЮ константу того же пакета
	// (`PrefixOperationCompute = PrefixInstance`). Собиратель, читающий только
	// строковые литералы, оставил бы такие имена нерезолвимыми, и префикс
	// операции вычислений остался бы вне суда — при том что объявлен он явно.
	type aliasRef struct{ dir, name, target string }
	var aliases []aliasRef

	err := docsIDFormWalkGo(opts, func(rel, dir string, file *ast.File, _ *token.FileSet) {
		census.GoFiles++
		if consts[dir] == nil {
			consts[dir] = map[string]string{}
		}
		for _, decl := range file.Decls {
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
					switch v := vs.Values[i].(type) {
					case *ast.BasicLit:
						if v.Kind != token.STRING {
							continue
						}
						lit, uerr := strconv.Unquote(v.Value)
						if uerr != nil {
							continue
						}
						consts[dir][name.Name] = lit
						census.ConstLiterals++
					case *ast.Ident:
						aliases = append(aliases, aliasRef{dir: dir, name: name.Name, target: v.Name})
					}
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}

	// Псевдонимы разрешаются до неподвижной точки: цепочка бывает длиннее одного
	// звена, а порядок обхода файлов ей не подчиняется.
	for round := 0; round < docsIDFormMaxRounds; round++ {
		changed := false
		for _, a := range aliases {
			if _, done := consts[a.dir][a.name]; done {
				continue
			}
			if v, ok := consts[a.dir][a.target]; ok {
				consts[a.dir][a.name] = v
				census.ConstLiterals++
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return consts, nil
}

// docsIDFormRound — один проход поиска неподвижной точки. Возвращает признак
// того, что множество чеканящих функций пополнилось.
func docsIDFormRound(
	opts DocsIDFormOptions,
	consts map[string]map[string]string,
	known map[string]string,
	minted map[string]map[string]bool,
	sites, unresolved, mintedAt map[string]bool,
) (bool, error) {
	changed := false
	err := docsIDFormWalkGo(opts, func(rel, dir string, file *ast.File, fset *token.FileSet) {
		imports := docsIDFormImports(opts, file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			params := docsIDFormParams(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				calleeDir, calleeName, ok := docsIDFormCallee(ce, dir, imports)
				if !ok {
					return true
				}
				for idx, arg := range ce.Args {
					form, ok := known[fmt.Sprintf("%s.%s#%d", calleeDir, calleeName, idx)]
					if !ok {
						continue
					}
					pos := fmt.Sprintf("%s:%d", rel, fset.Position(ce.Pos()).Line)
					if v, ok := docsIDFormResolve(arg, dir, imports, consts); ok {
						if minted[v] == nil {
							minted[v] = map[string]bool{}
						}
						minted[v][form] = true
						mintedAt[v+"\x00"+pos] = true
						sites[pos] = true
						delete(unresolved, pos)
						continue
					}
					// Аргумент — собственный параметр вызывающего: значит и он
					// чеканящая функция, только уровнем выше.
					if id, ok := arg.(*ast.Ident); ok {
						if j, ok := params[id.Name]; ok {
							key := fmt.Sprintf("%s.%s#%d", dir, fn.Name.Name, j)
							if _, seen := known[key]; !seen {
								known[key] = form
								changed = true
							}
							sites[pos] = true
							delete(unresolved, pos)
							continue
						}
					}
					if !sites[pos] {
						unresolved[pos] = true
					}
				}
				return true
			})
		}
	})
	return changed, err
}

// docsIDFormParams — имена параметров функции с их порядковыми номерами.
func docsIDFormParams(fn *ast.FuncDecl) map[string]int {
	out := map[string]int{}
	if fn.Type == nil || fn.Type.Params == nil {
		return out
	}
	idx := 0
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			idx++
			continue
		}
		for _, n := range f.Names {
			out[n.Name] = idx
			idx++
		}
	}
	return out
}

// docsIDFormCallee сводит вызов к паре «каталог, имя функции».
func docsIDFormCallee(ce *ast.CallExpr, dir string, imports map[string]string) (string, string, bool) {
	switch fn := ce.Fun.(type) {
	case *ast.Ident:
		return dir, fn.Name, true
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		if !ok {
			return "", "", false
		}
		target, ok := imports[pkg.Name]
		if !ok {
			return "", "", false
		}
		return target, fn.Sel.Name, true
	}
	return "", "", false
}

// docsIDFormImports — алиас пакета → каталог дерева, только для своего модуля.
func docsIDFormImports(opts DocsIDFormOptions, file *ast.File) map[string]string {
	imports := map[string]string{}
	for _, im := range file.Imports {
		p, uerr := strconv.Unquote(im.Path.Value)
		if uerr != nil {
			continue
		}
		if !strings.HasPrefix(p, opts.ModulePath+"/") {
			continue
		}
		target := strings.TrimPrefix(p, opts.ModulePath+"/")
		alias := path2pkgName(target)
		if im.Name != nil {
			alias = im.Name.Name
		}
		imports[alias] = target
	}
	return imports
}

// docsIDFormWalkGo обходит не-тестовые исходники дерева.
func docsIDFormWalkGo(opts DocsIDFormOptions, visit func(rel, dir string, file *ast.File, fset *token.FileSet)) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(opts.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(opts.Root, path)
		if rerr != nil {
			return rerr
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// Неразбираемый файл — не находка ЭТОГО анализатора.
			return nil //nolint:nilerr // предмет анализатора — чеканка, а не синтаксис чужого файла
		}
		visit(filepath.ToSlash(rel), filepath.ToSlash(filepath.Dir(rel)), file, fset)
		return nil
	})
}

// docsIDFormResolve сводит аргумент чеканки к строковому литералу.
func docsIDFormResolve(
	arg ast.Expr, dir string, imports map[string]string, consts map[string]map[string]string,
) (string, bool) {
	switch a := arg.(type) {
	case *ast.BasicLit:
		if a.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(a.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.Ident:
		v, ok := consts[dir][a.Name]
		return v, ok
	case *ast.SelectorExpr:
		pkg, ok := a.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		target, ok := imports[pkg.Name]
		if !ok {
			return "", false
		}
		v, ok := consts[target][a.Sel.Name]
		return v, ok
	}
	return "", false
}

// path2pkgName — имя пакета по умолчанию для пути импорта.
func path2pkgName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// docsIDFormPages собирает клиентские страницы судимых доменов.
func docsIDFormPages(opts DocsIDFormOptions) ([]string, error) {
	var out []string
	for _, domain := range opts.JudgedDomains {
		root := filepath.Join(opts.Root, filepath.FromSlash(domain))
		if _, err := os.Stat(root); err != nil {
			// Отсутствующий судимый домен — не молчание: пустой обход роняет
			// вердикт премисой, а не проходит.
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
				return nil
			}
			rel, rerr := filepath.Rel(opts.Root, path)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
