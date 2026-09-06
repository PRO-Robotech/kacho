// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package gosecsubject — гейт одного правила: у директивы подавления gosec
// обязан быть ПРЕДМЕТ.
//
// # Что здесь считается дефектом
//
// `// #nosec G304 -- причина` читается человеком как «здесь посмотрели и решили,
// что срабатывание ложно». Когда код рядом чинят по существу, правило перестаёт
// срабатывать — а строка остаётся. С этого момента она не подавляет НИЧЕГО и при
// этом продолжает утверждать, что решение принято: предъявляет закрытый долг за
// открытый и прячет следующую находку по той же координате, которую тот же
// человек уже согласился не смотреть.
//
// Соседний гейт `internal/repohygiene` `TestNoInertGosecSuppressions` ловит ту же
// беду с другой стороны — чужой диалект `//nolint` с именем gosec в перечне
// линтеров, который в этом дереве не читает никто. Там форма нерабочая при живом
// предмете; здесь форма рабочая, а предмета может не быть. Ни один из двух
// второго не заменяет. (Диалект назван здесь по частям намеренно: записанный
// целиком, он сам стал бы находкой того гейта — гейт судит код, а не прозу о
// себе, и различить их в комментарии ему нечем.)
//
// # Почему НЕ по номеру строки
//
// Сопоставление «директива — находка» по строкам пробовали и оно провалилось
// дважды подряд, оба раза на разборщике, а не на дереве: окно «строка директивы
// +3» дало 11 мнимых сирот (директива бывает многострочным комментарием, и код
// стоит на пять-семь строк ниже), окно «следующая строка кода» — 7. Предикат,
// дающий заведомо ложные находки, снимут первым же ложным срабатыванием.
//
// Здесь диапазон подавления не угадывается, а ВЫЧИСЛЯЕТСЯ той же связкой, какой
// его вычисляет сам сканер: `ast.NewCommentMap` привязывает группу комментариев
// к узлу, и подавление накрывает объединение их строк (gosec v2.28.0,
// `updateIgnoredRulesForNode`). Разница с угадыванием принципиальная: окно — это
// догадка о коде, а карта комментариев — то же самое обращение к стандартной
// библиотеке, которое делает сканер.
//
// # Чем это отличается от «сравнить два прогона»
//
// Второй прогон (`-nosec=true`) не нужен: с `-track-suppressions` сканер САМ
// сообщает каждую находку, которую подавил, — с правилом, файлом и строкой. Это
// и есть та половина, которой не хватало: «находки нет, потому что код чист» и
// «находки нет, потому что её скрыли» перестают выглядеть одинаково.
//
// Замер, на котором это построено (пин v2.28.0, оба модуля дерева): подавленных
// находок 117 + 36; прогон с `-nosec=true` даёт 122 + 36, из них 5 + 0 не
// подавлены ничем. 117+5 = 122 и 36+0 = 36 — множества сходятся, значит перечень
// подавленного полон.
//
// # Три исхода, а не два
//
// Директива, по координате которой находки нет, инертна ТОЛЬКО если файл вообще
// осматривался. Флага `-tests` у скана нет намеренно, поэтому `*_test.go` сканер
// не читает вовсе, и там «нет находки» означает «не было ОСМОТРА». Такие
// директивы объявляются вне осмотра и НЕ судятся — урок соседнего гейта
// IaC-скана, оплаченный там дважды.
//
// # Предпосылки, которые гейт меряет, а не предполагает
//
//  1. Распознаватель директив согласен со сканером. gosec печатает своё число
//     найденных директив (`Stats.nosec`); оно обязано совпасть с числом, которое
//     насчитал гейт по тем же файлам. Разойдись они — гейт судит не тот набор, и
//     узнать об этом было бы неоткуда. Замер на дереве: 141 и 42 у сканера, 141
//     и 42 у гейта.
//  2. Журнал скана содержит перечень прочитанных файлов, и его длина совпадает с
//     `Stats.files`. Иначе множество осмотренного пусто, и «вне осмотра»
//     становится вердиктом обо всём дереве сразу.
//  3. Каждый модуль дерева попал в перечень скана. Это второе, независимое
//     чтение того же предмета: модуль, вылетевший из скана, вылетел бы и из
//     вердикта, и оба были бы уверены в своей правоте.
//
// # Направление ошибки выбрано осознанно
//
// Находку сканера гейт засчитывает предметом КАЖДОЙ директиве, чей диапазон её
// накрывает, тогда как сам сканер выбирает ПЕРВЫЙ подходящий диапазон. Это
// шире, чем у сканера, и выбрано намеренно: так гейт может недобрать, но не
// может обвинить директиву, у которой предмет есть. Ложная находка стоит доверия
// ко всему гейту, недобор — одной пропущенной строки.
package gosecsubject

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// AllRules — то, чем gosec обозначает «подавить все правила»
// (`aliasOfAllRules` в analyzer.go). Голая директива и легаси-форма `block`
// подавляют всё.
const AllRules = "*"

