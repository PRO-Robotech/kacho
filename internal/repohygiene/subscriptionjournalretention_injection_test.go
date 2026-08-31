// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionjournalretention_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт
// сверки полос СПОСОБЕН упасть, и падает ровно на своём предмете.
//
// Инъекция идёт в ОБЕ стороны по КАЖДОЙ оси: дефект вносится по одной оси за раз
// у полосы, чьи остальные оси целы, и рядом стоит законный близнец той же формы,
// на котором гейт обязан молчать. Инъекция вида «завести ещё одну полосу»
// отвергнута осознанно: новая полоса нарушала бы всё, что требуется от полос
// вообще, и красное приходило бы от соседней оси (`testing.md` §«Гейт на класс»,
// п. 2в).
package repohygiene

import (
	"strings"
	"testing"
)

// ageAlwaysOK / ageNeverOK — подставные ответы про схему владельца. Свойство
// колонки срока читается из миграций, и подать его функцией — единственный
// способ проверить ПРАВИЛО без дерева.
func ageAlwaysOK(JournalLane) bool { return true }
func ageNeverOK(JournalLane) bool  { return false }

// laneOK — законная полоса, чистящая журнал. Служит близнецом всем инъекциям:
// каждая портит у неё РОВНО ОДНУ ось.
func laneOK() JournalLane {
	return JournalLane{
		Owner:     "services/probe",
		Table:     "kacho_probe.probe_outbox",
		Retention: RetainsSweeping,
		AgeColumn: "created_at",
		Sweeper:   true,
	}
}

// laneRetainingOK — вторая законная форма: владелец удерживает всё и уборщика не
// несёт. Без неё правило зеленело бы только на чистящих полосах, а «удержание»
// осталось бы неосмотренным.
func laneRetainingOK() JournalLane {
	return JournalLane{
		Owner:     "services/probe2",
		Table:     "probe2_outbox",
		Retention: RetainsEverythingName,
	}
}

func TestLaneRuleIsSilentOnBothLegalForms(t *testing.T) {
	got := JournalLaneFindings([]JournalLane{laneOK(), laneRetainingOK()}, ageAlwaysOK)
	if len(got) != 0 {
		t.Fatalf("правило нашло дефект на ЗАКОННЫХ полосах — оно ловит форму, а не существо:\n  %s",
			strings.Join(got, "\n  "))
	}
}

func TestLaneRuleFindsEachDefectSeparately(t *testing.T) {
	cases := []struct {
		name    string
		lane    JournalLane
		ageOK   func(JournalLane) bool
		wantSub string
	}{
		{
			name: "объявлена чистка, уборщик не провязан",
			lane: func() JournalLane { l := laneOK(); l.Sweeper = false; return l }(),
			// Колонка срока и её умолчание целы — красное обязано прийти ровно от
			// отсутствия провязки, а не от соседней оси.
			ageOK:   ageAlwaysOK,
			wantSub: "уборщик НЕ провязан",
		},
		{
			name:    "уборщик провязан, объявлено удержание",
			lane:    func() JournalLane { l := laneRetainingOK(); l.Sweeper = true; return l }(),
			ageOK:   ageAlwaysOK,
			wantSub: "потеря МОЛЧАЛИВАЯ",
		},
		{
			name:    "чистка объявлена без колонки срока",
			lane:    func() JournalLane { l := laneOK(); l.AgeColumn = ""; return l }(),
			ageOK:   ageAlwaysOK,
			wantSub: "без колонки срока",
		},
		{
			name:    "колонка срока названа при удержании",
			lane:    func() JournalLane { l := laneRetainingOK(); l.AgeColumn = "created_at"; return l }(),
			ageOK:   ageAlwaysOK,
			wantSub: "которого не читает никто",
		},
		{
			name:    "колонка срока не от часов базы",
			lane:    laneOK(),
			ageOK:   ageNeverOK,
			wantSub: "не объявлена `DEFAULT now()`",
		},
		{
			name:    "удержание не объявлено вовсе",
			lane:    func() JournalLane { l := laneOK(); l.Retention = ""; return l }(),
			ageOK:   ageAlwaysOK,
			wantSub: "«не объявлено»",
		},
		{
			name:    "удержание объявлено значением вне словаря",
			lane:    func() JournalLane { l := laneOK(); l.Retention = "RetainsSomehow"; return l }(),
			ageOK:   ageAlwaysOK,
			wantSub: "которого разбор не знает",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := JournalLaneFindings([]JournalLane{tc.lane}, tc.ageOK)
			if len(got) == 0 {
				t.Fatal("правило смолчало на внесённом дефекте")
			}
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, tc.wantSub) {
				t.Fatalf("находка не называет предмет %q:\n%s", tc.wantSub, joined)
			}
			// Находка обязана называть ПОЛОСУ: перечень без имени владельца
			// заставляет искать виновника руками.
			if !strings.Contains(joined, tc.lane.Owner) {
				t.Fatalf("находка не называет владельца %q:\n%s", tc.lane.Owner, joined)
			}
			// И обязана быть ОДНОЙ: две находки означали бы, что инъекция уронила
			// заодно соседнюю ось, и вердикт нового предмета неотличим от чужого.
			if len(got) != 1 {
				t.Fatalf("инъекция одной оси дала %d находок — красное приходит не только от предмета:\n%s",
					len(got), joined)
			}
		})
	}
}

