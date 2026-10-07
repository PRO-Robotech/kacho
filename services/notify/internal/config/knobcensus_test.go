// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// Перепись ручек notify утверждает ровно одно (CX1-78, NTF1-G07 последняя
// строка): ручки, которой можно выключить TLS или его проверку, в конфигурации
// notify нет. Умолчание адреса ретранслятора эта перепись не судит — его держит
// инъекция пробы полосы N13.
//
// Перепись берётся из [config.Knobs] — того же перечня, по которому загрузчик
// читает окружение, — а не выписывается здесь: ручка, заведённая в обход
// перечня, загрузчиком не читалась бы вовсе.

// tlsSwitchTokens — признаки имени ручки-выключателя транспорта.
var tlsSwitchTokens = []string{
	"INSECURE", "SKIP_VERIFY", "PLAINTEXT", "PLAIN_TEXT", "NO_TLS", "TLS_DISABLE",
	"DISABLE_TLS", "TLS_ENABLE", "ENABLE_TLS", "MTLS_ENABLE", "ALLOW_PLAIN",
	"STARTTLS_OPTIONAL", "TLS_OPTIONAL", "VERIFY_DISABLE", "DISABLE_VERIFY",
}

// tlsSwitches — ручки, которыми транспорт выключается: по имени либо как
// булев переключатель с упоминанием транспорта.
func tlsSwitches(knobs []config.Knob) []config.Knob {
	var out []config.Knob
	for _, k := range knobs {
		env := strings.ToUpper(k.Env)
		hit := false
		for _, tok := range tlsSwitchTokens {
			if strings.Contains(env, tok) {
				hit = true
				break
			}
		}
		if !hit && k.Kind == reflect.Bool &&
			(strings.Contains(env, "TLS") || strings.Contains(env, "SSL") || strings.Contains(env, "VERIFY")) {
			hit = true
		}
		if hit {
			out = append(out, k)
		}
	}
	return out
}

func TestNotifyHasNoKnobThatDisablesTLS(t *testing.T) {
	t.Parallel()
	knobs := config.Knobs()
	t.Logf("перепись ручек notify: %d", len(knobs))
	if len(knobs) == 0 {
		t.Fatal("перепись ручек пуста: «выключателя нет» на пустом обходе не утверждает ничего")
	}
	for _, k := range knobs {
		if !strings.HasPrefix(k.Env, "KACHO_NOTIFY_") {
			t.Errorf("ручка %s (%s) вне формы имени KACHO_NOTIFY_*", k.Name, k.Env)
		}
		if !strings.HasPrefix(k.Name, "notify.") {
			t.Errorf("имя ручки %q для текста отказа не в форме notify.*", k.Name)
		}
	}
	if found := tlsSwitches(knobs); len(found) > 0 {
		t.Fatalf("в конфигурации notify есть выключатель транспорта: %v", found)
	}
}

// Инъекция: выключатель, добавленный в перепись, — находка; без него — тишина.
func TestTLSSwitchCensusCanFail(t *testing.T) {
	t.Parallel()
	census := config.Knobs()
	if got := tlsSwitches(census); len(got) != 0 {
		t.Fatalf("близнец: настоящая перепись даёт находки %v", got)
	}
	for _, inj := range []config.Knob{
		{Name: "notify.smtp.insecureSkipVerify", Env: "KACHO_NOTIFY_SMTP_INSECURE_SKIP_VERIFY", Kind: reflect.Bool},
		{Name: "notify.smtp.tls", Env: "KACHO_NOTIFY_SMTP_TLS", Kind: reflect.Bool},
		{Name: "notify.peerTLS.enable", Env: "KACHO_NOTIFY_PEER_MTLS_ENABLE", Kind: reflect.Bool},
		{Name: "notify.smtp.startTLSOptional", Env: "KACHO_NOTIFY_SMTP_STARTTLS_OPTIONAL", Kind: reflect.String},
	} {
		got := tlsSwitches(append(append([]config.Knob{}, census...), inj))
		if len(got) != 1 || got[0].Env != inj.Env {
			t.Fatalf("инъекция %s не стала находкой: %v", inj.Env, got)
		}
	}
}
