// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// buildtagrunreach_test.go — у пакета под признаком сборки обязан быть ПРОГОН,
// который этот признак передаёт и этот пакет покрывает.
//
// # Предмет
//
// Соседний гейт (`buildtaggedtestpackage_test.go`) утверждает СОБИРАЕМОСТЬ под
// признаком и честно оговаривает, что исполнение держит строка в цели
// `test-integration` корневого Makefile. То есть держится оно ВНИМАНИЕМ: свойство
// «каждый пакет с признаком попадает в отбор» не проверял никто.
//
// Сегодня свойство выполняется СЛУЧАЙНО. Пакетов с включающим признаком в дереве
// два, и оба оказались покрыты: `services/compute/internal/repo` попадает в отбор
// по пути, `deploy` назван в рабочем процессе операндом. Первый же файл
// `//go:build integration` в `internal/apps/…` или `internal/authz…` окажется
// невидим для ВСЕХ прогонов молча — обычная сборка такие файлы не читает,
// короткий прогон идёт с `-short`, а интеграционная джоба отбирает пакеты ПО
// ПУТИ.
//
// Класс уже срабатывал дважды: у compute (#489) тег не передавался вовсе, а у
// проб модели прав отбор по пути до них не доставал — ради них завели отдельную
// цель `test-authz-fga`. Второй раз поштучно этот разрыв лечить не стоит.
//
// # Почему отбор ВЫВОДИТСЯ, а не выписывается
//
// Копия отбора в гейте разошлась бы с объявлением молча, и разошлась бы именно
// там, где расхождение не видно: обе стороны отвечают «покрыт» на пакете,
// который покрыт и так. Поэтому гейт читает ОБЪЯВЛЕНИЯ — корневой Makefile,
// рабочие процессы, скрипты — и достаёт из них признак, область `go list` и
// фильтр отбора. Меняется объявление — меняется и предикат гейта, без правки
// гейта.
//
// # Чего гейт НЕ утверждает
//
// Он утверждает, что пакет ПОПАДАЕТ В ОТБОР прогона, передающего его признак, —
// а не что внутри прогона исполнилась каждая проба. Сужение `-run` (рабочий
// процесс зовёт `deploy` именно так) остаётся вне его предмета: это отдельное
// свойство и у него отдельный владелец. Он также не утверждает, что прогон
// кто-то запускает: цель может существовать и не вызываться ни одной джобой —
// это третий вопрос, и смешивать их в одном предикате значило бы получить гейт,
// про который непонятно, что он проверяет.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// ── сбор пакетов под признаком ──────────────────────────────────────────────

// taggedPkgScan — что нашлось в дереве под включающими признаками сборки.
//
// Счётчики прочитанного едут ВМЕСТЕ с результатом, а не отдельным возвратом:
// «ноль пакетов с признаком» и «ноль прочитанных файлов» — разные исходы, и
// вызывающий обязан их различать.
type taggedPkgScan struct {
	ByPkg        map[string]map[string]bool // пакет -> набор включающих признаков
	Funcs        []taggedTestFunc           // пробы под признаком, поимённо и с координатой
	FilesScanned int
	FilesWithTag int
}

// taggedTestFunc — одна проба, объявленная под включающим признаком сборки.
//
// Имя и координата едут вместе: гейт отбора судит ИМЕНА (их сверяет `-run`), а
// чинить находку читатель идёт по координате. Находка без координаты требует от
// читателя обхода дерева, который гейт только что уже сделал.
type taggedTestFunc struct {
	Pkg  string // каталог пакета относительно корня
	Tag  string // включающий признак сборки файла
	Name string // имя функции: то, что сверяет `-run`
	Rel  string // файл относительно корня
	Line int    // строка объявления
}

func (f taggedTestFunc) coord() string { return fmt.Sprintf("%s:%d", f.Rel, f.Line) }

