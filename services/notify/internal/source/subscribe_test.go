// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// subscribe_test.go — подписка notify на ленту источника (приёмка NTF-1,
// раздел E; замысел З21): событие ленты будит `Claim`, недоступный поток не
// останавливает доставку по таймеру, исправный поток открывается один раз и
// не переоткрывается сроком.
//
// Предмет — сторона notify. Что источник не отдаёт событие службе без `reader`
// (NTF1-E02), решает сервер подписки источника (corelib), а не цикл notify, и
// здесь не утверждается.

import (
	"slices"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// NTF1-E01 (сторона цикла) — notify открыл подписку на ленту с
// `kinds: ["notification_feed"]`; при открытии зовёт `Claim`; событие ленты
// будит `Claim`, и строка доходит до получателя. Такт таймера — 5 мин, так что
// доставку за секунды объясняет только событие.
func TestSource_NTF1E01_FeedEventWakesClaim(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{})
	d := newDeliverer(8, src)
	startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, 5*time.Minute)

	if !waitFor(5*time.Second, func() bool { return len(src.streamsSeen()) >= 1 }) {
		t.Fatal("NTF1-E01: notify не открыл подписку на ленту источника за 5 с")
	}
	if got := src.streamsSeen()[0].kinds; !slices.Equal(got, []string{feedKind}) {
		t.Fatalf("NTF1-E01: подписка открыта с kinds %q, ожидалось [%q]", got, feedKind)
	}
	if !waitFor(3*time.Second, func() bool {
		c := src.claimCalls()
		return len(c) >= 1 && c[0].ended
	}) {
		t.Fatal("NTF1-E01: при открытии потока Claim не вызван за 3 с (З21: Claim — по событию, при (пере)открытии и по таймеру)")
	}
	before := len(src.claimCalls())

	ids := src.put(1, true)
	if !waitFor(3*time.Second, func() bool { n, _ := d.deliveries("probe", ids[0]); return n == 1 }) {
		t.Fatalf("NTF1-E01: строка %s не дошла до получателя за 3 с после события ленты при такте 5 мин; "+
			"вызовов Claim до события %d, после %d", ids[0], before, len(src.claimCalls()))
	}
	if after := len(src.claimCalls()); after <= before {
		t.Fatalf("NTF1-E01: событие ленты не вызвало Claim (вызовов до %d, после %d)", before, after)
	}
}

// NTF1-E03 — сервер подписки источника остановлен, сервер ленты доступен:
// строка дошла до получателя не позже такта T после постановки.
//
// Часы — настоящие (запас 500 мс на вызов петли); метрика «поток источника
// недоступен» из сценария здесь не утверждается: её имени нет ни в приёмке,
// ни в перечне метрик З27 — вопрос к приёмке в возврате полосы.
func TestSource_NTF1E03_StreamDownRowGoesByTimer(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{mode: subUnavailable})
	d := newDeliverer(8, src)
	startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)

	if !waitFor(5*time.Second, func() bool { return len(src.streamsSeen()) >= 1 }) {
		t.Fatal("NTF1-E03: notify ни разу не попытался открыть подписку за 5 с")
	}
	put := time.Now()
	ids := src.put(1, false)
	budget := tick + 500*time.Millisecond
	if !waitFor(budget, func() bool { n, _ := d.deliveries("probe", ids[0]); return n == 1 }) {
		t.Fatalf("NTF1-E03: при недоступном потоке строка %s не дошла до получателя за такт %s (+500 мс) "+
			"после постановки; вызовов Claim %d", ids[0], tick, len(src.claimCalls()))
	}
	if _, at := d.deliveries("probe", ids[0]); at.Sub(put) > budget {
		t.Fatalf("NTF1-E03: строка дошла через %s, позже такта %s", at.Sub(put), tick)
	}
}

