// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Пробы инвариантов базы ограничителя (заказ ревью схемы db, issue-2917 E2):
// каждое свойство держит механизм БАЗЫ, и проба гоняет конкурентные решения на
// двух репликах (у каждой свой пул, общее — только база).
//
// Однофактная порча схемы строится из КОПИИ миграции ограничителя: близнец и
// контроль собираются одним способом (копия, применённая к пустой базе), и
// отличаются ровно заменой одного фрагмента — сверка числа вхождений и
// изменённого фрагмента идёт в buildLimiterSchemaFromCopy.

// limiterMigrations — файлы цепочки края, несущие таблицы ограничителя, в
// порядке применения.
var limiterMigrations = []string{
	"../../idempotencypg/migrations/20261001180000_anon_mail_limiter.sql",
	"../../idempotencypg/migrations/20261001190000_anon_mail_passes_key_not_empty.sql",
}

// schemaSpoil — однофактная порча копии миграции: заменить фрагмент from на to
// ровно в одном месте одного файла.
type schemaSpoil struct {
	file     int
	from, to string
}

// gooseUp — раздел Up миграции goose.
func gooseUp(t testing.TB, sql string) string {
	t.Helper()
	_, rest, ok := strings.Cut(sql, "-- +goose Up")
	if !ok {
		t.Fatal("в копии миграции нет раздела Up")
	}
	up, _, ok := strings.Cut(rest, "-- +goose Down")
	if !ok {
		t.Fatal("в копии миграции нет раздела Down")
	}
	return up
}

// buildLimiterSchemaFromCopy — пустая база, схема края и разделы Up копий
// миграций ограничителя; spoil — порча (nil — контроль той же сборки).
func buildLimiterSchemaFromCopy(t testing.TB, spoil *schemaSpoil) string {
	t.Helper()
	dsn := pgtest.NewDB(t)
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if _, err := c.Exec(context.Background(), `CREATE SCHEMA IF NOT EXISTS kacho_gateway`); err != nil {
		t.Fatal(err)
	}
	for i, f := range limiterMigrations {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("копия миграции %s: %v", f, err)
		}
		up := gooseUp(t, string(raw))
		if spoil != nil && spoil.file == i {
			if n := strings.Count(up, spoil.from); n != 1 {
				t.Fatalf("порча %q: вхождений в %s %d, ожидалось ровно одно", spoil.from, f, n)
			}
			spoiled := strings.Replace(up, spoil.from, spoil.to, 1)
			if spoiled == up {
				t.Fatalf("порча %q не изменила копию %s", spoil.from, f)
			}
			up = spoiled
		}
		if _, err := c.Exec(context.Background(), up); err != nil {
			t.Fatalf("копия миграции %s: %v", f, err)
		}
	}
	return dsn
}

// Порчи механизмов (одна замена каждая).
var (
	spoilPowSpentPK = schemaSpoil{file: 0,
		from: "id         BYTEA       PRIMARY KEY CHECK (octet_length(id) = 16)",
		to:   "id         BYTEA       CHECK (octet_length(id) = 16)"}
	spoilBucketCheck = schemaSpoil{file: 0,
		from: "tokens DOUBLE PRECISION NOT NULL CHECK (tokens >= 0)",
		to:   "tokens DOUBLE PRECISION NOT NULL"}
)

// outcomeTally — счёт исходов конкурентных решений.
type outcomeTally struct {
	pass, challenge, reject, unavailable, other atomic.Int64
}

func (o *outcomeTally) add(v Verdict) {
	switch v.Outcome {
	case Pass:
		o.pass.Add(1)
	case Challenge:
		o.challenge.Add(1)
	case Reject:
		o.reject.Add(1)
	case StoreUnavailable:
		o.unavailable.Add(1)
	default:
		o.other.Add(1)
	}
}

func (o *outcomeTally) String() string {
	return fmt.Sprintf("pass %d · challenge %d · reject %d · 503 %d · вне набора %d",
		o.pass.Load(), o.challenge.Load(), o.reject.Load(), o.unavailable.Load(), o.other.Load())
}

// raceDecisions — запросы reqs одновременно (общий старт) на хранилищах stores
// по кругу.
func raceDecisions(stores []Store, reqs []Request) *outcomeTally {
	var (
		tally outcomeTally
		wg    sync.WaitGroup
	)
	start := make(chan struct{})
	for i, r := range reqs {
		wg.Add(1)
		s := stores[i%len(stores)]
		go func() {
			defer wg.Done()
			<-start
			tally.add(s.Decide(context.Background(), r))
		}()
	}
	close(start)
	wg.Wait()
	return &tally
}

