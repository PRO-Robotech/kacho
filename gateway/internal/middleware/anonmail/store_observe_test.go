// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// tickClock — монотонные часы пробы: каждое чтение — на step позже прежнего.
// Удержание строки ведра — разность двух чтений, поэтому оно ровно step на
// каждое решение, взявшее ведро, и 0 — на решении, которое ведра не брало.
type tickClock struct {
	mu   sync.Mutex
	t    time.Time
	step time.Duration
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(c.step)
	return c.t
}

// memoryRigTicking — звено над хранилищем memory, чьё удержание ведра судят
// часы tick.
func memoryRigTicking(t *testing.T, step time.Duration) (*rig, *MemoryStore) {
	t.Helper()
	l := testLimits()
	var mem *MemoryStore
	r := newRig(t, l, func(c *testClock) Store {
		mem = mustMemoryStore(t, l, c.Now)
		mem.memBackend.mono = (&tickClock{t: time.Unix(1_900_000_000, 0), step: step}).Now
		return mem
	}, 0)
	return r, mem
}

// TestMemoryStore_D66_BucketHoldIsCountedWhileTheBucketIsHeld — серия удержания
// строки ведра (Д66, CX2-92; ревью system-design CRIT-1): решение, взявшее
// ведро, прибавляет время от получения ведра до его отпускания; ожиданий ведра
// при свободном ведре — ноль. Решение, которому ведро не нужно (отказ на
// жёсткой ступени, пропуск по свежему доказательству — I-1), удержания не
// прибавляет: серия меряет занятость строки, а не число решений.
func TestMemoryStore_D66_BucketHoldIsCountedWhileTheBucketIsHeld(t *testing.T) {
	const step = 7 * time.Millisecond
	r, _ := memoryRigTicking(t, step)
	if rec := r.send(pathRecovery, "198.51.100.110", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("открытая ступень, ведро свободно: %d", rec.Code)
	}
	s := r.gate.Stats()
	t.Logf("после пропуска по жетону: %+v", s)
	if s.BucketHold != step {
		t.Fatalf("удержание ведра %s, ожидалось ровно %s (одно взятие)", s.BucketHold, step)
	}
	if s.BucketWaitTimeouts != 0 || s.StoreUnavailable != 0 {
		t.Fatalf("ведро свободно, а счётчики отказов растут: %+v", s)
	}

	// Свежее доказательство: ведро не берётся (I-1) — удержание не растёт.
	src := "198.51.100.111"
	for i := 0; i < r.limits.Source.Free; i++ {
		r.send(pathRecovery, src, "", "")
	}
	before := r.gate.Stats().BucketHold
	tok, bits := challengeOf(t, r.send(pathRecovery, src, "", ""))
	if rec := r.send(pathRecovery, src, tok+":"+solve(t, tok, bits), ""); rec.Code != http.StatusOK {
		t.Fatalf("свежее доказательство: %d", rec.Code)
	}
	if got := r.gate.Stats().BucketHold; got != before {
		t.Errorf("пропуск по доказательству прибавил удержание ведра: %s → %s", before, got)
	}
}

// holdBucket — ведро memory удержано соседом до конца пробы.
func holdBucket(t *testing.T, mem *MemoryStore) {
	t.Helper()
	mem.bucketLock <- struct{}{}
	t.Cleanup(func() { <-mem.bucketLock })
}

// sendWithin — ответ на запрос не позже limit; иначе провал пробы.
func sendWithin(t *testing.T, r *rig, limit time.Duration, ip, proof string) int {
	t.Helper()
	done := make(chan int, 1)
	go func() { done <- r.send(pathRecovery, ip, proof, "").Code }()
	select {
	case code := <-done:
		return code
	case <-time.After(limit):
		t.Fatalf("ответа нет за %s", limit)
	}
	return 0
}

// TestMemoryStore_I1_RejectAndProofPassDoNotWaitForTheBucket — строка ведра
// общего потока занята соседом (насыщение Д66): запрос, которому ведро не
// нужно, ответ получает по своим ключам — отказ на жёсткой ступени остаётся
// 429, пропуск по свежему доказательству остаётся 200, ожиданий ведра нет.
// Близнец меняет один факт — запросу ведро нужно (открытая ступень без
// доказательства): 503 и ожидание ведра +1. Без I-1 первые два получили бы 503:
// трафик, отвергнутый по своим ключам, занимал бы строку, общую для флота.
func TestMemoryStore_I1_RejectAndProofPassDoNotWaitForTheBucket(t *testing.T) {
	r, mem := memoryRigTicking(t, time.Millisecond)
	l := r.limits
	hard := "198.51.100.112"
	k, err := KeysFor(hard)
	if err != nil {
		t.Fatal(err)
	}
	// Источник над жёстким порогом: моменты записаны его же решениями.
	for i := 0; i < l.Source.Hard; i++ {
		if v := mem.Decide(context.Background(), Request{Keys: k, Now: r.clock.Now(),
			Proof: &Proof{ID: [challengeIDLen]byte{byte(i + 1)}, Bits: l.PoWBits.High, ExpiresAt: r.clock.Now().Add(ChallengeTTL)}}); v.Outcome != Pass {
			t.Fatalf("построение жёсткого порога, решение %d: %s", i, v.Outcome)
		}
	}
	proofSrc := "198.51.100.113"
	for i := 0; i < l.Source.Free; i++ {
		r.send(pathRecovery, proofSrc, "", "")
	}
	tok, bits := challengeOf(t, r.send(pathRecovery, proofSrc, "", ""))
	proof := tok + ":" + solve(t, tok, bits)

	holdBucket(t, mem)
	limit := anonMailStoreWait / 2
	if code := sendWithin(t, r, 10*anonMailStoreWait, hard, ""); code != http.StatusTooManyRequests {
		t.Errorf("жёсткая ступень при занятом ведре: %d, ожидался 429 по своим ключам", code)
	}
	start := time.Now()
	if code := sendWithin(t, r, 10*anonMailStoreWait, proofSrc, proof); code != http.StatusOK {
		t.Errorf("свежее доказательство при занятом ведре: %d, ожидался 200", code)
	}
	if took := time.Since(start); took > limit {
		t.Errorf("пропуск по доказательству ждал %s — ждал строку ведра", took)
	}
	if s := r.gate.Stats(); s.BucketWaitTimeouts != 0 || s.StoreUnavailable != 0 {
		t.Errorf("запросы без ведра растят счётчики отказов: %+v", s)
	}
	// Близнец: ведро нужно — 503 и ожидание ведра +1.
	if code := sendWithin(t, r, 10*anonMailStoreWait, "198.51.100.114", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("близнец «ведро нужно»: %d, ожидался 503 — проба не создала занятость ведра", code)
	}
	if s := r.gate.Stats(); s.BucketWaitTimeouts != 1 || s.StoreUnavailable != 1 {
		t.Errorf("близнец: счётчики %+v, ожидалось 1 и 1", s)
	}
}

// TestMemoryStore_I2_NoBaseClockNoOffset — у memory часов базы нет: смещение не
// измеряется и серии смещения нет (ноль значил бы «часы сверены»).
func TestMemoryStore_I2_NoBaseClockNoOffset(t *testing.T) {
	r, _ := memoryRigTicking(t, time.Millisecond)
	if rec := r.send(pathRecovery, "198.51.100.115", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("пропуск: %d", rec.Code)
	}
	if s := r.gate.Stats(); s.ClockOffsetMeasured {
		t.Errorf("memory объявил измеренное смещение часов: %+v", s)
	}
}
