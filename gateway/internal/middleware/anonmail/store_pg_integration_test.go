// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// TestPg_CX2_11_RaceOnANewKeyAcrossTwoReplicasPassesExactlyOne — N
// одновременных запросов с нового ключа при FREE = 1 на ДВУХ репликах (у
// каждой свой пул, общее — только база) → пропущен ровно один: у ключа без
// строк блокируется ключ, а не строка (CX2-11).
func TestPg_CX2_11_RaceOnANewKeyAcrossTwoReplicasPassesExactlyOne(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	l.Source.Free = 1
	a, _, _ := pgRig(t, dsn, l)
	b, _, _ := pgRig(t, dsn, l)
	const n = 24
	var ok, other atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		r := a
		if i%2 == 1 {
			r = b
		}
		go func() {
			defer wg.Done()
			<-start
			switch rec := r.send(pathRecovery, "198.51.100.50", "", ""); rec.Code {
			case http.StatusOK:
				ok.Add(1)
			case http.StatusTooManyRequests:
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	t.Logf("запросов %d на двух репликах · пропущено %d · вне {200, 429} %d", n, ok.Load(), other.Load())
	if ok.Load() != 1 {
		t.Fatalf("пропущено %d, ожидался ровно один", ok.Load())
	}
}

// TestPg_CX2_11_KeySpaceOfTheLockDoesNotMeetTheSchemaLock — пространство ключей
// блокировки (CX2-11, условие пересверки): соседнее соединение держит
// `pg_advisory_lock(schemaLockID)` одним bigint, а свёртка ключа звена подменена
// так, что obj — младшие 32 бита schemaLockID, а class — старшие. Форма с двумя
// int4 живёт в другом пространстве ключей — Admit отвечает в пределах
// lock_timeout. Близнец-инъекция: та же подмена в форме «один bigint» → 503.
func TestPg_CX2_11_KeySpaceOfTheLockDoesNotMeetTheSchemaLock(t *testing.T) {
	const schemaLockID int64 = 694_0001 // idempotencypg.schemaLockID
	dsn := edgeDB(t)
	holder, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close(context.Background()) }()
	if _, err := holder.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, schemaLockID); err != nil {
		t.Fatal(err)
	}
	fold := func(string) lockPair {
		return lockPair{class: int32(schemaLockID >> 32), obj: int32(schemaLockID & 0xffffffff)} // #nosec G115 -- проба пространства ключей
	}
	run := func(oneBigint bool) (int, time.Duration) {
		l := testLimits()
		r, ps, _ := pgRig(t, dsn, l)
		ps.store.fold = fold
		if oneBigint {
			ps.b.lockSQL = `SELECT pg_advisory_xact_lock(($1::bigint << 32) | ($2::bigint & 4294967295))`
		}
		start := time.Now()
		rec := r.send(pathRecovery, "198.51.100.51", "", "")
		return rec.Code, time.Since(start)
	}
	code, took := run(false)
	if code != http.StatusOK {
		t.Fatalf("форма с двумя int4 при удержанной блокировке схемы: %d за %s, ожидался пропуск", code, took)
	}
	tw, twTook := run(true)
	if tw != http.StatusServiceUnavailable {
		t.Fatalf("близнец «один bigint»: %d за %s — ожидался 503 (столкновение с блокировкой схемы)", tw, twTook)
	}
	t.Logf("два int4: %d за %s · один bigint: %d за %s", code, took, tw, twTook)
}

