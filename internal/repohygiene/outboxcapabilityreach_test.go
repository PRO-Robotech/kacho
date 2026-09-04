// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outboxcapabilityreach_test.go — способность, которую сервис заводит, обязана
// кем-то ИСПОЛНЯТЬСЯ.
//
// # Предмет
//
// Композиционный корень сервиса конструирует компонент из `pkg/outbox/**`,
// передаёт ему настройки и запускает. Каждый экспортируемый метод такого
// компонента — объявленная способность. Способность, которую не зовёт НИКТО и до
// которой не дотягивается ни один исполняемый путь самого пакета, — это не «запас
// на будущее»: она выглядит действующим механизмом, которого нет, и тянет за
// собой живые с виду порты и адаптеры в сервисах (ban #11, LEAN).
//
// # Свойство
//
// Для каждого экспортируемого метода типа, КОНСТРУИРУЕМОГО композиционным корнем
// из `pkg/outbox/**`, выполнено хотя бы одно:
//
//	(а) метод зовут из не-тестового файла ВНЕ его пакета — способность исполняет
//	    продукт;
//	(б) метод зовут из не-тестового файла ВНУТРИ его пакета — способность
//	    является ступенью другой, исполняемой способности.
//
// Ни (а), ни (б) — способности не существует ни для кого, кроме её собственных
// проб. Это находка.
//
// # Почему критерий ДВОЙНОЙ
//
// Одного «есть вызывающий вне пакета» мало: он покраснел бы на
// `metrics.Collector.Scan` — одном проходе скана, который в продукте зовёт `Run`
// того же пакета, а снаружи только интеграционные пробы. Это законная форма, и
// гейт, роняющий её, был бы снят первым же ложным срабатыванием. На дереве
// заведения критерий (б) оставляет молчащими ТРИ разные законные формы —
// исполняемую снаружи, внутреннюю ступень и петлю-исполнитель, — и краснеет ровно
// на способностях без исполнителя. Числа печатает перепись ниже.
//
// # Чем ограничен (называю честно)
//
// Принадлежность вызова разрешается СИНТАКСИЧЕСКИ — по имени метода в файлах,
// импортирующих его пакет, — а не выводом типа. Отсюда возможен ложный НЕДОБОР
// находок: совпадение имени (`row.Scan`) засчитается как вызов и заглушит гейт.
// Ложных СРАБАТЫВАНИЙ неточность не даёт — она только добавляет вызывающих,
// никогда не убирает. Разрешение по типам потребовало бы `golang.org/x/tools`
// (в go.mod он косвенный) и загрузки типов всего дерева; цена не окупает разницы
// на этом классе.
//
// # Состав дерева берётся у ИНДЕКСА, а не у диска
//
// `ocrScan` не ходит по файловой системе сам: состав подаётся ему списком. Гейт
// по дереву берёт список у `pkg/treecorpus` (то есть у `git ls-files`),
// инъекция — обходом своего синтетического дерева, которое репозиторием не
// является и индекса не имеет. Обход диска здесь читал бы игнорируемое —
// рабочие копии агентов, распаковки чартов, отчёты прогонов, — и вердикт стал
// бы свойством рабочего каталога, а не коммита.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

const (
	// ocrOutboxRoot — семья пакетов, чьи компоненты заводит композиционный корень.
	ocrOutboxRoot = "pkg/outbox"
	// ocrModulePrefix — префикс импортов этого модуля.
	ocrModulePrefix = "github.com/PRO-Robotech/kacho/"
)

// ocrCapability — одна объявленная способность: экспортируемый метод типа,
// который конструирует композиционный корень.
type ocrCapability struct {
	Pkg    string // относительный путь пакета
	Type   string // имя типа-приёмника
	Method string // имя метода
	File   string // где объявлен
	Line   int
}

func (c ocrCapability) String() string { return c.Pkg + "." + c.Type + "." + c.Method }

// ocrResult — находки плюс перепись осмотренного: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type ocrResult struct {
	Findings      []ocrCapability
	Packages      int
	PkgFiles      int
	RootFiles     int
	ImporterFiles int
	Constructed   int
	Capabilities  int
	SilentOutside int
	SilentInside  int
}

func ocrIsTest(path string) bool { return strings.HasSuffix(path, "_test.go") }

// ocrCorpus — состав дерева, поданный извне: абсолютные пути не-тестовых .go
// файлов. Диска этот файл не касается.
type ocrCorpus struct {
	root  string
	files []string // отсортированные абсолютные пути
}

