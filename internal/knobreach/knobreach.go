// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package knobreach отвечает на один вопрос: ДОЕЗЖАЕТ ли имя переменной
// окружения, которое задаёт проба, до конфигурации, которую строит загрузчик
// службы.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Имя, которого разбор конфигурации не знает, не отказывает и не
// предупреждает: величина остаётся умолчанием, и проба утверждает про
// конфигурацию, которой процесс не получал. Так пробы носителя задавали
// эфемерный порт именем, которого не читало ничто, — и занимали 9090/9091
// (kacho#2678); так же отрицание «ребра нет» обнуляло один адрес из двух.
//
// Загрузчиков в дереве ДВА вида, и правила подстановки у них разные:
// envconfig-разбор (`corelib/config.LoadPrefixed`) берёт имя из тега поля, а
// viper выводит его из ключа заменой разделителей — и замена у служб разная
// (у одной дефис в ключе доезжает до имени как есть, у другой становится
// подчёркиванием). Поэтому класс не виден ни одним образцом имени: судить его
// можно только ВОПРОСОМ К САМОМУ ЗАГРУЗЧИКУ (kacho#2737).
//
// ─────────────────────────────────────────────────────────────────────────────
// КАК СПРАШИВАЕТСЯ
//
// Имя снимается с переменной, конфигурация загружается — это исход «без
// имени». Затем имени по очереди даются значения разных видов (строка-часовой,
// числа, логические, длительность, адрес, пустое), и после каждого
// конфигурация загружается снова. Имя ДОЕЗЖАЕТ, если хоть одно значение меняет
// исход: загруженную конфигурацию либо текст отказа загрузчика. Значений
// несколько потому, что поле типизировано: число не примет строки-часового
// (и это тоже смена исхода — отказ разбора), а логическое, чьё умолчание
// «ложь», не изменится от «ложь».
//
// Исходов у вопроса ТРИ, и они различимы: доезжает · не доезжает · не
// определено (загрузчик отказывает одинаково при любом значении — вопрос не
// задан, и молчание здесь было бы ложным «не читается»).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ «ИМЕНЕМ, КОТОРОЕ ЗАДАЁТ ПРОБА»
//
// Строковый литерал вида `KACHO_…` в позиции ЗАДАНИЯ: первый аргумент вызова
// `Setenv`, ключ литерала карты, индекс карты в левой части присваивания. В
// прочих позициях (ожидаемый текст отказа, перечень имён, которые обязан
// назвать рендер) литерал — УПОМИНАНИЕ: он считается в переписи отдельно и не
// судится, потому что проба его не задаёт.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВТОРАЯ СОВОКУПНОСТЬ: ИМЕНА, КОТОРЫЕ ТЕКСТ ПРОЦЕССА НАЗЫВАЕТ ОПЕРАТОРУ
//
// Текст отказа старта называет действие, которым отказ снимается. Если он
// называет форму имени, которой разбор не читает, оператор, скопировавший имя
// из отказа, получает то же отсутствие величины, из-за которого страж и
// отказал (kacho#2739). Вопрос к загрузчику здесь тот же самый, поэтому и
// реализация одна: `ProseNames` собирает имена из текстов не-пробного кода,
// `Judge` судит их вместе с именами проб, перепись печатает объём каждой
// совокупности отдельно.
//
// Где гейт зовут и что его зовут все: `census_test.go` этого пакета.
package knobreach

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// namePattern — форма имени ручки платформы (`KACHO_<DOMAIN>_<NAME>`). Дефис
// в классе НЕ опечатка: у одного из загрузчиков он законная часть имени.
var namePattern = regexp.MustCompile(`^KACHO_[A-Za-z0-9_-]+$`)

// Setting — имя, которое проба ЗАДАЁТ, с координатой.
type Setting struct {
	Name string
	File string
	Line int
	Form string // setenv | map-key | index-assign
}

// Census — что прочитано в пробах одного пакета.
type Census struct {
	Files    int       // прочитано файлов проб
	Settings []Setting // имена в позиции задания, по вхождениям
	Mentions int       // литералы той же формы вне позиции задания
}

