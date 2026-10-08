// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// commitRig — звено над postgres за посредником; доказательство построено до
// вмешательства посредника: источник прошёл FREE и получил вызов.
type commitRig struct {
	*rig
	ps    *PostgresStore
	logs  *syncBuffer
	proxy *pgProxy
	dsn   string
	src   string
	proof string
}

func newCommitRig(t *testing.T) *commitRig {
	t.Helper()
	dsn := edgeDB(t)
	proxy, via := newPGProxy(t, dsn)
	l := testLimits()
	r, ps, logs := pgRig(t, via, l)
	// Соединения посредника рвутся ДО закрытия пула: иначе закрытие ждало бы
	// заглушённые соединения до предела их деструктора.
	t.Cleanup(proxy.dropAll)
	src := "198.51.100.70"
	for i := 0; i < l.Source.Free; i++ {
		if rec := r.send(pathRecovery, src, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("построение: %d", rec.Code)
		}
	}
	tok, bits := challengeOf(t, r.send(pathRecovery, src, "", ""))
	return &commitRig{rig: r, ps: ps, logs: logs, proxy: proxy, dsn: dsn, src: src, proof: tok + ":" + solve(t, tok, bits)}
}

func (c *commitRig) sourceKey() string {
	k, _ := KeysFor(c.src)
	return k.Source
}

// timed — запрос с доказательством; время ответа и момент, когда посредник
// увидел COMMIT (для отсчёта срока разрешения от отправки фиксации).
func (c *commitRig) timed() (code int, took time.Duration) {
	start := time.Now()
	rec := c.send(pathRecovery, c.src, c.proof, "")
	return rec.Code, time.Since(start)
}

// TestPg_CX2_46a_DelayedAndDeliveredCommitIsAPass — (а) посредник задерживает
// COMMIT на anonMailCommitWait + anonMailCancelGrace плюс запас и доставляет
// его, хотя клиент уже закрыл соединение: пропуск, 503 нет, полоса вызвана;
// повтор того же доказательства → Replayed и новый вызов. Близнецы-инъекции:
// ошибка COMMIT сведена к StoreUnavailable; чтение строки без ожидания конца
// исходной транзакции — оба дают 503 и повтор Replayed — красные.
func TestPg_CX2_46a_DelayedAndDeliveredCommitIsAPass(t *testing.T) {
	delay := anonMailCommitWait + anonMailCancelGrace + 300*time.Millisecond
	run := func(t *testing.T, inject func(*PostgresStore)) (code, again int, lane int64) {
		c := newCommitRig(t)
		if inject != nil {
			inject(c.ps)
		}
		c.proxy.set(func(p *pgProxy) { p.commitAction, p.commitDelay = "delay", delay })
		laneBefore := c.lane.calls.Load()
		code, took := c.timed()
		<-c.proxy.delivered
		c.proxy.set(func(p *pgProxy) { p.commitAction = "deliver" })
		lane = c.lane.calls.Load() - laneBefore
		again = c.send(pathRecovery, c.src, c.proof, "").Code
		t.Logf("COMMIT задержан на %s: ответ %d за %s · полоса %d · повтор %d · отмен отброшено %d",
			delay, code, took, lane, again, c.proxy.cancelsDropped.Load())
		return code, again, lane
	}
	code, again, lane := run(t, nil)
	if code != http.StatusOK || lane != 1 {
		t.Fatalf("(а): ответ %d, полоса %d — ожидались пропуск и вызов полосы", code, lane)
	}
	if again != http.StatusTooManyRequests {
		t.Fatalf("(а): повтор того же доказательства %d, ожидался новый вызов (Replayed)", again)
	}
	t.Run("близнец: ошибка COMMIT — StoreUnavailable", func(t *testing.T) {
		c, a, _ := run(t, func(ps *PostgresStore) {
			ps.b.onCommitError = func(context.Context, pendingCommit, time.Time) Outcome { return StoreUnavailable }
		})
		if c != http.StatusServiceUnavailable || a != http.StatusTooManyRequests {
			t.Fatalf("близнец: %d → %d, ожидались 503 → 429 (вызов истрачен после 503)", c, a)
		}
	})
	t.Run("близнец: чтение без опроса pg_xact_status", func(t *testing.T) {
		c, a, _ := run(t, func(ps *PostgresStore) {
			ps.b.classify = func(*string, error) pollClass { return pollEnded }
		})
		if c != http.StatusServiceUnavailable || a != http.StatusTooManyRequests {
			t.Fatalf("близнец: %d → %d, ожидались 503 → 429 (чтение опередило фиксацию)", c, a)
		}
	})
}

// TestPg_CX2_46b_DroppedCommitIs503AndTheProofStaysFresh — (б) посредник
// отбрасывает COMMIT: сервер снимает транзакцию пределом простоя, фиксации нет
// — 503 не раньше конца исходной транзакции; повтор того же доказательства →
// Fresh и пропуск.
func TestPg_CX2_46b_DroppedCommitIs503AndTheProofStaysFresh(t *testing.T) {
	c := newCommitRig(t)
	c.proxy.set(func(p *pgProxy) { p.commitAction = "drop" })
	code, took := c.timed()
	c.proxy.set(func(p *pgProxy) { p.commitAction = "deliver" })
	again := c.send(pathRecovery, c.src, c.proof, "").Code
	t.Logf("(б) COMMIT отброшен: %d за %s · повтор %d", code, took, again)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("(б): %d, ожидался 503", code)
	}
	if took < anonMailDecisionBudget {
		t.Errorf("(б): 503 через %s — раньше конца исходной транзакции (предел простоя %s)", took, anonMailDecisionBudget)
	}
	if again != http.StatusOK {
		t.Fatalf("(б): повтор %d — 503 истратил вызов", again)
	}
	if strings.Contains(c.logs.String(), "commit outcome unresolved") {
		t.Errorf("(б): исход выяснен (строки нет), а журнал пишет «не разрешён»")
	}
}

