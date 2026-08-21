// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// idempotency_store_contract_test.go — что середина делает с ответами хранилища.
//
// Хранилище здесь подставное намеренно: предмет этих проб — РЕШЕНИЕ середины на
// каждый из исходов допуска, а не устройство хранилища (его проверяет
// gateway/internal/idempotencypg на настоящей базе, двумя независимыми
// репликами). Подставное хранилище при этом не снисходительнее настоящего: оно
// не глотает ввод, а возвращает ровно те исходы, которые объявляет контракт.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedStore — хранилище, отвечающее заранее назначенным исходом.
type scriptedStore struct {
	reserve    IdempotencyReservation
	reserveErr error
	await      IdempotencyAwait
	commits    int64
	releases   int64
}

func (s *scriptedStore) Reserve(context.Context, string) (IdempotencyReservation, error) {
	return s.reserve, s.reserveErr
}

func (s *scriptedStore) Commit(context.Context, IdempotencyReservation, IdempotencyRecord, bool) {
	atomic.AddInt64(&s.commits, 1)
}

func (s *scriptedStore) Release(context.Context, IdempotencyReservation) {
	atomic.AddInt64(&s.releases, 1)
}

func (s *scriptedStore) Await(context.Context, IdempotencyReservation) IdempotencyAwait {
	return s.await
}

// bodyCode вынимает `code` из тела в форме grpc-gateway.
func bodyCode(t *testing.T, raw []byte) int {
	t.Helper()
	var got struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("тело ответа не разбирается как {code,message,details}: %v (%q)", err, raw)
	}
	if got.Message == "" {
		t.Fatalf("отказ без сообщения: %q — вызывающий не узнает, что произошло", raw)
	}
	return got.Code
}

// drivePost гонит один mutating-запрос с ключом.
func drivePost(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", nil)
	r.Header.Set("Idempotency-Key", "K")
	r.Header.Set("X-Kacho-Principal-Id", "user-A")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// TestIdempotency_StoreUnavailable_FailsClosedAndDoesNotMutate — вызывающий
// попросил однократность; дать её нечем — значит мутация не исполняется.
//
// Молча исполнить значило бы ответить успехом на просьбу, которую мы не
// выполнили: запрещённый класс «принято-и-проигнорировано», и заметить его
// можно было бы только по последствиям.
func TestIdempotency_StoreUnavailable_FailsClosedAndDoesNotMutate(t *testing.T) {
	var calls int64
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusOK)
	})

	t.Run("хранилище отказало — 503 и обработчик не тронут", func(t *testing.T) {
		atomic.StoreInt64(&calls, 0)
		store := &scriptedStore{reserveErr: context.DeadlineExceeded}
		rr := drivePost(HTTPIdempotency(store)(downstream))
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("ответ %d, ожидался 503: гарантия обещана и не может быть исполнена", rr.Code)
		}
		if got := bodyCode(t, rr.Body.Bytes()); got != 14 { // codes.Unavailable
			t.Fatalf("код в теле %d, ожидался 14 (UNAVAILABLE)", got)
		}
		if n := atomic.LoadInt64(&calls); n != 0 {
			t.Fatalf("обработчик вызван %d раз(а) при недоступном хранилище — мутация "+
				"исполнена без гарантии, которую у нас попросили", n)
		}
	})

	// Положительный контроль: без него отрицание было бы зелено и у середины,
	// которая отвергает ВСЁ.
	t.Run("хранилище работает — обработчик исполняется", func(t *testing.T) {
		atomic.StoreInt64(&calls, 0)
		store := &scriptedStore{reserve: IdempotencyReservation{
			Key: "K", Outcome: IdempotencyOwn, Lease: "lease",
		}}
		rr := drivePost(HTTPIdempotency(store)(downstream))
		if rr.Code != http.StatusOK {
			t.Fatalf("ответ %d, ожидался 200", rr.Code)
		}
		if n := atomic.LoadInt64(&calls); n != 1 {
			t.Fatalf("обработчик вызван %d раз(а), ожидался 1", n)
		}
		if got := atomic.LoadInt64(&store.commits); got != 1 {
			t.Fatalf("исход записан %d раз(а), ожидался 1", got)
		}
	})
}

