// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// captureLogger — logger, пишущий в buf (для проверки boot-time WARN'ов).
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// allEdgesSecured возвращает config, у которого КАЖДОЕ реально дозваниваемое
// transport-ребро (оба server-listener'а, project/geo peer-рёбра, authz Check,
// register-drainer) несёт enabled per-edge mTLS. Это единственная конфигурация,
// которую production-strict-гейт обязан пропускать.
func allEdgesSecured() config.Config {
	return config.Config{
		AuthMode:                  "production-strict",
		DBSSLMode:                 "verify-full",
		FGARegisterDrainerEnabled: true,
		PublicServerMTLS:          grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS:        grpcsrv.TLSServer{Enable: true},
		IAMProjectMTLS:            grpcclient.TLSClient{Enable: true},
		GeoMTLS:                   grpcclient.TLSClient{Enable: true},
		IAMAuthzMTLS:              grpcclient.TLSClient{Enable: true},
		IAMRegisterMTLS:           grpcclient.TLSClient{Enable: true},
		AuthZTrustedForwarderSANs: []string{"spiffe://kacho.cloud/ns/kacho-system/sa/kacho-api-gateway"},
		// per-object FGA List-filter active (production fail-closed gate).
		ListFilterEnabled: true,
		AuthZIAMGRPCAddr:  "kaname.kacho.svc:9091",
		// Ребро величин поднимается и защищается наравне с остальными:
		// объявление «не развёрнут» вывело бы его из наблюдения, и ослабление
		// его удостоверения перестало бы что-либо значить.
		QuotaAuthority:     "kaname-internal.kacho.svc:9091",
		QuotaAuthorityMTLS: grpcclient.TLSClient{Enable: true},
	}
}

// production-strict БЕЗ единого включённого per-edge mTLS обязан ПАДАТЬ: гейт
// строится исключительно на реально дозваниваемых transport-рёбрах.
func TestValidateAuthMode_ProductionStrict_AllPerEdgeMTLSDisabledFails(t *testing.T) {
	cfg := config.Config{
		AuthMode:                  "production-strict",
		DBSSLMode:                 "verify-full",
		FGARegisterDrainerEnabled: true, // register-drainer edge active
		// all per-edge mTLS disabled (zero-value Enable:false)
	}
	_, err := validateAuthMode(cfg, discardLogger())
	if err == nil {
		t.Fatalf("expected production-strict gate to reject config with all per-edge mTLS disabled")
	}
	// error must name the insecure edges, not the dead knob.
	for _, want := range []string{
		"PUBLIC_SERVER_MTLS", "INTERNAL_SERVER_MTLS",
		"IAM_PROJECT_MTLS", "GEO_MTLS", "IAM_AUTHZ_MTLS", "IAM_REGISTER_MTLS",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("gate error must name insecure edge %q; got: %v", want, err)
		}
	}
}

// production-strict со ВСЕМИ per-edge mTLS enabled обязан ПРОХОДИТЬ.
func TestValidateAuthMode_ProductionStrict_AllPerEdgeMTLSPasses(t *testing.T) {
	cfg := allEdgesSecured()
	prod, err := validateAuthMode(cfg, discardLogger())
	if err != nil {
		t.Fatalf("expected production-strict to pass with all per-edge mTLS enabled; got err: %v", err)
	}
	if !prod {
		t.Errorf("production-strict must report productionMode=true")
	}
}

