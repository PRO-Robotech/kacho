// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// ackpath_test.go — путь `Ack` строки пачки (полоса A2 задачи #2915; замысел
// З21 «`Ack` — в собственном контексте», SDR-Н1).
//
// Что держат пробы:
//
//   - пачка несёт путь `Ack` ТОГО ЖЕ соединения, по которому строки взяты в
//     аренду ([Batch.Feed]): исход строки уходит серверу ленты, выдавшему её,
//     с тем же удостоверением и точным SAN, — второго соединения к источнику
//     и второго перечня клиентов у исполнителей нет;
//   - останов: [Loops.Wait] закрывает соединения с источниками только ПОСЛЕ
//     того, как получатель пачек довёл строки в полёте ([Deliverer.Wait]) —
//     иначе `Ack` строки, чьё письмо уже ушло, падает на закрытом соединении,
//     и строка уходит повторно следующим `Claim` (дубль письма).

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// ackingDeliverer — получатель, который записывает исход каждой строки через
// путь `Ack` пачки. Исход строки идёт после release; Wait — дождаться строк
// в полёте, как у исполнителей (deliver.Worker.Wait).
type ackingDeliverer struct {
	release chan struct{}

	mu         sync.Mutex
	wg         sync.WaitGroup
	got        int
	nilFeed    int
	ackErrs    []error
	waitCalled bool
	once       sync.Once
}

func newAckingDeliverer() *ackingDeliverer {
	return &ackingDeliverer{release: make(chan struct{})}
}

func (d *ackingDeliverer) Free() int { return 8 }

func (d *ackingDeliverer) Deliver(ctx context.Context, b Batch) {
	for _, row := range b.Rows {
		d.mu.Lock()
		d.got++
		if b.Feed == nil {
			d.nilFeed++
			d.mu.Unlock()
			continue
		}
		d.mu.Unlock()
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			<-d.release
			ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, err := b.Feed.Ack(ackCtx, &notifyv1.AckRequest{
				Id:         row.GetId(),
				LeaseToken: row.GetLeaseToken(),
				Outcome:    &notifyv1.Outcome{Kind: notifyv1.OutcomeKind_SENT},
			})
			d.mu.Lock()
			d.ackErrs = append(d.ackErrs, err)
			d.mu.Unlock()
		}()
	}
}

// Wait — доводит строки в полёте: отпускает их исход и ждёт его.
func (d *ackingDeliverer) Wait() {
	d.mu.Lock()
	d.waitCalled = true
	d.mu.Unlock()
	d.letGo()
	d.wg.Wait()
}

func (d *ackingDeliverer) letGo() { d.once.Do(func() { close(d.release) }) }

func (d *ackingDeliverer) snapshot() (got, nilFeed int, errs []error, waitCalled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.got, d.nilFeed, append([]error(nil), d.ackErrs...), d.waitCalled
}

var _ Deliverer = (*ackingDeliverer)(nil)

func startAcking(t *testing.T, ca *testCA, roster []config.Source, d Deliverer) (context.CancelFunc, *Loops) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	loops, err := Start(ctx, Config{
		Sources:       roster,
		Peer:          ca.peerTLS(t),
		ClaimInterval: 100 * time.Millisecond,
		Deliverer:     d,
		Metrics:       prometheus.NewRegistry(),
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		cancel()
		t.Fatalf("Start отверг исправный перечень: %v", err)
	}
	return cancel, loops
}

// TestBatchCarriesTheAckPathOfItsLease — исход строки, записанный через путь
// пачки, принят сервером, выдавшим аренду.
func TestBatchCarriesTheAckPathOfItsLease(t *testing.T) {
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{})
	d := newAckingDeliverer()
	d.letGo()
	cancel, loops := startAcking(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d)
	t.Cleanup(func() { cancel(); loops.Wait() })

	ids := src.put(2, true)
	if !waitFor(10*time.Second, func() bool { return len(src.acksSeen()) == len(ids) }) {
		got, nilFeed, errs, _ := d.snapshot()
		t.Fatalf("исходов принято %d из %d: строк получено %d, из них без пути Ack %d, ошибки Ack %v",
			len(src.acksSeen()), len(ids), got, nilFeed, errs)
	}
	for _, a := range src.acksSeen() {
		if a.kind != "SENT" || a.token != "lease-"+a.id {
			t.Fatalf("исход %+v записан не тем, что отправил получатель", a)
		}
	}
}

// TestLoopsWaitDrainsTheDelivererBeforeClosingConnections — останов: исход
// строки в полёте доходит до источника после отмены контекста циклов.
func TestLoopsWaitDrainsTheDelivererBeforeClosingConnections(t *testing.T) {
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{})
	d := newAckingDeliverer()
	cancel, loops := startAcking(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d)

	src.put(1, true)
	if !waitFor(10*time.Second, func() bool { got, _, _, _ := d.snapshot(); return got == 1 }) {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: строка не дошла до получателя — останову нечего доводить")
	}
	cancel()
	done := make(chan struct{})
	go func() { loops.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(sourceCallTimeout + 5*time.Second):
		t.Fatal("Loops.Wait не вернулся после отмены")
	}
	// Получатель, которого Wait не дождался, доводит строку уже после закрытия
	// соединений — так выглядит останов процесса, где Ack отстал.
	d.letGo()
	d.wg.Wait()
	_, _, errs, waitCalled := d.snapshot()
	if !waitCalled {
		t.Errorf("Loops.Wait не дождался строк в полёте получателя (Deliverer.Wait не позван)")
	}
	if len(errs) != 1 || errs[0] != nil {
		t.Fatalf("исход строки в полёте не дошёл до источника после останова: %v", errs)
	}
	if n := len(src.acksSeen()); n != 1 {
		t.Fatalf("источник принял %d исходов, ожидался 1", n)
	}
}
