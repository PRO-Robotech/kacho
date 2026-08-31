// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/soheilhy/cmux"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"

	// Регистрация errdetails-типов в protoregistry — иначе protojson не
	// разворачивает Any в BadRequest.FieldViolations / ResourceInfo при
	// marshalling InvalidArgument-ответов в JSON, и клиент видит только
	// "failed to marshal error message".
	_ "google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/observability"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"
	"github.com/PRO-Robotech/kacho/pkg/subjectchange"

	// Обслуживается только нативный API kacho.cloud.*.

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/health"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
	"github.com/PRO-Robotech/kacho/gateway/internal/opsproxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/restmux"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Посадка процесса — ЧЕРЕЗ ЦЕНТРАЛЬНЫЙ ДЕСКРИПТОР, и до первого исходящего
	// соединения (задача продукта #1407). Раньше набора рёбер: страж, стоящий
	// после дозвона до соседей, судит посадку, в которой процесс уже говорит.
	posture, postureErr := describePosture(cfg, logger)
	if postureErr != nil {
		log.Fatalf("посадка процесса: %v", postureErr)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// SIGHUP — operator-driven reload signal for the permission catalog +
	// authz overrides. The signal handler is wired up after the authz
	// middleware is constructed (see `installAuthzSIGHUP` below).
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)

	// --- Backend connections: один постоянный ClientConn на backend ---
	// Активные backends: iam + vpc + compute (+ их internal-порты).
	// Account/Project обслуживает kacho-iam.
	// loadbalancer заморожен — dial не выполняется. grpc.NewClient ленив:
	// фактическое соединение устанавливается при первом RPC, поэтому отсутствие
	// еще-не-задеплоенного backend не валит запуск.
	//
	// Each backend dial selects its per-edge transport creds (mTLS
	// client-cert when KACHO_API_GATEWAY_MTLS_<EDGE>_ENABLE=true + cert material
	// present, else insecure — dev backward-compat). The "operation" self-loopback
	// stays always-insecure (in-process re-entry). Fail-fast on misconfig
	// (enabled edge w/o cert material) so the process never starts half-secured.
	backends, closeBackends, err := dialBackends(cfg)
	if err != nil {
		log.Fatalf("backend dial: %v", err)
	}
	defer closeBackends()

	// --- IAM subject client (gRPC-direct к kacho-iam:9091 для LookupSubject) ---
	// gRPC-direct ЗДЕСЬ — чтобы не рекурсировать через собственный middleware:
	// этот клиент зовут из auth-интерсептора, и пойти к себе же по REST значило бы
	// снова войти в цепочку, которая его и вызвала (loop-prevention).
	//
	// НЕ потому, что REST-маршрута нет: `internal/restmux/mux.go` регистрирует
	// InternalIAMService на ВНУТРЕННЕМ mux (`/iam/v1/internal/iam:lookupSubject`,
	// `:check`), и на внешний listener они не попадают — это делает HasInternalSuffix
	// у gRPC-роутера и `isInternalRoute` у REST. Прежняя редакция утверждала
	// «НЕ регистрируется в restmux»; это неверно и уже вводило в заблуждение.
	// Это ребро gateway→iam → те же MTLS_IAM_ENABLE creds, что и у iam
	// backend conns. Fail-fast on misconfig.
	iamSubjectCreds, err := iamEdgeDialCreds(cfg, cfg.IAMInternalAddr)
	if err != nil {
		log.Fatalf("iam subject mTLS creds: %v", err)
	}
	iamSubjectClient, err := clients.NewIAMSubjectClient(cfg.IAMInternalAddr, logger, iamSubjectCreds)
	if err != nil {
		log.Fatalf("iam subject client: %v", err)
	}
	defer func() { _ = iamSubjectClient.Close() }()

	authInterceptor := middleware.NewAuthInterceptor(
		// Ручка названа ЗДЕСЬ, а не спрятана за методом конфигурации: место сборки
		// обязано показывать, чем компонент настраивается, иначе он выглядит
		// ненастраиваемым ни одним профилем (`gateway/deploy/producerless_input_test.go`).
		config.PostureOf(cfg.AuthNMode),
		cfg.AuthNDevSecret,
		iamSubjectClient,
		logger,
	)

	// ПОЛОСА БАЗОВОГО СЕКРЕТА (#1142). Авторитет — тот же внутренний слушатель
	// iam, та же связь, тот же якорь доверия и то же окно вердикта: своей
	// величины полоса НЕ заводит.
	//
	// Провязка безусловна намеренно. Полоса, объявленная и не провязанная, —
	// мёртвый контроль: строка с нашей маркой уходила бы прочими полосами и
	// отвергалась бы как негодный подписанный токен, то есть отказом не той
	// природы, и заметить это можно было бы только по жалобе клиента.
	basicLane := middleware.NewBasicCredentialLane(
		middleware.NewBasicAuthorityFromStub(iamSubjectClient.BasicCredentialStub()),
	).WithLogger(logger)
	authInterceptor = authInterceptor.WithBasicCredentialLane(basicLane)
	logger.Info("basic credential lane wired",
		"authority", cfg.IAMInternalAddr,
		"verdict_window", middleware.BasicCredentialVerdictWindow.String(),
		// Потолок объявляется при старте: «сколько там записей» обязано быть
		// известно ДО того, как рост станет предметом разбора (#1218).
		"verdict_cache_capacity", basicLane.CacheStats().Capacity)

	// Kratos session-based auth для SPA (cookie ory_kratos_session).
	// Env KACHO_API_GATEWAY_KRATOS_PUBLIC_URL — base URL Kratos public API.
	// Default = cluster-internal kratos-public service.
	kratosURL := cfg.KratosPublicURL
	if kratosURL != "disabled" {
		authInterceptor = authInterceptor.WithKratos(middleware.NewKratosClient(kratosURL))
		logger.Info("kratos session-auth wired", "kratos_url", kratosURL)
	} else {
		logger.Info("kratos session-auth disabled by env")
	}

	// --- Hydra JWKS verifier wired into the principal-setting path ---
	//
	// The same JWTVerifier is the authoritative validator for Hydra-issued
	// RS256 access JWTs. It is constructed here (independent of the DPoP
	// feature flag) and wired into the AuthInterceptor so a real login token
	// authenticates on the principal path.
	// The DPoP middleware (below) reuses the SAME instance when enabled.
	//
	// Construction failure (e.g. empty resolved JWKS URL) is a MISCONFIGURATION,
	// not an outage: the constructor reads configuration and makes no network
	// call, so the same start can never succeed until the address and issuer are
	// set. It is therefore judged by a guard — fatal in a production-class env,
	// the previous warn-and-continue only under an explicit dev-class label.
	// Absorbed unconditionally, as it was, it made a permanent misconfiguration
	// the normal running mode: the edge reported itself as configured and
	// refused nothing for as long as it lived (security.md §8).
	// Хоп за ключами получает СВОЙ якорь доверия, ровно как административный. Пусто ⇒
	// транспорт по умолчанию (прежнее поведение); нечитаемая связка ⇒ ОТКАЗ В СТАРТЕ,
	// а не тихий откат к системным корням: край, который «настроен проверять» и не
	// проверяет, — худшее из состояний, потому что снаружи неотличим от исправного.
	jwksHopClient, jwksCAErr := newJWKSHopClient(
		cfg.HydraJWKSCAFile, time.Duration(cfg.JWKSFetchTimeoutSeconds)*time.Second)
	if jwksCAErr != nil {
		logger.Error("api-gateway refusing to start", "err", jwksCAErr)
		os.Exit(1)
	}
	// ОБЪЯВЛЕНИЕ ПРИЁМА — кого принимаем и откуда берём набор проверочных
	// ключей КАЖДОГО принимаемого издателя.
	//
	// Разбор и стражи живут в config (tokenissuers.go), а не здесь, по той же
	// причине, что у ПЕРВОЙ конфигурации проверяющего: предикат нужен ДВУМ
	// читателям — процессу при старте и пробе развёртывания, которая спрашивает
	// у профиля ровно то, что спросит процесс. Второй до main не дотягивается
	// by construction, поэтому предикат, оставленный здесь, пришлось бы
	// сформулировать заново — и он разошёлся бы молча.
	//
	// Отказ БЕЗУСЛОВЕН и не проходит через дев-послабление соседнего стража:
	// вырожденный перечень издателей означает «принимаем любого», а это
	// посадка, запрещённая в любом режиме.
	acceptance, accErr := cfg.TokenAcceptance()
	if accErr != nil {
		logger.Error("api-gateway refusing to start: token acceptance declaration", "err", accErr)
		os.Exit(1)
	}
	issuerRecords := make([]middleware.IssuerKeySet, 0, len(acceptance))
	acceptedIssuers := make([]string, 0, len(acceptance))
	platformAccepted := false
	for _, b := range acceptance {
		issuerRecords = append(issuerRecords, middleware.IssuerKeySet{
			Issuer:                  b.Issuer,
			KeySetURL:               b.KeySetURL,
			TokenTypes:              b.TokenTypes,
			TolerateAbsentTokenType: b.TolerateAbsentTokenType,
			ReadRevocation:          b.ReadRevocation,
		})
		acceptedIssuers = append(acceptedIssuers, b.Issuer)
		platformAccepted = platformAccepted || b.ReadRevocation
	}

	jwtVerifier, jverr := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		Issuers:          issuerRecords,
		JWKSCacheTTL:     time.Duration(cfg.JWKSCacheTTLSeconds) * time.Second,
		JWKSFetchTimeout: time.Duration(cfg.JWKSFetchTimeoutSeconds) * time.Second,
		HTTPClient:       jwksHopClient,
		ExpectedAudience: cfg.ExpectedAudience(),
		ClockSkew:        time.Duration(cfg.JWTClockSkewSeconds) * time.Second,
	})
	if tvErr := validateProductionTokenVerifierConfig(cfg.AppEnv, jverr); tvErr != nil {
		log.Fatalf("token verifier startup-validation: %v", tvErr)
	}
	if jverr != nil {
		logger.Warn("jwks verifier not wired into principal path (HMAC-dev only)",
			"err", jverr, "accepted_issuers", acceptedIssuers)
	} else {
		authInterceptor = authInterceptor.WithVerifier(jwtVerifier)
		logger.Info("token verifier wired into principal path",
			"accepted_issuers", acceptedIssuers,
			"platform_issuer_accepted", platformAccepted)
	}

	// Hybrid external listener: when enabled, a client that presents a
	// valid Kachō cert over the external listener (tls.VerifyClientCertIfGiven,
	// wired on the TLS listener below) authenticates on its mTLS SPIFFE identity —
	// the AuthInterceptor derives the principal from the verified cert and skips
	// the JWT requirement. Default off ⇒ JWT-only authN, behaviour unchanged.
	if cfg.HybridMTLSEnabled() {
		authInterceptor = authInterceptor.WithMTLSPrincipal(true)
		logger.Info("hybrid mTLS external listener: cert-principal path enabled")
	}

	// Machine principals are exempt from step-up (a machine has no second
	// factor). That exemption is only defensible if the machine's token is
	// protected some OTHER way — proof-of-possession. When enabled, an unbound
	// (plain-bearer) machine token is rejected 401 on both surfaces. Default off
	// until the provider mints bound tokens; see the config field's godoc for
	// the rollout order.
	authInterceptor = authInterceptor.WithRequireMachineTokenBinding(
		cfg.AuthNRequireMachineTokenBinding)
	if cfg.AuthNRequireMachineTokenBinding {
		logger.Info("machine principals must present a sender-constrained token (cnf)")
	} else {
		logger.Info("machine token binding NOT required " +
			"(set KACHO_API_GATEWAY_AUTHN_REQUIRE_MACHINE_TOKEN_BINDING=true once issuance mints bound tokens)")
	}

	logger.Info("auth-interceptor configured",
		"mode", cfg.AuthNMode,
		"iam_internal_addr", cfg.IAMInternalAddr,
		"dev_secret_set", cfg.AuthNDevSecret != "",
		"jwks_verifier_set", jverr == nil)

	// --- Revocation path: refuse to boot production without its addresses ---
	//
	// A verified signature says who minted the token and when it expires; it says
	// nothing about whether the token is still good. Only the identity provider
	// knows that, and only over its ADMIN API — which is a different Service and
	// port from the public issuer, so neither address can be worked out from what
	// the gateway already has. Left unset, both controls are simply off: no
	// request is ever checked for revocation, and signing out does not end the
	// provider-side session. Refuse rather than run without them.
	// AdminCAFile is part of what this guard JUDGES: it refuses the start when the
	// hop is https and no anchor is pinned. Omitting it here does not weaken the
	// guard, it makes it unsatisfiable — the field stays at its zero value, so the
	// answer is "nothing pinned" whatever the operator configured, and the refusal
	// names the knob that is already set. Locked by
	// admin_hop_wiring_test.go::TestCompositionRoot_FeedsTheTrustAnchorToTheRevocationGuard.
	//
	// ПОСАДКА ЛИЧНОСТИ подаётся тем же стражем (задача #1125): она разводит
	// требование АДМИНИСТРАТИВНОГО адреса, и только его. Негодное значение
	// отвергается здесь же — откат к «безопасному» не производится, потому что
	// безопасного среди двух значений нет: каждое снимает требования другого.
	identityLane, ipErr := cfg.ResolvedIdentityProvider()
	if ipErr != nil {
		log.Fatalf("identity posture startup-validation: %v", ipErr)
	}
	if rvErr := validateProductionRevocationConfig(cfg.AppEnv, RevocationConfig{
		IntrospectionURL: cfg.ResolvedHydraIntrospectionURL(),
		AdminURL:         cfg.ResolvedHydraAdminURL(),
		AdminCAFile:      cfg.HydraAdminCAFile,
		IdentityProvider: identityLane,
	}); rvErr != nil {
		log.Fatalf("revocation config startup-validation: %v", rvErr)
	}

	// --- Revocation check: mounted on the authN layer that always runs ---
	//
	// It used to be wired inside the sender-constrained-token middleware below,
	// which is gated by KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP — a toggle no profile
	// sets. So the check was configured, guarded and deployed, and never once
	// asked. Turning that toggle on is not the way to fix it: it would also start
	// DEMANDING proof-of-possession, which issuance does not yet mint (see
	// AuthNRequireMachineTokenBinding), so it would refuse every machine
	// credential. Whether a token was revoked is a question about ANY token, so
	// it belongs on the layer every request passes through.
	// One client for BOTH calls to the provider's admin API — introspection here
	// and the logout session-kill below. They address the same host, so a trust
	// anchor configured for one and not the other would be a difference nobody
	// intended. Built before either consumer so an unusable anchor stops the
	// process at the composition root rather than at the first request.
	adminHopClient, ahErr := newAdminHopClient(
		cfg.HydraAdminCAFile,
		time.Duration(cfg.IntrospectionTimeoutMs)*time.Millisecond)
	if ahErr != nil {
		log.Fatalf("admin API client: %v", ahErr)
	}
	if strings.TrimSpace(cfg.HydraAdminCAFile) != "" {
		logger.Info("admin API hop verified against a pinned trust anchor",
			"ca_file", cfg.HydraAdminCAFile)
	}

	if introspectionURL := cfg.ResolvedHydraIntrospectionURL(); introspectionURL != "" {
		revocationCache, rcErr := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
			HydraIntrospectionURL: introspectionURL,
			HTTPClient:            adminHopClient,
			MaxEntries:            cfg.IntrospectionCacheSize,
			TTL:                   time.Duration(cfg.IntrospectionCacheTTLSeconds) * time.Second,
			Timeout:               time.Duration(cfg.IntrospectionTimeoutMs) * time.Millisecond,
		})
		if rcErr != nil {
			log.Fatalf("revocation check: %v", rcErr)
		}
		// ИСТОЧНИКОВ ОТЗЫВА ДВА, И СПРАШИВАЮТСЯ ОБА (#797).
		//
		// Провайдер знает о своих отзывах и об истечении срока. О записи, которую
		// делает НАШ выход — по идентификатору удостоверения, — он не знает и
		// знать не может. До этой провязки наш отзыв не участвовал в решении на
		// пути запроса вовсе: он писался и читался только административными
		// путями, то есть выход записывал намерение, а не прекращал доступ.
		//
		// Соединение к iam уже поднято выше (dialBackends) и является
		// критическим: без него край не обслуживает ни одного запроса, потому
		// что iam фронтит и личность, и права. Поэтому отдельной ветки «а вдруг
		// его нет» здесь не заводится — она была бы веткой, в которой край всё
		// равно не работает.
		var revocationChecker middleware.TokenRevocationChecker = revocationCache
		if iamConn := backends["iamInternal"]; iamConn != nil {
			revocationChecker = middleware.NewLocalThenProviderRevocation(
				clients.NewSessionRevocationsAdapter(iamConn), revocationCache)
		}
		authInterceptor = authInterceptor.WithRevocationCheck(revocationChecker, 0)
		logger.Info("revocation check active on the authN path",
			"sources", "own record + provider introspection",
			"cache_ttl_s", cfg.IntrospectionCacheTTLSeconds,
			"cache_entries", cfg.IntrospectionCacheSize,
			"per_call_timeout_ms", cfg.IntrospectionTimeoutMs)
	} else {
		// Production-class environments never reach this branch — the guard above
		// refuses to start. A dev stand may legitimately have no admin API to ask.
		logger.Warn("revocation check NOT mounted: no introspection endpoint configured; "+
			"a revoked token stays usable until it expires on its own",
			"knob", "KACHO_HYDRA_INTROSPECTION_URL")
	}

	// ─── ОТЗЫВ НАШИХ ТОКЕНОВ — У НАС (Ф1б, задача #926) ─────────────────────
	//
	// Полоса отзыва — свойство ЗАПИСИ ИЗДАТЕЛЯ, а не настройки процесса: прежний
	// провайдер о наших токенах не знает by construction, и его ответ на наш
	// токен есть утверждение о предмете, которого у него нет. Поэтому читатель
	// второй, и живёт он РЯДОМ, а не вместо.
	//
	// # Почему этот блок стоит СНАРУЖИ ветки прежнего провайдера
	//
	// Он стоял внутри неё, и это делало невыразимой посадку, к которой фаза и
	// ведёт: «принимаем ТОЛЬКО нашего издателя». Такой профиль не задаёт адреса
	// прежнего провайдера — задавать нечего, — и наш читатель не провязывался
	// вовсе, а следом старт отвергался. Отказ был честный, но отвергал он не
	// ошибку оператора, а состояние, которое обязано быть законным: возможность,
	// объявленная и неисполнимая ни при каком входе, — тот же класс, что поле,
	// которое требуют и прислать нельзя.
	//
	// Читатели независимы, потому что независимы их предметы: у каждого свой
	// авторитет, свой якорь доверия, свой счётчик и своя семантика молчания.
	if platformAccepted {
		platformHopClient, phErr := newPlatformRevocationHopClient(
			cfg.PlatformTokenRevocationCAFile,
			cfg.PlatformTokenRevocationCertFile,
			cfg.PlatformTokenRevocationKeyFile,
			time.Duration(cfg.IntrospectionTimeoutMs)*time.Millisecond)
		if phErr != nil {
			log.Fatalf("platform revocation authority client: %v", phErr)
		}
		platformCache, pcErr := middleware.NewIntrospectionCache(middleware.IntrospectionCacheConfig{
			HydraIntrospectionURL: cfg.PlatformTokenRevocationURL,
			HTTPClient:            platformHopClient,
			MaxEntries:            cfg.IntrospectionCacheSize,
			TTL:                   time.Duration(cfg.IntrospectionCacheTTLSeconds) * time.Second,
			Timeout:               time.Duration(cfg.IntrospectionTimeoutMs) * time.Millisecond,
		})
		if pcErr != nil {
			// Наш издатель принимается, а спросить о его токенах некого. Отказ
			// при СТАРТЕ, а не отказ каждому запросу: первый виден оператору,
			// второй — арендатору.
			log.Fatalf("platform revocation check: %v", pcErr)
		}
		authInterceptor = authInterceptor.WithPlatformRevocationCheck(platformCache, 0)
		logger.Info("revocation of OUR OWN tokens is read on presentation",
			"authority_pinned", strings.TrimSpace(cfg.PlatformTokenRevocationCAFile) != "",
			// Личность на хопе — в самоотчёте, а не только в настройках:
			// авторитет спрашивает сертификат, и «предъявлять нечем» обязано
			// быть видно ДО первого отказа арендатору.
			"client_identity_presented", strings.TrimSpace(cfg.PlatformTokenRevocationCertFile) != "",
			"cache_ttl_s", cfg.IntrospectionCacheTTLSeconds,
			"unanswered_verdict", "refuse")
	}

	// ─── НАШ ОТЗЫВ ЧИТАЕТСЯ И НА БРАУЗЕРНОЙ ПОЛОСЕ (#1122) ─────────────────
	//
	// Полос личности человека здесь ДВЕ, и до этой провязки они объявляли разное.
	// Полоса предъявителя спрашивала про отзыв — свой и чужой. Полоса cookie не
	// спрашивала НИЧЕГО, и разницу никто не решал: обоснование звучало «сессия
	// перепроверяется у провайдера на каждом запросе». Про отзывы САМОГО
	// провайдера это верно; про запись, которую делает НАШ глагол выхода и
	// административный принудительный выход, — неверно и не может быть верным:
	// тот, у кого спрашивают про сессию, о нашей записи не знает by construction.
	//
	// Наблюдаемое следствие: администратор получал успех, а человек продолжал
	// работать в консоли.
	//
	// Отдельной ветки «а вдруг соединения нет» здесь не заводится по той же
	// причине, что у блока выше: соединение к службе прав критическое — без него
	// край не обслуживает ни одного запроса, потому что она фронтит и личность, и
	// права. Ветка была бы веткой, в которой край всё равно не работает.
	if kratosURL != "disabled" {
		if iamConn := backends["iamInternal"]; iamConn != nil {
			authInterceptor = authInterceptor.WithSessionCutoffCheck(
				clients.NewSessionRevocationsAdapter(iamConn), 0)
			logger.Info("session revocation is read on the browser lane",
				"keyed_by", "subject + authentication instant",
				"unanswered_verdict", "refuse",
				"revoked_verdict", "refuse and end the carrier")
		}
	}

	// --- Per-RPC authentication floor, on the layer that always runs ---
	//
	// The catalog says, per RPC, how strongly a caller must have authenticated.
	// That demand used to be applied only inside the sender-constrained-token
	// middleware below, which mounts behind a toggle no profile sets — so it held
	// nowhere, while the catalog, the identity service's mirror of it and the
	// cluster-internal arm all read as if it did. Same reasoning as the revocation
	// check above: proof-of-possession is a property of SOME tokens and may sit
	// behind a toggle; how strongly the caller authenticated is a property of
	// EVERY token and belongs where every request passes.
	stepUpCatalog, scErr := middleware.LoadEmbeddedPermissionCatalog(cfg.AuthZPermissionCatalogFile)
	stepUpFloors := -1 // unread catalog is an unknown, never a zero — see the guard
	if scErr != nil {
		logger.Error("permission catalog unreadable; the authentication floor cannot be applied",
			"err", scErr)
	} else {
		stepUpFloors = countDeclaredACRFloors(stepUpCatalog)
	}
	stepUpMounted := false
	if cfg.AuthNEnforceStepUp && scErr == nil && jverr == nil && jwtVerifier != nil {
		authInterceptor = authInterceptor.WithStepUp(
			middleware.NewStepUpGate(time.Now),
			middleware.NewCatalogPermissionLookup(stepUpCatalog),
			middleware.NewRestRouter(),
		)
		stepUpMounted = authInterceptor.StepUpMounted()
	}
	if suErr := validateProductionStepUpConfig(cfg.AppEnv, StepUpConfig{
		DeclaredFloors: stepUpFloors,
		Enforced:       stepUpMounted,
	}); suErr != nil {
		log.Fatalf("step-up startup-validation: %v", suErr)
	}
	if stepUpMounted {
		logger.Info("authentication floor active on the authN path",
			"catalog_entries", stepUpCatalog.Size(), "entries_with_floor", stepUpFloors)
	} else {
		// Reachable only outside a production-class env — the guard above refuses
		// otherwise. A local unit fixture may legitimately run without it.
		logger.Warn("authentication floor NOT applied: the per-RPC assurance demand in the "+
			"permission catalog holds nowhere in this process",
			"knob", stepUpEnforceKnob)
	}

	// --- DPoP / JWT verifier / mTLS-bound / step-up gate ---
	//
	// All wiring is feature-gated by KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP.
	// When disabled (default) the legacy auth-interceptor path remains the only
	// authN code path. When enabled we add a second middleware after the legacy
	// one — verified Hydra-issued tokens flow through it; dev / Kratos / HMAC
	// tokens pass through unchanged (they're not in JWT alg whitelist and the
	// JWT verifier rejects them gracefully → middleware passes through as
	// anonymous when requireForAllRequests=false).
	var dpopMiddleware *middleware.DPoPMiddleware
	var cnfGRPCInterceptor *middleware.CnfBindingInterceptor
	// ХРАНИЛИЩЕ ОДНОКРАТНОСТИ СТРОИТСЯ ЗДЕСЬ, а не ниже по файлу: его же
	// запись держит однократность предъявления доказательства владения
	// (DPoP), и страж этой проверки собирается парой строк ниже. Порядок
	// здесь — не стиль: страж, собранный раньше хранилища, получил бы память
	// процесса при живом общем хранилище, и различие увидела бы только вторая
	// реплика — то есть никто (#909).
	if pairErr := validateIdempotencyFleetPairing(IdempotencyPairing{
		StoreKind: cfg.IdempotencyStoreKind,
		DSN:       cfg.IdempotencyDSN,
		FleetSize: cfg.FleetSize,
	}); pairErr != nil {
		log.Fatalf("idempotency store startup-validation: %v", pairErr)
	}
	idempStore, sharedReplayStore, idempCloser, idempErr := buildIdempotencyStore(context.Background(), cfg, logger)
	if idempErr != nil {
		log.Fatalf("idempotency store: %v", idempErr)
	}
	if idempCloser != nil {
		defer func() { _ = idempCloser.Close() }()
	}

	if cfg.AuthNEnableDPoP {
		var verifierErr error
		// Reuse the SAME verifier instance already wired into the
		// AuthInterceptor (single JWKS cache, one source of truth). If its
		// construction failed above, DPoP cannot run either — fail-fast.
		if jverr != nil {
			log.Fatalf("jwt verifier (required by DPoP): %v", jverr)
		}
		verifier := jwtVerifier
		// ОДНОКРАТНОСТЬ ПРЕДЪЯВЛЕНИЯ — свойство ФЛОТА, а не процесса.
		//
		// Пока запись о предъявленном доказательстве живёт в памяти, обещание
		// верно ровно до второй реплики: перехваченное доказательство, поданное
		// в соседний под, записи не находит и проходит. Отказа при этом не
		// происходит и следа не остаётся (#909).
		//
		// Общее хранилище берётся ТО ЖЕ, что держит ключ однократности: второй
		// пул к той же базе ради того же предмета был бы вторым местом об одном.
		// Его отсутствие законно ровно при флоте в одну реплику — и это
		// проверено при старте, парой строк выше.
		replayTTL := cfg.DPoPReplayTTL()
		var replayGuard middleware.DPoPReplayGuard
		if sharedReplayStore != nil {
			replayGuard = newDPoPReplayStore(sharedReplayStore, replayTTL, dpopReplayStoreTimeout)
			logger.Info("dpop replay guard: shared, spans the whole fleet",
				"ttl", replayTTL, "fleet_size", cfg.FleetSize)
		} else {
			replayGuard = middleware.NewDPoPReplayCache(middleware.DPoPReplayCacheConfig{
				MaxEntries: cfg.DPoPReplayCacheSize,
				TTL:        replayTTL,
			})
			logger.Info("dpop replay guard: in this process only, valid for a single replica",
				"ttl", replayTTL, "fleet_size", cfg.FleetSize)
		}
		dpopValidator, derr := middleware.NewDPoPValidator(middleware.DPoPValidatorConfig{
			ReplayCache:  replayGuard,
			IatFreshness: time.Duration(cfg.DPoPIatFreshnessSeconds) * time.Second,
		})
		if derr != nil {
			log.Fatalf("dpop validator: %v", derr)
		}
		stepUp := middleware.NewStepUpGate(time.Now)

		// Step-up (ACR) gate keys on the per-RPC `required_acr_min` from the
		// permission catalog, resolved from the REST (method, path) via the
		// generated route table. The catalog is the one loaded above, where the
		// floor is applied unconditionally; this arm re-states it for the
		// sender-constrained path and decides nothing the layer above did not.
		if scErr != nil {
			log.Fatalf("step-up permission catalog: %v", scErr)
		}

		// No revocation wiring here on purpose: the check is mounted above, on the
		// layer that always runs. Asking again in this middleware would introspect
		// the same token twice per request whenever this toggle is on.
		dpopMiddleware, verifierErr = middleware.NewDPoPMiddleware(middleware.DPoPMiddlewareConfig{
			Verifier:              verifier,
			DPoP:                  dpopValidator,
			MTLS:                  middleware.NewMTLSBoundValidator(),
			StepUp:                stepUp,
			PermissionLookup:      middleware.NewCatalogPermissionLookup(stepUpCatalog),
			RestRouter:            middleware.NewRestRouter(),
			Logger:                logger,
			APIDomain:             cfg.APIDomain,
			RequireForAllRequests: cfg.Posture() == middleware.AuthModeProductionStrict,
		})
		if verifierErr != nil {
			log.Fatalf("dpop middleware: %v", verifierErr)
		}

		// Native gRPC surface: the REST DPoPMiddleware enforces cnf-binding only
		// on the HTTP path; the gRPC interceptor chain does not inspect cnf. Wire
		// a gRPC interceptor that mirrors it so a sender-constrained (DPoP- or
		// mTLS-bound) token cannot be replayed as a plain bearer over native gRPC
		// (CWE-294). Reuses the SAME JWKS verifier instance.
		cnfGRPCInterceptor, verifierErr = middleware.NewCnfBindingInterceptor(
			verifier, middleware.NewMTLSBoundValidator(), logger)
		if verifierErr != nil {
			log.Fatalf("cnf grpc interceptor: %v", verifierErr)
		}

		logger.Info("dpop-mw wired",
			"api_domain", cfg.APIDomain,
			"accepted_issuers", acceptedIssuers,
			"audience", cfg.ExpectedAudience(),
			"stepup_catalog_entries", stepUpCatalog.Size(),
		)
	} else {
		logger.Info("dpop-mw disabled (set KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP=true to enable)")
	}

	// --- logout handler ---
	//
	// The endpoint is intentionally exempt from the mandatory DPoP/authz
	// middleware (a user must be able to drop their browser session even with an
	// expired token). Because of that exemption the handler itself must
	// authenticate the caller before any server-side revocation: it verifies the
	// presented access token via the SAME JWKS verifier used on the principal
	// path and revokes ONLY the caller's own subject. Without a wired verifier
	// (jverr != nil, e.g. empty JWKS URL) revocation fails closed (401); only
	// cookie clearing remains.
	var logoutVerifier handler.CallerVerifier
	if jverr == nil {
		logoutVerifier = logoutVerifierAdapter{v: jwtVerifier}
	}
	logoutHandler, lerr := handler.NewLogoutHandler(handler.LogoutHandlerConfig{
		Logger:        logger,
		Verifier:      logoutVerifier,
		Revocations:   clients.NewSessionRevocationsAdapter(backends["iamInternal"]),
		HydraAdminURL: cfg.ResolvedHydraAdminURL(),
		// Same client as the introspection hop: same host, same trust anchor.
		// Without this the session kill would keep dialing on the system root
		// store, so an operator who moved the hop to TLS would find revocation
		// verified and sign-out silently failing on every logout.
		HTTPClient:      adminHopClient,
		HookSharedToken: cfg.HookSharedSecret,
	})
	if lerr != nil {
		log.Fatalf("logout handler: %v", lerr)
	}

	// --- AuthZ middleware (per-RPC enforcement) ---
	//
	// Pipeline order (after DPoP/JWT/mTLS/step-up):
	//   DPoP → JWT → mTLS-bound → Step-up → AUTHZ → handler
	//
	// All wiring is feature-gated by KACHO_API_GATEWAY_AUTHZ_ENABLED.
	// When false the middleware mounts as a no-op pass-through.
	var authzMW *middleware.AuthzMiddleware
	// authz — накопители, которые собирает та же сборка. Читает их коллектор
	// диагностической поверхности (ниже); объявлены здесь, потому что сборка
	// живёт в блоке, а поверхность поднимается за его пределами.
	var authz authzWiring
	{
		// Refuse to start if authz is disabled or fail-open in
		// any production-class environment (prod / production / staging). The
		// KACHO_APP_ENV signal is emitted from the helm overlay via extraEnv
		// (see kacho-deploy values.prod.yaml). Non-prod envs are tolerated and
		// surfaced via the WARN log below.
		appEnv := cfg.AppEnv
		if vErr := validateProductionAuthzConfig(appEnv, AuthzMiddlewareConfig{
			Enabled:      cfg.AuthZEnabled,
			FailOpen:     cfg.AuthZFailOpen,
			AuthNMode:    cfg.AuthNMode,
			DevSecretSet: cfg.AuthNDevSecret != "",
			// Окно отзыва — четвёртая ось того же контура. До этого она лишь
			// печаталась строкой `authz-mw wired` ниже: величина была видна и не
			// судилась.
			RevocationWindow: time.Duration(cfg.AuthZCacheTTLSeconds) * time.Second,
		}); vErr != nil {
			log.Fatalf("authz config startup-validation: %v", vErr)
		}
		// In the explicit dev-class envs (dev/local/test) surface relaxed config as
		// a structured warning so operators see it in pod logs without grepping
		// env-vars manually. An empty/unset env is NOT dev-class — it is
		// production-class and already hard-failed above when relaxed.
		switch appEnv {
		case "dev", "local", "test":
			if !cfg.AuthZEnabled || cfg.AuthZFailOpen {
				logger.Warn("authz config relaxed for non-prod env",
					"env", appEnv,
					"enabled", cfg.AuthZEnabled,
					"fail_open", cfg.AuthZFailOpen,
				)
			}
		}

		// Fail-open surfacing: KACHO_APP_ENV keys the fatal production-guard above.
		// An empty/unset env is now production-class, so a relaxed posture under an
		// unset env already hard-fails; this WARN additionally flags a relaxed
		// posture on the EXTERNAL advertised TLS edge (CWE-1188). The external
		// listener is a strong
		// "this is reachable from outside the cluster" signal, so emit a loud
		// startup WARN whenever it is enabled together with a relaxed posture,
		// independent of the env label — the operator sees the fail-open edge in pod
		// logs instead of it being invisible.
		if cfg.TLSEnabled() && (!cfg.AuthZEnabled || cfg.Posture() == middleware.AuthModeDev) {
			logger.Warn("SECURITY: external TLS edge enabled with a relaxed auth posture",
				"tls_listen_addr", cfg.TLSListenAddr,
				"authz_enabled", cfg.AuthZEnabled,
				"authn_mode", cfg.AuthNMode,
				"hint", "set KACHO_API_GATEWAY_AUTHZ_ENABLED=true and KACHO_API_GATEWAY_AUTHN_MODE=production-strict for the advertised endpoint",
			)
		}

		authz, err = buildAuthzMiddleware(cfg, logger)
		if err != nil {
			log.Fatalf("authz middleware: %v", err)
		}
		authzMW = authz.mw
		if cfg.AuthZEnabled {
			logger.Info("authz-mw wired",
				"iam_authorize_url", cfg.ResolvedIAMAuthorizeURL(),
				"cache_ttl_s", cfg.AuthZCacheTTLSeconds,
				"cache_max", cfg.AuthZCacheMaxEntries,
				"check_timeout_ms", cfg.AuthZCheckTimeoutMs,
				"fail_open", cfg.AuthZFailOpen,
				"app_env", appEnv,
				"catalog_override_file", cfg.AuthZPermissionCatalogFile,
				"overrides_file", cfg.AuthZOverridesFile,
				"trusted_xff", cfg.AuthZTrustedXForwardedFor,
			)
		} else {
			logger.Info("authz-mw disabled (set KACHO_API_GATEWAY_AUTHZ_ENABLED=true to enable)")
		}
	}

	// --- cluster-internal диагностическая поверхность (GET /metrics) ---
	//
	// Она входит в контур ОТДЕЛЬНЫМ ПРОФИЛЕМ (`pkg/servicecontract.Surface` +
	// `pkg/servicehost.ServeSurface`), как у семи остальных процессов
	// платформы: корень приносит сюда ОБЪЯВЛЕНИЕ, а подъём, самоотчёт и
	// гашение принадлежат профилю.
	//
	// Регистрация коллектора — ЕДИНСТВЕННОЕ место, где величины решения о
	// доступе выходят из процесса. Снимите её — и десять полос решения исчезнут с
	// поверхности, а не станут нулями; ровно это ловит проба
	// `TestUnregisteredCollectorIsRed`, а провязку в дереве — гейт
	// `TestDeclaredAccumulatorsHaveANonTestReader`.
	diagMetrics := gwmetrics.New(buildVersion, buildCommit)

	// Измеритель задержки обслуженного вызова — ОДИН на процесс, полос у него
	// две (внешний слушатель и внутренний).
	//
	// Край — единственная поверхность платформы, которую не поднимает носитель
	// входящего пути, поэтому отказ старта О13 (`servicecontract.New`) сюда не
	// достаёт и провязка живёт здесь. Измеритель тот же, что у семи сервисов:
	// ряды края ложатся в общее семейство, и вопрос «где во всей платформе вырос
	// хвост» остаётся одним запросом — а край стоит перед КАЖДЫМ обращением
	// арендатора, поэтому без его ряда картина неполна ровно в том месте, куда
	// смотрят первым.
	edgeLatency, elErr := grpcsrv.NewServerLatency(diagMetrics.Registerer())
	if elErr != nil {
		// Отказ подъёма, а не предупреждение: он означает несогласованное
		// объявление серии, и поднять край значило бы отдать ему диагностическую
		// поверхность без семейства, которого на ней не будет никогда.
		logger.Error("api-gateway refusing to start: измеритель задержки обслуженного вызова "+
			"не заводится в реестре края", "err", elErr)
		os.Exit(1)
	}
	// Заполнение кэша вердиктов базовой полосы — ТРЕТЬЯ величина сверх двух уже
	// объявленных (#1221). Объявление потолка при старте и однократное
	// предупреждение при его достижении отвечают «дошли ли», и ни одно —
	// «насколько близко и как быстро растём». Потолок сегодня константа, и
	// превратить его в ручку должно НАБЛЮДЕНИЕ: пока величины нет, решение
	// принимать не на чем.
	//
	// Провязка безусловна: полоса собрана выше безусловно, и величина, никем не
	// читаемая, считалась бы в никуда — её ноль не утверждал бы ничего.
	diagMetrics.RegisterBasicCredentialCache(basicLane.CacheStats)
	// Отставание уборки записей однократности предъявления (#1293). Провязка
	// условна ровно потому, что условна сама таблица: флот в одну реплику
	// обходится памятью процесса, и убирать тогда нечего. Там, где таблица есть,
	// её рост задаёт ПРЕДЪЯВИТЕЛЬ — внешняя сторона без границы темпа, — поэтому
	// «уборка догоняет» обязано быть видно величиной, а не выводиться из того,
	// что никто не жаловался.
	if sharedReplayStore != nil {
		diagMetrics.RegisterDPoPReplaySweep(sharedReplayStore.DPoPSweepStats)
		// Отставание уборки записей однократности (#1302) — ТОТ ЖЕ носитель и та
		// же условность: обе таблицы живут в общем хранилище, которого при флоте
		// в одну реплику нет вовсе. Строку в неё пишет каждая мутация с ключом
		// однократности, то есть темп задаёт ВЫЗЫВАЮЩИЙ, и «уборка догоняет»
		// обязано быть видно величиной, а не выводиться из того, что никто не
		// жаловался.
		diagMetrics.RegisterIdempotencyReap(sharedReplayStore.ReapSweepStats)
	}
	diagMetrics.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		snap := gwmetrics.AuthzSnapshot{Counts: authz.metrics.Counts()}
		if authz.calls != nil {
			snap.ClientCalls = authz.calls.CallsTotal()
		}
		return snap
	})
	diagDesc, diagDescErr := describeDiagnosticSurface(
		cfg.MetricsAddr, diagMetrics, posture.Spec().Mode, logger)
	if diagDescErr != nil {
		log.Fatalf("профиль диагностической поверхности: %v", diagDescErr)
	}
	// Собственный контекст: гасить поверхность надо ПОСЛЕ слушателей трафика, а
	// не одновременно с ними — иначе последний скрейп уносится раньше, чем
	// закончится остановка.
	diagCtx, stopDiag := context.WithCancel(context.Background())
	// Привязка порта СИНХРОННА: занятый адрес — ошибка посадки, и процесс не
	// вправе объявить себя поднявшимся, оставив её на код возврата.
	waitDiag, diagErr := servicehost.ServeSurface(diagCtx, diagDesc)
	if diagErr != nil {
		stopDiag()
		log.Fatalf("диагностическая поверхность: %v", diagErr)
	}

	// --- проекция потока изменений в браузер ---
	//
	// Собирается ЗДЕСЬ, а не у своего монтирования ниже, потому что она же —
	// реестр открытых потоков, который читает читатель отзыва (следующий блок).
	// Провязать его можно только тем, что уже существует.
	subscriptionStream, err := buildSubscriptionStreamHandler(cfg, backends, logger)
	if err != nil {
		log.Fatalf("subscription stream projection: %v", err)
	}

	// --- чтение журнала смены субъекта: отзыв доезжает до кэша решений И до потоков ---
	//
	// Соединение открывает ПОТРЕБИТЕЛЬ — то есть край. Владелец прав о крае не
	// знает и знать ему нечем: толчок из него снят вместе с адресом края (задача
	// #1024), а ребро осталось потребитель→владелец, как и всякое другое.
	//
	// Читатель живёт в ФУНДАМЕНТЕ (`pkg/subjectchange`), а не здесь: свойство
	// «смена прав доезжает до кэша решений» обязано держаться одной реализацией, и
	// одной пробой — сквозь обе стороны, вместе с производителем журнала.
	//
	// Полос у отзыва ДВЕ, и вторая не выводится из первой: кэш решений отвечает на
	// СЛЕДУЮЩИЙ запрос, а открытое соединение следующего запроса не делает (задача
	// #1022). Поэтому читателю передаётся реестр открытых потоков, и передаётся
	// он ОБЯЗАТЕЛЬНО — ноль отвергается сборкой.
	//
	// Работает только при включённом слое прав: гасить нечего, когда кэш —
	// заглушка.
	if authzMW != nil {
		reader := subjectchange.NewReader(backends["iamInternal"])
		sc, scErr := buildSubjectChangeWatcher(
			cfg, reader, authzMW.InvalidateCache, subscriptionStream, logger)
		if scErr != nil {
			log.Fatalf("subject-change reader: %v", scErr)
		}
		// Уборки на остановке у читателя нет: он только читает и держит курсор в
		// памяти. Выходит по отмене контекста (SIGTERM/SIGINT), догонять на выходе
		// нечего.
		go sc.Run(ctx)
		// Самоотчёт называет ОБЕ величины: перепрос, которым отзыв доезжает, и
		// срок, после которого его отсутствие само становится решением. Молчание
		// о втором сделало бы «fail-closed провязан» неотличимым от «не провязан».
		// Печатается НАБЛЮДЕНИЕ, а не литерал: `true` продолжал бы утверждать
		// «закрывает» при отключённом закрывателе.
		logger.Info("subject-change reader started",
			"interval", cfg.SubjectChangePollInterval,
			"stale_after", sc.StaleAfter().String(),
			"closes_streams", sc.ClosesStreams())
	}

	// --- перепрос состояния УДОСТОВЕРЕНИЯ на открытых потоках ---
	//
	// Вторая полоса отзыва, и она НЕ выводится из первой (задача #1410). Журнал
	// смены субъекта несёт отзыв ПРАВ; выход человека и принудительный выход
	// администратора отзывают САМО УДОСТОВЕРЕНИЕ и строк в тот журнал не пишут.
	// Переиспользовать перепрос изменений субъекта поэтому нечем.
	//
	// Провязывается БЕЗУСЛОВНО, в отличие от соседа: тот гасит кэш решений и без
	// включённого слоя прав гасить ему нечего, а здесь предмет — открытое
	// соединение, которое существует независимо от того, чем гейтится запрос.
	//
	// Окно отзыва выводится из объявленной границы на пути запроса (срок кэша
	// интроспекции), а не объявляется своей ручкой: две величины одного
	// механизма разошлись бы молча — и разошлись бы именно там, где расхождение
	// не видно.
	credentialSweeper, csErr := buildStreamRevocationSweeper(
		cfg, backends["iamInternal"], subscriptionStream, logger)
	if csErr != nil {
		log.Fatalf("subscription credential recheck: %v", csErr)
	}
	go credentialSweeper.Run(ctx)
	// Самоотчёт называет ОБЕ величины: окно, в пределах которого отзыв доезжает
	// до открытого соединения, и срок, после которого молчание авторитета само
	// становится решением закрыть. Молчание о втором сделало бы «fail-closed
	// провязан» неотличимым от «не провязан».
	logger.Info("subscription credential recheck started",
		"interval", credentialSweeper.Interval().String(),
		"stale_after", credentialSweeper.StaleAfter().String(),
		"stream_budget", cfg.SubscriptionStreamBudget.String())

	// --- gRPC server ---
	//
	// ПОТОЛОК ТЕМПА И ОДНОВРЕМЕННОСТИ на вызывающего — сначала величины, потом
	// ограничители, потом всё, что ими покрывается. Величины обоих слушателей
	// разрешаются ОДНИМ вызовом и ДО первой сборки сервера: негодный набор — это
	// отказ старта, а не предупреждение, и отказать он обязан раньше, чем
	// процесс начнёт выглядеть поднявшимся.
	publicAdmissionLimits, admErr := admissionLimits(cfg)
	if admErr != nil {
		log.Fatalf("request admission: %v", admErr)
	}
	// Ключ ВНЕШНЕГО слушателя — личность конечного пользователя: за краем сидит
	// арендатор, и предел объявлен на него. Личность к этому моменту ещё не
	// установлена — её ставит звено ниже по цепочке, и именно поэтому звено
	// допуска встаёт ПОСЛЕ него (см. admission.go).
	externalAdmission, admErr := grpcsrv.NewAdmission("public", publicAdmissionLimits, grpcsrv.PrincipalSubject)
	if admErr != nil {
		log.Fatalf("request admission (external listener): %v", admErr)
	}
	armAdmission(logger, externalAdmission, !cfg.AdmissionPublic.IsSilent())

	// Resolver handles native kacho.cloud.* — performs allowlist + domain
	// routing.
	resolver := proxy.Resolver(backends)
	grpcUnaryInterceptors := []grpc.UnaryServerInterceptor{
		middleware.UnaryRequestID,
		grpcsrv.UnaryPanicRecovery(logger),
		authInterceptor.Unary(),
	}
	grpcStreamInterceptors := []grpc.StreamServerInterceptor{
		middleware.StreamRequestID,
		grpcsrv.StreamPanicRecovery(logger),
		authInterceptor.Stream(),
	}
	// cnf-binding enforcement runs AFTER auth (token already shape-validated) and
	// BEFORE authz: a bound token presented unbound over gRPC is rejected before
	// any authorization decision. Mounted only when DPoP is enabled (parity with
	// the REST DPoPMiddleware).
	if cnfGRPCInterceptor != nil {
		grpcUnaryInterceptors = append(grpcUnaryInterceptors, cnfGRPCInterceptor.Unary())
		grpcStreamInterceptors = append(grpcStreamInterceptors, cnfGRPCInterceptor.Stream())
	}
	// ДОПУСК ПО ТЕМПУ И ОДНОВРЕМЕННОСТИ — здесь, и место несущее в обе стороны.
	//
	// ПОСЛЕ личности: ключом ведра служит она, и ограничитель, ключующийся до
	// звена, её устанавливающего, снимается подстановкой чужого заголовка — то
	// есть ограничивает только того, кто не пытается его обойти.
	//
	// ДО отказа в маршруте и решения о правах: и то и другое стоит дорого, а
	// решение о правах — это СЕТЕВОЙ вызов к iam на КАЖДОМ запросе, причём все
	// запросы края идут туда под ОДНОЙ личностью сертификата, то есть в одно
	// ведро на внутреннем слушателе iam. Поток одного арендатора, не
	// остановленный здесь, вычерпал бы это общее ведро — и решение о правах
	// перестало бы приниматься для ВСЕХ. Допуск после решения о правах защищал бы
	// только пересылку в домен, оставив усилитель нагрузки нетронутым.
	//
	// Звено ТОЛЬКО ПОТОКОВОЕ, и это не пропуск: проксируемый поток края не
	// проходит ни через один дескриптор службы (его несёт обработчик неизвестной
	// службы, который библиотека диспетчеризует как поток), а собственную
	// поверхность края покрывает обёртка регистратора ниже. Унарное звено рядом
	// с обёрткой означало бы двойное списание, а без обёртки — потерю
	// служебного изъятия для проверки здоровья.
	grpcStreamInterceptors = append(grpcStreamInterceptors, externalAdmission.StreamInterceptor())

	// Route refusal for Internal*Service — BEFORE authorization, AFTER
	// authentication. Position is the whole point, in both directions.
	//
	// Before authz: the decision "there is no such route" belongs to the
	// UnknownServiceHandler at the END of this chain, so authorization used to
	// answer first for any Internal* method whose permission the caller lacked —
	// with PermissionDenied AND THE PERMISSION'S NAME, while its neighbours
	// answered an indistinguishable "unknown method". That difference is an
	// existence-oracle for the admin surface, and it also made the isolation
	// invariant undemonstrable from outside: a probe could not tell "not routed
	// here" from "routed, but not permitted".
	//
	// After authN: the answer must match what a method that does not exist gives
	// THE SAME CALLER. An unauthenticated caller gets Unauthenticated for every
	// method; refusing ahead of authN would hand that caller NotFound for
	// Internal* and Unauthenticated for everything else — the same leak in
	// different clothes.
	grpcUnaryInterceptors = append(grpcUnaryInterceptors, proxy.UnaryRefuseInternalRoute())
	grpcStreamInterceptors = append(grpcStreamInterceptors, proxy.StreamRefuseInternalRoute())
	if authzMW != nil {
		grpcUnaryInterceptors = append(grpcUnaryInterceptors, authzMW.Unary())
		grpcStreamInterceptors = append(grpcStreamInterceptors, authzMW.Stream())
	}
	grpcUnaryInterceptors = append(grpcUnaryInterceptors, middleware.UnaryAccessLog(logger))
	grpcStreamInterceptors = append(grpcStreamInterceptors, middleware.StreamAccessLog(logger))
	// Измеритель СТАВИТСЯ ПЕРВЫМ, то есть самым внешним: он обязан накрывать всё,
	// что край делает ради запроса, включая отказ по правам и отказ маршрутизации.
	// Стоя за звеном прав, он оставил бы неизмеренным каждый отказ — исход, ради
	// которого в разбор происшествия и приходят.
	grpcUnaryInterceptors = append([]grpc.UnaryServerInterceptor{
		edgeLatency.UnaryServerInterceptor(grpcsrv.ListenerPublic),
	}, grpcUnaryInterceptors...)
	grpcStreamInterceptors = append([]grpc.StreamServerInterceptor{
		edgeLatency.StreamServerInterceptor(grpcsrv.ListenerPublic),
	}, grpcStreamInterceptors...)
	grpcSrv := proxy.NewServer(resolver,
		grpc.ChainUnaryInterceptor(grpcUnaryInterceptors...),
		grpc.ChainStreamInterceptor(grpcStreamInterceptors...),
	)
	// Нативная поверхность внешне достижимого gRPC-сервера — один список,
	// external_grpc_services.go. Запросы /kacho.cloud.operation.OperationService/*
	// идут напрямую туда, минуя transparent-proxy routing (server.go Resolver).
	opsProxy := opsproxy.New(backends)
	registerExternalGRPCServices(grpcSrv, externalAdmission.Registrar(grpcSrv), backends, opsProxy)

	// --- REST mux (grpc-gateway) ---
	// Регистрирует активные публичные сервисы + OperationService через OpsProxy
	// + kacho-only Internal admin-сервисы (vpc Region/Zone/AddressPool, compute
	// DiskType/Zone) на их internal-портах (9091). Internal-методы НЕ публикуются
	// на external/TLS endpoint в gRPC-проксе (allowlist + HasInternalSuffix);
	// REST-доступ к ним — только для UI / admin-tooling через cluster-internal
	// REST listener.
	restAddrs := cfg.BackendAddrs()
	// The REST-mux is a SEPARATE proxy-path from the gRPC routing — it dials each
	// backend itself. It threads the SAME per-edge dial creds the gRPC routing /
	// authz use (mTLS client-cert + per-backend ServerName when the edge is
	// enabled, else insecure) so backends requiring a verified client-cert do not
	// reset the UI REST calls. Fail-fast on misconfig — never start half-secured.
	restDialCreds, err := buildBackendDialCreds(cfg)
	if err != nil {
		log.Fatalf("rest mux backend dial creds: %v", err)
	}
	restHandler, err := restmux.NewMux(ctx, restAddrs, backends, restDialCreds)
	if err != nil {
		log.Fatalf("rest mux: %v", err)
	}

	// --- HTTP mux с health endpoints ---
	// Critical backends: iam фронтит authN+authZ на каждом запросе, без него
	// gateway не обслуживает аутентифицированный трафик → его недоступность валит
	// readiness. Прочие backends (vpc/compute/geo/nlb) — деградация одного домена,
	// реплика остается в rotation (см. health.HTTPReadyz).
	criticalBackends := map[string]bool{"iam": true, "iamInternal": true}
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/healthz", health.HTTPHealthz)
	httpMux.Handle("/readyz", health.HTTPReadyz(backends, criticalBackends, logger))

	// GET /iam/v1/auth/me — личность за сессией развёрнутого провайдера.
	// Регистрируется ДО `/` чтобы перебить grpc-gateway catch-all.
	sessionIdentity := middleware.NewSessionIdentityHandler(logger)
	// /me читает Kratos session если есть cookie ory_kratos_session.
	if kratosURL != "disabled" {
		sessionIdentity = sessionIdentity.
			// Тот же вопрос, что на полосе личности: две полосы, читающие одну
			// сессию, обязаны отвечать про неё одинаково.
			WithSessionCutoff(clients.NewSessionRevocationsAdapter(backends["iamInternal"])).
			WithKratos(middleware.NewKratosClient(kratosURL), iamSubjectClient).
			WithAdminChecker(iamSubjectClient) // permissions = ["*","admin"] для system-admin
	}
	sessionIdentity.Register(httpMux)

	// POST /oauth/logout — RFC 7009 token revocation +
	// best-effort Hydra session-kill (triggers RFC 8254 back-channel logout
	// to registered SPs).
	httpMux.Handle("/oauth/logout", logoutHandler)

	// GET /subscription/v1/events — ЕДИНСТВЕННАЯ проекция потока изменений в
	// браузер. Монтируется ДО `/`, иначе её перебьёт общий обработчик REST.
	//
	// Наружу выставляется СВОЯ поверхность края, а не метод владельца: у метода
	// внешнего пути нет и не заводится (его нет в allowlist, его имя отсекает
	// HasInternalSuffix, а `google.api.http` контракт не объявляет). Разбор —
	// gateway/docs/engineering/architecture/subscription-stream-projection.md.
	httpMux.Handle(subscriptionstream.Path, subscriptionStream)
	// Счётчики ручки провязываются в диагностическую поверхность ЗДЕСЬ, а не
	// «когда-нибудь»: величина, которую никто не читает, не отличима от «этот
	// код не исполнялся», и потолок, ни разу не сработавший, выглядит ровно как
	// потолок, не подключённый вовсе.
	diagMetrics.RegisterSubscriptionStream(subscriptionStream.Stats,
		cfg.SubscriptionMaxStreams, cfg.SubscriptionMaxStreamsPerSubject)

	httpMux.Handle("/", restHandler)

	// Хранилище однократности `Idempotency-Key`.
	//
	// ДОМЕН ПАРАЛЛЕЛИЗМА ГАРАНТИИ — ФЛОТ, А НЕ ПРОЦЕСС (#694). Хранилище в
	// памяти процесса законно ровно для одной реплики; пару «вид хранилища ↔
	// объявленный размер флота» сверяет отказ в старте ниже, а чарт рендерит
	// размер флота из того же значения, что питает автомасштабирование.

	// Build the HTTP chain. The DPoP middleware sits between the
	// legacy auth-interceptor and the access-log: legacy fills principal
	// from Kratos / dev-HMAC if present; DPoP middleware fills it from a
	// verified Hydra JWT if present. Anonymous requests pass through both
	// unless production-strict.
	//
	// AuthZ: the authz middleware mounts AFTER DPoP — by then
	// the request has principal-headers set; the authz layer reads them
	// to build the subject + condition context, then dispatches to
	// AuthorizeService.Check.
	var inner http.Handler = httpMux
	inner = middleware.HTTPIdempotency(idempStore)(inner)
	inner = middleware.HTTPAccessLog(logger)(inner)
	if authzMW != nil {
		inner = authzMW.HTTP(inner)
	}
	if dpopMiddleware != nil {
		inner = dpopMiddleware.Wrap(inner)
	}
	inner = authInterceptor.HTTP(inner)
	// Потолок тела — СНАРУЖИ всего, что тело читает (проверка прав буферизует
	// префикс, ключ идемпотентности хэширует префикс, сгенерированный хендлер
	// разбирает документ дважды: в сообщение и в обобщённое представление ради
	// проверки имён значений перечислений). Внутри любого из этих звеньев
	// потолок был бы объявлен и не действовал на самом дорогом участке; порядок
	// закреплён гейтом TestHTTPBodyCapIsOutermostBodyReader.
	inner = middleware.HTTPMaxBodyBytes(middleware.EdgeMaxRequestBodyBytes)(inner)
	httpHandler := middleware.HTTPRequestID(
		middleware.HTTPRecovery(logger)(inner),
	)

	httpSrv := &http.Server{
		Handler: httpHandler,
		// ReadHeaderTimeout bounds the slow-header (Slowloris) attack surface
		// independently of the body-read budget: a client trickling request
		// headers cannot pin a connection/goroutine indefinitely (CWE-400/770).
		// It applies only from the moment this server owns the connection — the
		// window BEFORE that (protocol sniffing by the multiplexer) is bounded
		// separately by edgeFirstByteBudget; see cmux_firstbyte.go.
		// WriteTimeout is intentionally left unset — the same server multiplexes
		// grpc-gateway responses (incl. long-lived streaming/long-poll REST) and a
		// blanket write deadline would truncate them; slow-read draining is bounded
		// instead by IdleTimeout + the reverse-proxy/L7 in front of the edge.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// SECURITY (fail-closed): the SAME httpSrv serves every HTTP listener —
		// the plaintext cmux listener the ingress targets, the advertised
		// external TLS listener, AND the dedicated cluster-internal admin REST
		// listener. ConnContext tags ONLY the internal admin listener's
		// connections (wrapped with listenerorigin.InternalListener below);
		// every other listener stays unmarked → external (the fail-closed
		// default), so the REST dispatcher / authz middleware 404 Internal*
		// paths regardless of which edge listener the request hit. This
		// inverts the earlier model, which marked only the TLS
		// listener external and left the ingress-facing plaintext listener
		// trusted → Internal* REST reachable from the edge.
		ConnContext: listenerorigin.InternalConnContext,
	}

	// ВНУТРЕННЕГО gRPC-СЛУШАТЕЛЯ У КРАЯ НЕТ — он снят вместе со своей
	// единственной службой (задача #1024).
	//
	// Слушатель существовал ради ОДНОГО метода: iam дозванивался до края и гасил
	// его кэш решений. Направление развёрнуто — соединение открывает ПОТРЕБИТЕЛЬ,
	// то есть сам край, — и модулей, зовущих край, не осталось ни одного. Порт без
	// единого метода есть входная поверхность, у которой нет предмета: её mTLS,
	// круг доверенных отправителей и потолок темпа сторожили бы пустоту.
	//
	// Что край продолжает выставлять внутрь кластера: ВНУТРЕННИЙ REST-мультиплексор
	// (`internalRestPort`) с путями `Internal*`. Это другой предмет и другой порт;
	// запрет #6 держится на нём по-прежнему.
	//
	// Самоотчёт посадки объявляет измерение внутреннего слушателя НЕПРИМЕНИМЫМ, а
	// не ложным: «слушателя нет вовсе» и «слушатель есть и не защищён» — разные
	// состояния, и схлопнуть их значило бы разрешить второе молчанием первого.
	observability.LogBootPosture(logger, bootPosture(cfg, identityLane))

	// --- cmux: HTTP/2 gRPC vs HTTP/1.1 REST на одном порту ---
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", cfg.ListenAddr, err)
	}
	logger.Info("api-gateway started", "addr", cfg.ListenAddr)

	cmuxer := newEdgeCmux(listener, edgeFirstByteBudget)
	// HTTP/2 с Content-Type: application/grpc → gRPC listener
	grpcL := cmuxer.MatchWithWriters(
		cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
	)
	// Все остальное → HTTP listener (grpc-gateway + healthz/readyz)
	httpL := cmuxer.Match(cmux.Any())

	go func() {
		serveErr := grpcSrv.Serve(grpcL)
		if serveErr != nil && serveErr != grpc.ErrServerStopped && ctx.Err() == nil {
			logger.Error("grpc listener died; shutting down", "error", serveErr)
			cancel()
		}
	}()

	go func() {
		serveErr := httpSrv.Serve(httpL)
		if serveErr != nil && serveErr != http.ErrServerClosed && ctx.Err() == nil {
			logger.Error("http listener died; shutting down", "error", serveErr)
			cancel()
		}
	}()

	// --- dedicated cluster-internal admin REST listener ---
	//
	// SECURITY (fail-closed): this is the ONLY listener wrapped
	// with listenerorigin.InternalListener, so it is the ONLY listener on which
	// the REST dispatcher serves Internal* paths (/vpc/v1/addressPools,
	// `:internal` infra-sensitive projections, InternalRegistry/Cluster/
	// Operations admin). Every other listener — the plaintext cmux listener the
	// ingress targets and the external TLS listener — is external (unmarked)
	// and 404s Internal* REST. The ingress MUST NOT target this port; admin-UI /
	// port-forward / cluster-internal tooling reach it via the `internal-rest`
	// Service port. It serves plain HTTP/1.1 REST (Internal* gRPC is blocked on
	// EVERY listener by the proxy's HasInternalSuffix router), so no cmux split
	// is needed. Empty addr → disabled (Internal* REST unreachable via gateway).
	var internalRESTListener net.Listener
	if cfg.InternalRESTAddr != "" {
		var restErr error
		internalRESTListener, restErr = net.Listen("tcp", cfg.InternalRESTAddr)
		if restErr != nil {
			log.Fatalf("internal REST listen %s: %v", cfg.InternalRESTAddr, restErr)
		}
		logger.Info("api-gateway internal admin REST started", "addr", cfg.InternalRESTAddr)
		go func() {
			serveErr := httpSrv.Serve(listenerorigin.InternalListener(internalRESTListener))
			if serveErr != nil && serveErr != http.ErrServerClosed && ctx.Err() == nil {
				logger.Error("internal REST listener died; shutting down", "error", serveErr)
				cancel()
			}
		}()
	}

	// --- TLS listener (опционально) для TLS-клиентов ---
	// Запускаем отдельный TLS-листенер; за ним — отдельный cmux, который точно так же
	// разделяет gRPC vs HTTP/REST после TLS-handshake. Тот же grpcSrv и httpSrv обслуживают
	// connections (через два независимых serve goroutine).
	var (
		tlsCmux     cmux.CMux
		tlsListener net.Listener
	)
	if cfg.TLSEnabled() {
		cert, certErr := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if certErr != nil {
			log.Fatalf("load TLS cert (%s, %s): %v", cfg.TLSCertFile, cfg.TLSKeyFile, certErr)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "http/1.1"},
			MinVersion:   tls.VersionTLS12,
		}
		// Hybrid: when enabled, accept an OPTIONAL client cert
		// (tls.VerifyClientCertIfGiven) with the internal CA as ClientCAs — a
		// browser without a cert still handshakes (JWT path), a client presenting a
		// valid Kachō cert gets it verified so the principal can be derived from its
		// SPIFFE SAN. Default (disabled) leaves ClientAuth=NoClientCert. This is the
		// EXTERNAL listener only; internal service listeners stay strict.
		if tlsCfg, certErr = cfg.ExternalListenerClientAuth(tlsCfg); certErr != nil {
			log.Fatalf("hybrid mTLS external listener: %v", certErr)
		}
		if cfg.HybridMTLSEnabled() {
			logger.Info("api-gateway external listener: optional client-cert (VerifyClientCertIfGiven) enabled")
		}
		var tlsErr error
		tlsListener, tlsErr = tls.Listen("tcp", cfg.TLSListenAddr, tlsCfg)
		if tlsErr != nil {
			log.Fatalf("tls listen %s: %v", cfg.TLSListenAddr, tlsErr)
		}
		logger.Info("api-gateway TLS started", "addr", cfg.TLSListenAddr)

		// Включаем h2c-style HTTP/2 поддержку для http.Server (через golang.org/x/net/http2),
		// иначе HTTP/2 over TLS не работает корректно.
		_ = http2.ConfigureServer(httpSrv, &http2.Server{})

		tlsCmux = newEdgeCmux(tlsListener, edgeFirstByteBudget)
		tlsGrpcL := tlsCmux.MatchWithWriters(
			cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
		)
		tlsHTTPL := tlsCmux.Match(cmux.Any())

		go func() {
			serveErr := grpcSrv.Serve(tlsGrpcL)
			if serveErr != nil && serveErr != grpc.ErrServerStopped && ctx.Err() == nil {
				logger.Error("tls grpc listener died; shutting down", "error", serveErr)
				cancel()
			}
		}()
		// SECURITY (fail-closed): the external TLS HTTP sub-listener is left
		// UNWRAPPED — its connections carry no internal-origin marker, so they
		// are external (the default) and the REST dispatcher 404s Internal*
		// paths arriving here. Internal* REST is served ONLY on the dedicated
		// cluster-internal admin listener (InternalListener-wrapped, below).
		go func() {
			serveErr := httpSrv.Serve(tlsHTTPL)
			if serveErr != nil && serveErr != http.ErrServerClosed && ctx.Err() == nil {
				logger.Error("tls http listener died; shutting down", "error", serveErr)
				cancel()
			}
		}()
		go func() {
			serveErr := tlsCmux.Serve()
			if serveErr != nil && ctx.Err() == nil {
				logger.Error("tls cmux died; shutting down", "error", serveErr)
				cancel()
			}
		}()
	}

	go func() {
		<-ctx.Done()
		logger.Info("shutting down")
		// Bound GracefulStop by the grace window, then force Stop(): a long-lived
		// proxied stream must not block exit until the kubelet sends SIGKILL.
		stopGraceful(grpcSrv, 10*time.Second)
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		_ = httpSrv.Shutdown(shutCtx)
		// Close the accept listeners so cmuxer.Serve()/tlsCmux.Serve() return and
		// main() exits instead of blocking until the kubelet sends SIGKILL.
		_ = listener.Close()
		if tlsListener != nil {
			_ = tlsListener.Close()
		}
		if internalRESTListener != nil {
			_ = internalRESTListener.Close()
		}
		// Диагностика гасится ПОСЛЕДНЕЙ: пока слушатели трафика дренируются, её
		// величины ещё нужны тому, кто смотрит на остановку. Ожидание
		// возвращается только после освобождения порта — без него следующий
		// старт того же процесса спотыкался бы о собственный предыдущий.
		stopDiag()
		if dwErr := waitDiag(); dwErr != nil {
			logger.Error("диагностическая поверхность остановлена с ошибкой", "err", dwErr)
		}
	}()

	// Wire SIGHUP → live reload of the authz permission catalog + overrides
	// from their on-disk paths (ConfigMap staged rollout / emergency override).
	installAuthzSIGHUP(hupCh, authzMW, logger)

	if serveErr := cmuxer.Serve(); serveErr != nil {
		logger.Error("cmux serve error", "error", serveErr)
	}
}