func ocrNewCorpus(root string, all []string) ocrCorpus {
	var out []string
	for _, f := range all {
		if strings.HasSuffix(f, ".go") && !ocrIsTest(f) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return ocrCorpus{root: root, files: out}
}

// rel — путь относительно корня, в слэшах.
func (c ocrCorpus) rel(abs string) string {
	r, err := filepath.Rel(c.root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(r)
}

// inDir — файлы НЕПОСРЕДСТВЕННО в каталоге (без обхода вглубь).
func (c ocrCorpus) inDir(relDir string) []string {
	var out []string
	for _, f := range c.files {
		r := c.rel(f)
		if path.Dir(r) == relDir {
			out = append(out, f)
		}
	}
	return out
}

// under — файлы под каталогом на любой глубине.
func (c ocrCorpus) under(relDir string) []string {
	var out []string
	pref := relDir + "/"
	for _, f := range c.files {
		if strings.HasPrefix(c.rel(f), pref) {
			out = append(out, f)
		}
	}
	return out
}

// subdirsOf — непосредственные подкаталоги, у которых есть свои файлы.
func (c ocrCorpus) subdirsOf(relDir string) []string {
	seen := map[string]bool{}
	pref := relDir + "/"
	for _, f := range c.files {
		r := c.rel(f)
		if !strings.HasPrefix(r, pref) {
			continue
		}
		rest := strings.TrimPrefix(r, pref)
		if i := strings.Index(rest, "/"); i > 0 {
			seen[relDir+"/"+rest[:i]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ocrReceiver — имя типа приёмника метода (без указателя), либо "".
func ocrReceiver(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	switch e := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// ocrResultTypes — экспортируемые имена типов результата функции.
func ocrResultTypes(fd *ast.FuncDecl) []string {
	if fd.Type.Results == nil {
		return nil
	}
	var out []string
	for _, f := range fd.Type.Results.List {
		e := f.Type
		if st, ok := e.(*ast.StarExpr); ok {
			e = st.X
		}
		if id, ok := e.(*ast.Ident); ok && ast.IsExported(id.Name) {
			out = append(out, id.Name)
		}
	}
	return out
}

// ocrPkgIndex — что гейту нужно знать про один пакет семьи.
type ocrPkgIndex struct {
	Ctors      map[string][]string        // экспортируемая функция -> типы результата
	Methods    map[string][]ocrCapability // тип -> экспортируемые методы
	InPkgCalls map[string]bool            // имена методов, зовомые внутри пакета
	Files      int
}

func ocrIndexPackage(c ocrCorpus, rel string) (ocrPkgIndex, error) {
	idx := ocrPkgIndex{Ctors: map[string][]string{}, Methods: map[string][]ocrCapability{}, InPkgCalls: map[string]bool{}}
	for _, path := range c.inDir(rel) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return idx, fmt.Errorf("parse %s: %w", path, err)
		}
		idx.Files++
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv := ocrReceiver(fd)
			if recv == "" {
				if ast.IsExported(fd.Name.Name) {
					if res := ocrResultTypes(fd); len(res) > 0 {
						idx.Ctors[fd.Name.Name] = res
					}
				}
				continue
			}
			if ast.IsExported(fd.Name.Name) && ast.IsExported(recv) {
				idx.Methods[recv] = append(idx.Methods[recv], ocrCapability{
					Pkg: rel, Type: recv, Method: fd.Name.Name,
					File: c.rel(path), Line: fset.Position(fd.Pos()).Line,
				})
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					idx.InPkgCalls[sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	return idx, nil
}

// ocrImportAliases — alias -> относительный путь пакета этого модуля.
func ocrImportAliases(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if !strings.HasPrefix(p, ocrModulePrefix) {
			continue
		}
		rel := strings.TrimPrefix(p, ocrModulePrefix)
		alias := filepath.Base(rel)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		out[alias] = rel
	}
	return out
}

// ocrScan — вся работа гейта над одним деревом. Вынесена отдельно, чтобы
// инъекция гоняла ТУ ЖЕ функцию на синтетическом дереве.
func ocrScan(c ocrCorpus) (ocrResult, error) {
	var res ocrResult

	idx := map[string]ocrPkgIndex{}
	for _, rel := range c.subdirsOf(ocrOutboxRoot) {
		if len(c.inDir(rel)) == 0 {
			continue
		}
		pi, err := ocrIndexPackage(c, rel)
		if err != nil {
			return res, err
		}
		idx[rel] = pi
		res.Packages++
		res.PkgFiles += pi.Files
	}

	// Какие типы конструирует композиционный корень сервиса.
	constructed := map[string]map[string]bool{}
	for _, svc := range c.subdirsOf("services") {
		for _, path := range c.under(svc + "/cmd") {
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return res, fmt.Errorf("parse %s: %w", path, perr)
			}
			res.RootFiles++
			aliases := ocrImportAliases(f)
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				rel, ok := aliases[id.Name]
				if !ok {
					return true
				}
				pi, ok := idx[rel]
				if !ok {
					return true
				}
				for _, typ := range pi.Ctors[sel.Sel.Name] {
					if constructed[rel] == nil {
						constructed[rel] = map[string]bool{}
					}
					constructed[rel][typ] = true
				}
				return true
			})
		}
	}
	for _, sset := range constructed {
		res.Constructed += len(sset)
	}

	var caps []ocrCapability
	for rel, types := range constructed {
		for typ := range types {
			caps = append(caps, idx[rel].Methods[typ]...)
		}
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i].String() < caps[j].String() })
	res.Capabilities = len(caps)

	// Критерий (а): вызывающие вне пакета, среди импортёров, не-тесты.
	outside := map[string]map[string]bool{}
	for _, path := range c.files {
		rel := c.rel(path)
		if strings.HasPrefix(rel, ocrOutboxRoot+"/") {
			continue // внутрипакетное — критерий (б)
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			continue
		}
		var touched []string
		for _, r := range ocrImportAliases(f) {
			if _, ok := idx[r]; ok {
				touched = append(touched, r)
			}
		}
		if len(touched) == 0 {
			continue
		}
		res.ImporterFiles++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			for _, r := range touched {
				if outside[r] == nil {
					outside[r] = map[string]bool{}
				}
				outside[r][sel.Sel.Name] = true
			}
			return true
		})
	}

	for _, cap := range caps {
		switch {
		case outside[cap.Pkg][cap.Method]:
			res.SilentOutside++
		case idx[cap.Pkg].InPkgCalls[cap.Method]:
			res.SilentInside++
		default:
			res.Findings = append(res.Findings, cap)
		}
	}
	return res, nil
}

