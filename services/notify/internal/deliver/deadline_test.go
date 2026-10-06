// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// deadline_test.go — срок обработки строки и контекст `Ack` (замысел З21,
// SDR-Н1; условия к коду УК31, УК37, УК65, УК71, УК80; CX1-27, CX1-57).
//
// Срок строки — по МОНОТОННЫМ часам notify от отправки `Claim` (Batch.SentAt):
// конец аренды `t_send + lease_remaining`, конец срока строки
// `t_send + expires_in`. Моменты часов источника (enqueued_at) в вычисление
// не входят. Сумма сроков пробы — 100ms + 1s + AckMargin(5s) = 6.1 с.

import (
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

func bhelloBuild(t *testing.T) *fixtureBuild {
	return buildOf(t, buildEntry{nsProbe, "probe-bhello", 1})
}

func bhelloRow(s rowSpec) *notifyv1.ClaimedNotification {
	s.template, s.rev, s.attrs = "probe-bhello", 1, map[string]string{"target": "/"}
	return claimed(s)
}

// wantNotStarted — строка не доведена до MAIL FROM и `Ack` не звался
// (клетка 8: «без Ack», З21).
func (r *rig) wantNotStarted(t *testing.T, module string, row *notifyv1.ClaimedNotification) {
	t.Helper()
	if n := r.sessions(); n != 0 {
		t.Errorf("SMTP-сессий %d, ждали 0", n)
	}
	if n := len(r.limiter.Reserves()); n != 0 {
		t.Errorf("резервов сетки %d, ждали 0", n)
	}
	if calls := r.feeds[module].Calls(row.GetId()); len(calls) != 0 {
		t.Errorf("Ack звался %d раз (последний — %s), ждали 0", len(calls), outcomeString(calls[len(calls)-1].req.GetOutcome()))
	}
}

// leaseShortCount — notify_lease_budget_short_total{source}; серии нет — 0.
func (r *rig) leaseShortCount(t *testing.T, module string) float64 {
	t.Helper()
	v, _ := counterValue(t, r.reg, "notify_lease_budget_short_total", map[string]string{"source": module})
	return v
}

// ── УК65, УК31: аренда короче суммы сроков ───────────────────────────────────

// TestLeaseShorterThanDeadlineSumStartsNoSession — аренда фикстурного сервера
// короче суммы сроков сборки notify → строка не доводится до MAIL FROM, `Ack`
// не шлётся, счётчик notify_lease_budget_short_total +1. Близнец — аренда,
// которой сумма сроков помещается, — письмо отправлено.
func TestLeaseShorterThanDeadlineSumStartsNoSession(t *testing.T) {
	t.Run("аренда 6 с < суммы сроков 6.1 с", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t)})
		rw := bhelloRow(rowSpec{lease: leaseShort})
		if got := r.one(nsProbe, rw); got != nil {
			t.Fatalf("исход записан: %s, ждали «без Ack»", outcomeString(got))
		}
		r.wantNotStarted(t, nsProbe, rw)
		if v := r.leaseShortCount(t, nsProbe); v != 1 {
			t.Fatalf("notify_lease_budget_short_total{source=probe} = %v, ждали 1", v)
		}
	})
	t.Run("близнец: аренда 8 с", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t)})
		rw := bhelloRow(rowSpec{lease: leaseOK})
		r.wantSent(t, rw, r.one(nsProbe, rw))
		if v := r.leaseShortCount(t, nsProbe); v != 0 {
			t.Fatalf("notify_lease_budget_short_total{source=probe} = %v, ждали 0", v)
		}
	})
}

// ── УК31, УК37: DATA дольше аренды ───────────────────────────────────────────

