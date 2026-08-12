// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Сценарий 08 приёмки — порядок, нарушение которого стоит дороже всего.
//
// Идентификатор ресурса чеканится при ПРИЁМЕ операции, до выполнения работы, и
// терминальная запись отказа его не стирает. Прочитанный без проверки, он попадёт в
// состояние как идентификатор несуществующего ресурса, а проявится это на следующем шаге
// как отказ в доступе — там, где причину уже не видно.
func TestAwaitRefusesToReturnIdOnFailedOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// done=true, error задан, И метаданные заполнены — ровно то сочетание, на
		// котором наивная реализация запишет фантом.
		_, _ = w.Write([]byte(`{
			"id":"opabc","done":true,
			"metadata":{"@type":"type.googleapis.com/kacho.cloud.vpc.v1.CreateNetworkMetadata","networkId":"netphantom"},
			"error":{"code":9,"message":"network is not empty"}
		}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	op, err := c.AwaitOperation(context.Background(), "opabc", AwaitOptions{Budget: time.Second})
	if err == nil {
		t.Fatal("операция с отказом принята за успех — идентификатор уехал бы в состояние")
	}
	if op != nil {
		t.Error("операция возвращена вместе с отказом: вызывающий смог бы достать метаданные")
	}
	if !strings.Contains(err.Error(), "network is not empty") {
		t.Errorf("текст отказа края потерян: %v", err)
	}
}

// Парный положительный: успешная операция отдаёт метаданные.
// Без него предыдущий тест зеленел бы на функции, которая всегда возвращает ошибку.
func TestAwaitReturnsOperationOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"opok","done":true,
			"metadata":{"@type":"type.googleapis.com/kacho.cloud.vpc.v1.CreateNetworkMetadata","networkId":"net123"}
		}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	op, err := c.AwaitOperation(context.Background(), "opok", AwaitOptions{Budget: time.Second})
	if err != nil {
		t.Fatalf("успешная операция отвергнута: %v", err)
	}
	netID, ok := op.MetadataString("networkId")
	if op.ID != "opok" || !ok || netID != "net123" {
		t.Errorf("операция вернулась неполной: id=%q networkId=%q ok=%v", op.ID, netID, ok)
	}
}

// Сценарий 11: завершение в ПЕРВОМ же ответе — законный случай, а не ошибка.
// Сеть и подсеть исполняются синхронно, и требование «хотя бы один цикл опроса» повесило
// бы каждый их apply на длительность интервала.
func TestAwaitAcceptsImmediatelyDoneOperation(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"opsync","done":true}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	if _, err := c.AwaitOperation(context.Background(), "opsync",
		AwaitOptions{Budget: time.Second}); err != nil {
		t.Fatalf("синхронно завершённая операция отвергнута: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("сделано %d запросов вместо одного — лишний цикл ожидания на синхронном пути", n)
	}
}

// Ожидание идёт РЕАЛЬНОЙ паузой, а не плотным циклом: иначе провайдер сам создаёт
// нагрузку на край, ради ожидания которого он и написан.
func TestAwaitWaitsBetweenPolls(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			_, _ = w.Write([]byte(`{"id":"opslow","done":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"opslow","done":true}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	start := time.Now()
	if _, err := c.AwaitOperation(context.Background(), "opslow",
		AwaitOptions{Budget: 5 * time.Second, Interval: 100 * time.Millisecond}); err != nil {
		t.Fatalf("ожидание провалилось: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("три опроса уложились в %v — паузы между ними нет", elapsed)
	}
}

// Исчерпание бюджета — свой исход с внятным текстом, а не «ресурса нет».
func TestAwaitBudgetExhaustionIsNamed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"opnever","done":false}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	_, err := c.AwaitOperation(context.Background(), "opnever",
		AwaitOptions{Budget: 250 * time.Millisecond, Interval: 50 * time.Millisecond})
	if err == nil {
		t.Fatal("бесконечное ожидание завершилось успехом")
	}
	if !strings.Contains(err.Error(), "opnever") {
		t.Errorf("текст не называет операцию: %v", err)
	}
}

// Сценарий 09: ответ «не найдено» при опросе называет ОБЕ возможные причины.
// Край закрывает чужие операции проверкой владения, поэтому «операции нет» и «операция
// не ваша» приходят одинаково, и выбирать одну из двух причин провайдер не вправе.
func TestAwaitNotFoundNamesBothCauses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":5,"message":"operation not found"}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	_, err := c.AwaitOperation(context.Background(), "opgone", AwaitOptions{Budget: time.Second})
	if err == nil {
		t.Fatal("отсутствие операции принято за успех")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "принципал") && !strings.Contains(low, "тем же") {
		t.Errorf("текст не называет вторую причину (иной принципал): %v", err)
	}
}

func mustClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	c, err := New(Config{Endpoint: endpoint, Token: "t"})
	if err != nil {
		t.Fatalf("конструктор клиента: %v", err)
	}
	return c
}

// Отказ АСИНХРОННОЙ мутации приходит тем же конвертом google.rpc.Status — и теряет
// подробности ровно так же, если их не прочитать. Проба закрепляет, что причина отказа
// операции доезжает до оператора: без неё «invalid argument» на apply не указывает ни на
// одно поле, которое надо править.
func TestAwaitOperationCarriesFailureDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"op-1","done":true,"metadata":{"targetGroupId":"tgr-1"},` +
			`"error":{"code":3,"message":"invalid argument","details":[` +
			`{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[` +
			`{"field":"health_check.timeout","description":"must be <= interval"}]}]}}`))
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL)
	_, err := c.AwaitOperation(context.Background(), "op-1", AwaitOptions{Budget: time.Second})
	if err == nil {
		t.Fatal("операция с отказом принята за успех")
	}
	if !strings.Contains(err.Error(), "health_check.timeout: must be <= interval") {
		t.Fatalf("подробность отказа потеряна: %v", err)
	}
}
