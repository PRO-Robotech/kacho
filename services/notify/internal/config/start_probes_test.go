// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// NTF1-E04 — таймер Claim без значения: отказ старта с именем ручки; с заданным — старт.
func TestNTF1E04ClaimIntervalWithoutValueRefusesStart(t *testing.T) {
	cases := []struct {
		name string
		edit *string
		why  string
	}{
		{"не задан", nil, "не задана"},
		{"пустая строка", str(""), ""},
		{"ниже границы", str("999ms"), "[1s..5m0s]"},
		{"выше границы", str("5m1s"), "[1s..5m0s]"},
		{"не длительность", str("thirty"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{"KACHO_NOTIFY_CLAIM_INTERVAL": c.edit})
			requireOnlyRefusal(t, start(t), "notify.claimInterval", c.why)
		})
	}
	for _, v := range []string{"1s", "5m"} {
		t.Run("близнец "+v, func(t *testing.T) {
			useFixture(t, map[string]*string{"KACHO_NOTIFY_CLAIM_INTERVAL": str(v)})
			if err := start(t); err != nil {
				t.Fatalf("таймер %s на границе отвергнут: %v", v, err)
			}
		})
	}
}

// NTF1-G06 — без origin установки: отказ с именем ручки; с заданным — старт.
func TestNTF1G06OriginWithoutValueRefusesStart(t *testing.T) {
	cases := []struct {
		name string
		edit *string
	}{
		{"не задан", nil},
		{"пустая строка", str("")},
		{"не https", str("http://console.example.invalid")},
		{"не абсолютный", str("console.example.invalid")},
		{"с путём", str("https://console.example.invalid/console")},
		{"с косой чертой", str("https://console.example.invalid/")},
		{"с запросом", str("https://console.example.invalid?x=1")},
		{"с фрагментом", str("https://console.example.invalid#top")},
		{"с удостоверением", str("https://u@console.example.invalid")},
		{"без узла", str("https://")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{"KACHO_NOTIFY_ORIGIN": c.edit})
			requireOnlyRefusal(t, start(t), "notify.origin")
		})
	}
	t.Run("близнец с портом", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_ORIGIN": str("https://console.example.invalid:8443")})
		if err := start(t); err != nil {
			t.Fatalf("origin с портом отвергнут: %v", err)
		}
	})
}

// NTF1-G15 — посадка: одна нарушенная ось → отказ с именем оси.
//
// Ось `sslmode` судит общий дескриптор (servicecontract), а не этот пакет —
// её проба живёт в cmd/notify (TestNTF1G15DBSSLModeAxisRefusesStart): своя
// проверка по оси, которую общий уже судит, была бы вторым местом об одном
// предмете.
func TestNTF1G15PostureAxesRefuseStart(t *testing.T) {
	t.Run("authMode не production", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_AUTH_MODE": str("dev")})
		requireOnlyRefusal(t, start(t), "notify.authMode", "ось authMode")
	})
	t.Run("authMode вне перечня", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_AUTH_MODE": str("prod")})
		requireOnlyRefusal(t, start(t), "notify.authMode", "ось authMode")
	})
	t.Run("authMode не задан", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_AUTH_MODE": nil})
		requireOnlyRefusal(t, start(t), "notify.authMode")
	})
	t.Run("близнец production-strict", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_AUTH_MODE": str("production-strict")})
		if err := start(t); err != nil {
			t.Fatalf("production-strict отвергнут: %v", err)
		}
	})
	for _, knob := range []struct{ env, name string }{
		{"KACHO_NOTIFY_PEER_TLS_CERT_FILE", "notify.peerTLS.certFile"},
		{"KACHO_NOTIFY_PEER_TLS_KEY_FILE", "notify.peerTLS.keyFile"},
		{"KACHO_NOTIFY_PEER_TLS_CA_FILE", "notify.peerTLS.caFile"},
	} {
		t.Run("mTLS выключен: нет "+knob.name, func(t *testing.T) {
			useFixture(t, map[string]*string{knob.env: nil})
			requireOnlyRefusal(t, start(t), knob.name, "ось mTLS")
		})
	}
	t.Run("sslmode не задан", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_DB_SSLMODE": nil})
		requireOnlyRefusal(t, start(t), "notify.db.sslMode")
	})
}

// NTF1-G15, ось «секрет почты не смонтирован» — половина пары Д45 (CX1-82 (б)).
//
// Инъекция «ось безусловна» (отказ без удостоверения при любом адресе) красит
// близнеца «адрес без имени»: он здесь же, и потому ось не может стать
// безусловной молча.
func TestNTF1G15MailSecretNotMountedIsHalfOfThePair(t *testing.T) {
	t.Run("имя в адресе, переменной удостоверения нет", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://u@h:587/")})
		requireOnlyRefusal(t, start(t), "notify.smtp.credential", "секрет почты не смонтирован")
	})
	t.Run("близнец: адрес без имени, переменной нет", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://h:587/")})
		if err := start(t); err != nil {
			t.Fatalf("адрес без имени без удостоверения отвергнут — ось стала безусловной: %v", err)
		}
	})
	t.Run("близнец: имя в адресе и удостоверение", func(t *testing.T) {
		useFixture(t, map[string]*string{
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://u@h:587/"),
			"KACHO_NOTIFY_SMTP_CREDENTIAL":     str("relay-credential"),
		})
		if err := start(t); err != nil {
			t.Fatalf("пара «имя и удостоверение» отвергнута: %v", err)
		}
	})
	t.Run("удостоверение без имени в адресе", func(t *testing.T) {
		useFixture(t, map[string]*string{
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://h:587/"),
			"KACHO_NOTIFY_SMTP_CREDENTIAL":     str("relay-credential"),
		})
		requireOnlyRefusal(t, start(t), "notify.smtp.credential", "имени пользователя")
	})
	t.Run("удостоверение задано пустым", func(t *testing.T) {
		useFixture(t, map[string]*string{
			"KACHO_NOTIFY_SMTP_CONNECTION_URI": str("smtp://u@h:587/"),
			"KACHO_NOTIFY_SMTP_CREDENTIAL":     str(""),
		})
		requireOnlyRefusal(t, start(t), "notify.smtp.credential", "пуст")
	})
}