// TestOutboxCapabilityHasADriver — каждая объявленная способность компонента
// outbox, который заводит композиционный корень, кем-то исполняется.
//
// Способность гейта краснеть и молчать доказана инъекцией в обе стороны —
// outboxcapabilityreach_injection_test.go.
func TestOutboxCapabilityHasADriver(t *testing.T) {
	root := repoRoot(t)
	// Состав — у индекса git, а не у диска: иначе вердикт стал бы свойством
	// рабочего каталога (игнорируемые копии, распаковки, отчёты), а не коммита.
	tracked, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева %s: %v", root, err)
	}
	res, err := ocrScan(ocrNewCorpus(root, tracked))
	if err != nil {
		t.Fatalf("разбор дерева %s: %v", root, err)
	}

	// Проверки предпосылки: молчание обязано что-то значить.
	if res.Packages == 0 {
		t.Fatalf("предпосылка сломана: под %s нет ни одного пакета (корень %s)", ocrOutboxRoot, root)
	}
	if res.PkgFiles == 0 {
		t.Fatalf("предпосылка сломана: разобрано 0 файлов пакетов %s", ocrOutboxRoot)
	}
	if res.RootFiles == 0 {
		t.Fatal("предпосылка сломана: не прочитано ни одного файла композиционного корня")
	}
	if res.Constructed == 0 {
		t.Fatalf("предпосылка сломана: композиционные корни не конструируют НИ ОДНОГО типа "+
			"из %s — либо семья переехала, либо резолв импортов сломан; молчание такого "+
			"гейта ничего не значит", ocrOutboxRoot)
	}
	if res.Capabilities == 0 {
		t.Fatal("предпосылка сломана: у конструируемых типов нет ни одного экспортируемого метода")
	}
	if res.ImporterFiles == 0 {
		t.Fatalf("предпосылка сломана: ни один файл вне %s не импортирует его пакеты", ocrOutboxRoot)
	}

	t.Logf("перепись: пакетов %s — %d, файлов пакетов %d, файлов композиционного корня %d, "+
		"файлов-импортёров %d, конструируемых типов %d, способностей проверено %d, "+
		"исполняются снаружи %d, внутренняя ступень %d, находок %d",
		ocrOutboxRoot, res.Packages, res.PkgFiles, res.RootFiles, res.ImporterFiles,
		res.Constructed, res.Capabilities, res.SilentOutside, res.SilentInside, len(res.Findings))

	if len(res.Findings) > 0 {
		var b strings.Builder
		b.WriteString("способность объявлена, но её никто не исполняет — ни продукт снаружи, " +
			"ни исполняемый путь самого пакета:\n")
		for _, c := range res.Findings {
			b.WriteString(fmt.Sprintf("  %s:%d — %s\n", c.File, c.Line, c))
		}
		b.WriteString("исходов три: завести исполнителя · снять вместе с производителем · " +
			"записать решением с предикатом появления (ban #11)")
		t.Fatal(b.String())
	}
}
