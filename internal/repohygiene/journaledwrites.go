// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journaledwrites.go — ГЕЙТ ПОМОЩНИКА ЗАПИСИ ЖУРНАЛА (УК3-27, замысел
// issue-2918 З4; приёмка NTF-3, NTF3-57, NTF3-58, NTF3-62).
//
// # Предмет
//
// Строку журнала подписки модуля без инициатора база не принимает (колонка
// `initiator NOT NULL`, умолчание — из настройки транзакции
// `kacho_journal.initiator`). Настройку выставляет ЛОКАЛЬНО к транзакции ровно
// один производитель — помощник `corelib/journaltx` ([journaltx.Begin]); его
// источник — принципал контекста. Поэтому любая запись, способная породить
// строку журнала, обязана идти транзакцией помощника. Гейт судит пять правил:
//
//	(а) `Begin`/`BeginTx`/`BeginFunc`/`BeginTxFunc` на пуле (и на соединении) в
//	    не-тестовом дереве подключённых модулей — находка, кроме транзакций,
//	    объявленных `ReadOnly` признаком в самом вызове; получатель, тип которого
//	    разбор не установил, — тоже находка (непрочитанное не «разрешено»);
//	(б) `Exec`/`Query`/`QueryRow` на пуле, чей SQL пишет журналируемую таблицу
//	    (журнал модуля либо таблицу, на которой функция базы пишет журнал), —
//	    находка; запись, чью таблицу разбор не установил, — находка;
//	(в) оператор вставки в журнал, называющий колонку `initiator`, — находка:
//	    производитель значения один — умолчание колонки;
//	(г) `set_config('kacho_journal.initiator', …, false)`, `SET` этой настройки
//	    без `LOCAL` и параметр старта сессии `-c kacho_journal.initiator=` в
//	    не-тестовом коде — находка где угодно в стволе: сессионная настройка
//	    стала бы инициатором ЧУЖОЙ следующей транзакции соединения;
//	(д) вызов `journaltx.AsComponent` в не-тестовом дереве модулей вне двух пар
//	    §8 замысла — `(storage, reconciler)`, `(nlb, free-ip-runner)` — находка
//	    (в том числе любой вызов в compute, CX3H-02); отсутствие вызова пары —
//	    тоже находка: фоновый путь без личности компонента получил бы отказ
//	    `journaltx.Begin`. Перечень вызывающих печатается.
//
// # Журналируемые таблицы ВЫВЕДЕНЫ из дерева
//
// Модуль — каталог `services/<m>/internal/subscriptionjournal/` с константой
// `Table`. Журналируемая таблица — сам журнал и всякая таблица, на которой
// объявлен триггер функции, вставляющей в журнал. Функции и триггеры читаются по
// ВСЕМ ревизиям миграций: живое тело знает только база, и перепись называет
// верхнюю границу (см. границы `journalwriteforms.go`).
//
// # Границы, названные словами
//
//  1. тип получателя устанавливается разбором пакета (поля структур, параметры,
//     локальные присваивания), без проверки типов компилятором; что не
//     установлено — находка, а не молчание;
//  2. пул, переданный ВНЕШНЕЙ функции (не из дерева модуля), внутрь которой разбор
//     не заходит, не судится; его число печатается переписью;
//  3. SQL, собранный не литералом, не константой, не `fmt.Sprintf` с
//     литеральным форматом и не одиночным локальным присваиванием этих форм, —
//     находка «оператор не установлен».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Правила гейта — буквы замысла.
const (
	JWRuleBegin       = "(а)"
	JWRulePoolWrite   = "(б)"
	JWRuleNamesColumn = "(в)"
	JWRuleSessionSet  = "(г)"
	JWRuleComponent   = "(д)"
)

// JournaledComponentPair — пара личности компонента §8 замысла.
type JournaledComponentPair struct {
	Module, Service, Role string
}

// JournaledComponentPairs — две законные пары `AsComponent` в дереве модулей.
var JournaledComponentPairs = []JournaledComponentPair{
	{Module: "storage", Service: "storage", Role: "reconciler"},
	{Module: "nlb", Service: "nlb", Role: "free-ip-runner"},
}

// JournaledWriteOptions — вход гейта.
type JournaledWriteOptions struct {
	Root string
	// TrunkRoots — каталоги ствола для правила (г); пусто — умолчание.
	TrunkRoots []string
	// Pairs — законные пары (д); nil — [JournaledComponentPairs].
	Pairs []JournaledComponentPair
}

// JournaledWriteFinding — одна находка.
type JournaledWriteFinding struct {
	Rule   string
	Module string
	Pos    string
	Detail string
}

func (f JournaledWriteFinding) String() string {
	return fmt.Sprintf("%s %s: %s — %s", f.Rule, f.Module, f.Pos, f.Detail)
}

