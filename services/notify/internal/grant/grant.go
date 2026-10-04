// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package grant — право источника на письмо: ответ kaname
// `InternalNotificationGrantService/ResolveSend` и его следствие для строки
// ленты (З23 замысла NTF-1; клетка 7 `deliver.Decide`, З22).
//
// Что здесь решено и почему:
//
//   - Ответ классифицирует [peeranswer.Classify] типом, без «прочего»: три
//     исхода kaname, «недоступен», «отказ». Исход вне трёх и незаданный
//     вариант — «отказ» ([peeranswer.Malformed]), а не решение о письме.
//   - «Недоступен» и «отказ» — DEFER(platform_unavailable): строка ждёт в
//     пределах своего срока, попытка не тратится. «Отказ» сверх того —
//     сигнал misconfigured: это неисправность настройки, а не «права ещё нет»,
//     и тревога grant_skew на него не срабатывает (NTF1-G23).
//   - Отсрочка одна — [Policy.DeferFor] (ручка `notify.deferFor`, [1s..15m]
//     без умолчания): источник не выдаёт строку до `now() + defer_for`, и
//     вызовов ResolveSend по строке в NOT_YET_GRANTED за окно T не больше
//     T/deferFor + 1 (CX1-14, УК14).
//   - Кэша ответа нет — вопрос на каждое письмо (CX1-06 (б)): отзыв действует
//     со следующей строки, а не по истечении кэша.
//   - Бюджета отказов на этом пути нет ни здесь, ни на внутреннем слушателе
//     kaname (М9, CX1-28 (б)): отказы одного пространства не отнимают ответы
//     у другого. Правка, которая бюджет добавит, обязана назвать корзину
//     «принципал × пространство».
//   - Исключение `kaname` (приёмка Р3, NTF1-G22): источник с
//     `authorization: certificate` подтверждает право точным SAN сервера ленты
//     при подключении (N2), и ResolveSend по его строкам не зовётся.
//   - Каждый вызов — под своим сроком [Policy.CallTimeout] поверх контекста
//     строки (`arch-per-call-deadline`); срок строки (З21) остаётся старшим.
package grant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/peeranswer"
)

// Decision — решение kaname в ответе ResolveSend. Нулевое значение — «решения
// в ответе нет»: переходник контракта отдаёт его и на незаданный вариант, и на
// значение, которого notify не знает (расхождение версий контракта).
type Decision uint8

// Решения kaname.
const (
	// DecisionUnset — решения в ответе нет.
	DecisionUnset Decision = iota
	// DecisionAllow — ALLOW: отправлять.
	DecisionAllow
	// DecisionNotYetGranted — NOT_YET_GRANTED: записи выдачи нет.
	DecisionNotYetGranted
	// DecisionRevoked — REVOKED: надгробие либо отсечка.
	DecisionRevoked
)

// Query — вопрос ResolveSend: пространство (модуль источника), имя шаблона
// без пространства и отметка постановки строки в полной точности ленты.
type Query struct {
	Namespace  string
	Template   string
	EnqueuedAt time.Time
}

// Peer — порт kaname: один вызов ResolveSend. Ошибка — статус gRPC вызова;
// решение читается только при nil-ошибке.
type Peer interface {
	ResolveSend(ctx context.Context, q Query) (Decision, error)
}

// Policy — сроки пути ResolveSend.
type Policy struct {
	// CallTimeout — срок одного вызова (ручка `notify.resolveSendTimeout`).
	CallTimeout time.Duration
	// DeferFor — отсрочка строки после grant_skew и platform_unavailable
	// (ручка `notify.deferFor`), в [feed.MinDefer..feed.MaxDefer].
	DeferFor time.Duration
}

// deferForKnob — имя ручки отсрочки в тексте отказа.
const deferForKnob = "notify.deferFor"

