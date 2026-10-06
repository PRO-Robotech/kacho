// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// claimdeadline_test.go — срок вызова `Claim` и установления `Subscribe`
// (замысел З21 «Вызов `Claim` и установление `Subscribe` — под своим сроком»,
// SDR-Н2, CX1-71, CX1-72; §8 — `sourceCallTimeout` = 10 с).
//
// Часы настоящие: срок — константа сборки, а не ручка, и пробы меряют именно
// её. Нижние границы утверждаются вместе с верхними: срок, равный такту
// `claimInterval` (1 с), отменял бы живой, но медленный источник.

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// §8 — срок вызова источника — константа 10 с.
func TestSourceCallTimeoutIsTheDesignConstant(t *testing.T) {
	t.Parallel()
	if sourceCallTimeout != wantCallTimeout {
		t.Fatalf("sourceCallTimeout = %s, замысел §8 называет %s", sourceCallTimeout, wantCallTimeout)
	}
}

// SDR-Н2 — источник A не отвечает на `Claim` (вариант — не шлёт
// `SubscriptionOpened`): строки B доставлены, вызов A завершён по сроку,
// после разморозки строки A доставлены не позже `claimInterval` +
// `sourceCallTimeout`. Близнец — A отвечает: строки обоих доставлены.
func TestStalledSourceDoesNotStopOtherSources(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)

	t.Run("claim-stalls", func(t *testing.T) {
		t.Parallel()
		a := newFakeSource(t, ca, "alpha", sourceOpts{})
		b := newFakeSource(t, ca, "beta", sourceOpts{})
		a.setFrozen(true)
		d := newDeliverer(8, a, b)
		startLoops(t, ca, []config.Source{a.record(config.AuthorizationResolveSend),
			b.record(config.AuthorizationResolveSend)}, d, tick)

		if !waitFor(5*time.Second, func() bool { return len(a.claimCalls()) >= 1 }) {
			t.Fatal("SDR-Н2: notify не вызвал Claim у A за 5 с")
		}
		hung := a.claimCalls()[0]
		idsB := b.put(1, true)
		if !waitFor(3*time.Second, func() bool { n, _ := d.deliveries("beta", idsB[0]); return n == 1 }) {
			t.Fatalf("SDR-Н2: строка B %s не доставлена за 3 с, пока A висит на Claim — зависший источник "+
				"держит чужой цикл", idsB[0])
		}
		a.setFrozen(false)
		unfrozen := time.Now()
		idsA := a.put(1, true)

		if !waitFor(time.Until(hung.start.Add(wantCallTimeout+2*time.Second)), func() bool { return a.claimCalls()[0].ended }) {
			t.Fatalf("SDR-Н2: вызов Claim к зависшему A не завершён за sourceCallTimeout (%s) + 2 с — у вызова "+
				"нет своего срока", wantCallTimeout)
		}
		c := a.claimCalls()[0]
		if lived := c.end.Sub(c.start); lived < wantCallTimeout-500*time.Millisecond {
			t.Fatalf("SDR-Н2: вызов Claim к A отменён через %s — раньше sourceCallTimeout %s (срок — не ручка claimInterval)",
				lived, wantCallTimeout)
		}
		bound := tick + wantCallTimeout + time.Second
		if !waitFor(time.Until(unfrozen.Add(bound)), func() bool { n, _ := d.deliveries("alpha", idsA[0]); return n == 1 }) {
			t.Fatalf("SDR-Н2: после разморозки строка A %s не доставлена за claimInterval + sourceCallTimeout "+
				"(+1 с) = %s; вызовов Claim у A %d", idsA[0], bound, len(a.claimCalls()))
		}
	})

	t.Run("opened-never-comes", func(t *testing.T) {
		t.Parallel()
		a := newFakeSource(t, ca, "alpha", sourceOpts{mode: subSilent})
		b := newFakeSource(t, ca, "beta", sourceOpts{})
		d := newDeliverer(8, a, b)
		startLoops(t, ca, []config.Source{a.record(config.AuthorizationResolveSend),
			b.record(config.AuthorizationResolveSend)}, d, tick)

		if !waitFor(5*time.Second, func() bool { return len(a.streamsSeen()) >= 1 }) {
			t.Fatal("SDR-Н2: notify не открыл подписку у A за 5 с")
		}
		idsB := b.put(1, true)
		if !waitFor(3*time.Second, func() bool { n, _ := d.deliveries("beta", idsB[0]); return n == 1 }) {
			t.Fatalf("SDR-Н2: строка B %s не доставлена за 3 с, пока поток A не открыт", idsB[0])
		}
		put := time.Now()
		idsA := a.put(1, false)
		bound := tick + wantCallTimeout + time.Second
		if !waitFor(time.Until(put.Add(bound)), func() bool { n, _ := d.deliveries("alpha", idsA[0]); return n == 1 }) {
			t.Fatalf("SDR-Н2: строка A %s не доставлена по таймеру за %s при молчащем потоке A", idsA[0], bound)
		}
		opened := a.streamsSeen()[0].start
		if !waitFor(time.Until(opened.Add(wantCallTimeout+2*time.Second)), func() bool { return a.streamsSeen()[0].ended }) {
			t.Fatalf("SDR-Н2: установление потока A без SubscriptionOpened не отменено за sourceCallTimeout (%s) + 2 с",
				wantCallTimeout)
		}
		first := a.streamsSeen()[0]
		if lived := first.end.Sub(first.start); lived < wantCallTimeout-500*time.Millisecond {
			t.Fatalf("SDR-Н2: установление потока A отменено через %s — раньше sourceCallTimeout %s", lived, wantCallTimeout)
		}
	})

	t.Run("twin-both-answer", func(t *testing.T) {
		t.Parallel()
		a := newFakeSource(t, ca, "alpha", sourceOpts{})
		b := newFakeSource(t, ca, "beta", sourceOpts{})
		d := newDeliverer(8, a, b)
		startLoops(t, ca, []config.Source{a.record(config.AuthorizationResolveSend),
			b.record(config.AuthorizationResolveSend)}, d, tick)

		if !waitFor(5*time.Second, func() bool { return len(a.streamsSeen()) >= 1 && len(b.streamsSeen()) >= 1 }) {
			t.Fatal("близнец SDR-Н2: потоки A и B не открыты за 5 с")
		}
		idsA, idsB := a.put(1, true), b.put(1, true)
		if !waitFor(3*time.Second, func() bool {
			na, _ := d.deliveries("alpha", idsA[0])
			nb, _ := d.deliveries("beta", idsB[0])
			return na == 1 && nb == 1
		}) {
			na, _ := d.deliveries("alpha", idsA[0])
			nb, _ := d.deliveries("beta", idsB[0])
			t.Fatalf("близнец SDR-Н2: оба источника отвечают, доставлено A %d, B %d — ожидалось по 1", na, nb)
		}
	})
}