// JournaledWriteModule — перепись одного модуля.
type JournaledWriteModule struct {
	Name            string
	Journal         string
	JournaledTables []string
	GoFiles         int
	SQLFiles        int
	HelperBegins    int // открытий транзакции помощником journaltx.Begin
	BeginCalls      int // (а) прочих вызовов открытия транзакции, включая точки сохранения
	BeginOnPool     int
	BeginReadOnly   int
	BeginOnTx       int // точки сохранения на транзакции
	BeginUnresolved int
	PoolStatements  int // (б) Exec/Query/QueryRow на пуле
	PoolWrites      int // из них пишущих
	PoolToExternal  int // пул передан внешней функции
	AsComponent     []string
}

// JournaledWriteCensus — перепись целиком.
type JournaledWriteCensus struct {
	Modules        []JournaledWriteModule
	TrunkFiles     int // файлов, прочитанных правилом (г)
	SetConfigCalls int // вызовов set_config в стволе
}

// Module — перепись модуля по имени.
func (c JournaledWriteCensus) Module(name string) (JournaledWriteModule, bool) {
	for _, m := range c.Modules {
		if m.Name == name {
			return m, true
		}
	}
	return JournaledWriteModule{}, false
}

var jwDefaultTrunkRoots = []string{"deploy", "gateway", "internal", "pkg", "services", "tools", "terraform"}

// AuditJournaledWrites — гейт УК3-27. Находки возвращаются значением; перепись
// печатается в log.
func AuditJournaledWrites(opts JournaledWriteOptions, log io.Writer) ([]JournaledWriteFinding, JournaledWriteCensus, error) {
	var (
		findings []JournaledWriteFinding
		census   JournaledWriteCensus
	)
	pairs := opts.Pairs
	if pairs == nil {
		pairs = JournaledComponentPairs
	}
	modules, err := jwDiscoverModules(opts.Root)
	if err != nil {
		return nil, census, err
	}
	for _, name := range modules {
		m, fs, err := jwAuditModule(opts.Root, name, pairs)
		if err != nil {
			return nil, census, err
		}
		census.Modules = append(census.Modules, m)
		findings = append(findings, fs...)
	}
	// (д) — у каждой пары ровно один вызов в её модуле.
	for _, p := range pairs {
		m, ok := census.Module(p.Module)
		if !ok {
			findings = append(findings, JournaledWriteFinding{Rule: JWRuleComponent, Module: p.Module, Pos: "—",
				Detail: fmt.Sprintf("модуля пары (%s, %s) в дереве нет", p.Service, p.Role)})
			continue
		}
		n := 0
		for _, c := range m.AsComponent {
			if strings.HasSuffix(c, fmt.Sprintf("(%s, %s)", p.Service, p.Role)) {
				n++
			}
		}
		if n != 1 {
			findings = append(findings, JournaledWriteFinding{Rule: JWRuleComponent, Module: p.Module, Pos: "—",
				Detail: fmt.Sprintf("вызовов AsComponent пары (%s, %s): %d, ожидается ровно 1 — фоновый путь без личности компонента получит отказ journaltx.Begin", p.Service, p.Role, n)})
		}
	}
	trunk := opts.TrunkRoots
	if trunk == nil {
		trunk = jwDefaultTrunkRoots
	}
	tf, sc, fs, err := jwAuditTrunkSessionSettings(opts.Root, trunk)
	if err != nil {
		return nil, census, err
	}
	census.TrunkFiles, census.SetConfigCalls = tf, sc
	findings = append(findings, fs...)

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Rule != findings[j].Rule {
			return findings[i].Rule < findings[j].Rule
		}
		return findings[i].Pos < findings[j].Pos
	})
	jwPrintCensus(log, census, findings)
	return findings, census, nil
}

// JournaledWritePremiseFailures — причины, по которым перепись беспредметна.
func JournaledWritePremiseFailures(c JournaledWriteCensus) []string {
	var out []string
	if len(c.Modules) == 0 {
		out = append(out, "модулей с журналом подписки не найдено — обход пуст")
	}
	for _, m := range c.Modules {
		if m.GoFiles == 0 {
			out = append(out, fmt.Sprintf("%s: не прочитано ни одного Go-файла", m.Name))
		}
		if m.Journal == "" {
			out = append(out, fmt.Sprintf("%s: имя журнала не установлено", m.Name))
		}
		if m.HelperBegins == 0 && m.BeginCalls == 0 && m.PoolStatements == 0 {
			out = append(out, fmt.Sprintf("%s: распознаватель не нашёл ни одного открытия транзакции и ни одного оператора на пуле — он мёртв", m.Name))
		}
	}
	if c.TrunkFiles == 0 {
		out = append(out, "правило (г): ствол не прочитан")
	}
	return out
}

