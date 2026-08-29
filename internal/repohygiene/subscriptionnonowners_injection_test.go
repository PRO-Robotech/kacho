// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subscriptionnonowners_injection_test.go — доказательство, что гейт «домен либо
// служит глагол, либо несёт запись решения» СПОСОБЕН упасть.
//
// Каждая ось прогоняется дважды: на внесённом дефекте (обязано найтись, и находка
// обязана называть КООРДИНАТУ — домен либо документ) и на законном близнеце той же
// формы (обязано смолчать). Односторонняя проба зеленела бы и на гейте, который
// краснеет всегда.
//
// Инъекция подаёт синтетический вход СУДЯЩЕЙ функции [subscriptionOwnershipFindings],
// а не правит настоящее дерево: гейт в бою берёт состав у индекса git, и проба,
// пишущая в дерево запустившего её репозитория, портит чужое состояние.
//
// Инъекция ломает РОВНО проверяемое (п.2в корпуса): каждый случай берёт запись,
// у которой все прочие требования выполнены, и снимает ОДНО.

// okRecord — читатель документа, у которого всё в порядке.
func okRecord(string) (bool, error) { return true, nil }

// goodEntry — запись ведомости, выполняющая ВСЕ требования. Инъекция портит её по
// одному полю, чтобы красное приходило от проверяемого, а не от соседа.
func goodEntry(domain string) subscriptionNonOwner {
	return subscriptionNonOwner{
		Domain:  domain,
		Because: "причина названа",
		Issue:   4242,
		Record:  "docs/decision.md",
	}
}

// hasFindingNaming — есть ли находка, называющая координату.
func hasFindingNaming(findings []string, coord string) bool {
	for _, f := range findings {
		if strings.Contains(f, coord) {
			return true
		}
	}
	return false
}

func TestSubscriptionNonOwnerGateFallsAndStaysSilent(t *testing.T) {
	services := []string{"alpha", "bravo", "charlie"}
	// alpha служит глагол; bravo и charlie — нет.
	owners := []string{"alpha"}
	noTypes := map[string]int{"alpha": 3, "bravo": 0, "charlie": 0}

	// Контроль: всё цело — гейт МОЛЧИТ. Без него всякое красное ниже
	// неотличимо от гейта, краснеющего всегда.
	base := []subscriptionNonOwner{goodEntry("bravo"), goodEntry("charlie")}
	if got := subscriptionOwnershipFindings(services, owners, base, noTypes, okRecord); len(got) != 0 {
		t.Fatalf("контроль: на целом входе гейт обязан молчать, а он сказал %v", got)
	}

	cases := []struct {
		name    string
		owners  []string
		ledger  []subscriptionNonOwner
		types   map[string]int
		record  func(string) (bool, error)
		coord   string // координата, которую находка обязана назвать
		wantRed bool
	}{
		{
			name:    "домен без записи — находка",
			owners:  owners,
			ledger:  []subscriptionNonOwner{goodEntry("bravo")}, // charlie не записан
			types:   noTypes,
			record:  okRecord,
			coord:   "charlie",
			wantRed: true,
		},
		{
			name:    "законный близнец: владелец записи не требует",
			owners:  []string{"alpha", "charlie"},
			ledger:  []subscriptionNonOwner{goodEntry("bravo")},
			types:   noTypes,
			record:  okRecord,
			wantRed: false,
		},
		{
			name:    "запись пережила предмет: домен стал владельцем",
			owners:  []string{"alpha", "bravo"},
			ledger:  base,
			types:   noTypes,
			record:  okRecord,
			coord:   "bravo",
			wantRed: true,
		},
		{
			name:    "запись называет домен, которого в дереве нет",
			owners:  owners,
			ledger:  append(append([]subscriptionNonOwner{}, base...), goodEntry("delta")),
			types:   noTypes,
			record:  okRecord,
			coord:   "delta",
			wantRed: true,
		},
		{
			name:   "запись без причины",
			owners: owners,
			ledger: func() []subscriptionNonOwner {
				e := goodEntry("bravo")
				e.Because = "   "
				return []subscriptionNonOwner{e, goodEntry("charlie")}
			}(),
			types:   noTypes,
			record:  okRecord,
			coord:   "без причины",
			wantRed: true,
		},
		{
			name:   "запись без номера задачи",
			owners: owners,
			ledger: func() []subscriptionNonOwner {
				e := goodEntry("bravo")
				e.Issue = 0
				return []subscriptionNonOwner{e, goodEntry("charlie")}
			}(),
			types:   noTypes,
			record:  okRecord,
			coord:   "не называет задачи",
			wantRed: true,
		},
		{
			name:   "предпосылка умерла: типы модели прав появились",
			owners: owners,
			ledger: func() []subscriptionNonOwner {
				e := goodEntry("bravo")
				e.NoRightsModelTypes = true
				return []subscriptionNonOwner{e, goodEntry("charlie")}
			}(),
			types:   map[string]int{"alpha": 3, "bravo": 2, "charlie": 0},
			record:  okRecord,
			coord:   "предпосылка умерла",
			wantRed: true,
		},
		{
			name:   "законный близнец: предпосылка объявлена и ЖИВА",
			owners: owners,
			ledger: func() []subscriptionNonOwner {
				e := goodEntry("bravo")
				e.NoRightsModelTypes = true
				return []subscriptionNonOwner{e, goodEntry("charlie")}
			}(),
			types:   noTypes,
			record:  okRecord,
			wantRed: false,
		},
		{
			name:    "законный близнец: типы есть, но запись на них НЕ опирается",
			owners:  owners,
			ledger:  base,
			types:   map[string]int{"alpha": 3, "bravo": 2, "charlie": 0},
			record:  okRecord,
			wantRed: false,
		},
		{
			name:   "запись не называет документа",
			owners: owners,
			ledger: func() []subscriptionNonOwner {
				e := goodEntry("bravo")
				e.Record = ""
				return []subscriptionNonOwner{e, goodEntry("charlie")}
			}(),
			types:   noTypes,
			record:  okRecord,
			coord:   "не называет документа",
			wantRed: true,
		},
		{
			name:    "документ не читается",
			owners:  owners,
			ledger:  base,
			types:   noTypes,
			record:  func(string) (bool, error) { return false, fmt.Errorf("нет такого файла") },
			coord:   "docs/decision.md",
			wantRed: true,
		},
		{
			name:    "документ не называет адрес ручки",
			owners:  owners,
			ledger:  base,
			types:   noTypes,
			record:  func(string) (bool, error) { return false, nil },
			coord:   subscriptionHandlePath,
			wantRed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subscriptionOwnershipFindings(services, tc.owners, tc.ledger, tc.types, tc.record)
			switch {
			case tc.wantRed && len(got) == 0:
				t.Fatalf("дефект внесён, а гейт смолчал — он его не измеряет")
			case !tc.wantRed && len(got) != 0:
				t.Fatalf("законный близнец обязан молчать, а гейт сказал %v", got)
			case tc.wantRed && !hasFindingNaming(got, tc.coord):
				t.Fatalf("находка не называет координату %q: %v — читатель пойдёт искать не там",
					tc.coord, got)
			}
		})
	}
}

