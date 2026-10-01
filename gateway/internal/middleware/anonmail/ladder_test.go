// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// testLimits — пределы в форме ориентиров Р8: F < P < H у источника, Ps < Hs у
// подсетей, BASE < HIGH.
func testLimits() config.AnonMailLimits {
	return config.AnonMailLimits{
		Source: config.AnonMailSourceLimits{
			Free: 3, PoW: 5, Hard: 8,
			FreeWindow: 15 * time.Minute, PoWWindow: time.Hour, HardWindow: 2 * time.Hour,
		},
		PoWBits:          config.AnonMailPoWBits{Base: 10, High: 14},
		SubnetV4Len24:    config.AnonMailSubnetLimits{PoW: 20, Hard: 40},
		SubnetV6Len56:    config.AnonMailSubnetLimits{PoW: 30, Hard: 60},
		SubnetV6Len48:    config.AnonMailSubnetLimits{PoW: 50, Hard: 100},
		SubnetPoWWindow:  time.Hour,
		SubnetHardWindow: 2 * time.Hour,
		Global:           config.AnonMailGlobalFlow{RatePerSecond: 5, Burst: 10},
	}
}

// TestLadder_NTF2_60_ThreeRungsTopDown — лестница источника (Р5): ступени
// проверяются сверху вниз; каждую задаёт своя ручка.
func TestLadder_NTF2_60_ThreeRungsTopDown(t *testing.T) {
	l := testLimits()
	cases := []struct {
		name string
		c    SourceCounts
		want Rung
	}{
		{"ниже FREE", SourceCounts{Free: 2, PoW: 2, Hard: 2}, RungOpen},
		{"на FREE", SourceCounts{Free: 3, PoW: 3, Hard: 3}, RungBase},
		{"на POW", SourceCounts{Free: 5, PoW: 5, Hard: 5}, RungHigh},
		{"на HARD", SourceCounts{Free: 8, PoW: 8, Hard: 8}, RungHard},
		// Окна свои: счёт окна FREE вышел из окна, а POW ещё нет — повышенная
		// ступень держится счётом своего окна.
		{"окно FREE опустело, POW нет", SourceCounts{Free: 0, PoW: 5, Hard: 5}, RungHigh},
		{"окна FREE и POW опустели, HARD нет", SourceCounts{Free: 0, PoW: 0, Hard: 8}, RungHard},
	}
	for _, c := range cases {
		if got := Ladder(l, Counts{Source: c.c}); got != c.want {
			t.Errorf("%s: ступень %s, ожидалась %s", c.name, got, c.want)
		}
	}
}

// TestLadder_NTF2_60b_PoWKnobAloneSetsTheHighRung — близнец (б): при POW = HARD
// повышенной ступени нет — её задаёт ручка POW и только она.
func TestLadder_NTF2_60b_PoWKnobAloneSetsTheHighRung(t *testing.T) {
	l := testLimits()
	l.Source.PoW = l.Source.Hard
	for n := l.Source.Free; n < l.Source.Hard; n++ {
		if got := Ladder(l, Counts{Source: SourceCounts{Free: n, PoW: n, Hard: n}}); got != RungBase {
			t.Errorf("счёт %d при POW = HARD: ступень %s, ожидалась base", n, got)
		}
	}
	if got := Ladder(l, Counts{Source: SourceCounts{Free: 8, PoW: 8, Hard: 8}}); got != RungHard {
		t.Errorf("сверх HARD: %s", got)
	}
}

// TestLadder_NTF2_61_SubnetAxisChallengesWithBaseAndRejectsAtHard — ось
// подсети: сверх Ps — вызов БАЗОВОЙ сложности (счёт источника ниже POW), сверх
// Hs — жёсткий отказ; пределы — своей длины префикса.
func TestLadder_NTF2_61_SubnetAxisChallengesWithBaseAndRejectsAtHard(t *testing.T) {
	l := testLimits()
	for _, sn := range []struct {
		len      int
		pow, hrd int
	}{{24, 20, 40}, {56, 30, 60}, {48, 50, 100}} {
		below := Counts{Subnets: []SubnetCounts{{Len: sn.len, PoW: sn.pow - 1, Hard: sn.pow - 1}}}
		atPoW := Counts{Subnets: []SubnetCounts{{Len: sn.len, PoW: sn.pow, Hard: sn.pow}}}
		atHard := Counts{Subnets: []SubnetCounts{{Len: sn.len, PoW: sn.hrd, Hard: sn.hrd}}}
		if got := Ladder(l, below); got != RungOpen {
			t.Errorf("/%d ниже Ps: %s", sn.len, got)
		}
		if got := Ladder(l, atPoW); got != RungBase {
			t.Errorf("/%d на Ps: %s, ожидалась base", sn.len, got)
		}
		if got := Ladder(l, atHard); got != RungHard {
			t.Errorf("/%d на Hs: %s, ожидалась hard", sn.len, got)
		}
	}
	// Источник на POW и подсеть на Ps — повышенная сложность источника
	// перебивает базовую подсети.
	c := Counts{Source: SourceCounts{Free: 5, PoW: 5, Hard: 5}, Subnets: []SubnetCounts{{Len: 24, PoW: 20, Hard: 20}}}
	if got := Ladder(l, c); got != RungHigh {
		t.Errorf("источник на POW и подсеть на Ps: %s, ожидалась high", got)
	}
}