// TestPg_CX2_28_LockWaitBeyondLockTimeoutIsStoreUnavailable — ожидание
// блокировки ключа дольше lock_timeout → StoreUnavailable → 503 (третий повод
// 503, CX2-28 (б)); близнец — блокировка не удержана: пропуск.
func TestPg_CX2_28_LockWaitBeyondLockTimeoutIsStoreUnavailable(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	r, _, logs := pgRig(t, dsn, l)
	keys, _ := KeysFor("198.51.100.52")
	p := keyLockPair(keys.Source)
	holder, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close(context.Background()) }()
	tx, err := holder.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `SELECT pg_advisory_xact_lock($1::int4, $2::int4)`, p.class, p.obj); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	rec := r.send(pathRecovery, "198.51.100.52", "", "")
	took := time.Since(start)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ключ удержан: %d, ожидался 503", rec.Code)
	}
	if took > anonMailStoreWait+cancelCost+500*time.Millisecond {
		t.Errorf("503 через %s — ожидание не ограничено lock_timeout", took)
	}
	// CX2-93 (а): 55P03 на блокировке КЛЮЧА — не ожидание строки ведра:
	// растёт только счётчик недоступности. Счётчик по коду 55P03 здесь красный.
	if s := r.gate.Stats(); s.BucketWaitTimeouts != 0 || s.StoreUnavailable != 1 {
		t.Errorf("ключ удержан: счётчики %+v, ожидалось ожиданий ведра 0, недоступности 1", s)
	}
	// GS-E2-10: префикс пакета в тексте ошибки — один раз.
	for _, ln := range strings.Split(logs.String(), "\n") {
		if strings.Contains(ln, "step="+stepLock) && strings.Count(ln, "anonmail:") != 1 {
			t.Errorf("префикс «anonmail:» в строке журнала не один: %q", ln)
		}
	}
	_ = tx.Rollback(context.Background())
	if rec := r.send(pathRecovery, "198.51.100.52", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("близнец — ключ свободен: %d", rec.Code)
	}
	t.Logf("ключ удержан: 503 за %s · счётчик %d", took, r.gate.Stats().StoreUnavailable)
}

// TestPg_NTF2_59_StoreDownEveryRequestIs503AndTheProofIsNotSpent — сценарий 59
// на хранилище postgres за посредником пробы.
func TestPg_NTF2_59_StoreDownEveryRequestIs503AndTheProofIsNotSpent(t *testing.T) {
	run := func(t *testing.T, down bool) {
		dsn := edgeDB(t)
		proxy, viaProxy := newPGProxy(t, dsn)
		l := testLimits()
		r, _, _ := pgRig(t, viaProxy, l)
		s6, s4 := "198.51.100.66", "198.51.100.64"
		for i := 0; i < l.Source.Free; i++ {
			if rec := r.send(pathRecovery, s6, "", ""); rec.Code != http.StatusOK {
				t.Fatalf("построение S6: %d", rec.Code)
			}
		}
		tokC, bitsC := challengeOf(t, r.send(pathRecovery, s6, "", ""))
		proofC := tokC + ":" + solve(t, tokC, bitsC)
		laneBefore := r.lane.calls.Load()
		if down {
			proxy.set(func(p *pgProxy) { p.refuse = true })
			proxy.dropAll()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, err := pgx.Connect(ctx, viaProxy)
			cancel()
			if err == nil {
				t.Fatal("хранилище «остановлено», а соединение с ним установлено — условие не создано")
			}
			t.Logf("хранилище остановлено: соединение отвергнуто (%v)", err)
		}
		a := r.send(pathRecovery, s4, "", `{"email":"a@example.test"}`)
		b := r.send(pathRecovery, s4, "", `{"email":"z@example.test"}`)
		c := r.send(pathRegister, s4, "", `{"email":"z@example.test","password":"Valid-Passw0rd!"}`)
		g := r.send(pathRecovery, s6, proofC, `{"email":"z@example.test"}`)
		if !down {
			for name, rec := range map[string]*httptest.ResponseRecorder{"а": a, "б": b, "в": c, "г": g} {
				if rec.Code != http.StatusOK {
					t.Errorf("близнец (%s): %d, ожидалось 200", name, rec.Code)
				}
			}
			if a.Body.String() != b.Body.String() {
				t.Error("близнец: ответы (а) и (б) различаются")
			}
			if r.gate.Stats().StoreUnavailable != 0 {
				t.Error("близнец: метрика недоступности сдвинулась")
			}
			return
		}
		for name, rec := range map[string]*httptest.ResponseRecorder{"а": a, "б": b, "в": c, "г": g} {
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("(%s): %d, ожидалось 503", name, rec.Code)
				continue
			}
			bd := parseStatus(t, rec)
			if bd.Code != 14 || bd.Message != "request limiter is unavailable" || len(bd.Details) != 0 {
				t.Errorf("(%s): тело %s", name, rec.Body.String())
			}
		}
		if a.Body.String() != b.Body.String() || a.Body.String() != c.Body.String() {
			t.Errorf("ответы различаются: %q · %q · %q", a.Body, b.Body, c.Body)
		}
		if got := r.lane.calls.Load() - laneBefore; got != 0 {
			t.Errorf("полоса получила %d из четырёх запросов", got)
		}
		if got := r.gate.Stats().StoreUnavailable; got != 4 {
			t.Errorf("метрика недоступности %d, ожидалось 4", got)
		}
		// Д66: соединение отвергнуто — сбой, а не насыщение.
		if got := r.gate.Stats().BucketWaitTimeouts; got != 0 {
			t.Errorf("хранилище остановлено: метрика насыщения %d, ожидалось 0", got)
		}
		// (д) хранилище поднято, часы стоят.
		proxy.set(func(p *pgProxy) { p.refuse = false })
		time.Sleep(1100 * time.Millisecond) // пул проверяет соединение, простоявшее дольше секунды
		if rec := r.send(pathRecovery, s6, proofC, `{"email":"z@example.test"}`); rec.Code != http.StatusOK {
			t.Fatalf("(д) решение C после восстановления: %d %s — 503 истратил вызов", rec.Code, rec.Body.String())
		}
	}
	t.Run("хранилище остановлено", func(t *testing.T) { run(t, true) })
	t.Run("близнец: хранилище исправно", func(t *testing.T) { run(t, false) })
}