// Каждое отдельное отключённое ребро должно валить гейт (по одному).
func TestValidateAuthMode_ProductionStrict_EachEdgeRequired(t *testing.T) {
	cases := map[string]func(*config.Config){
		"PUBLIC_SERVER_MTLS":   func(c *config.Config) { c.PublicServerMTLS.Enable = false },
		"INTERNAL_SERVER_MTLS": func(c *config.Config) { c.InternalServerMTLS.Enable = false },
		"IAM_PROJECT_MTLS":     func(c *config.Config) { c.IAMProjectMTLS.Enable = false },
		"GEO_MTLS":             func(c *config.Config) { c.GeoMTLS.Enable = false },
		"IAM_AUTHZ_MTLS":       func(c *config.Config) { c.IAMAuthzMTLS.Enable = false },
		"IAM_REGISTER_MTLS":    func(c *config.Config) { c.IAMRegisterMTLS.Enable = false },
	}
	for edge, disable := range cases {
		t.Run(edge, func(t *testing.T) {
			cfg := allEdgesSecured()
			disable(&cfg)
			_, err := validateAuthMode(cfg, discardLogger())
			if err == nil {
				t.Fatalf("expected gate to reject config with %s disabled", edge)
			}
			if !strings.Contains(err.Error(), edge) {
				t.Errorf("gate error must name disabled edge %q; got: %v", edge, err)
			}
		})
	}
}

// SkipPeerValidation снимает требование на project/geo peer-рёбра (они не
// дозваниваются) — но живёт эта ветка теперь ТОЛЬКО в dev: в production обоих
// видов сам выключатель отказывает в старте
// (TestValidateAuthMode_Production_SkipPeerValidationRefusesBoot). Здесь
// проверяется, что dev-предупреждение о небезопасном транспорте не жалуется на
// рёбра, которых при выключателе нет: иначе оператор dev-стенда получал бы
// требование включить mTLS на проводе, который никто не поднимает.
func TestValidateAuthMode_Dev_SkipPeerValidationDropsPeerEdgesFromWarning(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.AuthMode = "dev"
	cfg.SkipPeerValidation = true
	cfg.IAMProjectMTLS.Enable = false
	cfg.GeoMTLS.Enable = false

	var buf bytes.Buffer
	if _, err := validateAuthMode(cfg, captureLogger(&buf)); err != nil {
		t.Fatalf("dev must not gate transport at all; got: %v", err)
	}
	for _, edge := range []string{"IAM_PROJECT_MTLS", "GEO_MTLS"} {
		if strings.Contains(buf.String(), edge) {
			t.Errorf("peer edge %s is not dialed under SkipPeerValidation; the dev warning must not name it: %s", edge, buf.String())
		}
	}
}

// AuthZBreakglass в production-strict ОТКАЗЫВАЕТ старту (раньше — только снимал
// требование на authz-ребро и проходил). См.
// TestValidateAuthMode_Production_BreakglassRefusesBoot ниже.
func TestValidateAuthMode_ProductionStrict_BreakglassRefusesBoot(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.AuthZBreakglass = true
	cfg.IAMAuthzMTLS.Enable = false
	if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
		t.Fatalf("breakglass in production-strict must refuse boot, got nil")
	}
}

// FGARegisterDrainerEnabled=false снимает требование на register-drainer ребро.
func TestValidateAuthMode_ProductionStrict_DrainerDisabledDropsRegisterEdge(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.FGARegisterDrainerEnabled = false
	cfg.IAMRegisterMTLS.Enable = false
	if _, err := validateAuthMode(cfg, discardLogger()); err != nil {
		t.Fatalf("register-drainer disabled; gate must not require IAM_REGISTER_MTLS: %v", err)
	}
}

// DBSSLMode-проверка сохранена.
func TestValidateAuthMode_ProductionStrict_DBSSLModeStillEnforced(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.DBSSLMode = "disable"
	if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
		t.Fatalf("expected gate to reject DBSSLMode=disable in production-strict")
	}
}

// production ОБЯЗАН отказать в старте, если allow-list доверенных forwarder'ов
// (KACHO_COMPUTE_AUTHZ_TRUSTED_FORWARDER_SANS) пуст: с пустым списком
// principalIsTrusted доверяет forwarded x-kacho-principal-* ЛЮБОМУ mTLS-verified
// peer'у — любой sibling с валидным mesh-cert'ом форжит end-user principal и
// авторизуется как жертва (CWE-441/CWE-290 confused deputy → tenant crossing).
// Finding: «Production mode does not require a trusted-forwarder allow-list».
func TestValidateAuthMode_Production_RequiresTrustedForwarders(t *testing.T) {
	cfg := securedProduction()
	cfg.AuthZTrustedForwarderSANs = nil
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("production must reject an unnarrowed circle (any mTLS peer trusted as forwarder → subject spoofing)")
	}
	if !strings.Contains(err.Error(), "AUTHZ_TRUSTED_FORWARDER_SANS") {
		t.Errorf("gate error must name the empty forwarder allow-list; got: %v", err)
	}
	// с непустым кругом — стартует.
	cfg.AuthZTrustedForwarderSANs = []string{"spiffe://kacho.cloud/ns/kacho-system/sa/kacho-api-gateway"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("production with a narrowed circle must pass; got: %v", err)
	}
}