// TestDecide_ClosedOutcomesByRungProofAndBucket — исход решения (Р5, З8): HARD
// — отказ и доказательство не принимается; свежее доказательство нужной
// сложности — пропуск без жетона ведра; без доказательства — вызов по ступени,
// а на открытой ступени — жетон ведра, без жетона — вызов базовой сложности
// (NTF2-74); доказательство, не прошедшее проверку, — новый вызов, а не пропуск
// (NTF2-62).
func TestDecide_ClosedOutcomesByRungProofAndBucket(t *testing.T) {
	l := testLimits()
	base, high := l.PoWBits.Base, l.PoWBits.High
	cases := []struct {
		name      string
		rung      Rung
		proof     proofState
		proofBits int
		tokens    float64
		want      Outcome
		bits      int
		take      bool
	}{
		{"открыто, жетон есть", RungOpen, proofAbsent, 0, 1, Pass, 0, true},
		{"открыто, жетона нет — общий поток", RungOpen, proofAbsent, 0, 0.99, Challenge, base, false},
		{"base без доказательства", RungBase, proofAbsent, 0, 10, Challenge, base, false},
		{"high без доказательства", RungHigh, proofAbsent, 0, 10, Challenge, high, false},
		{"hard и свежее доказательство — отказ", RungHard, proofFresh, high, 10, Reject, 0, false},
		{"base и свежее base — пропуск без жетона", RungBase, proofFresh, base, 0, Pass, 0, false},
		{"high и свежее base — мало, вызов high", RungHigh, proofFresh, base, 10, Challenge, high, false},
		{"high и свежее high — пропуск", RungHigh, proofFresh, high, 0, Pass, 0, false},
		{"открыто, ведро пусто, свежее base — пропуск", RungOpen, proofFresh, base, 0, Pass, 0, false},
		{"открыто и отвергнутое доказательство — вызов, не пропуск", RungOpen, proofRejected, 0, 10, Challenge, base, false},
		{"high и отвергнутое — вызов high", RungHigh, proofRejected, 0, 10, Challenge, high, false},
	}
	for _, c := range cases {
		got := decide(l, c.rung, c.proof, c.proofBits, c.tokens)
		if got.outcome != c.want || got.bits != c.bits || got.takeToken != c.take {
			t.Errorf("%s: %+v, ожидалось исход %s бит %d жетон %v", c.name, got, c.want, c.bits, c.take)
		}
	}
}

// TestOutcome_SetIsClosed — тип исходов закрыт: Pass · Challenge · Reject ·
// StoreUnavailable; ветки «не смог спросить → пропустить» в нём нет.
func TestOutcome_SetIsClosed(t *testing.T) {
	want := []string{"pass", "challenge", "reject", "store_unavailable"}
	got := Outcomes()
	if len(got) != len(want) {
		t.Fatalf("исходов %d, ожидалось %d", len(got), len(want))
	}
	for i, o := range got {
		if o.String() != want[i] {
			t.Errorf("исход %d — %q, ожидался %q", i, o, want[i])
		}
	}
	if Outcome(0).Valid() {
		t.Error("нулевое значение исхода — допустимый исход: незаполненное решение прошло бы за одно из закрытых")
	}
}

// TestRefill_TokenBucket — ведро общего потока: темп RATE в секунду до BURST;
// часы, ушедшие назад, не отнимают жетонов и не сдвигают момент назад.
func TestRefill_TokenBucket(t *testing.T) {
	g := config.AnonMailGlobalFlow{RatePerSecond: 5, Burst: 10}
	t0 := time.Unix(1_700_000_000, 0)
	if tok, at := refill(0, t0, t0.Add(time.Second), g); tok != 5 || !at.Equal(t0.Add(time.Second)) {
		t.Errorf("секунда при 5/с: %v жетонов, момент %v", tok, at)
	}
	if tok, _ := refill(0, t0, t0.Add(time.Hour), g); tok != 10 {
		t.Errorf("час: %v жетонов, ожидался потолок 10", tok)
	}
	if tok, at := refill(3, t0, t0.Add(-time.Second), g); tok != 3 || !at.Equal(t0) {
		t.Errorf("часы назад: %v жетонов, момент %v", tok, at)
	}
}

// TestRetryAfter_SecondsUntilTheCountedMomentLeavesTheWindow — Retry-After —
// целые секунды до момента, когда засчитанный момент выйдет из окна HARD;
// не меньше одной.
func TestRetryAfter_SecondsUntilTheCountedMomentLeavesTheWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	w := 2 * time.Hour
	if got := retryAfterFor(now, now.Add(-w+90*time.Second+time.Millisecond), w); got != 91*time.Second {
		t.Errorf("выход через 90,001 с: %v, ожидалось 91 с", got)
	}
	if got := retryAfterFor(now, now.Add(-w), w); got != time.Second {
		t.Errorf("выход сейчас: %v, ожидалась 1 с", got)
	}
}
