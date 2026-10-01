// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/pgtest"
)

// TestPg_SECE2_1_CountDoesNotDependOnTheOrderOfMoments — SEC-E2-1 на
// postgres: FREE + 3 решения одного источника пропускают ровно FREE при
// моментах и по возрастанию, и по убыванию (реплики флота сверены не точнее
// своих часов, и момент соседа бывает позже своего).
func TestPg_SECE2_1_CountDoesNotDependOnTheOrderOfMoments(t *testing.T) {
	l := orderLimits()
	for _, desc := range []bool{false, true} {
		s := newPgStore(t, edgeDB(t), l, discardLogger())
		got := orderedDecisions(t, s, "198.51.100.84", l.Source.Free+3, desc)
		_ = s.Close()
		if got != l.Source.Free {
			t.Errorf("моменты по убыванию=%v: пропущено %d, ожидалось FREE=%d", desc, got, l.Source.Free)
		}
	}
}

// TestPg_SECE2_1_RaceOnDifferentMomentsAcrossTwoReplicasPassesExactlyOne —
// близнец TestPg_CX2_11_RaceOnANewKeyAcrossTwoReplicasPassesExactlyOne,
// меняющий один факт: у каждого решения свой момент.
func TestPg_SECE2_1_RaceOnDifferentMomentsAcrossTwoReplicasPassesExactlyOne(t *testing.T) {
	dsn := edgeDB(t)
	l := orderLimits()
	l.Source.Free = 1
	a := newPgStore(t, dsn, l, discardLogger())
	b := newPgStore(t, dsn, l, discardLogger())
	defer func() { _ = a.Close(); _ = b.Close() }()
	if got := raceOnDifferentMoments(t, []Store{a, b}, "198.51.100.85", 24); got != 1 {
		t.Fatalf("пропущено %d из 24 на двух репликах, ожидался ровно один", got)
	}
}

// TestPg_GSE2_1_MissingSchemaIsLoggedAsMisconfiguration — база без цепочки
// миграций края (таблиц ограничителя нет): решение — 503, и журнал называет
// это неправильной настройкой уровнем ERROR с именем шага; клиентского адреса
// в журнале нет. Близнец — база с цепочкой: пропуск, ни строки ERROR.
func TestPg_GSE2_1_MissingSchemaIsLoggedAsMisconfiguration(t *testing.T) {
	run := func(t *testing.T, dsn string) (Verdict, string) {
		logs := &syncBuffer{}
		s := newPgStore(t, dsn, testLimits(), slog.New(slog.NewTextHandler(logs, nil)))
		defer func() { _ = s.Close() }()
		keys, _ := KeysFor("198.51.100.86")
		return s.Decide(context.Background(), Request{Keys: keys, Now: time.Now()}), logs.String()
	}
	v, logs := run(t, pgtest.NewDB(t))
	if v.Outcome != StoreUnavailable || v.Saturated {
		t.Fatalf("схемы нет: исход %s, насыщение %v", v.Outcome, v.Saturated)
	}
	if !strings.Contains(logs, "level=ERROR") || !strings.Contains(logs, "step=") || !strings.Contains(logs, "42P01") {
		t.Errorf("схемы нет, а журнал не называет неправильную настройку: %q", logs)
	}
	if strings.Contains(logs, "198.51.100") {
		t.Errorf("журнал несёт клиентский адрес: %q", logs)
	}
	tv, tlogs := run(t, edgeDB(t))
	if tv.Outcome != Pass || strings.Contains(tlogs, "level=ERROR") {
		t.Errorf("близнец — схема есть: исход %s, журнал %q", tv.Outcome, tlogs)
	}
	t.Logf("схемы нет: %s", strings.TrimSpace(logs))
}