// TestPg_CX2_43_MarkWrittenBucketNotObtainedIs503AndRepeatIsFresh — CX2-43 на
// postgres: пометка вставлена, строку ведра держит соседняя транзакция пробы
// дольше anonMailStoreWait → 503; после освобождения повтор того же решения →
// Fresh и пропуск. Близнец-инъекция: пометка вынесена в отдельную
// зафиксированную транзакцию до решения → повтор Replayed и новый вызов.
func TestPg_CX2_43_MarkWrittenBucketNotObtainedIs503AndRepeatIsFresh(t *testing.T) {
	run := func(t *testing.T, markApart bool) (int, int) {
		dsn := edgeDB(t)
		l := testLimits()
		r, ps, _ := pgRig(t, dsn, l)
		src := "198.51.100.43"
		for i := 0; i < l.Source.Free; i++ {
			r.send(pathRecovery, src, "", "")
		}
		tok, bits := challengeOf(t, r.send(pathRecovery, src, "", ""))
		proof := tok + ":" + solve(t, tok, bits)
		if markApart {
			p, err := r.pow.Verify(proof)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ps.b.pool.Exec(context.Background(),
				`INSERT INTO kacho_gateway.pow_spent (id, expires_at) VALUES ($1, $2)`, p.ID[:], p.ExpiresAt); err != nil {
				t.Fatal(err)
			}
		}
		holder, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Close(context.Background()) }()
		tx, err := holder.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(context.Background(), `SELECT 1 FROM kacho_gateway.anon_mail_bucket WHERE id = 1 FOR UPDATE`); err != nil {
			t.Fatal(err)
		}
		held := r.send(pathRecovery, src, proof, "")
		// CX2-93 (а): строка ведра не взята за lock_timeout — ожидание ведра:
		// растут оба счётчика.
		if s := r.gate.Stats(); s.BucketWaitTimeouts != 1 || s.StoreUnavailable != 1 {
			t.Errorf("строка ведра удержана: счётчики %+v, ожидалось 1 и 1", s)
		}
		_ = tx.Rollback(context.Background())
		again := r.send(pathRecovery, src, proof, "")
		return held.Code, again.Code
	}
	held, again := run(t, false)
	if held != http.StatusServiceUnavailable || again != http.StatusOK {
		t.Fatalf("одна транзакция: %d → %d, ожидались 503 → 200", held, again)
	}
	tHeld, tAgain := run(t, true)
	if tHeld != http.StatusServiceUnavailable || tAgain != http.StatusTooManyRequests {
		t.Fatalf("близнец «пометка отдельно»: %d → %d, ожидались 503 → 429 (проба различает неделимость)", tHeld, tAgain)
	}
	t.Logf("одна транзакция: %d → %d · близнец: %d → %d", held, again, tHeld, tAgain)
}