// TestDataLongerThanLeaseGivesOneSession — узел отвечает на DATA позже конца
// аренды → SMTP-сессий по строке не больше одной: срок сессии ставит notify
// (крайний момент строки), отправитель со своим пределом 30 с его не
// заменяет. Строка без записанного исхода выдаётся снова следующим `Claim`
// (так делает источник после конца аренды) — второй сессии быть не должно.
func TestDataLongerThanLeaseGivesOneSession(t *testing.T) {
	stop := make(chan struct{})
	r := newRig(t, rigOpts{build: bhelloBuild(t), relay: smtptest.Script{Data: func() smtptest.Reply {
		select {
		case <-time.After(leaseOK + time.Second):
			return smtptest.OK
		case <-stop:
			return smtptest.Reply{Code: 451, Text: "4.3.0 probe stopped"}
		}
	}}})
	t.Cleanup(func() { close(stop) })
	rw := bhelloRow(rowSpec{lease: leaseOK})
	sentAt := time.Now()
	r.deliver(nsProbe, sentAt, rw)
	r.reclaimUnrecorded(nsProbe, rw)
	if n := r.sessions(); n > 1 {
		t.Fatalf("SMTP-сессий по строке %d, ждали не больше 1 — исход не записан в аренде, строка выдана снова и сессия повторена", n)
	}
	d := r.sender.Deadlines()
	if len(d) != 1 || d[0].IsZero() {
		t.Fatalf("срок SMTP-сессии не поставлен notify: %v", d)
	}
	if lim := sentAt.Add(leaseOK - config.AckMargin); d[0].After(lim) {
		t.Fatalf("срок SMTP-сессии %v после конца аренды минус AckMargin на %v", d[0].Sub(sentAt), d[0].Sub(lim))
	}
	calls := r.feeds[nsProbe].Calls(rw.GetId())
	for _, c := range calls {
		if !c.at.Before(sentAt.Add(leaseOK)) {
			t.Fatalf("Ack после конца аренды: через %v от отправки Claim", c.at.Sub(sentAt))
		}
	}
}

// ── УК71: часы источника сдвинуты ────────────────────────────────────────────

// TestSourceClockShiftDoesNotEnterRowDeadline — часы базы источника на +10 с
// (enqueued_at в будущем для notify), аренда 8 с, узел отвечает на DATA
// через 3 с (дольше срока сессии notify) → SMTP-сессий ≤ 1, срок сессии —
// от отправки Claim по часам notify, Ack не опаздывает. Близнец без сдвига —
// то же.
func TestSourceClockShiftDoesNotEnterRowDeadline(t *testing.T) {
	for _, shift := range []time.Duration{10 * time.Second, 0} {
		t.Run("сдвиг "+shift.String(), func(t *testing.T) {
			stop := make(chan struct{})
			r := newRig(t, rigOpts{build: bhelloBuild(t), relay: smtptest.Script{Data: func() smtptest.Reply {
				select {
				case <-time.After(3 * time.Second):
					return smtptest.OK
				case <-stop:
					return smtptest.Reply{Code: 451, Text: "4.3.0 probe stopped"}
				}
			}}})
			t.Cleanup(func() { close(stop) })
			rw := bhelloRow(rowSpec{lease: leaseOK, enqueuedShift: shift})
			sentAt := time.Now()
			r.deliver(nsProbe, sentAt, rw)
			r.reclaimUnrecorded(nsProbe, rw)
			if n := r.sessions(); n > 1 {
				t.Fatalf("SMTP-сессий %d, ждали ≤ 1", n)
			}
			d := r.sender.Deadlines()
			if len(d) == 0 || d[0].IsZero() {
				t.Fatalf("срок SMTP-сессии не поставлен notify: %v", d)
			}
			limit := probeResolveSendTimeout + probeSMTPSessionTimeout + 250*time.Millisecond
			if got := d[0].Sub(sentAt); got > limit {
				t.Fatalf("срок SMTP-сессии через %v от отправки Claim, ждали ≤ %v (сроки notify)", got, limit)
			}
			calls := r.feeds[nsProbe].Calls(rw.GetId())
			if len(calls) == 0 {
				t.Fatal("Ack по строке не звался")
			}
			for _, c := range calls {
				if !c.at.Before(sentAt.Add(leaseOK)) {
					t.Fatalf("Ack опоздал: через %v от отправки Claim", c.at.Sub(sentAt))
				}
			}
		})
	}
}