// CX1-72 — источник коммитит аренду и отвечает позже срока: вызов отменён,
// `notify_claim_call_timeouts_total{source}` +1, строки отправлены по `Claim`
// после конца аренды, писем по строке 1. Близнец — ответ за 9 с при
// `claimInterval` = 1 с: не отменён, счётчик 0.
func TestClaimCallTimeoutAfterLeaseCommitIsCounted(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)

	t.Run("late-answer-is-cancelled", func(t *testing.T) {
		t.Parallel()
		const lease = 3 * time.Second
		src := newFakeSource(t, ca, "probe", sourceOpts{leaseTTL: lease})
		ids := src.put(1, false)
		src.delayNext(wantCallTimeout + time.Second)
		d := newDeliverer(8, src)
		reg := startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)

		if !waitFor(5*time.Second, func() bool { return len(src.claimCalls()) >= 1 }) {
			t.Fatal("CX1-72: notify не вызвал Claim за 5 с")
		}
		if !waitFor(wantCallTimeout+5*time.Second, func() bool { return src.claimCalls()[0].ended }) {
			t.Fatalf("CX1-72: первый вызов Claim (ответ через %s) не завершён за %s — у вызова нет своего срока",
				wantCallTimeout+time.Second, wantCallTimeout+5*time.Second)
		}
		c := src.claimCalls()[0]
		if c.leased != 1 {
			t.Fatalf("фикстура CX1-72: первый вызов закоммитил аренду %d строк, ожидалась 1", c.leased)
		}
		if c.err == nil {
			t.Fatalf("CX1-72: вызов с ответом через %s не отменён по сроку sourceCallTimeout %s", wantCallTimeout+time.Second, wantCallTimeout)
		}
		if lived := c.end.Sub(c.start); lived < wantCallTimeout-500*time.Millisecond {
			t.Fatalf("CX1-72: вызов отменён через %s — раньше sourceCallTimeout %s", lived, wantCallTimeout)
		}
		if !waitFor(2*time.Second, func() bool {
			v, ok := counterValue(t, reg, "notify_claim_call_timeouts_total", "probe")
			return ok && v == 1
		}) {
			v, ok := counterValue(t, reg, "notify_claim_call_timeouts_total", "probe")
			t.Fatalf("CX1-72: notify_claim_call_timeouts_total{source=\"probe\"} = %v (ряд есть: %v), ожидалось 1", v, ok)
		}
		if !waitFor(tick+3*time.Second, func() bool { n, _ := d.deliveries("probe", ids[0]); return n >= 1 }) {
			t.Fatalf("CX1-72: строка %s, чья аренда закоммичена отменённым вызовом, не отправлена по следующему "+
				"Claim после конца аренды", ids[0])
		}
		if _, at := d.deliveries("probe", ids[0]); at.Before(c.start.Add(lease)) {
			t.Fatalf("CX1-72: строка доставлена раньше конца аренды отменённого вызова")
		}
		<-time.After(3 * tick)
		if n, _ := d.deliveries("probe", ids[0]); n != 1 {
			t.Fatalf("CX1-72: строка %s дошла до получателя %d раз, ожидался 1", ids[0], n)
		}
	})

	t.Run("twin-answer-within-deadline", func(t *testing.T) {
		t.Parallel()
		src := newFakeSource(t, ca, "probe", sourceOpts{leaseTTL: 30 * time.Second})
		ids := src.put(1, false)
		src.delayNext(9 * time.Second)
		d := newDeliverer(8, src)
		reg := startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)

		if !waitFor(5*time.Second, func() bool { return len(src.claimCalls()) >= 1 }) {
			t.Fatal("близнец CX1-72: notify не вызвал Claim за 5 с")
		}
		if !waitFor(wantCallTimeout+5*time.Second, func() bool { return src.claimCalls()[0].ended }) {
			t.Fatal("близнец CX1-72: первый вызов Claim (ответ через 9 с) не завершён за 15 с")
		}
		if c := src.claimCalls()[0]; c.err != nil {
			t.Fatalf("близнец CX1-72: ответ за 9 с при claimInterval %s отменён через %s — срок вызова короче "+
				"sourceCallTimeout %s", tick, c.end.Sub(c.start), wantCallTimeout)
		}
		if !waitFor(2*time.Second, func() bool { n, _ := d.deliveries("probe", ids[0]); return n == 1 }) {
			t.Fatalf("близнец CX1-72: строка %s из ответа за 9 с не дошла до получателя", ids[0])
		}
		if v, ok := counterValue(t, reg, "notify_claim_call_timeouts_total", "probe"); ok && v != 0 {
			t.Fatalf("близнец CX1-72: notify_claim_call_timeouts_total{source=\"probe\"} = %v, ожидалось 0", v)
		}
	})
}