func jwPrintCensus(log io.Writer, c JournaledWriteCensus, findings []JournaledWriteFinding) {
	if log == nil {
		return
	}
	_, _ = fmt.Fprintf(log, "УК3-27 перепись: модулей %d\n", len(c.Modules))
	for _, m := range c.Modules {
		_, _ = fmt.Fprintf(log, "  %s: журнал %s; журналируемых таблиц %d [%s]; Go-файлов %d, SQL-файлов %d\n",
			m.Name, m.Journal, len(m.JournaledTables), strings.Join(m.JournaledTables, ", "), m.GoFiles, m.SQLFiles)
		_, _ = fmt.Fprintf(log, "    (а) помощником journaltx.Begin %d; прочих открытий транзакции %d: на пуле %d, из них ReadOnly %d; точек сохранения %d; получатель не установлен %d\n",
			m.HelperBegins, m.BeginCalls, m.BeginOnPool, m.BeginReadOnly, m.BeginOnTx, m.BeginUnresolved)
		_, _ = fmt.Fprintf(log, "    (б) операторов на пуле %d, из них пишущих %d; пул передан внешней функции %d\n",
			m.PoolStatements, m.PoolWrites, m.PoolToExternal)
		_, _ = fmt.Fprintf(log, "    (д) вызывающих AsComponent %d [%s]\n", len(m.AsComponent), strings.Join(m.AsComponent, "; "))
	}
	_, _ = fmt.Fprintf(log, "  (г) файлов ствола %d, вызовов set_config %d\n", c.TrunkFiles, c.SetConfigCalls)
	byRule := map[string]int{}
	for _, f := range findings {
		byRule[f.Rule]++
	}
	_, _ = fmt.Fprintf(log, "  находок %d: (а) %d · (б) %d · (в) %d · (г) %d · (д) %d\n", len(findings),
		byRule[JWRuleBegin], byRule[JWRulePoolWrite], byRule[JWRuleNamesColumn], byRule[JWRuleSessionSet], byRule[JWRuleComponent])
}

// ── модули и журналируемые таблицы ──────────────────────────────────────────

func jwDiscoverModules(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "services", e.Name(), "internal", "subscriptionjournal")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func jwJournalTable(root, module string) (string, error) {
	dir := filepath.Join(root, "services", module, "internal", "subscriptionjournal")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return "", err
		}
		if t, ok := topLevelStringDecls(f)["Table"]; ok {
			return t, nil
		}
	}
	return "", nil
}