// TestRowExpiryIsJudgedByNotifyMonotonicClock — срок строки (`expires_in`)
// судится по монотонным часам notify от отправки Claim (Р11, З21): часы
// notify ушли на 1 с, срок строки 500 мс, часы источника на +10 с → строку
// notify не начинает и `Ack` не шлёт. Близнец — срок строки 1 ч.
func TestRowExpiryIsJudgedByNotifyMonotonicClock(t *testing.T) {
	t.Run("срок прошёл", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t), clock: &testClock{advance: time.Second}})
		rw := bhelloRow(rowSpec{lease: leaseOK + 2*time.Second, expiresIn: 500 * time.Millisecond, enqueuedShift: 10 * time.Second})
		if got := r.one(nsProbe, rw); got != nil {
			t.Fatalf("исход записан: %s, ждали «без Ack»", outcomeString(got))
		}
		r.wantNotStarted(t, nsProbe, rw)
	})
	t.Run("близнец: срок 1 ч", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t), clock: &testClock{advance: time.Second}})
		rw := bhelloRow(rowSpec{lease: leaseOK + 2*time.Second, expiresIn: time.Hour, enqueuedShift: 10 * time.Second})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
}

// TestLeaseBudgetIsReadFromInjectedMonotonicClock — остаток аренды считают
// внедряемые часы notify: часы ушли на 3 с, аренда 8 с → остаток 5 с меньше
// суммы сроков → строка не начата. Близнец — часы без сдвига.
func TestLeaseBudgetIsReadFromInjectedMonotonicClock(t *testing.T) {
	t.Run("часы +3 с", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t), clock: &testClock{advance: 3 * time.Second}})
		rw := bhelloRow(rowSpec{lease: leaseOK})
		if got := r.one(nsProbe, rw); got != nil {
			t.Fatalf("исход записан: %s, ждали «без Ack»", outcomeString(got))
		}
		r.wantNotStarted(t, nsProbe, rw)
		if v := r.leaseShortCount(t, nsProbe); v != 1 {
			t.Fatalf("notify_lease_budget_short_total{source=probe} = %v, ждали 1", v)
		}
	})
	t.Run("близнец: часы без сдвига", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t), clock: &testClock{}})
		rw := bhelloRow(rowSpec{lease: leaseOK})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
}

// ── УК80: стенные часы прыгнули назад ────────────────────────────────────────

// TestWallClockJumpBackDoesNotMoveRowDeadline — стенные часы прыгают назад на
// 1 ч между Claim и проверкой срока, монотонные — нет: крайний момент строки
// не сдвинулся — аренда 6 с по-прежнему короче суммы сроков, строка не
// начата. Снявший монотонное показание (`time.Now().UTC()`, `.Round(0)`)
// получил бы остаток «6 с + 1 ч» и начал бы сессию. Близнецы — тот же
// случай без скачка (не начата) и аренда 8 с со скачком (отправлено).
func TestWallClockJumpBackDoesNotMoveRowDeadline(t *testing.T) {
	for _, jump := range []time.Duration{-time.Hour, 0} {
		t.Run("скачок "+jump.String()+", аренда 6 с", func(t *testing.T) {
			r := newRig(t, rigOpts{build: bhelloBuild(t), clock: &testClock{wallJump: jump}})
			rw := bhelloRow(rowSpec{lease: leaseShort})
			if got := r.one(nsProbe, rw); got != nil {
				t.Fatalf("исход записан: %s, ждали «без Ack»", outcomeString(got))
			}
			r.wantNotStarted(t, nsProbe, rw)
			if v := r.leaseShortCount(t, nsProbe); v != 1 {
				t.Fatalf("notify_lease_budget_short_total{source=probe} = %v, ждали 1", v)
			}
		})
	}
	t.Run("близнец: скачок -1h, аренда 8 с", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t), clock: &testClock{wallJump: -time.Hour}})
		rw := bhelloRow(rowSpec{lease: leaseOK})
		r.wantSent(t, rw, r.one(nsProbe, rw))
	})
}

// ── SDR-Н1: контекст Ack ─────────────────────────────────────────────────────

