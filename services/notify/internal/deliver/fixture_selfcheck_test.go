// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// fixture_selfcheck_test.go — положительный контроль фикстуры без
// испытуемого. Каждое поведение, которым пробы полосы N3 ставят испытуемому
// вопрос, здесь исполнено и утверждено: сломанная фикстура краснеет сама и
// своим текстом, а не выдаёт себя за отсутствующую возможность `deliver`.

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/notify/spec"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

// TestFixtureTemplatesPassTheRealLoader — каждый шаблон фикстуры проходит
// загрузчик spec; ревизии одного шаблона отличаются набором (G25) либо
// классом (G26 (д)), а не именем.
func TestFixtureTemplatesPassTheRealLoader(t *testing.T) {
	loaded := map[string]spec.Template{}
	for name := range fixtureTemplates {
		loaded[name] = loadTemplate(t, name, 1)
	}
	t.Logf("перепись: шаблонов фикстуры %d, загружено %d", len(fixtureTemplates), len(loaded))
	if len(loaded) != 7 {
		t.Fatalf("шаблонов фикстуры %d, ждали 7", len(loaded))
	}
	all, r1 := loaded["probe-all"], loaded["probe-all-r1"]
	if all.Name != r1.Name {
		t.Fatalf("ревизии probe-all названы по-разному: %q и %q", all.Name, r1.Name)
	}
	if spec.SameSet(spec.SetOf(all), spec.SetOf(r1)) {
		t.Fatal("набор ревизии 1 probe-all совпал с ревизией 2 — условие G25 не создано")
	}
	if len(all.Attrs) != 6 || len(r1.Attrs) != 5 {
		t.Fatalf("атрибутов probe-all %d и ревизии 1 %d — ждали 6 и 5", len(all.Attrs), len(r1.Attrs))
	}
	opt, optSec := loaded["probe-opt"], loaded["probe-opt-security"]
	if opt.Name != optSec.Name || opt.Class != spec.ClassNotice || optSec.Class != spec.ClassSecurity {
		t.Fatalf("probe-opt: имена %q/%q, классы %q/%q — ждали одно имя, notice и security",
			opt.Name, optSec.Name, opt.Class, optSec.Class)
	}
	if loaded["probe-sec"].Class != spec.ClassSecurity {
		t.Fatal("probe-sec не класса security — условие G20 не создано")
	}
	b := buildOf(t, buildEntry{nsProbe, "probe-all", 2}, buildEntry{nsKaname, "probe-opt-security", 2})
	if tpl, ok := b.Template(nsProbe, "probe-all"); !ok || tpl.Revision.Number != 2 {
		t.Fatal("сборка не отдаёт probe/probe-all ревизии 2")
	}
	if _, ok := b.Template(nsProbeB, "probe-all"); ok {
		t.Fatal("сборка отдала шаблон чужого пространства")
	}
}

