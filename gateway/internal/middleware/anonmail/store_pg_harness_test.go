// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
)

// driverCancelSleep — сон драйвера после запроса отмены до снятия наблюдения
// (pgx v5.10.0 pgconn.go:3004; замысел М65, УК58): соединение не отдаётся
// следующему оператору до его конца. Входит в допуск проб на НАСТОЯЩЕМ пуле.
const driverCancelSleep = 100 * time.Millisecond

// cancelCost — цена одной отмены по сроку на пуле ограничителя (УК58):
// запасной дедлайн обработчика и сон драйвера.
const cancelCost = anonMailCancelGrace + driverCancelSleep

// edgeDB — база края с цепочкой миграций хранилища однократности (в том числе
// таблицами ограничителя): строит её само хранилище, как корень края.
func edgeDB(t testing.TB) string {
	t.Helper()
	dsn := pgtest.NewDB(t)
	idem, err := idempotencypg.New(context.Background(), idempotencypg.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("хранилище однократности: %v", err)
	}
	_ = idem.Close()
	return dsn
}

// syncBuffer — журнал пробы, безопасный к записи из горутин.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newPgStore(t testing.TB, dsn string, l config.AnonMailLimits, log *slog.Logger) *PostgresStore {
	t.Helper()
	s, err := NewPostgresStore(context.Background(), dsn, l, log)
	if err != nil {
		t.Fatalf("хранилище ограничителя: %v", err)
	}
	return s
}

func pgRig(t testing.TB, dsn string, l config.AnonMailLimits) (*rig, *PostgresStore, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var ps *PostgresStore
	r := newRig(t, l, func(*testClock) Store { ps = newPgStore(t, dsn, l, log); return ps }, 0)
	return r, ps, logs
}

// sourceCount — сколько моментов пропуска записано за ключом (прямым чтением).
func sourceCount(t testing.TB, dsn, key string) int {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var n int
	if err := c.QueryRow(context.Background(),
		`SELECT count(*) FROM kacho_gateway.anon_mail_passes WHERE key = $1`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func rawPool(t testing.TB, dsn string, max int32) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = max
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
