// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
)

// TestPg_I1_ProofExpiredByTheBaseClockIsNotPassedByALaggingReplica — срок
// доказательства и его уборка судятся ОДНИМИ часами (ревью db, I1). Пометку
// вызова уборщик снимает по часам базы; реплика, чьи часы отстают на lag,
// считает тот же вызов живым. Если срок судят только часы реплики, после
// уборки пометки повтор того же доказательства проходит как свежий —
// одноразовость пробита.
//
// Пробу исполняет одна функция; близнец меняет ровно один факт — часы реплики
// идут вровень с часами базы: первое предъявление пропущено, пометка живёт,
// уборщик её не трогает, повтор получает новый вызов.
func TestPg_I1_ProofExpiredByTheBaseClockIsNotPassedByALaggingReplica(t *testing.T) {
	type result struct{ first, replay int }
	run := func(t *testing.T, lag time.Duration) result {
		t.Helper()
		dsn := edgeDB(t)
		l := testLimits()
		r, _, _ := pgRig(t, dsn, l)
		// Часы реплики: часы машины пробы (той же, что у базы) минус lag.
		r.clock.Set(time.Now().Add(-lag))
		src := "198.51.100.90"
		for i := 0; i < l.Source.Free; i++ {
			if rec := r.send(pathRecovery, src, "", ""); rec.Code != http.StatusOK {
				t.Fatalf("построение порога: %d", rec.Code)
			}
		}
		tok, bits := challengeOf(t, r.send(pathRecovery, src, "", ""))
		proof := tok + ":" + solve(t, tok, bits)
		p, err := r.pow.Verify(proof)
		if err != nil {
			t.Fatalf("по часам реплики доказательство не живо — условие не создано: %v", err)
		}
		first := r.send(pathRecovery, src, proof, "")
		// Уборка — после срока вызова по часам базы (часы машины пробы), если
		// срок близок; у близнеца вызов живёт минуты, и уборщику нечего снять.
		if wait := time.Until(p.ExpiresAt) + 100*time.Millisecond; wait > 0 && wait < 30*time.Second {
			time.Sleep(wait)
		}
		idem, err := idempotencypg.New(context.Background(), idempotencypg.Config{DSN: dsn})
		if err != nil {
			t.Fatal(err)
		}
		sw, err := idem.PurgeAnonMail(context.Background())
		_ = idem.Close()
		if err != nil {
			t.Fatalf("уборка: %v", err)
		}
		replay := r.send(pathRecovery, src, proof, "")
		t.Logf("отставание часов реплики %s: первое %d · уборка унесла %d · повтор %d", lag, first.Code, sw.Removed, replay.Code)
		return result{first: first.Code, replay: replay.Code}
	}
	// (а) Часы реплики отстают на срок вызова без трёх секунд: первое
	// предъявление застаёт вызов живым и по часам базы — пропуск и пометка;
	// затем срок по часам базы истекает, уборщик снимает пометку, а реплика
	// (её часы стоят) всё ещё считает вызов живым и предъявляет его снова.
	spent := run(t, ChallengeTTL-3*time.Second)
	if spent.first != http.StatusOK {
		t.Fatalf("(а) первое предъявление живого вызова: %d, ожидался пропуск — условие не создано", spent.first)
	}
	if spent.replay == http.StatusOK {
		t.Errorf("(а) повтор после уборки пометки по часам базы пропущен — одноразовость держат не одни часы")
	}
	// (б) Часы реплики отстают больше срока вызова: по часам базы вызов истёк
	// в момент выпуска — ни первое предъявление, ни повтор не пропущены.
	lagging := run(t, ChallengeTTL+5*time.Minute)
	if lagging.first == http.StatusOK || lagging.replay == http.StatusOK {
		t.Errorf("(б) доказательство, истёкшее по часам базы, пропущено отстающей репликой: %d → %d",
			lagging.first, lagging.replay)
	}
	twin := run(t, 0)
	if twin.first != http.StatusOK || twin.replay != http.StatusTooManyRequests {
		t.Fatalf("близнец «часы вровень»: %d → %d, ожидались 200 → 429 (иначе проба не отличила бы срок от поломки)",
			twin.first, twin.replay)
	}
}
