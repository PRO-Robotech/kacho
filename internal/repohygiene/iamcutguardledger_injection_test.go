// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iamcutguardledger_injection_test.go — опыт: способна ли ведомость упасть и
// способна ли она смолчать.
//
// Инъекция идёт по КАЖДОМУ требованию отдельно и меняет РОВНО ОДИН факт против
// законного близнеца. Дельта не объявляется, а вычисляется: контроль обязан
// быть пуст, инъекция — дать ровно одну находку.
//
// Отдельная ось — ПУСТАЯ ведомость: гейт обязан на ней ПРОЙТИ. Пустая ведомость
// есть цель разреза, а не поломка; отказ на ней подталкивал бы держать запись
// ради зелёного.
package repohygiene

import (
	"strings"
	"testing"
)

// ledgerTwin — законный близнец: по одной записи каждого исхода, все координаты
// резолвятся, преемники объявляют названные пробы.
func ledgerTwin() ([]iamCutGuard, iamCutLedgerFacts) {
	l := []iamCutGuard{
		{
			Carrier: "services/iam/internal/a/one_test.go", PlatformSubject: "gateway/x.json",
			Verdict: iamCutTransferred, SuccessorFile: "internal/repohygiene/one_test.go",
			SuccessorTest: "TestOne", Why: "перенесено целиком",
		},
		{
			Carrier: "services/iam/internal/b/two_test.go", PlatformSubject: "deploy/y.txt",
			Verdict: iamCutCovered, SuccessorFile: "deploy/two_test.go",
			SuccessorTest: "TestTwo", Why: "преемник уже был",
		},
		{
			Carrier: "services/iam/internal/c/three_test.go", PlatformSubject: "Makefile",
			Verdict: iamCutSubjectLeave, Why: "предмет уезжает со службой",
		},
		{
			Carrier: "services/iam/internal/d/four_test.go", PlatformSubject: "services",
			Verdict: iamCutRemainder, SuccessorIssue: 4242, Why: "остаток, задача заведена",
		},
		{
			Carrier: "services/iam/tools/five_test.go",
			Verdict: iamCutReclassified, Why: "координаты синтетические",
		},
		{
			Carrier: "services/iam/internal/e/six_test.go",
			Verdict: iamCutSubjectMoved, SuccessorFile: "services/iam/tools/six.sh",
			Why: "предмет перенесён к своему единственному исполнителю",
		},
	}
	f := iamCutLedgerFacts{
		ServicePresent: true,
		PathExists: map[string]bool{
			"services/iam/internal/a/one_test.go":   true,
			"services/iam/internal/b/two_test.go":   true,
			"services/iam/internal/c/three_test.go": true,
			"services/iam/internal/d/four_test.go":  true,
			"services/iam/tools/five_test.go":       true,
			"services/iam/internal/e/six_test.go":   true,
			"services/iam/tools/six.sh":             true,
			"gateway/x.json":                        true,
			"deploy/y.txt":                          true,
			"Makefile":                              true,
			"services":                              true,
			"internal/repohygiene/one_test.go":      true,
			"deploy/two_test.go":                    true,
		},
		TestsDeclaredBy: map[string][]string{
			"internal/repohygiene/one_test.go": {"TestOne"},
			"deploy/two_test.go":               {"TestTwo"},
		},
	}
	return l, f
}

func TestIamCutLedgerInjection_SilentOnALegitimateLedger(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	found, c := auditIamCutLedger(l, f)
	if len(found) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %s", strings.Join(found, "; "))
	}
	// Перенесённый предмет тоже читается — отдельной ветвью, поэтому преемников
	// прочитано три, а не два: два файла проб и один файл предмета.
	if c.Entries != 6 || c.CarriersJudged != 6 || c.SuccessorsRead != 3 {
		t.Fatalf("перепись не сходится с входом: %+v", c)
	}
}

// TestIamCutLedgerInjection_EmptyLedgerPasses — гейт не падает на ДОСТИЖЕНИИ
// своей цели.
func TestIamCutLedgerInjection_EmptyLedgerPasses(t *testing.T) {
	t.Parallel()
	found, c := auditIamCutLedger(nil, iamCutLedgerFacts{ServicePresent: true})
	if len(found) != 0 {
		t.Fatalf("пустая ведомость объявлена находкой: %v", found)
	}
	if c.Entries != 0 {
		t.Fatalf("перепись пустой ведомости не ноль: %+v", c)
	}
}