// collectTaggedTestPackages — ОДИН сканер на все три гейта.
//
// Прежде цикл обхода стоял внутри `auditBuildTaggedTestPackages`. Второй гейт с
// собственной копией того же обхода — это два места об одном предмете, из
// которых со временем верно одно; отбор включающих признаков достаточно тонок
// (`integration || !short` даёт один тег, а не два), чтобы копия разошлась
// незаметно. По той же причине сюда добавлен сбор ИМЁН проб: третий гейт
// (`buildtagrunselection_test.go`) судит отбор `-run`, и свой обход дерева был
// бы третьей копией того же цикла.
func collectTaggedTestPackages(root string) (taggedPkgScan, error) {
	scan := taggedPkgScan{ByPkg: map[string]map[string]bool{}}

	files, err := treecorpus.UnderWithSuffix(root, "_test.go")
	if err != nil {
		return scan, fmt.Errorf("перечень файлов проб: %w", err)
	}
	scan.FilesScanned = len(files)

	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return scan, fmt.Errorf("путь %s относительно корня: %w", abs, err)
		}
		rel = filepath.ToSlash(rel)

		line, err := buildTagLine(abs)
		if err != nil {
			return scan, fmt.Errorf("чтение %s: %w", rel, err)
		}
		if line == "" {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return scan, fmt.Errorf("разбор признака сборки в %s: %w", rel, err)
		}
		kept := enablingTags(expr)
		if len(kept) == 0 {
			continue
		}
		scan.FilesWithTag++

		pkg := filepath.ToSlash(filepath.Dir(rel))
		if scan.ByPkg[pkg] == nil {
			scan.ByPkg[pkg] = map[string]bool{}
		}
		names, err := testFuncNames(abs)
		if err != nil {
			return scan, fmt.Errorf("разбор проб в %s: %w", rel, err)
		}
		for _, tag := range kept {
			scan.ByPkg[pkg][tag] = true
			for _, n := range names {
				scan.Funcs = append(scan.Funcs, taggedTestFunc{
					Pkg: pkg, Tag: tag, Name: n.name, Rel: rel, Line: n.line,
				})
			}
		}
	}
	return scan, nil
}

// testFuncDecl — имя пробы и строка её объявления.
type testFuncDecl struct {
	name string
	line int
}

// testFuncNames — имена проб файла, взятые РАЗБОРОМ, а не образцом по тексту.
//
// Образец по тексту здесь негоден дважды: `func TestX` встречается в строковом
// литерале соседней пробы инъекции (в этом пакете такие литералы есть) и в
// комментарии, объясняющем пробу. Разбор судит УЗЕЛ объявления, поэтому ни то,
// ни другое под него не подпадает by construction. Признак сборки разбору не
// мешает: `go/parser` читает файл независимо от того, включён ли тег.
//
// Границу гейт объявляет сам: считаются только `func TestXxx(*testing.T)` —
// то, что отбирает `-run` у `go test`. Замеры (`Benchmark`) отбирает `-bench`,
// фаззеры (`Fuzz`) — своя пара флагов; это другие предметы, и молча
// приписывать их сюда значило бы утверждать о них то, чего гейт не проверял.
func testFuncNames(path string) ([]testFuncDecl, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []testFuncDecl
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil {
			continue
		}
		if !strings.HasPrefix(fn.Name.Name, "Test") || !isTestingTParam(fn) {
			continue
		}
		out = append(out, testFuncDecl{
			name: fn.Name.Name,
			line: fset.Position(fn.Pos()).Line,
		})
	}
	return out, nil
}

// isTestingTParam — единственный параметр функции есть `*testing.T`.
//
// Без этой проверки под перепись попал бы помощник с именем на `Test` и другой
// сигнатурой: `-run` его не отбирает, и требовать его отбора значило бы
// производить находку, которую нечем закрыть.
func isTestingTParam(fn *ast.FuncDecl) bool {
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "testing"
}

// ── извлечение прогонов из объявлений ───────────────────────────────────────