// Вне боевого режима круг тоже гейтится — но пустой круг остаётся возможным как
// ЯВНЫЙ опт-ин. Стража, молчащая вне боевого режима, ни разу не исполняется на
// локальном стенде, и «забыл выставить круг» находится только на боевом профиле.
func TestValidate_Dev_RefusesAnUnnarrowedCircleWithoutTheOptIn(t *testing.T) {
	cfg := securedProduction()
	cfg.AuthMode = "dev"
	cfg.AuthZTrustedForwarderSANs = nil

	err := cfg.Validate()
	if err == nil {
		t.Fatal("dev с несуженным кругом и без опт-ина обязан отказать в старте")
	}
	if !strings.Contains(err.Error(), "AUTHZ_TRUST_ANY_FORWARDER") {
		t.Errorf("отказ обязан назвать ручку опт-ина, иначе стенд не поднять; got: %v", err)
	}

	// Положительный контроль: без него отрицание зеленело бы и на «отказывать всегда».
	cfg.AuthZTrustAnyForwarder = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("явный опт-ин обязан пропускать локальную фикстуру; got: %v", err)
	}
}

// Опт-ин НЕ действует в боевом режиме: иначе он был бы ручкой, снимающей защиту
// на развёрнутом стенде.
func TestValidate_Production_IgnoresTheDevOptIn(t *testing.T) {
	cfg := securedProduction()
	cfg.AuthZTrustedForwarderSANs = nil
	cfg.AuthZTrustAnyForwarder = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("боевой режим обязан отказать на несуженном круге даже с опт-ином")
	}
}

// production-strict — тот же fail-closed forwarder-гейт.
func TestValidateAuthMode_ProductionStrict_RequiresTrustedForwarders(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.AuthZTrustedForwarderSANs = nil
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("production-strict must reject an unnarrowed circle")
	}
	if !strings.Contains(err.Error(), "AUTHZ_TRUSTED_FORWARDER_SANS") {
		t.Errorf("gate error must name the empty forwarder allow-list; got: %v", err)
	}
}

// production ОБЯЗАН отказать в старте, если per-object FGA List-фильтр можно
// молча выключить: KACHO_COMPUTE_LIST_FILTER_ENABLED=false ИЛИ пустой
// KACHO_COMPUTE_AUTHZ_IAM_GRPC_ADDR (→ authzConn nil → handler bypass'ит фильтр).
// Без фильтра principal с project-tier viewer видит ВСЕ Disk/Image/Snapshot/
// Instance проекта, включая объекты без per-object v_get (over-show / BOLA-lite,
// CWE-862 / OWASP A01). Fail-closed зеркалит requireDBSSLMode /
// Config.Validate → grpcsrv.TrustedForwarders.Require.
func TestValidateAuthMode_Production_RequiresListFilter(t *testing.T) {
	// master-switch off → отказ.
	cfg := securedProduction()
	cfg.ListFilterEnabled = false
	if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
		t.Fatalf("production must reject LIST_FILTER_ENABLED=false (public List bypasses per-object FGA filter)")
	} else if !strings.Contains(err.Error(), "LIST_FILTER_ENABLED") {
		t.Errorf("gate error must name the disabled list-filter switch; got: %v", err)
	}

	// authz endpoint unset → authzConn nil → фильтр не строится → отказ.
	cfg = securedProduction()
	cfg.AuthZIAMGRPCAddr = ""
	if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
		t.Fatalf("production must reject empty AUTHZ_IAM_GRPC_ADDR (authzConn nil → List filter disabled)")
	} else if !strings.Contains(err.Error(), "AUTHZ_IAM_GRPC_ADDR") {
		t.Errorf("gate error must name the missing authz endpoint; got: %v", err)
	}

	// enabled + endpoint set → стартует.
	if _, err := validateAuthMode(securedProduction(), discardLogger()); err != nil {
		t.Fatalf("production with list-filter enabled + authz endpoint must pass; got: %v", err)
	}
}