// CX1-71 — исправный источник: `claimInterval` = 1 с, пять тактов → поток
// открыт ровно один раз (и счётчик открытий = 1); строка, поставленная после
// пятого такта, доставлена по событию раньше следующего такта.
func TestHealthySubscribeStreamOpensOnce(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{})
	d := newDeliverer(8, src)
	reg := startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)

	if !waitFor(10*time.Second, func() bool { return len(src.claimCalls()) >= 6 }) {
		t.Fatalf("CX1-71: за 10 с при такте %s Claim вызван %d раз, ожидалось ≥ 6 (открытие + 5 тактов)",
			tick, len(src.claimCalls()))
	}
	if n := len(src.streamsSeen()); n != 1 {
		t.Fatalf("CX1-71: за пять тактов поток подписки открыт %d раз, ожидался 1 — исправный поток "+
			"рвётся сроком, и доставка по событию стала опросом по таймеру", n)
	}
	if v, ok := counterValue(t, reg, "notify_source_stream_opens_total", "probe"); !ok || v != 1 {
		t.Fatalf("CX1-71: notify_source_stream_opens_total{source=\"probe\"} = %v (ряд есть: %v), ожидалось 1", v, ok)
	}

	// Строка ставится сразу после очередного такта: следующий такт — через
	// ~1 с, событие — через миллисекунды.
	n0 := len(src.claimCalls())
	if !waitFor(3*time.Second, func() bool {
		c := src.claimCalls()
		return len(c) > n0 && c[len(c)-1].ended
	}) {
		t.Fatal("CX1-71: очередной такт Claim не наступил за 3 с")
	}
	put := time.Now()
	ids := src.put(1, true)
	if !waitFor(3*time.Second, func() bool { n, _ := d.deliveries("probe", ids[0]); return n == 1 }) {
		t.Fatalf("CX1-71: строка %s не доставлена за 3 с", ids[0])
	}
	if _, at := d.deliveries("probe", ids[0]); at.Sub(put) >= tick*6/10 {
		t.Fatalf("CX1-71: строка доставлена через %s после постановки — не раньше такта %s: событие "+
			"ленты Claim не будит", at.Sub(put), tick)
	}
	if n := len(src.streamsSeen()); n != 1 {
		t.Fatalf("CX1-71: после доставки по событию открытий потока %d, ожидался 1", n)
	}
}

// CX1-71, второй близнец — источник не шлёт `SubscriptionOpened`: установление
// отменено по `sourceCallTimeout` (а не по такту `claimInterval`), поток
// открыт заново, открытий больше одного.
func TestSubscribeWithoutOpenedIsReopenedAfterCallTimeout(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{mode: subSilent})
	d := newDeliverer(8, src)
	reg := startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)

	if !waitFor(5*time.Second, func() bool { return len(src.streamsSeen()) >= 1 }) {
		t.Fatal("CX1-71: notify не открыл подписку за 5 с")
	}
	if !waitFor(wantCallTimeout+3*time.Second, func() bool { s := src.streamsSeen(); return s[0].ended }) {
		t.Fatalf("CX1-71: поток без SubscriptionOpened не отменён за %s — у установления нет срока", wantCallTimeout+3*time.Second)
	}
	first := src.streamsSeen()[0]
	if lived := first.end.Sub(first.start); lived < wantCallTimeout-500*time.Millisecond || lived > wantCallTimeout+2*time.Second {
		t.Fatalf("CX1-71: установление потока отменено через %s, ожидалось ≈ sourceCallTimeout %s", lived, wantCallTimeout)
	}
	if !waitFor(tick+3*time.Second, func() bool { return len(src.streamsSeen()) >= 2 }) {
		t.Fatalf("CX1-71: после отмены установления поток не открыт заново (открытий %d)", len(src.streamsSeen()))
	}
	if v, ok := counterValue(t, reg, "notify_source_stream_opens_total", "probe"); !ok || v < 2 {
		t.Fatalf("CX1-71: notify_source_stream_opens_total{source=\"probe\"} = %v (ряд есть: %v), ожидалось > 1", v, ok)
	}
}