// TestLaneScannerReadsDeclarationAsASyntaxNode — разбор читает УЗЕЛ, а не слово.
//
// Обе половины утверждаются вместе: объявление в коде распознаётся, а то же слово
// в комментарии и в строке — нет. Без второй половины предикат по подстроке
// прошёл бы эту пробу и краснел бы на собственном объяснении в дереве.
func TestLaneScannerReadsDeclarationAsASyntaxNode(t *testing.T) {
	src := []byte(`package subscriptionjournal

import "github.com/PRO-Robotech/kacho/pkg/subscription"

// Здесь в прозе стоит RetainsFromEarliestRow и AgeColumn: "не читать".
const Table = "kacho_probe.probe_outbox"

const explain = "Retention: subscription.RetainsFromEarliestRow"

func Journal() subscription.Journal {
	return subscription.Journal{
		Storage: subscription.Storage{
			Table:          Table,
			PositionColumn: "sequence_no",
			Retention:      subscription.RetainsEverything,
		},
	}
}
`)
	lane, found, err := ScanJournalLane("services/probe", "journal.go", src)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !found {
		t.Fatal("объявление не распознано — разбор перестал видеть литерал subscription.Storage")
	}
	if lane.Retention != RetainsEverythingName {
		t.Fatalf("прочитано удержание %q, а объявлено %q: слово из комментария или строки "+
			"перевесило узел объявления", lane.Retention, RetainsEverythingName)
	}
	if lane.Table != "kacho_probe.probe_outbox" {
		t.Fatalf("имя таблицы не разобрано из константы пакета: %q", lane.Table)
	}
	if lane.AgeColumn != "" {
		t.Fatalf("колонка срока прочитана из комментария: %q", lane.AgeColumn)
	}
}

// TestSweepWiringIsRecognisedByTheCallNotTheWord — провязка опознаётся ВЫЗОВОМ.
//
// Пара обязательна: файл, где имя стоит только в комментарии и в строке, провязки
// НЕ несёт, а файл с вызовом — несёт. Односторонняя проба зеленела бы на
// предикате по подстроке, который в этом дереве краснел бы на прозе обоих
// владельцев.
func TestSweepWiringIsRecognisedByTheCallNotTheWord(t *testing.T) {
	prose := []byte(`package main

// Уборка журнала поднимается вызовом StartJournalRetentionSweep — но не здесь.
const doc = "subscription.StartJournalRetentionSweep"

func run() {}
`)
	wired, err := JournalSweepWiredIn("prose.go", prose)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if wired {
		t.Fatal("проза и строковый литерал засчитаны за провязку — предикат судит слово, а не вызов")
	}

	call := []byte(`package main

import "github.com/PRO-Robotech/kacho/pkg/subscription"

func run(ctx any, db any, j any, cfg any, log any) {
	_, _ = subscription.StartJournalRetentionSweep(ctx, db, j, cfg, log)
}
`)
	wired, err = JournalSweepWiredIn("call.go", call)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if !wired {
		t.Fatal("настоящий вызов не опознан — гейт объявил бы провязанную полосу непровязанной")
	}
}

// TestLaneRuleOnEmptyInputFindsNothing — правило на ПУСТОМ входе молчит.
//
// Пустой перечень полос — это «смотреть было не на что», и различает эти два
// состояния перепись гейта, а не правило: правило, падающее на пустом входе,
// заставило бы держать полосу ради зелёного.
func TestLaneRuleOnEmptyInputFindsNothing(t *testing.T) {
	if got := JournalLaneFindings(nil, ageAlwaysOK); len(got) != 0 {
		t.Fatalf("правило нашло дефект на пустом входе: %v", got)
	}
}