// TestAckAfterRowDeadlineStillLands — узел отвечает на DATA у самого
// крайнего момента строки, ответ ленты на `Ack` приходит уже после него →
// `Ack` доведён (он под `WithDeadline(WithoutCancel(<строка>), t_send +
// lease_remaining)`), строка `sent`, писем 1. Под контекстом строки `Ack`
// упал бы, строку выдал бы следующий Claim, и писем стало бы 2.
func TestAckAfterRowDeadlineStillLands(t *testing.T) {
	var snd atomic.Pointer[recordingSender]
	r := newRig(t, rigOpts{build: bhelloBuild(t), relay: smtptest.Script{Data: func() smtptest.Reply {
		if eff, ok := snd.Load().LastEffective(); ok {
			time.Sleep(time.Until(eff.Add(-150 * time.Millisecond)))
		}
		return smtptest.OK
	}}})
	snd.Store(r.sender)
	rw := bhelloRow(rowSpec{lease: leaseOK})
	r.feeds[nsProbe].ackDelay[rw.GetId()] = func() time.Time {
		eff, _ := r.sender.LastEffective()
		return eff.Add(400 * time.Millisecond)
	}
	sentAt := time.Now()
	r.deliver(nsProbe, sentAt, rw)
	again := r.reclaimUnrecorded(nsProbe, rw)

	eff, ok := r.sender.LastEffective()
	if !ok {
		t.Fatalf("SMTP-сессии не было: строка не доведена до отправки (исход %s)", outcomeString(r.feeds[nsProbe].Recorded(rw.GetId())))
	}
	calls := r.feeds[nsProbe].Calls(rw.GetId())
	if len(calls) == 0 {
		t.Fatal("Ack по строке не звался")
	}
	// Условие пробы: ответ ленты на Ack (eff + 400ms) приходит позже самого
	// позднего крайнего момента строки t_send + resolveSend + smtpSession.
	if !eff.Add(400 * time.Millisecond).After(sentAt.Add(probeResolveSendTimeout + probeSMTPSessionTimeout)) {
		t.Fatalf("ФИКСТУРА: условие не создано — ответ на Ack (через %v от Claim) не позже крайнего момента строки",
			eff.Add(400*time.Millisecond).Sub(sentAt))
	}
	wantOutcome(t, r.feeds[nsProbe].Recorded(rw.GetId()), outSent)
	if again != 0 || r.letters() != 1 {
		t.Fatalf("строк выдано повторно %d, писем %d — ждали 0 и 1", again, r.letters())
	}
	first := calls[0]
	if first.deadline.IsZero() {
		t.Fatal("у контекста Ack нет срока — ждали t_send + lease_remaining")
	}
	if got := first.deadline.Sub(sentAt); got > leaseOK+50*time.Millisecond || got < leaseOK-300*time.Millisecond {
		t.Fatalf("срок контекста Ack через %v от отправки Claim, ждали t_send + lease_remaining = %v", got, leaseOK)
	}
}

// TestAckRetriesTransientRefusalUntilLeaseEnd — первый `Ack` получает
// UNAVAILABLE либо DEADLINE_EXCEEDED → повтор до конца аренды, исход
// записан, писем 1. Близнец — INTERNAL: не повторяется (вызов один).
func TestAckRetriesTransientRefusalUntilLeaseEnd(t *testing.T) {
	for _, c := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded} {
		t.Run(c.String(), func(t *testing.T) {
			r := newRig(t, rigOpts{build: bhelloBuild(t), feed: func(_ string, f *fakeFeed) { f.failFirst = c }})
			rw := bhelloRow(rowSpec{lease: leaseOK})
			r.deliver(nsProbe, time.Now(), rw)
			again := r.reclaimUnrecorded(nsProbe, rw)
			wantOutcome(t, r.feeds[nsProbe].Recorded(rw.GetId()), outSent)
			if n := len(r.feeds[nsProbe].Calls(rw.GetId())); n != 2 {
				t.Fatalf("вызовов Ack %d, ждали 2 (отказ и повтор)", n)
			}
			if again != 0 || r.letters() != 1 {
				t.Fatalf("строк выдано повторно %d, писем %d — ждали 0 и 1", again, r.letters())
			}
		})
	}
	t.Run("близнец: INTERNAL не повторяется", func(t *testing.T) {
		r := newRig(t, rigOpts{build: bhelloBuild(t), feed: func(_ string, f *fakeFeed) { f.failFirst = codes.Internal }})
		rw := bhelloRow(rowSpec{lease: leaseOK})
		r.deliver(nsProbe, time.Now(), rw)
		if n := len(r.feeds[nsProbe].Calls(rw.GetId())); n != 1 {
			t.Fatalf("вызовов Ack %d, ждали 1", n)
		}
		if got := r.feeds[nsProbe].Recorded(rw.GetId()); got != nil {
			t.Fatalf("исход записан: %s, ждали «нет»", outcomeString(got))
		}
	})
}
