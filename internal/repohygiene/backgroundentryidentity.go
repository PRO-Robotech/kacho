// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// backgroundentryidentity.go — ГЕЙТ ПЕРЕСЫЛАЕМОЙ ЛИЧНОСТИ ФОНОВЫХ ВХОДОВ
// (УК3-31, замысел issue-2918 §11а; CX3D-01 (б), CX3H-02 (в); З6, З13).
//
// # Предмет
//
// Фоновый вход — функция, которую зовёт процесс, а не запрос: у её контекста
// нет принципала пользователя. Межмодульный клиент пересылает личность
// контекста владельцу (`auth.PropagateOutgoing`); на контексте без принципала
// он пересылает запасную `{system, bootstrap}` — ту же пару, что даёт явный
// `operations.SystemPrincipal()`. Владелец, чей метод ПИШЕТ (меняет состояние
// и журнал), получает тогда безымянного системного вызывающего. Гейт судит
// обе половины заказа:
//
//	(1) вход достигает пересылающего вызова пишущего метода БЕЗ явного
//	    принципала в контексте — находка;
//	(2) достигает его с `operations.WithPrincipal(…, operations.SystemPrincipal())`
//	    — находка.
//
// `journaltx.AsComponent(ctx, служба, роль)` и `WithPrincipal(…,
// auth.SystemPrincipalFor(служба, роль))` — явный принципал компонента; пара
// вне таблицы фоновых путей [JournaledComponentPairs] — находка (3).
//
// # Перечень входов объявлен здесь, а не выведен
//
// [BackgroundEntries] — входы, названные замыслом: nlb `reconcileOne`, compute
// `FinishStuckDeletes`. Пустой перечень — беспредметность, а не зелёный.
//
// # Исключение — строкой ведомости с предикатом снятия
//
// [BackgroundEntryExceptions] — вход compute `FinishStuckDeletes` по половине
// (1): его пересылаемую личность под-фаза не меняет (CX3H-02). Исключение
// печатается в объёме и истекает само: строка без поглощённой находки —
// находка; строка на вход вне перечня — находка; строка, чья задача закрыта, —
// находка (решение отделено от сетевого измерения, см. пробу трекера).
//
// # Как строится достижимость — границы названы словами
//
//  1. граф вызовов строится РАЗБОРОМ модуля (`services/<модуль>/`, не-тестовые
//     файлы), без проверки типов компилятором. Вызов функции пакета модуля и
//     метода установленного типа идёт в тело; метод интерфейса и метод
//     получателя, чей тип не установлен, — во ВСЕ одноимённые методы модуля
//     (верхняя граница; число неустановленных печатается);
//  2. пакеты вне модуля (corelib, `pkg/`, внешние) в обход не входят: их
//     пересылающие вызовы этим гейтом не видны;
//  3. состояние принципала ведётся в порядке исходного текста функции: установка
//     действует на всё, что стоит ниже неё в той же функции и в вызванных
//     оттуда, ветвление не различается;
//  4. пересылающий вызов — вызов, чей первый аргумент — `auth.PropagateOutgoing(…)`
//     либо имя, которому в той же функции присвоен его результат; метод с
//     приставкой чтения ([beiReadVerbs]) пишущим не считается.
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
	"sort"
	"strconv"
	"strings"
)

// Половины заказа гейта.
const (
	BEIHalfNoPrincipal     = "(1)"
	BEIHalfSystemPrincipal = "(2)"
	BEIHalfUndeclaredPair  = "(3)"
	BEIExceptionRule       = "(искл.)"
)

// BackgroundEntry — фоновый вход: функция (метод) в каталоге модуля.
type BackgroundEntry struct {
	Module string // каталог под `services/`
	Dir    string // каталог пакета от корня дерева
	Recv   string // тип получателя; пусто — функция пакета
	Func   string
}

// Key — имя входа в ведомости исключений: `<модуль>.<функция>`.
func (e BackgroundEntry) Key() string { return e.Module + "." + e.Func }

