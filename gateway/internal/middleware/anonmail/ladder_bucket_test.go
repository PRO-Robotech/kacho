// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import "testing"

// TestDecide_I1_OnlyTheTokenPathReadsTheBucket — строка ведра берётся только
// тогда, когда исход от неё зависит (ревью system-design I-1, решение Д71).
// Утверждение двустороннее и перебирает весь вход чистой части решения:
//
//   - там, где чистая часть говорит «ведро не нужно», исход одинаков при пустом
//     и при полном ведре — решение, принятое без строки ведра, то же, что
//     принятое с ней (неделимость «решение и счёт» не меняется);
//   - там, где говорит «нужно», исход при пустом и полном ведре РАЗНЫЙ — иначе
//     строка бралась бы зря, а проба не отличала бы нужное от лишнего.
//
// Перепись печатается; ни одного «нужно» либо ни одного «не нужно» на полном
// переборе — беспредметный вход.
func TestDecide_I1_OnlyTheTokenPathReadsTheBucket(t *testing.T) {
	l := testLimits()
	rungs := []Rung{RungOpen, RungBase, RungHigh, RungHard}
	states := []proofState{proofAbsent, proofRejected, proofFresh}
	bitsSet := []int{0, l.PoWBits.Base, l.PoWBits.High}
	need, noNeed := 0, 0
	for _, r := range rungs {
		for _, p := range states {
			for _, b := range bitsSet {
				pol, needs := decideWithoutBucket(l, r, p, b)
				empty, full := decide(l, r, p, b, 0), decide(l, r, p, b, float64(l.Global.Burst))
				if needs {
					need++
					if empty == full {
						t.Errorf("ступень %s, доказательство %d, бит %d: «ведро нужно», а исход от ведра не зависит (%+v)", r, p, b, empty)
					}
					continue
				}
				noNeed++
				if empty != pol || full != pol {
					t.Errorf("ступень %s, доказательство %d, бит %d: «ведро не нужно», а исход зависит от ведра: без ведра %+v, пустое %+v, полное %+v",
						r, p, b, pol, empty, full)
				}
				if pol.takeToken {
					t.Errorf("ступень %s, доказательство %d, бит %d: жетон взят без строки ведра", r, p, b)
				}
			}
		}
	}
	t.Logf("ПЕРЕПИСЬ: входов %d · строка ведра нужна %d · не нужна %d", need+noNeed, need, noNeed)
	if need == 0 || noNeed == 0 {
		t.Fatalf("перебор беспредметен: нужна %d, не нужна %d", need, noNeed)
	}
	if need != len(bitsSet) {
		t.Errorf("строка ведра нужна на %d входах, ожидалось %d — только открытая ступень без заголовка доказательства", need, len(bitsSet))
	}
}