// ManifestName — перечень, который скан оставляет гейту: по строке на модуль,
// `<каталог><TAB><отчёт><TAB><журнал>`. Перечень не выписывается в двух местах:
// его пишет тот, кто сканировал, а гейт отдельно сверяет его с индексом git.
const ManifestName = "gosec-suppressions-manifest.txt"

// nosecTag / directivePrefix — обе формы, которые читает gosec v2.28.0.
const (
	nosecTag        = "#nosec"
	directivePrefix = "//gosec:disable"
)

// Directive — одна директива подавления, как её видит сканер.
type Directive struct {
	File   string   // путь относительно корня репозитория
	Line   int      // строка САМОГО тега, а не начала группы комментариев
	Start  int      // первая строка диапазона подавления
	End    int      // последняя строка диапазона подавления
	Rules  []string // правила, которые директива подавляет; AllRules — все
	Reason string   // текст после `--`
}

// RuleKey — правила директивы в каноничном виде: отсортированы и склеены
// запятой. Ведомость ключуется этим, а не строкой: перестановка `G304 G703`
// не должна выглядеть новой записью.
func (d Directive) RuleKey() string {
	r := append([]string(nil), d.Rules...)
	sort.Strings(r)
	return strings.Join(r, ",")
}

// Finding — находка сканера. Подавленная или нет — здесь неважно: и та и другая
// доказывают, что правило по этой координате СРАБАТЫВАЕТ, а значит у директивы
// есть предмет. Считать предметом только подавленные было бы уже, чем нужно:
// находку, которую сканер отнёс к соседнему диапазону, директива всё равно
// объясняет.
type Finding struct {
	File  string
	Rule  string
	Start int
	End   int
}

// LedgerRow — запись ведомости инертных директив.
type LedgerRow struct {
	File   string
	Rule   string // RuleKey директивы
	Count  int    // ТОЧНОЕ число инертных директив, а не потолок
	Reason string
	Line   int    // строка в самой ведомости
	Why    string // заполняется, когда запись признана устаревшей
}

func (r LedgerRow) key() string { return r.File + "\x00" + r.Rule }

// ── РАСПОЗНАВАТЕЛЬ ──────────────────────────────────────────────────────────