// TestPg_CX2_44_ExhaustedLimiterPoolDoesNotTouchIdempotency — свой пул
// ограничителя (CX2-44): пул исчерпан соединениями, удержанными пробой →
// анонимный запрос получает 503 в пределах anonMailStoreWait, а мутация с
// Idempotency-Key в тот же момент исполняется. Близнец-инъекция — ограничитель
// на пуле однократности: та же мутация получает отказ — красный.
func TestPg_CX2_44_ExhaustedLimiterPoolDoesNotTouchIdempotency(t *testing.T) {
	run := func(t *testing.T, shared bool) (anon int, anonTook time.Duration, mutation int) {
		dsn := edgeDB(t)
		l := testLimits()
		idemPool := rawPool(t, dsn, 6)
		idem, err := idempotencypg.NewWithPool(context.Background(), idemPool, idempotencypg.Config{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = idem.Close() })
		var ps *PostgresStore
		r := newRig(t, l, func(*testClock) Store {
			if shared {
				ps = newPostgresStoreWithPool(idemPool, l, discardLogger(), false)
			} else {
				ps = newPgStore(t, dsn, l, discardLogger())
			}
			return ps
		}, 0)
		hold := ps.b.pool.Config().MaxConns
		var held []interface{ Release() }
		for i := int32(0); i < hold; i++ {
			c, err := ps.b.pool.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, c)
		}
		defer func() {
			for _, c := range held {
				c.Release()
			}
		}()
		start := time.Now()
		rec := r.send(pathRecovery, "198.51.100.44", "", "")
		anonTook = time.Since(start)
		// CX2-93 (а): истёкший захват соединения — не ожидание строки ведра.
		if s := r.gate.Stats(); !shared && (s.StoreUnavailable != 1 || s.BucketWaitTimeouts != 0) {
			t.Errorf("пул исчерпан: счётчики %+v, ожидалось недоступности 1, ожиданий ведра 0", s)
		}
		mut := middleware.HTTPIdempotency(idem)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(`{}`)).WithContext(ctx)
		req.Header.Set("Idempotency-Key", "k-1")
		req.Header.Set("X-Kacho-Principal-Id", "usr-1")
		mrec := httptest.NewRecorder()
		mut.ServeHTTP(mrec, req)
		return rec.Code, anonTook, mrec.Code
	}
	anon, took, mut := run(t, false)
	if anon != http.StatusServiceUnavailable || took > anonMailStoreWait+200*time.Millisecond {
		t.Errorf("исчерпанный пул ограничителя: %d за %s, ожидался 503 в пределах %s", anon, took, anonMailStoreWait)
	}
	if mut != http.StatusOK {
		t.Errorf("мутация с Idempotency-Key при исчерпанном пуле ограничителя: %d, ожидалось 200", mut)
	}
	_, _, twMut := run(t, true)
	if twMut == http.StatusOK {
		t.Errorf("близнец «общий пул»: мутация исполнена — проба не отличила бы свой пул от общего")
	}
	t.Logf("свой пул: анонимный %d за %s, мутация %d · общий пул: мутация %d", anon, took, mut, twMut)
}