// BackgroundEntries — входы, названные замыслом (УК3-31).
var BackgroundEntries = []BackgroundEntry{
	{Module: "nlb", Dir: "services/nlb/internal/apps/kacho/jobs", Recv: "FreeIPRunner", Func: "reconcileOne"},
	{Module: "compute", Dir: "services/compute/internal/apps/kacho/api/instance", Recv: "InstanceService", Func: "FinishStuckDeletes"},
}

// BackgroundEntryException — строка ведомости исключений гейта.
type BackgroundEntryException struct {
	Entry   string // [BackgroundEntry.Key]
	Half    string // какую половину исключает
	Issue   int    // задача, чьим закрытием строка снимается
	Why     string
	Removal string // предикат снятия словами
}

// BackgroundEntryExceptions — ведомость исключений (CX3H-02 (в)).
var BackgroundEntryExceptions = []BackgroundEntryException{
	{
		Entry: "compute.FinishStuckDeletes",
		Half:  BEIHalfNoPrincipal,
		Issue: 2934,
		Why: "пересылаемая личность доделывателя — kacho#2934; смена личности превращает отказ " +
			"сужающего чтения в пустой успех (CX3H-02)",
		Removal: "#2934 закрыт и в kaname на его ревизии есть привязка, выполняющая viewer на " +
			"compute_instance и viewer ∪ v_list на vpc_network_interface для личности доделывателя, " +
			"либо проход различает «сужено» и «пусто»",
	},
}

// beiReadVerbs — приставки методов чтения: такой пересылающий вызов не пишет.
var beiReadVerbs = []string{"Get", "List", "Check", "Lookup", "Watch"}

const (
	beiModulePrefix    = "github.com/PRO-Robotech/kacho/"
	beiAuthPath        = "github.com/PRO-Robotech/corelib/auth"
	beiOperationsPath  = "github.com/PRO-Robotech/corelib/operations"
	beiJournaltxPath   = "github.com/PRO-Robotech/corelib/journaltx"
	beiMaxDepth        = 48
	beiStateNone       = "без принципала"
	beiStateSystem     = "operations.SystemPrincipal()"
	beiStateExplicit   = "явный принципал"
	beiStateComponentP = "компонент "
	beiOriginSep       = " @"
)

// BackgroundEntryOptions — вход гейта.
type BackgroundEntryOptions struct {
	Root       string
	Entries    []BackgroundEntry          // nil — [BackgroundEntries]
	Exceptions []BackgroundEntryException // nil — [BackgroundEntryExceptions]
	Pairs      []JournaledComponentPair   // nil — [JournaledComponentPairs]
	// IssueStates — состояние задач ведомости (`OPEN`/`CLOSED`); nil — не
	// измерялось (сетевое измерение — отдельной ручкой).
	IssueStates map[int]string
}

// BackgroundEntryFinding — находка.
type BackgroundEntryFinding struct {
	Half   string
	Entry  string
	Pos    string
	Detail string
}

func (f BackgroundEntryFinding) String() string {
	return fmt.Sprintf("%s %s: %s — %s", f.Half, f.Entry, f.Pos, f.Detail)
}

// BackgroundEntryReach — один пересылающий вызов, достигнутый со входа.
type BackgroundEntryReach struct {
	Pos    string
	Method string
	Write  bool
	State  string
}

// BackgroundEntryCensus — объём по входу.
type BackgroundEntryCensus struct {
	Entry       string
	Found       bool
	ModuleFiles int
	Visited     int // функций в обходе
	Unresolved  int // получателей, чей тип не установлен (верхняя граница)
	Reaches     []BackgroundEntryReach
	Exempted    []BackgroundEntryFinding
}

// Writes — пересылающих вызовов пишущих методов.
func (c BackgroundEntryCensus) Writes() int {
	n := 0
	for _, r := range c.Reaches {
		if r.Write {
			n++
		}
	}
	return n
}