// УК31 — страж суммы сроков: resolveSendTimeout + smtp.sessionTimeout + ackMargin
// < feed.LeaseTTL, с именами обеих ручек.
func TestDeadlineSumGuardNamesBothKnobs(t *testing.T) {
	t.Run("сумма не меньше аренды", func(t *testing.T) {
		err := config.DeadlineSum(30*time.Second, 120*time.Second, 155*time.Second)
		if err == nil {
			t.Fatal("сумма 30s+120s+ackMargin при аренде 155s принята")
		}
		for _, want := range []string{
			"notify.resolveSendTimeout", "KACHO_NOTIFY_RESOLVE_SEND_TIMEOUT",
			"notify.smtp.sessionTimeout", "KACHO_NOTIFY_SMTP_SESSION_TIMEOUT",
			"feed.LeaseTTL",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("отказ суммы сроков не называет %q: %v", want, err)
			}
		}
	})
	t.Run("близнец: сумма меньше аренды на единицу", func(t *testing.T) {
		if err := config.DeadlineSum(30*time.Second, 120*time.Second,
			30*time.Second+120*time.Second+config.AckMargin+time.Nanosecond); err != nil {
			t.Fatalf("сумма ровно под арендой отвергнута: %v", err)
		}
	})
	t.Run("границы ручек укладываются в настоящую аренду", func(t *testing.T) {
		useFixture(t, map[string]*string{
			"KACHO_NOTIFY_RESOLVE_SEND_TIMEOUT": str("30s"),
			"KACHO_NOTIFY_SMTP_SESSION_TIMEOUT": str("120s"),
		})
		if err := start(t); err != nil {
			t.Fatalf("верхние границы ручек отвергнуты при настоящей аренде: %v", err)
		}
	})
	for _, c := range []struct{ env, name, v string }{
		{"KACHO_NOTIFY_RESOLVE_SEND_TIMEOUT", "notify.resolveSendTimeout", "99ms"},
		{"KACHO_NOTIFY_RESOLVE_SEND_TIMEOUT", "notify.resolveSendTimeout", "31s"},
		{"KACHO_NOTIFY_SMTP_SESSION_TIMEOUT", "notify.smtp.sessionTimeout", "999ms"},
		{"KACHO_NOTIFY_SMTP_SESSION_TIMEOUT", "notify.smtp.sessionTimeout", "121s"},
	} {
		t.Run(c.name+"="+c.v, func(t *testing.T) {
			useFixture(t, map[string]*string{c.env: str(c.v)})
			requireOnlyRefusal(t, start(t), c.name)
		})
	}
}

// Ручки посадки процесса без умолчания: незаданная — отказ с её именем.
func TestProcessKnobsWithoutValueRefuseStart(t *testing.T) {
	for _, c := range []struct{ env, name string }{
		{"KACHO_NOTIFY_DB_HOST", "notify.db.host"},
		{"KACHO_NOTIFY_DB_PORT", "notify.db.port"},
		{"KACHO_NOTIFY_DB_USER", "notify.db.user"},
		{"KACHO_NOTIFY_DB_PASSWORD", "notify.db.password"},
		{"KACHO_NOTIFY_DB_NAME", "notify.db.name"},
		{"KACHO_NOTIFY_DIAG_ADDR", "notify.diagAddr"},
		{"KACHO_NOTIFY_RESOLVE_SEND_TIMEOUT", "notify.resolveSendTimeout"},
		{"KACHO_NOTIFY_SMTP_SESSION_TIMEOUT", "notify.smtp.sessionTimeout"},
	} {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{c.env: nil})
			requireOnlyRefusal(t, start(t), c.name, "не задана")
		})
	}
	for _, c := range []struct{ env, name, v string }{
		{"KACHO_NOTIFY_DB_PORT", "notify.db.port", "0"},
		{"KACHO_NOTIFY_DB_PORT", "notify.db.port", "65536"},
		{"KACHO_NOTIFY_DIAG_ADDR", "notify.diagAddr", "9095"},
		{"KACHO_NOTIFY_DIAG_ADDR", "notify.diagAddr", ":0"},
	} {
		t.Run(c.name+"="+c.v, func(t *testing.T) {
			useFixture(t, map[string]*string{c.env: str(c.v)})
			requireOnlyRefusal(t, start(t), c.name)
		})
	}
}