// TestPg_UK52_WarmUpTakesAllConnectionsAtOnce — прогрев (CX2-44 (3), УК52 (1)):
// после построения у пула anonMailPoolConns открытых соединений до первого
// запроса; одновременное взятие на пуле без MinConns открывает все, близнец
// «взять — отпустить по одному» — одно.
func TestPg_UK52_WarmUpTakesAllConnectionsAtOnce(t *testing.T) {
	dsn := edgeDB(t)
	ps := newPgStore(t, dsn, testLimits(), discardLogger())
	defer func() { _ = ps.Close() }()
	if got := ps.b.pool.Stat().TotalConns(); got != anonMailPoolConns {
		t.Errorf("после построения открыто %d, ожидалось %d", got, anonMailPoolConns)
	}
	measure := func(sequential bool) int32 {
		cfg, err := limiterPoolConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.MinConns = 0
		p, err := newLimiterPool(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		pgtest.ClosePoolAtEnd(t, p)
		if sequential {
			for i := 0; i < anonMailPoolConns; i++ {
				c, err := p.Acquire(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				c.Release()
			}
		} else if err := warmUp(context.Background(), p, anonMailPoolConns); err != nil {
			t.Fatal(err)
		}
		return p.Stat().TotalConns()
	}
	at, seq := measure(false), measure(true)
	if at != anonMailPoolConns {
		t.Errorf("одновременный прогрев открыл %d, ожидалось %d", at, anonMailPoolConns)
	}
	if seq != 1 {
		t.Errorf("близнец «по одному» открыл %d, ожидалось 1 (иначе проба не отличила бы прогрев от его отсутствия)", seq)
	}
	t.Logf("после построения %d · одновременный прогрев %d · по одному %d", ps.b.pool.Stat().TotalConns(), at, seq)
}

// TestPg_UK45_ColdPoolFirstRequestIs503WithoutWarmUp — близнец-инъекция
// прогрева: MinConns = 0 без прогрева — первый запрос на базе с задержкой
// установки соединения больше anonMailStoreWait получает 503; прогретый пул на
// той же базе — пропуск.
func TestPg_UK45_ColdPoolFirstRequestIs503WithoutWarmUp(t *testing.T) {
	dsn := edgeDB(t)
	proxy, viaProxy := newPGProxy(t, dsn)
	l := testLimits()
	warm, _, _ := pgRig(t, viaProxy, l)
	proxy.set(func(p *pgProxy) { p.acceptDelay = anonMailStoreWait + 150*time.Millisecond })
	if rec := warm.send(pathRecovery, "198.51.100.45", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("прогретый пул на медленной установке: %d", rec.Code)
	}
	cfg, err := limiterPoolConfig(viaProxy)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MinConns = 0
	pool, err := newLimiterPool(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	cold := newRig(t, l, func(*testClock) Store { return newPostgresStoreWithPool(pool, l, discardLogger(), true) }, 0)
	if rec := cold.send(pathRecovery, "198.51.100.46", "", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("близнец «холодный пул»: %d, ожидался 503 — проба не отличила бы прогрев", rec.Code)
	}
}

// TestPg_K1_VanishedHolderIsReleasedByTheServer — «исчезнувший держатель» (К1
// ревью замысла): соседнее соединение открывает транзакцию решения через
// beginDecision (с её SET LOCAL), берёт строку ведра и замолкает. Запрос внутри
// anonMailDecisionBudget — 503; после срока и запаса — пропуск; держатель
// получает 25P03. Близнец без idle_in_transaction_session_timeout — 503 после
// того же срока.
func TestPg_K1_VanishedHolderIsReleasedByTheServer(t *testing.T) {
	run := func(t *testing.T, limits string) (inside, after int, holderErr error) {
		dsn := edgeDB(t)
		l := testLimits()
		r, _, _ := pgRig(t, dsn, l)
		holderStore := newPgStore(t, dsn, l, discardLogger())
		defer func() { _ = holderStore.Close() }()
		holderStore.b.limitsSQL = limits
		ctx := context.Background()
		x, err := holderStore.b.beginDecision(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := x.bucket(ctx); err != nil {
			t.Fatal(err)
		}
		inside = r.send(pathRecovery, "198.51.100.47", "", "").Code
		time.Sleep(anonMailDecisionBudget + 500*time.Millisecond)
		after = r.send(pathRecovery, "198.51.100.48", "", "").Code
		_, holderErr = x.tx.Exec(ctx, `SELECT 1`)
		x.rollback(context.Background())
		return inside, after, holderErr
	}
	in, after, herr := run(t, decisionLimitsSQL)
	var pgErr *pgconn.PgError
	code := ""
	if errors.As(herr, &pgErr) {
		code = pgErr.Code
	}
	t.Logf("держатель молчит: внутри срока %d · после %d · код завершения держателя %q (%v)", in, after, code, herr)
	if in != http.StatusServiceUnavailable || after != http.StatusOK {
		t.Fatalf("внутри срока %d, после %d — ожидались 503 и пропуск", in, after)
	}
	if code != "25P03" {
		t.Errorf("держатель завершён с кодом %q, ожидался 25P03", code)
	}
	_, twAfter, _ := run(t, decisionLimitsSQLFor(anonMailDecisionBudget, anonMailStoreWait, limitIdleInTransaction))
	if twAfter != http.StatusServiceUnavailable {
		t.Fatalf("близнец без idle_in_transaction_session_timeout: после срока %d, ожидался 503", twAfter)
	}
}

// TestPg_K1_HolderInALongStatementIsReleasedWithinTwoBudgets — второй вариант:
// держатель в долгом операторе (pg_sleep после взятия ведра) — пропуск после
// 2 × anonMailDecisionBudget; близнец без statement_timeout — 503.
func TestPg_K1_HolderInALongStatementIsReleasedWithinTwoBudgets(t *testing.T) {
	run := func(t *testing.T, limits string) int {
		dsn := edgeDB(t)
		l := testLimits()
		r, _, _ := pgRig(t, dsn, l)
		holderStore := newPgStore(t, dsn, l, discardLogger())
		defer func() { _ = holderStore.Close() }()
		holderStore.b.limitsSQL = limits
		ctx := context.Background()
		x, err := holderStore.b.beginDecision(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := x.bucket(ctx); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _, _ = x.tx.Exec(ctx, `SELECT pg_sleep(5)`); close(done) }()
		time.Sleep(2*anonMailDecisionBudget + 500*time.Millisecond)
		code := r.send(pathRecovery, "198.51.100.49", "", "").Code
		<-done
		x.rollback(context.Background())
		return code
	}
	if got := run(t, decisionLimitsSQL); got != http.StatusOK {
		t.Fatalf("после 2 × срока решения: %d, ожидался пропуск", got)
	}
	if got := run(t, decisionLimitsSQLFor(anonMailDecisionBudget, anonMailStoreWait, limitStatement)); got != http.StatusServiceUnavailable {
		t.Fatalf("близнец без statement_timeout: %d, ожидался 503", got)
	}
}

// TestPg_UK54_AfterADeadlineThePoolKeepsAllConnections — обработчик отмены
// задан явно (УК54): после срыва срока решения (заглушка оператора дольше
// anonMailDecisionBudget) открытых соединений пула — anonMailPoolConns, и
// следующее решение берёт прогретое. Близнец-инъекция — обработчик pgx по
// умолчанию: соединение закрыто клиентом, открытых на одно меньше.
func TestPg_UK54_AfterADeadlineThePoolKeepsAllConnections(t *testing.T) {
	stub := "SET LOCAL lock_timeout = 0; SELECT pg_sleep(5)"
	run := func(t *testing.T, defaultHandler bool) (int, int32, time.Duration) {
		dsn := edgeDB(t)
		l := testLimits()
		cfg, err := limiterPoolConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if defaultHandler {
			def, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.BuildContextWatcherHandler = def.BuildContextWatcherHandler
		}
		pool, err := newLimiterPool(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := warmUp(context.Background(), pool, anonMailPoolConns); err != nil {
			t.Fatal(err)
		}
		var ps *PostgresStore
		r := newRig(t, l, func(*testClock) Store { ps = newPostgresStoreWithPool(pool, l, discardLogger(), true); return ps }, 0)
		ps.b.limitsSQL = stub
		start := time.Now()
		code := r.send(pathRecovery, "198.51.100.54", "", "").Code
		took := time.Since(start)
		// Живые соединения — свободные и взятые; уничтожаемое (закрытое
		// клиентом) в счёт не входит, хотя TotalConns его ещё держит.
		// Возврат соединения в пул (и уничтожение закрытого) идёт в фоне
		// pgxpool: ждём, пока взятых не останется, но не дольше 400 мс — раньше
		// проверки здоровья (500 мс), которая пополнила бы пул до MinConns.
		for wait := time.Now().Add(400 * time.Millisecond); pool.Stat().AcquiredConns() > 0 && time.Now().Before(wait); {
			time.Sleep(10 * time.Millisecond)
		}
		if !defaultHandler {
			// Следующее решение берёт прогретое соединение: новых не строится.
			ps.b.limitsSQL = decisionLimitsSQL
			before := pool.Stat().NewConnsCount()
			if c := r.send(pathRecovery, "198.51.100.55", "", "").Code; c != http.StatusOK || pool.Stat().NewConnsCount() != before {
				t.Errorf("следующее решение: %d, построено новых соединений %d", c, pool.Stat().NewConnsCount()-before)
			}
		}
		st := pool.Stat()
		t.Logf("пул после срыва: total %d idle %d acquired %d constructing %d new %d maxLifetimeDestroy %d idleDestroy %d",
			st.TotalConns(), st.IdleConns(), st.AcquiredConns(), st.ConstructingConns(), st.NewConnsCount(),
			st.MaxLifetimeDestroyCount(), st.MaxIdleDestroyCount())
		return code, st.IdleConns() + st.AcquiredConns(), took
	}
	code, open, took := run(t, false)
	t.Logf("CancelRequest: %d за %s · открыто %d", code, took, open)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("оператор дольше срока: %d, ожидался 503", code)
	}
	if took > anonMailDecisionBudget+cancelCost+anonMailStoreWait+200*time.Millisecond {
		t.Errorf("ответ через %s — дольше срока решения с допуском отмены", took)
	}
	if open != anonMailPoolConns {
		t.Errorf("после срыва срока открыто %d, ожидалось %d", open, anonMailPoolConns)
	}
	_, twOpen, _ := run(t, true)
	if twOpen != anonMailPoolConns-1 {
		t.Errorf("близнец «обработчик по умолчанию»: открыто %d, ожидалось %d — проба не отличила бы обработчик", twOpen, anonMailPoolConns-1)
	}
}