// TestFixtureRowsCarryTheClaimContract — строка фикстуры — форма ответа
// `Claim`: id `ntf-…`, токен-UUID, остатки длительностями, адрес.
func TestFixtureRowsCarryTheClaimContract(t *testing.T) {
	r := claimed(rowSpec{template: "probe-bhello", rev: 1, attrs: map[string]string{"target": "/"}})
	if !regexp.MustCompile(`^ntf-[0-9a-v]{17}$`).MatchString(r.GetId()) {
		t.Fatalf("id строки %q не формы ntf-…", r.GetId())
	}
	if _, err := uuid.Parse(r.GetLeaseToken()); err != nil {
		t.Fatalf("токен аренды не UUID: %v", err)
	}
	if r.GetLeaseRemaining().AsDuration() != leaseOK || r.GetExpiresIn().AsDuration() != expiresFar {
		t.Fatalf("остатки %v/%v, ждали %v/%v", r.GetLeaseRemaining().AsDuration(), r.GetExpiresIn().AsDuration(), leaseOK, expiresFar)
	}
	if r.GetAddress() != fixtureTo || r.GetClass() != notifyv1.NotificationClass_NOTICE {
		t.Fatalf("адресат %q, класс %v", r.GetAddress(), r.GetClass())
	}
	edited := withAttrs(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": delAttr, "c": ""})
	if _, ok := edited["a"]; ok || edited["b"] != "2" {
		t.Fatalf("withAttrs: %v", edited)
	}
	if v, ok := edited["c"]; !ok || v != "" {
		t.Fatal("withAttrs: пустое значение при присутствующем ключе потеряно")
	}
	if sumOfDeadlines != 6100*time.Millisecond || leaseShort >= sumOfDeadlines || leaseOK <= sumOfDeadlines {
		t.Fatalf("сумма сроков %v; аренды %v и %v не по разные стороны от неё", sumOfDeadlines, leaseShort, leaseOK)
	}
}

// TestFixtureFeedHonorsTheAckContract — правила оператора `Ack` (З9).
func TestFixtureFeedHonorsTheAckContract(t *testing.T) {
	ctx := context.Background()
	sent := &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_SENT}
	invalid := &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_INVALID, Reason: notifyv1.OutcomeReason_ATTRS_INVALID}
	ack := func(f *fakeFeed, r *notifyv1.ClaimedNotification, o *notifyv1.Outcome, d time.Duration) error {
		req := &notifyv1.AckRequest{Id: r.GetId(), LeaseToken: r.GetLeaseToken(), Outcome: o}
		if d != 0 {
			req.DeferFor = durationpb.New(d)
		}
		_, err := f.Ack(ctx, req)
		return err
	}
	wantCode := func(t *testing.T, err error, c codes.Code, text string) {
		t.Helper()
		if status.Code(err) != c {
			t.Fatalf("код %v (%v), ждали %v", status.Code(err), err, c)
		}
		if text != "" && status.Convert(err).Message() != text {
			t.Fatalf("текст %q, ждали %q", status.Convert(err).Message(), text)
		}
	}

	f := newFakeFeed()
	r := claimed(rowSpec{})
	f.lease(time.Now(), r)
	if err := ack(f, r, sent, 0); err != nil {
		t.Fatalf("первый Ack: %v", err)
	}
	if err := ack(f, r, sent, 0); err != nil {
		t.Fatalf("повтор той же пары — успех (З9 п.1), получено %v", err)
	}
	wantCode(t, ack(f, r, invalid, 0), codes.FailedPrecondition, reasonOutcomeRecorded)
	if outcomeString(f.Recorded(r.GetId())) != "SENT(OUTCOME_REASON_UNSPECIFIED)" {
		t.Fatalf("записан %s", outcomeString(f.Recorded(r.GetId())))
	}

	other := claimed(rowSpec{})
	f.lease(time.Now(), other)
	bad := &notifyv1.AckRequest{Id: other.GetId(), LeaseToken: uuid.NewString(), Outcome: sent}
	_, err := f.Ack(ctx, bad)
	wantCode(t, err, codes.FailedPrecondition, reasonLeaseLost)
	wantCode(t, ack(f, other, &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_DEFER, Reason: notifyv1.OutcomeReason_TEMPLATE_SKEW}, 0),
		codes.InvalidArgument, "defer_for: must be in [1s..15m]")
	wantCode(t, ack(f, other, sent, time.Minute), codes.InvalidArgument, "defer_for: only with DEFER")
	wantCode(t, ack(f, other, &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_EXPIRED, Reason: notifyv1.OutcomeReason_NO_ACK}, 0),
		codes.InvalidArgument, "")
	if err := ack(f, other, &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_DEFER, Reason: notifyv1.OutcomeReason_TEMPLATE_SKEW}, probeDeferFor); err != nil {
		t.Fatalf("DEFER с defer_for в границах: %v", err)
	}

	expired := claimed(rowSpec{lease: 50 * time.Millisecond})
	f.lease(time.Now(), expired)
	time.Sleep(80 * time.Millisecond)
	wantCode(t, ack(f, expired, sent, 0), codes.FailedPrecondition, reasonLeaseLost)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	late := claimed(rowSpec{})
	f.lease(time.Now(), late)
	_, err = f.Ack(cancelled, &notifyv1.AckRequest{Id: late.GetId(), LeaseToken: late.GetLeaseToken(), Outcome: sent})
	wantCode(t, err, codes.Canceled, "")

	slow := claimed(rowSpec{})
	f.lease(time.Now(), slow)
	f.ackDelay[slow.GetId()] = func() time.Time { return time.Now().Add(300 * time.Millisecond) }
	short, cancelShort := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelShort()
	_, err = f.Ack(short, &notifyv1.AckRequest{Id: slow.GetId(), LeaseToken: slow.GetLeaseToken(), Outcome: sent})
	wantCode(t, err, codes.DeadlineExceeded, "")
	if f.Recorded(slow.GetId()) != nil {
		t.Fatal("Ack, оборванный сроком контекста, записал исход")
	}
	if calls := f.Calls(slow.GetId()); len(calls) != 1 || calls[0].deadline.IsZero() {
		t.Fatalf("вызовы медленного Ack: %d, срок записан: %v", len(calls), len(calls) == 1 && !calls[0].deadline.IsZero())
	}

	ff := newFakeFeed()
	ff.failFirst = codes.Unavailable
	once := claimed(rowSpec{})
	ff.lease(time.Now(), once)
	wantCode(t, ack(ff, once, sent, 0), codes.Unavailable, "")
	if err := ack(ff, once, sent, 0); err != nil {
		t.Fatalf("второй Ack после UNAVAILABLE: %v", err)
	}
	if len(ff.Calls(once.GetId())) != 2 {
		t.Fatalf("вызовов %d, ждали 2", len(ff.Calls(once.GetId())))
	}
}

