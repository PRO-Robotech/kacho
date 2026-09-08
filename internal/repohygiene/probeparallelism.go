// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Разбор параллелизма проб гейтов дерева.
//
// # Предмет
//
// Пакет — единица бюджета времени у `go test`: `-timeout` отмеряется тестовому
// БИНАРЮ, и его исчерпание роняет пакет целиком. Вердикта не остаётся ни у одной
// пробы, включая прошедшие, — то есть цена одной медленной пробы не «медленно»,
// а «вердикта нет».
//
// При этом ПРОБЫ внутри пакета исполняются последовательно, пока каждая не
// объявит себя параллельной: `-parallel` разрешает столько одновременных проб,
// сколько ядер, но берёт в этот счёт только те, что позвали [testing.T.Parallel].
// Ранер конвейера даёт ЧЕТЫРЕ ядра (замер runner-probe 2026-09-08: `nproc` = 4,
// 15 ГБ памяти), и пакет гейтов занимал ОДНО из них тридцать минут подряд.
//
// # Почему это гейт, а не разовая правка
//
// Разовая правка истекает со следующим заведённым гейтом: новая проба без
// объявления снова стоит последовательного времени, и заметить это нечем —
// прогон остаётся зелёным, просто медленнее. Свойство «пакет гейтов дерева
// укладывается в свой бюджет с запасом» держится тем, что КАЖДАЯ проба объявляет
// свой режим: параллельна либо названа в ведомости с причиной.
//
// # Область
//
// Только `internal/repohygiene/...` — каталог, чей пакет упирался в бюджет.
// Прочие пакеты гейтов дерева (`deploy`, `internal/release`, `tools/...`) от
// своего предела на порядок дальше (замер конвейера 2026-09-08: 255 с, 103 с,
// 126 с против 2400 с), и требовать от них того же значило бы заводить правило
// без предмета. Каталог обходится РЕКУРСИВНО: новый подпакет попадает под гейт
// сам, а не после того, как о нём вспомнят.

// SequentialProbeAllowance — запись ведомости: проба, которая обязана остаться
// последовательной, и причина.
//
// Причина обязана называть, ЧТО ИМЕННО ломается от параллельного соседа, а не
// «так исторически». Ведомость самоистекает: запись без пробы и запись на пробу,
// которая объявила себя параллельной, — находки.
type SequentialProbeAllowance struct {
	// Dir — каталог пакета относительно корня, со слэшами.
	Dir string
	// Probe — имя верхнеуровневой пробы.
	Probe string
	// Why — что ломает параллельный сосед.
	Why string
}

// ProbeParallelismOptions — вход разбора.
type ProbeParallelismOptions struct {
	// Root — корень дерева.
	Root string
	// Dir — каталог обхода относительно корня, со слэшами.
	Dir string
	// Allow — ведомость последовательных проб.
	Allow []SequentialProbeAllowance
	// Index — состав каталога: абсолютные пути файлов проб. nil означает индекс
	// git ([treecorpus]) — единственный источник на пути дерева.
	//
	// Поле существует ради ИНЪЕКЦИИ, и подменяется им ровно одно: откуда взят
	// перечень файлов. Разбор, признак и ведомость остаются те же, поэтому обе
	// стороны инъекции гоняют ту же функцию, что и гейт по дереву, — а не её
	// копию, снисходительнее настоящей.
	Index func(base string) ([]string, error)
}

// ProbeParallelismCensus — объём осмотренного. Печатается всегда: «находок ноль»
// обязано быть отличимо от «прочитано ноль».
type ProbeParallelismCensus struct {
	// Dirs — сколько каталогов с пробами обойдено.
	Dirs int
	// Files — сколько файлов проб прочитано.
	Files int
	// Probes — сколько верхнеуровневых проб осмотрено.
	Probes int
	// Parallel — сколько из них объявили себя параллельными.
	Parallel int
	// Sequential — сколько последовательных по ведомости.
	Sequential int
}

// String — перепись одной строкой.
func (c ProbeParallelismCensus) String() string {
	return fmt.Sprintf("перепись: каталогов с пробами %d · файлов проб прочитано %d · "+
		"проб осмотрено %d · параллельных %d · последовательных по ведомости %d",
		c.Dirs, c.Files, c.Probes, c.Parallel, c.Sequential)
}

// ProbeParallelismFinding — одна находка.
type ProbeParallelismFinding struct {
	// Where — координата: каталог и имя пробы.
	Where string
	// What — что именно не так.
	What string
}

// String — находка одной строкой.
func (f ProbeParallelismFinding) String() string { return f.Where + ": " + f.What }