var (
	jwFuncDeclRe = regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([\w."]+)\s*\(`)
	jwTriggerRe  = regexp.MustCompile(`(?is)CREATE\s+(?:CONSTRAINT\s+)?TRIGGER\s+[\w"]+\s+(?:AFTER|BEFORE|INSTEAD\s+OF)\b[^;]*?\bON\s+([\w."]+)[^;]*?EXECUTE\s+(?:FUNCTION|PROCEDURE)\s+([\w."]+)\s*\(`)
	jwDollarRe   = regexp.MustCompile(`\$[A-Za-z_]*\$`)
	jwSQLComment = regexp.MustCompile(`--[^\n]*`)
)

// jwJournaledTables — журнал и таблицы с триггером функции, пишущей журнал.
func jwJournaledTables(root, module, journal string) ([]string, int, error) {
	dir := filepath.Join(root, "services", module, "internal", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, 0, err
	}
	journalBare := bareTable(journal)
	set := map[string]bool{journalBare: true}
	journaling := map[string]bool{}
	type trig struct{ table, fn string }
	var trigs []trig
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		n++
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, 0, err
		}
		src := jwSQLComment.ReplaceAllString(string(raw), "")
		for _, loc := range jwFuncDeclRe.FindAllStringSubmatchIndex(src, -1) {
			name := bareTable(src[loc[2]:loc[3]])
			body := jwFunctionBody(src[loc[1]:])
			for _, t := range jwWriteTargets(body) {
				if t.verb == "INSERT" && bareTable(t.table) == journalBare {
					journaling[name] = true
				}
			}
		}
		for _, m := range jwTriggerRe.FindAllStringSubmatch(src, -1) {
			trigs = append(trigs, trig{table: bareTable(m[1]), fn: bareTable(m[2])})
		}
	}
	for _, t := range trigs {
		if journaling[t.fn] {
			set[t.table] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, n, nil
}

// jwFunctionBody — тело функции между первой парой долларовых кавычек.
func jwFunctionBody(rest string) string {
	loc := jwDollarRe.FindStringIndex(rest)
	if loc == nil {
		return ""
	}
	tag := rest[loc[0]:loc[1]]
	after := rest[loc[1]:]
	end := strings.Index(after, tag)
	if end < 0 {
		return after
	}
	return after[:end]
}

// ── SQL: цели записи ────────────────────────────────────────────────────────

type jwWriteTarget struct{ verb, table string }

var (
	jwWriteRe       = regexp.MustCompile(`(?i)\b(INSERT\s+INTO|UPDATE|DELETE\s+FROM|MERGE\s+INTO)\s+(?:ONLY\s+)?([%\w."]+)`)
	jwNotWriteVerbs = regexp.MustCompile(`(?i)\b(DO\s+UPDATE|FOR\s+(?:NO\s+KEY\s+)?UPDATE|ON\s+UPDATE|ON\s+DELETE|OR\s+UPDATE|OR\s+DELETE|AFTER\s+UPDATE|BEFORE\s+UPDATE|AFTER\s+DELETE|BEFORE\s+DELETE|UPDATE\s+OF)\b`)
)

func jwWriteTargets(sql string) []jwWriteTarget {
	clean := jwNotWriteVerbs.ReplaceAllStringFunc(sql, func(s string) string { return strings.Repeat(" ", len(s)) })
	var out []jwWriteTarget
	for _, m := range jwWriteRe.FindAllStringSubmatch(clean, -1) {
		verb := strings.ToUpper(strings.Fields(m[1])[0])
		out = append(out, jwWriteTarget{verb: verb, table: m[2]})
	}
	return out
}

// ── Go: разбор модуля ───────────────────────────────────────────────────────

type jwPkg struct {
	files   []*ast.File
	rels    []string
	consts  map[string]string
	structs map[string]map[string]string // тип → поле → тип поля
	embeds  map[string][]string          // тип → встроенные типы пакета (продвижение полей)
	fields  map[string]map[string]bool   // поле → множество типов (по пакету)
}

// jwFieldType — тип поля name структуры owner с продвижением через встроенные
// структуры пакета (поиск в ширину, как правило выбора Go: ближайший уровень
// побеждает; два одноимённых поля на одном уровне — неоднозначность, ""). ok=false —
// поле не найдено.
func (pk *jwPkg) jwFieldType(owner, name string) (string, bool) {
	level := []string{owner}
	seen := map[string]bool{owner: true}
	for depth := 0; depth < 4 && len(level) > 0; depth++ {
		found := map[string]bool{}
		var next []string
		for _, t := range level {
			if fm, ok := pk.structs[t]; ok {
				if ft, ok := fm[name]; ok {
					found[ft] = true
				}
			}
			for _, e := range pk.embeds[t] {
				if !seen[e] {
					seen[e] = true
					next = append(next, e)
				}
			}
		}
		switch len(found) {
		case 0:
			level = next
			continue
		case 1:
			for ft := range found {
				return ft, true
			}
		}
		return "", true
	}
	return "", false
}

func jwIsPool(t string) bool {
	return strings.HasSuffix(t, "pgxpool.Pool") || strings.HasSuffix(t, "pgx.Conn") || strings.HasSuffix(t, "pgxpool.Conn")
}

func jwIsTx(t string) bool {
	t = strings.TrimPrefix(t, "*")
	return t == "pgx.Tx" || t == "journaltx.Tx"
}

func jwAuditModule(root, name string, pairs []JournaledComponentPair) (JournaledWriteModule, []JournaledWriteFinding, error) {
	m := JournaledWriteModule{Name: name}
	var findings []JournaledWriteFinding
	journal, err := jwJournalTable(root, name)
	if err != nil {
		return m, nil, err
	}
	m.Journal = journal
	tables, nsql, err := jwJournaledTables(root, name, journal)
	if err != nil {
		return m, nil, err
	}
	m.JournaledTables, m.SQLFiles = tables, nsql
	journaled := map[string]bool{}
	for _, t := range tables {
		journaled[t] = true
	}

	// Пакеты модуля.
	pkgs := map[string]*jwPkg{}
	fset := token.NewFileSet()
	base := filepath.Join(root, "services", name)
	err = filepath.WalkDir(base, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, p)
		dir := filepath.Dir(p)
		pk := pkgs[dir]
		if pk == nil {
			pk = &jwPkg{consts: map[string]string{}, structs: map[string]map[string]string{}, embeds: map[string][]string{}, fields: map[string]map[string]bool{}}
			pkgs[dir] = pk
		}
		pk.files = append(pk.files, f)
		pk.rels = append(pk.rels, filepath.ToSlash(rel))
		m.GoFiles++
		return nil
	})
	if err != nil {
		return m, nil, err
	}
	dirs := make([]string, 0, len(pkgs))
	for d := range pkgs {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		pk := pkgs[d]
		for _, f := range pk.files {
			for k, v := range topLevelStringDecls(f) {
				pk.consts[k] = v
			}
			jwCollectConstConcat(f, pk.consts)
			for _, decl := range f.Decls {
				switch dd := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range dd.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						st, ok := ts.Type.(*ast.StructType)
						if !ok {
							continue
						}
						fm := map[string]string{}
						for _, fld := range st.Fields.List {
							typ := types.ExprString(fld.Type)
							if len(fld.Names) == 0 {
								// Встроенная структура пакета: её поля продвигаются
								// (jwFieldType). Встроенный тип другого пакета разбор не
								// раскрывает — его поля остаются неустановленными.
								if et := strings.TrimPrefix(typ, "*"); !strings.Contains(et, ".") {
									pk.embeds[ts.Name.Name] = append(pk.embeds[ts.Name.Name], et)
								}
							}
							for _, n := range fld.Names {
								fm[n.Name] = typ
								if pk.fields[n.Name] == nil {
									pk.fields[n.Name] = map[string]bool{}
								}
								pk.fields[n.Name][typ] = true
							}
						}
						pk.structs[ts.Name.Name] = fm
					}
				}
			}
		}
		for i, f := range pk.files {
			fs := jwAuditFile(fset, pk, f, pk.rels[i], name, journaled, pairs, &m)
			findings = append(findings, fs...)
		}
	}
	return m, findings, nil
}

