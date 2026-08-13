// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Сценарий 01 приёмки: адрес края обязателен, и отказ называет ОБА способа его задать.
//
// Умолчание здесь запрещено сознательно: адрес, выведенный из чужого значения, всегда
// непуст, поэтому провайдер выглядел бы настроенным и ходил бы в никуда.
func TestNewRequiresEndpointAndNamesBothWays(t *testing.T) {
	_, err := New(Config{Token: "t"})
	if err == nil {
		t.Fatal("клиент без адреса края создался — умолчание запрещено сценарием 01")
	}
	msg := err.Error()
	for _, want := range []string{"endpoint", "KACHO_ENDPOINT"} {
		if !strings.Contains(msg, want) {
			t.Errorf("отказ не называет %q: %s\n"+
				"Оператор обязан узнать из текста, ЧЕМ задать значение — иначе он ищет это в коде.",
				want, msg)
		}
	}
}

// Тот же сценарий, положительная сторона: с адресом и токеном клиент создаётся.
// Без этой пары отрицание зеленело бы на любой поломке конструктора.
func TestNewAcceptsCompleteConfig(t *testing.T) {
	c, err := New(Config{Endpoint: "https://api.example", Token: "t"})
	if err != nil {
		t.Fatalf("полная конфигурация отвергнута: %v", err)
	}
	if c.httpClient.Timeout == 0 {
		t.Error("срок вызова не задан — неотвечающий край подвесил бы операцию навсегда")
	}
}

// Сценарий 03: доверие задаётся бандлом, отключения проверки не существует.
//
// Проверяется ПОВЕДЕНИЕ, а не отсутствие поля: неизвестный сертификат обязан приводить к
// отказу соединения. Гейт на отсутствие ручки живёт отдельно (он судит дерево, а не вызов).
func TestUnknownCertificateIsRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{Endpoint: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("конструктор: %v", err)
	}
	if _, err := c.Do(context.Background(), http.MethodGet, "/vpc/v1/networks", nil, nil); err == nil {
		t.Fatal("соединение с неизвестным сертификатом прошло — доверие обязано быть fail-closed")
	}
}

// Та же проверка с другой стороны: сертификат, объявленный доверенным, принимается.
// Без положительного близнеца предыдущий тест зеленел бы и на клиенте, который вообще
// никуда не ходит.
func TestKnownCertificateIsAccepted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	c, err := New(Config{Endpoint: srv.URL, Token: "t", TLSRoots: pool})
	if err != nil {
		t.Fatalf("конструктор: %v", err)
	}
	resp, err := c.Do(context.Background(), http.MethodGet, "/vpc/v1/networks", nil, nil)
	if err != nil {
		t.Fatalf("доверенный сертификат отвергнут: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("статус %d, ожидался 200", resp.StatusCode)
	}
}

// Сценарий 05: ключ идемпотентности ДЕТЕРМИНИРОВАН.
//
// Выведен из содержания запроса, а не из случайного значения и не из времени: иначе повтор
// той же операции получил бы другой ключ, и заголовок перестал бы значить хоть что-нибудь.
func TestIdempotencyKeyIsDeterministic(t *testing.T) {
	a := IdempotencyKey("kacho_vpc_network", "module.net", []byte(`{"name":"n1"}`))
	b := IdempotencyKey("kacho_vpc_network", "module.net", []byte(`{"name":"n1"}`))
	if a != b {
		t.Errorf("тот же запрос дал разные ключи: %q != %q", a, b)
	}
	c := IdempotencyKey("kacho_vpc_network", "module.net", []byte(`{"name":"n2"}`))
	if a == c {
		t.Error("разные тела дали один ключ — повтор с другим содержанием считался бы тем же запросом")
	}
	d := IdempotencyKey("kacho_vpc_subnet", "module.net", []byte(`{"name":"n1"}`))
	if a == d {
		t.Error("разные типы ресурса дали один ключ")
	}
	if a == "" {
		t.Error("пустой ключ — заголовок был бы отброшен краем")
	}
}

