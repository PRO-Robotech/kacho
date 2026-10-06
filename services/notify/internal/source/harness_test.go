// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// harness_test.go — провязка фикстуры к испытуемому: получатель пачек как
// порт [Deliverer] и запуск циклов [Start] по перечню. Отделена от
// fixture_test.go намеренно: фикстура и её самопроверка собираются и
// исполняются без испытуемого (положительный контроль фикстуры идёт ПЕРВЫМ),
// а здесь — единственное место фикстуры, которое испытуемого называет.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// Deliver — порт получателя: пачка одного `Claim` одного источника.
func (d *fakeDeliverer) Deliver(_ context.Context, b Batch) {
	now := time.Now()
	module := b.Source.Module
	d.mu.Lock()
	for _, r := range b.Rows {
		d.got = append(d.got, delivery{module: module, id: r.GetId(), at: now})
	}
	src := d.sources[module]
	d.mu.Unlock()
	if src != nil {
		for _, r := range b.Rows {
			src.complete(r.GetId())
		}
	}
}

// Wait — строк в полёте у получателя фикстуры нет: исход строки он
// записывает в самом Deliver.
func (d *fakeDeliverer) Wait() {}

// Получатель фикстуры — порт испытуемого, а не своя копия.
var _ Deliverer = (*fakeDeliverer)(nil)

// startLoops поднимает циклы по перечню и гасит их при уборке пробы.
func startLoops(t *testing.T, ca *testCA, roster []config.Source, d Deliverer,
	interval time.Duration) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	loops, err := Start(ctx, Config{
		Sources:       roster,
		Peer:          ca.peerTLS(t),
		ClaimInterval: interval,
		Deliverer:     d,
		Metrics:       reg,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		cancel()
		t.Fatalf("Start отверг исправный перечень источников: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		done := make(chan struct{})
		go func() {
			loops.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(wantCallTimeout + 5*time.Second):
			t.Errorf("циклы источников не остановились за %s после отмены контекста", wantCallTimeout+5*time.Second)
		}
	})
	return reg
}