// production-strict — тот же fail-closed list-filter гейт.
func TestValidateAuthMode_ProductionStrict_RequiresListFilter(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.ListFilterEnabled = false
	if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
		t.Fatalf("production-strict must reject LIST_FILTER_ENABLED=false")
	} else if !strings.Contains(err.Error(), "LIST_FILTER_ENABLED") {
		t.Errorf("gate error must name the disabled list-filter switch; got: %v", err)
	}

	cfg = allEdgesSecured()
	cfg.AuthZIAMGRPCAddr = ""
	if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
		t.Fatalf("production-strict must reject empty AUTHZ_IAM_GRPC_ADDR")
	} else if !strings.Contains(err.Error(), "AUTHZ_IAM_GRPC_ADDR") {
		t.Errorf("gate error must name the missing authz endpoint; got: %v", err)
	}
}

// dev не требует ни mTLS, ни SSL (insecure dev-defaults только логируются).
func TestValidateAuthMode_DevNoGate(t *testing.T) {
	prod, err := validateAuthMode(config.Config{AuthMode: "dev",
		QuotaAuthority: corequota.NotDeployed}, discardLogger())
	if err != nil {
		t.Fatalf("dev must not enforce any transport gate; got err: %v", err)
	}
	if prod {
		t.Errorf("dev must report productionMode=false")
	}
}

// securedProduction — минимально-валидный "production": оба server-листенера под
// mTLS + TLS-DB. Peer-рёбра (iam/geo/authz/register) — plaintext (послабление
// plain production относительно production-strict: они mesh-encrypted).
func securedProduction() config.Config {
	return config.Config{
		AuthMode:                  "production",
		DBSSLMode:                 "require",
		PublicServerMTLS:          grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS:        grpcsrv.TLSServer{Enable: true},
		AuthZTrustedForwarderSANs: []string{"spiffe://kacho.cloud/ns/kacho-system/sa/kacho-api-gateway"},
		// per-object FGA List-filter active (production fail-closed gate).
		ListFilterEnabled: true,
		AuthZIAMGRPCAddr:  "kaname.kacho.svc:9091",
		// Транспорт ребра проверки прав: с тех пор как страж требует его в ОБОИХ
		// боевых режимах, окружение без этой ручки боевым не является. Фикстура,
		// снисходительнее продукта, делает невидимым ровно тот дефект, ради
		// которого её подставляют, — поэтому измерение выставлено и здесь, а
		// ослабляется только в своей пробе.
		IAMAuthzMTLS: grpcclient.TLSClient{Enable: true},
		// Объявление домена величин — часть законной посадки: у ручки ровно два
		// законных значения, и незаданное среди них не значится. Здесь стоит
		// «не развёрнут», потому что эта отправная точка ребра величин не
		// поднимает: адрес без удостоверения был бы ВТОРЫМ ослаблением, и
		// красное перестало бы означать то, что объявлено пробой. Посадку с
		// поднятым ребром величин проверяет allEdgesSecured().
		QuotaAuthority: corequota.NotDeployed,
	}
}

