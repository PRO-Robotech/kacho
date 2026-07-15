// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
)

// quietLogger — slog в /dev/null, чтобы тест не шумел.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// secure — genuinely-secure baseline: authz + mTLS на обоих листенерах + запиненный
// trusted-forwarder SAN (api-gateway SA), без breakglass. Secure-by-default требует
// непустой allow-list форвардеров на любом non-breakglass старте.
func secure() config.Config {
	return config.Config{
		AuthMode:                  "dev",
		AuthZIAMGRPCAddr:          "kacho-iam:9091",
		PublicServerMTLS:          grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS:        grpcsrv.TLSServer{Enable: true},
		AuthZTrustedForwarderSANs: []string{gatewaySAN},
	}
}

// ── validateAuthMode: режим + строгость DB-SSL (authz/mTLS — не здесь) ──

func TestValidateAuthMode(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{"dev", config.Config{AuthMode: "dev"}, false},
		{"dev + ssl disable → ok (dev unaffected)", config.Config{AuthMode: "dev", DBSSLMode: "disable"}, false},
		{"dev + ssl empty → ok (dev unaffected)", config.Config{AuthMode: "dev", DBSSLMode: ""}, false},
		{"production + ssl require → ok", config.Config{AuthMode: "production", DBSSLMode: "require"}, false},
		{"production + ssl verify-full → ok", config.Config{AuthMode: "production", DBSSLMode: "verify-full"}, false},
		{"production + ssl disable → err", config.Config{AuthMode: "production", DBSSLMode: "disable"}, true},
		{"production + ssl empty → err", config.Config{AuthMode: "production", DBSSLMode: ""}, true},
		{"production-strict + ssl require", config.Config{AuthMode: "production-strict", DBSSLMode: "require"}, false},
		{"production-strict + ssl disable → err", config.Config{AuthMode: "production-strict", DBSSLMode: "disable"}, true},
		{"unknown mode → err", config.Config{AuthMode: "wat"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAuthMode(tc.cfg, quietLogger())
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got err=%v", tc.wantErr, err)
			}
		})
	}
}

// ── validateSecurityConfig: secure-by-default; breakglass — единственный обход ──

func TestValidateSecurityConfig(t *testing.T) {
	noMTLS := secure()
	noMTLS.PublicServerMTLS.Enable = false

	noInternalMTLS := secure()
	noInternalMTLS.InternalServerMTLS.Enable = false

	noAuthz := secure()
	noAuthz.AuthZIAMGRPCAddr = ""

	// В production/production-strict пустой allow-list доверенных форвардеров —
	// критичный gap: любой mTLS-verified peer может форвардить произвольного
	// principal'а (confused-deputy до admin-CRUD). Секьюр-гейт обязан отвергать
	// старт без запиненного SAN api-gateway (opt-in trust-any в prod НЕ honored).
	prodNoFwd := secure()
	prodNoFwd.AuthMode = "production"
	prodNoFwd.AuthZTrustedForwarderSANs = nil

	prodWithFwd := secure()
	prodWithFwd.AuthMode = "production"

	prodStrictNoFwd := secure()
	prodStrictNoFwd.AuthMode = "production-strict"
	prodStrictNoFwd.AuthZTrustedForwarderSANs = nil

	prodStrictWithFwd := secure()
	prodStrictWithFwd.AuthMode = "production-strict"

	// Пустая строка в списке — не форвардер (corelib WithTrustedForwarders
	// отбрасывает "" → пустой allow-list → trust-any). Должен отвергаться так же.
	prodEmptyStrFwd := secure()
	prodEmptyStrFwd.AuthMode = "production"
	prodEmptyStrFwd.AuthZTrustedForwarderSANs = []string{""}

	// Secure-by-default: dev с пустым allow-list форвардеров (trust-any) БОЛЬШЕ НЕ
	// стартует молча — нужен либо запиненный SAN, либо ЯВНЫЙ dev-опт-ин
	// AuthZTrustAnyForwarder=true. Пустой список без опт-ина → fail-closed отказ.
	devNoFwd := secure()
	devNoFwd.AuthMode = "dev"
	devNoFwd.AuthZTrustedForwarderSANs = nil

	// dev + пустой allow-list + явный trust-any опт-ин → ok (back-compat escape hatch).
	devTrustAny := secure()
	devTrustAny.AuthMode = "dev"
	devTrustAny.AuthZTrustedForwarderSANs = nil
	devTrustAny.AuthZTrustAnyForwarder = true

	// dev + запиненный SAN (без опт-ина) → ok (secure путь) — это и есть secure().
	devWithFwd := secure()
	devWithFwd.AuthMode = "dev"

	// production + trust-any опт-ин, но БЕЗ реального SAN → всё равно err: опт-ин
	// dev-only, в production trust-any недопустим.
	prodTrustAnyOptIn := secure()
	prodTrustAnyOptIn.AuthMode = "production"
	prodTrustAnyOptIn.AuthZTrustedForwarderSANs = nil
	prodTrustAnyOptIn.AuthZTrustAnyForwarder = true

	// Breakglass — аварийный ПОЛНЫЙ обход authz Check + mTLS. В production posture
	// (production / production-strict) он НЕ honored: один env-флаг молча снял бы
	// всю аутентификацию/авторизацию на развёрнутом стенде, а forged
	// principal-header на plaintext-листенере дал бы admin Region/Zone CRUD
	// (CWE-489). Разрешён ТОЛЬКО вне production (dev / emergency-local).
	bgDev := config.Config{AuthZBreakglass: true, AuthMode: "dev"}
	bgProd := config.Config{AuthZBreakglass: true, AuthMode: "production"}
	bgProdStrict := config.Config{AuthZBreakglass: true, AuthMode: "production-strict"}

	cases := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{"secure (authz + both mTLS) → ok", secure(), false},
		{"no authz addr, no breakglass → err", noAuthz, true},
		{"public mTLS off, no breakglass → err", noMTLS, true},
		{"internal mTLS off, no breakglass → err", noInternalMTLS, true},
		{"breakglass bypasses all requirements → ok", config.Config{AuthZBreakglass: true}, false},
		{"breakglass + dev → ok (emergency-only, non-prod)", bgDev, false},
		{"breakglass + production → err (bypass not honored in prod)", bgProd, true},
		{"breakglass + production-strict → err (bypass not honored in prod)", bgProdStrict, true},
		{"production without trusted forwarders → err", prodNoFwd, true},
		{"production with trusted forwarder → ok", prodWithFwd, false},
		{"production-strict without trusted forwarders → err", prodStrictNoFwd, true},
		{"production-strict with trusted forwarder → ok", prodStrictWithFwd, false},
		{"production with empty-string forwarder (trust-any) → err", prodEmptyStrFwd, true},
		{"production trust-any opt-in without SAN → err (opt-in not honored in prod)", prodTrustAnyOptIn, true},
		{"dev without trusted forwarders, no opt-in → err (secure-by-default)", devNoFwd, true},
		{"dev with explicit trust-any opt-in → ok (back-compat escape hatch)", devTrustAny, false},
		{"dev with pinned SAN → ok (secure path)", devWithFwd, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSecurityConfig(tc.cfg)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got err=%v", tc.wantErr, err)
			}
		})
	}
}