// authzReloader is the narrow reload port the SIGHUP handler drives.
// *middleware.AuthzMiddleware satisfies it.
type authzReloader interface {
	// Reload re-reads the authz config (permission catalog + overrides) from
	// disk and returns any per-component failure; the previous-good config is
	// preserved on failure.
	Reload() error
}

// installAuthzSIGHUP drains SIGHUP notifications and, on each one, triggers a
// live reload of the authz config (permission catalog + overrides) from the
// on-disk paths remembered at startup — so an operator's ConfigMap edit
// (emergency explicit-deny, catalog fix) applies without a pod restart.
// Reload is best-effort: on failure the previous-good config is kept and the
// loop keeps serving subsequent signals. Reload is a no-op when authz is
// disabled or backed by the embedded asset (no on-disk path).
func installAuthzSIGHUP(hupCh <-chan os.Signal, authz authzReloader, logger *slog.Logger) {
	go func() {
		for sig := range hupCh {
			logger.Info("SIGHUP received; reloading authz config", "signal", sig.String())
			if err := authz.Reload(); err != nil {
				logger.Error("authz config reload failed; keeping previous-good config", "error", err)
				continue
			}
			logger.Info("authz config reloaded")
		}
	}()
}

// stopGraceful runs GracefulStop bounded by timeout, then forces Stop() — so a
// long-lived proxied stream cannot block process shutdown past the grace window.
// logoutVerifierAdapter bridges the gateway's JWKS access-token verifier to the
// narrow identity port the logout handler needs. It exposes ONLY the validated
// subject/jti, so the handler revokes the caller's own session and never trusts
// a client-supplied subject.
type logoutVerifierAdapter struct{ v *middleware.JWTVerifier }

