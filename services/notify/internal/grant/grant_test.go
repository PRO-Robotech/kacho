// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package grant_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
	"github.com/PRO-Robotech/kacho/services/notify/internal/peeranswer"
)

// answer — ответ фикстурного kaname на одну пару «пространство/шаблон».
type answer struct {
	decision grant.Decision
	err      error
	// block — ждать конца контекста вызова и вернуть его ошибку (kaname не
	// отвечает в срок).
	block bool
}

// call — вызов, который видел фикстурный kaname.
type call struct {
	q           grant.Query
	hasDeadline bool
	deadlineIn  time.Duration
}

// peer — фикстурный kaname: отвечает по пробе, записывает каждый вызов.
type peer struct {
	mu      sync.Mutex
	answers map[string]answer // ключ — "<namespace>/<template>"
	calls   []call
}

func newPeer() *peer { return &peer{answers: map[string]answer{}} }

func (p *peer) set(ns, tmpl string, a answer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answers[ns+"/"+tmpl] = a
}

func (p *peer) ResolveSend(ctx context.Context, q grant.Query) (grant.Decision, error) {
	dl, ok := ctx.Deadline()
	p.mu.Lock()
	p.calls = append(p.calls, call{q: q, hasDeadline: ok, deadlineIn: time.Until(dl)})
	a, found := p.answers[q.Namespace+"/"+q.Template]
	p.mu.Unlock()
	if !found {
		// Проба, не задавшая ответа, — дефект пробы: громко, не «разрешено».
		return grant.DecisionUnset, status.Error(codes.Internal, "фикстура: ответ не задан для "+q.Namespace+"/"+q.Template)
	}
	if a.block {
		<-ctx.Done()
		return grant.DecisionUnset, status.FromContextError(ctx.Err()).Err()
	}
	return a.decision, a.err
}

func (p *peer) callsFor(ns string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		if c.q.Namespace == ns {
			n++
		}
	}
	return n
}

func (p *peer) last(t *testing.T) call {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.calls) == 0 {
		t.Fatal("kaname не спрошена ни разу")
	}
	return p.calls[len(p.calls)-1]
}

func source(module string, auth config.Authorization) config.Source {
	return config.Source{
		Module:         module,
		FeedAddr:       module + ":9091",
		SAN:            "spiffe://kacho.cloud/ns/kacho/sa/kacho-" + module,
		Classes:        []feed.Class{feed.ClassNotice},
		RecipientForms: []config.RecipientForm{config.RecipientAddress},
		Authorization:  auth,
	}
}

var policy = grant.Policy{CallTimeout: 2 * time.Second, DeferFor: 30 * time.Second}

type rig struct {
	peer *peer
	reg  *prometheus.Registry
	res  *grant.Resolver
}

func newRig(t *testing.T, p grant.Policy, sources ...config.Source) rig {
	t.Helper()
	if len(sources) == 0 {
		sources = []config.Source{source("probe", config.AuthorizationResolveSend)}
	}
	reg := prometheus.NewPedanticRegistry()
	sig, err := peeranswer.NewSignals(reg)
	if err != nil {
		t.Fatalf("NewSignals: %v", err)
	}
	pr := newPeer()
	res, err := grant.New(pr, sources, p, reg, sig)
	if err != nil {
		t.Fatalf("grant.New: %v", err)
	}
	return rig{peer: pr, reg: reg, res: res}
}

func (r rig) gate(t *testing.T, module string) *grant.Gate {
	t.Helper()
	g, err := r.res.For(module)
	if err != nil {
		t.Fatalf("For(%q): %v", module, err)
	}
	return g
}

// series — значения серий семейства name; ключ — метки «имя=значение»,
// склеенные «|» в порядке имён.
func series(t *testing.T, reg prometheus.Gatherer, name string) map[string]float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("сбор метрик: %v", err)
	}
	out := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			parts := make([]string, 0, len(m.GetLabel()))
			for _, l := range m.GetLabel() {
				parts = append(parts, l.GetName()+"="+l.GetValue())
			}
			out[strings.Join(parts, "|")] = m.GetCounter().GetValue()
		}
	}
	return out
}