// Names — уникальные имена из Settings, отсортированные.
func (c Census) Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range c.Settings {
		if !seen[s.Name] {
			seen[s.Name] = true
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}

// ProbeSettings читает ВСЕ файлы проб каталога (`*_test.go`) — ровно то
// множество, которое компилятор собирает в пробный двоичный файл пакета.
func ProbeSettings(dir string) (Census, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return Census{}, err
	}
	sort.Strings(paths)
	var c Census
	for _, p := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return Census{}, fmt.Errorf("разбор %s: %w", p, err)
		}
		c.Files++
		settings, mentions := fileSettings(fset, f, filepath.Base(p))
		c.Settings = append(c.Settings, settings...)
		c.Mentions += mentions
	}
	return c, nil
}

// fileSettings разбирает один файл. Позиция литерала определяется по его
// РОДИТЕЛЮ в синтаксическом дереве, а не по тексту строки: имя в комментарии
// или в тексте ожидаемого отказа не является заданием.
func fileSettings(fset *token.FileSet, f *ast.File, name string) ([]Setting, int) {
	parent := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			parent[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})

	var out []Setting
	mentions := 0
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !namePattern.MatchString(s) {
			return true
		}
		form := settingForm(lit, parent)
		if form == "" {
			mentions++
			return true
		}
		out = append(out, Setting{Name: s, File: name, Line: fset.Position(lit.Pos()).Line, Form: form})
		return true
	})
	return out, mentions
}

// settingForm — в какой позиции ЗАДАНИЯ стоит литерал; "" — упоминание.
func settingForm(lit *ast.BasicLit, parent map[ast.Node]ast.Node) string {
	switch p := parent[lit].(type) {
	case *ast.CallExpr:
		if len(p.Args) > 0 && p.Args[0] == lit && calleeName(p.Fun) == "Setenv" {
			return "setenv"
		}
	case *ast.KeyValueExpr:
		if p.Key != lit {
			return ""
		}
		if cl, ok := parent[p].(*ast.CompositeLit); ok && isMapLiteral(cl, parent) {
			return "map-key"
		}
	case *ast.IndexExpr:
		if p.Index != lit {
			return ""
		}
		if as, ok := parent[p].(*ast.AssignStmt); ok {
			for _, l := range as.Lhs {
				if l == p {
					return "index-assign"
				}
			}
		}
	}
	return ""
}