// taggedRun — прогон, передающий признак сборки, каким он объявлен в дереве.
type taggedRun struct {
	Source  string           // "Makefile:316" — координата, иначе находку не проверить
	Tag     string           // признак, который прогон передаёт
	Scopes  []string         // область: операнды `go test` либо питающего `go list`
	Filters []*regexp.Regexp // отбор `grep -E`, если прогон питается конвейером
	// Select и Skip — сужение ВНУТРИ пакета: `-run` и `-skip` вызова.
	//
	// Пустой Select означает «берёт всё», а не «не берёт ничего»: `go test` без
	// `-run` исполняет каждую пробу пакета. Это ровно тот вид ручки, о котором
	// предупреждает `polyrepo.md`: пустое значение обязано читаться в ту
	// сторону, в какую его читает исполнитель, а не в удобную.
	Select *regexp.Regexp
	Skip   *regexp.Regexp
	// RunPattern и SkipPattern — исходные строки, ради находки: читателю нужен
	// текст, который стоит в объявлении, а не то, во что его скомпилировали.
	RunPattern  string
	SkipPattern string
}

func (r taggedRun) String() string {
	s := fmt.Sprintf("%s: -tags=%s область=%v", r.Source, r.Tag, r.Scopes)
	if len(r.Filters) > 0 {
		pats := make([]string, 0, len(r.Filters))
		for _, f := range r.Filters {
			pats = append(pats, f.String())
		}
		s += fmt.Sprintf(" отбор=%v", pats)
	}
	return s
}

// declarationKind — как у файла устроен комментарий.
//
// Комментарий обязан отбрасываться, и это не педантизм: в `.github/workflows/ci.yaml`
// РЯДОМ с настоящим вызовом стоит абзац, который его объясняет и содержит
// `-tags helmcharts` дословно. Гейт по сырому тексту принял бы прозу за прогон —
// то есть засчитал бы покрытие по комментарию, объясняющему покрытие.
type declarationKind int

const (
	kindMakefile declarationKind = iota
	kindYAML
	kindShell
)