func misconfigured(t *testing.T, reg prometheus.Gatherer, src string) float64 {
	t.Helper()
	sum := 0.0
	for k, v := range series(t, reg, "notify_misconfigured_total") {
		if strings.Contains(k, "source="+src) {
			sum += v
		}
	}
	return sum
}

func resolveSendTotal(t *testing.T, reg prometheus.Gatherer, src, code, outcome string) float64 {
	t.Helper()
	return series(t, reg, "notify_resolve_send_total")["code="+code+"|outcome="+outcome+"|source="+src]
}

func requireAllowed(t *testing.T, v grant.Verdict) {
	t.Helper()
	if !v.Allowed() {
		o, d := v.Outcome()
		t.Fatalf("ждали «отправлять», получили исход %v, defer_for %v", o, d)
	}
	if o, d := v.Outcome(); o != (feed.Outcome{}) || d != 0 {
		t.Fatalf("разрешённая строка несёт исход %v, defer_for %v", o, d)
	}
}

func requireOutcome(t *testing.T, v grant.Verdict, want feed.Outcome, deferFor time.Duration) {
	t.Helper()
	if v.Allowed() {
		t.Fatalf("ждали исход %v, получили «отправлять»", want)
	}
	o, d := v.Outcome()
	if o != want || d != deferFor {
		t.Fatalf("исход (%v, defer_for %v), ждали (%v, defer_for %v)", o, d, want, deferFor)
	}
}

var (
	deferPlatform  = feed.Outcome{Kind: feed.KindDefer, Reason: feed.ReasonPlatformUnavailable}
	deferGrantSkew = feed.Outcome{Kind: feed.KindDefer, Reason: feed.ReasonGrantSkew}
	deniedRevoked  = feed.Outcome{Kind: feed.KindDenied, Reason: feed.ReasonRevoked}
)

// NTF1-F01 (сторона notify) — ALLOW: строку отправлять; вопрос несёт
// пространство источника, имя шаблона и отметку постановки дословно.
func TestNTF1F01AllowSends(t *testing.T) {
	r := newRig(t, policy)
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionAllow})
	t0 := time.Date(2026, 10, 4, 12, 0, 0, 123456789, time.UTC)

	requireAllowed(t, r.gate(t, "probe").Decide(context.Background(), "probe-hello", t0))

	c := r.peer.last(t)
	if c.q.Namespace != "probe" || c.q.Template != "probe-hello" || !c.q.EnqueuedAt.Equal(t0) {
		t.Fatalf("вопрос kaname: %+v, ждали (probe, probe-hello, %v)", c.q, t0)
	}
	if got := resolveSendTotal(t, r.reg, "probe", "OK", "allow"); got != 1 {
		t.Fatalf("notify_resolve_send_total{probe, OK, allow} = %v, ждали 1", got)
	}
	if m := misconfigured(t, r.reg, "probe"); m != 0 {
		t.Fatalf("misconfigured = %v на ALLOW, ждали 0", m)
	}
}

// NTF1-F04 (сторона notify) — NOT_YET_GRANTED: DEFER(grant_skew) на отсрочку
// политики; затем выдача появилась — тот же вопрос ALLOW.
func TestNTF1F04NotYetGrantedDefersGrantSkew(t *testing.T) {
	r := newRig(t, policy)
	g := r.gate(t, "probe")
	t0 := time.Now()
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionNotYetGranted})
	requireOutcome(t, g.Decide(context.Background(), "probe-hello", t0), deferGrantSkew, policy.DeferFor)
	if got := resolveSendTotal(t, r.reg, "probe", "OK", "not_yet_granted"); got != 1 {
		t.Fatalf("notify_resolve_send_total{probe, OK, not_yet_granted} = %v, ждали 1", got)
	}
	if m := misconfigured(t, r.reg, "probe"); m != 0 {
		t.Fatalf("misconfigured = %v на NOT_YET_GRANTED — это «права ещё нет», а не неисправность", m)
	}

	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionAllow})
	requireAllowed(t, g.Decide(context.Background(), "probe-hello", t0))
}

// NTF1-F05, F06 (сторона notify) — REVOKED: терминальный DENIED(revoked),
// без отсрочки.
func TestNTF1F05F06RevokedIsDeniedRevoked(t *testing.T) {
	r := newRig(t, policy)
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionRevoked})
	requireOutcome(t, r.gate(t, "probe").Decide(context.Background(), "probe-hello", time.Now()), deniedRevoked, 0)
	if got := resolveSendTotal(t, r.reg, "probe", "OK", "revoked"); got != 1 {
		t.Fatalf("notify_resolve_send_total{probe, OK, revoked} = %v, ждали 1", got)
	}
}

