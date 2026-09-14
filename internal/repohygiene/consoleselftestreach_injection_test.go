// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleselftestreach_injection_test.go — ПРОБЫ СОБСТВЕННОЙ ПРЕДПОСЫЛКИ ГЕЙТА
// достижимости самопроверок консоли.
//
// Гейт, не доказавший, что умеет краснеть, доказательством не является. Суждение
// вынесено в `adjudicateConsoleSelftestReach`, поэтому обе стороны КАЖДОЙ ветви
// утверждаются синтетическим входом, а не порчей рабочей копии: испорченная
// копия проверяет одну ветвь и оставляет остальные без свидетеля.
package repohygiene

import (
	"strings"
	"testing"
)

func TestConsoleReachGateRedOnSelftestNobodyCalls(t *testing.T) {
	t.Parallel()
	findings := adjudicateConsoleSelftestReach([]consoleSelftest{
		{Path: "ui-future/e2e/scripts/a-selftest.ts", Base: "a-selftest.ts", CalledBy: []string{"console-e2e.yml"}},
		{Path: "ui-future/e2e/scripts/b-selftest.ts", Base: "b-selftest.ts"},
	}, map[string]string{})
	if len(findings) != 1 {
		t.Fatalf("находок %d, ждали ровно одну про b-selftest.ts: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "b-selftest.ts") {
		t.Errorf("находка не называет КООРДИНАТУ незвонимой самопроверки: %s", findings[0])
	}
	if strings.Contains(findings[0], "a-selftest.ts") {
		t.Errorf("находка приписана звонимой самопроверке: %s", findings[0])
	}
}

// Законный близнец: тот же набор, отличающийся РОВНО одним фактом — вызов есть.
// Без него краснота выше доказывала бы лишь то, что гейт умеет краснеть, но не
// то, что краснеет он на отсутствии вызова.
func TestConsoleReachGateSilentWhenEverySelftestIsCalled(t *testing.T) {
	t.Parallel()
	findings := adjudicateConsoleSelftestReach([]consoleSelftest{
		{Path: "ui-future/e2e/scripts/a-selftest.ts", Base: "a-selftest.ts", CalledBy: []string{"console-e2e.yml"}},
		{Path: "ui-future/e2e/scripts/b-selftest.ts", Base: "b-selftest.ts", CalledBy: []string{"console-e2e.yml"}},
	}, map[string]string{})
	if len(findings) != 0 {
		t.Errorf("гейт краснеет на дереве, где зовётся каждая самопроверка: %v", findings)
	}
}

// ПУСТОЙ ОБХОД — ОТКАЗ, А НЕ ВСЕРАЗРЕШЕНИЕ, и он обязан наступать РАНЬШЕ
// ведомости: иначе пустота уезжает в послабление и замолкает.
func TestConsoleReachGateRedOnEmptyWalk(t *testing.T) {
	t.Parallel()
	findings := adjudicateConsoleSelftestReach(nil, map[string]string{})
	if len(findings) != 1 {
		t.Fatalf("на пустом обходе находок %d, ждали ровно одну: %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "обход пуст") {
		t.Errorf("отказ на пустом обходе не называет ПРИЧИНУ: %s", findings[0])
	}
}

func TestConsoleReachGateRedOnEmptyWalkEvenWhenTheRosterWouldForgiveEverything(t *testing.T) {
	t.Parallel()
	// Ведомость прощает всё, что могло бы найтись. Пустота обязана пережить это:
	// послабление относится к ПРОЧИТАННОМУ, а прочитано ничего.
	findings := adjudicateConsoleSelftestReach(nil, map[string]string{
		"ui-future/e2e/scripts/a-selftest.ts": "прощено ради пробы",
	})
	if len(findings) == 0 {
		t.Fatal("пустой обход прошёл под ведомостью — послабление съело пустоту")
	}
	if !strings.Contains(findings[0], "обход пуст") {
		t.Errorf("первая находка не про пустоту, значит порядок ветвей потерян: %s", findings[0])
	}
}

func TestConsoleReachGateSilentOnRosterEntryThatStillHasASubject(t *testing.T) {
	t.Parallel()
	findings := adjudicateConsoleSelftestReach([]consoleSelftest{
		{Path: "ui-future/e2e/scripts/b-selftest.ts", Base: "b-selftest.ts"},
	}, map[string]string{"ui-future/e2e/scripts/b-selftest.ts": "предмет ещё есть"})
	if len(findings) != 0 {
		t.Errorf("запись ведомости с живым предметом объявлена находкой: %v", findings)
	}
}

func TestConsoleReachGateRedOnRosterEntryThatLostItsSubject(t *testing.T) {
	t.Parallel()
	// Вызов появился — прощать больше нечего.
	appeared := adjudicateConsoleSelftestReach([]consoleSelftest{
		{Path: "ui-future/e2e/scripts/b-selftest.ts", Base: "b-selftest.ts", CalledBy: []string{"console-e2e.yml"}},
	}, map[string]string{"ui-future/e2e/scripts/b-selftest.ts": "устарело"})
	if len(appeared) != 1 || !strings.Contains(appeared[0], "потеряла предмет") {
		t.Errorf("запись, которой нечего прощать, не объявлена находкой: %v", appeared)
	}

	// Файла нет вовсе — зеркало ведомости.
	gone := adjudicateConsoleSelftestReach([]consoleSelftest{
		{Path: "ui-future/e2e/scripts/a-selftest.ts", Base: "a-selftest.ts", CalledBy: []string{"console-e2e.yml"}},
	}, map[string]string{"ui-future/e2e/scripts/vanished-selftest.ts": "устарело"})
	if len(gone) != 1 || !strings.Contains(gone[0], "vanished-selftest.ts") {
		t.Errorf("запись о несуществующем файле не объявлена находкой: %v", gone)
	}
}

// ПРЕДИКАТ ВЫЗОВА судится отдельно: он и есть то место, где гейт слепнет тише
// всего. Упоминание имени в объясняющем комментарии вызовом НЕ является —
// иначе гейт остался бы зелёным на снятом вызове, покраснев на собственном
// объяснении рядом.
func TestConsoleSelftestCallPredicateSeparatesCallFromMention(t *testing.T) {
	t.Parallel()
	re := consoleSelftestCallRe("quota-posture-selftest.ts")

	called := "      - name: гейт\n        id: selftest-quota-posture\n" +
		"        working-directory: ui-future/e2e\n" +
		"        run: node scripts/quota-posture-selftest.ts\n"
	if !re.MatchString(called) {
		t.Error("настоящий вызов не опознан — гейт краснел бы на исправном дереве")
	}

	mentioned := "      # образец рядом: node scripts/quota-posture-selftest.ts стоит до стенда\n" +
		"      - name: что-то другое\n        run: echo ok\n"
	if re.MatchString(mentioned) {
		t.Error("упоминание в комментарии зачтено за вызов — гейт зеленел бы на снятом вызове")
	}

	other := "        run: node scripts/stream-verdict-selftest.ts\n"
	if re.MatchString(other) {
		t.Error("вызов ДРУГОЙ самопроверки зачтён за свой — координаты перепутаны")
	}
}

// Собственная предпосылка чтения дерева: обход обязан быть НЕПУСТЫМ числом, а не
// печатью. Проба падает ровно там, где `collectConsoleSelftests` разошёлся бы с
// раскладкой дерева, — и тогда все остальные пробы этого файла судили бы
// синтетику, ничего не говоря о репозитории.
func TestConsoleReachCollectorActuallyReadsTheTree(t *testing.T) {
	t.Parallel()
	found := collectConsoleSelftests(t, repoRoot(t))
	if len(found) == 0 {
		t.Fatal("обход дерева дал НОЛЬ самопроверок консоли: предикат разошёлся с " +
			"раскладкой, и зелёный гейт означал бы «ничего не прочитано»")
	}
	withCall := 0
	for _, s := range found {
		if len(s.CalledBy) > 0 {
			withCall++
		}
	}
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: самопроверок консоли %d, из них с вызовом %d",
		len(found), withCall)
	if withCall == 0 {
		t.Fatal("ни одна самопроверка не опознана как звонимая: предикат вызова " +
			"разошёлся с формой шага, и всякая находка гейта была бы о его слепоте")
	}
}