// isMapLiteral — литерал карты: тип назван картой явно либо опущен внутри
// литерала карты карт.
func isMapLiteral(cl *ast.CompositeLit, parent map[ast.Node]ast.Node) bool {
	if cl.Type != nil {
		_, ok := cl.Type.(*ast.MapType)
		return ok
	}
	// Тип опущен — он выводится из объемлющего литерала.
	if kv, ok := parent[cl].(*ast.KeyValueExpr); ok {
		if outer, ok := parent[kv].(*ast.CompositeLit); ok {
			if mt, ok := outer.Type.(*ast.MapType); ok {
				_, inner := mt.Value.(*ast.MapType)
				return inner
			}
		}
	}
	return false
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

// Loader строит конфигурацию службы из ТЕКУЩЕГО окружения процесса — тем же
// вызовом, что композиционный корень на старте.
type Loader func() (any, error)

// Verdict — исход вопроса об одном имени.
type Verdict int

const (
	// Reaches — хоть одно значение меняет исход загрузки.
	Reaches Verdict = iota + 1
	// DoesNotReach — ни одно значение исхода не меняет: имя не читает никто.
	DoesNotReach
	// Undetermined — загрузчик отказывает при любом значении ОДНИМ И ТЕМ ЖЕ
	// текстом: вопрос не задан. Это третья категория, а не «не читается».
	Undetermined
)

func (v Verdict) String() string {
	switch v {
	case Reaches:
		return "доезжает"
	case DoesNotReach:
		return "НЕ ДОЕЗЖАЕТ"
	case Undetermined:
		return "НЕ ОПРЕДЕЛЕНО"
	}
	return "?"
}

// probeValues — значения, которые получает имя. Виды разные намеренно: поле
// типизировано, и значение чужого вида меняет исход отказом разбора, а
// значение, совпавшее с умолчанием, не меняет ничего.
var probeValues = []string{
	"kacho-knob-reach-sentinel",
	"7",
	"0",
	"true",
	"false",
	"7s",
	"tcp://127.0.0.1:7",
	"",
}

// Resolve спрашивает загрузчик об имени. Окружение процесса меняется на время
// вопроса и возвращается в прежнее состояние — вызывающий не обязан держать
// его в порядке сам.
func Resolve(name string, load Loader) (Verdict, error) {
	prev, had := os.LookupEnv(name)
	defer func() {
		if had {
			_ = os.Setenv(name, prev)
		} else {
			_ = os.Unsetenv(name)
		}
	}()

	if err := os.Unsetenv(name); err != nil {
		return 0, err
	}
	base, baseFailed := outcome(load)
	allFailedAlike := baseFailed
	for _, v := range probeValues {
		if err := os.Setenv(name, v); err != nil {
			return 0, err
		}
		got, failed := outcome(load)
		if got != base {
			return Reaches, nil
		}
		allFailedAlike = allFailedAlike && failed
	}
	if allFailedAlike {
		return Undetermined, fmt.Errorf("загрузчик отказывает одинаково при любом значении %s: %s", name, base)
	}
	return DoesNotReach, nil
}

// outcome — исход загрузки одной строкой: снимок конфигурации либо текст
// отказа. Снимок детерминирован: указатели разыменованы, ключи карт
// упорядочены, адресов в нём нет.
func outcome(load Loader) (string, bool) {
	v, err := load()
	if err != nil {
		return "ОТКАЗ: " + err.Error(), true
	}
	var b strings.Builder
	dump(&b, reflect.ValueOf(v), map[uintptr]bool{}, 0)
	return b.String(), false
}

func dump(b *strings.Builder, v reflect.Value, seen map[uintptr]bool, depth int) {
	if depth > 64 {
		b.WriteString("…")
		return
	}
	if !v.IsValid() {
		b.WriteString("<nil>")
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		if seen[v.Pointer()] {
			b.WriteString("<cycle>")
			return
		}
		seen[v.Pointer()] = true
		b.WriteString("&")
		dump(b, v.Elem(), seen, depth+1)
		delete(seen, v.Pointer())
	case reflect.Interface:
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		dump(b, v.Elem(), seen, depth+1)
	case reflect.Struct:
		b.WriteString(v.Type().String())
		b.WriteString("{")
		for i := 0; i < v.NumField(); i++ {
			b.WriteString(v.Type().Field(i).Name)
			b.WriteString(":")
			dump(b, v.Field(i), seen, depth+1)
			b.WriteString(" ")
		}
		b.WriteString("}")
	case reflect.Map:
		if v.IsNil() {
			b.WriteString("map(nil)")
			return
		}
		keys := v.MapKeys()
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			var kb, vb strings.Builder
			dump(&kb, k, seen, depth+1)
			dump(&vb, v.MapIndex(k), seen, depth+1)
			parts = append(parts, kb.String()+"="+vb.String())
		}
		sort.Strings(parts)
		b.WriteString("map[" + strings.Join(parts, " ") + "]")
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			b.WriteString("[](nil)")
			return
		}
		b.WriteString("[")
		for i := 0; i < v.Len(); i++ {
			dump(b, v.Index(i), seen, depth+1)
			b.WriteString(" ")
		}
		b.WriteString("]")
	case reflect.String:
		b.WriteString(strconv.Quote(v.String()))
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		b.WriteString(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		b.WriteString(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	case reflect.Complex64, reflect.Complex128:
		fmt.Fprint(b, v.Complex())
	default:
		// Функции, каналы, небезопасные указатели: их значение — адрес, и в
		// снимок он не идёт, иначе два одинаковых исхода различались бы.
		b.WriteString("<" + v.Kind().String() + ">")
	}
}

// ProseCensus — что прочитано в НЕ-пробном коде: имена, которые тексты
// процесса называют оператору (отказы старта, изъятия, подсказки).
type ProseCensus struct {
	Files   int       // прочитано файлов
	Names   []Setting // имена в тексте, по вхождениям
	Skipped int       // приставки (`KACHO_X_*`, `KACHO_X_%s`) и литералы-имена целиком
}

// proseNamePattern — имя внутри текста. Последний знак — буква или цифра:
// разделитель в конце означает приставку, а не имя.
var proseNamePattern = regexp.MustCompile(`KACHO_[A-Za-z0-9_-]*[A-Za-z0-9]`)

// ProseNames читает НЕ-пробные файлы Go каталогов и собирает имена, которые
// строковые литералы называют в тексте.
//
// Судится только ТЕКСТ — литерал, где имя стоит среди других знаков: это то,
// что оператор читает и по чему действует (kacho#2739). Литерал, целиком
// равный имени, — не текст: это либо само чтение (`BindEnv`, `Getenv`), либо
// приставка разбора, и о нём решает загрузчик, а не этот гейт. Имя, за которым
// в тексте идёт `_`, `*`, `%`, `<`, `{` или `-`, — приставка семейства имён, а
// не имя; оно считается в переписи пропущенным. Теги полей не читаются вовсе:
// это объявление имени загрузчику, то есть читатель, а не текст.
func ProseNames(dirs ...string) (ProseCensus, error) {
	var c ProseCensus
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			return ProseCensus{}, err
		}
		sort.Strings(paths)
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if err != nil {
				return ProseCensus{}, fmt.Errorf("разбор %s: %w", p, err)
			}
			c.Files++
			names, skipped := fileProse(fset, f, filepath.Join(filepath.Base(dir), filepath.Base(p)))
			c.Names = append(c.Names, names...)
			c.Skipped += skipped
		}
	}
	return c, nil
}