// NTF1-F07 (сторона notify) — отзыв одного шаблона не трогает соседний:
// решение — по шаблону строки, а не по пространству.
func TestNTF1F07RevokedTemplateDoesNotTouchItsNeighbour(t *testing.T) {
	r := newRig(t, policy)
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionRevoked})
	r.peer.set("probe", "probe-bye", answer{decision: grant.DecisionAllow})
	g := r.gate(t, "probe")
	requireOutcome(t, g.Decide(context.Background(), "probe-hello", time.Now()), deniedRevoked, 0)
	requireAllowed(t, g.Decide(context.Background(), "probe-bye", time.Now()))
}

// NTF1-F11, F23 (сторона notify) — kaname недоступна (UNAVAILABLE с
// фиксированным текстом kaname, нет ответа в срок): DEFER(platform_unavailable),
// сигнала misconfigured нет, grant_skew не выставлен.
func TestNTF1F11F23UnavailableDefersPlatformUnavailable(t *testing.T) {
	for _, c := range []struct {
		name string
		a    answer
		code string
	}{
		{"F23: UNAVAILABLE сбоя чтения выдачи", answer{err: status.Error(codes.Unavailable, "notification grant service temporarily unavailable")}, "UNAVAILABLE"},
		{"F11: kaname не ответила в срок вызова", answer{block: true}, "DEADLINE_EXCEEDED"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, grant.Policy{CallTimeout: 100 * time.Millisecond, DeferFor: policy.DeferFor})
			r.peer.set("probe", "probe-hello", c.a)
			requireOutcome(t, r.gate(t, "probe").Decide(context.Background(), "probe-hello", time.Now()), deferPlatform, policy.DeferFor)
			if got := resolveSendTotal(t, r.reg, "probe", c.code, "unavailable"); got != 1 {
				t.Fatalf("notify_resolve_send_total{probe, %s, unavailable} = %v, ждали 1", c.code, got)
			}
			if m := misconfigured(t, r.reg, "probe"); m != 0 {
				t.Fatalf("misconfigured = %v на недоступности, ждали 0", m)
			}
			if got := resolveSendTotal(t, r.reg, "probe", "OK", "not_yet_granted"); got != 0 {
				t.Fatalf("недоступность прочитана как «права ещё нет»: %v", got)
			}
		})
	}
}

// NTF1-G23 (сторона notify) — отказ ResolveSend: каждый — DEFER
// (platform_unavailable) и misconfigured +1; grant_skew не выставлен; затем
// ALLOW — отправлять. Близнец F01 — ALLOW сразу, misconfigured нулевой.
func TestNTF1G23RefusalDefersAndSignalsMisconfigured(t *testing.T) {
	r := newRig(t, policy)
	g := r.gate(t, "probe")
	cases := []struct {
		name    string
		a       answer
		code    string
		outcome string
	}{
		{"(а) PERMISSION_DENIED", answer{err: status.Error(codes.PermissionDenied, "permission denied")}, "PERMISSION_DENIED", "refused"},
		{"(б) INVALID_ARGUMENT", answer{err: status.Error(codes.InvalidArgument, "enqueued_at: required")}, "INVALID_ARGUMENT", "refused"},
		{"(в) INTERNAL", answer{err: status.Error(codes.Internal, "internal error")}, "INTERNAL", "refused"},
		{"(г) исход, которого notify не знает", answer{decision: grant.Decision(99)}, "OK", "protocol"},
		{"(г') вариант ответа не задан", answer{decision: grant.DecisionUnset}, "OK", "protocol"},
	}
	for i, c := range cases {
		r.peer.set("probe", "probe-hello", c.a)
		requireOutcome(t, g.Decide(context.Background(), "probe-hello", time.Now()), deferPlatform, policy.DeferFor)
		if m := misconfigured(t, r.reg, "probe"); m != float64(i+1) {
			t.Fatalf("%s: misconfigured = %v, ждали %d", c.name, m, i+1)
		}
		if got := resolveSendTotal(t, r.reg, "probe", c.code, c.outcome); got < 1 {
			t.Fatalf("%s: notify_resolve_send_total{probe, %s, %s} = %v, ждали ≥ 1", c.name, c.code, c.outcome, got)
		}
	}
	if got := resolveSendTotal(t, r.reg, "probe", "OK", "not_yet_granted"); got != 0 {
		t.Fatalf("отказ прочитан как «права ещё нет» (grant_skew): %v", got)
	}
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionAllow})
	requireAllowed(t, g.Decide(context.Background(), "probe-hello", time.Now()))

	t.Run("близнец F01: ALLOW сразу", func(t *testing.T) {
		twin := newRig(t, policy)
		twin.peer.set("probe", "probe-hello", answer{decision: grant.DecisionAllow})
		requireAllowed(t, twin.gate(t, "probe").Decide(context.Background(), "probe-hello", time.Now()))
		if m := misconfigured(t, twin.reg, "probe"); m != 0 {
			t.Fatalf("misconfigured близнеца = %v, ждали 0", m)
		}
	})
}