// AuditProbeParallelism — разбирает пробы каталога и возвращает находки вместе с
// переписью.
//
// Состав берётся у индекса git ([treecorpus]), а не обходом диска: рабочие копии
// агентов и распаковки под тем же каталогом частью дерева не являются, и вердикт
// обязан быть свойством КОММИТА.
//
// Признак читается РАЗБОРОМ, а не поиском по образцу. Это не педантизм: строка
// `func TestProbe(t *testing.T) {` встречается в этом дереве внутри строковых
// литералов фикстур инъекции — поиск по образцу считал бы их пробами и требовал
// объявления от текста, который никто не исполняет.
func AuditProbeParallelism(o ProbeParallelismOptions) ([]ProbeParallelismFinding, ProbeParallelismCensus, error) {
	var census ProbeParallelismCensus

	index := o.Index
	if index == nil {
		index = func(base string) ([]string, error) {
			return treecorpus.UnderWithSuffix(base, "_test.go")
		}
	}
	base := filepath.Join(o.Root, filepath.FromSlash(o.Dir))
	files, err := index(base)
	if err != nil {
		return nil, census, fmt.Errorf("состав %s: %w", o.Dir, err)
	}
	if len(files) == 0 {
		return nil, census, fmt.Errorf("под %s нет ни одного файла проб — смотреть не на что; "+
			"«ноль находок» здесь означало бы «ноль прочитанного», поэтому это отказ, "+
			"а не пустой успех", o.Dir)
	}

	type key struct{ dir, probe string }
	parallel := map[key]bool{}
	order := make([]key, 0, len(files))
	dirs := map[string]struct{}{}

	for _, abs := range files {
		rel, rerr := filepath.Rel(o.Root, abs)
		if rerr != nil {
			return nil, census, fmt.Errorf("путь %s: %w", abs, rerr)
		}
		slashed := filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(slashed))
		dirs[dir] = struct{}{}
		census.Files++

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, abs, nil, 0)
		if perr != nil {
			return nil, census, fmt.Errorf("разбор %s: %w", slashed, perr)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			recv, ok := topLevelProbeReceiver(fd)
			if !ok {
				continue
			}
			k := key{dir: dir, probe: fd.Name.Name}
			order = append(order, k)
			parallel[k] = firstStatementIsParallel(fd.Body, recv)
		}
	}
	census.Dirs = len(dirs)
	census.Probes = len(order)

	allowed := map[key]SequentialProbeAllowance{}
	for _, a := range o.Allow {
		allowed[key{dir: a.Dir, probe: a.Probe}] = a
	}

	var findings []ProbeParallelismFinding
	for _, k := range order {
		if parallel[k] {
			census.Parallel++
			if a, ok := allowed[k]; ok {
				findings = append(findings, ProbeParallelismFinding{
					Where: k.dir + " " + k.probe,
					What: fmt.Sprintf("запись ведомости («%s») стоит на пробе, которая объявила "+
						"себя параллельной — исключать ей больше нечего. Снимите запись: "+
						"оставленная, она остаётся местом, куда последовательную пробу вносят "+
						"незамеченной", a.Why),
				})
			}
			continue
		}
		if _, ok := allowed[k]; ok {
			census.Sequential++
			continue
		}
		findings = append(findings, ProbeParallelismFinding{
			Where: k.dir + " " + k.probe,
			What: "проба не объявила себя параллельной (`t.Parallel()` первым оператором) и не " +
				"названа в ведомости последовательных. Пакет — единица бюджета времени: " +
				"последовательная проба занимает одно ядро из четырёх и приближает пакет к " +
				"пределу, за которым вердикта не остаётся НИ У ОДНОЙ пробы. Исходы два: " +
				"объявить параллельной либо внести в ведомость, назвав, что ломает " +
				"параллельный сосед",
		})
	}

	seen := map[key]struct{}{}
	for _, k := range order {
		seen[k] = struct{}{}
	}
	stale := make([]string, 0, len(o.Allow))
	for _, a := range o.Allow {
		if _, ok := seen[key{dir: a.Dir, probe: a.Probe}]; !ok {
			stale = append(stale, a.Dir+" "+a.Probe)
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		findings = append(findings, ProbeParallelismFinding{
			Where: s,
			What: "запись ведомости не находит своей пробы — у неё больше нет предмета " +
				"(проба переименована, переехала или снята). Снимите запись вместе с предметом",
		})
	}

	return findings, census, nil
}

// topLevelProbeReceiver — имя приёмника, если это верхнеуровневая проба
// `func Test…(t *testing.T)`. `TestMain(m *testing.M)` пробой не является:
// параллелизма у него нет, он запускает прогон.
func topLevelProbeReceiver(fd *ast.FuncDecl) (string, bool) {
	if !strings.HasPrefix(fd.Name.Name, "Test") {
		return "", false
	}
	ps := fd.Type.Params.List
	if len(ps) != 1 || len(ps[0].Names) != 1 {
		return "", false
	}
	star, ok := ps[0].Type.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "testing" {
		return "", false
	}
	return ps[0].Names[0].Name, true
}

// firstStatementIsParallel — тело начинается вызовом `<приёмник>.Parallel()`.
//
// Требуется именно ПЕРВЫМ оператором, а не где-нибудь в теле: вызов после
// подготовки оставляет подготовку последовательной, и «объявлено» перестаёт
// означать «исполняется параллельно». Одна форма вместо двух ещё и делает
// признак однозначным.
func firstStatementIsParallel(body *ast.BlockStmt, recv string) bool {
	if len(body.List) == 0 {
		return false
	}
	es, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Parallel" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == recv
}