func (a logoutVerifierAdapter) Verify(ctx context.Context, token string) (*handler.VerifiedCaller, error) {
	vt, err := a.v.Verify(ctx, token)
	if err != nil {
		return nil, err
	}
	return &handler.VerifiedCaller{Subject: vt.Subject, JTI: vt.JTI}, nil
}

func stopGraceful(s *grpc.Server, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		s.Stop()
	}
}

// authzWiring — то, что композиционный корень получает от сборки проверки прав.
//
// Не только звено: величины решений и обращений по проводу накапливаются ВНУТРИ
// сборки, а читает их коллектор диагностической поверхности, который живёт
// снаружи. Пока сборка отдавала одно звено, оба накопителя оставались
// недосягаемы, и их ноль ничего не утверждал.
type authzWiring struct {
	// mw — само звено, встраиваемое в цепочки gRPC и HTTP.
	mw *middleware.AuthzMiddleware
	// metrics — накопитель десяти полос решения (четыре исхода принятого решения
	// плюс шесть механизмов допуска БЕЗ ответа модели, #798), окна вердиктов,
	// длительности и объявленной посадки.
	metrics *middleware.AuthzMetrics
	// calls — клиент владельца прав; считает обращения ПО ПРОВОДУ, включая
	// повторы. Отдельная величина, потому что из числа решений она не выводится.
	calls *clients.IAMAuthorizeClient
}

