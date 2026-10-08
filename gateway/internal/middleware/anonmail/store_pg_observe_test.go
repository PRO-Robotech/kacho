// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// seedHard — источник src за жёстким порогом: Hard моментов пропуска в окне,
// записанных прямо в таблицу (момент — at).
func seedHard(t *testing.T, dsn, src string, at time.Time, n int) {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	key := mustKeys(t, src).Source
	for i := 0; i < n; i++ {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Exec(context.Background(),
			`INSERT INTO kacho_gateway.anon_mail_passes (key, at, decision_id) VALUES ($1, $2, $3)`,
			key, at.Add(-time.Duration(i+1)*time.Second), id[:]); err != nil {
			t.Fatal(err)
		}
	}
}

// holdBucketRow — строка ведра удержана транзакцией пробы до конца пробы.
func holdBucketRow(t *testing.T, dsn string) {
	t.Helper()
	holder, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := holder.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
		_ = holder.Close(context.Background())
	})
	if _, err := tx.Exec(context.Background(), `SELECT 1 FROM kacho_gateway.anon_mail_bucket WHERE id = 1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
}

// TestPg_I1_RejectAndProofPassDoNotWaitForTheBucketRow — строка ведра общего
// потока удержана (насыщение Д66): решение, которому ведро не нужно, его не
// ждёт — отказ на жёсткой ступени 429, пропуск по свежему доказательству 200,
// ожиданий ведра нет (ревью system-design I-1, решение Д71). Близнец меняет один
// факт — ведро нужно (открытая ступень без доказательства): 503, оба счётчика
// +1.
func TestPg_I1_RejectAndProofPassDoNotWaitForTheBucketRow(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	r, _, _ := pgRig(t, dsn, l)
	hard := "198.51.100.120"
	seedHard(t, dsn, hard, r.clock.Now(), l.Source.Hard)
	proofSrc := "198.51.100.121"
	for i := 0; i < l.Source.Free; i++ {
		r.send(pathRecovery, proofSrc, "", "")
	}
	tok, bits := challengeOf(t, r.send(pathRecovery, proofSrc, "", ""))
	proof := tok + ":" + solve(t, tok, bits)

	holdBucketRow(t, dsn)
	before := r.gate.Stats()
	start := time.Now()
	if rec := r.send(pathRecovery, hard, "", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("жёсткая ступень при удержанной строке ведра: %d, ожидался 429", rec.Code)
	}
	if rec := r.send(pathRecovery, proofSrc, proof, ""); rec.Code != http.StatusOK {
		t.Errorf("свежее доказательство при удержанной строке ведра: %d, ожидался 200", rec.Code)
	}
	took := time.Since(start)
	after := r.gate.Stats()
	t.Logf("два решения без ведра за %s · счётчики %+v", took, after)
	if took > anonMailStoreWait {
		t.Errorf("решения без ведра ждали %s — дольше одного ожидания хранилища", took)
	}
	if after.StoreUnavailable != before.StoreUnavailable || after.BucketWaitTimeouts != before.BucketWaitTimeouts {
		t.Errorf("решения без ведра растят счётчики отказов: %+v → %+v", before, after)
	}
	if rec := r.send(pathRecovery, "198.51.100.122", "", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("близнец «ведро нужно»: %d, ожидался 503 — проба не создала удержания строки", rec.Code)
	}
	if s := r.gate.Stats(); s.BucketWaitTimeouts != before.BucketWaitTimeouts+1 || s.StoreUnavailable != before.StoreUnavailable+1 {
		t.Errorf("близнец: счётчики %+v, ожидалось +1 и +1 к %+v", s, before)
	}
}

// TestPg_D66_BucketHoldSecondsGrowOnlyWhileTheRowIsHeld — серия удержания
// строки ведра на postgres (Д66; ревью system-design CRIT-1): пропуск по жетону
// прибавляет удержание > 0, ожиданий ведра при свободной строке — 0; отказ на
// жёсткой ступени строки не берёт и удержания не прибавляет (I-1).
func TestPg_D66_BucketHoldSecondsGrowOnlyWhileTheRowIsHeld(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	r, _, _ := pgRig(t, dsn, l)
	if rec := r.send(pathRecovery, "198.51.100.123", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("открытая ступень: %d", rec.Code)
	}
	s := r.gate.Stats()
	t.Logf("после пропуска по жетону: %+v", s)
	if s.BucketHold <= 0 || s.BucketHold > anonMailDecisionBudget+anonMailCommitWait {
		t.Fatalf("удержание строки ведра %s, ожидалось в (0, %s]", s.BucketHold, anonMailDecisionBudget+anonMailCommitWait)
	}
	if s.BucketWaitTimeouts != 0 || s.StoreUnavailable != 0 {
		t.Fatalf("строка свободна, а счётчики отказов растут: %+v", s)
	}
	hard := "198.51.100.124"
	seedHard(t, dsn, hard, r.clock.Now(), l.Source.Hard)
	if rec := r.send(pathRecovery, hard, "", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("жёсткая ступень: %d", rec.Code)
	}
	if got := r.gate.Stats().BucketHold; got != s.BucketHold {
		t.Errorf("отказ на жёсткой ступени прибавил удержание строки ведра: %s → %s", s.BucketHold, got)
	}
}

// TestPg_I2_ClockOffsetIsMeasuredAgainstTheBaseClock — смещение часов реплики
// от часов базы (ревью system-design I-2, решение Д71) меряется в каждом
// решении оператором, который решение и так исполняет: момент решения минус
// clock_timestamp() базы. Реплика, ушедшая вперёд на 20 с, видна смещением
// около +20 с; близнец — часы вровень с базой (база и проба на одной машине) —
// смещение не больше срока решения по модулю.
func TestPg_I2_ClockOffsetIsMeasuredAgainstTheBaseClock(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	run := func(t *testing.T, ahead time.Duration) time.Duration {
		t.Helper()
		r, _, _ := pgRig(t, dsn, l)
		if s := r.gate.Stats(); s.ClockOffsetMeasured {
			t.Fatalf("смещение объявлено измеренным до первого решения: %+v", s)
		}
		r.clock.Set(time.Now().Add(ahead))
		if rec := r.send(pathRecovery, "198.51.100.125", "", ""); rec.Code == http.StatusServiceUnavailable {
			t.Fatalf("решение не состоялось: %d", rec.Code)
		}
		s := r.gate.Stats()
		if !s.ClockOffsetMeasured {
			t.Fatalf("решение исполнено, а смещение не измерено: %+v", s)
		}
		return s.ClockOffset
	}
	const ahead = 20 * time.Second
	got := run(t, ahead)
	twin := run(t, 0)
	t.Logf("реплика впереди на %s: смещение %s · близнец «вровень»: %s", ahead, got, twin)
	if got < ahead-anonMailDecisionBudget || got > ahead+anonMailStoreWait {
		t.Errorf("смещение %s, ожидалось в [%s, %s]", got, ahead-anonMailDecisionBudget, ahead+anonMailStoreWait)
	}
	if twin < -anonMailDecisionBudget || twin > anonMailStoreWait {
		t.Errorf("близнец: смещение %s при часах вровень — измерение не отличает сдвиг от его отсутствия", twin)
	}
}

// TestPg_I1_MixedRaceKeepsTheBucketExact — неделимость «решение и счёт» при
// строке ведра, взятой только на пути жетона (I-1): две реплики, одновременно
// n открытых решений с разных подсетей при BURST = k и m решений источника за
// жёстким порогом. Пропущено ровно k, отказов ровно m, вызовов n − k, жетонов
// после — 0: решения без ведра ни жетона не взяли, ни счёта не сбили.
func TestPg_I1_MixedRaceKeepsTheBucketExact(t *testing.T) {
	const n, k, m = 12, 5, 6
	dsn := edgeDB(t)
	l := orderLimits()
	l.Global.Burst = k
	stores := twoReplicas(t, dsn, l)
	now := dbMoment()
	hard := v4(5, 0, 1)
	seedHard(t, dsn, hard, now, l.Source.Hard)
	reqs := make([]Request, 0, n+m)
	for i := 0; i < n; i++ {
		reqs = append(reqs, Request{Keys: mustKeys(t, v4(4, i, 1)), Now: now})
		if i < m {
			reqs = append(reqs, Request{Keys: mustKeys(t, hard), Now: now})
		}
	}
	tally := raceDecisions(stores, reqs)
	tokens := scalar[float64](t, dsn, `SELECT tokens FROM kacho_gateway.anon_mail_bucket WHERE id = 1`)
	t.Logf("решений %d (открытых %d, за порогом %d) при BURST=%d · %s · жетонов после %v", n+m, n, m, k, tally, tokens)
	if tally.pass.Load() != k || tally.reject.Load() != m || tally.challenge.Load() != n-k ||
		tally.unavailable.Load() != 0 || tally.other.Load() != 0 {
		t.Fatalf("исходы %s, ожидалось pass %d · reject %d · challenge %d", tally, k, m, n-k)
	}
	if tokens != 0 {
		t.Fatalf("жетонов после гонки %v, ожидалось 0", tokens)
	}
}