// jwCollectConstConcat — строковые константы, собранные сложением констант и литералов.
func jwCollectConstConcat(f *ast.File, consts map[string]string) {
	for pass := 0; pass < 3; pass++ {
		for _, d := range f.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if _, done := consts[n.Name]; done {
						continue
					}
					if s, ok := jwConstString(vs.Values[i], consts); ok {
						consts[n.Name] = s
					}
				}
			}
		}
	}
}

func jwConstString(e ast.Expr, consts map[string]string) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[x.Name]
		return s, ok
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok1 := jwConstString(x.X, consts)
		r, ok2 := jwConstString(x.Y, consts)
		return l + r, ok1 && ok2
	case *ast.ParenExpr:
		return jwConstString(x.X, consts)
	}
	return "", false
}

// jwScope — что известно о локальных именах функции.
type jwScope struct {
	recvName, recvType string
	types              map[string]string     // имя → объявленный тип
	assigns            map[string][]ast.Expr // имя → правые части присваиваний
}

func jwFuncScope(fn *ast.FuncDecl) *jwScope {
	sc := &jwScope{types: map[string]string{}, assigns: map[string][]ast.Expr{}}
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		rt := strings.TrimPrefix(types.ExprString(fn.Recv.List[0].Type), "*")
		if i := strings.IndexByte(rt, '['); i >= 0 {
			rt = rt[:i]
		}
		sc.recvType = rt
		for _, n := range fn.Recv.List[0].Names {
			sc.recvName = n.Name
		}
	}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			t := types.ExprString(f.Type)
			for _, n := range f.Names {
				sc.types[n.Name] = t
			}
		}
	}
	addFields(fn.Type.Params)
	if fn.Body == nil {
		return sc
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			addFields(x.Type.Params)
		case *ast.ValueSpec:
			for i, nm := range x.Names {
				if x.Type != nil {
					sc.types[nm.Name] = types.ExprString(x.Type)
				}
				if i < len(x.Values) {
					sc.assigns[nm.Name] = append(sc.assigns[nm.Name], x.Values[i])
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) == len(x.Rhs) {
				for i, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						sc.assigns[id.Name] = append(sc.assigns[id.Name], x.Rhs[i])
					}
				}
			} else if len(x.Rhs) == 1 {
				// a, err := f(...) — первое имя получает результат вызова.
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					sc.assigns[id.Name] = append(sc.assigns[id.Name], x.Rhs[0])
				}
			}
		}
		return true
	})
	return sc
}

// jwTypeOf — тип выражения-получателя: "" если не установлен.
func jwTypeOf(e ast.Expr, sc *jwScope, pk *jwPkg, depth int) string {
	if depth > 4 {
		return ""
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return jwTypeOf(x.X, sc, pk, depth+1)
	case *ast.Ident:
		if t, ok := sc.types[x.Name]; ok {
			return t
		}
		if as := sc.assigns[x.Name]; len(as) == 1 {
			return jwTypeOf(as[0], sc, pk, depth+1)
		}
		return ""
	case *ast.SelectorExpr:
		owner := ""
		if id, ok := x.X.(*ast.Ident); ok && id.Name == sc.recvName {
			owner = sc.recvType
		} else {
			owner = strings.TrimPrefix(jwTypeOf(x.X, sc, pk, depth+1), "*")
		}
		if t, ok := pk.jwFieldType(owner, x.Sel.Name); ok {
			return t
		}
		if ts := pk.fields[x.Sel.Name]; len(ts) == 1 {
			for t := range ts {
				return t
			}
		}
		return ""
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			switch sel.Sel.Name {
			case "NewPool", "NewWithConfig":
				return "*pgxpool.Pool"
			case "Begin", "BeginTx":
				// Результат открытия транзакции — транзакция: помощника либо pgx.
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "journaltx" {
					return "*journaltx.Tx"
				}
				return "pgx.Tx"
			}
		}
		return ""
	case *ast.UnaryExpr:
		return jwTypeOf(x.X, sc, pk, depth+1)
	}
	return ""
}