// TestFixtureClockSeesOnlyStrippedArithmeticJump — часы пробы: монотонная
// арифметика не видит скачка стенных часов, арифметика над моментом без
// монотонного показания — видит (форма класса УК80).
func TestFixtureClockSeesOnlyStrippedArithmeticJump(t *testing.T) {
	c := &testClock{wallJump: -time.Hour}
	t0 := c.Now()
	if !hasMonotonic(t0) || hasMonotonic(t0.UTC()) || hasMonotonic(t0.Round(0)) {
		t.Fatal("признак монотонного показания не различает момент и снятый момент")
	}
	if d := c.Since(t0); d < 0 || d > time.Second {
		t.Fatalf("Since по монотонным часам %v — скачок стенных часов просочился", d)
	}
	if d := c.Since(t0.UTC()); d > -59*time.Minute {
		t.Fatalf("Since по снятому моменту %v — скачок −1h не виден", d)
	}
	c.advance = 3 * time.Second
	if d := c.Since(t0); d < 3*time.Second || d > 4*time.Second {
		t.Fatalf("сдвиг монотонных часов +3s: Since %v", d)
	}
	if u := c.Until(t0.Add(10 * time.Second)); u < 6*time.Second || u > 7*time.Second {
		t.Fatalf("Until %v, ждали ~7s", u)
	}
}

