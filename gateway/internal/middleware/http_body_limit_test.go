// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// http_body_limit_test.go — предмет: у тела запроса на REST-краю обязан быть
// потолок.
//
// Само по себе «принять большое тело» стоило бы дёшево, если бы тело только
// пересылали. Но на краю оно буферизуется для извлечения области, разбирается в
// сообщение и ещё раз разбирается в обобщённое представление ради проверки имён
// значений перечислений — то есть резидентно живёт кратно своему размеру, и так
// на каждый запрос в полёте. Без потолка эта величина ничем не ограничена.
package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func bodyLimitProbe(t *testing.T, limit int64) (handler http.Handler, reached *atomic.Int64, readBytes *atomic.Int64) {
	t.Helper()
	reached = &atomic.Int64{}
	readBytes = &atomic.Int64{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		n, _ := io.Copy(io.Discard, r.Body)
		readBytes.Add(n)
		w.WriteHeader(http.StatusOK)
	})
	return HTTPMaxBodyBytes(limit)(next), reached, readBytes
}

// TestHTTPMaxBodyBytes_DeclaredOversizeIsRejectedBeforeAnyRead — тело с
// объявленной длиной больше потолка отвергается 413, и обработчик за
// ограничителем не вызывается вовсе.
//
// Утверждение наблюдаемое с двух сторон: код ответа И число вызовов
// обработчика. «Вернулся 413» без второй половины не отличило бы отказ ПОСЛЕ
// того, как тело уже прочитали и размножили.
func TestHTTPMaxBodyBytes_DeclaredOversizeIsRejectedBeforeAnyRead(t *testing.T) {
	const limit = 1024
	h, reached, _ := bodyLimitProbe(t, limit)

	body := strings.Repeat("x", limit*4)
	req := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("тело в %d байт при потолке %d получило %d вместо 413", len(body), limit, rec.Code)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("обработчик за ограничителем вызван %d раз(а) на заведомо превышающем теле: "+
			"отказ, выданный после разбора, память уже не экономит", n)
	}
}

// TestHTTPMaxBodyBytes_UndeclaredOversizeIsCutOff — заявленную длину клиент
// может не прислать (chunked). Тогда потолок обязан сработать на чтении: сколько
// бы байт ни было прислано, обработчик не прочитает больше потолка.
func TestHTTPMaxBodyBytes_UndeclaredOversizeIsCutOff(t *testing.T) {
	const limit = 1024
	h, _, readBytes := bodyLimitProbe(t, limit)

	body := strings.Repeat("x", limit*8)
	req := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1 // длина не объявлена
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if n := readBytes.Load(); n > limit {
		t.Fatalf("из тела без объявленной длины прочитано %d байт при потолке %d: "+
			"объём, удерживаемый в памяти на один запрос, ничем не ограничен", n, limit)
	}
}

// TestHTTPMaxBodyBytes_BodyWithinLimitPassesThroughByteForByte — законная
// половина: тело в пределах потолка доезжает до обработчика целиком и
// неизменным. Без неё «фикс» мог бы состоять в том, чтобы резать всех.
func TestHTTPMaxBodyBytes_BodyWithinLimitPassesThroughByteForByte(t *testing.T) {
	const limit = 1024
	want := strings.Repeat("y", limit-1)

	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("чтение тела в пределах потолка не должно давать ошибку: %v", err)
		}
		got = string(b)
		w.WriteHeader(http.StatusOK)
	})
	h := HTTPMaxBodyBytes(limit)(next)

	req := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(want))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("тело в пределах потолка получило %d", rec.Code)
	}
	if got != want {
		t.Fatalf("тело изменилось: получено %d байт из %d", len(got), len(want))
	}
}

// TestHTTPMaxBodyBytes_ExactlyAtLimitPasses — граница включительно: ровно
// потолок — это ещё «в пределах».
func TestHTTPMaxBodyBytes_ExactlyAtLimitPasses(t *testing.T) {
	const limit = 1024
	h, reached, readBytes := bodyLimitProbe(t, limit)

	req := httptest.NewRequest(http.MethodPost, "/vpc/v1/networks", strings.NewReader(strings.Repeat("z", limit)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("тело ровно в потолок получило %d вместо 200", rec.Code)
	}
	if reached.Load() != 1 || readBytes.Load() != limit {
		t.Fatalf("тело ровно в потолок должно доехать целиком: вызовов %d, прочитано %d",
			reached.Load(), readBytes.Load())
	}
}

// TestEdgeBodyCapCoversTheScopeInspector — предпосылка: всё, что край
// принимает, обязано целиком поддаваться осмотру для извлечения области.
//
// Если потолок станет больше буфера осмотра, законный запрос в пределах потолка
// будет разрезан посередине документа, область не извлечётся и вызывающий
// получит отказ по правам вместо обработки. Тогда менять надо оба числа
// осознанно, а не одно из них молча.
func TestEdgeBodyCapCoversTheScopeInspector(t *testing.T) {
	if EdgeMaxRequestBodyBytes > int64(bodyInspectCap) {
		t.Fatalf("потолок тела %d больше буфера осмотра %d: законное тело в пределах потолка "+
			"не отдаст область и получит отказ по правам", EdgeMaxRequestBodyBytes, bodyInspectCap)
	}
}