// AuditBackgroundEntryIdentity — гейт УК3-31.
func AuditBackgroundEntryIdentity(opts BackgroundEntryOptions, log io.Writer) ([]BackgroundEntryFinding, []BackgroundEntryCensus, error) {
	entries := opts.Entries
	if entries == nil {
		entries = BackgroundEntries
	}
	exceptions := opts.Exceptions
	if exceptions == nil {
		exceptions = BackgroundEntryExceptions
	}
	pairs := opts.Pairs
	if pairs == nil {
		pairs = JournaledComponentPairs
	}

	var (
		findings []BackgroundEntryFinding
		census   []BackgroundEntryCensus
		mods     = map[string]*beiModule{}
	)
	for _, e := range entries {
		m, ok := mods[e.Module]
		if !ok {
			var err error
			m, err = beiLoadModule(opts.Root, e.Module)
			if err != nil {
				return nil, nil, err
			}
			mods[e.Module] = m
		}
		c, fs := beiAuditEntry(m, e, pairs)
		census = append(census, c)
		findings = append(findings, fs...)
	}

	// Ведомость исключений: поглощение, самоистечение, предикат снятия.
	byEntry := map[string]bool{}
	for _, e := range entries {
		byEntry[e.Key()] = true
	}
	var kept []BackgroundEntryFinding
	absorbed := map[int]int{}
	for _, f := range findings {
		idx := -1
		for i, x := range exceptions {
			if x.Entry == f.Entry && x.Half == f.Half && byEntry[x.Entry] {
				idx = i
				break
			}
		}
		if idx < 0 {
			kept = append(kept, f)
			continue
		}
		absorbed[idx]++
		for ci := range census {
			if census[ci].Entry == f.Entry {
				census[ci].Exempted = append(census[ci].Exempted, f)
			}
		}
	}
	findings = kept
	for i, x := range exceptions {
		switch {
		case !byEntry[x.Entry]:
			findings = append(findings, BackgroundEntryFinding{Half: BEIExceptionRule, Entry: x.Entry, Pos: "ведомость",
				Detail: fmt.Sprintf("строка исключения на вход вне перечня входов (задача #%d)", x.Issue)})
		case absorbed[i] == 0:
			findings = append(findings, BackgroundEntryFinding{Half: BEIExceptionRule, Entry: x.Entry, Pos: "ведомость",
				Detail: fmt.Sprintf("строка исключения половины %s не поглотила ни одной находки — исключать нечего, строка истекла (задача #%d)", x.Half, x.Issue)})
		}
	}
	findings = append(findings, BackgroundExceptionsWhoseRemovalHolds(exceptions, opts.IssueStates)...)

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Entry != findings[j].Entry {
			return findings[i].Entry < findings[j].Entry
		}
		return findings[i].Pos < findings[j].Pos
	})
	beiPrint(log, census, exceptions, findings)
	return findings, census, nil
}

// BackgroundExceptionsWhoseRemovalHolds — решение о предикате снятия, отделённое
// от сетевого измерения: строка, чья задача закрыта, — находка. Состояние
// неизвестно (не измерено) — не находка: несостоявшееся измерение вердиктом не
// становится и считается отдельно.
func BackgroundExceptionsWhoseRemovalHolds(exceptions []BackgroundEntryException, states map[int]string) []BackgroundEntryFinding {
	var out []BackgroundEntryFinding
	for _, x := range exceptions {
		if strings.EqualFold(states[x.Issue], "CLOSED") {
			out = append(out, BackgroundEntryFinding{Half: BEIExceptionRule, Entry: x.Entry, Pos: "ведомость",
				Detail: fmt.Sprintf("задача #%d закрыта, а строка исключения стоит — предикат снятия: %s", x.Issue, x.Removal)})
		}
	}
	return out
}

// BackgroundEntryPremiseFailures — причины беспредметности.
func BackgroundEntryPremiseFailures(c []BackgroundEntryCensus) []string {
	var out []string
	if len(c) == 0 {
		out = append(out, "перечень фоновых входов пуст — судить нечего")
	}
	for _, e := range c {
		switch {
		case e.ModuleFiles == 0:
			out = append(out, fmt.Sprintf("%s: в модуле не прочитано ни одного Go-файла", e.Entry))
		case !e.Found:
			out = append(out, fmt.Sprintf("%s: функция входа в дереве не найдена", e.Entry))
		case e.Writes() == 0:
			out = append(out, fmt.Sprintf("%s: вход не достигает ни одного пересылающего вызова пишущего метода — распознаватель мёртв либо вход перестал писать", e.Entry))
		}
	}
	return out
}

