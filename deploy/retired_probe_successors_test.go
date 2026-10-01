// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retired_probe_successors_test.go — У СНИМАЕМОЙ И ПЕРЕПИСЫВАЕМОЙ ПРОБЫ ПОЛОСЫ
// ЛИЧНОСТИ, ЧЕЙ КЛАСС ОТКАЗА ВОСПРОИЗВОДИМ ПОД `own`, ЕСТЬ ПРЕЕМНИК — ПРОБА,
// ОБЪЯВЛЕННАЯ НА ГОЛОВЕ (kacho#2929; ведомость #2731; вход #1276).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Ведомость `docs/architecture/identity-probe-fate-census.md` назначает судьбу
// каждой пробе `deploy/identity_*_test.go`. Строка «снять» уносит вместе с
// пробой и класс отказа, который она ловила; если этот класс воспроизводим на
// НАШЕЙ посадке, его обязан держать кто-то другой — иначе снятие молча сузило
// бы защиту. Здесь названы преемники для строк, чей класс воспроизводим:
//
//	34     каждая полоса регистрации выдаёт сессию
//	48–51  дальше входа — только с подтверждённым адресом
//
// Эти пробы сняты kacho#2818 вместе со своими строками (правило ведомости:
// «снята проба — тем же изменением снимаются её строка»), и ведомость сама
// говорит, что держится ли их свойство в пробах службы, в этом дереве не
// измерено, — это измерение и есть предмет файла. Строки 16–17 (посадка без
// доменного имени) kacho#2818 переписал на наш носитель и оставил: класс держит
// сама проба, и преемника у неё нет.
//
// Утверждается: строка ведомости с этим номером, если она есть, называет тот
// файл, что здесь (ведомость, перенумерованная без правки этой таблицы, —
// находка), и её исход «снять» либо «переписать» (преемник у остающейся пробы —
// ошибка таблицы); строки нет — файла пробы в дереве тоже нет (иначе ведомость
// не знает пробы, которой назначен преемник); у строки есть хотя бы один
// преемник; каждый преемник ОБЪЯВЛЕН — функция верхнего уровня с этим именем
// разобрана в названном файле своего дерева; и преемник в платформе сам не
// снимается по ведомости.
//
// Деревья два: платформа (это дерево) и служба доступа — ПО ПИНУ go.mod, тем
// деревом, против которого платформа собирается и чей образ исполняет стенд.
//
// Вторая проба файла называет ревизию пина службы доступа — в go.mod и в образе
// зонта — и пробы Ф7 (`TestAccessKey_F7_*`), объявленные в этой ревизии.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Что преемник ЗЕЛЁН: судится объявление, вердикт даёт прогон самого преемника
// (платформа — этот же пакет и тег `helmcharts`; служба доступа — её конвейер
// на ревизии пина). Что преемник покрывает класс ЦЕЛИКОМ: где покрытие частично,
// это сказано полем «не покрывает» и печатается.
package deploy_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Деревья преемников.
const (
	successorTreePlatform = "kacho"
	successorTreeAccess   = "kaname"
)

// probeFateLedger — ведомость судьбы проб относительно пакета.
var probeFateLedger = filepath.Join("..", "docs", "architecture", "identity-probe-fate-census.md")

// fateRow — строка ведомости: номер, файл пробы, исход.
type fateRow struct {
	Number  int
	File    string
	Outcome string
}

// fateRowRef — строка ведомости, как её называет таблица преемников.
type fateRowRef struct {
	Number int
	File   string
}

// probeSuccessor — проба-преемник: дерево, путь от его корня, имя функции.
type probeSuccessor struct {
	Tree string
	File string
	Func string
}

// ledgerSuccession — строки ведомости одного свойства и их преемники.
type ledgerSuccession struct {
	Rows      []fateRowRef
	What      string
	Heirs     []probeSuccessor
	Uncovered string
}

// successorLocator — строка объявления пробы в дереве либо ошибка.
type successorLocator func(tree, file, fn string) (int, error)

// probePresence — лежит ли файл пробы (путь от `deploy/`) в дереве.
type probePresence func(file string) bool