// Метрика несёт код: RESOURCE_EXHAUSTED отличим от PERMISSION_DENIED без
// чтения журналов (CX1-28 (в)).
func TestResolveSendMetricCarriesTheCode(t *testing.T) {
	r := newRig(t, policy)
	g := r.gate(t, "probe")
	r.peer.set("probe", "probe-hello", answer{err: status.Error(codes.ResourceExhausted, "x")})
	g.Decide(context.Background(), "probe-hello", time.Now())
	r.peer.set("probe", "probe-hello", answer{err: status.Error(codes.PermissionDenied, "x")})
	g.Decide(context.Background(), "probe-hello", time.Now())
	if a, b := resolveSendTotal(t, r.reg, "probe", "RESOURCE_EXHAUSTED", "refused"),
		resolveSendTotal(t, r.reg, "probe", "PERMISSION_DENIED", "refused"); a != 1 || b != 1 {
		t.Fatalf("RESOURCE_EXHAUSTED = %v, PERMISSION_DENIED = %v, ждали по 1", a, b)
	}
}

// Серии законных клеток заведены нулём до первого вызова: «ноль за всю
// жизнь» отличим от «серии нет».
func TestResolveSendSeriesAreDeclaredUpFront(t *testing.T) {
	r := newRig(t, policy, source("probe", config.AuthorizationResolveSend))
	got := series(t, r.reg, "notify_resolve_send_total")
	for _, k := range []string{
		"code=OK|outcome=allow|source=probe",
		"code=OK|outcome=not_yet_granted|source=probe",
		"code=OK|outcome=revoked|source=probe",
		"code=OK|outcome=protocol|source=probe",
		"code=UNAVAILABLE|outcome=unavailable|source=probe",
		"code=DEADLINE_EXCEEDED|outcome=unavailable|source=probe",
		"code=PERMISSION_DENIED|outcome=refused|source=probe",
		"code=RESOURCE_EXHAUSTED|outcome=refused|source=probe",
	} {
		if v, ok := got[k]; !ok || v != 0 {
			t.Errorf("серия %s: %v (есть: %v), ждали заведённый 0", k, v, ok)
		}
	}
	if m := series(t, r.reg, "notify_misconfigured_total"); len(m) == 0 {
		t.Error("серии misconfigured источника не заведены")
	}
}

// Кэша ответа нет (CX1-06 (б)): каждый вопрос — вызов kaname, смена ответа
// видна следующей строкой.
func TestNoAnswerCache(t *testing.T) {
	r := newRig(t, policy)
	g := r.gate(t, "probe")
	t0 := time.Now()
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionAllow})
	requireAllowed(t, g.Decide(context.Background(), "probe-hello", t0))
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionRevoked})
	requireOutcome(t, g.Decide(context.Background(), "probe-hello", t0), deniedRevoked, 0)
	if n := r.peer.callsFor("probe"); n != 2 {
		t.Fatalf("вызовов kaname: %d, ждали 2", n)
	}
}