func beiPrint(log io.Writer, c []BackgroundEntryCensus, ex []BackgroundEntryException, findings []BackgroundEntryFinding) {
	if log == nil {
		return
	}
	_, _ = fmt.Fprintf(log, "УК3-31 перепись: входов %d, строк исключений %d, находок %d\n", len(c), len(ex), len(findings))
	for _, e := range c {
		_, _ = fmt.Fprintf(log, "  %s: найден %v; Go-файлов модуля %d; функций в обходе %d; получателей не установлено %d; пересылающих вызовов %d, из них пишущих %d; исключено %d\n",
			e.Entry, e.Found, e.ModuleFiles, e.Visited, e.Unresolved, len(e.Reaches), e.Writes(), len(e.Exempted))
		for _, r := range e.Reaches {
			kind := "чтение"
			if r.Write {
				kind = "запись"
			}
			_, _ = fmt.Fprintf(log, "    %s %s (%s) — %s\n", r.Pos, r.Method, kind, r.State)
		}
		for _, x := range e.Exempted {
			_, _ = fmt.Fprintf(log, "    исключено: %s\n", x)
		}
	}
	for _, x := range ex {
		_, _ = fmt.Fprintf(log, "  строка исключения: %s половина %s, задача #%d; причина: %s; предикат снятия: %s\n",
			x.Entry, x.Half, x.Issue, x.Why, x.Removal)
	}
}

// ── разбор модуля ─────────────────────────────────────────────────────────

type beiFile struct {
	rel     string
	dir     string
	f       *ast.File
	imports map[string]string // имя → путь
}

type beiFunc struct {
	key  string
	file *beiFile
	decl *ast.FuncDecl
}

type beiType struct {
	iface  bool
	fields map[string]ast.Expr
	file   *beiFile
}

type beiModule struct {
	name   string
	fset   *token.FileSet
	files  int
	funcs  map[string]*beiFunc // dir|recv|name
	byName map[string][]string // имя метода → ключи
	types  map[string]*beiType // dir|T
	consts map[string]string   // dir|имя → строка
}

func beiKey(dir, recv, name string) string { return dir + "|" + recv + "|" + name }