func fileProse(fset *token.FileSet, f *ast.File, name string) ([]Setting, int) {
	tags := map[*ast.BasicLit]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if fl, ok := n.(*ast.Field); ok && fl.Tag != nil {
			tags[fl.Tag] = true
		}
		return true
	})
	var out []Setting
	skipped := 0
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || tags[lit] {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, m := range proseNamePattern.FindAllStringIndex(s, -1) {
			if s[m[0]:m[1]] == s {
				skipped++
				continue
			}
			if m[1] < len(s) && strings.ContainsRune("_*%<{-", rune(s[m[1]])) {
				skipped++
				continue
			}
			out = append(out, Setting{Name: s[m[0]:m[1]], File: name, Line: fset.Position(lit.Pos()).Line, Form: "текст"})
		}
		return true
	})
	return out, skipped
}

// Package — пакет пробного кода и загрузчик его службы.
type Package struct {
	// Service — имя службы для переписи.
	Service string
	// Dir — каталог пакета проб; пробы гейта зовут его как ".".
	Dir string
	// ProseDirs — каталоги НЕ-пробного кода, чьи тексты называют ручки
	// оператору: сам пакет процесса и пакет его конфигурации. Пусто — тексты не
	// судятся (перепись это называет).
	ProseDirs []string
	// Base — окружение, без которого загрузчик не строит конфигурацию вовсе
	// (обязательные величины). Берётся у самих проб пакета, а не выдумывается.
	Base map[string]string
	// Load — загрузчик службы.
	Load Loader
}

// Finding — имя, которое проба задаёт либо текст называет, и которого не
// читает загрузчик.
type Finding struct {
	Setting Setting
	Verdict Verdict
	Detail  string
}

// Report — исход гейта по пакету.
type Report struct {
	Probes    Census
	Prose     ProseCensus
	Unique    int // уникальных имён по обеим совокупностям
	Reached   int
	Findings  []Finding
	NotJudged []Finding // исход «не определено»
}

// ErrNothingExamined — обход не нашёл ни одного имени в позиции задания.
// Это НЕ зелёное: «ноль находок» обязано быть отличимо от «ноль прочитанного».
var ErrNothingExamined = errors.New("НЕ ВЫПОЛНИЛОСЬ: в пробах пакета ни одного имени в позиции задания")

// ErrNoProse — названы каталоги текстов, а файлов Go в них ноль: каталог
// переехал, и совокупность текстов не прочитана вовсе.
var ErrNoProse = errors.New("НЕ ВЫПОЛНИЛОСЬ: в каталогах текстов ни одного файла Go")