// FindDirectives разбирает файл и возвращает директивы подавления в том виде,
// в каком их видит gosec.
//
// Повторяются три решения сканера, и каждое несущее:
//
//   - тег признаётся только в НАЧАЛЕ строки внутри группы комментариев
//     (`findNoSecTag`). Поэтому упоминание в прозе — «директива вида #nosec …» —
//     директивой не является, и гейт не считает собственное объяснение предметом;
//   - у одного узла засчитывается ПЕРВАЯ группа с директивой (`ignore` выходит на
//     ней). Без этого счёт разошёлся бы с `Stats.nosec` сканера;
//   - диапазон — объединение строк узла и группы (`updateIgnoredRulesForNode`).
//
// Файл, который не разбирается, возвращает ошибку: молчаливый пропуск сделал бы
// из него слепое пятно, а «ноль директив» там означало бы «не прочитано».
func FindDirectives(rel string, src []byte) ([]Directive, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	cmap := ast.NewCommentMap(fset, f, f.Comments)

	var out []Directive
	for n, groups := range cmap {
		for _, g := range groups {
			args, tagLine, ok := directiveArgs(fset, g)
			if !ok {
				continue
			}
			start, end := n.Pos(), n.End()
			if g.Pos() < start {
				start = g.Pos()
			}
			if g.End() > end {
				end = g.End()
			}
			rules, reason := parseArgs(args)
			out = append(out, Directive{
				File:   rel,
				Line:   tagLine,
				Start:  fset.Position(start).Line,
				End:    fset.Position(end).Line,
				Rules:  rules,
				Reason: reason,
			})
			break // как у сканера: один узел — одна директива
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// directiveArgs → (аргументы директивы, строка тега, найдена ли).
func directiveArgs(fset *token.FileSet, g *ast.CommentGroup) (string, int, bool) {
	if args, ok := findNoSecTag(g.Text()); ok {
		return args, tagLineIn(fset, g, nosecTag), true
	}
	// Второй диалект сканера проверяется покомментарийно, а не по тексту группы:
	// префикс обязан открывать сам комментарий.
	for _, c := range g.List {
		after, cut := strings.CutPrefix(c.Text, directivePrefix)
		if cut && (len(after) == 0 || after[0] == ' ') {
			return strings.TrimSpace(after), fset.Position(c.Pos()).Line, true
		}
	}
	return "", 0, false
}

// findNoSecTag повторяет одноимённую функцию gosec: тег засчитывается, только
// если он открывает текст группы либо стоит в начале строки внутри неё.
func findNoSecTag(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", false
	}
	if strings.HasPrefix(t, nosecTag) {
		return t[len(nosecTag):], true
	}
	idx := strings.Index(t, nosecTag)
	if idx <= 0 {
		return "", false
	}
	for i := idx - 1; i >= 0; i-- {
		if t[i] == '\n' {
			return t[idx+len(nosecTag):], true
		}
		if t[i] != ' ' && t[i] != '\t' {
			break
		}
	}
	return "", false
}

// tagLineIn — строка, на которой стоит сам тег. Группа бывает длинной, и её
// первая строка находкой не является: по такой координате читатель ищет не там.
func tagLineIn(fset *token.FileSet, g *ast.CommentGroup, tag string) int {
	for _, c := range g.List {
		for i, line := range strings.Split(c.Text, "\n") {
			s := strings.TrimLeft(line, "/* \t")
			if strings.HasPrefix(s, tag) {
				return fset.Position(c.Pos()).Line + i
			}
		}
	}
	return fset.Position(g.Pos()).Line
}

// parseArgs повторяет разбор аргументов из gosec `ignore`: причина отделяется
// первым `--`, правила ищутся как `G` плюс три цифры, пустой список и легаси
// `block` означают «все правила».
func parseArgs(args string) ([]string, string) {
	reason := ""
	if idx := strings.Index(args, "--"); idx > -1 {
		reason = strings.TrimSpace(strings.TrimLeft(args[idx+2:], "-"))
		args = args[:idx]
	}
	d := strings.TrimSpace(args)
	var rules []string
	if d != "" && d != "block" {
		for i := 0; i < len(d); {
			if d[i] == 'G' && i+4 <= len(d) {
				id, ok := d[i:i+4], true
				for j := 1; j < 4; j++ {
					if id[j] < '0' || id[j] > '9' {
						ok = false
						break
					}
				}
				if ok {
					rules = append(rules, id)
					i += 4
					continue
				}
			}
			i++
		}
	}
	if len(rules) == 0 {
		rules = []string{AllRules}
	}
	return rules, reason
}

// ── ПРЕДМЕТ ─────────────────────────────────────────────────────────────────

// SplitBySubject делит директивы на те, чьё правило по их координате
// срабатывает, и те, чьё — нет.
func SplitBySubject(dirs []Directive, findings []Finding) (live, inert []Directive) {
	byFile := map[string][]Finding{}
	for _, f := range findings {
		byFile[f.File] = append(byFile[f.File], f)
	}
	for _, d := range dirs {
		if hasSubject(d, byFile[d.File]) {
			live = append(live, d)
			continue
		}
		inert = append(inert, d)
	}
	return live, inert
}

func hasSubject(d Directive, findings []Finding) bool {
	for _, f := range findings {
		// Тот же предикат перекрытия, каким сканер выбирает диапазон
		// подавления (`ignores.get`): накрывает либо диапазон директивы находку,
		// либо находка — диапазон директивы.
		covers := d.Start <= f.Start && d.End >= f.End
		covered := f.Start <= d.Start && f.End >= d.End
		if !covers && !covered {
			continue
		}
		for _, r := range d.Rules {
			if r == AllRules || r == f.Rule {
				return true
			}
		}
	}
	return false
}

// SplitByScanned отделяет директивы в прочитанных сканером файлах от остальных.
// Вторые НЕ судятся: там отсутствие находки говорит об отсутствии осмотра.
func SplitByScanned(dirs []Directive, scanned map[string]bool) (judged, unjudged []Directive) {
	for _, d := range dirs {
		if scanned[d.File] {
			judged = append(judged, d)
			continue
		}
		unjudged = append(unjudged, d)
	}
	return judged, unjudged
}

// ── ВЕДОМОСТЬ ───────────────────────────────────────────────────────────────

var ruleKeyRe = regexp.MustCompile(`^(\*|G[0-9]{3}(,G[0-9]{3})*)$`)

// ParseLedger разбирает ведомость инертных директив.
//
// Причина обязательна и обязана быть ПРОЗОЙ, а не номером задачи. Подавление
// бывает принятым решением, а бывает проглоченным отказом, и машинно эти два
// случая неразличимы: различает их человек, и он обязан это написать. Гейт,
// требующий номер, покраснел бы на верном коде — и его отключили бы первым.
func ParseLedger(path string, body []byte) ([]LedgerRow, error) {
	var rows []LedgerRow
	for i, raw := range strings.Split(string(body), "\n") {
		lineno := i + 1
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			return nil, fmt.Errorf("%s:%d: полей %d, ожидалось 4 через табуляцию "+
				"(путь, правила, точное число, причина): %q", path, lineno, len(parts), line)
		}
		file, rule, countS := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		reason := strings.TrimSpace(parts[3])
		if file == "" {
			return nil, fmt.Errorf("%s:%d: путь пуст", path, lineno)
		}
		if !ruleKeyRe.MatchString(rule) {
			return nil, fmt.Errorf("%s:%d: %q не похоже на набор правил gosec "+
				"(`G304`, `G304,G703` или `*`)", path, lineno, rule)
		}
		count, err := strconv.Atoi(countS)
		if err != nil || count <= 0 {
			return nil, fmt.Errorf("%s:%d: %q не число больше нуля. Число здесь ТОЧНОЕ: "+
				"потолок не краснеет на сокращении долга и потому не истекает", path, lineno, countS)
		}
		if reason == "" {
			return nil, fmt.Errorf("%s:%d: запись без причины. Подавление бывает принятым "+
				"решением, а бывает проглоченным отказом — машинно они неразличимы, поэтому "+
				"различие обязан написать человек", path, lineno)
		}
		rows = append(rows, LedgerRow{File: file, Rule: rule, Count: count, Reason: reason, Line: lineno})
	}
	return rows, nil
}

// ApplyLedger сверяет ведомость с инертными директивами дерева.
//
// Исходов два, и оба обязаны быть находками:
//   - инертная директива, которой ведомость не называет, — новое подавление
//     без предмета;
//   - запись, которой больше нечего исключать (или чьё число разошлось), —
//     послабление, пережившее свой предмет. Именно этим ведомость истекает сама.
func ApplyLedger(rows []LedgerRow, inert []Directive) (uncovered []Directive, stale []LedgerRow) {
	actual := map[string]int{}
	for _, d := range inert {
		actual[(LedgerRow{File: d.File, Rule: d.RuleKey()}).key()]++
	}
	declared := map[string]bool{}
	for _, r := range rows {
		declared[r.key()] = true
		got := actual[r.key()]
		switch {
		case got == 0:
			r.Why = "исключать больше нечего: инертных директив по этой координате нет — " +
				"либо их сняли, либо предмет вернулся"
			stale = append(stale, r)
		case got != r.Count:
			r.Why = fmt.Sprintf("число разошлось: объявлено %d, инертных сегодня %d. "+
				"Число здесь ТОЧНОЕ, а не потолок", r.Count, got)
			stale = append(stale, r)
		}
	}
	for _, d := range inert {
		if !declared[(LedgerRow{File: d.File, Rule: d.RuleKey()}).key()] {
			uncovered = append(uncovered, d)
		}
	}
	return uncovered, stale
}

// ── ПРЕДПОСЫЛКИ ─────────────────────────────────────────────────────────────

// CheckRecognizerAgrees сверяет счёт гейта с собственным счётом сканера.
//
// Распознаватель здесь — вторая реализация того, что gosec делает у себя.
// Разойдись они, гейт судил бы не тот набор директив, и «ноль находок» означало
// бы «не туда смотрели». Сканер печатает своё число (`Stats.nosec`), поэтому
// сверять есть с чем — и это дешевле любого разбора.
func CheckRecognizerAgrees(module string, scanner, gate int) error {
	if scanner == gate {
		return nil
	}
	return fmt.Errorf("модуль %s: сканер насчитал директив %d, гейт — %d. "+
		"Распознаватель разошёлся со сканером: гейт судит не тот набор, и вердикт "+
		"по нему ничего не значит. Чинить надо распознаватель "+
		"(tools/gosecsubject: findNoSecTag / parseArgs), а не число",
		module, scanner, gate)
}

var checkingFileRe = regexp.MustCompile(`Checking file: (.+)$`)

// ParseScanLog достаёт из журнала скана перечень файлов, которые сканер
// ПРОЧИТАЛ, и сверяет его длину с переписью самого сканера.
//
// Перечень берётся у сканера, а не выводится заново из дерева: свои исключения
// (`-exclude-dir`), теги сборки и состав пакетов сканер применяет сам, и второе
// чтение разошлось бы с ним молча. Пустой перечень — отказ: иначе «вне осмотра»
// стало бы вердиктом обо всём дереве сразу, и он выглядел бы как чистота.
func ParseScanLog(r io.Reader, wantFiles int) ([]string, error) {
	var files []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if m := checkingFileRe.FindStringSubmatch(sc.Text()); m != nil {
			files = append(files, strings.TrimSpace(m[1]))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("журнал скана не дочитан: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("в журнале скана нет ни одной строки «Checking file:» — " +
			"перечень осмотренного пуст. Судить не о чем: «нет находки» и «не читали» " +
			"здесь неразличимы")
	}
	if wantFiles > 0 && len(files) != wantFiles {
		return nil, fmt.Errorf("сканер сообщает, что прочитал %d файлов, а в журнале "+
			"строк «Checking file:» — %d. Перечень осмотренного неполон, и вердикт "+
			"«вне осмотра» по нему был бы ложным", wantFiles, len(files))
	}
	return files, nil
}

// ── СБОРКА ──────────────────────────────────────────────────────────────────

// Module — один модуль дерева вместе с тем, что оставил по нему скан.
type Module struct {
	Dir     string
	Report  string
	ScanLog string
}

// Options — вход гейта.
type Options struct {
	Root      string
	Artifacts string // каталог, куда скан положил отчёты и перечень
	Ledger    string // путь к ведомости относительно корня
}

// ModuleCensus — объём осмотренного по одному модулю.
type ModuleCensus struct {
	Dir            string
	ScannedFiles   int
	Findings       int
	Suppressed     int
	ScannerNosec   int
	GateDirectives int
}

// Census — объём осмотренного целиком.
type Census struct {
	TrackedGoFiles int
	Candidates     int
	Parsed         int
	Directives     int
	Judged         int
	Unjudged       int
	Live           int
	Inert          int
	LedgerRows     int
	Modules        []ModuleCensus
}

// Report — вердикт.
type Report struct {
	Census    Census
	Uncovered []Directive
	Stale     []LedgerRow
	Unjudged  []Directive
}

type gosecReport struct {
	Issues []struct {
		RuleID       string `json:"rule_id"`
		File         string `json:"file"`
		Line         string `json:"line"`
		Suppressions []struct {
			Kind string `json:"kind"`
		} `json:"suppressions"`
	} `json:"Issues"`
	Stats struct {
		Files int `json:"files"`
		Nosec int `json:"nosec"`
	} `json:"Stats"`
}

// Scan исполняет гейт. Ошибка означает «судить не о чем» (третий исход);
// находки лежат в отчёте.
func Scan(opts Options) (*Report, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	mods, err := readManifest(filepath.Join(opts.Artifacts, ManifestName))
	if err != nil {
		return nil, err
	}
	if err := everyModuleIsInTheManifest(root, mods); err != nil {
		return nil, err
	}

	rep := &Report{}
	scanned := map[string]bool{}
	var findings []Finding
	perModuleScanner := map[string]int{}

	for _, m := range mods {
		raw, err := os.ReadFile(m.Report)
		if err != nil {
			return nil, fmt.Errorf("модуль %s: отчёт подавлений не прочитан: %w", m.Dir, err)
		}
		var doc gosecReport
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("модуль %s: отчёт подавлений не разбирается: %w", m.Dir, err)
		}
		logf, err := os.Open(m.ScanLog)
		if err != nil {
			return nil, fmt.Errorf("модуль %s: журнал скана не прочитан: %w", m.Dir, err)
		}
		files, err := ParseScanLog(logf, doc.Stats.Files)
		_ = logf.Close()
		if err != nil {
			return nil, fmt.Errorf("модуль %s: %w", m.Dir, err)
		}

		mc := ModuleCensus{Dir: m.Dir, Findings: len(doc.Issues), ScannerNosec: doc.Stats.Nosec}
		for _, f := range files {
			rel, ok := relTo(root, f)
			if !ok {
				return nil, fmt.Errorf("модуль %s: сканер прочитал %q — это вне корня %q. "+
					"Отчёты сняты не с того дерева, вердикт по ним был бы о чужом коде",
					m.Dir, f, root)
			}
			if !scanned[rel] {
				mc.ScannedFiles++
			}
			scanned[rel] = true
		}
		for _, is := range doc.Issues {
			rel, ok := relTo(root, is.File)
			if !ok {
				continue
			}
			for _, s := range is.Suppressions {
				if s.Kind == "inSource" {
					mc.Suppressed++
					break
				}
			}
			s, e := parseIssueLine(is.Line)
			findings = append(findings, Finding{File: rel, Rule: is.RuleID, Start: s, End: e})
		}
		perModuleScanner[m.Dir] = doc.Stats.Nosec
		rep.Census.Modules = append(rep.Census.Modules, mc)
	}

	tracked, err := trackedGoFiles(root)
	if err != nil {
		return nil, err
	}
	rep.Census.TrackedGoFiles = len(tracked)

	var dirs []Directive
	for _, rel := range tracked {
		// #nosec G304 -- путь склеен из корня осматриваемого дерева и ОТНОСИТЕЛЬНОГО
		// имени, пришедшего из индекса git этого же дерева; подставить посторонний
		// файл извне нечем.
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		// Предфильтр — НЕОБХОДИМОЕ условие, а не достаточное: всякая директива
		// содержит одну из двух подстрок, поэтому файл без них директив не несёт.
		// Достаточность решает разбор ниже, а не эта проверка.
		if !strings.Contains(string(body), nosecTag) && !strings.Contains(string(body), directivePrefix) {
			continue
		}
		rep.Census.Candidates++
		found, err := FindDirectives(rel, body)
		if err != nil {
			return nil, fmt.Errorf("файл-кандидат не разобран, а значит стал бы слепым "+
				"пятном гейта: %w", err)
		}
		rep.Census.Parsed++
		dirs = append(dirs, found...)
	}
	rep.Census.Directives = len(dirs)

	judged, unjudged := SplitByScanned(dirs, scanned)
	rep.Census.Judged, rep.Census.Unjudged = len(judged), len(unjudged)
	rep.Unjudged = unjudged

	// Предпосылка 1 — помодульно: гейт и сканер обязаны насчитать одно и то же
	// число директив на одном и том же наборе файлов.
	perModuleGate := map[string]int{}
	for _, d := range judged {
		perModuleGate[owningModule(mods, d.File)]++
	}
	for _, m := range mods {
		if err := CheckRecognizerAgrees(m.Dir, perModuleScanner[m.Dir], perModuleGate[m.Dir]); err != nil {
			return nil, err
		}
	}
	for i := range rep.Census.Modules {
		rep.Census.Modules[i].GateDirectives = perModuleGate[rep.Census.Modules[i].Dir]
	}

	live, inert := SplitBySubject(judged, findings)
	rep.Census.Live, rep.Census.Inert = len(live), len(inert)

	ledgerPath := filepath.Join(root, filepath.FromSlash(opts.Ledger))
	// #nosec G304 -- имя ведомости задаёт вызывающий гейта (в дереве это константа
	// точки входа), а корень — осматриваемое дерево; из запроса сюда не приходит
	// ничего.
	ledgerBody, err := os.ReadFile(ledgerPath)
	if err != nil {
		return nil, fmt.Errorf("ведомость %s не прочитана: %w", opts.Ledger, err)
	}
	rows, err := ParseLedger(opts.Ledger, ledgerBody)
	if err != nil {
		return nil, err
	}
	rep.Census.LedgerRows = len(rows)

	rep.Uncovered, rep.Stale = ApplyLedger(rows, inert)
	sort.Slice(rep.Uncovered, func(i, j int) bool {
		if rep.Uncovered[i].File != rep.Uncovered[j].File {
			return rep.Uncovered[i].File < rep.Uncovered[j].File
		}
		return rep.Uncovered[i].Line < rep.Uncovered[j].Line
	})
	sort.Slice(rep.Stale, func(i, j int) bool { return rep.Stale[i].Line < rep.Stale[j].Line })
	return rep, nil
}

