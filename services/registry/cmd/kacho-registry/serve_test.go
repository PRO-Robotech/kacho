// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// discardLogger — тихий slog для тестов validateAuthMode (ветки логируют WARN).
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestValidateSecurityConfig — fail-closed гейт security.md: без breakglass ОБА
// листенера обязаны иметь authz-Check (AuthZIAMGRPCAddr!="") И mTLS
// (Public+Internal ServerMTLS.Enable). breakglass=true — полный обход.
func TestValidateSecurityConfig(t *testing.T) {
	bothMTLS := func() config.Config {
		return config.Config{
			AuthMode:           "dev",
			AuthZIAMGRPCAddr:   "kacho-iam-internal.kacho.svc:9091",
			PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
			InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{"all-set-ok", func(*config.Config) {}, false},
		{"breakglass-bypasses-everything-in-dev", func(c *config.Config) {
			// breakglass=true в DEV даже при пустом addr и выключенном mTLS → nil.
			// Аварийный обход остаётся доступным там, где он и задуман.
			c.AuthZBreakglass = true
			c.AuthZIAMGRPCAddr = ""
			c.PublicServerMTLS.Enable = false
			c.InternalServerMTLS.Enable = false
		}, false},
		{"breakglass-in-production-rejected", func(c *config.Config) {
			c.AuthMode = "production"
			c.AuthZBreakglass = true
		}, true},
		{"breakglass-in-production-strict-rejected", func(c *config.Config) {
			c.AuthMode = "production-strict"
			c.AuthZBreakglass = true
		}, true},
		{"empty-iam-addr-rejected", func(c *config.Config) { c.AuthZIAMGRPCAddr = "" }, true},
		{"public-mtls-disabled-rejected", func(c *config.Config) { c.PublicServerMTLS.Enable = false }, true},
		{"internal-mtls-disabled-rejected", func(c *config.Config) { c.InternalServerMTLS.Enable = false }, true},
		{"both-mtls-disabled-rejected", func(c *config.Config) {
			c.PublicServerMTLS.Enable = false
			c.InternalServerMTLS.Enable = false
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := bothMTLS()
			tc.mutate(&cfg)
			err := validateSecurityConfig(cfg)
			if tc.wantErr && err == nil {
				t.Fatalf("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

// TestValidateSecurityConfig_BreakglassInProduction_MessageNamesKnob — registry
// был единственным сервисом, где breakglass не гейтился ВООБЩЕ (ни fatal, ни WARN):
// `if cfg.AuthZBreakglass { return nil }` первым стейтментом снимал и authz-Check,
// и mTLS на ОБОИХ листенерах в любом режиме, включая production-strict.
//
// Assert'им СООБЩЕНИЕ, а не только факт ошибки: причина отказа — часть контракта
// оператора (иначе «почему не стартует» решается перебором).
func TestValidateSecurityConfig_BreakglassInProduction_MessageNamesKnob(t *testing.T) {
	for _, mode := range []string{"production", "production-strict"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{
				AuthMode:           mode,
				AuthZBreakglass:    true,
				AuthZIAMGRPCAddr:   "kacho-iam-internal.kacho.svc:9091",
				PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
				InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
			}
			err := validateSecurityConfig(cfg)
			if err == nil {
				t.Fatalf("breakglass in %s must refuse boot, got nil", mode)
			}
			if !strings.Contains(err.Error(), "KACHO_REGISTRY_AUTHZ_BREAKGLASS") {
				t.Errorf("error must name the offending knob; got: %q", err.Error())
			}
			if !strings.Contains(err.Error(), "production mode") {
				t.Errorf("error must state that the mode is production; got: %q", err.Error())
			}
		})
	}
}

// TestMTLSOnBothListenersIsRefusedInAnyMode — РУБЕЖ, который перевод на носитель
// не отменяет и отменить не вправе.
//
// Носитель судит транспорт слушателей ТОЛЬКО в боевой посадке
// (`servicecontract` О8). Вне её сборщик креденшелов на невзведённой ручке
// отдаёт незашифрованные креды БЕЗ ошибки — и оба слушателя поднялись бы
// открытым текстом, а на незашифрованном слушателе переданная личность
// принимается ОТ ЛЮБОГО: круг отправителей её не сужает, потому что сертификата
// нет вовсе. Поэтому `validateSecurityConfig` остаётся в композиционном корне и
// действует в ЛЮБОМ режиме.
//
// Проба утверждает ОБЕ половины сразу, потому что порознь каждая зеленеет на
// сломанном: исход (dev + выключенный mTLS = отказ, и отказ называет ручку) и
// РАЗМЕЩЕНИЕ (страж зовётся из корня ДО того, как носитель поднимет слушатели).
// Без второй половины страж мог бы остаться правильным и никем не вызванным.
func TestMTLSOnBothListenersIsRefusedInAnyMode(t *testing.T) {
	t.Run("исход: dev с выключенным mTLS не стартует", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			knob string
			off  func(*config.Config)
		}{
			{"public", "KACHO_REGISTRY_PUBLIC_SERVER_MTLS_ENABLE",
				func(c *config.Config) { c.PublicServerMTLS.Enable = false }},
			{"internal", "KACHO_REGISTRY_INTERNAL_SERVER_MTLS_ENABLE",
				func(c *config.Config) { c.InternalServerMTLS.Enable = false }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				cfg := config.Config{
					AuthMode:           "dev",
					AuthZIAMGRPCAddr:   "kacho-iam-internal.kacho.svc:9091",
					PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
					InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
				}
				tc.off(&cfg)
				err := validateSecurityConfig(cfg)
				if err == nil {
					t.Fatalf("dev с выключенным mTLS на %s слушателе принят: на незашифрованном "+
						"слушателе переданную личность принимает ЛЮБОЙ пир — круг отправителей её "+
						"не сужает, потому что сертификата нет вовсе", tc.name)
				}
				if !strings.Contains(err.Error(), tc.knob) {
					t.Fatalf("отказ обязан назвать ручку %s, иначе стенд не поднять: %q", tc.knob, err.Error())
				}
			})
		}
	})

	t.Run("размещение: страж зовётся из корня ДО подъёма слушателей", func(t *testing.T) {
		src, err := os.ReadFile(registryServeSrc)
		if err != nil {
			t.Fatalf("композиционный корень не читается: %v", err)
		}
		root := string(src)
		guard := strings.Index(root, "validateSecurityConfig(cfg)")
		if guard < 0 {
			t.Fatal("композиционный корень больше НЕ ЗОВЁТ validateSecurityConfig: носитель судит " +
				"транспорт слушателей только в боевой посадке, поэтому без этого вызова dev поднимался " +
				"бы открытым текстом на обоих слушателях")
		}
		host := strings.Index(root, "servicehost.Serve(")
		if host < 0 {
			t.Fatal("композиционный корень не поднимает слушатели носителем — предпосылка пробы исчезла")
		}
		if guard > host {
			t.Fatal("страж mTLS зовётся ПОСЛЕ подъёма слушателей: к моменту отказа они уже приняли бы " +
				"соединения открытым текстом")
		}
	})
}

// TestValidateAuthMode — whitelist режимов + строгость DB-SSL. dev/production —
// без SSL-требований; production-strict обязан иметь sslmode require|verify-ca|
// verify-full; неизвестный режим — отказ старта.
func TestValidateAuthMode(t *testing.T) {
	cases := []struct {
		name     string
		authMode string
		sslMode  string
		wantErr  bool
	}{
		{"dev-disable-ok", "dev", "disable", false},
		{"dev-empty-ssl-ok", "dev", "", false},
		{"dev-require-ok", "dev", "require", false},
		{"production-disable-ok", "production", "disable", false},
		{"production-require-ok", "production", "require", false},
		{"prod-strict-require-ok", "production-strict", "require", false},
		{"prod-strict-verify-ca-ok", "production-strict", "verify-ca", false},
		{"prod-strict-verify-full-ok", "production-strict", "verify-full", false},
		{"prod-strict-disable-rejected", "production-strict", "disable", true},
		{"prod-strict-empty-ssl-rejected", "production-strict", "", true},
		{"prod-strict-prefer-rejected", "production-strict", "prefer", true},
		{"unknown-mode-rejected", "bogus", "require", true},
		{"empty-mode-rejected", "", "require", true},
	}
	log := discardLogger()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{AuthMode: tc.authMode, DBSSLMode: tc.sslMode}
			err := validateAuthMode(cfg, log)
			if tc.wantErr && err == nil {
				t.Fatalf("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

// Проверки требований к адресу набора ключей и к пину издателя переехали в
// tokenverifier_test.go вместе со своим предметом: издатель перестал быть
// скаляром, а адрес набора — одним на всех. Здесь они не дублируются — два места
// об одном предмете расходятся молча.

// TestRequireDataplaneTLSAck — data-plane OCI-листенер обслуживает открытый HTTP
// (bearer identity-JWT транзитят по сокету). В production/production-strict молчаливый
// plaintext-старт запрещён: оператор обязан ЯВНО подтвердить внешнюю TLS-терминацию
// (KACHO_REGISTRY_DATAPLANE_TLS_TERMINATED_EXTERNALLY=true), иначе старт отклоняется.
// В dev — no-op (как открытый HTTP у набора ключей и DB sslmode=disable).
// Параллель Config.TokenAcceptance.
func TestRequireDataplaneTLSAck(t *testing.T) {
	cases := []struct {
		name          string
		authMode      string
		tlsTerminated bool
		wantErr       bool
	}{
		{"dev-noack-ok", "dev", false, false},
		{"dev-ack-ok", "dev", true, false},
		{"prod-noack-rejected", "production", false, true},
		{"prod-ack-ok", "production", true, false},
		{"prod-strict-noack-rejected", "production-strict", false, true},
		{"prod-strict-ack-ok", "production-strict", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireDataplaneTLSAck(posture(t, tc.authMode), tc.tlsTerminated)
			if tc.wantErr && err == nil {
				t.Fatalf("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

// TestValidateSecurityConfig_Production_BreakglassRefusesBoot — отказ старта при
// аварийном обходе в боевом режиме, закреплённый ОТДЕЛЬНЫМ утверждением, а не
// строкой таблицы выше.
//
// Свойство то же, и таблица его проверяет; разница в том, что здесь оно
// названо ИМЕНЕМ ФУНКЦИИ. Гейт `audit-list-filter` разрешает провязку
// авторизатора, способного остаться пустым, ровно пока отказ ручки в боевом
// режиме закреплён пробой композиционного корня, и ищет её по имени — подкейс
// таблицы он не видит by construction.
//
// Второе отличие несущее: проба утверждает ИМЯ РУЧКИ в тексте отказа. Оператор,
// упёршийся в отказ старта, обязан прочитать, что именно выключить, — иначе
// стенд не поднять, а отказ станет загадкой вместо диагностики.
func TestValidateSecurityConfig_Production_BreakglassRefusesBoot(t *testing.T) {
	for _, mode := range []string{"production", "production-strict"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{
				AuthMode:           mode,
				AuthZIAMGRPCAddr:   "kacho-iam-internal.kacho.svc:9091",
				PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
				InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
				AuthZBreakglass:    true,
			}
			err := validateSecurityConfig(cfg)
			if err == nil {
				t.Fatalf("аварийный обход в режиме %s обязан отказать в старте, получено nil", mode)
			}
			if !strings.Contains(err.Error(), "KACHO_REGISTRY_AUTHZ_BREAKGLASS") {
				t.Errorf("отказ обязан называть ручку KACHO_REGISTRY_AUTHZ_BREAKGLASS, получено: %q", err.Error())
			}
		})
	}
}

// posture — посадка по её написанию, для табличных проб. Разбор общий, поэтому
// проба утверждает о ТОМ ЖЕ словаре, который читает страж; собственный перевод
// строки в значение был бы третьим местом об одном предмете.
func posture(t *testing.T, raw string) servicecontract.Mode {
	t.Helper()
	mode, err := servicecontract.ParseMode(raw)
	if err != nil {
		t.Fatalf("посадка %q не разбирается общим словарём: %v", raw, err)
	}
	return mode
}
