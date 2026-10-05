// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

// from_address_test.go — ручка адреса отправителя `notify.smtp.fromAddress`
// (замысел З20 «Отправитель», §8; §12а «Сравнения доменов»: домен `From` —
// домен проверок DNS установки, Д101). Ручка без умолчания: незаданная, пустая
// или вне формы `notify/address` с непустым доменом — отказ старта с её
// именем. Значения адреса текст отказа не несёт: адрес — персональные данные
// (пакет address ошибок со значением не выпускает).
//
// Близнец — адрес фикстуры: старт принят, домен `From` загружен в форме
// `address.NormalizeDomain` (регистр сведён профилем).

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

const (
	fromKnob = "notify.smtp.fromAddress"
	envFrom  = "KACHO_NOTIFY_SMTP_FROM_ADDRESS"
)

func TestSenderFromAddressIsRequiredAndInForm(t *testing.T) {
	cases := []struct {
		name string
		edit *string
		why  []string
	}{
		{"переменная не задана", nil, []string{"не задана"}},
		{"пустая строка", str(""), nil},
		{"нет «@»", str("notify.example.invalid"), []string{"вне формы адреса"}},
		{"пустой домен", str("notify@"), []string{"вне формы адреса"}},
		{"пустая локальная часть", str("@example.invalid"), []string{"вне формы адреса"}},
		{"управляющий символ", str("notify@example.invalid\r\nBcc: x@y"), []string{"вне формы адреса"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFixture(t, map[string]*string{envFrom: c.edit})
			err := start(t)
			requireOnlyRefusal(t, err, fromKnob, c.why...)
			if c.edit != nil && *c.edit != "" && containsAny(err.Error(), *c.edit) {
				t.Fatalf("текст отказа несёт значение адреса: %v", err)
			}
		})
	}

	t.Run("близнец: адрес фикстуры, домен в нормализованной форме", func(t *testing.T) {
		useFixture(t, map[string]*string{envFrom: str("Notify@Example.INVALID")})
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("загрузчик отверг законный адрес: %v", err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("страж отверг законный адрес: %v", err)
		}
		if got := cfg.FromDomain(); got != "example.invalid" {
			t.Fatalf("домен From %q, ожидался нормализованный %q", got, "example.invalid")
		}
	})
}
