// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iamcutguardledger_test.go — гейт ведомости сторожей разреза (задача #2361).
//
// Что он требует от КАЖДОЙ записи, и почему каждое из требований истекает само:
//
//  1. НОСИТЕЛЬ СУЩЕСТВУЕТ — пока служба в дереве. Запись о носителе, которого
//     нет, описывает не дерево, а чью-то память: путь мог быть неверен с самого
//     начала, а носитель мог быть снят вместе со своим предметом. И то и другое
//     — находка, а не «стало лучше». После разреза службы в дереве не будет, и
//     эта половина честно объявляется НЕПРОВЕРЕННОЙ отдельной строкой переписи:
//     «ноль находок» обязано быть отличимо от «ноль прочитанного».
//  2. ПРЕДМЕТ ПЛАТФОРМЫ СУЩЕСТВУЕТ. Запись объяснена координатой, ради которой
//     она заведена. Исчезла координата — исчез предмет, и запись пережила его.
//  3. ПРЕЕМНИК СУЩЕСТВУЕТ И ОБЪЯВЛЯЕТ НАЗВАННУЮ ПРОБУ. Перенос, чей преемник
//     назван словами и не заведён, — то же обещание, что и маркер отсрочки:
//     за ним никто не отвечает, и он переживает своё основание. Проба ищется
//     РАЗБОРОМ, а не подстрокой: её имя стоит и в прозе этого файла, и предикат
//     по тексту зеленел бы на собственном объяснении.
//  4. У ОСТАТКА НАЗВАН НОМЕР ЗАДАЧИ. Остаток без задачи — молчание, а молчание
//     после разреза неотличимо от исправной работы.
//
// На ПУСТОЙ ведомости гейт проходит и говорит об этом: пустая ведомость — цель,
// а не поломка. Способность падать доказана инъекцией на синтетике, а не тем,
// что сегодня ведомость непуста.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// iamCutServiceDir — каталог службы. Пока он в дереве, носители проверяемы.
const iamCutServiceDir = "services/iam"

// iamCutLedgerFacts — вход предиката, отделённый от дерева ради инъекции.
type iamCutLedgerFacts struct {
	// ServicePresent — есть ли каталог службы в дереве.
	ServicePresent bool
	// PathExists — резолвится ли координата (носитель, предмет, преемник).
	PathExists map[string]bool
	// TestsDeclaredBy — имена проб, объявленных файлом-преемником.
	TestsDeclaredBy map[string][]string
}

// iamCutLedgerCensus — объём осмотренного, по осям.
type iamCutLedgerCensus struct {
	Entries        int
	ByVerdict      map[iamCutVerdict]int
	CarriersJudged int
	SubjectsJudged int
	SuccessorsRead int
}

