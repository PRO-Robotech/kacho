// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

// openfga_check_retry_test.go — доказательство RED→GREEN по задаче #720.
//
// ПРЕДМЕТ. Вопрос к хранилищу прав на горячем пути авторизации звал транспорт
// НАПРЯМУЮ, минуя повтор, который получает каждое остальное чтение хранилища
// через общий `c.do()`. Одиночный перебой становился терминальным отказом
// недоступности арендатору — на ИДЕМПОТЕНТНОМ чтении, где повтор безопасен.
//
// НАБЛЮДАВШАЯСЯ ФОРМА (#720): ответ не пришёл за отведённый срок. Именно она
// стоит первым кейсом, и она же объясняет, почему у попытки СВОЙ срок: повтор,
// делящий один общий бюджет с первой попыткой, эту форму не переживает by
// construction — бюджет уже израсходован.
//
// Отрицания стоят В ПАРЕ с положительными: без кейса «здоровый ответ стоит
// ровно одного запроса» утверждение «перебой поглощён» было бы неотличимо от
// «клиент всегда ходит трижды».

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/iam/internal/clients"
)

// checkProbeTimeout — срок одной попытки в пробах. Много меньше боевых 200ms,
// чтобы проба ждала миллисекунды, а не секунды; на разбираемое свойство
// величина не влияет.
const checkProbeTimeout = 40 * time.Millisecond

// scriptedStore поднимает хранилище прав, отвечающее по сценарию: обработчик
// получает номер запроса (с единицы) и решает, что ответить.
func scriptedStore(t *testing.T, handle func(n int, w http.ResponseWriter)) (endpoint string, requests func() int) {
	t.Helper()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle(int(n.Add(1)), w)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), func() int { return int(n.Load()) }
}

func newProbeClient(endpoint string) *clients.OpenFGAHTTPClient {
	return &clients.OpenFGAHTTPClient{
		Endpoint:           endpoint,
		StoreID:            "st_test",
		AuthorizationModel: "01MODEL",
		CheckTimeout:       checkProbeTimeout,
	}
}

// TestCheck_SlowAnswerBlipIsAbsorbedByRetry — НАБЛЮДАВШАЯСЯ форма #720.
//
// RED до фикса: первая попытка не укладывается в срок, повтора нет, вызывающий
// получает недоступность, и арендатор — 503 посреди здорового прогона.
func TestCheck_SlowAnswerBlipIsAbsorbedByRetry(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			// Перебой: ответ не поспевает к сроку попытки.
			time.Sleep(4 * checkProbeTimeout)
			return
		}
		_, _ = fmt.Fprint(w, `{"allowed":true}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.Check(context.Background(), "user:usr01", "v_get", "account:acc01")
	if err != nil {
		t.Fatalf("одиночный перебой обязан быть поглощён повтором на идемпотентном чтении, "+
			"а не стать терминальной недоступностью: %v", err)
	}
	if !allowed {
		t.Fatalf("после повтора вопрос отвечен разрешением, получено allowed=false")
	}
	if got := requests(); got != 2 {
		t.Fatalf("ожидалось 2 запроса (перебой + повтор), сделано %d", got)
	}
	cnt := c.CheckOutcomeCounts()
	if cnt.Recovered != 1 || cnt.Answered != 0 {
		t.Fatalf("поглощённый перебой обязан быть виден отдельной клеткой: "+
			"recovered=%d answered=%d (ожидалось 1 и 0)", cnt.Recovered, cnt.Answered)
	}
}

// TestCheck_ServerErrorBlipIsAbsorbedByRetry — вторая переживаемая форма (5xx).
func TestCheck_ServerErrorBlipIsAbsorbedByRetry(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, `{"code":"unavailable"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"allowed":true}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.Check(context.Background(), "user:usr01", "v_get", "account:acc01")
	if err != nil || !allowed {
		t.Fatalf("5xx на первой попытке — перебой, он обязан быть поглощён: allowed=%v err=%v", allowed, err)
	}
	if got := requests(); got != 2 {
		t.Fatalf("ожидалось 2 запроса, сделано %d", got)
	}
	if got := c.CheckOutcomeCounts().Recovered; got != 1 {
		t.Fatalf("recovered=%d, ожидалось 1", got)
	}
}

// TestCheck_HealthyAnswerCostsExactlyOneRequest — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к
// двум кейсам выше. Без него «перебой поглощён» неотличимо от «клиент ходит
// трижды всегда», а «recovered=1» — от «recovered растёт на каждом вопросе».
func TestCheck_HealthyAnswerCostsExactlyOneRequest(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"allowed":true}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.Check(context.Background(), "user:usr01", "v_get", "account:acc01")
	if err != nil || !allowed {
		t.Fatalf("здоровое хранилище: allowed=%v err=%v", allowed, err)
	}
	if got := requests(); got != 1 {
		t.Fatalf("здоровый ответ обязан стоить РОВНО одного запроса, сделано %d "+
			"(повтор на успехе — это утроенная нагрузка на хранилище)", got)
	}
	cnt := c.CheckOutcomeCounts()
	if cnt.Answered != 1 || cnt.Recovered != 0 {
		t.Fatalf("answered=%d recovered=%d, ожидалось 1 и 0", cnt.Answered, cnt.Recovered)
	}
}