// production ОБЯЗАН отказать в старте с plaintext-листенерами: forwarded
// principal доверяется на plaintext → subject spoofing / tenant crossing
// (CWE-290). Раньше это гейтилось только в production-strict. Finding «Non-strict
// production AuthMode leaves listeners plaintext … subject spoofing».
func TestValidateAuthMode_Production_RequiresListenerMTLS(t *testing.T) {
	// оба листенера plaintext (+ валидный DBSSL, чтобы изолировать listener-гейт).
	cfg := config.Config{AuthMode: "production", DBSSLMode: "require",
		QuotaAuthority: corequota.NotDeployed}
	_, err := validateAuthMode(cfg, discardLogger())
	if err == nil {
		t.Fatalf("production must reject plaintext listeners (forwarded principal spoofing)")
	}
	for _, want := range []string{"PUBLIC_SERVER_MTLS_ENABLE", "INTERNAL_SERVER_MTLS_ENABLE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("gate error must name insecure listener %q; got: %v", want, err)
		}
	}

	// по одному листенеру — тоже отказ.
	for _, disable := range []func(*config.Config){
		func(c *config.Config) { c.PublicServerMTLS.Enable = false },
		func(c *config.Config) { c.InternalServerMTLS.Enable = false },
	} {
		c := securedProduction()
		disable(&c)
		if _, err := validateAuthMode(c, discardLogger()); err == nil {
			t.Errorf("production must reject a single plaintext listener")
		}
	}
}

// production ОБЯЗАН отказать при sslmode=disable — DB-креды/строки идут открытым
// текстом (CWE-319). Раньше SSL-проверка была только в production-strict. Finding
// «DB connection allows sslmode=disable outside production-strict».
func TestValidateAuthMode_Production_RequiresDBSSL(t *testing.T) {
	for _, bad := range []string{"", "disable"} {
		cfg := securedProduction()
		cfg.DBSSLMode = bad
		if _, err := validateAuthMode(cfg, discardLogger()); err == nil {
			t.Errorf("production must reject KACHO_COMPUTE_DB_SSLMODE=%q", bad)
		}
	}
	// require/verify-ca/verify-full — принимаются.
	for _, ok := range []string{"require", "verify-ca", "verify-full"} {
		cfg := securedProduction()
		cfg.DBSSLMode = ok
		if _, err := validateAuthMode(cfg, discardLogger()); err != nil {
			t.Errorf("production must accept DBSSLMode=%q; got err: %v", ok, err)
		}
	}
}

// breakglass в production — ОТКАЗ СТАРТА (fail-closed), а не громкий WARN.
//
// Раньше compute (и vpc) лишь предупреждали, тогда как geo и nlb отвергали: одна и
// та же настройка означала «сервис поднят без авторизации вообще» в одних сервисах
// и «сервис не поднимется» в других. WARN не защищает — leftover breakglass после
// инцидента переживает рестарт и оставляет ОБА листенера без per-RPC Check
// (object-self v_get/v_update/v_delete и cross-tenant Check не оцениваются),
// нарушая инвариант security.md «AuthN+AuthZ ВЕЗДЕ» + kacho core rule «production-mode обязателен ВЕЗДЕ».
//
// Assert'им СООБЩЕНИЕ, а не только факт ошибки: причина отказа — часть контракта
// оператора (он должен понять, что снимать, а не гадать).
func TestValidateAuthMode_Production_BreakglassRefusesBoot(t *testing.T) {
	for _, mode := range []string{"production", "production-strict"} {
		t.Run(mode, func(t *testing.T) {
			var cfg config.Config
			if mode == "production-strict" {
				cfg = allEdgesSecured()
			} else {
				cfg = securedProduction()
			}
			cfg.AuthMode = mode
			cfg.AuthZBreakglass = true
			// production-strict иначе потребует IAM_AUTHZ_MTLS; breakglass его снимал.
			cfg.IAMAuthzMTLS.Enable = false

			var buf bytes.Buffer
			_, err := validateAuthMode(cfg, captureLogger(&buf))
			if err == nil {
				t.Fatalf("breakglass in %s must refuse boot (fail-closed), got nil", mode)
			}
			msg := err.Error()
			if !strings.Contains(msg, "KACHO_COMPUTE_AUTHZ_BREAKGLASS") {
				t.Errorf("error must name the offending knob KACHO_COMPUTE_AUTHZ_BREAKGLASS; got: %q", msg)
			}
			if !strings.Contains(strings.ToLower(msg), "production") {
				t.Errorf("error must state that the mode is production; got: %q", msg)
			}
		})
	}
}