// probeSuccessions — преемники строк 34 и 48–51 ведомости: пробы сняты
// kacho#2818 вместе со строками, их класс воспроизводим под `own`.
var probeSuccessions = []ledgerSuccession{
	{
		Rows: []fateRowRef{{34, "identity_registration_lanes_issue_a_session_test.go"}},
		What: "каждая полоса регистрации выдаёт сессию",
		Heirs: []probeSuccessor{
			{successorTreeAccess, "internal/check/registration_lanes_test.go", "TestRegistration_EveryLaneIssuesASession"},
			{successorTreeAccess, "internal/check/registration_lanes_injection_test.go", "TestRegistrationLanesGateRedsOnALaneWithoutASession"},
			{successorTreeAccess, "internal/handler/loginlanehttp/register_test.go", "TestLane_F4_01_RegisterIssuesTheSessionCookieAndANewFormContext"},
		},
	},
	{
		Rows: []fateRowRef{
			{48, "identity_verification_mirrors_the_requirement_injection_test.go"},
			{49, "identity_verification_mirrors_the_requirement_test.go"},
			{50, "identity_verified_address_required_on_both_lanes_injection_test.go"},
			{51, "identity_verified_address_required_on_both_lanes_test.go"},
		},
		What: "дальше входа — только с подтверждённым адресом; поток подтверждения собран на каждом стеке own",
		Heirs: []probeSuccessor{
			{successorTreeAccess, "internal/authzguard/address_gate_integration_test.go", "TestEV60_EveryPublicMethodRefusesTheUnverified"},
			{successorTreeAccess, "internal/handler/loginlanehttp/address_verification_lane_integration_test.go", "TestEV01_RegistrationIssuesAVerificationSessionAndQueuesTheLetter"},
			{successorTreePlatform, "deploy/address_verification_knobs_umbrella_test.go", "TestAddressVerificationKnobs_EveryOwnStackDeliversAllFive"},
			{successorTreePlatform, "deploy/address_verification_knobs_umbrella_test.go", "TestAddressVerificationKnobs_RemovedKnobReachesTheGuardUnset"},
			{successorTreePlatform, "deploy/address_gate_stand_render_test.go", "TestF6b54_ProdLandingCarriesNoReceiverAndFiveKnobsLiftNothing"},
		},
	},
}

// fateLedgerHeader — заголовок таблицы ведомости, ячейками. Документ несёт и
// другие таблицы с номером в первой ячейке (решение по гейтам признака, судьба
// проб вне ведомости — kacho#1276), со своими номерами и своим местом исхода;
// ведомость отличает от них только заголовок. По нему же её находит гейт
// документа (`identityProbeFateHeader` в internal/repohygiene/identityprobefate.go):
// заголовок, переписанный там и здесь не переписанный, — отказ «заголовка нет»,
// а не молчаливое чтение чужой таблицы.
var fateLedgerHeader = []string{"#", "файл", "что утверждает", "предпосылка", "исход", "основание (координата)"}

