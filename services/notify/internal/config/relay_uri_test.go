// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// relay_uri_test.go — узел почты: ручка `notify.smtp.connectionURI`, закрытый
// разбор адреса и страж старта (полоса N13; Д44, Д45; CX1-77, CX1-79, CX1-84;
// замысел З20 «Узел почты», §8 строка `notify.smtp.connectionURI`).
//
// Каждое отрицание меняет ОДНУ строку фикстуры `testdata/boot.env` и требует
// РОВНО одну находку с именем ручки адреса и именем поля (requireOnlyRefusal):
// отказ по чужой причине пробу не зеленит. Близнец каждой оси — адрес той же
// формы с исправленным единственным фактом.

import (
	"testing"
)

const relayKnob = "notify.smtp.connectionURI"

// TestNotifyStartRefusedWithoutRelayAddress — Д44: у ручки адреса нет умолчания.
// Незаданная и пустая — отказ старта с именем ручки, а не подстановка и не
// отложенный отказ первой SMTP-сессии. Близнец — действительный адрес фикстуры.
func TestNotifyStartRefusedWithoutRelayAddress(t *testing.T) {
	t.Run("переменная не задана", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": nil})
		requireOnlyRefusal(t, start(t), relayKnob, "не задана")
	})
	t.Run("пустая строка", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("")})
		requireOnlyRefusal(t, start(t), relayKnob)
	})
	t.Run("близнец: действительный адрес фикстуры", func(t *testing.T) {
		useFixture(t, nil)
		if err := start(t); err != nil {
			t.Fatalf("действительный адрес ретранслятора отвергнут: %v", err)
		}
	})
}

// TestRelayURIParseIsClosed — разбор адреса закрытой таблицей (Д44, CX1-77,
// CX1-84): схема → режим только `smtp`/`smtps`; порт только из адреса и в
// [1..65535]; узел непуст; пароля, параметров запроса и пути нет; имя
// пользователя — раскодированное и непустое. Каждое нарушение — отказ с
// именем ручки и ПОЛЯ (слово поля — в тексте отказа).
func TestRelayURIParseIsClosed(t *testing.T) {
	cases := []struct {
		name  string
		uri   string
		cred  *string // удостоверение: nil — переменной нет
		field string
	}{
		{"чужая схема http", "http://relay.example.invalid:587/", nil, "схема"},
		{"схема lmtp", "lmtp://relay.example.invalid:24/", nil, "схема"},
		{"схема smtp+insecure", "smtp+insecure://relay.example.invalid:25/", nil, "схема"},
		{"без схемы", "relay.example.invalid:587", nil, "схема"},
		{"адрес без порта", "smtp://relay.example.invalid/", nil, "порт"},
		{"адрес без порта smtps", "smtps://relay.example.invalid/", nil, "порт"},
		{"порт 0", "smtp://relay.example.invalid:0/", nil, "порт"},
		{"порт 65536", "smtp://relay.example.invalid:65536/", nil, "порт"},
		{"пустой узел", "smtp://:587/", nil, "узел"},
		{"пароль в адресе", "smtp://u:p@relay.example.invalid:587/", str("relay-credential"), "пароль"},
		{"параметр запроса", "smtp://relay.example.invalid:587/?tls=off", nil, "запрос"},
		{"пустой параметр запроса", "smtp://relay.example.invalid:587/?", nil, "запрос"},
		{"путь", "smtp://relay.example.invalid:587/relay", nil, "путь"},
		{"пустое имя пользователя (CX1-84)", "smtp://@relay.example.invalid:587/", nil, "имя пользователя"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{
				"KACHO_NOTIFY_SMTP_CONNECTION_URI": str(c.uri),
				"KACHO_NOTIFY_SMTP_CREDENTIAL":     c.cred,
			})
			requireOnlyRefusal(t, start(t), relayKnob, c.field)
		})
	}

	// Близнецы: законные формы адреса, каждая — пара к своей оси выше.
	twins := []struct {
		name string
		uri  string
		cred *string
	}{
		{"нижняя граница порта smtp", "smtp://h:1/", nil},
		{"верхняя граница порта smtps", "smtps://h:65535/", nil},
		{"без завершающей косой черты", "smtp://relay.example.invalid:587", nil},
		{"IP-литерал узла", "smtp://192.0.2.10:25/", nil},
		{"IPv6-литерал узла", "smtps://[2001:db8::10]:465/", nil},
		{"раскодированное имя a@b с удостоверением (CX1-84)", "smtps://a%40b@h:465/", str("relay-credential")},
	}
	for _, c := range twins {
		t.Run("близнец: "+c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{
				"KACHO_NOTIFY_SMTP_CONNECTION_URI": str(c.uri),
				"KACHO_NOTIFY_SMTP_CREDENTIAL":     c.cred,
			})
			if err := start(t); err != nil {
				t.Fatalf("законный адрес %q отвергнут: %v", c.uri, err)
			}
		})
	}
}

// TestRelayURIRefusalDoesNotCarryThePassword — адрес с паролем отвергнут, и
// текст отказа пароля не несёт: отказ уходит в журнал процесса.
func TestRelayURIRefusalDoesNotCarryThePassword(t *testing.T) {
	const secret = "pw-marker-7f3a9c"
	useFixture(t, map[string]*string{
		"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://u:" + secret + "@relay.example.invalid:587/"),
		"KACHO_NOTIFY_SMTP_CREDENTIAL":     str("relay-credential"),
	})
	err := start(t)
	requireOnlyRefusal(t, err, relayKnob, "пароль")
	if containsAny(err.Error(), secret) {
		t.Fatalf("текст отказа раскрыл пароль адреса: %v", err)
	}
}