// declarationFiles — где объявляются прогоны. Отбор ПО ВИДУ файла, а не списком
// путей: список разошёлся бы с деревом, а вид — свойство, которое дерево несёт
// само. Документация (`.md`) сюда не входит намеренно: страница, цитирующая
// команду, прогоном не является.
func declarationFiles(root string) ([]string, error) {
	tracked, err := treecorpus.Under(root)
	if err != nil {
		return nil, fmt.Errorf("перечень отслеживаемых файлов: %w", err)
	}
	var out []string
	for _, abs := range tracked {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(rel)
		switch {
		case base == "Makefile" || strings.HasSuffix(base, ".mk"):
			out = append(out, abs)
		case strings.HasPrefix(rel, ".github/workflows/") &&
			(strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml")):
			out = append(out, abs)
		case strings.HasSuffix(base, ".sh"):
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out, nil
}

func kindOf(rel string) declarationKind {
	base := filepath.Base(rel)
	switch {
	case base == "Makefile" || strings.HasSuffix(base, ".mk"):
		return kindMakefile
	case strings.HasSuffix(base, ".sh"):
		return kindShell
	default:
		return kindYAML
	}
}

// stripComment — убирает комментарий строки по виду файла.
//
// Режется только комментарий, начинающий строку (с отступом), а не всякий `#`:
// решётка встречается внутри значений и регулярных выражений, и резать по
// первому вхождению значило бы терять настоящие вызовы.
func stripComment(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "#") {
		return ""
	}
	return line
}

var (
	// `-tags=X` либо `-tags X`; значение — до пробела или кавычки.
	reTagFlag = regexp.MustCompile(`-tags[= ]'?"?([A-Za-z0-9_,.-]+)`)
	// вызов пробника: `go test`, `$(GO) test`, `${GO} test`.
	reGoTest = regexp.MustCompile(`(?:\bgo|\$\(GO\)|\$\{GO\})\s+test\b`)
	reGoList = regexp.MustCompile(`(?:\bgo|\$\(GO\)|\$\{GO\})\s+list\b`)
	// операнд-путь: `./services/$(SVC)/...`, `./deploy/`, `./...`
	reOperand = regexp.MustCompile(`\./[A-Za-z0-9_./*-]*(?:\$\([A-Za-z0-9_]+\)|\$\{[A-Za-z0-9_]+\})?[A-Za-z0-9_./*-]*`)
	// отбор конвейером: grep -E '<re>' / grep -E "<re>"
	reGrepE = regexp.MustCompile(`grep\s+(?:-[A-Za-z]+\s+)*-[A-Za-z]*E[A-Za-z]*\s+'([^']+)'|grep\s+(?:-[A-Za-z]+\s+)*-[A-Za-z]*E[A-Za-z]*\s+"([^"]+)"`)
	// сужение внутри пакета: `-run X`, `-run=X`, `-test.run='X'`, `-run "X"`.
	// Все четыре формы законны у `go test`, поэтому распознаются все четыре:
	// форма, о которой распознаватель не знает, делает сужение НЕВИДИМЫМ, а не
	// редким (`testing.md` §«Гейт на класс» п.7).
	reRunFlag  = regexp.MustCompile(`-(?:test\.)?run[= ]\s*(?:'([^']*)'|"([^"]*)"|([^\s'"]+))`)
	reSkipFlag = regexp.MustCompile(`-(?:test\.)?skip[= ]\s*(?:'([^']*)'|"([^"]*)"|([^\s'"]+))`)
)

// lastFlagValue — значение флага, каким его увидит `go test`.
//
// Флаг, повторённый дважды, у `go test` разрешается ПОСЛЕДНИМ вхождением, и
// гейт обязан читать его так же: иначе он судил бы отбор, которого исполнитель
// не применяет.
func lastFlagValue(re *regexp.Regexp, line string) string {
	ms := re.FindAllStringSubmatch(line, -1)
	if len(ms) == 0 {
		return ""
	}
	m := ms[len(ms)-1]
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// topLevelPattern — часть образца `-run`, относящаяся к пробе верхнего уровня.
//
// `go test` делит образец по `/`: первый сегмент сверяется с именем пробы,
// остальные — с именами вложенных. Гейт судит только верхний уровень и говорит
// об этом прямо: вложенные пробы он не перечисляет, поэтому и утверждать об их
// отборе не вправе.
func topLevelPattern(pat string) string {
	if i := strings.IndexByte(pat, '/'); i >= 0 {
		return pat[:i]
	}
	return pat
}

// lookBack — сколько строк назад искать питающий `go list` и `grep -E`.
//
// Вызов, питаемый через `xargs`, своих операндов не несёт: область задана выше,
// в той же рецептуре. Окно конечно и намеренно невелико — оно ограничивает
// радиус догадки; если объявление растянется шире, гейт перестанет находить
// область и СКАЖЕТ об этом находкой, а не сочтёт пакет непокрытым молча.
const lookBack = 20

// extractTaggedRuns — читает объявления и достаёт из них прогоны с признаком.
func extractTaggedRuns(root string) ([]taggedRun, int, error) {
	files, err := declarationFiles(root)
	if err != nil {
		return nil, 0, err
	}

	var runs []taggedRun
	for _, abs := range files {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, 0, err
		}
		rel = filepath.ToSlash(rel)

		raw, err := os.ReadFile(abs) // #nosec G304 — путь из индекса git этого дерева
		if err != nil {
			return nil, 0, fmt.Errorf("чтение %s: %w", rel, err)
		}
		kind := kindOf(rel)
		lines := strings.Split(string(raw), "\n")

		for i, rawLine := range lines {
			line := stripComment(rawLine)
			if line == "" || !reGoTest.MatchString(line) {
				continue
			}
			m := reTagFlag.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			// `-tags=a,b` — прогон передаёт оба.
			for _, tag := range strings.Split(m[1], ",") {
				tag = strings.TrimSpace(tag)
				if tag == "" {
					continue
				}
				run := taggedRun{
					Source: fmt.Sprintf("%s:%d", rel, i+1),
					Tag:    tag,
					Scopes: operandsOf(line, kind),
				}
				if pat := lastFlagValue(reRunFlag, line); pat != "" {
					run.RunPattern = pat
					if re, err := regexp.Compile(topLevelPattern(pat)); err == nil {
						run.Select = re
					}
				}
				if pat := lastFlagValue(reSkipFlag, line); pat != "" {
					run.SkipPattern = pat
					if re, err := regexp.Compile(topLevelPattern(pat)); err == nil {
						run.Skip = re
					}
				}
				// Вызов без своих операндов питается конвейером: область и отбор
				// стоят выше, в той же рецептуре.
				if len(run.Scopes) == 0 {
					run.Scopes, run.Filters = feedingSelection(lines, i, kind)
				}
				runs = append(runs, run)
			}
		}
	}
	return runs, len(files), nil
}

// operandsOf — операнды-пути вызова, без флагов и их значений.
func operandsOf(line string, kind declarationKind) []string {
	var out []string
	for _, cand := range reOperand.FindAllString(line, -1) {
		out = append(out, normaliseScope(cand, kind))
	}
	return out
}

// feedingSelection — область и отбор, питающие вызов через `xargs`.
func feedingSelection(lines []string, at int, kind declarationKind) ([]string, []*regexp.Regexp) {
	var scopes []string
	var filters []*regexp.Regexp

	for j := at - 1; j >= 0 && j >= at-lookBack; j-- {
		line := stripComment(lines[j])
		if line == "" {
			continue
		}
		if len(scopes) == 0 && reGoList.MatchString(line) {
			scopes = operandsOf(line, kind)
		}
		for _, m := range reGrepE.FindAllStringSubmatch(line, -1) {
			pat := m[1]
			if pat == "" {
				pat = m[2]
			}
			if kind == kindMakefile {
				// В Makefile `$$` — это экранированный `$`; регулярное выражение
				// `(/|$$)` означает «слэш или конец строки», а не «два доллара».
				pat = strings.ReplaceAll(pat, "$$", "$")
			}
			if re, err := regexp.Compile(pat); err == nil {
				filters = append(filters, re)
			}
		}
	}
	return scopes, filters
}

// normaliseScope — приводит операнд к виду, сравнимому с путём пакета.
//
// Подстановка make (`$(SVC)`) превращается в `*`: цель перебирает по ней все
// сервисы, то есть область покрывает любой сегмент на этом месте.
func normaliseScope(op string, kind declarationKind) string {
	s := strings.TrimPrefix(op, "./")
	s = strings.TrimSuffix(s, "/")
	if kind == kindMakefile || kind == kindShell {
		s = regexp.MustCompile(`\$\([A-Za-z0-9_]+\)|\$\{[A-Za-z0-9_]+\}`).ReplaceAllString(s, "*")
	}
	return s
}

// scopeCovers — покрывает ли область пакет.
//
// `...` последним сегментом означает «этот каталог и всё под ним»; `*` — ровно
// один любой сегмент.
func scopeCovers(scope, pkg string) bool {
	if scope == "" {
		return false
	}
	if scope == "..." {
		return true
	}
	want := strings.Split(scope, "/")
	got := strings.Split(pkg, "/")

	for i, seg := range want {
		if seg == "..." {
			return true // всё, что глубже, покрыто
		}
		if i >= len(got) {
			return false
		}
		if seg != "*" && seg != got[i] {
			return false
		}
	}
	return len(want) == len(got)
}

// runCovers — попадает ли пакет в отбор прогона.
func runCovers(run taggedRun, pkg, modulePath string) bool {
	covered := false
	for _, s := range run.Scopes {
		if scopeCovers(s, pkg) {
			covered = true
			break
		}
	}
	if !covered {
		return false
	}
	// Фильтр применяется к ИМПОРТ-ПУТИ: именно его печатает `go list`, и именно
	// по нему отбирает `grep` в объявлении.
	full := modulePath + "/" + pkg
	for _, f := range run.Filters {
		if !f.MatchString(full) {
			return false
		}
	}
	return true
}

// ── гейт ────────────────────────────────────────────────────────────────────

type tagRunFinding struct {
	Pkg  string
	Tag  string
	Runs []string // что рассматривалось — иначе находку нечем опровергнуть
}

func (f tagRunFinding) String() string {
	return fmt.Sprintf(
		"%s несёт признак сборки %q, но НИ ОДИН объявленный прогон его не покрывает.\n"+
			"    рассмотрены прогоны с этим признаком: %s",
		f.Pkg, f.Tag, strings.Join(f.Runs, " · "))
}

type tagRunCensus struct {
	DeclarationFiles int
	RunsFound        int
	TagsInRuns       []string
	FilesScanned     int
	FilesWithTag     int
	PackagesChecked  int
	PairsChecked     int
}

func (c tagRunCensus) String() string {
	return fmt.Sprintf(
		"перепись: объявлений прочитано %d · прогонов с признаком найдено %d (признаки: %s) · "+
			"файлов проб прочитано %d · из них с признаком %d · пакетов под признаком %d · "+
			"пар пакет×признак проверено %d",
		c.DeclarationFiles, c.RunsFound, strings.Join(c.TagsInRuns, ", "),
		c.FilesScanned, c.FilesWithTag, c.PackagesChecked, c.PairsChecked)
}

// auditTaggedPackagesAreExecuted — судья. Вынесен из тела теста, чтобы ТОТ ЖЕ
// судья судил синтетическое дерево пробы инъекции.
func auditTaggedPackagesAreExecuted(root, modulePath string) ([]tagRunFinding, tagRunCensus, error) {
	var census tagRunCensus

	scan, err := collectTaggedTestPackages(root)
	if err != nil {
		return nil, census, err
	}
	census.FilesScanned = scan.FilesScanned
	census.FilesWithTag = scan.FilesWithTag
	census.PackagesChecked = len(scan.ByPkg)

	runs, declFiles, err := extractTaggedRuns(root)
	if err != nil {
		return nil, census, err
	}
	census.DeclarationFiles = declFiles
	census.RunsFound = len(runs)

	tagSet := map[string]bool{}
	for _, r := range runs {
		tagSet[r.Tag] = true
	}
	census.TagsInRuns = buildTagSortedKeys(tagSet)

	var findings []tagRunFinding
	for _, pkg := range buildTagSortedKeys(buildTagPkgSet(scan.ByPkg)) {
		for _, tag := range buildTagSortedKeys(scan.ByPkg[pkg]) {
			census.PairsChecked++

			var considered []string
			ok := false
			for _, r := range runs {
				if r.Tag != tag {
					continue
				}
				considered = append(considered, r.String())
				if runCovers(r, pkg, modulePath) {
					ok = true
					break
				}
			}
			if ok {
				continue
			}
			if len(considered) == 0 {
				considered = []string{"ни одного — признак не передаёт ни один объявленный прогон"}
			}
			findings = append(findings, tagRunFinding{Pkg: pkg, Tag: tag, Runs: considered})
		}
	}
	return findings, census, nil
}

// TestBuildTagPackagesAreReachedByADeclaredRun — гейт класса.
//
// Пустое дерево признаков — законный исход: гейт печатает перепись и проходит.
// Отказом является «ноль прочитанных файлов» и «ноль прочитанных объявлений»:
// тогда молчание означает, что судья не работал.
func TestBuildTagPackagesAreReachedByADeclaredRun(t *testing.T) {
	root := repoRoot(t)

	findings, census, err := auditTaggedPackagesAreExecuted(root, "github.com/PRO-Robotech/kacho")
	if err != nil {
		t.Fatalf("обход дерева сорвался — вердикта нет: %v", err)
	}
	t.Log(census.String())

	if census.FilesScanned == 0 {
		t.Fatal("прочитано ноль файлов проб — предпосылка гейта не выполнена. " +
			"«Ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if census.DeclarationFiles == 0 {
		t.Fatal("прочитано ноль объявлений — отбор не из чего вывести. " +
			"Гейт, не нашедший ни одного прогона, обязан молчать не может: он не работал")
	}

	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "пакетов под признаком сборки вне всякого объявленного прогона: %d\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s\n\n", f)
	}
	b.WriteString("Пробы такого пакета не исполняются НИ ОДНИМ прогоном: обычная сборка файлы\n")
	b.WriteString("под признаком не читает, короткий прогон идёт с -short, а интеграционная\n")
	b.WriteString("джоба отбирает пакеты по пути. Исходов два: внести пакет в область\n")
	b.WriteString("существующего прогона либо завести прогон, передающий этот признак.\n")
	b.WriteString(census.String())
	t.Fatal(b.String())
}