// ledgerCells — ячейки строки таблицы Markdown без крайних разделителей, либо
// nil, если строка таблицей не является.
func ledgerCells(line string) []string {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") || len(s) < 2 {
		return nil
	}
	cells := strings.Split(s[1:len(s)-1], "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// isLedgerHeader — строка является заголовком ведомости дословно.
func isLedgerHeader(cells []string) bool {
	if len(cells) != len(fateLedgerHeader) {
		return false
	}
	for i, c := range cells {
		if c != fateLedgerHeader[i] {
			return false
		}
	}
	return true
}

// parseLedgerRows — строки ведомости: таблица под заголовком `fateLedgerHeader`
// (ровно один в документе) до первой строки, таблицей не являющейся; строка —
// `| N | `файл` | … | исход | … |`, ровно шесть ячеек. Иная форма строки с
// номером — ОТКАЗ: исход читался бы не из своей ячейки. Заголовка нет или их два
// — ОТКАЗ: строки читались бы из чужой таблицы либо из одной из двух наугад.
func parseLedgerRows(doc string) (map[int]fateRow, error) {
	lines := strings.Split(doc, "\n")
	var headers []int
	for i, line := range lines {
		if isLedgerHeader(ledgerCells(line)) {
			headers = append(headers, i)
		}
	}
	switch len(headers) {
	case 0:
		return nil, fmt.Errorf("заголовка ведомости %q в документе нет — строки читались бы из чужой таблицы",
			"| "+strings.Join(fateLedgerHeader, " | ")+" |")
	case 1:
	default:
		return nil, fmt.Errorf("заголовков ведомости в документе %d (строки %v) — какая таблица судит, не установлено",
			len(headers), headers)
	}
	out := map[int]fateRow{}
	for i := headers[0] + 1; i < len(lines); i++ {
		cells := ledgerCells(lines[i])
		if cells == nil {
			break // таблица кончилась
		}
		n, err := strconv.Atoi(cells[0])
		if err != nil {
			continue // разделитель под заголовком
		}
		if len(cells) != len(fateLedgerHeader) {
			return nil, fmt.Errorf("строка %d ведомости с номером %d несёт %d ячеек, а не %d — форма сменилась",
				i+1, n, len(cells), len(fateLedgerHeader))
		}
		out[n] = fateRow{Number: n, File: strings.Trim(cells[1], "`"), Outcome: cells[4]}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("в ведомости не найдено ни одной строки — обход пуст")
	}
	return out, nil
}

// judgeSuccessions — ЯДРО: чистая функция от ведомости, таблицы, поиска и
// состава дерева. Строки нет, и файла пробы нет — проба снята вместе со строкой
// (правило ведомости), и преемник по-прежнему обязан быть объявлен; строки нет,
// а файл есть — ведомость не знает пробы, которой назначен преемник.
func judgeSuccessions(ledger map[int]fateRow, table []ledgerSuccession, locate successorLocator, present probePresence) (lines, findings []string) {
	removed := map[string]bool{}
	for _, r := range ledger {
		if r.Outcome == "снять" {
			removed["deploy/"+r.File] = true
		}
	}
	for _, s := range table {
		var nums, outcomes []string
		for _, ref := range s.Rows {
			nums = append(nums, strconv.Itoa(ref.Number))
			row, ok := ledger[ref.Number]
			switch {
			case !ok && !present(ref.File):
				outcomes = append(outcomes, "снята вместе со строкой")
				continue
			case !ok:
				findings = append(findings, fmt.Sprintf("строки %d в ведомости нет, а проба %s в дереве есть — "+
					"таблица преемников называет строку, которой нет", ref.Number, ref.File))
				continue
			case row.File != ref.File:
				findings = append(findings, fmt.Sprintf("строка %d: ведомость называет %s, таблица преемников — %s; "+
					"ведомость перенумерована, и преемник приписан другой пробе", ref.Number, row.File, ref.File))
			case row.Outcome != "снять" && row.Outcome != "переписать":
				findings = append(findings, fmt.Sprintf("строка %d (%s): исход «%s» — преемник назначен пробе, которая "+
					"остаётся", ref.Number, row.File, row.Outcome))
			}
			outcomes = append(outcomes, row.Outcome)
		}
		if len(s.Heirs) == 0 {
			findings = append(findings, fmt.Sprintf("строки %s: преемник не назван", strings.Join(nums, ",")))
			continue
		}
		var coords []string
		for _, h := range s.Heirs {
			if h.Tree == successorTreePlatform && removed[h.File] {
				findings = append(findings, fmt.Sprintf("строки %s: преемник %s сам снимается по ведомости — класс "+
					"уйдёт вместе с ним", strings.Join(nums, ","), h.File))
				continue
			}
			line, err := locate(h.Tree, h.File, h.Func)
			if err != nil {
				findings = append(findings, fmt.Sprintf("строки %s: преемник %s %s %s не объявлен — %v",
					strings.Join(nums, ","), h.Tree, h.File, h.Func, err))
				continue
			}
			coords = append(coords, fmt.Sprintf("%s %s:%d %s", h.Tree, h.File, line, h.Func))
		}
		l := fmt.Sprintf("строки %s (%s) — %s → %s", strings.Join(nums, ","), strings.Join(outcomes, ", "),
			s.What, strings.Join(coords, "; "))
		if s.Uncovered != "" {
			l += " · не покрывает: " + s.Uncovered
		}
		lines = append(lines, l)
	}
	return lines, findings
}

// declaredFuncLine — строка объявления функции верхнего уровня (без
// получателя) с этим именем, разбором файла. Имя в комментарии, в строке или
// у метода объявлением не является.
func declaredFuncLine(path string, src []byte, fn string) (int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, fmt.Errorf("%s не разбирается: %w", path, err)
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Recv == nil && fd.Name.Name == fn {
			return fset.Position(fd.Pos()).Line, nil
		}
	}
	return 0, fmt.Errorf("в %s функции %s верхнего уровня нет", path, fn)
}

