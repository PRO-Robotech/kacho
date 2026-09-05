// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// store_integration_test.go — однократность держится ФЛОТОМ, а не процессом (#694).
//
// Каждая проба здесь строит ДВЕ И БОЛЕЕ НЕЗАВИСИМЫХ реплики: отдельный экземпляр
// хранилища, отдельный пул, отдельная середина. Общего у них ровно одно — база.
// Именно так устроен флот подов, и именно это отличает настоящую проверку от
// той, что делит один объект в памяти между двумя обработчиками: последняя
// зелена и на хранилище в памяти процесса, то есть не утверждает ничего.
package idempotencypg_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// replica — одна реплика края: своё хранилище поверх общей базы.
func replica(t *testing.T, dsn string, cfg idempotencypg.Config) *idempotencypg.Store {
	t.Helper()
	cfg.DSN = dsn
	s, err := idempotencypg.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("построение реплики: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// post гонит один mutating-запрос с ключом через середину реплики.
func post(h http.Handler, principal, path, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("X-Kacho-Principal-Id", principal)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// TestSharedStore_SecondReplicaReplaysAndDoesNotCallDownstream — ПРЕДИКАТ ЗАДАЧИ
// #694 (а): повтор с тем же ключом, поданный ДРУГОЙ реплике, отдаёт сохранённый
// ответ и не зовёт обработчик.
func TestSharedStore_SecondReplicaReplaysAndDoesNotCallDownstream(t *testing.T) {
	dsn := pgtest.NewDB(t)

	var calls int64
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"op":"op-` + strconv.FormatInt(n, 10) + `"}`))
	})

	podA := middleware.HTTPIdempotency(replica(t, dsn, idempotencypg.Config{}))(downstream)
	podB := middleware.HTTPIdempotency(replica(t, dsn, idempotencypg.Config{}))(downstream)

	first := post(podA, "user-A", "/compute/v1/instances", "same-key")
	if first.Code != http.StatusOK {
		t.Fatalf("первая реплика ответила %d, ожидалось 200", first.Code)
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("обработчик вызван %d раз(а) на первом запросе, ожидался 1", got)
	}

	second := post(podB, "user-A", "/compute/v1/instances", "same-key")
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("повтор, попавший в ДРУГУЮ реплику, дошёл до обработчика: вызовов %d, ожидался 1.\n"+
			"Это и есть предмет #694: домен параллелизма защиты обязан совпадать с флотом.", got)
	}
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("вторая реплика ответила иначе: %d %q против %d %q",
			second.Code, second.Body.String(), first.Code, first.Body.String())
	}
	if second.Header().Get("X-Idempotent-Replayed") != "true" {
		t.Fatalf("вторая реплика не пометила ответ как повторённый: заголовки %v", second.Header())
	}
}

// TestSharedStore_ConcurrentSameKeyAcrossReplicas_SingleDownstream — два и более
// ОДНОВРЕМЕННЫХ предъявления одного ключа в РАЗНЫЕ реплики: проходит ровно одно.
//
// Детерминированно, без time.Sleep: держатель удерживается в обработчике до тех
// пор, пока его вход не подтверждён, поэтому сломанный допуск обязан впустить
// второго — и второй будет посчитан, а не проскочит незамеченным.
func TestSharedStore_ConcurrentSameKeyAcrossReplicas_SingleDownstream(t *testing.T) {
	dsn := pgtest.NewDB(t)

	const replicas = 8
	var calls int64
	start := make(chan struct{})
	release := make(chan struct{})
	entered := make(chan struct{}, replicas)

	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		n := atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"op":"op-` + strconv.FormatInt(n, 10) + `"}`))
	})

	handlers := make([]http.Handler, replicas)
	for i := range handlers {
		handlers[i] = middleware.HTTPIdempotency(
			replica(t, dsn, idempotencypg.Config{PollInterval: 10 * time.Millisecond}),
		)(downstream)
	}

	var wg sync.WaitGroup
	codes := make([]int, replicas)
	bodies := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			rr := post(handlers[idx], "user-A", "/vpc/v1/networks", "contested-key")
			codes[idx] = rr.Code
			bodies[idx] = rr.Body.String()
		}(i)
	}
	close(start)
	// Держатель внутри обработчика; сломанный допуск впустит второго, и он тоже
	// будет удержан до подсчёта. Ожидание ограничено: сорванная фикстура обязана
	// НАЗВАТЬ себя, а не повиснуть — повисшая проба не даёт вердикта вовсе, а
	// его отсутствие читается как красное о предмете.
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		close(release)
		wg.Wait()
		t.Fatalf("ни одна реплика не вошла в обработчик за 30 с; ответы: %v — "+
			"предмет не проверен, фикстура сорвана", codes)
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("обработчик исполнен %d раз(а), ожидался ровно 1 — допуск не атомарен", got)
	}
	leader := -1
	for i := 0; i < replicas; i++ {
		if codes[i] == http.StatusOK {
			if leader == -1 {
				leader = i
			}
			if bodies[i] != bodies[leader] {
				t.Fatalf("реплика %d ответила %q, держатель — %q: исходы разошлись",
					i, bodies[i], bodies[leader])
			}
			continue
		}
		// Единственный законный иной исход — «ключ в работе»: ждущий не уложился
		// в бюджет. Исполнить downstream он не вправе — это было бы вторым
		// исполнением.
		if codes[i] != http.StatusConflict {
			t.Fatalf("реплика %d ответила %d; законны только 200 (повтор ответа) и 409 (ключ в работе)",
				i, codes[i])
		}
	}
	if leader == -1 {
		t.Fatal("ни одна реплика не получила ответ держателя")
	}
}