// Свой срок на каждом вызове (arch-per-call-deadline): родитель без срока —
// вызов всё равно под CallTimeout.
func TestEveryCallCarriesItsOwnDeadline(t *testing.T) {
	r := newRig(t, policy)
	r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionAllow})
	r.gate(t, "probe").Decide(context.Background(), "probe-hello", time.Now())
	c := r.peer.last(t)
	if !c.hasDeadline || c.deadlineIn <= 0 || c.deadlineIn > policy.CallTimeout {
		t.Fatalf("срок вызова: есть %v, остаток %v, ждали (0..%v]", c.hasDeadline, c.deadlineIn, policy.CallTimeout)
	}
}

// УК14, УК28 — темп ResolveSend по строке в NOT_YET_GRANTED: источник не
// выдаёт строку до not_before = now + defer_for, поэтому за окно T вызовов
// не больше T/deferFor + 1. Время — управляемое: шаг Claim — 1 с.
func TestResolveSendPaceIsBoundedByDeferFor(t *testing.T) {
	for _, deferFor := range []time.Duration{feed.MinDefer, 30 * time.Second, feed.MaxDefer} {
		r := newRig(t, grant.Policy{CallTimeout: policy.CallTimeout, DeferFor: deferFor})
		r.peer.set("probe", "probe-hello", answer{decision: grant.DecisionNotYetGranted})
		g := r.gate(t, "probe")
		const window = time.Hour
		var notBefore time.Duration
		for now := time.Duration(0); now <= window; now += time.Second {
			if now < notBefore {
				continue // Claim такую строку не выдаёт (З8)
			}
			o, d := g.Decide(context.Background(), "probe-hello", time.Now()).Outcome()
			if o != deferGrantSkew {
				t.Fatalf("deferFor %v: исход %v, ждали %v", deferFor, o, deferGrantSkew)
			}
			notBefore = now + d
		}
		limit := int(window/deferFor) + 1
		if n := r.peer.callsFor("probe"); n > limit {
			t.Fatalf("deferFor %v: вызовов ResolveSend за %v — %d, предел T/deferFor + 1 = %d", deferFor, window, n, limit)
		}
	}
}

// УК32, УК37 — пространство A отказывает, а строки kaname (исключение по
// сертификату) и соседнего пространства B идут: общего бюджета отказов на
// пути ResolveSend нет, исключение kaname kaname не спрашивает.
func TestRefusingNamespaceDoesNotStarveOthers(t *testing.T) {
	r := newRig(t, policy,
		source("probe-a", config.AuthorizationResolveSend),
		source("probe-b", config.AuthorizationResolveSend),
		source("kaname", config.AuthorizationCertificate),
	)
	r.peer.set("probe-a", "hello", answer{err: status.Error(codes.PermissionDenied, "permission denied")})
	r.peer.set("probe-b", "hello", answer{decision: grant.DecisionAllow})
	a, b, k := r.gate(t, "probe-a"), r.gate(t, "probe-b"), r.gate(t, "kaname")

	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := a.Decide(context.Background(), "hello", time.Now())
			if o, d := v.Outcome(); v.Allowed() || o != deferPlatform || d != policy.DeferFor {
				t.Errorf("отказ A: (разрешено %v, %v, %v), ждали (%v, %v)", v.Allowed(), o, d, deferPlatform, policy.DeferFor)
			}
		}()
	}
	wg.Wait()

	requireAllowed(t, b.Decide(context.Background(), "hello", time.Now()))
	requireAllowed(t, k.Decide(context.Background(), "password-reset", time.Now()))
	if n := r.peer.callsFor("kaname"); n != 0 {
		t.Fatalf("строка kaname спросила ResolveSend %d раз, ждали 0 (NTF1-G22)", n)
	}
	if m := misconfigured(t, r.reg, "probe-a"); m != 200 {
		t.Fatalf("misconfigured probe-a = %v, ждали 200", m)
	}
	if m := misconfigured(t, r.reg, "probe-b") + misconfigured(t, r.reg, "kaname"); m != 0 {
		t.Fatalf("отказы A легли в чужие серии misconfigured: %v", m)
	}
}