// treeLocator — поиск объявления в дереве платформы и в пине службы доступа.
func treeLocator(platformRoot, accessRoot string) successorLocator {
	return func(tree, file, fn string) (int, error) {
		root := platformRoot
		if tree == successorTreeAccess {
			root = accessRoot
		}
		path := filepath.Join(root, filepath.FromSlash(file))
		src, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		return declaredFuncLine(path, src, fn)
	}
}

func TestRetiredIdentityProbeSuccessorsAreDeclaredOnHead(t *testing.T) {
	raw, err := os.ReadFile(probeFateLedger)
	if err != nil {
		t.Fatalf("ведомость %s не читается (%v) — назначать преемников не по чему", probeFateLedger, err)
	}
	ledger, err := parseLedgerRows(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	pin := productModulePins(t, "..")[kanameModulePart]
	present := func(file string) bool {
		_, err := os.Stat(file)
		return err == nil
	}
	lines, findings := judgeSuccessions(ledger, probeSuccessions, treeLocator("..", kanameModuleDir(t, "..")), present)
	heirs := 0
	for _, s := range probeSuccessions {
		heirs += len(s.Heirs)
	}
	t.Logf("перепись: строк ведомости %d · свойств с преемниками %d · преемников %d · пин службы доступа %s",
		len(ledger), len(probeSuccessions), heirs, pin)
	for _, l := range lines {
		t.Log("  " + l)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// f7Prefix — приставка проб Ф7 (привязка ключей доступа) в службе доступа.
const f7Prefix = "TestAccessKey_F7_"

func TestAccessServicePinNamesItsRevisionAndCarriesTheF7Probes(t *testing.T) {
	version := productModulePins(t, "..")[kanameModulePart]
	dir := kanameModuleDir(t, "..")
	revision := "—"
	if m := pseudoVersion.FindStringSubmatch(version); m != nil {
		revision = m[1]
	}

	pins, _ := outsourcedImagePins(t, "..")
	refs := map[string][]string{}
	agree := 0
	for _, p := range pins {
		if p.Module != productModuleprefix+kanameModulePart {
			continue
		}
		refs[p.Ref()] = append(refs[p.Ref()], p.Stand)
		if tagNamesVersion(p.Tag, p.Version) {
			agree++
		}
	}
	if len(refs) == 0 {
		t.Fatal("ни один стенд не объявляет образа службы доступа — ревизию в зонте назвать нечем")
	}
	var refLines []string
	stands := 0
	for ref, ss := range refs {
		sort.Strings(ss)
		stands += len(ss)
		refLines = append(refLines, fmt.Sprintf("%s (стендов %d: %s)", ref, len(ss), strings.Join(ss, ", ")))
	}
	sort.Strings(refLines)

	type f7Probe struct {
		File string
		Line int
		Name string
	}
	var f7 []f7Probe
	files := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(src, []byte("func "+f7Prefix)) {
			return err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("%s не разбирается: %w", p, err)
		}
		rel, _ := filepath.Rel(dir, p)
		found := false
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, f7Prefix) {
				f7 = append(f7, f7Probe{filepath.ToSlash(rel), fset.Position(fd.Pos()).Line, fd.Name.Name})
				found = true
			}
		}
		if found {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход пина службы доступа: %v", err)
	}
	sort.Slice(f7, func(i, j int) bool {
		if f7[i].File != f7[j].File {
			return f7[i].File < f7[j].File
		}
		return f7[i].Line < f7[j].Line
	})

	t.Logf("пин службы доступа: go.mod %s%s %s (ревизия %s) · образ в зонте: %s · согласие образа с "+
		"модулем %d из %d объявлений", productModuleprefix, kanameModulePart, version, revision,
		strings.Join(refLines, "; "), agree, stands)
	t.Logf("пробы Ф7 в ревизии пина: %d в %d файлах", len(f7), files)
	for _, p := range f7 {
		t.Logf("  %s:%d %s", p.File, p.Line, p.Name)
	}
	if len(f7) == 0 {
		t.Fatalf("в пине службы доступа %s не объявлено ни одной пробы %s* — привязка ключей доступа "+
			"(Ф7) в исполняемой ревизии ничем не держится", version, f7Prefix)
	}
	if agree != stands {
		t.Errorf("образ службы доступа в зонте согласен с пином модуля на %d из %d объявлений — ревизия, "+
			"которую исполняет стенд, не та, чьи пробы названы", agree, stands)
	}
}