func beiLoadModule(root, module string) (*beiModule, error) {
	m := &beiModule{name: module, fset: token.NewFileSet(), funcs: map[string]*beiFunc{},
		byName: map[string][]string{}, types: map[string]*beiType{}, consts: map[string]string{}}
	base := filepath.Join(root, "services", module)
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := d.Name()
			if p != base && (n == "testdata" || n == "vendor" || n == "node_modules" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(m.fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("УК3-31: разбор %s: %w", rel, err)
		}
		m.files++
		bf := &beiFile{rel: rel, dir: filepath.ToSlash(filepath.Dir(rel)), f: f, imports: map[string]string{}}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			name := jwPackageNameOf(path)
			if im.Name != nil {
				name = im.Name.Name
			}
			bf.imports[name] = path
		}
		for _, d := range f.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				recv := ""
				if x.Recv != nil && len(x.Recv.List) == 1 {
					recv = beiBaseTypeName(x.Recv.List[0].Type)
				}
				k := beiKey(bf.dir, recv, x.Name.Name)
				m.funcs[k] = &beiFunc{key: k, file: bf, decl: x}
				if recv != "" {
					m.byName[x.Name.Name] = append(m.byName[x.Name.Name], k)
				}
			case *ast.GenDecl:
				for _, s := range x.Specs {
					switch sp := s.(type) {
					case *ast.TypeSpec:
						t := &beiType{fields: map[string]ast.Expr{}, file: bf}
						switch tt := sp.Type.(type) {
						case *ast.InterfaceType:
							t.iface = true
						case *ast.StructType:
							for _, fl := range tt.Fields.List {
								for _, n := range fl.Names {
									t.fields[n.Name] = fl.Type
								}
							}
						}
						m.types[bf.dir+"|"+sp.Name.Name] = t
					case *ast.ValueSpec:
						if x.Tok != token.CONST {
							continue
						}
						for i, n := range sp.Names {
							if i < len(sp.Values) {
								if bl, ok := sp.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
									if s, err := strconv.Unquote(bl.Value); err == nil {
										m.consts[bf.dir+"|"+n.Name] = s
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	return m, err
}

func beiBaseTypeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return beiBaseTypeName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return beiBaseTypeName(x.X)
	case *ast.IndexListExpr:
		return beiBaseTypeName(x.X)
	}
	return ""
}

// beiTypeRef — установленный тип: внешний, тип модуля либо неизвестный.
type beiTypeRef struct {
	external  bool
	dir, name string
}

// resolveTypeExpr — тип по выражению типа в контексте файла.
func (m *beiModule) resolveTypeExpr(e ast.Expr, bf *beiFile) beiTypeRef {
	switch x := e.(type) {
	case *ast.StarExpr:
		return m.resolveTypeExpr(x.X, bf)
	case *ast.ParenExpr:
		return m.resolveTypeExpr(x.X, bf)
	case *ast.Ident:
		switch x.Name {
		case "error", "string", "bool", "int", "int32", "int64", "uint", "uint32", "uint64", "float64", "byte", "rune", "any":
			return beiTypeRef{external: true}
		}
		return beiTypeRef{dir: bf.dir, name: x.Name}
	case *ast.SelectorExpr:
		pk, ok := x.X.(*ast.Ident)
		if !ok {
			return beiTypeRef{}
		}
		path, ok := bf.imports[pk.Name]
		if !ok {
			return beiTypeRef{}
		}
		if !strings.HasPrefix(path, beiModulePrefix+"services/"+m.name+"/") {
			return beiTypeRef{external: true}
		}
		return beiTypeRef{dir: strings.TrimPrefix(path, beiModulePrefix), name: x.Sel.Name}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		return beiTypeRef{external: true}
	case *ast.IndexExpr:
		return m.resolveTypeExpr(x.X, bf)
	}
	return beiTypeRef{}
}

// ── обход входа ───────────────────────────────────────────────────────────

type beiWalk struct {
	m       *beiModule
	entry   string
	pairs   []JournaledComponentPair
	seen    map[string]bool
	census  *BackgroundEntryCensus
	out     []BackgroundEntryFinding
	reached map[string]bool
}

func beiAuditEntry(m *beiModule, e BackgroundEntry, pairs []JournaledComponentPair) (BackgroundEntryCensus, []BackgroundEntryFinding) {
	c := BackgroundEntryCensus{Entry: e.Key(), ModuleFiles: m.files}
	fn, ok := m.funcs[beiKey(e.Dir, e.Recv, e.Func)]
	if !ok {
		return c, []BackgroundEntryFinding{{Half: BEIExceptionRule, Entry: e.Key(), Pos: e.Dir,
			Detail: fmt.Sprintf("функция входа %s.%s в дереве не найдена", e.Recv, e.Func)}}
	}
	c.Found = true
	w := &beiWalk{m: m, entry: e.Key(), pairs: pairs, seen: map[string]bool{}, census: &c, reached: map[string]bool{}}
	w.visit(fn, beiStateNone, 0)
	return c, w.out
}

// beiScope — что известно о локальных именах функции.
type beiScope struct {
	fn         *beiFunc
	recvName   string
	recvType   beiTypeRef
	types      map[string]ast.Expr
	assigns    map[string]ast.Expr
	propagated map[string]bool
}

func (w *beiWalk) visit(fn *beiFunc, state string, depth int) {
	memo := fn.key + "#" + state
	if w.seen[memo] || depth > beiMaxDepth || fn.decl.Body == nil {
		return
	}
	w.seen[memo] = true
	w.census.Visited++

	sc := &beiScope{fn: fn, types: map[string]ast.Expr{}, assigns: map[string]ast.Expr{}, propagated: map[string]bool{}}
	if fn.decl.Recv != nil && len(fn.decl.Recv.List) == 1 {
		r := fn.decl.Recv.List[0]
		sc.recvType = w.m.resolveTypeExpr(r.Type, fn.file)
		for _, n := range r.Names {
			sc.recvName = n.Name
		}
	}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				sc.types[n.Name] = f.Type
			}
		}
	}
	addFields(fn.decl.Type.Params)

	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			addFields(x.Type.Params)
		case *ast.ValueSpec:
			for i, nm := range x.Names {
				if x.Type != nil {
					sc.types[nm.Name] = x.Type
				}
				if i < len(x.Values) {
					sc.assigns[nm.Name] = x.Values[i]
					if s, ok := w.principalSetting(x.Values[i], fn.file); ok {
						state = s + beiOriginSep + posOf(w.m.fset, fn.file.rel, x.Pos())
					}
					if w.isPropagate(x.Values[i], fn.file) {
						sc.propagated[nm.Name] = true
					}
				}
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok {
					continue
				}
				var rhs ast.Expr
				switch {
				case len(x.Lhs) == len(x.Rhs):
					rhs = x.Rhs[i]
				case len(x.Rhs) == 1 && i == 0:
					rhs = x.Rhs[0]
				}
				if rhs == nil {
					continue
				}
				if _, has := sc.assigns[id.Name]; !has {
					sc.assigns[id.Name] = rhs
				}
				if s, ok := w.principalSetting(rhs, fn.file); ok {
					state = s + beiOriginSep + posOf(w.m.fset, fn.file.rel, x.Pos())
				}
				if w.isPropagate(rhs, fn.file) {
					sc.propagated[id.Name] = true
				}
			}
		case *ast.CallExpr:
			if w.forwardingCall(x, sc, state) {
				return true
			}
			for _, callee := range w.callees(x, sc) {
				w.visit(callee, state, depth+1)
			}
		}
		return true
	})
}

