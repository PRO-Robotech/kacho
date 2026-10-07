// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

// classes_test.go — сетка на адресата и ворота источника знают ровно классы,
// которые источник отдаёт notify по сети: закрытый перечень ленты без классов
// только процесса (feed.LocalOnlyClasses — obligation, NTF-5 Р12). Класс
// только процесса сеть не выдаёт (Claim сервера ленты его не знает), окон
// сетки у него нет ([limits.Limiter] отвечает на него «вне перечня»), и ряд
// `notify_recipient_net_hits_total{class}` с ним был бы рядом, который никто
// не пишет.

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// TestLimits_NetworkClassesAreTheFeedClassesTheNetworkCarries — перечень сети
// выводится из перечней ленты, а не выписан: каждый класс ленты либо в нём,
// либо в классах только процесса, ни один — в обоих. Предпосылка отрицания —
// класс только процесса существует.
func TestLimits_NetworkClassesAreTheFeedClassesTheNetworkCarries(t *testing.T) {
	network, local := limits.NetworkClasses(), feed.LocalOnlyClasses()
	if len(local) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: у ленты нет классов только процесса — отрицанию нечего подавать")
	}
	if len(network) == 0 {
		t.Fatal("перечень классов сети пуст")
	}
	for _, c := range feed.Classes() {
		inNet, inLocal := slices.Contains(network, c), slices.Contains(local, c)
		if inNet == inLocal {
			t.Errorf("класс ленты %q: в перечне сети %v, в классах только процесса %v — ровно одно из двух", c, inNet, inLocal)
		}
	}
	for _, c := range network {
		if !slices.Contains(feed.Classes(), c) {
			t.Errorf("класс %q перечня сети вне перечня ленты %v", c, feed.Classes())
		}
	}
}

// TestLimits_NetHitsSeriesAreTheNetworkClasses — ряды
// `notify_recipient_net_hits_total{class}`, выставленные при сборке сетки, —
// ровно классы сети: класса только процесса среди них нет. Близнец — каждый
// класс сети ряд имеет. Пул ленивый: сборка сетки к базе не обращается.
func TestLimits_NetHitsSeriesAreTheNetworkClasses(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://probe@127.0.0.1:1/probe?sslmode=disable")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ленивый пул не собран: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	reg := prometheus.NewRegistry()
	if _, err := limits.New(limits.Options{
		Pool:       pool,
		Key:        make([]byte, limits.KeyMinBytes),
		Grid:       gridWide,
		Now:        newClock(noon).Now,
		Registerer: reg,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}); err != nil {
		t.Fatalf("limits.New отказал на годных опциях: %v", err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("реестр метрик не собран: %v", err)
	}
	var got []feed.Class
	for _, mf := range mfs {
		if mf.GetName() != "notify_recipient_net_hits_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "class" {
					got = append(got, feed.Class(lp.GetValue()))
				}
			}
		}
	}
	want := slices.Clone(limits.NetworkClasses())
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("ряды notify_recipient_net_hits_total{class} = %v, ожидались классы сети %v", got, want)
	}
}