// dev-режим breakglass'ом не затронут: аварийный обход остаётся доступным там, где
// он и задуман (in-process фикстуры / локальная отладка), — ужесточение не должно
// превращаться в запрет самого механизма.
func TestValidateAuthMode_Dev_BreakglassAllowed(t *testing.T) {
	var cfg config.Config
	cfg.AuthMode = "dev"
	cfg.AuthZBreakglass = true
	if _, err := validateAuthMode(cfg, discardLogger()); err != nil {
		t.Fatalf("breakglass in dev must remain allowed; got: %v", err)
	}
}

// Обратная сторона: без breakglass production НЕ должен эмитить breakglass-WARN
// (иначе алерт-шум / притупление внимания к реальному emergency-обходу).
func TestValidateAuthMode_Production_NoBreakglassNoWarn(t *testing.T) {
	var buf bytes.Buffer
	if _, err := validateAuthMode(securedProduction(), captureLogger(&buf)); err != nil {
		t.Fatalf("secured production must pass; got: %v", err)
	}
	if strings.Contains(strings.ToLower(buf.String()), "breakglass") {
		t.Errorf("production WITHOUT breakglass must not emit a breakglass WARN; got log: %q", buf.String())
	}
}

// production с mTLS-листенерами + TLS-DB, но plaintext peer-рёбрами — ПРОХОДИТ:
// это осознанная разница ladder'а plain production vs production-strict (peer
// строгий mTLS требует именно strict).
func TestValidateAuthMode_Production_PeerEdgesNotRequired(t *testing.T) {
	cfg := securedProduction()
	// peer-рёбра явно plaintext + активны.
	cfg.FGARegisterDrainerEnabled = true
	prod, err := validateAuthMode(cfg, discardLogger())
	if err != nil {
		t.Fatalf("plain production must not require peer-edge mTLS; got err: %v", err)
	}
	if !prod {
		t.Errorf("production must report productionMode=true")
	}
}

// Список из одних ПУСТЫХ записей (`SANS=","`) для corelib не существует:
// grpcsrv.WithTrustedForwarders принимает только s != "", поэтому такой список
// вырождается там в пустое множество — то есть снова «доверяем ЛЮБОМУ пиру с
// проверенным сертификатом». Стража, считающая длину сырого среза, этого не видит:
// значение из одной запятой проходит гейт, и сервис стартует, доверяя всем.
//
// Тот же предикат (s != "") уже применяет самоотчёт о посадке (hasNonEmpty), так
// что после правки «стража пропустила» и «отчёт говорит: круг сужен» не могут
// разъехаться.
func TestValidateAuthMode_Production_RejectsBlankOnlyTrustedForwarders(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  func() config.Config
	}{
		{"production", securedProduction},
		{"production-strict", allEdgesSecured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			cfg.AuthZTrustedForwarderSANs = []string{"", ""}
			err := cfg.Validate()
			if err == nil {
				t.Fatal("a list of blank entries passed the guard: corelib drops empty strings, " +
					"so the resulting allow-list is empty and every certificate-verified peer " +
					"may forward an end-user identity")
			}
			if !strings.Contains(err.Error(), "AUTHZ_TRUSTED_FORWARDER_SANS") {
				t.Errorf("gate error must name the knob; got: %v", err)
			}
		})
	}
}