func (p Policy) validate() error {
	var errs []error
	if p.DeferFor < feed.MinDefer || p.DeferFor > feed.MaxDefer {
		errs = append(errs, fmt.Errorf("%s = %v вне [%v..%v]", deferForKnob, p.DeferFor, feed.MinDefer, feed.MaxDefer))
	}
	if p.CallTimeout < config.ResolveSendTimeoutMin || p.CallTimeout > config.ResolveSendTimeoutMax {
		errs = append(errs, fmt.Errorf("%s = %v вне [%v..%v]", config.KnobOfField("ResolveSendTimeout").Name,
			p.CallTimeout, config.ResolveSendTimeoutMin, config.ResolveSendTimeoutMax))
	}
	return errors.Join(errs...)
}

// Verdict — следствие ответа для строки: «отправлять» либо исход Ack с
// отсрочкой. Отсрочка неотделима от исхода DEFER: её несёт тот же Verdict.
type Verdict struct {
	allowed  bool
	outcome  feed.Outcome
	deferFor time.Duration
}

// Allowed — строку можно вести дальше по клеткам (сетка, рендер, SMTP).
func (v Verdict) Allowed() bool { return v.allowed }

// Outcome — исход Ack и defer_for (не нуль только у DEFER). У разрешённой
// строки — нулевой исход.
func (v Verdict) Outcome() (feed.Outcome, time.Duration) { return v.outcome, v.deferFor }

// Значения метки outcome у notify_resolve_send_total — закрытый перечень.
const (
	outcomeAllow         = "allow"
	outcomeNotYetGranted = "not_yet_granted"
	outcomeRevoked       = "revoked"
	outcomeProtocol      = "protocol"
	outcomeUnavailable   = "unavailable"
	outcomeRefused       = "refused"
)

// Resolver — путь ResolveSend процесса: порт kaname, политика, метрики.
// Состояния между вызовами не держит: параллельные строки не делят ничего,
// кроме счётчиков.
type Resolver struct {
	peer    Peer
	policy  Policy
	calls   *prometheus.CounterVec
	signals *peeranswer.Signals
	gates   map[string]*Gate
}

// New собирает путь ResolveSend для перечня источников. Отказ — ошибка
// программы с именем предмета: политика вне границ, нет порта, реестра или
// сигналов, пустой перечень, повтор модуля, вид права вне перечня.
func New(peer Peer, sources []config.Source, p Policy, reg prometheus.Registerer, sig *peeranswer.Signals) (*Resolver, error) {
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("grant: политика: %w", err)
	}
	switch {
	case peer == nil:
		return nil, errors.New("grant: порт kaname (peer) не задан")
	case reg == nil:
		return nil, errors.New("grant: реестр метрик не задан")
	case sig == nil:
		return nil, errors.New("grant: сигналы misconfigured не заданы")
	case len(sources) == 0:
		return nil, errors.New("grant: перечень источников пуст")
	}
	r := &Resolver{peer: peer, policy: p, signals: sig, gates: make(map[string]*Gate, len(sources))}
	for _, s := range sources {
		if _, dup := r.gates[s.Module]; dup {
			return nil, fmt.Errorf("grant: модуль %q назван в перечне дважды", s.Module)
		}
		g := &Gate{r: r, module: s.Module}
		switch s.Authorization {
		case config.AuthorizationResolveSend:
		case config.AuthorizationCertificate:
			g.certificate = true
		default:
			return nil, fmt.Errorf("grant: модуль %q: вид права %q вне перечня %v", s.Module, s.Authorization, config.Authorizations())
		}
		r.gates[s.Module] = g
	}
	r.calls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "notify_resolve_send_total",
		Help: "Вызовы ResolveSend по источнику: код ответа gRPC и исход (решение kaname либо род ответа).",
	}, []string{"source", "code", "outcome"})
	if err := reg.Register(r.calls); err != nil {
		return nil, fmt.Errorf("grant: регистрация notify_resolve_send_total: %w", err)
	}
	for _, s := range sources {
		r.declare(s.Module)
		sig.Declare(s.Module)
	}
	return r, nil
}