// Строка источника с исключением по сертификату — без вызова kaname и без
// метрики вызова (NTF1-G22, сторона N4).
func TestCertificateSourceNeverAsksKaname(t *testing.T) {
	r := newRig(t, policy, source("kaname", config.AuthorizationCertificate))
	requireAllowed(t, r.gate(t, "kaname").Decide(context.Background(), "password-reset", time.Now()))
	if n := r.peer.callsFor("kaname"); n != 0 {
		t.Fatalf("вызовов ResolveSend от kaname: %d, ждали 0", n)
	}
	for k, v := range series(t, r.reg, "notify_resolve_send_total") {
		if strings.Contains(k, "source=kaname") && v != 0 {
			t.Fatalf("серия %s = %v, ждали 0", k, v)
		}
	}
}

// Конструктор отказывает на политике вне границ и на неполной сборке —
// ошибкой программы с именем предмета, а не подстановкой.
func TestNewRefusesOutOfBoundsPolicyAndBrokenWiring(t *testing.T) {
	ok := []config.Source{source("probe", config.AuthorizationResolveSend)}
	sig := func() (*prometheus.Registry, *peeranswer.Signals) {
		reg := prometheus.NewPedanticRegistry()
		s, err := peeranswer.NewSignals(reg)
		if err != nil {
			t.Fatalf("NewSignals: %v", err)
		}
		return reg, s
	}
	for _, c := range []struct {
		name    string
		peer    grant.Peer
		sources []config.Source
		p       grant.Policy
		nilReg  bool
		nilSig  bool
		want    string
	}{
		{name: "deferFor ниже 1 с", peer: newPeer(), sources: ok, p: grant.Policy{CallTimeout: time.Second, DeferFor: feed.MinDefer - time.Nanosecond}, want: "deferFor"},
		{name: "deferFor выше 15 мин", peer: newPeer(), sources: ok, p: grant.Policy{CallTimeout: time.Second, DeferFor: feed.MaxDefer + time.Nanosecond}, want: "deferFor"},
		{name: "deferFor не задан", peer: newPeer(), sources: ok, p: grant.Policy{CallTimeout: time.Second}, want: "deferFor"},
		{name: "срок вызова ниже границы", peer: newPeer(), sources: ok, p: grant.Policy{CallTimeout: config.ResolveSendTimeoutMin - time.Nanosecond, DeferFor: time.Minute}, want: "resolveSendTimeout"},
		{name: "срок вызова выше границы", peer: newPeer(), sources: ok, p: grant.Policy{CallTimeout: config.ResolveSendTimeoutMax + time.Nanosecond, DeferFor: time.Minute}, want: "resolveSendTimeout"},
		{name: "нет kaname", sources: ok, p: policy, want: "peer"},
		{name: "перечень пуст", peer: newPeer(), p: policy, want: "источник"},
		{name: "неизвестный вид права", peer: newPeer(), sources: []config.Source{source("probe", config.Authorization("trust-me"))}, p: policy, want: "trust-me"},
		{name: "модуль повторён", peer: newPeer(), sources: append(ok, ok[0]), p: policy, want: "probe"},
		{name: "нет реестра", peer: newPeer(), sources: ok, p: policy, nilReg: true, want: "реестр"},
		{name: "нет сигналов", peer: newPeer(), sources: ok, p: policy, nilSig: true, want: "сигнал"},
	} {
		t.Run(c.name, func(t *testing.T) {
			reg, s := sig()
			var registerer prometheus.Registerer = reg
			if c.nilReg {
				registerer = nil
			}
			if c.nilSig {
				s = nil
			}
			_, err := grant.New(c.peer, c.sources, c.p, registerer, s)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("grant.New: %v, ждали отказ с %q", err, c.want)
			}
		})
	}
	t.Run("законная политика на границах", func(t *testing.T) {
		for _, p := range []grant.Policy{
			{CallTimeout: config.ResolveSendTimeoutMin, DeferFor: feed.MinDefer},
			{CallTimeout: config.ResolveSendTimeoutMax, DeferFor: feed.MaxDefer},
		} {
			reg, s := sig()
			if _, err := grant.New(newPeer(), ok, p, reg, s); err != nil {
				t.Fatalf("grant.New(%+v): %v", p, err)
			}
		}
	})
}

// Источник вне перечня — ошибка сборки цикла, а не тихое «разрешено».
func TestForUnknownModuleIsAnError(t *testing.T) {
	r := newRig(t, policy)
	if _, err := r.res.For("vpc"); err == nil || !strings.Contains(err.Error(), "vpc") {
		t.Fatalf("For(vpc): %v, ждали отказ с именем модуля", err)
	}
}