// auditIamCutLedger — предикат четырёх требований. Пусто = норма.
func auditIamCutLedger(l []iamCutGuard, f iamCutLedgerFacts) ([]string, iamCutLedgerCensus) {
	c := iamCutLedgerCensus{Entries: len(l), ByVerdict: map[iamCutVerdict]int{}}
	var found []string

	seen := map[string]bool{}
	for _, e := range l {
		c.ByVerdict[e.Verdict]++

		if e.Carrier == "" {
			found = append(found, "запись без носителя — описывать нечего")
			continue
		}
		if seen[e.Carrier] {
			found = append(found, fmt.Sprintf(
				"%s: носитель назван ведомостью дважды — два исхода об одном предмете, "+
					"и действующим окажется прочитанный последним", e.Carrier))
		}
		seen[e.Carrier] = true

		if !strings.HasPrefix(e.Carrier, iamCutServiceDir+"/") {
			found = append(found, fmt.Sprintf(
				"%s: носитель вне %s — ведомость о разрезе службы, и запись о чужом "+
					"дереве в ней не истекает ничем", e.Carrier, iamCutServiceDir))
		}

		// 1. НОСИТЕЛЬ. Судится, только пока служба в дереве.
		if f.ServicePresent {
			c.CarriersJudged++
			if !f.PathExists[e.Carrier] {
				found = append(found, fmt.Sprintf(
					"%s: носителя в дереве нет — запись описывает не дерево, а память; "+
						"путь неверен либо носитель снят, и запись пережила предмет", e.Carrier))
			}
		}

		// 2. ПРЕДМЕТ ПЛАТФОРМЫ.
		switch {
		case e.Verdict == iamCutReclassified:
			if e.PlatformSubject != "" {
				found = append(found, fmt.Sprintf(
					"%s: переклассифицированная запись называет предмет платформы %q — "+
						"переклассификация означает, что предмета нет вовсе",
					e.Carrier, e.PlatformSubject))
			}
		case e.PlatformSubject == "":
			found = append(found, fmt.Sprintf(
				"%s: исход %q без координаты предмета — истекать записи нечем",
				e.Carrier, e.Verdict))
		default:
			c.SubjectsJudged++
			if !f.PathExists[e.PlatformSubject] {
				found = append(found, fmt.Sprintf(
					"%s: предмета платформы %s в дереве нет — запись пережила то, ради чего "+
						"заведена, и её надо снять, а не оставить",
					e.Carrier, e.PlatformSubject))
			}
		}

		// 3 и 4. ПРЕЕМНИК.
		switch e.Verdict {
		case iamCutTransferred, iamCutCovered:
			if e.SuccessorFile == "" || e.SuccessorTest == "" {
				found = append(found, fmt.Sprintf(
					"%s: исход %q без названного преемника — обещание, за которым никто не отвечает",
					e.Carrier, e.Verdict))
				break
			}
			c.SuccessorsRead++
			if !f.PathExists[e.SuccessorFile] {
				found = append(found, fmt.Sprintf(
					"%s: преемник %s в дереве не найден — перенос объявлен и не сделан",
					e.Carrier, e.SuccessorFile))
				break
			}
			if !ledgerNames(f.TestsDeclaredBy[e.SuccessorFile], e.SuccessorTest) {
				found = append(found, fmt.Sprintf(
					"%s: %s не объявляет пробы %s — преемник назван, но утверждения в нём нет",
					e.Carrier, e.SuccessorFile, e.SuccessorTest))
			}
		case iamCutRemainder:
			if e.SuccessorIssue <= 0 {
				found = append(found, fmt.Sprintf(
					"%s: остаток без номера задачи-преемника — молчание после разреза "+
						"неотличимо от исправной работы", e.Carrier))
			}
		case iamCutSubjectLeave, iamCutReclassified:
			if e.SuccessorFile != "" || e.SuccessorTest != "" || e.SuccessorIssue != 0 {
				found = append(found, fmt.Sprintf(
					"%s: исход %q называет преемника — у уезжающего предмета преемника не бывает",
					e.Carrier, e.Verdict))
			}
		default:
			found = append(found, fmt.Sprintf(
				"%s: исход %q не из закрытого набора — «оставить как есть» исходом не является",
				e.Carrier, e.Verdict))
		}

		if strings.TrimSpace(e.Why) == "" {
			found = append(found, fmt.Sprintf(
				"%s: запись без обоснования — читатель не восстановит, почему исход именно этот",
				e.Carrier))
		}
	}
	sort.Strings(found)
	return found, c
}

// ledgerNames — объявлено ли имя в перечне.
func ledgerNames(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// iamCutLedgerFactsFromTree — факты, снятые с дерева по индексу git.
func iamCutLedgerFactsFromTree(t *testing.T, root string, tt *trackedTree, l []iamCutGuard) iamCutLedgerFacts {
	t.Helper()
	f := iamCutLedgerFacts{
		PathExists:      map[string]bool{},
		TestsDeclaredBy: map[string][]string{},
	}
	for rel := range tt.files {
		if strings.HasPrefix(rel, iamCutServiceDir+"/") {
			f.ServicePresent = true
			break
		}
	}

	resolve := func(rel string) {
		if rel == "" {
			return
		}
		if _, done := f.PathExists[rel]; done {
			return
		}
		// Координатой бывает и файл, и каталог: `services` — каталог, а
		// `deploy/stacks.txt` — файл. Индекс git каталогов не несёт, поэтому
		// каталог опознаётся приставкой пути.
		if tt.hasFile(rel) {
			f.PathExists[rel] = true
			return
		}
		for other := range tt.files {
			if strings.HasPrefix(other, rel+"/") {
				f.PathExists[rel] = true
				return
			}
		}
		f.PathExists[rel] = false
	}

	for _, e := range l {
		resolve(e.Carrier)
		resolve(e.PlatformSubject)
		resolve(e.SuccessorFile)
		if e.SuccessorFile == "" || !f.PathExists[e.SuccessorFile] {
			continue
		}
		if _, done := f.TestsDeclaredBy[e.SuccessorFile]; done {
			continue
		}
		f.TestsDeclaredBy[e.SuccessorFile] = declaredTestNames(t, filepath.Join(root, filepath.FromSlash(e.SuccessorFile)))
	}
	return f
}

// declaredTestNames — имена проб, объявленных файлом. Разбор, а не поиск по
// тексту: имя пробы стоит и в прозе, и предикат по подстроке зеленел бы на
// комментарии, объясняющем эту же пробу.
func declaredTestNames(t *testing.T, abs string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, abs, nil, 0)
	if err != nil {
		t.Fatalf("разбор преемника %s: %v — нечитаемый файл есть НАХОДКА, "+
			"а не «пробы в нём нет»", abs, err)
	}
	var out []string
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil {
			continue
		}
		if strings.HasPrefix(fn.Name.Name, "Test") {
			out = append(out, fn.Name.Name)
		}
	}
	return out
}