// declare заводит нулём законные клетки источника: OK с решением либо
// «protocol», каждый иной код — со своим родом. «Ноль за всю жизнь» отличим
// от «серии нет».
func (r *Resolver) declare(module string) {
	ok := peeranswer.CodeLabel(codes.OK)
	for _, o := range []string{outcomeAllow, outcomeNotYetGranted, outcomeRevoked, outcomeProtocol} {
		r.calls.WithLabelValues(module, ok, o)
	}
	for _, c := range peeranswer.Codes() {
		switch peeranswer.GenusOf(c) {
		case peeranswer.Answered:
			// OK — заведён выше по решениям.
		case peeranswer.Unavailable:
			r.calls.WithLabelValues(module, peeranswer.CodeLabel(c), outcomeUnavailable)
		case peeranswer.Refused:
			r.calls.WithLabelValues(module, peeranswer.CodeLabel(c), outcomeRefused)
		}
	}
}

// For — путь ResolveSend источника. Модуль вне перечня — ошибка сборки
// цикла источника, а не тихое «разрешено».
func (r *Resolver) For(module string) (*Gate, error) {
	g, ok := r.gates[module]
	if !ok {
		return nil, fmt.Errorf("grant: модуль %q вне перечня источников", module)
	}
	return g, nil
}

// Gate — путь ResolveSend одного источника.
type Gate struct {
	r      *Resolver
	module string
	// certificate — исключение `kaname`: право подтверждено SAN сервера ленты.
	certificate bool
}

// Decide — следствие права для строки шаблона template, поставленной в
// enqueuedAt. Вызов kaname — под своим сроком поверх ctx строки.
func (g *Gate) Decide(ctx context.Context, template string, enqueuedAt time.Time) Verdict {
	if g.certificate {
		return Verdict{allowed: true}
	}
	callCtx, cancel := context.WithTimeout(ctx, g.r.policy.CallTimeout)
	defer cancel()
	d, err := g.r.peer.ResolveSend(callCtx, Query{Namespace: g.module, Template: template, EnqueuedAt: enqueuedAt})
	a := peeranswer.Classify(err)
	switch a.Genus() {
	case peeranswer.Answered:
		return g.decided(d)
	case peeranswer.Unavailable:
		g.count(a, outcomeUnavailable)
		return g.deferred(feed.ReasonPlatformUnavailable)
	case peeranswer.Refused:
		return g.refused(a, outcomeRefused, peeranswer.CauseResolveSendRefused)
	}
	// Нулевой род Classify не производит; если он появится — это дефект
	// классификатора, и он звучит как отказ, а не проходит молча.
	return g.refused(a, outcomeRefused, peeranswer.CauseResolveSendRefused)
}

// decided — ответ получен: решение kaname либо его отсутствие.
func (g *Gate) decided(d Decision) Verdict {
	ok := peeranswer.Classify(nil)
	switch d {
	case DecisionAllow:
		g.count(ok, outcomeAllow)
		return Verdict{allowed: true}
	case DecisionNotYetGranted:
		g.count(ok, outcomeNotYetGranted)
		return g.deferred(feed.ReasonGrantSkew)
	case DecisionRevoked:
		g.count(ok, outcomeRevoked)
		return Verdict{outcome: feed.Outcome{Kind: feed.KindDenied, Reason: feed.ReasonRevoked}}
	case DecisionUnset:
		return g.refused(peeranswer.Malformed(), outcomeProtocol, peeranswer.CauseResolveSendProtocol)
	}
	// Значение вне перечня — тот же исход, что незаданный вариант (УК69).
	return g.refused(peeranswer.Malformed(), outcomeProtocol, peeranswer.CauseResolveSendProtocol)
}

func (g *Gate) refused(a peeranswer.Answer, outcome string, cause peeranswer.Cause) Verdict {
	g.count(a, outcome)
	g.r.signals.Misconfigured(g.module, cause)
	return g.deferred(feed.ReasonPlatformUnavailable)
}

func (g *Gate) deferred(reason feed.Reason) Verdict {
	return Verdict{outcome: feed.Outcome{Kind: feed.KindDefer, Reason: reason}, deferFor: g.r.policy.DeferFor}
}

func (g *Gate) count(a peeranswer.Answer, outcome string) {
	g.r.calls.WithLabelValues(g.module, peeranswer.CodeLabel(a.Code()), outcome).Inc()
}