// TestIdempotency_HolderStillInFlight_AnswersInFlightAndDoesNotMutate — держатель
// брони не уложился в бюджет ожидания: вызывающий получает «ключ в работе», а не
// второе исполнение.
func TestIdempotency_HolderStillInFlight_AnswersInFlightAndDoesNotMutate(t *testing.T) {
	var calls int64
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&calls, 1)
		w.WriteHeader(http.StatusOK)
	})

	t.Run("держатель ещё работает — 409, обработчик не тронут", func(t *testing.T) {
		atomic.StoreInt64(&calls, 0)
		store := &scriptedStore{
			reserve: IdempotencyReservation{Key: "K", Outcome: IdempotencyWait, Lease: "x"},
			await:   IdempotencyAwait{Outcome: IdempotencyAwaitBusy},
		}
		rr := drivePost(HTTPIdempotency(store)(downstream))
		if rr.Code != http.StatusConflict {
			t.Fatalf("ответ %d, ожидался 409", rr.Code)
		}
		if got := bodyCode(t, rr.Body.Bytes()); got != 10 { // codes.Aborted
			t.Fatalf("код в теле %d, ожидался 10 (ABORTED)", got)
		}
		if n := atomic.LoadInt64(&calls); n != 0 {
			t.Fatalf("обработчик вызван %d раз(а), пока ключ держит другой — это и есть "+
				"второе исполнение мутации", n)
		}
	})

	t.Run("держатель оставил исход — он и отдаётся", func(t *testing.T) {
		atomic.StoreInt64(&calls, 0)
		store := &scriptedStore{
			reserve: IdempotencyReservation{Key: "K", Outcome: IdempotencyWait, Lease: "x"},
			await: IdempotencyAwait{
				Outcome: IdempotencyAwaitReplay,
				Record: IdempotencyRecord{
					StatusCode: http.StatusCreated, ContentType: "application/json",
					Body: []byte(`{"op":"op-1"}`),
				},
			},
		}
		rr := drivePost(HTTPIdempotency(store)(downstream))
		if rr.Code != http.StatusCreated || rr.Body.String() != `{"op":"op-1"}` {
			t.Fatalf("ответ %d %q, ожидался 201 с исходом держателя", rr.Code, rr.Body.String())
		}
		if rr.Header().Get("X-Idempotent-Replayed") != "true" {
			t.Fatal("повтор не помечен как повторённый")
		}
		if n := atomic.LoadInt64(&calls); n != 0 {
			t.Fatalf("обработчик вызван %d раз(а) при готовом исходе держателя", n)
		}
	})

	t.Run("держатель ушёл без исхода — ключ свободен, исполняем сами", func(t *testing.T) {
		atomic.StoreInt64(&calls, 0)
		store := &scriptedStore{
			reserve: IdempotencyReservation{Key: "K", Outcome: IdempotencyWait, Lease: "x"},
			await:   IdempotencyAwait{Outcome: IdempotencyAwaitVacant},
		}
		rr := drivePost(HTTPIdempotency(store)(downstream))
		if rr.Code != http.StatusOK {
			t.Fatalf("ответ %d, ожидался 200", rr.Code)
		}
		if n := atomic.LoadInt64(&calls); n != 1 {
			t.Fatalf("обработчик вызван %d раз(а), ожидался 1", n)
		}
	})
}

// TestMemoryStore_DoesNotSpanReplicas_HenceTheBootGuard — характеристика границы,
// а не дефект: хранилище в памяти процесса НЕ охватывает флот, и это ровно то,
// из-за чего пара «память + больше одной реплики» отвергается при старте.
//
// Проба закрепляет границу с ОБЕИХ сторон: одно хранилище на двух обработчиков
// (один под) держит однократность; два независимых хранилища (два пода) — нет.
// Без второй половины отказ в старте выглядел бы перестраховкой; без первой —
// нельзя было бы отличить негодное хранилище от негодной середины.
func TestMemoryStore_DoesNotSpanReplicas_HenceTheBootGuard(t *testing.T) {
	newDownstream := func(calls *int64) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(calls, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"op":"op"}`))
		})
	}

	t.Run("ОДИН процесс: однократность держится", func(t *testing.T) {
		var calls int64
		store := NewIdempotencyStore(time.Minute)
		down := newDownstream(&calls)
		a := HTTPIdempotency(store)(down)
		b := HTTPIdempotency(store)(down)
		drivePost(a)
		drivePost(b)
		if n := atomic.LoadInt64(&calls); n != 1 {
			t.Fatalf("в одном процессе обработчик исполнен %d раз(а), ожидался 1", n)
		}
	})

	t.Run("ДВА процесса: однократности нет — потому и отказ в старте", func(t *testing.T) {
		var calls int64
		down := newDownstream(&calls)
		podA := HTTPIdempotency(NewIdempotencyStore(time.Minute))(down)
		podB := HTTPIdempotency(NewIdempotencyStore(time.Minute))(down)
		drivePost(podA)
		drivePost(podB)
		if n := atomic.LoadInt64(&calls); n != 2 {
			t.Fatalf("два независимых хранилища дали %d исполнений; ожидалось 2 — если их "+
				"стало 1, хранилище в памяти процесса каким-то образом охватило флот, и "+
				"отказ validateIdempotencyFleetPairing потерял предмет", n)
		}
	})
}