// owningModule — самый длинный подходящий каталог модуля. Файл службы иначе
// достался бы корневому модулю, и счёт директив разошёлся бы у обоих.
func owningModule(mods []Module, rel string) string {
	best := ""
	for _, m := range mods {
		if m.Dir == "." {
			if best == "" {
				best = "."
			}
			continue
		}
		if strings.HasPrefix(rel, m.Dir+"/") && len(m.Dir) > len(best) {
			best = m.Dir
		}
	}
	return best
}

func readManifest(path string) ([]Module, error) {
	// #nosec G304 -- путь собран из каталога артефактов, который называет тот же
	// вызывающий, что и корень дерева; перечень пишет наш же скрипт скана.
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("перечень скана %s не прочитан: %w. "+
			"Скан не оставил того, что читает гейт: вердикт не выносится", ManifestName, err)
	}
	var out []Module
	for i, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(p) != 3 {
			return nil, fmt.Errorf("%s:%d: полей %d, ожидалось 3", ManifestName, i+1, len(p))
		}
		out = append(out, Module{Dir: p[0], Report: p[1], ScanLog: p[2]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s пуст — ни один модуль не сканировался. Это отказ, "+
			"а не чистота", ManifestName)
	}
	return out, nil
}

// everyModuleIsInTheManifest — второе, НЕЗАВИСИМОЕ чтение того же предмета.
// Модуль, вылетевший из скана, вылетел бы и из вердикта, и оба были бы уверены
// в своей правоте.
func everyModuleIsInTheManifest(root string, mods []Module) error {
	out, err := gitenv.Command(root, "ls-files", "*go.mod").Output()
	if err != nil {
		return fmt.Errorf("перечень модулей не выведен из индекса git: %w", err)
	}
	have := map[string]bool{}
	for _, m := range mods {
		have[m.Dir] = true
	}
	var missing []string
	for _, f := range strings.Fields(string(out)) {
		d := filepath.ToSlash(filepath.Dir(f))
		if !have[d] {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("модули Go, которых нет в перечне скана: %s. "+
			"Их директивы не судились бы вовсе, а вердикт выглядел бы полным",
			strings.Join(missing, ", "))
	}
	return nil
}

func trackedGoFiles(root string) ([]string, error) {
	out, err := gitenv.Command(root, "ls-files", "*.go").Output()
	if err != nil {
		return nil, fmt.Errorf("перечень файлов .go не выведен из индекса git: %w. "+
			"Обход диска читал бы и рабочие копии под .claude/, и вердикт стал бы "+
			"свойством чужого каталога, а не коммита", err)
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" {
			continue
		}
		files = append(files, filepath.ToSlash(f))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("в индексе git нет ни одного файла .go — обход пуст, " +
			"судить не о чем")
	}
	return files, nil
}

