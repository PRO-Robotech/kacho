// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build integration

package repo

// audit_shipping_integration_test.go — журнал аудита вычислений ДОЕЗЖАЕТ до
// приёмника, и доезжает ЦЕЛИКОМ.
//
// # Что здесь проверяется сверх проб самого механизма
//
// Пробы `pkg/audit` гоняют вывоз по таблице, объявленной в них же, поэтому они
// утверждают о МЕХАНИЗМЕ и молчат о том, совпадает ли с ним ЖИВАЯ таблица.
// Здесь применены настоящие миграции службы и работает настоящая функция
// записи — то есть проверяется ровно то, чего механизм проверить не может: что
// форма журнала службы вывозу адресуема, а колонки, которых нет у соседа
// (актор, второе лицо, предмет, область), доезжают до приёмника.

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/audit"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
)

type capturingSink struct {
	mu  sync.Mutex
	got []audit.Record
}

func (s *capturingSink) Ship(_ context.Context, r audit.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, r)
	return nil
}

func (s *capturingSink) Name() string { return "capturing" }

func (s *capturingSink) records() []audit.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Record, len(s.got))
	copy(out, s.got)
	return out
}

// TestAuditJournalReachesTheSink — строка, записанная НАСТОЯЩЕЙ функцией
// эмиссии, вывозится и помечается доставленной вместе с обоими лицами.
func TestAuditJournalReachesTheSink(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, emitAudit(ctx, tx, AuditEvent{
		EventType:    "instance.create",
		ResourceType: "Instance",
		ResourceID:   "ins-shipping",
		ProjectID:    "prj-shipping",
		Actor:        operations.Principal{Type: "service_account", ID: "sa-worker"},
		OnBehalfOf:   operations.Principal{Type: "user", ID: "usr-initiator"},
		Payload:      map[string]any{"zone_id": "zn-a"},
	}))
	require.NoError(t, tx.Commit(ctx))

	sink := &capturingSink{}
	sh, err := audit.NewShipper(pool, sink, metrics.NewMemRecorder(),
		observability.NewSlogger(io.Discard),
		audit.ShipperConfig{Table: "public.audit_outbox"})
	require.NoError(t, err)

	res, err := sh.Pass(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, res.Shipped, "живая таблица журнала обязана быть адресуема вывозу")
	require.Zero(t, res.Deferred)

	got := sink.records()
	require.Len(t, got, 1)
	rec := got[0]
	require.Equal(t, "instance.create", rec.EventType)
	require.Equal(t, "ins-shipping", rec.Fields["resource_id"])
	require.Equal(t, "prj-shipping", rec.Fields["project_id"])
	// Оба лица — то, ради чего журнал существует. Доехало бы одно, и «кто
	// попросил» осталось бы неизвестным.
	require.Equal(t, "service_account", rec.Fields["actor_type"])
	require.Equal(t, "sa-worker", rec.Fields["actor_id"])
	require.Equal(t, "user", rec.Fields["on_behalf_of_type"])
	require.Equal(t, "usr-initiator", rec.Fields["on_behalf_of_id"])
	payload, ok := rec.Fields["payload"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "zn-a", payload["zone_id"])
	require.NotContains(t, rec.Fields, "status", "учётные колонки доставки — не часть записи журнала")

	var (
		status   string
		attempts int
		sentAt   *string
	)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, attempts, sent_at::text FROM audit_outbox WHERE id = $1`, rec.ID).
		Scan(&status, &attempts, &sentAt))
	require.Equal(t, "sent", status)
	require.Equal(t, 1, attempts)
	require.NotNil(t, sentAt)
}

// TestAuditJournalStatusVocabularyIsTwo — ОТРИЦАНИЕ: словарь состояний закрыт
// ровно тем, что продукт производит.
func TestAuditJournalStatusVocabularyIsTwo(t *testing.T) {
	pool := auditTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, emitAudit(ctx, tx, AuditEvent{
		EventType:    "instance.delete",
		ResourceType: "Instance",
		ResourceID:   "ins-vocab",
		Actor:        operations.Principal{Type: "user", ID: "usr-vocab"},
	}))
	require.NoError(t, tx.Commit(ctx))

	// Положительный контроль: законное состояние ограничением принимается.
	_, err = pool.Exec(ctx,
		`UPDATE audit_outbox SET status = 'pending' WHERE resource_id = 'ins-vocab'`)
	require.NoError(t, err)

	// Отрицание: КАЖДОЕ снятое состояние отвергается базой, а не кодом.
	//
	// Перебор обязателен: проба, щупающая одно значение из двух, остаётся
	// зелёной при возврате второго — то есть половину словаря не проверяет
	// вовсе, и это ровно та половина, о которой никто не вспомнит.
	for _, gone := range []string{"in_flight", "failed"} {
		_, err = pool.Exec(ctx,
			`UPDATE audit_outbox SET status = $1 WHERE resource_id = 'ins-vocab'`, gone)
		require.Error(t, err,
			"состояние %q продукт не производит — оно обязано быть невыразимо в таблице", gone)
	}
}