// jwSQLText — текст SQL выражения; ok=false — не установлен.
func jwSQLText(e ast.Expr, sc *jwScope, pk *jwPkg, depth int) (string, bool) {
	if depth > 4 {
		return "", false
	}
	switch x := e.(type) {
	case *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr:
		if s, ok := jwConstString(x, pk.consts); ok {
			return s, true
		}
		if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			// Неустановленная часть сложения читается подстановкой `%s`: SQL
			// остаётся разобранным, а имя таблицы на её месте — «не установлено».
			l, ok1 := jwSQLText(b.X, sc, pk, depth+1)
			if !ok1 {
				l = "%s"
			}
			r, ok2 := jwSQLText(b.Y, sc, pk, depth+1)
			if !ok2 {
				r = "%s"
			}
			return l + r, ok1 || ok2
		}
		if p, ok := x.(*ast.ParenExpr); ok {
			return jwSQLText(p.X, sc, pk, depth+1)
		}
		return "", false
	case *ast.Ident:
		if s, ok := pk.consts[x.Name]; ok {
			return s, true
		}
		// Локальная переменная: каждое её присваивание обязано разобраться —
		// оператор судится по объединению всех ветвей.
		as := sc.assigns[x.Name]
		if len(as) == 0 {
			return "", false
		}
		parts := make([]string, 0, len(as))
		for _, a := range as {
			t, ok := jwSQLText(a, sc, pk, depth+1)
			if !ok {
				return "", false
			}
			parts = append(parts, t)
		}
		return strings.Join(parts, "\n;\n"), true
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "fmt" && sel.Sel.Name == "Sprintf" && len(x.Args) > 0 {
				// Формат — текст; глаголы подстановки остаются `%s` и читаются как
				// «не установлено» там, где стоят на месте имени таблицы.
				return jwSQLText(x.Args[0], sc, pk, depth+1)
			}
		}
		return "", false
	}
	return "", false
}

func jwReadOnlyOpts(e ast.Expr) bool {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "AccessMode" {
			if strings.HasSuffix(types.ExprString(kv.Value), "ReadOnly") {
				return true
			}
		}
	}
	return false
}

func jwImportName(f *ast.File, path string) string {
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if p != path {
			continue
		}
		if im.Name != nil {
			return im.Name.Name
		}
		return jwPackageNameOf(p)
	}
	return ""
}

var jwMajorSuffix = regexp.MustCompile(`^v[0-9]+$`)

// jwPackageNameOf — имя пакета по пути импорта: последний элемент, а у пути с
// суффиксом мажорной версии (`…/pgx/v5`) — предыдущий.
func jwPackageNameOf(p string) string {
	parts := strings.Split(p, "/")
	last := parts[len(parts)-1]
	if jwMajorSuffix.MatchString(last) && len(parts) > 1 {
		return parts[len(parts)-2]
	}
	return last
}

const jwJournaltxPath = "github.com/PRO-Robotech/corelib/journaltx"