// TestIamCutGuardLedgerIsHonest — сам гейт.
func TestIamCutGuardLedgerIsHonest(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	f := iamCutLedgerFactsFromTree(t, root, tt, iamCutGuardLedger)
	found, c := auditIamCutLedger(iamCutGuardLedger, f)

	byVerdict := make([]string, 0, len(c.ByVerdict))
	for _, v := range []iamCutVerdict{
		iamCutTransferred, iamCutCovered, iamCutSubjectLeave, iamCutRemainder, iamCutReclassified,
	} {
		byVerdict = append(byVerdict, fmt.Sprintf("%s %d", v, c.ByVerdict[v]))
	}
	carriers := fmt.Sprintf("носителей сверено %d", c.CarriersJudged)
	if !f.ServicePresent {
		carriers = "носителей НЕ сверялось: каталога службы в дереве нет (разрез состоялся)"
	}
	t.Logf("перепись: записей %d (%s); %s; предметов сверено %d; преемников прочитано %d",
		c.Entries, strings.Join(byVerdict, " · "), carriers, c.SubjectsJudged, c.SuccessorsRead)

	if c.Entries == 0 {
		t.Log("ведомость пуста — разрез разобран целиком; это ЦЕЛЬ, а не поломка")
		return
	}
	if len(found) > 0 {
		t.Fatalf("ведомость сторожей разреза разошлась с деревом — %d находка(и):\n  %s\n\n"+
			"Запись, потерявшая носителя, предмет или преемника, унаследует следующую "+
			"слепую зону: молчащий сторож неотличим от исправного.",
			len(found), strings.Join(found, "\n  "))
	}
}

// TestIamCutGuardLedgerCoversTheAdjudication — ведомость покрывает ВЕСЬ перечень
// адъюдикации, а не его удобную часть.
//
// Перечень, разобранный частично, читается как разобранный полностью — это
// отдельный вред, и он не виден ни по одной находке. Поэтому число записей
// закреплено: адъюдикация #2330 назвала СЕМНАДЦАТЬ носителей, и ведомость обязана
// нести ровно столько. Уменьшилось — часть перечня выпала молча; выросло — либо
// нашлись новые, и тогда правится и это число вместе с перечнем, либо запись
// задвоена (что ловит гейт выше).
func TestIamCutGuardLedgerCoversTheAdjudication(t *testing.T) {
	t.Parallel()
	const adjudicated = 17
	if len(iamCutGuardLedger) != adjudicated {
		t.Fatalf("ведомость несёт %d записей при %d адъюдицированных носителях — "+
			"перечень, разобранный частично, читается как разобранный полностью",
			len(iamCutGuardLedger), adjudicated)
	}
	t.Logf("перепись: адъюдицировано %d, в ведомости %d", adjudicated, len(iamCutGuardLedger))
}

// TestIamCutGuardLedgerSuccessorsAreOutsideTheService — преемник живёт ВНЕ
// службы.
//
// Ось несущая: преемник внутри службы уехал бы вместе с ней, и «перенесено»
// означало бы «переложено на ту же полку». Проверяется отдельно от гейта выше,
// потому что существование файла и его МЕСТО — разные утверждения.
func TestIamCutGuardLedgerSuccessorsAreOutsideTheService(t *testing.T) {
	t.Parallel()
	var findings []string
	named := 0
	for _, e := range iamCutGuardLedger {
		if e.SuccessorFile == "" {
			continue
		}
		named++
		if strings.HasPrefix(e.SuccessorFile, iamCutServiceDir+"/") {
			findings = append(findings, fmt.Sprintf(
				"%s: преемник %s лежит внутри службы — он уедет вместе с нею, и перенос "+
					"ничего не переносит", e.Carrier, e.SuccessorFile))
		}
	}
	t.Logf("перепись: записей с названным преемником %d", named)
	if named == 0 {
		t.Fatal("ни одна запись не называет преемника-файла — ось беспредметна")
	}
	if len(findings) > 0 {
		t.Fatalf("преемник внутри службы — %d находка(и):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