func (w *beiWalk) importIs(bf *beiFile, name, path string) bool {
	return bf.imports[name] == path
}

// principalSetting — выражение, устанавливающее принципал контекста.
func (w *beiWalk) principalSetting(e ast.Expr, bf *beiFile) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pk, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	switch {
	case w.importIs(bf, pk.Name, beiJournaltxPath) && sel.Sel.Name == "AsComponent" && len(call.Args) == 3:
		return beiStateComponentP + w.pair(call.Args[1], call.Args[2], bf), true
	case w.importIs(bf, pk.Name, beiOperationsPath) && sel.Sel.Name == "WithoutPrincipal":
		return beiStateNone, true
	case w.importIs(bf, pk.Name, beiOperationsPath) && sel.Sel.Name == "WithPrincipal" && len(call.Args) == 2:
		if inner, ok := call.Args[1].(*ast.CallExpr); ok {
			if isel, ok := inner.Fun.(*ast.SelectorExpr); ok {
				if ip, ok := isel.X.(*ast.Ident); ok {
					switch {
					case w.importIs(bf, ip.Name, beiOperationsPath) && isel.Sel.Name == "SystemPrincipal":
						return beiStateSystem, true
					case w.importIs(bf, ip.Name, beiAuthPath) && isel.Sel.Name == "SystemPrincipalFor" && len(inner.Args) == 2:
						return beiStateComponentP + w.pair(inner.Args[0], inner.Args[1], bf), true
					}
				}
			}
		}
		return beiStateExplicit, true
	}
	return "", false
}

func (w *beiWalk) pair(a, b ast.Expr, bf *beiFile) string {
	return "(" + w.strOf(a, bf) + ", " + w.strOf(b, bf) + ")"
}