// TestReserveIsAtomic_ExactlyOneOwnerAcrossReplicas — тот же инвариант на уровне
// самого хранилища, без HTTP: N независимых реплик просят один ключ разом,
// держателем становится ровно одна.
//
// Проба существует отдельно от предыдущей потому, что предыдущая проверяет
// СЛЕДСТВИЕ (обработчик вызван один раз), а эта — ПРИЧИНУ (допуск неделим).
// Раздельные «посмотреть» и «записать» зелены на первой при малой нагрузке и
// красны на этой всегда.
func TestReserveIsAtomic_ExactlyOneOwnerAcrossReplicas(t *testing.T) {
	dsn := pgtest.NewDB(t)

	const replicas = 16
	stores := make([]*idempotencypg.Store, replicas)
	for i := range stores {
		stores[i] = replica(t, dsn, idempotencypg.Config{})
	}

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		mu      sync.Mutex
		owners  int
		waiters int
		other   []middleware.IdempotencyOutcome
	)
	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			res, err := stores[idx].Reserve(context.Background(), "one-key")
			if err != nil {
				t.Errorf("реплика %d: допуск отказал: %v", idx, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch res.Outcome {
			case middleware.IdempotencyOwn:
				owners++
			case middleware.IdempotencyWait:
				waiters++
			default:
				other = append(other, res.Outcome)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if owners != 1 {
		t.Fatalf("держателей %d, ожидался ровно 1 (ждущих %d, прочих %v)", owners, waiters, other)
	}
	if waiters != replicas-1 {
		t.Fatalf("ждущих %d, ожидалось %d (прочие исходы: %v)", waiters, replicas-1, other)
	}
}

// TestExpiredRecordIsReapedAndTheKeyBecomesClaimableAgain — погашение живёт до
// истечения ключа, потом запись убирается.
//
// Без сборщика хранилище растёт без границы: у ключа, предъявленного один раз,
// нет никого, кто пришёл бы его убрать. Проба утверждает ОБЕ стороны: запись
// исчезает И ключ снова становится свободным (а не остаётся навсегда занятым).
func TestExpiredRecordIsReapedAndTheKeyBecomesClaimableAgain(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()

	// TTL исчезающе мал: запись становится просроченной к следующему оператору.
	// Часы — серверные, поэтому ожидания на стороне пробы не требуется вовсе.
	//
	// Шаг уборки задан ЯВНО и заведомо больше жизни пробы: уборку здесь зовут
	// САМИ, и исход обязан принадлежать вызову, а не гонке с тикером. Шаг иначе
	// выводится из TTL (`ReapIntervalFor`), и при исчезающе малом TTL сборщик
	// унёс бы запись между `Commit` и переписью — проба падала бы на своей
	// фикстуре, а не на предмете.
	s := replica(t, dsn, idempotencypg.Config{TTL: time.Nanosecond, ReapInterval: time.Hour})

	res, err := s.Reserve(ctx, "short-lived")
	if err != nil {
		t.Fatalf("допуск: %v", err)
	}
	if res.Outcome != middleware.IdempotencyOwn {
		t.Fatalf("первый предъявитель получил исход %v, ожидался «держатель»", res.Outcome)
	}
	s.Commit(ctx, res, middleware.IdempotencyRecord{
		StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(`{}`),
	}, true)

	if n, lerr := s.Len(ctx); lerr != nil || n != 1 {
		t.Fatalf("после записи в хранилище %d записей (err=%v), ожидалась 1", n, lerr)
	}
	sw, err := s.Reap(ctx)
	if err != nil {
		t.Fatalf("сборщик: %v", err)
	}
	if sw.Removed != 1 {
		t.Fatalf("сборщик унёс %d записей, ожидалась 1", sw.Removed)
	}
	if !sw.Drained {
		t.Fatalf("сборщик объявил себя недогнавшим, унеся весь хвост (%d)", sw.Removed)
	}
	if n, lerr := s.Len(ctx); lerr != nil || n != 0 {
		t.Fatalf("после сборки в хранилище %d записей (err=%v), ожидалось 0", n, lerr)
	}

	again, err := s.Reserve(ctx, "short-lived")
	if err != nil {
		t.Fatalf("повторный допуск: %v", err)
	}
	if again.Outcome != middleware.IdempotencyOwn {
		t.Fatalf("после истечения ключ остался занят: исход %v, ожидался «держатель»", again.Outcome)
	}
}

// TestUnfinishedLeaseOfADeadHolderIsTakenOver — держатель, умерший, не оставив
// исхода (упавший под), отдаёт ключ по истечении СРОКА БРОНИ, а не навсегда.
//
// Без этого одна смерть пода делала бы ключ непригодным до конца TTL — сутки
// отказа по ключу, которого никто не исполнил.
func TestUnfinishedLeaseOfADeadHolderIsTakenOver(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()

	dead := replica(t, dsn, idempotencypg.Config{LeaseTTL: time.Nanosecond})
	res, err := dead.Reserve(ctx, "orphaned")
	if err != nil {
		t.Fatalf("допуск: %v", err)
	}
	if res.Outcome != middleware.IdempotencyOwn {
		t.Fatalf("исход %v, ожидался «держатель»", res.Outcome)
	}
	// Под умер: ни Commit, ни Release не позвал никто.

	survivor := replica(t, dsn, idempotencypg.Config{})
	took, err := survivor.Reserve(ctx, "orphaned")
	if err != nil {
		t.Fatalf("перехват: %v", err)
	}
	if took.Outcome != middleware.IdempotencyOwn {
		t.Fatalf("бронь умершего держателя не перехвачена: исход %v", took.Outcome)
	}

	// И обратный контроль: умерший держатель, вернувшись, чужую бронь НЕ
	// перезаписывает — CAS по идентификатору брони.
	dead.Commit(ctx, res, middleware.IdempotencyRecord{
		StatusCode: http.StatusTeapot, ContentType: "text/plain", Body: []byte("stale"),
	}, true)
	after, err := survivor.Reserve(ctx, "orphaned")
	if err != nil {
		t.Fatalf("допуск после возвращения умершего: %v", err)
	}
	if after.Outcome == middleware.IdempotencyReplay && after.Record.StatusCode == http.StatusTeapot {
		t.Fatal("ответ умершего держателя перезаписал живую бронь — CAS по брони не действует")
	}
}

// TestLiveLeaseIsNotStolen — положительный контроль к перехвату: пока бронь жива,
// её НЕ перехватывают. Без этой пробы предыдущая была бы зелена и у хранилища,
// которое отдаёт ключ каждому просящему.
func TestLiveLeaseIsNotStolen(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()

	a := replica(t, dsn, idempotencypg.Config{})
	b := replica(t, dsn, idempotencypg.Config{})

	first, err := a.Reserve(ctx, "held")
	if err != nil {
		t.Fatalf("допуск: %v", err)
	}
	if first.Outcome != middleware.IdempotencyOwn {
		t.Fatalf("исход %v, ожидался «держатель»", first.Outcome)
	}
	second, err := b.Reserve(ctx, "held")
	if err != nil {
		t.Fatalf("второй допуск: %v", err)
	}
	if second.Outcome != middleware.IdempotencyWait {
		t.Fatalf("живая бронь перехвачена: исход %v, ожидалось «ждать»", second.Outcome)
	}
}