// Judge — гейт без привязки к `testing`: исход возвращается значением, чтобы
// его способность упасть доказывалась пробой, а не прочтением.
//
// Окружение Base выставляет ВЫЗЫВАЮЩИЙ (проба — через t.Setenv, чтобы
// прогон вернул его на место).
func Judge(p Package) (Report, error) {
	c, err := ProbeSettings(p.Dir)
	if err != nil {
		return Report{}, err
	}
	r := Report{Probes: c}
	if len(c.Settings) == 0 {
		return r, fmt.Errorf("%w (файлов проб %d, упоминаний %d)", ErrNothingExamined, c.Files, c.Mentions)
	}
	if len(p.ProseDirs) > 0 {
		pc, err := ProseNames(p.ProseDirs...)
		if err != nil {
			return r, err
		}
		r.Prose = pc
		if pc.Files == 0 {
			return r, fmt.Errorf("%w (%s)", ErrNoProse, strings.Join(p.ProseDirs, ", "))
		}
	}

	all := append(append([]Setting{}, c.Settings...), r.Prose.Names...)
	seen := map[string]bool{}
	var names []string
	for _, s := range all {
		if !seen[s.Name] {
			seen[s.Name] = true
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	r.Unique = len(names)

	verdicts := map[string]Verdict{}
	details := map[string]string{}
	for _, n := range names {
		v, err := Resolve(n, p.Load)
		verdicts[n] = v
		if err != nil {
			details[n] = err.Error()
		}
		if v == Reaches {
			r.Reached++
		}
	}
	for _, s := range all {
		switch verdicts[s.Name] {
		case DoesNotReach:
			r.Findings = append(r.Findings, Finding{Setting: s, Verdict: DoesNotReach})
		case Undetermined:
			r.NotJudged = append(r.NotJudged, Finding{Setting: s, Verdict: Undetermined, Detail: details[s.Name]})
		}
	}
	return r, nil
}

// CensusLine — перепись осмотренного одной строкой: объём КАЖДОЙ совокупности
// отдельно, чтобы «ноль находок в текстах» было отличимо от «тексты не
// читались».
func (r Report) CensusLine(service string) string {
	return fmt.Sprintf("служба %s: файлов проб %d · заданий имён %d · упоминаний вне задания %d · "+
		"файлов текстов %d · имён в текстах %d (приставок пропущено %d) · "+
		"уникальных имён %d · доезжает %d · не доезжает %d вхождений · не определено %d вхождений",
		service, r.Probes.Files, len(r.Probes.Settings), r.Probes.Mentions,
		r.Prose.Files, len(r.Prose.Names), r.Prose.Skipped,
		r.Unique, r.Reached, len(r.Findings), len(r.NotJudged))
}

// T — то, что гейту нужно от пробы. Интерфейс, а не *testing.T: способность
// гейта упасть проверяется подставным исполнителем.
type T interface {
	Helper()
	Setenv(key, value string)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// Gate — гейт пакета: каждое имя, которое задают его пробы и называют тексты
// процесса, обязано доезжать до загрузчика службы.
func Gate(t T, p Package) {
	t.Helper()
	for k, v := range p.Base {
		t.Setenv(k, v)
	}
	r, err := Judge(p)
	if err != nil {
		t.Fatalf("гейт имён ручек, служба %s: %v", p.Service, err)
		return
	}
	t.Logf("%s", r.CensusLine(p.Service))
	for _, f := range r.Findings {
		if f.Setting.Form == "текст" {
			t.Errorf("%s:%d: текст процесса называет оператору имя %s, но загрузчик службы %s его НЕ "+
				"ЧИТАЕТ: оператор, выставивший его по тексту, получит то же отсутствие величины. Назови "+
				"в тексте форму, которую читает разбор",
				f.Setting.File, f.Setting.Line, f.Setting.Name, p.Service)
			continue
		}
		t.Errorf("%s:%d: имя %s задаётся пробой (%s), но загрузчик службы %s его НЕ ЧИТАЕТ: "+
			"ни одно значение не меняет загруженной конфигурации, и проба утверждает про величину, "+
			"которой процесс не получает. Назови ручку формой, которую читает разбор, либо сними "+
			"задание вместе с пробой, которой оно не нужно",
			f.Setting.File, f.Setting.Line, f.Setting.Name, f.Setting.Form, p.Service)
	}
	for _, f := range r.NotJudged {
		t.Errorf("%s:%d: имя %s — НЕ ОПРЕДЕЛЕНО, вопрос к загрузчику не задан: %s",
			f.Setting.File, f.Setting.Line, f.Setting.Name, f.Detail)
	}
}