func (w *beiWalk) strOf(e ast.Expr, bf *beiFile) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if s, err := strconv.Unquote(x.Value); err == nil {
			return s
		}
	case *ast.Ident:
		if s, ok := w.m.consts[bf.dir+"|"+x.Name]; ok {
			return s
		}
	}
	return "?"
}

func (w *beiWalk) isPropagate(e ast.Expr, bf *beiFile) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pk, ok := sel.X.(*ast.Ident)
	return ok && sel.Sel.Name == "PropagateOutgoing" && w.importIs(bf, pk.Name, beiAuthPath)
}

// forwardingCall судит пересылающий вызов; true — вызов пересылающий (в тело
// не входим: это вызов чужого метода по проводу).
func (w *beiWalk) forwardingCall(call *ast.CallExpr, sc *beiScope, state string) bool {
	if len(call.Args) == 0 || w.isPropagate(call, sc.fn.file) {
		// Сам `PropagateOutgoing(ctx)` на уже пересланном имени — не вызов
		// владельца, а повторная обёртка.
		return false
	}
	fwd := w.isPropagate(call.Args[0], sc.fn.file)
	if id, ok := call.Args[0].(*ast.Ident); ok && sc.propagated[id.Name] {
		fwd = true
	}
	if !fwd {
		return false
	}
	method := ""
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		method = f.Sel.Name
	case *ast.Ident:
		method = f.Name
	}
	write := true
	for _, v := range beiReadVerbs {
		if strings.HasPrefix(method, v) {
			write = false
			break
		}
	}
	pos := posOf(w.m.fset, sc.fn.file.rel, call.Pos())
	rk := pos + "#" + state
	if w.reached[rk] {
		return true
	}
	w.reached[rk] = true
	w.census.Reaches = append(w.census.Reaches, BackgroundEntryReach{Pos: pos, Method: method, Write: write, State: state})
	if !write {
		return true
	}
	kind, origin, _ := strings.Cut(state, beiOriginSep)
	where := ""
	if origin != "" {
		where = " (установлено " + origin + ")"
	}
	switch {
	case kind == beiStateNone:
		w.out = append(w.out, BackgroundEntryFinding{Half: BEIHalfNoPrincipal, Entry: w.entry, Pos: pos,
			Detail: fmt.Sprintf("пишущий метод %s пересылает личность контекста без явного принципала — на провод уходит запасная {system, bootstrap}", method)})
	case kind == beiStateSystem:
		w.out = append(w.out, BackgroundEntryFinding{Half: BEIHalfSystemPrincipal, Entry: w.entry, Pos: pos,
			Detail: fmt.Sprintf("пишущий метод %s пересылает operations.SystemPrincipal()%s — безымянную системную личность вместо принципала компонента", method, where)})
	case strings.HasPrefix(kind, beiStateComponentP):
		p := strings.TrimPrefix(kind, beiStateComponentP)
		declared := false
		for _, x := range w.pairs {
			if x.Module == w.m.name && "("+x.Service+", "+x.Role+")" == p {
				declared = true
			}
		}
		if !declared {
			w.out = append(w.out, BackgroundEntryFinding{Half: BEIHalfUndeclaredPair, Entry: w.entry, Pos: pos,
				Detail: fmt.Sprintf("пишущий метод %s пересылает принципал компонента %s%s вне таблицы фоновых путей", method, p, where)})
		}
	}
	return true
}