func jwAuditFile(fset *token.FileSet, pk *jwPkg, f *ast.File, rel, module string,
	journaled map[string]bool, pairs []JournaledComponentPair, m *JournaledWriteModule) []JournaledWriteFinding {
	var out []JournaledWriteFinding
	add := func(rule string, p token.Pos, detail string) {
		out = append(out, JournaledWriteFinding{Rule: rule, Module: module, Pos: posOf(fset, rel, p), Detail: detail})
	}
	pgxName := jwImportName(f, "github.com/jackc/pgx/v5")
	jtxName := jwImportName(f, jwJournaltxPath)
	journalBare := bareTable(m.Journal)

	// (в) — литералы, вставляющие в журнал с колонкой initiator.
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if jwInsertNamesInitiator(s, journalBare) {
			add(JWRuleNamesColumn, lit.Pos(), "оператор вставки в журнал называет колонку initiator; производитель значения один — умолчание колонки")
		}
		return true
	})

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		sc := jwFuncScope(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			method := sel.Sel.Name
			pkgIdent, _ := sel.X.(*ast.Ident)

			// (д) journaltx.AsComponent
			if jtxName != "" && pkgIdent != nil && pkgIdent.Name == jtxName && method == "AsComponent" {
				svc, ok1 := "", false
				role, ok2 := "", false
				if len(call.Args) == 3 {
					svc, ok1 = jwConstString(call.Args[1], pk.consts)
					role, ok2 = jwConstString(call.Args[2], pk.consts)
				}
				if !ok1 || !ok2 {
					m.AsComponent = append(m.AsComponent, posOf(fset, rel, call.Pos())+" (?, ?)")
					add(JWRuleComponent, call.Pos(), "пара AsComponent не установлена: служба и роль — не литерал и не константа")
					return true
				}
				m.AsComponent = append(m.AsComponent, posOf(fset, rel, call.Pos())+fmt.Sprintf(" (%s, %s)", svc, role))
				lawful := false
				for _, p := range pairs {
					if p.Module == module && p.Service == svc && p.Role == role {
						lawful = true
					}
				}
				if !lawful {
					add(JWRuleComponent, call.Pos(), fmt.Sprintf("AsComponent(%s, %s) вне пар §8 замысла для модуля %s", svc, role, module))
				}
				return true
			}

			// (а) pgx.BeginFunc / pgx.BeginTxFunc — пакетные функции.
			if pgxName != "" && pkgIdent != nil && pkgIdent.Name == pgxName && (method == "BeginFunc" || method == "BeginTxFunc") {
				m.BeginCalls++
				if len(call.Args) < 2 {
					return true
				}
				t := jwTypeOf(call.Args[1], sc, pk, 0)
				ro := method == "BeginTxFunc" && len(call.Args) >= 3 && jwReadOnlyOpts(call.Args[2])
				jwJudgeBegin(t, ro, method, call, m, add)
				return true
			}

			// (а) X.Begin / X.BeginTx / X.BeginFunc / X.BeginTxFunc.
			switch method {
			case "Begin", "BeginTx", "BeginFunc", "BeginTxFunc":
				if pkgIdent != nil && pkgIdent.Name == jtxName {
					if method == "Begin" {
						m.HelperBegins++
					}
					return true
				}
				if pkgIdent != nil && pkgIdent.Name == pgxName {
					return true // pgx.BeginFunc / pgx.BeginTxFunc разобраны выше
				}
				m.BeginCalls++
				t := jwTypeOf(sel.X, sc, pk, 0)
				ro := (method == "BeginTx" || method == "BeginTxFunc") && len(call.Args) >= 2 && jwReadOnlyOpts(call.Args[1])
				jwJudgeBegin(t, ro, method, call, m, add)
				return true
			case "Exec", "Query", "QueryRow":
				t := jwTypeOf(sel.X, sc, pk, 0)
				if !jwIsPool(t) {
					return true
				}
				m.PoolStatements++
				if len(call.Args) < 2 {
					return true
				}
				text, ok := jwSQLText(call.Args[1], sc, pk, 0)
				if !ok {
					add(JWRulePoolWrite, call.Pos(), fmt.Sprintf("%s на пуле: текст оператора не установлен разбором", method))
					return true
				}
				targets := jwWriteTargets(text)
				if len(targets) == 0 {
					return true
				}
				m.PoolWrites++
				for _, tg := range targets {
					if strings.Contains(bareTable(tg.table), "%") {
						add(JWRulePoolWrite, call.Pos(), fmt.Sprintf("%s на пуле: %s в таблицу, не установленную разбором (%s) — автокоммитная запись мимо помощника", method, tg.verb, tg.table))
						continue
					}
					if journaled[bareTable(tg.table)] {
						add(JWRulePoolWrite, call.Pos(), fmt.Sprintf("%s на пуле: %s в журналируемую таблицу %s — автокоммитная запись мимо помощника", method, tg.verb, bareTable(tg.table)))
					}
				}
				return true
			}

			// Пул, переданный внешней функции (граница 2): пакет импорта вне дерева.
			if pkgIdent != nil && jwExternalImport(f, pkgIdent.Name) {
				for _, a := range call.Args {
					if jwIsPool(jwTypeOf(a, sc, pk, 0)) {
						m.PoolToExternal++
						break
					}
				}
			}
			return true
		})
	}
	return out
}

func jwJudgeBegin(t string, readOnly bool, method string, call *ast.CallExpr, m *JournaledWriteModule,
	add func(rule string, p token.Pos, detail string)) {
	switch {
	case jwIsTx(t):
		m.BeginOnTx++
	case jwIsPool(t):
		m.BeginOnPool++
		if readOnly {
			m.BeginReadOnly++
			return
		}
		add(JWRuleBegin, call.Pos(), fmt.Sprintf("%s на %s — транзакция открыта мимо journaltx.Begin", method, t))
	default:
		m.BeginUnresolved++
		add(JWRuleBegin, call.Pos(), fmt.Sprintf("%s: тип получателя не установлен разбором — открытие транзакции не осмотрено", method))
	}
}