func relTo(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func parseIssueLine(l string) (int, int) {
	parts := strings.SplitN(l, "-", 2)
	s, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	e := s
	if len(parts) == 2 {
		if v, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			e = v
		}
	}
	return s, e
}

// Print печатает перепись ПЕРВОЙ. Вердикт без объёма, который за ним стоит, —
// то же самое утверждение, которое этот гейт и ловит.
// oneLine — причина директивы бывает многострочной; в перечне находок она
// обязана занимать одну строку, иначе координаты тонут в чужой прозе.
func oneLine(s string) string {
	f := strings.Fields(s)
	out := strings.Join(f, " ")
	if len(out) > 120 {
		return out[:117] + "..."
	}
	return out
}

func Print(r *Report, w io.Writer) {
	if r == nil {
		return
	}
	c := r.Census
	_, _ = fmt.Fprintf(w, "gosec-подавления: файлов .go в индексе %d; из них несут тег %d, разобрано %d;\n"+
		"  директив прочитано %d — судимых %d, вне осмотра %d;\n"+
		"  из судимых: с живым предметом %d, инертных %d; записей ведомости %d\n",
		c.TrackedGoFiles, c.Candidates, c.Parsed, c.Directives, c.Judged, c.Unjudged,
		c.Live, c.Inert, c.LedgerRows)
	for _, m := range c.Modules {
		_, _ = fmt.Fprintf(w, "  модуль %-16s осмотрено файлов %5d; находок %4d (подавлено %4d); "+
			"директив: сканер %3d, гейт %3d\n",
			m.Dir, m.ScannedFiles, m.Findings, m.Suppressed, m.ScannerNosec, m.GateDirectives)
	}
	if c.Unjudged > 0 {
		_, _ = fmt.Fprintf(w, "  вне осмотра %d директив — сканер этих файлов не читал "+
			"(флага -tests у скана нет намеренно). «Нет находки» там означает отсутствие\n"+
			"  ОСМОТРА, а не отсутствие предмета, поэтому они не судятся.\n", c.Unjudged)
	}
	if c.Inert == 0 && c.LedgerRows == 0 {
		_, _ = fmt.Fprintln(w, "  инертных директив нет и ведомость пуста — это цель механизма, "+
			"а не его простой: он сработает на следующей записи в тот же прогон")
	}
	for _, d := range r.Uncovered {
		_, _ = fmt.Fprintf(w, "  %s:%d: #nosec %s подавляет НИЧЕГО — правило по этой координате "+
			"не срабатывает (причина в директиве: %q)\n",
			d.File, d.Line, d.RuleKey(), oneLine(d.Reason))
	}
	for _, s := range r.Stale {
		_, _ = fmt.Fprintf(w, "  ведомость:%d: %s %s — %s\n", s.Line, s.File, s.Rule, s.Why)
	}
}