// callees — тела модуля, в которые идёт вызов.
func (w *beiWalk) callees(call *ast.CallExpr, sc *beiScope) []*beiFunc {
	bf := sc.fn.file
	switch f := call.Fun.(type) {
	case *ast.Ident:
		if fn, ok := w.m.funcs[beiKey(bf.dir, "", f.Name)]; ok {
			return []*beiFunc{fn}
		}
		return nil
	case *ast.SelectorExpr:
		if pk, ok := f.X.(*ast.Ident); ok && !w.local(pk.Name, sc) {
			if path, ok := bf.imports[pk.Name]; ok {
				if !strings.HasPrefix(path, beiModulePrefix+"services/"+w.m.name+"/") {
					return nil
				}
				if fn, ok := w.m.funcs[beiKey(strings.TrimPrefix(path, beiModulePrefix), "", f.Sel.Name)]; ok {
					return []*beiFunc{fn}
				}
				return nil
			}
		}
		ref := w.typeOf(f.X, sc, 0)
		switch {
		case ref.external:
			return nil
		case ref.name != "":
			if t, ok := w.m.types[ref.dir+"|"+ref.name]; ok && !t.iface {
				if fn, ok := w.m.funcs[beiKey(ref.dir, ref.name, f.Sel.Name)]; ok {
					return []*beiFunc{fn}
				}
			}
			if t, ok := w.m.types[ref.dir+"|"+ref.name]; ok && t.iface {
				return w.byName(f.Sel.Name)
			}
			if fn, ok := w.m.funcs[beiKey(ref.dir, ref.name, f.Sel.Name)]; ok {
				return []*beiFunc{fn}
			}
		}
		out := w.byName(f.Sel.Name)
		if len(out) > 0 {
			w.census.Unresolved++
		}
		return out
	}
	return nil
}

func (w *beiWalk) local(name string, sc *beiScope) bool {
	if name == sc.recvName {
		return true
	}
	if _, ok := sc.types[name]; ok {
		return true
	}
	_, ok := sc.assigns[name]
	return ok
}

func (w *beiWalk) byName(name string) []*beiFunc {
	var out []*beiFunc
	for _, k := range w.m.byName[name] {
		out = append(out, w.m.funcs[k])
	}
	return out
}

// typeOf — тип выражения-получателя.
func (w *beiWalk) typeOf(e ast.Expr, sc *beiScope, depth int) beiTypeRef {
	if depth > 6 {
		return beiTypeRef{}
	}
	bf := sc.fn.file
	switch x := e.(type) {
	case *ast.ParenExpr:
		return w.typeOf(x.X, sc, depth+1)
	case *ast.StarExpr:
		return w.typeOf(x.X, sc, depth+1)
	case *ast.UnaryExpr:
		return w.typeOf(x.X, sc, depth+1)
	case *ast.CompositeLit:
		if x.Type != nil {
			return w.m.resolveTypeExpr(x.Type, bf)
		}
	case *ast.Ident:
		if x.Name == sc.recvName && sc.recvName != "" {
			return sc.recvType
		}
		if t, ok := sc.types[x.Name]; ok {
			return w.m.resolveTypeExpr(t, bf)
		}
		if a, ok := sc.assigns[x.Name]; ok {
			return w.typeOf(a, sc, depth+1)
		}
	case *ast.SelectorExpr:
		owner := w.typeOf(x.X, sc, depth+1)
		if owner.external {
			return owner
		}
		if owner.name != "" {
			if t, ok := w.m.types[owner.dir+"|"+owner.name]; ok {
				if ft, ok := t.fields[x.Sel.Name]; ok {
					return w.m.resolveTypeExpr(ft, t.file)
				}
			}
		}
	case *ast.CallExpr:
		switch f := x.Fun.(type) {
		case *ast.SelectorExpr:
			if pk, ok := f.X.(*ast.Ident); ok && !w.local(pk.Name, sc) {
				if path, ok := bf.imports[pk.Name]; ok {
					if !strings.HasPrefix(path, beiModulePrefix+"services/"+w.m.name+"/") {
						return beiTypeRef{external: true}
					}
					if fn, ok := w.m.funcs[beiKey(strings.TrimPrefix(path, beiModulePrefix), "", f.Sel.Name)]; ok {
						return w.firstResult(fn)
					}
				}
			}
		case *ast.Ident:
			if fn, ok := w.m.funcs[beiKey(bf.dir, "", f.Name)]; ok {
				return w.firstResult(fn)
			}
		}
	}
	return beiTypeRef{}
}

func (w *beiWalk) firstResult(fn *beiFunc) beiTypeRef {
	r := fn.decl.Type.Results
	if r == nil || len(r.List) == 0 {
		return beiTypeRef{}
	}
	return w.m.resolveTypeExpr(r.List[0].Type, fn.file)
}