// TestPg_UK64_CommitAppliedAnswersLostAllTheWindow — окно исключения Р5 (УК64,
// УК67): COMMIT доставлен и исполнен сервером, ответы на него и на каждый опрос
// отбрасываются весь anonMailResolveBudget → 503 не раньше срока от отправки
// фиксации, счётчик +1, WARN «исход фиксации не разрешён» с decision_id; затем
// посредник пропускает трафик — повтор того же доказательства → 429
// PROOF_OF_WORK_REQUIRED с новым вызовом, счёт источника вырос на 1 (г).
// Близнецы: COMMIT отброшен (фиксация не применена) — повтор проходит, счёт не
// вырос; опрос отвечен в срок — пропуск без 503.
func TestPg_UK64_CommitAppliedAnswersLostAllTheWindow(t *testing.T) {
	type res struct {
		code, again        int
		took               time.Duration
		before, mid, after int
		unavailable        uint64
		warned, decisionID bool
	}
	run := func(t *testing.T, action string, mute time.Duration) res {
		c := newCommitRig(t)
		before := sourceCount(t, c.dsn, c.sourceKey())
		c.proxy.set(func(p *pgProxy) { p.commitAction, p.commitDelay = action, mute })
		code, took := c.timed()
		c.proxy.set(func(p *pgProxy) { p.commitAction, p.muteUntil = "deliver", time.Time{} })
		time.Sleep(anonMailDecisionBudget + time.Second) // сервер снял брошенную транзакцию; пул заменил соединения
		mid := sourceCount(t, c.dsn, c.sourceKey())
		again := c.send(pathRecovery, c.src, c.proof, "").Code
		logs := c.logs.String()
		return res{
			code: code, again: again, took: took,
			before: before, mid: mid, after: sourceCount(t, c.dsn, c.sourceKey()),
			unavailable: c.gate.Stats().StoreUnavailable,
			warned:      strings.Contains(logs, "commit outcome unresolved"),
			decisionID:  strings.Contains(logs, "decision_id="),
		}
	}
	window := anonMailResolveBudget + 500*time.Millisecond
	g := run(t, "blackhole", window)
	t.Logf("окно (в, г): %d за %s · счётчик %d · WARN %v (decision_id %v) · повтор %d · счёт источника до %d · после окна %d · после повтора %d",
		g.code, g.took, g.unavailable, g.warned, g.decisionID, g.again, g.before, g.mid, g.after)
	if g.code != http.StatusServiceUnavailable || g.unavailable != 1 || !g.warned || !g.decisionID {
		t.Fatalf("окно: %+v — ожидались 503, счётчик 1, WARN с decision_id", g)
	}
	if g.took < anonMailResolveBudget {
		t.Errorf("503 через %s — раньше срока разрешения %s", g.took, anonMailResolveBudget)
	}
	if g.again != http.StatusTooManyRequests || g.mid != g.before+1 || g.after != g.mid {
		t.Fatalf("(г): повтор %d, счёт %d → %d → %d — ожидались новый вызов и рост счёта на 1 окном", g.again, g.before, g.mid, g.after)
	}
	t.Run("близнец (г): фиксация не применена", func(t *testing.T) {
		tw := run(t, "dropMute", window)
		t.Logf("близнец (г): %+v", tw)
		if tw.code != http.StatusServiceUnavailable || tw.again != http.StatusOK || tw.mid != tw.before {
			t.Fatalf("близнец: %+v — ожидались 503, затем пропуск, счёт до повтора не вырос", tw)
		}
	})
	t.Run("близнец: опрос отвечен в срок", func(t *testing.T) {
		tw := run(t, "blackhole", anonMailCommitWait+anonMailCancelGrace+200*time.Millisecond)
		t.Logf("близнец (опрос в срок): %+v", tw)
		if tw.code != http.StatusOK || tw.warned {
			t.Fatalf("близнец: %+v — ожидался пропуск без 503 и без WARN", tw)
		}
	})
}

// TestPg_CX2_46_RemainderBaseUnreachableAllTheWindow — проба остатка:
// посредник задерживает COMMIT и не пропускает никаких соединений на всё окно
// разрешения → 503 не раньше anonMailResolveBudget, счётчик +1, WARN с
// decision_id.
func TestPg_CX2_46_RemainderBaseUnreachableAllTheWindow(t *testing.T) {
	c := newCommitRig(t)
	c.proxy.set(func(p *pgProxy) { p.commitAction, p.commitDelay = "outage", anonMailResolveBudget+2*time.Second })
	code, took := c.timed()
	logs := c.logs.String()
	t.Logf("остаток: %d за %s · счётчик %d", code, took, c.gate.Stats().StoreUnavailable)
	if code != http.StatusServiceUnavailable || c.gate.Stats().StoreUnavailable != 1 {
		t.Fatalf("остаток: %d, счётчик %d", code, c.gate.Stats().StoreUnavailable)
	}
	if took < anonMailResolveBudget {
		t.Errorf("503 через %s — раньше срока разрешения", took)
	}
	if max := anonMailDecisionBudget + anonMailCommitWait + anonMailResolveBudget + 3*cancelCost + time.Second; took > max {
		t.Errorf("503 через %s — дольше худшей цены %s", took, max)
	}
	if !strings.Contains(logs, "commit outcome unresolved") || !strings.Contains(logs, "decision_id=") {
		t.Errorf("нет WARN «исход фиксации не разрешён» с decision_id:\n%s", logs)
	}
}
