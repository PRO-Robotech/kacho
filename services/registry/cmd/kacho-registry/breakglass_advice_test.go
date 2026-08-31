// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// breakglass_advice_test.go — отказ старта не советует то, в чём ТОТ ЖЕ процесс
// откажет следующим стражем.
//
// # Предмет
//
// Текст отказа старта — часть контракта ОПЕРАТОРА: по нему в три часа ночи
// выбирают следующий шаг, и другого источника в этот момент нет. Отказ,
// называющий заведомо неисполнимое действие, стоит полного цикла выкатки и
// ожидания раскатки — потраченного на то, что сам продукт знает как невозможное.
//
// Наблюдалось (#1592): в боевом режиме отсутствие адреса iam давало отказ
// «…(or KACHO_REGISTRY_AUTHZ_BREAKGLASS=true to bypass)», а взведённый по этому
// совету breakglass — отказ «production mode (%s): KACHO_REGISTRY_AUTHZ_BREAKGLASS
// must not be enabled» от стража, стоящего в ТОЙ ЖЕ функции ВЫШЕ по тексту.
// Первый отказ рекомендовал ровно то, что второй запрещает, в том же режиме.
//
// # Что здесь утверждается
//
//  1. в боевых режимах отказ НЕ предлагает breakglass и называет ручку, которой
//     нехватка чинится на самом деле;
//  2. в боевых режимах отказ называет РЕЖИМ — иначе оператор не узнает, что
//     правила зависят от него, и повторит попытку;
//  3. в `dev` совет про breakglass ОСТАЁТСЯ (положительный контроль): там он
//     исполним, и снять его значило бы отобрать у разработчика рабочий путь.
//
// Третий пункт — не украшение. Без него утверждение «в боевом нет breakglass»
// зеленело бы на тексте, из которого breakglass вырезан ВЕЗДЕ, то есть на
// починке, ломающей dev.
package main

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
)

// breakglassKnob — ручка, чей совет разбирается. Пишется одной константой:
// поиск подстроки по частям разошёлся бы с текстом молча.
const breakglassKnob = "KACHO_REGISTRY_AUTHZ_BREAKGLASS"

// wiredConfig — посадка, у которой взведено ВСЁ; каждый случай снимает ровно
// одно условие, чтобы отказ был предметным, а не суммой пропусков.
func wiredConfig(mode string) config.Config {
	return config.Config{
		AuthMode:           mode,
		DBSSLMode:          "require",
		AuthZIAMGRPCAddr:   "kacho-iam-internal.kacho.svc:9091",
		PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
	}
}

// TestBootRefusalDoesNotAdviseWhatTheNextGuardRefuses — боевой отказ называет
// исполнимый следующий шаг.
func TestBootRefusalDoesNotAdviseWhatTheNextGuardRefuses(t *testing.T) {
	cases := []struct {
		name string
		// unwire снимает одно условие посадки.
		unwire func(*config.Config)
		// wantKnob — ручка, которой это условие чинится на самом деле.
		wantKnob string
	}{
		{
			name:     "адрес iam не задан",
			unwire:   func(c *config.Config) { c.AuthZIAMGRPCAddr = "" },
			wantKnob: "KACHO_REGISTRY_AUTHZ_IAM_GRPC_ADDR",
		},
		{
			name:     "mTLS публичного листенера выключен",
			unwire:   func(c *config.Config) { c.PublicServerMTLS.Enable = false },
			wantKnob: "KACHO_REGISTRY_PUBLIC_SERVER_MTLS_ENABLE",
		},
		{
			name:     "mTLS внутреннего листенера выключен",
			unwire:   func(c *config.Config) { c.InternalServerMTLS.Enable = false },
			wantKnob: "KACHO_REGISTRY_INTERNAL_SERVER_MTLS_ENABLE",
		},
	}

	for _, mode := range []string{"production", "production-strict"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				cfg := wiredConfig(mode)
				tc.unwire(&cfg)

				err := validateSecurityConfig(cfg)
				if err == nil {
					t.Fatalf("посадка неполна — старт обязан быть отвергнут, получен nil")
				}
				msg := err.Error()

				if strings.Contains(msg, breakglassKnob) {
					t.Errorf("отказ советует %s, который тот же процесс отвергает в режиме %s;\n"+
						"оператор потратит на этот совет полный цикл выкатки. Текст: %q",
						breakglassKnob, mode, msg)
				}
				if !strings.Contains(msg, tc.wantKnob) {
					t.Errorf("отказ не называет ручку %s, которой он чинится; текст: %q", tc.wantKnob, msg)
				}
				if !strings.Contains(msg, mode) {
					t.Errorf("отказ не называет режим %q — оператор не узнает, что правила от него зависят; текст: %q",
						mode, msg)
				}
			})
		}
	}
}

// TestBootRefusalKeepsBreakglassAdviceInDev — положительный контроль к тесту выше.
//
// В `dev` breakglass исполним и остаётся штатным обходом. Проба обязана быть
// парной: отрицание «в боевом breakglass не советуется» само по себе зеленеет на
// тексте, где совета нет НИГДЕ.
func TestBootRefusalKeepsBreakglassAdviceInDev(t *testing.T) {
	cases := []struct {
		name   string
		unwire func(*config.Config)
	}{
		{"адрес iam не задан", func(c *config.Config) { c.AuthZIAMGRPCAddr = "" }},
		{"mTLS публичного листенера выключен", func(c *config.Config) { c.PublicServerMTLS.Enable = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := wiredConfig("dev")
			tc.unwire(&cfg)

			err := validateSecurityConfig(cfg)
			if err == nil {
				t.Fatalf("посадка неполна — старт обязан быть отвергнут, получен nil")
			}
			if !strings.Contains(err.Error(), breakglassKnob) {
				t.Errorf("в dev совет про %s обязан остаться — там он исполним; текст: %q",
					breakglassKnob, err.Error())
			}
		})
	}
}