// buildAuthzMiddleware constructs the AuthZ middleware from
// configuration. When AuthZEnabled=false this returns a no-op middleware
// (the caller still wires it into the chain, but it pass-through everything).
func buildAuthzMiddleware(cfg config.Config, logger *slog.Logger) (authzWiring, error) {
	if !cfg.AuthZEnabled {
		// Накопитель собирается и на выключенной проверке: серии обязаны стоять
		// нулями и здесь, иначе «проверка выключена» на поверхности выглядело бы
		// как «коллектора нет».
		authzMetrics := middleware.NewAuthzMetrics()
		mw, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
			Enabled: false,
			Logger:  logger,
			Metrics: authzMetrics,
		})
		return authzWiring{mw: mw, metrics: authzMetrics}, err
	}

	catalog, err := middleware.LoadEmbeddedPermissionCatalog(cfg.AuthZPermissionCatalogFile)
	if err != nil {
		return authzWiring{}, err
	}

	overrides := middleware.NewAuthzOverrides()
	if cfg.AuthZOverridesFile != "" {
		if oerr := overrides.LoadFromFile(cfg.AuthZOverridesFile); oerr != nil {
			// Reload-failures on first start are fatal — we have no prior
			// good state to fall back to.
			return authzWiring{}, oerr
		}
	}

	// iam-authorize is the gateway→iam edge → mTLS under MTLS_IAM_ENABLE
	// (same edge as iam-subject + iam backend conns). Fail-fast on
	// misconfig (enabled without cert material) — never a silent insecure fallback.
	authorizeAddr := cfg.ResolvedIAMAuthorizeURL()
	authorizeCreds, err := iamEdgeDialCreds(cfg, authorizeAddr)
	if err != nil {
		return authzWiring{}, fmt.Errorf("iam authorize mTLS creds: %w", err)
	}
	authzClient, err := clients.NewIAMAuthorizeClient(clients.IAMAuthorizeClientConfig{
		Addr:           authorizeAddr,
		Timeout:        time.Duration(cfg.AuthZCheckTimeoutMs) * time.Millisecond,
		Logger:         logger,
		TransportCreds: authorizeCreds,
	})
	if err != nil {
		return authzWiring{}, err
	}

	// Build the REST<->gRPC route table so the authz
	// middleware can resolve an incoming REST path to a gRPC FQN (and the
	// catalog entry). Also feeds the ResourceExtractor's HTTP path strategy
	// with FQN -> path-template mappings to pluck `{field}` scope ids.
	restRouter := middleware.NewRestRouter()

	// Накопитель решений собирается ЗДЕСЬ и отдаётся наружу полем: звено считает
	// в него на горячем пути, а читает его коллектор диагностической
	// поверхности. Пока накопитель заводило само звено, снаружи к нему было не
	// подобраться — и его четыре нуля не утверждали ничего.
	authzMetrics := middleware.NewAuthzMetrics()
	mw, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled:         true,
		FailOpen:        cfg.AuthZFailOpen,
		Catalog:         catalog,
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, cfg.AuthZTrustedXForwardedFor, middleware.WithTrustedProxyHops(cfg.AuthZTrustedProxyCount)),
		Resources:       middleware.NewResourceExtractor(restRouter.PathTemplates()),
		Checker:         clients.NewAuthzChecker(authzClient),
		Overrides:       overrides,
		Logger:          logger,
		Now:             time.Now,
		CacheTTL:        time.Duration(cfg.AuthZCacheTTLSeconds) * time.Second,
		CacheMaxEntries: cfg.AuthZCacheMaxEntries,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
		RestRouter:      restRouter,
		Metrics:         authzMetrics,
	})
	if err != nil {
		return authzWiring{}, err
	}
	return authzWiring{mw: mw, metrics: authzMetrics, calls: authzClient}, nil
}
