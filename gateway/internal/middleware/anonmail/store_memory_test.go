// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMemoryStore_CX2_43_MarkWrittenBucketNotObtainedIs503AndRepeatIsFresh —
// одноразовость, решение и момент — одна неделимая операция (CX2-43; Р5 «503
// не истрачивает вызов»): верное свежее доказательство, пометка записана, затем
// ведро не получено в срок (удержано соседом дольше anonMailStoreWait) → 503;
// повтор того же решения после освобождения ведра → Fresh и пропуск.
//
// Близнец-инъекция меняет один факт — пометка вынесена в отдельную
// зафиксированную запись до решения: повтор → Replayed и новый вызов.
func TestMemoryStore_CX2_43_MarkWrittenBucketNotObtainedIs503AndRepeatIsFresh(t *testing.T) {
	// Пробу исполняет одна функция; инъекция меняет ровно один факт, и её
	// утверждение обязано разойтись с утверждением продукта — так проба
	// доказывает, что способна упасть.
	run := func(t *testing.T, markApart bool) (held, again int) {
		l := testLimits()
		var mem *MemoryStore
		r := newRig(t, l, func(c *testClock) Store { mem = NewMemoryStore(l, c.Now); return mem }, 0)
		src := "198.51.100.40"
		for i := 0; i < l.Source.Free; i++ {
			r.send(pathRecovery, src, "", "")
		}
		tok, bits := challengeOf(t, r.send(pathRecovery, src, "", ""))
		proof := tok + ":" + solve(t, tok, bits)
		if markApart {
			p, err := r.pow.Verify(proof)
			if err != nil {
				t.Fatal(err)
			}
			mem.markCommitted(p.ID, p.ExpiresAt)
		}
		// Ведро удержано соседом дольше предела ожидания.
		mem.bucketLock <- struct{}{}
		h := r.send(pathRecovery, src, proof, "")
		<-mem.bucketLock
		a := r.send(pathRecovery, src, proof, "")
		return h.Code, a.Code
	}
	held, again := run(t, false)
	if held != http.StatusServiceUnavailable {
		t.Fatalf("ведро удержано: %d, ожидался 503", held)
	}
	if again != http.StatusOK {
		t.Fatalf("повтор того же решения: %d — 503 истратил вызов", again)
	}
	// Близнец-инъекция: пометка вынесена отдельной записью — повтор получает
	// новый вызов (Replayed), утверждение продукта на нём красное.
	tHeld, tAgain := run(t, true)
	if tHeld != http.StatusServiceUnavailable || tAgain != http.StatusTooManyRequests {
		t.Fatalf("близнец «пометка отдельно»: %d, затем %d — ожидались 503 и вызов 429 "+
			"(иначе проба не отличила бы неделимость от порядка вызовов)", tHeld, tAgain)
	}
	t.Logf("одна операция: %d → %d · близнец «пометка отдельно»: %d → %d", held, again, tHeld, tAgain)
}

// TestMemoryStore_CX2_11_RaceOnANewKeyPassesExactlyOne — N одновременных
// запросов с нового ключа при FREE = 1 → пропущен ровно один.
func TestMemoryStore_CX2_11_RaceOnANewKeyPassesExactlyOne(t *testing.T) {
	l := testLimits()
	l.Source.Free = 1
	r := memoryRig(t, l)
	const n = 32
	var ok atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if rec := r.send(pathRecovery, "198.51.100.41", "", ""); rec.Code == http.StatusOK {
				ok.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("пропущено %d из %d, ожидался ровно один", ok.Load(), n)
	}
}

// blockingBackend — заглушка хранилища, у которой первый же оператор держится
// дольше срока решения (до отмены контекста).
type blockingBackend struct{ held atomic.Int64 }

func (b *blockingBackend) begin(ctx context.Context) (decisionTx, error) {
	return &blockingTx{b: b}, nil
}
func (b *blockingBackend) close() error { return nil }

type blockingTx struct{ b *blockingBackend }

func (x *blockingTx) lock(ctx context.Context, _ []lockPair) error {
	x.b.held.Add(1)
	<-ctx.Done()
	return ctx.Err()
}
func (x *blockingTx) markSpent(context.Context, Proof) (bool, error) { return false, errNotReached }
func (x *blockingTx) bucket(context.Context) (float64, time.Time, error) {
	return 0, time.Time{}, errNotReached
}
func (x *blockingTx) counts(context.Context, string, time.Time, []time.Duration) ([]int, error) {
	return nil, errNotReached
}
func (x *blockingTx) nthMoment(context.Context, string, time.Time, time.Duration, int) (time.Time, error) {
	return time.Time{}, errNotReached
}
func (x *blockingTx) recordPass(context.Context, Keys, time.Time, *Proof, *bucketWrite) error {
	return errNotReached
}
func (x *blockingTx) commit(context.Context) Outcome { return StoreUnavailable }
func (x *blockingTx) rollback()                       {}

// TestStore_DecisionBudgetEndsInStoreUnavailable — модульная проба срока
// решения (К1 ревью замысла; УК54): заглушка хранилища держит оператор дольше
// anonMailDecisionBudget → StoreUnavailable в пределах срока плюс допуск
// anonMailCancelGrace; счётчик звена +1. Сон драйвера (100 мс) в допуск не
// входит: в заглушке драйвера нет.
func TestStore_DecisionBudgetEndsInStoreUnavailable(t *testing.T) {
	l := testLimits()
	bb := &blockingBackend{}
	r := newRig(t, l, func(c *testClock) Store { return newStore(bb, l, discardLogger()) }, 0)
	start := time.Now()
	rec := r.send(pathRecovery, "198.51.100.42", "", "")
	took := time.Since(start)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("оператор дольше срока: %d, ожидался 503", rec.Code)
	}
	if took < anonMailDecisionBudget || took > anonMailDecisionBudget+anonMailCancelGrace {
		t.Errorf("ответ через %s, ожидался в [%s, %s]", took, anonMailDecisionBudget, anonMailDecisionBudget+anonMailCancelGrace)
	}
	if r.gate.Stats().StoreUnavailable != 1 || bb.held.Load() != 1 {
		t.Errorf("счётчик %d, операторов %d", r.gate.Stats().StoreUnavailable, bb.held.Load())
	}
	t.Logf("срок решения %s · ответ через %s · допуск %s", anonMailDecisionBudget, took, anonMailCancelGrace)
}

// TestMemoryStore_SweepRemovesExpiredSubjects — память ограничена: моменты
// старше срока хранения (З26) и пометки после срока вызова уходят уборкой.
func TestMemoryStore_SweepRemovesExpiredSubjects(t *testing.T) {
	l := testLimits()
	clock := newTestClock()
	m := NewMemoryStore(l, clock.Now)
	defer func() { _ = m.Close() }()
	k, _ := KeysFor("198.51.100.43")
	var id [16]byte
	id[0] = 1
	if v := m.Decide(context.Background(), Request{Keys: k, Now: clock.Now(),
		Proof: &Proof{ID: id, Bits: l.PoWBits.High, ExpiresAt: clock.Now().Add(ChallengeTTL)}}); v.Outcome != Pass {
		t.Fatalf("исход %s", v.Outcome)
	}
	if m.size() == (memSize{}) {
		t.Fatal("после пропуска хранилище пусто")
	}
	clock.Add(PassRetention())
	m.sweep(clock.Now())
	if got := m.size(); got.moments != 0 || got.spent != 0 {
		t.Errorf("после срока хранения осталось %+v", got)
	}
}
