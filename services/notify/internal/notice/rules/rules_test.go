// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package rules

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}

func equalTimes(a, b []time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// TestReminders_RuleOfP5 — правило Р5 по каждому виду: MAINTENANCE — одно за
// lead; DECOMMISSION — три, по возрастанию; прошедшие к now не назначаются;
// прочие виды напоминаний не имеют.
func TestReminders_RuleOfP5(t *testing.T) {
	t.Parallel()
	now := at("2026-10-01T00:00:00Z")
	cases := []struct {
		name  string
		kind  Kind
		start string
		want  []string
	}{
		{"MAINTENANCE за 24 ч", KindMaintenance, "2026-10-03T02:00:00Z", []string{"2026-10-02T02:00:00Z"}},
		{"MAINTENANCE: момент прошёл", KindMaintenance, "2026-10-01T12:00:00Z", nil},
		{"MAINTENANCE: момент ровно now не назначается", KindMaintenance, "2026-10-02T00:00:00Z", nil},
		{"DECOMMISSION: три", KindDecommission, "2026-11-10T00:00:00Z",
			[]string{"2026-10-11T00:00:00Z", "2026-11-03T00:00:00Z", "2026-11-09T00:00:00Z"}},
		{"DECOMMISSION: −30 сут прошёл", KindDecommission, "2026-10-20T00:00:00Z",
			[]string{"2026-10-13T00:00:00Z", "2026-10-19T00:00:00Z"}},
		{"TERMS_CHANGE: нет", KindTermsChange, "2026-11-10T00:00:00Z", nil},
		{"OUTAGE: нет", KindOutage, "2026-10-01T00:00:00Z", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var want []time.Time
			for _, s := range c.want {
				want = append(want, at(s))
			}
			if got := Reminders(c.kind, at(c.start), now, 24*time.Hour); !equalTimes(got, want) {
				t.Fatalf("Reminders(%s, %s) = %v, ожидалось %v", c.kind, c.start, got, want)
			}
		})
	}
}

// TestKindTable_CoversEveryKindOfTheContract — у каждого вида перечня есть
// строка, и поля строки несут ровно таблицу Р4.
func TestKindTable_CoversEveryKindOfTheContract(t *testing.T) {
	t.Parallel()
	want := map[Kind]KindRule{
		KindMaintenance:      {Required, Required, StateScheduled, CategoryOperations, true, true, StageScheduled},
		KindOutage:           {Forbidden, Forbidden, StateInProgress, CategoryOperations, true, true, StageStarted},
		KindDecommission:     {Required, Forbidden, StateScheduled, CategoryOperations, true, true, StageScheduled},
		KindSuspension:       {Forbidden, Forbidden, StateInProgress, CategoryAccountLegal, false, false, StageStarted},
		KindSecurityIncident: {Forbidden, Forbidden, StateInProgress, CategorySecurity, false, true, StageOpened},
		KindTermsChange:      {Required, Forbidden, StateScheduled, CategoryAccountLegal, false, true, StageScheduled},
	}
	if len(Kinds()) != len(want) {
		t.Fatalf("видов в перечне %d, в таблице Р4 %d", len(Kinds()), len(want))
	}
	for _, k := range Kinds() {
		got, ok := RuleOf(k)
		if !ok || got != want[k] {
			t.Fatalf("строка вида %s = %+v (есть=%v), ожидалось %+v", k, got, ok, want[k])
		}
	}
	if _, ok := RuleOf("COLOUR"); ok {
		t.Fatal("вид вне перечня принят таблицей")
	}
}

// TestTransitions_LetterAndSupersededFollowP5 — письмо перехода и закрываемые
// этапы по таблицам Р5; у глагола без письма строки не закрываются, кроме
// напоминаний Start.
func TestTransitions_LetterAndSupersededFollowP5(t *testing.T) {
	t.Parallel()
	letters := map[Kind]Stage{KindMaintenance: StageCompleted, KindDecommission: StageCompleted,
		KindOutage: StageResolved, KindSuspension: StageLifted, KindSecurityIncident: StageClosed}
	for _, k := range Kinds() {
		got, ok := Letter(VerbComplete, k)
		want, wantOK := letters[k]
		if got != want || ok != wantOK {
			t.Fatalf("письмо Complete %s = %q/%v, ожидалось %q/%v", k, got, ok, want, wantOK)
		}
	}
	if s, ok := Letter(VerbStart, KindMaintenance); ok || s != "" {
		t.Fatalf("у Start письмо %q", s)
	}
	if got := Superseded(VerbComplete, KindTermsChange); len(got) != 0 {
		t.Fatalf("Complete без письма закрывает %v", got)
	}
	if got := Superseded(VerbStart, KindMaintenance); len(got) != 1 || got[0] != StageReminder {
		t.Fatalf("Start закрывает %v, ожидалось [reminder]", got)
	}
	if !TransitionOf(VerbComplete).Supports(KindOutage) || TransitionOf(VerbStart).Supports(KindOutage) {
		t.Fatal("доступность глаголов вида OUTAGE не по Р5")
	}
}

// TestTenantResourceTypes_ClosedListOfEighteen — перечень ровно из 18 типов;
// чужой тип не входит.
func TestTenantResourceTypes_ClosedListOfEighteen(t *testing.T) {
	t.Parallel()
	if n := len(TenantResourceTypes()); n != 18 {
		t.Fatalf("типов ресурса арендатора %d, ожидалось 18", n)
	}
	for _, typ := range []string{"account", "vpc_address_pool", ""} {
		if IsTenantResourceType(typ) {
			t.Fatalf("тип %q принят перечнем", typ)
		}
	}
	if !IsTenantResourceType("compute_instance") {
		t.Fatal("compute_instance не принят перечнем")
	}
}