func TestIamCutLedgerInjection_FindsAMissingCarrier(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.PathExists["services/iam/internal/c/three_test.go"] = false
	assertExactlyOne(t, l, f, "носителя в дереве нет")
}

// TestIamCutLedgerInjection_CarrierNotJudgedAfterTheCut — после разреза половина
// носителей НЕ судится, и это говорится числом, а не молчанием.
func TestIamCutLedgerInjection_CarrierNotJudgedAfterTheCut(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.ServicePresent = false
	for k := range f.PathExists {
		if strings.HasPrefix(k, "services/iam/") {
			f.PathExists[k] = false
		}
	}
	found, c := auditIamCutLedger(l, f)
	if len(found) != 0 {
		t.Fatalf("исчезнувшая служба объявлена находкой: %v — после разреза это "+
			"объявленный исход, а не поломка", found)
	}
	if c.CarriersJudged != 0 {
		t.Fatalf("носители сверялись при отсутствующей службе: %d", c.CarriersJudged)
	}
	if c.SubjectsJudged == 0 {
		t.Fatal("предметы платформы перестали сверяться вместе со службой — " +
			"ведомость потеряла бы ровно ту половину, ради которой заведена")
	}
}

func TestIamCutLedgerInjection_FindsAVanishedPlatformSubject(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.PathExists["Makefile"] = false
	assertExactlyOne(t, l, f, "предмета платформы")
}

func TestIamCutLedgerInjection_FindsAnUnbuiltSuccessor(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.PathExists["internal/repohygiene/one_test.go"] = false
	assertExactlyOne(t, l, f, "перенос объявлен и не сделан")
}

// TestIamCutLedgerInjection_FindsASuccessorWithoutTheNamedTest — файл есть, а
// названной пробы в нём нет.
//
// Ось несущая: без неё «преемник назван» означало бы «файл существует», и
// перенос доказывался бы наличием пустого файла.
func TestIamCutLedgerInjection_FindsASuccessorWithoutTheNamedTest(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.TestsDeclaredBy["deploy/two_test.go"] = []string{"TestSomethingElse"}
	assertExactlyOne(t, l, f, "не объявляет пробы")
}

func TestIamCutLedgerInjection_FindsARemainderWithoutATask(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[3].SuccessorIssue = 0
	assertExactlyOne(t, l, f, "остаток без номера задачи")
}

func TestIamCutLedgerInjection_FindsALeavingSubjectThatNamesASuccessor(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[2].SuccessorIssue = 99
	assertExactlyOne(t, l, f, "у уезжающего предмета преемника не бывает")
}

func TestIamCutLedgerInjection_FindsAReclassifiedEntryThatKeepsASubject(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[4].PlatformSubject = "deploy/y.txt"
	assertExactlyOne(t, l, f, "переклассификация означает")
}

func TestIamCutLedgerInjection_FindsADuplicateCarrier(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l = append(l, iamCutGuard{
		Carrier: l[0].Carrier, PlatformSubject: "gateway/x.json",
		Verdict: iamCutSubjectLeave, Why: "второй исход об одном предмете",
	})
	assertExactlyOne(t, l, f, "назван ведомостью дважды")
}

func TestIamCutLedgerInjection_FindsAnUnknownVerdict(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[2].Verdict = "оставить как есть"
	found, _ := auditIamCutLedger(l, f)
	if !anyContains(found, "не из закрытого набора") {
		t.Fatalf("исход вне набора не назван находкой: %v", found)
	}
}

func TestIamCutLedgerInjection_FindsAnEntryWithoutAReason(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[1].Why = "   "
	assertExactlyOne(t, l, f, "без обоснования")
}

