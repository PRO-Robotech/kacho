// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// harness_test.go — провязка фикстуры к испытуемому. Единственный файл
// фикстуры, который называет испытуемого: фикстура (fixture_test.go) и её
// самопроверка собираются и исполняются без него.
//
// Что пробы требуют от пакета `deliver` (замысел З21, З22, SDR-Н1):
//
//   - [New] собирает исполнителей строк из [Config]; результат — получатель
//     пачек цикла источника (`source.Deliverer`: Free, Deliver) и Wait —
//     дождаться строк в полёте (остановка процесса доводит их, З24 (в)).
//   - Порты: сборка ([Build]), право ([Grants] — `*grant.Resolver`), сетка
//     ([Limiter] — `*limits.Limiter`), рендер ([Renderer], предмет N5),
//     отправитель ([Sender] — `*smtp.Sender`), лента источника по модулю
//     ([Feed] — клиент gRPC ленты), внедряемые часы ([Clock]: длительности,
//     а не моменты для арифметики сроков, УК80).
//   - [Resolved] — описание строки после клеток 1–2; рендер получает его.

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/grant"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
	"github.com/PRO-Robotech/kacho/services/notify/internal/source"
)

// fixtureRender — рендер фикстуры в порт испытуемого.
type fixtureRender struct{ r *fakeRenderer }

func (f fixtureRender) Render(Resolved) ([]byte, error) { return f.r.render() }

// rig — процесс notify пробы: испытуемый на портах фикстуры.
type rig struct {
	t       *testing.T
	build   *fixtureBuild
	peer    *fakePeer
	limiter *fakeLimiter
	render  *fakeRenderer
	relay   *smtptest.Relay
	sender  *recordingSender
	feeds   map[string]*fakeFeed
	sources map[string]config.Source
	clock   *testClock
	reg     *prometheus.Registry
	log     *logBuffer
	w       *Worker
}

// rigOpts — условия процесса пробы. Нулевые поля — исправные умолчания
// фикстуры: узел, принимающий всё; отправитель со своим пределом 30 с (срок
// SMTP ставит испытуемый); kaname отвечает ALLOW; часы без сдвига.
type rigOpts struct {
	build       *fixtureBuild
	sources     []config.Source
	relay       smtptest.Script
	senderLimit time.Duration
	clock       *testClock
	decision    grant.Decision
	peerDelay   time.Duration
	exhausted   bool
	feed        func(module string, f *fakeFeed)
}

func newRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	if o.build == nil {
		t.Fatal("ФИКСТУРА: сборка не задана")
	}
	if len(o.sources) == 0 {
		o.sources = []config.Source{sourceOf(nsProbe, config.RecipientAddress)}
	}
	if o.senderLimit == 0 {
		o.senderLimit = 30 * time.Second
	}
	if o.clock == nil {
		o.clock = &testClock{}
	}
	if o.decision == grant.DecisionUnset {
		o.decision = grant.DecisionAllow
	}
	r := &rig{
		t:       t,
		build:   o.build,
		peer:    &fakePeer{decision: o.decision, delay: o.peerDelay},
		limiter: &fakeLimiter{exhausted: o.exhausted},
		render:  &fakeRenderer{},
		feeds:   map[string]*fakeFeed{},
		sources: map[string]config.Source{},
		clock:   o.clock,
		reg:     prometheus.NewRegistry(),
	}
	r.relay, r.sender = relayWith(t, o.relay, o.senderLimit)
	feeds := map[string]Feed{}
	for _, s := range o.sources {
		f := newFakeFeed()
		if o.feed != nil {
			o.feed(s.Module, f)
		}
		r.feeds[s.Module] = f
		r.sources[s.Module] = s
		feeds[s.Module] = f
	}
	logger, lb := newLog()
	r.log = lb
	w, err := New(Config{
		Build:              o.build,
		Sources:            o.sources,
		Grants:             newResolver(t, r.peer, r.reg, o.sources...),
		Limiter:            r.limiter,
		Render:             fixtureRender{r.render},
		Sender:             r.sender,
		Feeds:              feeds,
		From:               fixtureFrom,
		Workers:            8,
		ResolveSendTimeout: probeResolveSendTimeout,
		SMTPSessionTimeout: probeSMTPSessionTimeout,
		DeferFor:           probeDeferFor,
		Clock:              o.clock,
		Metrics:            r.reg,
		Log:                logger,
	})
	if err != nil {
		t.Fatalf("deliver.New: %v", err)
	}
	r.w = w
	return r
}

// deliver отдаёт пачку `Claim` источника module, отправленного в sentAt
// (монотонное показание), и ждёт, пока исполнители её доведут.
func (r *rig) deliver(module string, sentAt time.Time, rows ...*notifyv1.ClaimedNotification) {
	r.t.Helper()
	src, ok := r.sources[module]
	if !ok {
		r.t.Fatalf("ФИКСТУРА: источника %q нет в перечне", module)
	}
	r.feeds[module].lease(sentAt, rows...)
	var _ source.Deliverer = r.w
	if free := r.w.Free(); free < len(rows) {
		r.t.Fatalf("свободных исполнителей %d меньше пачки %d", free, len(rows))
	}
	r.w.Deliver(context.Background(), source.Batch{Source: src, Rows: rows, SentAt: sentAt})
	r.w.Wait()
}

// one — одна строка источника module, отправленная сейчас; исход записан
// лентой (nil — исхода нет).
func (r *rig) one(module string, row *notifyv1.ClaimedNotification) *notifyv1.Outcome {
	r.t.Helper()
	r.deliver(module, time.Now(), row)
	return r.feeds[module].Recorded(row.GetId())
}

// reclaimUnrecorded — следующий `Claim` источника: строки без записанного
// исхода выдаются снова с новым токеном аренды (так делает оператор аренды
// после её конца, З8). Возвращает, сколько строк выдано повторно.
func (r *rig) reclaimUnrecorded(module string, rows ...*notifyv1.ClaimedNotification) int {
	r.t.Helper()
	var again []*notifyv1.ClaimedNotification
	for _, row := range rows {
		if r.feeds[module].Recorded(row.GetId()) != nil {
			continue
		}
		next := claimed(rowSpec{})
		next.Id = row.GetId()
		next.Template, next.SchemaRev, next.Class = row.GetTemplate(), row.GetSchemaRev(), row.GetClass()
		next.Recipient, next.Attrs = row.GetRecipient(), row.GetAttrs()
		next.LeaseRemaining, next.ExpiresIn, next.EnqueuedAt = row.GetLeaseRemaining(), row.GetExpiresIn(), row.GetEnqueuedAt()
		again = append(again, next)
	}
	if len(again) > 0 {
		r.deliver(module, time.Now(), again...)
	}
	return len(again)
}

// sessions — SMTP-сессий, принятых узлом.
func (r *rig) sessions() int { return len(r.relay.Sessions()) }

// letters — писем, принятых узлом.
func (r *rig) letters() int { return len(r.relay.Messages()) }
