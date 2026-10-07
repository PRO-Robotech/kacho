// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

// retention_integration_test.go — уборка notify по возрасту окна (замысел §6:
// «строки старше окна снимает уборка notify, в том числе ничьи строки
// прежнего ключа после ротации», З24). Предметы — recipient_net и global_daily;
// ограда — строка-одиночка, уборке не подлежит.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/retention"
)

// subjectOf — уборщик предмета по имени таблицы; нет — отказ пробы.
func subjectOf(t *testing.T, subjects []retention.Subject, name string) retention.Subject {
	t.Helper()
	for _, s := range subjects {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("уборщика %s нет среди предметов уборки %d", name, len(subjects))
	return retention.Subject{}
}

func countRows(t *testing.T, pool *pgxpool.Pool, q string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatalf("счёт строк %q: %v", q, err)
	}
	return n
}

// Строки окон, закончившихся раньше порога (часы базы), снимаются — в том числе
// ничьи строки прежнего ключа; строки действующих окон и сегодняшняя строка
// потолка остаются (близнец). Партия ограничивает оператор: две прошедших строки
// при партии 1 — снята одна и партия названа полной.
func TestLimits_RetentionSweepsEndedWindowsOnly(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(time.Now())
	r := started(t, pool, keyOne, gridWide, clk)
	ctx := context.Background()
	if _, err := r.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "live@example.invalid"))); err != nil {
		t.Fatal(err)
	}
	if _, err := r.lim.Reserve(ctx, row("kaname", feed.ClassSecurity, addr(t, "live@example.invalid"))); err != nil {
		t.Fatal(err)
	}
	// Прошедшие окна: ключ прежнего ключа сетки (ничья строка после ротации).
	old := netKey(t, keyTwo, addr(t, "old@example.invalid"))
	if _, err := pool.Exec(ctx, `INSERT INTO recipient_net (key, class, period, window_start, count) VALUES
		($1, 'notice', 'hour', '2026-01-01T10:00:00Z', 3),
		($1, 'notice', 'day', '2026-01-01T00:00:00Z', 5),
		($1, 'security', 'day', '2026-01-02T00:00:00Z', 1)`, old); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: прошедшие окна не посеяны: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO global_daily (day, count) VALUES ('2026-01-01', 9), ('2026-01-02', 4)`); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: прошедшие сутки потолка не посеяны: %v", err)
	}

	subjects := r.lim.RetentionSubjects()
	net := subjectOf(t, subjects, "recipient_net")
	daily := subjectOf(t, subjects, "global_daily")
	if net.Grace < feed.LeaseTTL || daily.Grace < feed.LeaseTTL {
		t.Fatalf("порог уборки (%s, %s) короче аренды строки %s: окно сняли бы раньше, чем освобождение "+
			"резерва в нём обязано закончиться", net.Grace, daily.Grace, feed.LeaseTTL)
	}

	removed, full, err := net.Sweep(ctx, net.Grace, 1)
	if err != nil || removed != 1 || !full {
		t.Fatalf("партия 1 при трёх прошедших строках: снято %d, полная %v, %v — ожидалось 1 и полная", removed, full, err)
	}
	removed, full, err = net.Sweep(ctx, net.Grace, 1000)
	if err != nil || removed != 2 || full {
		t.Fatalf("остаток прошедших окон: снято %d, полная %v, %v — ожидалось 2 и неполная", removed, full, err)
	}
	if got := countRows(t, pool, `SELECT count(*) FROM recipient_net WHERE window_start < '2026-02-01'`); got != 0 {
		t.Fatalf("строк прошедших окон осталось %d", got)
	}
	// Близнец: окна действующего часа и суток (notice: час и сутки, security: сутки).
	if got := countRows(t, pool, `SELECT count(*) FROM recipient_net`); got != 3 {
		t.Fatalf("строк действующих окон %d, ожидалось 3 — уборка сняла живое окно", got)
	}

	removed, _, err = daily.Sweep(ctx, daily.Grace, 1000)
	if err != nil || removed != 2 {
		t.Fatalf("прошедшие сутки потолка: снято %d, %v — ожидалось 2", removed, err)
	}
	if got := countRows(t, pool, `SELECT count(*) FROM global_daily`); got != 1 {
		t.Fatalf("строк потолка после уборки %d, ожидалась 1 (сегодняшняя)", got)
	}
	if got := countRows(t, pool, `SELECT count(*) FROM recipient_key_fence`); got != 1 {
		t.Fatalf("ограда после уборки: строк %d, ожидалась 1", got)
	}
}