// Сценарий 02: токен не попадает в текст ошибки.
//
// Утверждается НАБЛЮДАЕМОЕ. Файл состояния и журнал терраформа проверяются на своём уровне;
// здесь закрывается путь, которым секрет утекает чаще всего — диагностика транспорта.
func TestTokenNeverAppearsInErrors(t *testing.T) {
	const secret = "s3cr3t-token-value"

	// Заведомо недостижимый адрес: интересен текст отказа, а не ответ.
	c, err := New(Config{Endpoint: "https://127.0.0.1:1", Token: secret, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("конструктор: %v", err)
	}
	_, err = c.Do(context.Background(), http.MethodGet, "/vpc/v1/networks", nil, nil)
	if err == nil {
		t.Fatal("ожидался отказ соединения")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("токен утёк в текст ошибки: %s", err.Error())
	}
}

// Заголовки: авторизация и идемпотентность доезжают до края в ожидаемой форме.
func TestRequestCarriesAuthorizationAndIdempotency(t *testing.T) {
	var gotAuth, gotIdem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{Endpoint: srv.URL, Token: "abc"})
	if err != nil {
		t.Fatalf("конструктор: %v", err)
	}
	if _, err := c.Do(context.Background(), http.MethodPost, "/vpc/v1/networks", nil,
		&Headers{IdempotencyKey: "key-1"}); err != nil {
		t.Fatalf("запрос: %v", err)
	}
	if gotAuth != "Bearer abc" {
		t.Errorf("заголовок авторизации %q, ожидался %q", gotAuth, "Bearer abc")
	}
	if gotIdem != "key-1" {
		t.Errorf("ключ идемпотентности %q, ожидался %q", gotIdem, "key-1")
	}
}

// Срок вызова принадлежит клиенту и применяется КАЖДЫМ вызовом.
//
// Не «часть методов — да, часть — нет»: неотвечающий край подвешивает горутину навсегда,
// и это тот отказ, который выглядит как «терраформ завис», а не как ошибка.
func TestPerCallDeadlineApplies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(Config{Endpoint: srv.URL, Token: "t", Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("конструктор: %v", err)
	}
	start := time.Now()
	if _, err := c.Do(context.Background(), http.MethodGet, "/vpc/v1/networks", nil, nil); err == nil {
		t.Fatal("вызов не прервался по сроку")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("срок не применился: вызов длился %v", elapsed)
	}
}

// Единственный производитель настроек TLS — и у него нет отключения проверки.
//
// Проверяется идентичность конструкции, а не имя поля конфигурации: ручка могла бы
// называться как угодно, а дыра появляется именно здесь.
func TestTLSConfigNeverSkipsVerification(t *testing.T) {
	cfgs := []Config{
		{Endpoint: "https://a", Token: "t"},
		{Endpoint: "https://a", Token: "t", TLSRoots: x509.NewCertPool()},
	}
	for i, cfg := range cfgs {
		c, err := New(cfg)
		if err != nil {
			t.Fatalf("конфигурация %d: %v", i, err)
		}
		tr, ok := c.httpClient.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("конфигурация %d: транспорт не *http.Transport", i)
		}
		var tlsCfg *tls.Config = tr.TLSClientConfig
		if tlsCfg == nil {
			continue // умолчания Go — проверка включена
		}
		if tlsCfg.InsecureSkipVerify {
			t.Errorf("конфигурация %d: проверка сертификата отключена", i)
		}
	}
}

// Ключ идемпотентности обязан меняться вместе с ТЕЛОМ запроса.
//
// Край ключ соблюдает: тот же ключ возвращает ту же операцию (проверено на живом крае
// контролем в обе стороны). Поэтому ключ, не зависящий от тела, делает ОТКАЗ ЛИПКИМ —
// исправленная настройка воспроизводит прежнюю неудачную операцию, и пользователь
// уверен, что его правка не применилась.
func TestIdempotencyKeyFollowsTheBody(t *testing.T) {
	a := IdempotencyKey("kacho_nlb_listener", "lb/http", []byte(`{"port":80}`))
	b := IdempotencyKey("kacho_nlb_listener", "lb/http", []byte(`{"port":80,"targetPort":80}`))
	if a == b {
		t.Fatal("разные тела дали один ключ — исправленный запрос воспроизвёл бы прежний отказ")
	}
	// Парный положительный: ОДИН И ТОТ ЖЕ запрос обязан дать тот же ключ, иначе потерянный
	// ответ породил бы второй ресурс — ровно то, ради чего ключ и нужен.
	if a != IdempotencyKey("kacho_nlb_listener", "lb/http", []byte(`{"port":80}`)) {
		t.Fatal("одинаковые запросы дали разные ключи — повтор создал бы дубль")
	}
}