// twoReplicas — две реплики над одной базой.
func twoReplicas(t testing.TB, dsn string, l config.AnonMailLimits) []Store {
	t.Helper()
	a := newPgStore(t, dsn, l, discardLogger())
	b := newPgStore(t, dsn, l, discardLogger())
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return []Store{a, b}
}

func mustKeys(t testing.TB, addr string) Keys {
	t.Helper()
	k, err := KeysFor(addr)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// v4 — адрес 10.<a>.<b>.<c>.
func v4(a, b, c int) string {
	return netip.AddrFrom4([4]byte{10, byte(a), byte(b), byte(c)}).String() // #nosec G115 -- адреса пробы, значения < 256
}

func scalar[T any](t testing.TB, dsn, q string, args ...any) T {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var v T
	if err := c.QueryRow(context.Background(), q, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// ── O1 ──────────────────────────────────────────────────────────────────────

// doubleSpendRounds — раунды по одному доказательству, предъявленному с двух
// источников через две реплики одновременно. Возвращает счёт исходов и число
// раундов, в которых пропуск случился не ровно один раз.
func doubleSpendRounds(t *testing.T, dsn string, rounds int) (*outcomeTally, int) {
	t.Helper()
	stores := twoReplicas(t, dsn, orderLimits())
	total := &outcomeTally{}
	bad := 0
	for i := 0; i < rounds; i++ {
		p := Proof{Bits: testLimits().PoWBits.High, ExpiresAt: time.Now().Add(time.Hour)}
		if _, err := rand.Read(p.ID[:]); err != nil {
			t.Fatal(err)
		}
		now := dbMoment()
		reqs := []Request{
			{Keys: mustKeys(t, v4(1, i, 1)), Now: now, Proof: &p},
			{Keys: mustKeys(t, v4(2, i, 1)), Now: now, Proof: &p},
		}
		r := raceDecisions(stores, reqs)
		if r.pass.Load() != 1 {
			bad++
		}
		total.pass.Add(r.pass.Load())
		total.challenge.Add(r.challenge.Load())
		total.reject.Add(r.reject.Load())
		total.unavailable.Add(r.unavailable.Load())
		total.other.Add(r.other.Load())
	}
	return total, bad
}

// TestPg_O1_OneProofFromTwoSourcesAcrossTwoReplicasPassesExactlyOnce — одно
// доказательство предъявлено одновременно с двух источников (разные ключи —
// блокировки ключей решения не сериализуют) через две реплики: пропущено ровно
// одно решение, второе получает новый вызов. Держит первичный ключ pow_spent:
// вставка ON CONFLICT (id) DO NOTHING второго решения ждёт незафиксированную
// вставку первого и узнаёт повтор.
//
// Близнец — копия миграции без PRIMARY KEY у pow_spent.id (один факт): арбитра
// ON CONFLICT (id) нет, пометка падает 42P10, и решение закрывается отказом
// хранилища — ни одного пропуска, а не двойной. Контроль — та же сборка из
// копии без порчи: ровно один.
func TestPg_O1_OneProofFromTwoSourcesAcrossTwoReplicasPassesExactlyOnce(t *testing.T) {
	const rounds = 16
	t.Run("цепочка края", func(t *testing.T) {
		dsn := edgeDB(t)
		tally, bad := doubleSpendRounds(t, dsn, rounds)
		t.Logf("раундов %d · решений %d · %s · раундов не с одним пропуском %d", rounds, 2*rounds, tally, bad)
		if bad != 0 || tally.pass.Load() != rounds || tally.challenge.Load() != rounds {
			t.Fatalf("доказательство пропущено не ровно один раз: %s, раундов-нарушителей %d", tally, bad)
		}
		if got := scalar[int](t, dsn, `SELECT count(*) FROM kacho_gateway.pow_spent`); got != rounds {
			t.Fatalf("пометок pow_spent %d, ожидалось %d (по одной на доказательство)", got, rounds)
		}
	})
	t.Run("контроль: копия миграции без порчи", func(t *testing.T) {
		tally, bad := doubleSpendRounds(t, buildLimiterSchemaFromCopy(t, nil), rounds)
		t.Logf("%s · раундов-нарушителей %d", tally, bad)
		if bad != 0 || tally.pass.Load() != rounds {
			t.Fatalf("контроль сборки из копии: %s, раундов-нарушителей %d", tally, bad)
		}
	})
	t.Run("близнец: pow_spent без первичного ключа", func(t *testing.T) {
		tally, bad := doubleSpendRounds(t, buildLimiterSchemaFromCopy(t, &spoilPowSpentPK), rounds)
		t.Logf("%s · раундов-нарушителей %d", tally, bad)
		if bad != rounds || tally.pass.Load() != 0 || tally.unavailable.Load() != 2*rounds {
			t.Fatalf("без первичного ключа ожидался отказ хранилища на каждом решении (ни одного пропуска): %s", tally)
		}
	})
}

// ── O2 ──────────────────────────────────────────────────────────────────────

// bucketRace — n одновременных решений с n источников в n разных подсетях /24
// (блокировки ключей не пересекаются — сериализует только строка ведра) при
// BURST = k и одном моменте (ведро не пополняется между решениями).
func bucketRace(t *testing.T, dsn string, n, k int) *outcomeTally {
	t.Helper()
	l := orderLimits()
	l.Global.Burst = k
	stores := twoReplicas(t, dsn, l)
	now := dbMoment()
	reqs := make([]Request, n)
	for i := range reqs {
		reqs[i] = Request{Keys: mustKeys(t, v4(3, i, 1)), Now: now}
	}
	return raceDecisions(stores, reqs)
}

// TestPg_O2_BucketRaceAcrossTwoReplicasPassesExactlyBurst — N одновременных
// решений с открытой ступени (каждый источник ниже своего FREE) при ведре
// BURST = k: пропущено ровно k (по жетону на пропуск), остальные — вызов
// базовой сложности; ведро после гонки — 0, не меньше. Держит строка ведра
// FOR UPDATE: каждое решение читает остаток, зафиксированный предыдущим.
func TestPg_O2_BucketRaceAcrossTwoReplicasPassesExactlyBurst(t *testing.T) {
	const n, k = 16, 5
	dsn := edgeDB(t)
	tally := bucketRace(t, dsn, n, k)
	tokens := scalar[float64](t, dsn, `SELECT tokens FROM kacho_gateway.anon_mail_bucket WHERE id = 1`)
	t.Logf("решений %d при BURST=%d на двух репликах · %s · жетонов после %v", n, k, tally, tokens)
	if tally.pass.Load() != k || tally.challenge.Load() != n-k {
		t.Fatalf("пропущено %d при BURST=%d, ожидалось ровно %d (%s)", tally.pass.Load(), k, k, tally)
	}
	if tokens != 0 {
		t.Fatalf("жетонов после гонки %v, ожидалось 0", tokens)
	}
}

// bucketDecrements — n одновременных списаний жетона прямым оператором
// (`tokens = tokens - 1`, каждое — своя фиксация) с ведра в k жетонов.
// Возвращает число принятых, число отказов 23514 и остаток.
func bucketDecrements(t *testing.T, dsn string, n, k int) (ok, check23514 int64, left float64) {
	t.Helper()
	pool := rawPool(t, dsn, int32(n)) // #nosec G115 -- n пробы мал
	if _, err := pool.Exec(context.Background(),
		`UPDATE kacho_gateway.anon_mail_bucket SET tokens = $1 WHERE id = 1`, float64(k)); err != nil {
		t.Fatal(err)
	}
	var (
		wg        sync.WaitGroup
		okN, chkN atomic.Int64
		otherErr  atomic.Value
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := pool.Exec(context.Background(),
				`UPDATE kacho_gateway.anon_mail_bucket SET tokens = tokens - 1 WHERE id = 1`)
			var pe *pgconn.PgError
			switch {
			case err == nil:
				okN.Add(1)
			case errors.As(err, &pe) && pe.Code == "23514":
				chkN.Add(1)
			default:
				otherErr.Store(err.Error())
			}
		}()
	}
	close(start)
	wg.Wait()
	if e := otherErr.Load(); e != nil {
		t.Fatalf("списание жетона: отказ вне 23514: %v", e)
	}
	return okN.Load(), chkN.Load(), scalar[float64](t, dsn, `SELECT tokens FROM kacho_gateway.anon_mail_bucket WHERE id = 1`)
}

// TestPg_O2_BucketCheckKeepsTokensNonNegative — CHECK (tokens >= 0) строки
// ведра: из N одновременных списаний с ведра в k жетонов база принимает ровно
// k, остальные отвергает 23514, и остаток — 0. Близнец — копия миграции без
// этого CHECK (один факт): списания принимаются все, остаток уходит в минус.
// Контроль — та же сборка из копии без порчи.
func TestPg_O2_BucketCheckKeepsTokensNonNegative(t *testing.T) {
	const n, k = 12, 5
	for _, c := range []struct {
		name  string
		dsn   func(t *testing.T) string
		check bool
	}{
		{"цепочка края", func(t *testing.T) string { return edgeDB(t) }, true},
		{"контроль: копия миграции без порчи", func(t *testing.T) string { return buildLimiterSchemaFromCopy(t, nil) }, true},
		{"близнец: ведро без CHECK (tokens >= 0)", func(t *testing.T) string { return buildLimiterSchemaFromCopy(t, &spoilBucketCheck) }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			ok, chk, left := bucketDecrements(t, c.dsn(t), n, k)
			t.Logf("списаний %d с %d жетонов · принято %d · 23514 %d · остаток %v", n, k, ok, chk, left)
			if c.check {
				if ok != k || chk != n-k || left != 0 {
					t.Fatalf("CHECK есть: принято %d (ожидалось %d), 23514 %d (ожидалось %d), остаток %v (ожидался 0)", ok, k, chk, n-k, left)
				}
				return
			}
			if ok != n || left != float64(k-n) {
				t.Fatalf("близнец без CHECK: принято %d, остаток %v — ожидалось %d и %d", ok, left, n, k-n)
			}
		})
	}
}

// ── O3 ──────────────────────────────────────────────────────────────────────

// subnetThresholdRace — подсеть 10.4.<net>.0/24 с счётом threshold−1
// (последовательные пропуски с разных источников этой подсети), затем n
// одновременных решений с n новых источников той же подсети на двух репликах.
// Источники различны — общий у решений только ключ подсети.
func subnetThresholdRace(t *testing.T, dsn string, l config.AnonMailLimits, net, threshold, n int) (*outcomeTally, int) {
	t.Helper()
	stores := twoReplicas(t, dsn, l)
	now := dbMoment()
	for i := 0; i < threshold-1; i++ {
		if v := stores[i%2].Decide(context.Background(), Request{Keys: mustKeys(t, v4(4, net, 1+i)), Now: now}); v.Outcome != Pass {
			t.Fatalf("посев счёта подсети: решение %d — %s, ожидался пропуск", i, v.Outcome)
		}
	}
	reqs := make([]Request, n)
	for i := range reqs {
		reqs[i] = Request{Keys: mustKeys(t, v4(4, net, 100+i)), Now: now}
	}
	tally := raceDecisions(stores, reqs)
	subnet := mustKeys(t, v4(4, net, 1)).Subnets[0].Key
	return tally, scalar[int](t, dsn, `SELECT count(*) FROM kacho_gateway.anon_mail_passes WHERE key = $1`, subnet)
}

// TestPg_O3_SubnetThresholdRaceAcrossTwoReplicasPassesExactlyOne — ось
// подсети на пороге: счёт /24 на единицу ниже порога, N одновременных
// решений с разных источников этой подсети через две реплики — пропущено ровно
// одно, счёт подсети ровно порог; остальные — вызов (порог Ps) либо отказ
// (порог Hs). Ведро не вмешивается (BURST выше числа решений).
func TestPg_O3_SubnetThresholdRaceAcrossTwoReplicasPassesExactlyOne(t *testing.T) {
	const n, threshold = 12, 4
	for i, c := range []struct {
		name   string
		limits config.AnonMailSubnetLimits
		rest   func(*outcomeTally) int64
	}{
		{"порог Ps — вызов", config.AnonMailSubnetLimits{PoW: threshold, Hard: 40}, func(o *outcomeTally) int64 { return o.challenge.Load() }},
		{"порог Hs — отказ", config.AnonMailSubnetLimits{PoW: threshold, Hard: threshold}, func(o *outcomeTally) int64 { return o.reject.Load() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := orderLimits()
			l.SubnetV4Len24 = c.limits
			dsn := edgeDB(t)
			tally, subnetCount := subnetThresholdRace(t, dsn, l, i, threshold, n)
			t.Logf("решений %d на пороге %d · %s · счёт подсети после %d", n, threshold, tally, subnetCount)
			if tally.pass.Load() != 1 || c.rest(tally) != n-1 {
				t.Fatalf("на пороге подсети пропущено %d, ожидался ровно один (%s)", tally.pass.Load(), tally)
			}
			if subnetCount != threshold {
				t.Fatalf("счёт подсети %d, ожидался ровно порог %d", subnetCount, threshold)
			}
		})
	}
}

// dbMoment — момент решения с точностью timestamptz (микросекунды): момент
// ведра, записанный базой, равен моменту следующего решения, и ведро между
// решениями одной гонки не пополняется на остаток наносекунд.
func dbMoment() time.Time { return time.Now().Truncate(time.Microsecond) }