// assertExactlyOne — одно-фактная инъекция обязана дать РОВНО одну находку, и
// именно названную. Больше одной означает, что красное пришло от соседа, и
// доказательство стало совпадением.
func assertExactlyOne(t *testing.T, l []iamCutGuard, f iamCutLedgerFacts, want string) {
	t.Helper()
	base, _ := auditIamCutLedger(ledgerTwinOf(l), ledgerFactsTwin(f))
	if len(base) != 0 {
		t.Fatalf("контроль не пуст, дельта неизмерима: %v", base)
	}
	found, _ := auditIamCutLedger(l, f)
	if len(found) != 1 {
		t.Fatalf("одно-фактная инъекция дала %d находок, а обязана одну: %v", len(found), found)
	}
	if !strings.Contains(found[0], want) {
		t.Fatalf("находка не о том: %q, ожидалось про %q", found[0], want)
	}
}

// ledgerTwinOf / ledgerFactsTwin — контроль берётся ЗАНОВО, а не из
// испорченного входа: сравнение испорченного с самим собой дельты не даёт.
func ledgerTwinOf([]iamCutGuard) []iamCutGuard            { l, _ := ledgerTwin(); return l }
func ledgerFactsTwin(iamCutLedgerFacts) iamCutLedgerFacts { _, f := ledgerTwin(); return f }

func anyContains(hay []string, needle string) bool {
	for _, s := range hay {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// ── ось «предмет перенесён к службе» (#2378): четыре инъекции ───────────────
//
// Она отличается от всех прочих ЗНАКОМ требования к месту: у переноса
// утверждения преемник обязан лежать ВНЕ службы, у перенесённого предмета —
// ВНУТРИ. Обе стороны проверяются, иначе исход стал бы способом объявить
// перенос там, где ничего не переехало.

// TestIamCutLedgerInjection_FindsAMovedSubjectThatIsNotInTheTree — переезд
// объявлен, файла нет.
func TestIamCutLedgerInjection_FindsAMovedSubjectThatIsNotInTheTree(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.PathExists["services/iam/tools/six.sh"] = false
	assertExactlyOne(t, l, f, "переезд объявлен и не сделан")
}

// TestIamCutLedgerInjection_FindsAMovedSubjectKeepingAPlatformCoordinate —
// координата платформы у перенесённого предмета: две координаты одного предмета,
// а истекала бы только одна.
func TestIamCutLedgerInjection_FindsAMovedSubjectKeepingAPlatformCoordinate(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[5].PlatformSubject = "Makefile"
	assertExactlyOne(t, l, f, "всё ещё назван координатой платформы")
}

// TestIamCutLedgerInjection_FindsAMovedSubjectLandingOutsideTheService — «перенесён»
// туда, где он и был: у платформы. Тогда исполнителя у него по-прежнему нет.
func TestIamCutLedgerInjection_FindsAMovedSubjectLandingOutsideTheService(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[5].SuccessorFile = "deploy/six.sh"
	f.PathExists["deploy/six.sh"] = true
	assertExactlyOne(t, l, f, "он остался у платформы")
}

// TestIamCutLedgerInjection_FindsAMovedSubjectWithoutADestination — переезд без
// координаты: то же обещание, что и перенос, объявленный словами.
func TestIamCutLedgerInjection_FindsAMovedSubjectWithoutADestination(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	l[5].SuccessorFile = ""
	assertExactlyOne(t, l, f, "не называет, КУДА перенесён")
}

// TestIamCutLedgerInjection_MovedSubjectNotJudgedAfterTheCut — после разреза
// перенесённый предмет НЕ судится, и это обязано быть видно числом: файл уехал
// вместе со службой, и его отсутствие в дереве платформы есть норма, а не
// находка.
func TestIamCutLedgerInjection_MovedSubjectNotJudgedAfterTheCut(t *testing.T) {
	t.Parallel()
	l, f := ledgerTwin()
	f.ServicePresent = false
	f.PathExists["services/iam/tools/six.sh"] = false
	found, c := auditIamCutLedger(l, f)
	if len(found) != 0 {
		t.Fatalf("после разреза отсутствие перенесённого предмета объявлено находкой: %v", found)
	}
	if c.SuccessorsRead != 2 {
		t.Fatalf("перенесённый предмет зачтён прочитанным при отсутствии службы: %+v", c)
	}
}