// TestFixtureRelayCountsSessionsAndRecordsTheDeadline — настоящий отправитель
// против узла: сессия и письмо считаются; срок контекста записан; ответ на
// DATA позже срока — письмо не принято клиентом.
func TestFixtureRelayCountsSessionsAndRecordsTheDeadline(t *testing.T) {
	relay, snd := relayWith(t, smtptest.Script{}, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a := snd.Send(ctx, smtp.Envelope{From: fixtureFrom, To: fixtureTo}, []byte(fixtureMessage))
	if smtp.Classify(a).Outcome.Kind != feed.KindSent {
		t.Fatalf("исправный узел: попытка %+v", a)
	}
	if len(relay.Sessions()) != 1 || len(relay.Messages()) != 1 {
		t.Fatalf("сессий %d, писем %d — ждали 1 и 1", len(relay.Sessions()), len(relay.Messages()))
	}
	d, _ := ctx.Deadline()
	if got := snd.Deadlines(); len(got) != 1 || !got[0].Equal(d) {
		t.Fatalf("записан срок %v, ждали %v", got, d)
	}

	slowRelay, slowSnd := relayWith(t, smtptest.Script{Data: func() smtptest.Reply {
		time.Sleep(700 * time.Millisecond)
		return smtptest.OK
	}}, 30*time.Second)
	sctx, scancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer scancel()
	sa := slowSnd.Send(sctx, smtp.Envelope{From: fixtureFrom, To: fixtureTo}, []byte(fixtureMessage))
	if sa.Stage == smtp.StageDone {
		t.Fatalf("ответ на DATA позже срока принят клиентом: %+v", sa)
	}
	if len(slowRelay.Sessions()) != 1 {
		t.Fatalf("сессий %d, ждали 1", len(slowRelay.Sessions()))
	}
}

// TestFixturePeerAndResolver — настоящий путь ResolveSend на поддельном
// порту: вызов считается; исключение kaname вызова не делает.
func TestFixturePeerAndResolver(t *testing.T) {
	reg := prometheus.NewRegistry()
	peer := &fakePeer{decision: grant.DecisionAllow}
	r := newResolver(t, peer, reg, sourceOf(nsProbe, config.RecipientAddress), sourceOf(nsKaname, config.RecipientAddress))
	g, err := r.For(nsProbe)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Decide(context.Background(), "probe-bhello", time.Now()).Allowed() || peer.Calls() != 1 {
		t.Fatalf("probe: ALLOW не пропущен либо вызовов %d", peer.Calls())
	}
	peer.decision = grant.DecisionRevoked
	if o, _ := g.Decide(context.Background(), "probe-bhello", time.Now()).Outcome(); o.Kind != feed.KindDenied || o.Reason != feed.ReasonRevoked {
		t.Fatalf("REVOKED: исход %+v", o)
	}
	k, err := r.For(nsKaname)
	if err != nil {
		t.Fatal(err)
	}
	if !k.Decide(context.Background(), "probe-opt", time.Now()).Allowed() || peer.Calls() != 2 {
		t.Fatalf("kaname: исключение certificate не пропустило строку либо позвало kaname (вызовов %d)", peer.Calls())
	}
}

// TestFixtureLimiterExhaustionIsTheProductType — исчерпание сетки фикстуры
// различимо тем же errors.Is, что исчерпание limits.Limiter.
func TestFixtureLimiterExhaustionIsTheProductType(t *testing.T) {
	l := &fakeLimiter{exhausted: true}
	_, err := l.Reserve(context.Background(), limits.Row{Source: nsProbe, Class: feed.ClassNotice})
	var ex *limits.ExhaustedError
	if !errors.Is(err, limits.ErrRecipientNetExhausted) || !errors.As(err, &ex) || ex.Class != feed.ClassNotice {
		t.Fatalf("исчерпание фикстуры не той формы: %v", err)
	}
	if errors.Is(err, limits.ErrRecipientKeySuperseded) {
		t.Fatal("исчерпание неотличимо от сторожа ограды")
	}
	ok := &fakeLimiter{}
	res, err := ok.Reserve(context.Background(), limits.Row{Source: nsProbe, Class: feed.ClassSecurity})
	if err != nil || res == nil || len(ok.Reserves()) != 1 || ok.Reserves()[0].Class != feed.ClassSecurity {
		t.Fatalf("резерв: %v, записано %d", err, len(ok.Reserves()))
	}
	if err := ok.Release(context.Background(), res); err != nil {
		t.Fatal(err)
	}
}

// TestFixtureCounterReader — читатель счётчика отличает «серии нет» от нуля.
func TestFixtureCounterReader(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "n3_fixture_total", Help: "fixture"}, []string{"source"})
	reg.MustRegister(c)
	c.WithLabelValues(nsProbe).Add(2)
	if v, ok := counterValue(t, reg, "n3_fixture_total", map[string]string{"source": nsProbe}); !ok || v != 2 {
		t.Fatalf("значение %v, серия %v", v, ok)
	}
	if _, ok := counterValue(t, reg, "n3_fixture_total", map[string]string{"source": nsKaname}); ok {
		t.Fatal("несуществующая серия прочитана как есть")
	}
}