var jwInsertColsRe = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+([\w."]+)\s*\(([^)]*)\)`)

func jwInsertNamesInitiator(sql, journalBare string) bool {
	for _, m := range jwInsertColsRe.FindAllStringSubmatch(sql, -1) {
		if bareTable(m[1]) != journalBare {
			continue
		}
		for _, c := range strings.Split(m[2], ",") {
			if strings.EqualFold(strings.Trim(strings.TrimSpace(c), `"`), "initiator") {
				return true
			}
		}
	}
	return false
}

// ── (г) сессионная настройка инициатора в стволе ────────────────────────────

const jwSetting = "kacho_journal.initiator"

var (
	jwSetConfigRe   = regexp.MustCompile(`(?is)\bset_config\s*\(\s*([^,]+?)\s*,[^;]*?,\s*(true|false)\s*\)`)
	jwSetStmtRe     = regexp.MustCompile(`(?is)\bSET\s+(LOCAL\s+|SESSION\s+)?` + regexp.QuoteMeta(jwSetting) + `\b`)
	jwStartOptionRe = regexp.MustCompile(`-c\s*` + regexp.QuoteMeta(jwSetting) + `\s*=`)
)

func jwAuditTrunkSessionSettings(root string, roots []string) (files, setConfigs int, out []JournaledWriteFinding, err error) {
	for _, r := range roots {
		base := filepath.Join(root, r)
		if _, serr := os.Stat(base); serr != nil {
			continue
		}
		werr := filepath.WalkDir(base, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			isGo := strings.HasSuffix(p, ".go")
			isSQL := strings.HasSuffix(p, ".sql")
			if !isGo && !isSQL {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			files++
			var texts []jwText
			if isGo {
				fset := token.NewFileSet()
				f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
				if perr != nil {
					return nil // не-Go-код в .go (шаблоны) правилом не судится
				}
				ast.Inspect(f, func(n ast.Node) bool {
					if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
							texts = append(texts, jwText{pos: posOf(fset, rel, lit.Pos()), s: s})
						}
					}
					return true
				})
			} else {
				raw, rerr := os.ReadFile(p)
				if rerr != nil {
					return rerr
				}
				texts = append(texts, jwText{pos: rel, s: jwSQLComment.ReplaceAllString(string(raw), "")})
			}
			module := serviceOf(rel)
			isTest := strings.HasSuffix(p, "_test.go")
			for _, t := range texts {
				for _, sm := range jwSetConfigRe.FindAllStringSubmatch(t.s, -1) {
					setConfigs++
					name := strings.TrimSpace(sm[1])
					local := strings.EqualFold(sm[2], "true")
					if local {
						continue
					}
					switch {
					case strings.Trim(name, `'`) == jwSetting:
						out = append(out, JournaledWriteFinding{Rule: JWRuleSessionSet, Module: module, Pos: t.pos,
							Detail: "set_config над " + jwSetting + " с третьим аргументом false — сессионная настройка инициатора"})
					case !strings.HasPrefix(name, "'"):
						out = append(out, JournaledWriteFinding{Rule: JWRuleSessionSet, Module: module, Pos: t.pos,
							Detail: "set_config с нелитеральным именем настройки и третьим аргументом false — сессионная установка не осмотрена"})
					}
				}
				for _, sm := range jwSetStmtRe.FindAllStringSubmatch(t.s, -1) {
					if !strings.EqualFold(strings.TrimSpace(sm[1]), "LOCAL") {
						out = append(out, JournaledWriteFinding{Rule: JWRuleSessionSet, Module: module, Pos: t.pos,
							Detail: "SET " + jwSetting + " без LOCAL — сессионная настройка инициатора"})
					}
				}
				if !isTest && jwStartOptionRe.MatchString(t.s) {
					out = append(out, JournaledWriteFinding{Rule: JWRuleSessionSet, Module: module, Pos: t.pos,
						Detail: "параметр старта сессии -c " + jwSetting + "= — сессионная настройка инициатора"})
				}
			}
			return nil
		})
		if werr != nil {
			return 0, 0, nil, werr
		}
	}
	return files, setConfigs, out, nil
}

type jwText struct{ pos, s string }

// jwExternalImport — имя name в файле f — импорт пакета вне этого дерева.
func jwExternalImport(f *ast.File, name string) bool {
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		n := jwPackageNameOf(p)
		if im.Name != nil {
			n = im.Name.Name
		}
		if n == name {
			return !strings.HasPrefix(p, "github.com/PRO-Robotech/kacho/")
		}
	}
	return false
}