// Стража list-фильтра обязана охранять НЕ ТОЛЬКО его наличие, но и его отказ.
//
// KACHO_COMPUTE_LIST_FILTER_FAIL_OPEN=true переводит фильтр в degraded-режим:
// на ЛЮБОЙ ошибке обращения к iam страница отдаётся НЕотфильтрованной
// (authzfilter.handleErr → «return ids, nil»; поведение зафиксировано в
// TestFGAFilter_FailOpenReturnsPage и TestInstanceHandler_List_IAMDown_FailOpen).
// То есть при включённой ручке достаточно недоступности соседа, чтобы
// principal с project-tier viewer увидел ВСЕ Instance/MachineType проекта —
// ровно тот over-show, ради которого фильтр и требуется (CWE-862).
//
// Прежняя стража проверяла только master-switch и адрес endpoint'а, поэтому
// конфигурация «фильтр включён, но открыт при отказе» проходила молча: контроль
// присутствовал, а свойство, которое он охраняет, не гарантировалось.
// Симметрично AUTHZ_BREAKGLASS — аварийный обход не живёт в production.
func TestValidateAuthMode_Production_RejectsListFilterFailOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  func() config.Config
	}{
		{"production", securedProduction},
		{"production-strict", allEdgesSecured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			cfg.ListFilterFailOpen = true
			_, err := validateAuthMode(cfg, discardLogger())
			if err == nil {
				t.Fatal("a fail-open list filter passed the guard: on any authz error the page " +
					"is returned unfiltered, so the per-object gate the guard exists to enforce " +
					"is bypassed by an unreachable peer alone")
			}
			if !strings.Contains(err.Error(), "LIST_FILTER_FAIL_OPEN") {
				t.Errorf("gate error must name the knob; got: %v", err)
			}
		})
	}
}

// Обратная сторона той же стражи: fail-closed конфигурация (ручка выключена)
// обязана стартовать. Без этого утверждения предыдущий тест удовлетворялся бы
// стражей, отвергающей вообще всё.
func TestValidateAuthMode_Production_AcceptsFailClosedListFilter(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  func() config.Config
	}{
		{"production", securedProduction},
		{"production-strict", allEdgesSecured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg()
			cfg.ListFilterFailOpen = false
			if _, err := validateAuthMode(cfg, discardLogger()); err != nil {
				t.Fatalf("fail-closed list filter must boot; got: %v", err)
			}
		})
	}
}

// В dev аварийный degraded-режим остаётся доступен — там он и задуман
// (паритет с AUTHZ_BREAKGLASS, который production отвергает, а dev допускает).
func TestValidateAuthMode_Dev_AllowsListFilterFailOpen(t *testing.T) {
	cfg := config.Config{AuthMode: "dev", ListFilterFailOpen: true,
		QuotaAuthority: corequota.NotDeployed}
	if _, err := validateAuthMode(cfg, discardLogger()); err != nil {
		t.Fatalf("dev must keep the emergency degraded mode available; got: %v", err)
	}
}

// KACHO_COMPUTE_SKIP_PEER_VALIDATION снимает КАЖДУЮ кросс-сервисную проверку
// сразу (существование проекта, существование зоны, когерентность размещения
// подсети интерфейса, высвобождение интерфейса и тома на удалении): dialPeers
// подменяет все peer-клиенты no-op-заглушками, для которых любой чужой
// идентификатор «существует». Это аварийный dev-выключатель того же рода, что
// AuthZBreakglass, и обязан вести себя так же — production ОТКАЗЫВАЕТ В СТАРТЕ.
func TestValidateAuthMode_Production_SkipPeerValidationRefusesBoot(t *testing.T) {
	cfg := securedProduction()
	cfg.SkipPeerValidation = true
	_, err := validateAuthMode(cfg, discardLogger())
	if err == nil {
		t.Fatalf("skip-peer-validation in production must refuse boot, got nil")
	}
	if !strings.Contains(err.Error(), "KACHO_COMPUTE_SKIP_PEER_VALIDATION") {
		t.Errorf("refusal must name the knob so the operator can act on it; got: %v", err)
	}
}

// То же в production-strict: там выключатель вдобавок СНИМАЛ требование mTLS с
// project/geo/vpc-рёбер, поэтому его молчаливое принятие ослабляло и соседний гейт.
func TestValidateAuthMode_ProductionStrict_SkipPeerValidationRefusesBoot(t *testing.T) {
	cfg := allEdgesSecured()
	cfg.SkipPeerValidation = true
	_, err := validateAuthMode(cfg, discardLogger())
	if err == nil {
		t.Fatalf("skip-peer-validation in production-strict must refuse boot, got nil")
	}
	if !strings.Contains(err.Error(), "KACHO_COMPUTE_SKIP_PEER_VALIDATION") {
		t.Errorf("refusal must name the knob; got: %v", err)
	}
}