// TestCheck_CleanDenyIsNotRetried — законный близнец ДРУГОЙ конструкции: 400 —
// это не сбой, а отказ в доступе, который не разрешится никогда. Повтор здесь
// означал бы троекратный вопрос на каждом отказе.
func TestCheck_CleanDenyIsNotRetried(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"code":"validation_error"}`)
	})
	c := newProbeClient(endpoint)

	allowed, err := c.Check(context.Background(), "user:usr01", "v_get", "account:acc01")
	if err != nil {
		t.Fatalf("400 — чистый отказ в доступе (nil error), получено %v", err)
	}
	if allowed {
		t.Fatalf("400 обязан отказывать")
	}
	if got := requests(); got != 1 {
		t.Fatalf("отказ в доступе повтору не подлежит: ожидался 1 запрос, сделано %d", got)
	}
	if got := c.CheckOutcomeCounts().Answered; got != 1 {
		t.Fatalf("отказ в доступе — ОТВЕЧЕННЫЙ вопрос: answered=%d, ожидалось 1", got)
	}
}

// TestCheck_MisconfigurationIsNotRetriedAndNamedApart — второй законный
// близнец, тоже другой конструкции: по адресу нас не пускают (404/403).
// Временем не лечится, поэтому повтора нет, и клетка отдельная — настройку не
// прятать под сбой (security.md §Hardening-инвариант 8).
func TestCheck_MisconfigurationIsNotRetriedAndNamedApart(t *testing.T) {
	endpoint, requests := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"code":"store_not_found"}`)
	})
	c := newProbeClient(endpoint)

	if _, err := c.Check(context.Background(), "user:usr01", "v_get", "account:acc01"); err == nil {
		t.Fatalf("404 от хранилища обязан быть отказом, а не молчаливым «нельзя»")
	}
	if got := requests(); got != 1 {
		t.Fatalf("настройка повтору не подлежит: ожидался 1 запрос, сделано %d", got)
	}
	cnt := c.CheckOutcomeCounts()
	if cnt.Rejected != 1 || cnt.ServerError != 0 || cnt.Deadline != 0 {
		t.Fatalf("настройка обязана стоять в СВОЕЙ клетке: rejected=%d server_error=%d deadline=%d",
			cnt.Rejected, cnt.ServerError, cnt.Deadline)
	}
}

// TestCheck_OutageShapesLandInDistinctCells — предмет наблюдаемости из #720:
// «хранилище моргнуло» и «до хранилища не дозвонились» обязаны РАЗЛИЧАТЬСЯ
// числом, без чтения журнала построчно. Сегодня оба приезжают вызывающему
// одним кодом недоступности — и это единственное, что о них было известно.
func TestCheck_OutageShapesLandInDistinctCells(t *testing.T) {
	// Форма «не ответило вовремя»: слушатель жив, ответа нет.
	slowEndpoint, _ := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		time.Sleep(4 * checkProbeTimeout)
	})
	slow := newProbeClient(slowEndpoint)
	if _, err := slow.Check(context.Background(), "user:usr01", "v_get", "account:acc01"); err == nil {
		t.Fatalf("неотвечающее хранилище обязано дать отказ")
	}

	// Форма «не дозвонились»: слушателя нет вовсе (реплик не осталось).
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadEndpoint := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()
	down := newProbeClient(deadEndpoint)
	if _, err := down.Check(context.Background(), "user:usr01", "v_get", "account:acc01"); err == nil {
		t.Fatalf("свёрнутое хранилище обязано дать отказ")
	}

	slowCnt, downCnt := slow.CheckOutcomeCounts(), down.CheckOutcomeCounts()
	if slowCnt.Deadline != 1 {
		t.Fatalf("«не ответило вовремя» обязано попасть в клетку deadline, получено %+v", slowCnt)
	}
	if downCnt.Connect != 1 {
		t.Fatalf("«не дозвонились» обязано попасть в клетку connect, получено %+v", downCnt)
	}
	if slowCnt.Connect != 0 || downCnt.Deadline != 0 {
		t.Fatalf("две формы перебоя обязаны РАЗЛИЧАТЬСЯ, а не сливаться: slow=%+v down=%+v", slowCnt, downCnt)
	}
}

// TestCheck_CallerBudgetIsNeverExceeded — повтор не смеет тратить чужой
// бюджет: срок вызывающего остаётся верхней границей, а его отмена не
// засчитывается хранилищу как отказ.
func TestCheck_CallerBudgetIsNeverExceeded(t *testing.T) {
	endpoint, _ := scriptedStore(t, func(_ int, w http.ResponseWriter) {
		time.Sleep(10 * checkProbeTimeout)
	})
	c := newProbeClient(endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), checkProbeTimeout/2)
	defer cancel()
	start := time.Now()
	if _, err := c.Check(ctx, "user:usr01", "v_get", "account:acc01"); err == nil {
		t.Fatalf("истёкший срок вызывающего обязан дать отказ")
	}
	elapsed := time.Since(start)
	// Три попытки по сроку попытки — это заведомо больше; уложились в один
	// бюджет вызывающего с запасом ⇒ повтор его границу уважает.
	if elapsed > 3*checkProbeTimeout {
		t.Fatalf("повтор потратил чужой бюджет: прошло %v при сроке вызывающего %v",
			elapsed, checkProbeTimeout/2)
	}
	if got := c.CheckOutcomeCounts().Canceled; got != 1 {
		t.Fatalf("уход вызывающего — не отказ хранилища: canceled=%d, ожидалось 1", got)
	}
}
