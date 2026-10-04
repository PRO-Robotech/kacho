// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// roster_test.go — перечень источников и клиент с точным SAN (приёмка
// NTF1-G22; замысел З21 «На каждый источник — один цикл», УК24), размер
// `Claim` по свободным исполнителям (З21 «Размер `Claim`»).
//
// Сторона G22 «строка `kaname` отправлена без `ResolveSend`» — решение
// получателя пачки (полосы N3/N4), здесь не утверждается: цикл передаёт
// получателю запись перечня вместе с пачкой.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// NTF1-G22 — страж старта: `authorization: certificate` допустим только
// записи `kaname`; у любой другой — отказ с именем записи. Близнец —
// `kaname` с `certificate` и `probe` с `resolveSend`: старт, строки обоих
// доходят до получателя.
func TestSource_NTF1G22_CertificateOnlyForKaname(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)

	t.Run("foreign-record-with-certificate-is-refused", func(t *testing.T) {
		t.Parallel()
		kaname := newFakeSource(t, ca, "kaname", sourceOpts{})
		probe := newFakeSource(t, ca, "probe", sourceOpts{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		loops, err := Start(ctx, Config{
			Sources: []config.Source{
				kaname.record(config.AuthorizationCertificate),
				probe.record(config.AuthorizationCertificate),
			},
			Peer:          ca.peerTLS(t),
			ClaimInterval: time.Second,
			Deliverer:     newDeliverer(8, kaname, probe),
			Metrics:       prometheus.NewRegistry(),
			Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err == nil {
			cancel()
			loops.Wait()
			t.Fatal("NTF1-G22: перечень с authorization: certificate у записи probe принят — исключение " +
				"Р3 допустимо только kaname")
		}
		msg := err.Error()
		if !strings.Contains(msg, `probe`) || !strings.Contains(msg, string(config.AuthorizationCertificate)) {
			t.Fatalf("NTF1-G22: отказ старта не называет запись probe и значение certificate: %q", msg)
		}
	})

	t.Run("twin-kaname-certificate-probe-resolvesend", func(t *testing.T) {
		t.Parallel()
		kaname := newFakeSource(t, ca, "kaname", sourceOpts{})
		probe := newFakeSource(t, ca, "probe", sourceOpts{})
		d := newDeliverer(8, kaname, probe)
		startLoops(t, ca, []config.Source{
			kaname.record(config.AuthorizationCertificate),
			probe.record(config.AuthorizationResolveSend),
		}, d, time.Second)
		idsK, idsP := kaname.put(1, true), probe.put(1, true)
		if !waitFor(5*time.Second, func() bool {
			nk, _ := d.deliveries("kaname", idsK[0])
			np, _ := d.deliveries("probe", idsP[0])
			return nk == 1 && np == 1
		}) {
			nk, _ := d.deliveries("kaname", idsK[0])
			np, _ := d.deliveries("probe", idsP[0])
			t.Fatalf("близнец NTF1-G22: доставлено kaname %d, probe %d — ожидалось по 1", nk, np)
		}
	})
}

// NTF1-G22 — сервер ленты `kaname` предъявил SAN, отличный от записи
// перечня: подключение отвергнуто, ни `Claim`, ни `Subscribe` до сервера не
// дошли, строка не доставлена. Близнец — тот же сервер с SAN записи: строка
// доставлена. Различие — ровно URI-SAN сертификата сервера
// (TestFixtureServerSANIsTheOnlyDifference).
//
// Тревога `source_identity_mismatch` здесь не утверждается: её носителя
// (имени метрики) нет ни в приёмке, ни в перечне метрик З27 — вопрос к
// приёмке в возврате полосы.
func TestSource_NTF1G22_ForeignServerSANIsRefused(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)

	t.Run("foreign-san", func(t *testing.T) {
		t.Parallel()
		src := newFakeSource(t, ca, "kaname", sourceOpts{serverSAN: sanOf("probe")})
		ids := src.put(1, true)
		d := newDeliverer(8, src)
		startLoops(t, ca, []config.Source{src.record(config.AuthorizationCertificate)}, d, tick)

		<-time.After(3 * tick)
		if n := len(src.claimCalls()); n != 0 {
			t.Fatalf("NTF1-G22: Claim дошёл до сервера ленты с чужим SAN %s (вызовов %d) — клиент не сверяет "+
				"точный SAN записи %s", sanOf("probe"), n, sanOf("kaname"))
		}
		if n := len(src.streamsSeen()); n != 0 {
			t.Fatalf("NTF1-G22: Subscribe дошёл до сервера с чужим SAN (потоков %d)", n)
		}
		if n, _ := d.deliveries("kaname", ids[0]); n != 0 {
			t.Fatalf("NTF1-G22: строка сервера с чужим SAN доставлена %d раз", n)
		}
	})

	t.Run("twin-own-san", func(t *testing.T) {
		t.Parallel()
		src := newFakeSource(t, ca, "kaname", sourceOpts{})
		ids := src.put(1, true)
		d := newDeliverer(8, src)
		startLoops(t, ca, []config.Source{src.record(config.AuthorizationCertificate)}, d, tick)

		if !waitFor(3*tick, func() bool { n, _ := d.deliveries("kaname", ids[0]); return n == 1 }) {
			t.Fatalf("близнец NTF1-G22: сервер с SAN записи, строка %s не доставлена за %s (вызовов Claim %d)",
				ids[0], 3*tick, len(src.claimCalls()))
		}
	})
}

// З21 — размер `Claim` — число свободных исполнителей получателя: каждый
// вызов просит от 1 до свободных; при нуле свободных `Claim` не зовётся.
// Близнец — свободные появились: строка доставлена.
func TestSource_ClaimAsksNoMoreThanFreeWorkers(t *testing.T) {
	t.Parallel()
	const tick = time.Second
	ca := newTestCA(t)

	t.Run("two-free", func(t *testing.T) {
		t.Parallel()
		src := newFakeSource(t, ca, "probe", sourceOpts{})
		ids := src.put(5, false)
		d := newDeliverer(2, src)
		startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)

		if !waitFor(8*time.Second, func() bool {
			for _, id := range ids {
				if n, _ := d.deliveries("probe", id); n != 1 {
					return false
				}
			}
			return true
		}) {
			t.Fatalf("З21: пять строк при двух свободных исполнителях не доставлены за 8 с (вызовов Claim %d)",
				len(src.claimCalls()))
		}
		for i, c := range src.claimCalls() {
			if c.max < 1 || c.max > 2 {
				t.Fatalf("З21: вызов Claim #%d просил max=%d при 2 свободных исполнителях", i+1, c.max)
			}
		}
	})

	t.Run("none-free-then-one", func(t *testing.T) {
		t.Parallel()
		src := newFakeSource(t, ca, "probe", sourceOpts{})
		d := newDeliverer(0, src)
		startLoops(t, ca, []config.Source{src.record(config.AuthorizationResolveSend)}, d, tick)
		if !waitFor(5*time.Second, func() bool { return len(src.streamsSeen()) >= 1 }) {
			t.Fatal("З21: notify не открыл подписку за 5 с")
		}
		ids := src.put(1, true)
		<-time.After(3 * tick)
		if n := len(src.claimCalls()); n != 0 {
			t.Fatalf("З21: при нуле свободных исполнителей Claim вызван %d раз (max первого %d)",
				n, src.claimCalls()[0].max)
		}
		d.setFree(1)
		src.put(0, true)
		if !waitFor(3*tick, func() bool { n, _ := d.deliveries("probe", ids[0]); return n == 1 }) {
			t.Fatalf("близнец З21: свободный исполнитель появился, строка %s не доставлена за %s", ids[0], 3*tick)
		}
	})
}
