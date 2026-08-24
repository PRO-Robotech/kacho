// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
)

// Поле `code` в теле отказа края — это код gRPC (`google.rpc.Status.code`), а
// НЕ номер HTTP-статуса. Так объявлено у каждого соседнего писателя отказа:
// 401 несёт 16 (`writeHTTPUnauthorized`, `writeHTTPUnauth` — там это записано
// комментарием дословно), 403 несёт 7, а отказ слоя прав при недоступном
// источнике вердикта несёт 14 (`authz.go`, ветвь `outcomeError`).
//
// ПОЧЕМУ ЭТО НЕ ПЕДАНТИЗМ. Клиент машинно ключуется на `code`, а не на прозу
// сообщения (`api-conventions.md` §«gRPC-код → HTTP-статус»), и число 503 в
// этом поле не значит НИЧЕГО: в перечне кодов gRPC его нет. Вызывающий,
// разбирающий ответ, получает вместо «повтори позже» неизвестную величину.
//
// НАБЛЮДАЛОСЬ: волна `authz-failclosed` утверждает ПАРУ (503 и code 14), и
// полоса чтения отзыва на предъявлении отвечала первой — то есть пара не
// сходилась на исправном по существу отказе.
//
// Проба парная НАМЕРЕННО: рядом стоит писатель 401, у которого поле заполнено
// верно. Без него утверждение «code == 14» зеленело бы и на пробе, читающей не
// то поле, и на теле, где `code` просто отсутствует.
func TestRefusalBodyCarriesTheGRPCCodeNotTheHTTPStatus(t *testing.T) {
	readCode := func(t *testing.T, rec *httptest.ResponseRecorder) float64 {
		t.Helper()
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("тело отказа не разбирается как JSON: %v (тело %q)", err, rec.Body.String())
		}
		raw, ok := body["code"]
		if !ok {
			t.Fatalf("в теле отказа нет поля code — клиенту ключеваться не на что: %q", rec.Body.String())
		}
		num, ok := raw.(float64)
		if !ok {
			t.Fatalf("поле code не число: %#v (тело %q)", raw, rec.Body.String())
		}
		return num
	}

	t.Run("unavailable", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeHTTPServiceUnavailable(rec, revocationUnavailableReason)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("HTTP-статус: получено %d, ожидалось %d", rec.Code, http.StatusServiceUnavailable)
		}
		if got, want := readCode(t, rec), float64(codes.Unavailable); got != want {
			t.Fatalf("code в теле: получено %v, ожидалось %v (UNAVAILABLE). "+
				"Номер HTTP-статуса в это поле не кладётся — оно про google.rpc.Status", got, want)
		}
	})

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: сосед, у которого поле уже верно.
	t.Run("unauthenticated control", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeHTTPUnauthorized(rec, "token validation failed")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("HTTP-статус: получено %d, ожидалось %d", rec.Code, http.StatusUnauthorized)
		}
		if got, want := readCode(t, rec), float64(codes.Unauthenticated); got != want {
			t.Fatalf("code в теле: получено %v, ожидалось %v (UNAUTHENTICATED)", got, want)
		}
	})
}