// TestRightsModelTypeCensusReadsDeclarationsOnly — счётчик типов модели прав судит
// ОБЪЯВЛЕНИЕ, а не упоминание: слово `type` стоит и в комментариях модели.
func TestRightsModelTypeCensusReadsDeclarationsOnly(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, subscriptionRightsModelRel)
	if err := os.MkdirAll(filepath.Dir(model), 0o750); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	body := strings.Join([]string{
		"model",
		"  schema 1.1",
		"type user",
		"type vpc_network",
		"type vpc_subnet",
		"  relations",
		"    # type geo_region здесь только НАЗВАН — объявлением это не является",
		"    define v_get: [user]",
		"type compute_instance",
	}, "\n")
	if err := os.WriteFile(model, []byte(body), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}

	byDomain, total, err := rightsModelTypesByDomain(dir)
	if err != nil {
		t.Fatalf("перепись модели: %v", err)
	}
	t.Logf("перепись: типов %d · по доменам %v", total, byDomain)

	if total != 4 {
		t.Errorf("объявлений типов 4, счётчик насчитал %d", total)
	}
	if byDomain["vpc"] != 2 || byDomain["compute"] != 1 {
		t.Errorf("по доменам ожидалось vpc:2 compute:1, вышло %v", byDomain)
	}
	// Несущее: упоминание в комментарии типом НЕ становится — иначе запись geo
	// «типов ноль» умирала бы от собственного объяснения.
	if n := byDomain["geo"]; n != 0 {
		t.Errorf("`geo` назван только в комментарии, а счётчик засчитал %d — он судит "+
			"слово, а не объявление, и запись, объясняющая своё отсутствие, убивала бы "+
			"собственную предпосылку", n)
	}

	// Отсутствие модели — отказ, а не тихий ноль: неоткрытый файл не есть
	// отсутствие типов.
	if _, _, err := rightsModelTypesByDomain(t.TempDir()); err == nil {
		t.Error("модели нет, а перепись ответила успехом — «ноль прочитанного» стало " +
			"неотличимо от «ноль объявленного»")
	}
}
