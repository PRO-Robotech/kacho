// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// Ключ сетки на адресата (`notify.recipientKey`, З24) — КЛЮЧЕВОЙ материал:
// ключ HMAC-SHA256 над нормализованным адресом (строки сетки) и над меткой
// отпечатка (ограда ключа). Страж старта fail-closed (Д89): незаданный —
// отказ, короче 32 байт — отказ; значение в текст отказа не попадает.
const (
	recipientKeyKnob = "notify.recipientKey"
	recipientKeyEnv  = "KACHO_NOTIFY_RECIPIENT_KEY"
)

func TestRecipientKeyUnsetRefusesStart(t *testing.T) {
	useFixture(t, map[string]*string{recipientKeyEnv: nil})
	requireOnlyRefusal(t, start(t), recipientKeyKnob, "не задана")
}

func TestRecipientKeyShorterThan32BytesRefusesStart(t *testing.T) {
	for _, v := range []string{"", "k", strings.Repeat("s", 31)} {
		t.Run(v, func(t *testing.T) {
			useFixture(t, map[string]*string{recipientKeyEnv: str(v)})
			err := start(t)
			requireOnlyRefusal(t, err, recipientKeyKnob, "32 байт")
			if v != "" && strings.Contains(err.Error(), v) {
				t.Fatalf("текст отказа несёт значение ключа: %v", err)
			}
		})
	}
}

// Близнец: ключ ровно 32 байт — граница включена, старт принят; значение ключа
// доезжает до поля и не раскрывается строковым видом.
func TestRecipientKeyOf32BytesStarts(t *testing.T) {
	key := strings.Repeat("k", 31) + "Z"
	useFixture(t, map[string]*string{recipientKeyEnv: str(key)})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("ключ 32 байт отвергнут: %v", err)
	}
	if got := cfg.RecipientKey().Bytes(); string(got) != key {
		t.Fatalf("ключ доехал как %q", got)
	}
	if s := cfg.RecipientKey().String(); strings.Contains(s, key) || strings.Contains(s, "Z") {
		t.Fatalf("строковый вид ключа раскрывает значение: %q", s)
	}
}