// dev — единственный режим, где выключатель задуман; там он остаётся доступен.
func TestValidateAuthMode_Dev_SkipPeerValidationAllowed(t *testing.T) {
	if _, err := validateAuthMode(config.Config{AuthMode: "dev", SkipPeerValidation: true,
		QuotaAuthority: corequota.NotDeployed}, discardLogger()); err != nil {
		t.Fatalf("dev must keep the skip-peer-validation escape available; got: %v", err)
	}
}

// Наблюдаемый уровень: процесс НЕ стартует. runServe обязан вернуть ошибку
// раньше, чем поднимет хоть один листенер или откроет пул к БД — то есть отказ
// виден как невозможность запуска, а не как WARN в логе живого процесса.
func TestRunServe_SkipPeerValidationInProduction_RefusesToStart(t *testing.T) {
	cfg := securedProduction()
	cfg.SkipPeerValidation = true

	done := make(chan error, 1)
	go func() { done <- runServe(cfg) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("runServe must refuse to start with skip-peer-validation in production")
		}
		if !strings.Contains(err.Error(), "KACHO_COMPUTE_SKIP_PEER_VALIDATION") {
			t.Errorf("startup refusal must name the knob; got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("runServe did not refuse to start — the process came up with every cross-service check disabled")
	}
}

// ── транспорт ребра, несущего решение о доступе ──

// Ребро compute→iam несёт РЕШЕНИЕ о доступе (per-RPC Check) и переданную личность
// вызывающего. Невзведённая ручка не даёт ошибки сама по себе: клиентские creds
// вырождаются в insecure БЕЗ ошибки, процесс поднимается, отчитывается «authz
// enabled», и каждый Check уходит по открытому каналу.
//
// Требование действует в ОБОИХ боевых режимах. Прежде оно стояло только в
// строгом, и это было записанным послаблением: обычный production не является
// более слабой посадкой — это та же посадка, а страж, не срабатывающий в ней,
// есть контроль, чья ветка не исполнялась ни разу.
//
// Проверяется в обе стороны: невзведённое ребро → отказ с именем ручки;
// взведённое → молчание (страж не запрещает законное).
func TestValidateAuthMode_Production_RequiresAuthzEdgeTransport(t *testing.T) {
	cfg := securedProduction()
	cfg.IAMAuthzMTLS = grpcclient.TLSClient{Enable: false}

	_, err := validateAuthMode(cfg, discardLogger())
	if err == nil {
		t.Fatal("plain production must refuse an unarmed transport on the authz Check edge")
	}
	if !strings.Contains(err.Error(), "IAM_AUTHZ_MTLS_ENABLE") {
		t.Fatalf("the refusal must name the knob the operator has to set; got: %v", err)
	}

	// Законный близнец той же формы: ребро взведено — страж молчит.
	cfg.IAMAuthzMTLS = grpcclient.TLSClient{Enable: true}
	if _, err := validateAuthMode(cfg, discardLogger()); err != nil {
		t.Fatalf("an armed authz edge must boot; got: %v", err)
	}
}

// Ребро, которого НЕТ, требовать не за что: без адреса composition root его не
// поднимает вовсе. Страж обязан читать тот же предикат, что и проводка, — иначе
// он либо запрещает законное, либо пропускает открытый канал.
func TestValidateAuthMode_Production_AuthzEdgeNotDialled_NoRequirement(t *testing.T) {
	cfg := securedProduction()
	cfg.AuthZIAMGRPCAddr = ""
	cfg.IAMAuthzMTLS = grpcclient.TLSClient{Enable: false}
	// Фильтр видимости требует того же адреса, поэтому изолируем измерение.
	cfg.ListFilterEnabled = false

	if _, err := validateAuthMode(cfg, discardLogger()); err != nil &&
		strings.Contains(err.Error(), "IAM_AUTHZ_MTLS_ENABLE") {
		t.Fatalf("страж требует транспорт ребра, которое не поднимается: %v", err)
	}
}
