// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package middleware — auth.go: JWT validation + Principal injection.
//
// Поведение зависит от cfg.AuthN.Mode.
//
// Две стратегии валидации Bearer, выбираются по `alg` в JWT-хедере:
//   - **HMAC-dev** (HS256, `KACHO_API_GATEWAY_AUTHN_DEV_SECRET`) — dev/e2e токены.
//     Работает **только в mode=dev**: в production / production-strict эта
//     симметричная стратегия ОТКЛЮЧЕНА (SEC — symmetric-key principal forgery,
//     CWE-347), даже если dev-secret задан. Валидно-подписанный HS256 токен в
//     prod → reject Unauthenticated (единственная принятая стратегия — Hydra JWKS).
//   - **Hydra JWKS** (RS256/ES256/EdDSA, `WithVerifier`) — реальные login-токены;
//     principal берется из верифицированных `kacho_principal_*` claims (top-level
//     или `ext_claims`), SubjectLookuper — fallback только при их отсутствии.
//
// Per-mode:
//   - **dev** (default): backwards-compat. Без Bearer — pass-through anonymous
//     (Principal{system, anonymous}). С Bearer — валидируется (HMAC-dev ИЛИ Hydra
//     JWKS по alg); HMAC-subject (external_id) не найден в kacho_iam → fallback на
//     anonymous, чтобы не ломать существующие newman-сценарии. Bad token (любая
//     стратегия) → reject Unauthenticated, НИКОГДА anonymous.
//   - **production**: Subject lookup → kaname; NotFound → reject.
//   - **production-strict**: Bearer обязателен **всегда** (`Unauthenticated`
//     без него); все остальные правила — как в production.
//
// Архитектура:
//
//	┌─ parse Bearer ─┐    ┌─ JWT validate ─┐    ┌─ SubjectLookup ─┐    ┌─ Principal in ctx ─┐
//	│ Authorization │ → │ JWKS/HMAC      │ → │ kaname:9091  │ → │ x-kacho-principal-* │
//	└────────────────┘    └─────────────────┘    └─────────────────┘    └─────────────────────┘
//
// Loop-prevention: InternalIAMService.LookupSubject зовется **gRPC-direct**
// (через iamSubjectClient), НЕ через restmux, иначе recursion.
package middleware

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
)

// AuthMode — режим работы auth-interceptor'а. ТИП ОБЩИЙ, и это не сокращение
// записи.
//
// Прежде это была своя строка со своими тремя написаниями, а корень приводил к
// ней СЫРОЕ значение ручки приведением типа — без разбора. Тогда написание вне
// перечня (`Production`, значение с хвостовым пробелом, опечатка) не совпадало ни
// с одной константой и уезжало в ветку `default` каждого свича: край поднимался,
// объявляя посадку, которой не существует. Разбор общим словарём делает такое
// состояние невыразимым — неизвестное написание отвергается на старте и называет
// весь допустимый набор.
type AuthMode = servicecontract.Mode

const (
	AuthModeDev              = servicecontract.ModeDev
	AuthModeProduction       = servicecontract.ModeProduction
	AuthModeProductionStrict = servicecontract.ModeProductionStrict
)

// authFailedMsg is the single, constant client-visible message for every
// authN-failure returned by the gRPC interceptor. It deliberately does NOT
// echo backend/JWT error detail (info-exposure, CWE-209) and does NOT vary by
// whether the token was malformed vs the subject was unprovisioned — a varying
// message is a provisioned-subject enumeration oracle (CWE-204). The detailed
// cause is logged server-side only (mirrors the redaction already done in
// authz.go). The REST path uses its own fixed strings via writeHTTPUnauthorized.
const authFailedMsg = "authentication failed"

// SubjectLookuper — port-интерфейс для subject-резолва. Реализация —
// `internal/clients/iam_subject_client.go` (gRPC-direct к kaname:9091).
// Декларирован тут, чтобы не тащить proto-dep в middleware (Clean
// Architecture — handler/middleware layer).
type SubjectLookuper interface {
	LookupByExternalID(ctx context.Context, externalID string) (Subject, error)
}

// KratosSubjectLookuper — опциональное расширение SubjectLookuper для Kratos
// session-flow: при NotFound делает lazy-upsert User mirror'а через
// InternalUserService.UpsertFromIdentity.
type KratosSubjectLookuper interface {
	LookupOrUpsertFromKratos(ctx context.Context, identityID, email, displayName string) (Subject, error)
}

// Subject — резолвленный subject (User или ServiceAccount).
type Subject struct {
	Type        string // "user" | "service_account"
	ID          string
	DisplayName string
}

// TokenVerifier — port для JWKS-валидации Hydra-issued RS256 access JWT.
// Реализация — `*JWTVerifier` (jwt_verifier.go), уже сконструированная
// в cmd/main.go для DPoP; тот же инстанс переиспользуется в principal-path.
// Декларирован интерфейсом, чтобы AuthInterceptor не был жестко привязан к
// конкретному типу (тестируемость + Clean Architecture).
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (*VerifiedToken, error)
}

// AuthInterceptor — JWT validate + subject lookup + Principal injection.
type AuthInterceptor struct {
	mode      AuthMode
	devSecret []byte // HMAC-secret для mode=dev (если пуст — Bearer reject в dev/production-strict).
	// basicLane — полоса базового секрета (#1142). nil → полосы нет.
	basicLane     *BasicCredentialLane
	subjectLookup SubjectLookuper
	kratos        *KratosClient // optional Ory Kratos /whoami client (nil → disabled)
	verifier      TokenVerifier // JWKS-валидатор Hydra RS256 access JWT (nil → disabled, HMAC-only)
	mtlsPrincipal bool          // derive Principal from a verified client cert (hybrid external listener)
	// requireMachineBinding — when true, a token whose principal is a MACHINE
	// (kacho_principal_type=service_account) must be sender-constrained (RFC
	// 7800 `cnf`: DPoP jkt or mTLS x5t#S256). See machineBindingViolationFor.
	requireMachineBinding bool
	// revocation — asks the identity provider whether a verified token is still
	// live (nil → unmounted). See auth_revocation.go for why the check lives on
	// this always-running layer rather than behind a feature toggle.
	revocation TokenRevocationChecker
	// revocationFailures rate-limits the report of a revocation check that is not
	// answering, so an outage is visible without flooding the log.
	revocationFailures *introspectionFailureReporter
	// revocationSkips rate-limits the separate report of a token the check could
	// not be asked about at all (no identifier). Separate window and counter — see
	// WithRevocationCheck.
	revocationSkips *introspectionFailureReporter
	// platformRevocation — авторитет отзыва НАШИХ токенов.
	//
	// Полоса выбирается по ИЗДАТЕЛЮ, а не по настройке процесса: прежний
	// провайдер о наших токенах не знает by construction, и его ответ на наш
	// токен есть утверждение о чужом предмете, а не «действует» или «отозван».
	//
	// На этой полосе «авторитет не ответил» означает ОТКАЗ — см.
	// auth_revocation.go, где асимметрия двух полос объявлена и обоснована.
	platformRevocation TokenRevocationChecker
	// platformRevocationFailures — свой счётчик и своё окно доклада: «наш
	// авторитет молчит» и «чужой авторитет молчит» суть разные неисправности с
	// разными исправлениями и разными читателями.
	platformRevocationFailures *introspectionFailureReporter
	// sessionCutoff — НАШ авторитет отзыва, спрошенный про СУБЪЕКТА, на полосе
	// браузерной сессии. У неё нет удостоверения, поэтому спрашивать про неё по
	// идентификатору токена нечем; спрашивают по паре (субъект, момент
	// аутентификации). См. auth_session_cutoff.go.
	sessionCutoff SessionCutoffReader
	// sessionCutoffFailures — своё окно доклада: у этой полосы свой читатель и
	// своё последствие, и слитое с полосой предъявителя окно подавляло бы первый
	// доклад одной из них.
	sessionCutoffFailures *introspectionFailureReporter
	// sessionAssuranceUnknown — своё окно доклада для полосы сессии, назвавшей
	// уровень уверенности, который край перевести не может (или не назвавшей
	// его вовсе). Состояние означает «пол на этой полосе не удовлетворить
	// ничем», и оно не исчезает само — см. auth_session_stepup.go.
	sessionAssuranceUnknown *introspectionFailureReporter
	// basicAssuranceUnknown — то же окно доклада для полосы базового
	// удостоверения: величина уровня уехала с оси каталога, и полоса больше не
	// может поручиться ни за один уровень (auth_basic_stepup.go). Окно своё, а
	// не общее с полосой сессии: диагнозы и починки у них разные, а слитое окно
	// подавляло бы первый доклад одной из них.
	basicAssuranceUnknown *introspectionFailureReporter
	// authMethodsUnusable — окно доклада о способах подтверждения, которые край
	// не смог довезти до модели прав. Заводится сразу, а не вместе с полосой:
	// состояние возможно на ЛЮБОЙ полосе, несущей удостоверение, и своей ручки
	// у него нет — оно свойство чужого словаря (см. reportUnusableAuthMethods).
	authMethodsUnusable *introspectionFailureReporter
	// stepUp / stepUpLookup / stepUpRoutes — the per-RPC authentication floor,
	// applied on this always-running layer rather than behind a feature toggle.
	// All three or none; see auth_stepup.go for why the floor cannot live where
	// proof-of-possession does.
	stepUp       *StepUpGate
	stepUpLookup PermissionLookup
	stepUpRoutes RestRouteResolver
	logger       *slog.Logger

	// Headers, которые auth-interceptor пропускает в backend metadata
	// (после успешного auth). Backend через corelib `grpcsrv.PrincipalExtractInterceptor`
	// прочитает их в ctx.
	mdKeyPrincipalType    string // principalmeta.MetaPrincipalType
	mdKeyPrincipalID      string // principalmeta.MetaPrincipalID
	mdKeyPrincipalDisplay string // principalmeta.MetaPrincipalDisplay
}

// NewAuthInterceptor создает interceptor с настройками из конфига.
func NewAuthInterceptor(mode AuthMode, devSecret string, lookup SubjectLookuper, logger *slog.Logger) *AuthInterceptor {
	return &AuthInterceptor{
		mode:                  mode,
		devSecret:             []byte(devSecret),
		subjectLookup:         lookup,
		logger:                logger,
		mdKeyPrincipalType:    principalmeta.MetaPrincipalType,
		mdKeyPrincipalID:      principalmeta.MetaPrincipalID,
		mdKeyPrincipalDisplay: principalmeta.MetaPrincipalDisplay,
		authMethodsUnusable:   newIntrospectionFailureReporter(0, nil),
	}
}

// WithRequireMachineTokenBinding demands that MACHINE principals present a
// sender-constrained token (RFC 7800 `cnf` — DPoP `jkt` or mTLS `x5t#S256`).
//
// Why here and not in DPoPMiddleware / CnfBindingInterceptor: those are gated
// behind KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP, which defaults off. This
// interceptor is the one authN layer that always runs, so it is the only place
// the requirement is actually reachable on a deployed stand.
//
// Why machines only: a machine principal is exempt from step-up because it has
// no second factor. "Exempt" must mean "protected differently", not
// "unprotected" — proof-of-possession is that different protection. The
// interactive path is untouched: browser flows issue plain bearers and gain
// their assurance from step-up instead.
//
// Default OFF. The provider does not yet register OAuth2 clients that mint
// bound tokens, so enabling this before issuance lands rejects every
// service-account token. Sequence: land issuance → observe `cnf` on minted
// machine tokens → enable here.
func (a *AuthInterceptor) WithRequireMachineTokenBinding(require bool) *AuthInterceptor {
	a.requireMachineBinding = require
	return a
}

// machineBindingViolation reports whether the verified token belongs to a
// machine principal that failed to present a sender-constrained token.
//
// Fail-closed shape: the check keys on the principal type carried by the
// token's own verified claims, and treats "no cnf at all" as the violation. A
// token that IS bound is validated for actual possession by the DPoP / mTLS
// validators (they own the proof check); this guard answers only the prior
// question — "is this machine credential replayable as a plain bearer?".
func (a *AuthInterceptor) machineBindingViolation(vt *VerifiedToken) bool {
	return a.machineBindingViolationFor(vt, verifiedClaim(vt, "kacho_principal_type"))
}

// machineBindingViolationFor is the same check against an ALREADY-RESOLVED
// principal type. It exists for the SubjectLookuper fallback, which reaches a
// principal type without going through the token's own claims: guarding only
// the claims path would leave the fallback as the way in for exactly the
// credential the control is meant to constrain.
func (a *AuthInterceptor) machineBindingViolationFor(vt *VerifiedToken, principalType string) bool {
	if !a.requireMachineBinding || vt == nil {
		return false
	}
	if principalType != "service_account" {
		return false // human / unknown principal — untouched by this control
	}
	return !vt.Cnf.HasJkt && !vt.Cnf.HasX5tS
}

// WithKratos подключает Kratos /whoami client.
// Если выставлено, HTTP middleware сначала пытается резолвить principal по
// ory_kratos_session cookie; при отсутствии cookie / 401 — fallback на JWT.
func (a *AuthInterceptor) WithKratos(c *KratosClient) *AuthInterceptor {
	a.kratos = c
	// Полоса смонтирована — значит у неё есть и доклад о непереводимом уровне
	// уверенности. Заводить его позже было бы нечем: своей ручки у этого
	// состояния нет, оно свойство ОТВЕТА провайдера, а не настройки края.
	a.sessionAssuranceUnknown = newIntrospectionFailureReporter(0, nil)
	return a
}

// WithVerifier подключает JWKS-валидатор Hydra-issued RS256 access JWT.
// Когда выставлен, validateJWT детектит signing method: RS256/ES256/EdDSA →
// проверка через JWKS-verifier (verified claims), HMAC → существующий dev-path.
// Principal строится из верифицированных `kacho_principal_*` claims напрямую
// (top-level или ext_claims); SubjectLookuper — fallback только при их
// отсутствии. nil → JWKS-path выключен (HMAC-only).
// WithBasicCredentialLane провязывает полосу базового секрета. nil → полосы
// нет вовсе, и строка с нашей маркой уходит прочими полосами как негодная —
// это и есть посадка до появления авторитета, а не мягкий проход.
func (a *AuthInterceptor) WithBasicCredentialLane(l *BasicCredentialLane) *AuthInterceptor {
	a.basicLane = l
	if l != nil {
		a.basicAssuranceUnknown = newIntrospectionFailureReporter(0, nil)
	}
	return a
}

func (a *AuthInterceptor) WithVerifier(v TokenVerifier) *AuthInterceptor {
	a.verifier = v
	return a
}

// WithMTLSPrincipal enables the hybrid-listener cert-principal path: when
// the external TLS listener runs with tls.VerifyClientCertIfGiven, a request that
// arrived over a connection with a VERIFIED client cert (a valid Kachō cert) has
// its Principal derived from the cert's SPIFFE SAN BEFORE the JWT path — so a
// service→service caller authenticates on its mTLS identity with no JWT. When the
// flag is off (default), or no verified cert is present, the existing JWT flow is
// unchanged. The cert is trusted ONLY because the listener already verified the
// chain (VerifiedChains non-empty); client-supplied x-kacho-principal-* metadata
// is still stripped (no spoofing).
func (a *AuthInterceptor) WithMTLSPrincipal(enable bool) *AuthInterceptor {
	a.mtlsPrincipal = enable
	return a
}

// Unary — gRPC unary server interceptor.
func (a *AuthInterceptor) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		newCtx, err := a.authorize(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

// Stream — gRPC stream server interceptor.
func (a *AuthInterceptor) Stream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		newCtx, err := a.authorize(ss.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		// wrappedStream объявлен в request_id.go — переиспользуем для ctx-override.
		wrapped := &wrappedStream{ServerStream: ss, ctx: newCtx}
		return handler(srv, wrapped)
	}
}

// authorize — основной flow: parse → validate → lookup → inject Principal.
func (a *AuthInterceptor) authorize(ctx context.Context, fullMethod string) (context.Context, error) {
	// Strip client-supplied x-kacho-principal-* incoming metadata, чтобы
	// исключить header-injection privilege escalation.
	if inMD, ok := metadata.FromIncomingContext(ctx); ok {
		var stripped bool
		md := inMD.Copy()
		for k := range md {
			if isClientForgeableIdentityHeader(strings.ToLower(k)) {
				delete(md, k)
				stripped = true
			}
		}
		if stripped {
			ctx = metadata.NewIncomingContext(ctx, md)
			a.logger.Warn("auth: stripped client-supplied x-kacho-principal-* metadata",
				"method", fullMethod)
		}
	}
	// Hybrid external listener: a connection that presented a VALID Kachō
	// client cert (the listener verified its chain via VerifyClientCertIfGiven)
	// authenticates on its mTLS identity — derive the Principal from the verified
	// cert's SPIFFE SAN and SKIP the JWT requirement entirely (no SubjectLookuper
	// round-trip). This precedes the Bearer flow so a service→service caller with
	// a cert but no JWT is NOT rejected in production-strict. The cert is trusted
	// only because the listener already verified it; client-supplied
	// x-kacho-principal-* metadata was stripped above (no spoofing).
	if a.mtlsPrincipal {
		if pType, pID, ok := principalFromVerifiedPeer(ctx); ok {
			a.logger.Debug("auth: principal derived from verified client cert (mTLS)",
				"method", fullMethod, "type", pType, "id", pID)
			return a.injectPrincipal(ctx, pType, pID, pID), nil
		}
	}

	bearer := extractBearer(ctx)

	// Empty Bearer handling per mode.
	//
	// A request with no credential is not authenticated, and `production` must
	// say so. It used to be answered with an injected {system, anonymous}
	// principal, which reads as a NAME everywhere downstream: subject-extraction
	// resolves it, so the "unauthenticated" gate never fires, and the ownership
	// predicate matches it against itself, so every anonymous request owns every
	// other anonymous request's operations. `dev` keeps the pass-through — it is
	// the in-process/dev-stand affordance and the only mode where the HMAC token
	// path lives at all.
	if bearer == "" {
		switch a.mode {
		case AuthModeDev:
			return a.injectAnonymous(ctx), nil
		default: // production / production-strict
			return nil, status.Error(codes.Unauthenticated, "missing Bearer token")
		}
	}

	// ПОЛОСА БАЗОВОГО СЕКРЕТА — ПЕРВОЙ ПОСЛЕ ПУСТОГО ВХОДА И ТЕРМИНАЛЬНАЯ
	// (задача #1142, приёмка BAT-1 §5.1).
	//
	// Полоса выбирается ПО МАРКЕ, а не по неудаче разбора подписанного токена.
	// Стой она последней «на всякий случай» — всякая негодная строка приходила
	// бы к ней входом, и диагностика стала бы невозможной. Стой она без
	// терминальности — негодное удостоверение уезжало бы дальше как
	// «удостоверения нет вовсе», и вызывающий получал бы отказ не той природы.
	if a.basicLane != nil && a.basicLane.Owns(bearer) {
		cred, err := a.basicLane.Verify(ctx, bearer)
		switch {
		case errors.Is(err, ErrCredentialStateUnknown):
			// Отдельный исход и наружу тоже: это не отказ в удостоверении, а
			// неспособность установить его состояние.
			return nil, status.Error(codes.Unavailable, "credential state could not be established")
		case err != nil:
			return nil, status.Error(codes.Unauthenticated, basicCredentialRefusalText())
		}
		// ДОСТАТОЧНО ЛИ СИЛЬНО АУТЕНТИФИЦИРОВАН ВЫЗЫВАЮЩИЙ ДЛЯ ЭТОГО ГЛАГОЛА
		// (#1215). Спрашивается ровно там же, где на прочих полосах, — ДО
		// записи личности, чтобы не прошедший пол запрос не доехал ни до прав,
		// ни до backend. Освобождения этой полосы by design нет: пол есть
		// свойство ВСЯКОГО обращения, а не свойство того, чем его подписали.
		as := a.basicCredentialAssurance(cred, fullMethod)
		if suErr := a.stepUpVerdictGRPC(fullMethod, as, stepUpLaneBasic); suErr != nil {
			// Отказ рендерится ЕДИНЫМ отказом полосы — байт-в-байт тем же, что
			// на негодном секрете. Различимый подтверждал бы годность
			// предъявленного, а церемонии, которая подняла бы уровень, у этой
			// полосы нет (auth_basic_stepup.go, разбор оракула).
			return nil, status.Error(codes.Unauthenticated, basicCredentialRefusalText())
		}
		ctx = a.injectPrincipal(ctx, cred.PrincipalType, cred.PrincipalID, cred.DisplayName)
		// Уровень удостоверения — вместе с принципалом и по той же причине
		// (см. развёрнутый разбор у REST-половины полосы). Едет ОТОБРАЖЁННАЯ на
		// ось величина, а не сырая константа: второй замок обязан читать то же
		// значение, по которому вынес вердикт первый.
		return withBasicCredentialLevel(ctx, as.ACR), nil
	}

	// Hydra-issued RS256/ES256/EdDSA access JWT → validate via JWKS
	// verifier (a SECOND strategy alongside the HMAC-dev path). On a verified
	// token the Principal is derived directly from the `kacho_principal_*`
	// claims (top-level or ext_claims) — no SubjectLookuper round-trip unless
	// those claims are absent. A present-but-bad token (bad sig / expired /
	// wrong iss / disallowed alg) is REJECTED Unauthenticated (fail-closed),
	// NEVER downgraded to anonymous.
	//
	// ЗДЕСЬ СТОЯЛО «или недостижимый источник ключей — тоже Unauthenticated», и
	// это было неверно (#1194): недоступность зависимости — не приговор
	// удостоверению. Она отвечает `Unavailable`, как и соседняя ветвь чтения
	// отзыва ниже, чей довод дословно тот же. Fail-closed при этом не ослаблен:
	// отказ остаётся отказом, обработчик не исполняется.
	if a.verifier != nil && isAsymmetricJWT(bearer) {
		vt, verr := a.verifier.Verify(ctx, bearer)
		if verr != nil {
			// Тот же разбор, что и на полосе REST, и по той же причине: пробел на
			// одной поверхности обессмысливает другую.
			if keySourceUnanswerable(verr) {
				a.logger.Error("auth: token key source unanswerable; refusing Unavailable",
					"method", fullMethod, "err", verr)
				return nil, status.Error(codes.Unavailable, keySourceUnavailableReason)
			}
			a.logger.Warn("auth: Hydra JWT validation failed (JWKS)",
				"method", fullMethod, "err", verr)
			return nil, status.Error(codes.Unauthenticated, authFailedMsg)
		}
		// Machine principals must prove possession — parity with the REST path.
		// A gap on either surface makes the other pointless: a replayer just
		// uses the unguarded one.
		if a.machineBindingViolation(vt) {
			a.logger.Warn("auth: machine token is not sender-constrained; rejected",
				"method", fullMethod)
			return nil, status.Error(codes.Unauthenticated, authFailedMsg)
		}
		// A verified signature says who minted the token and when it expires. It
		// does not say the token is still good — a sign-out or a revoked key
		// leaves a valid signature behind. Only the provider knows, and it is
		// asked here, on the layer that always runs, using the token this branch
		// has ALREADY verified (no second parse of the same bearer).
		switch a.revocationCheck(ctx, vt, "grpc", fullMethod) {
		case revocationRevoked:
			// The credential is dead, which is an authN failure like any other and
			// carries the same constant message (no varying text to read state off).
			a.logger.Warn("auth: token reported not live by the provider; rejected",
				"method", fullMethod)
			return nil, status.Error(codes.Unauthenticated, authFailedMsg)
		case revocationUnanswerable:
			// The fault is this deployment's configuration, not the caller's
			// credential: Unavailable, so a client retries instead of pointlessly
			// re-authenticating.
			return nil, status.Error(codes.Unavailable, revocationUnavailableReason)
		}
		// Same floor, same reason, on the native surface — where the method is
		// named by the transport and needs no route resolution.
		if suErr := a.enforceStepUpGRPC(fullMethod, vt); suErr != nil {
			return nil, suErr
		}
		ctx, unusableMethods := withTokenContextMetadata(ctx, vt)
		reportUnusableAuthMethods(a.logger, a.authMethodsUnusable, stepUpLaneBearer, fullMethod, unusableMethods)
		pType, pID, display, perr := principalFromVerifiedToken(vt)
		if perr == nil {
			return a.injectPrincipal(ctx, pType, pID, display), nil
		}
		// Claims absent → fall back to SubjectLookuper on the verified sub.
		return a.authorizeViaLookup(ctx, fullMethod, vt.Subject, vt)
	}

	// Validate JWT via the HMAC-dev path (dev/e2e tokens, zero regression).
	//
	// A Bearer header that is present but malformed / expired /
	// signature-invalid is a FAILED authentication
	// attempt — it must be rejected with `Unauthenticated` (HTTP 401), NOT
	// silently downgraded to anonymous (which would then hit authz and
	// surface as 403). authN failures precede authZ. This holds in every
	// mode: a bad token never grants anonymous access. A *missing* Bearer is
	// handled above (dev/production → anonymous, strict → 401).
	claims, err := a.validateJWT(bearer)
	if err != nil {
		a.logger.Warn("auth: JWT validation failed",
			"method", fullMethod, "err", err)
		return nil, status.Error(codes.Unauthenticated, authFailedMsg)
	}

	// Resolve subject via kaname (gRPC-direct).
	subjectID, _ := claims["sub"].(string)
	if subjectID == "" {
		return nil, status.Error(codes.Unauthenticated, "token missing subject")
	}

	// Service Account / API-token principals. A token
	// minted by the Hydra client_credentials flow (or a static API token)
	// carries `kacho_principal_type=service_account` + `kacho_sa_id=<svaId>`.
	// `sub` is the SA id itself, which is NOT a User `external_id`, so the
	// User LookupByExternalID below would miss and (in dev) downgrade the SA
	// to anonymous. Resolve the SA principal directly from the typed claims.
	if pt, _ := claims["kacho_principal_type"].(string); pt == "service_account" {
		saID, _ := claims["kacho_sa_id"].(string)
		if saID == "" {
			saID = subjectID
		}
		return a.injectPrincipal(ctx, "service_account", saID, saID), nil
	}

	// nil vt — the HMAC-dev tail carries no verified asymmetric token, so there
	// is no `cnf` to weigh (and this path is disabled outside dev anyway).
	return a.authorizeViaLookup(ctx, fullMethod, subjectID, nil)
}

// authorizeViaLookup резолвит principal через SubjectLookuper по external id
// (sub). Используется как HMAC-dev tail, так и JWKS-fallback (verified
// токен без kacho_principal_* claims). Поведение при неудачном резолве зависит
// от mode: dev → anonymous (back-compat newman), production /
// production-strict → reject Unauthenticated.
//
// Владелец личности отвечает РАЗНЫМИ ошибками на «такой нет» и «есть, но
// аутентификация ей запрещена» — и оба ведут сюда, в один и тот же отказ с
// постоянным текстом. Различие нужно не здесь, а внутри: по нему
// LookupOrUpsertFromKratos решает, заводить ли зеркало личности (заблокированной
// — не заводить), и по нему же в лог попадает настоящая причина. Наружу
// различие не выходит намеренно: иначе край стал бы оракулом существования
// личности.
//
// vt is the verified token when one exists (JWKS-fallback), nil otherwise. It is
// carried so the machine-binding requirement also covers a machine principal
// resolved by LOOKUP rather than by claims — guarding only the claims path would
// leave this branch as the way in for the very credential it constrains.
func (a *AuthInterceptor) authorizeViaLookup(ctx context.Context, fullMethod, subjectID string, vt *VerifiedToken) (context.Context, error) {
	if subjectID == "" {
		return nil, status.Error(codes.Unauthenticated, "token missing subject")
	}
	subj, err := a.subjectLookup.LookupByExternalID(ctx, subjectID)
	if err != nil {
		switch a.mode {
		case AuthModeProductionStrict, AuthModeProduction:
			// production[-strict]: у токена обязан быть принципал, которому
			// kaname разрешает аутентифицироваться. Настоящая причина (нет
			// такой личности либо она есть и запрещена) пишется в лог как есть;
			// наружу уходит постоянный текст, чтобы валидно подписанный токен
			// без принципала был неотличим от токена с плохой подписью (никакого
			// перебора личностей и утечки текста ошибки iam).
			a.logger.Warn("auth: principal not usable per kaname (rejecting)",
				"method", fullMethod, "external_id", subjectID, "err", err)
			return nil, status.Error(codes.Unauthenticated, authFailedMsg)
		default: // dev
			a.logger.Debug("auth: principal not usable per kaname, fallback to anonymous",
				"method", fullMethod, "external_id", subjectID, "err", err)
			return a.injectAnonymous(ctx), nil
		}
	}

	if a.machineBindingViolationFor(vt, subj.Type) {
		a.logger.Warn("auth: machine token (resolved by lookup) is not sender-constrained; rejected",
			"method", fullMethod)
		return nil, status.Error(codes.Unauthenticated, authFailedMsg)
	}

	// Inject Principal в ctx + metadata (backend читает через corelib).
	return a.injectPrincipal(ctx, subj.Type, subj.ID, subj.DisplayName), nil
}

// injectAnonymous marks the request as "nobody" — dev-mode only (see authorize).
// The marker is a reserved word, NOT an identity: everything that compares
// identities must read it through operations.Principal.IsAnonymous, which is why
// the word is taken from there rather than spelled out here. Producer and
// recogniser sharing one constant is what keeps them from drifting apart.
func (a *AuthInterceptor) injectAnonymous(ctx context.Context) context.Context {
	return a.injectPrincipal(ctx, "system", operations.AnonymousPrincipalID, "")
}

func (a *AuthInterceptor) injectPrincipal(ctx context.Context, pType, pID, displayName string) context.Context {
	p := operations.Principal{Type: pType, ID: pID, DisplayName: displayName}
	ctx = operations.WithPrincipal(ctx, p)

	// Inject в INCOMING metadata: proxy-слой (opsproxy.Get/Cancel,
	// shimproxy.Handler) пересобирает backend-outgoing из INCOMING через
	// principalmeta.OutgoingFromIncoming, а opsproxy.checkOperationOwnership
	// читает caller-principal тоже из INCOMING. Без записи в incoming
	// gRPC-direct-путь молча роняет principal (клиент аутентифицирован, но hop
	// форвардит анонима → ownership-check/backend видят анонимного caller'а).
	// Incoming уже очищен от client-forgeable x-kacho-principal-* выше, так что
	// это доверенный override, а не spoof. (REST-путь работает, потому что
	// restmux выставляет principal как incoming — здесь достигается паритет.)
	inMD, _ := metadata.FromIncomingContext(ctx)
	if inMD == nil {
		inMD = metadata.MD{}
	} else {
		inMD = inMD.Copy()
	}
	inMD.Set(a.mdKeyPrincipalType, pType)
	inMD.Set(a.mdKeyPrincipalID, pID)
	inMD.Set(a.mdKeyPrincipalDisplay, displayName)
	ctx = metadata.NewIncomingContext(ctx, inMD)

	// Inject в outgoing metadata — для нативного handler'а, который форвардит
	// собственный outgoing-ctx напрямую (без OutgoingFromIncoming-пересборки).
	md, _ := metadata.FromOutgoingContext(ctx)
	if md == nil {
		md = metadata.MD{}
	} else {
		md = md.Copy()
	}
	md.Set(a.mdKeyPrincipalType, pType)
	md.Set(a.mdKeyPrincipalID, pID)
	md.Set(a.mdKeyPrincipalDisplay, displayName)
	return metadata.NewOutgoingContext(ctx, md)
}

// isAsymmetricJWT peeks the unverified JWT header and reports whether its `alg`
// is in the asymmetric whitelist (RS256/ES256/EdDSA) — i.e. a Hydra-issued
// access token that must go through the JWKS verifier rather than the HMAC-dev
// path. HS* / none / non-JWT → false (handled by the HMAC branch / rejected).
// This is a strategy selector only; the verifier re-checks the alg authoritatively
// against the pinned JWK (algorithm-confusion is impossible here — a forged HS256
// with an asymmetric-looking header would fail HMAC validation, and an RS256
// claiming HS256 fails the JWKS alg whitelist).
func isAsymmetricJWT(tokenStr string) bool {
	headerB64, _, ok := strings.Cut(tokenStr, ".")
	if !ok {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		return false
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if jerr := json.Unmarshal(raw, &hdr); jerr != nil {
		return false
	}
	_, asymmetric := AllowedJWTAlgs[hdr.Alg]
	return asymmetric
}

// principalFromVerifiedToken derives the Kachō Principal from a JWKS-verified
// Hydra token's `kacho_principal_*` claims. It reads each claim robustly
// from EITHER the top level (Hydra allowed_top_level_claims promotion) OR the
// nested `ext_claims` map (token_hook session.access_token.ext_claims). Returns
// an error when the principal claims are absent so the caller can fall back to
// the SubjectLookuper. displayName comes from a present display claim, else
// the principal id.
func principalFromVerifiedToken(vt *VerifiedToken) (pType, pID, displayName string, err error) {
	pType = verifiedClaim(vt, "kacho_principal_type")
	pID = verifiedClaim(vt, "kacho_principal_id")
	if pType == "" || pID == "" {
		return "", "", "", fmt.Errorf("verified token carries no kacho_principal_* claims")
	}
	displayName = verifiedClaim(vt, "kacho_principal_display_name")
	if displayName == "" {
		displayName = pID
	}
	return pType, pID, displayName, nil
}

// verifiedClaim reads a string claim from a verified token, preferring the
// top-level claim, then the nested `ext_claims` map (claim-placement
// robustness).
func verifiedClaim(vt *VerifiedToken, key string) string {
	if vt.Claims != nil {
		if s, ok := vt.Claims[key].(string); ok && s != "" {
			return s
		}
	}
	if vt.ExtClaims != nil {
		if s, ok := vt.ExtClaims[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// principalFromVerifiedPeer derives a Kachō Principal from the verified client
// cert of the gRPC peer (hybrid listener). It returns ok=false unless the
// peer carries TLS auth-info with a NON-EMPTY VerifiedChains — i.e. the listener
// actually verified the presented cert against the trust anchor
// (VerifyClientCertIfGiven). An unverified / absent cert (browser, JWT client)
// yields ok=false so the caller falls through to the JWT path.
//
// The principal is mapped from the leaf's SPIFFE URI SAN
// (spiffe://kacho.cloud/ns/<ns>/sa/<sa>): a service→service caller is a
// `service_account` whose id is the `<sa>` segment. Display name defaults to the
// id at the call site.
func principalFromVerifiedPeer(ctx context.Context) (pType, pID string, ok bool) {
	p, present := peer.FromContext(ctx)
	if !present || p == nil {
		return "", "", false
	}
	tlsInfo, isTLS := p.AuthInfo.(credentials.TLSInfo)
	if !isTLS {
		return "", "", false
	}
	leaf := verifiedLeaf(tlsInfo.State.VerifiedChains)
	if leaf == nil {
		return "", "", false
	}
	for _, u := range leaf.URIs {
		if sa, matched := saFromSPIFFE(u.String()); matched {
			return "service_account", sa, true
		}
	}
	return "", "", false
}

// verifiedLeaf returns the leaf (chain[0]) of the first non-empty verified chain,
// or nil when no chain was verified (the cert was absent or did not chain to a
// trusted CA).
func verifiedLeaf(chains [][]*x509.Certificate) *x509.Certificate {
	for _, chain := range chains {
		if len(chain) > 0 && chain[0] != nil {
			return chain[0]
		}
	}
	return nil
}

// saFromSPIFFE parses a Kachō SPIFFE id of the form
// `spiffe://kacho.cloud/ns/<ns>/sa/<sa>` and returns the `<sa>` workload segment.
// Any other URI shape (or a non-Kachō trust domain) → matched=false.
func saFromSPIFFE(uri string) (sa string, matched bool) {
	const prefix = "spiffe://kacho.cloud/ns/"
	rest, ok := strings.CutPrefix(uri, prefix)
	if !ok {
		return "", false
	}
	// rest = "<ns>/sa/<sa>"
	_, after, ok := strings.Cut(rest, "/sa/")
	if !ok || after == "" {
		return "", false
	}
	// Guard against trailing path segments — the sa segment must be terminal.
	if strings.Contains(after, "/") {
		return "", false
	}
	return after, true
}

func (a *AuthInterceptor) validateJWT(tokenStr string) (jwt.MapClaims, error) {
	// SEC (sec-hardening-r8): the symmetric HMAC-dev token path is a dev/e2e
	// affordance ONLY. In production / production-strict it MUST be refused
	// wholesale — a validly-HS256-signed token (an attacker who learned or
	// guessed KACHO_API_GATEWAY_AUTHN_DEV_SECRET) would otherwise yield a real
	// principal, and a `kacho_principal_type=service_account` claim is injected
	// as a service_account with NO IAM lookup (symmetric-key principal forgery,
	// CWE-347). The only accepted Bearer strategy in prod is the asymmetric JWKS
	// (Hydra) verifier, which runs BEFORE this path for RS256/ES256/EdDSA tokens.
	// Fail closed regardless of whether a dev-secret happens to be configured
	// (defense-in-depth alongside the fatal startup guard in cmd/api-gateway).
	if a.mode != AuthModeDev {
		return nil, fmt.Errorf("HMAC-dev token path disabled in %q mode", a.mode)
	}
	if len(a.devSecret) == 0 {
		// HMAC-dev path requires a configured dev-secret. Hydra RS256 tokens are
		// validated by the JWKS verifier branch BEFORE reaching here, so
		// an empty dev-secret only disables the HS256-dev path.
		return nil, fmt.Errorf("no signing key configured (dev secret empty)")
	}
	parsed, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.devSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("unexpected claims type")
	}
	return claims, nil
}

// setPrincipalHeaders writes the resolved principal onto the REST request in
// both the plain form (read by restmux WithMetadata → outgoing gRPC metadata)
// and the legacy grpc-gateway convention form (fallback path). Shared by the
// Hydra-JWT branch with the Kratos and SA-token paths.
// withBasicCredentialLevel кладёт уровень удостоверения во ВХОДЯЩИЕ метаданные —
// туда же, откуда его читает страж повышения (`verifiedTokenFromCtxOrHTTP`).
// Входящие уже очищены от подделываемых клиентом заголовков выше по цепочке,
// поэтому это доверенная простановка, а не подмена.
func withBasicCredentialLevel(ctx context.Context, level string) context.Context {
	if level == "" {
		return ctx
	}
	inMD, _ := metadata.FromIncomingContext(ctx)
	if inMD == nil {
		inMD = metadata.MD{}
	} else {
		inMD = inMD.Copy()
	}
	inMD.Set(principalmeta.MetaTokenACR, level)
	return metadata.NewIncomingContext(ctx, inMD)
}

func setPrincipalHeaders(r *http.Request, pType, pID, displayName string) {
	r.Header.Set(principalmeta.HeaderPrincipalType, pType)
	r.Header.Set(principalmeta.HeaderPrincipalID, pID)
	r.Header.Set(principalmeta.HeaderPrincipalDisplay, displayName)
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalType, pType)
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalID, pID)
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, displayName)
}

// writeHTTPUnauthorized emits a 401 with a gRPC-shaped JSON body (code 16 =
// Unauthenticated) and a `WWW-Authenticate` challenge. Used by the REST auth
// path when a Bearer token is present but fails validation — authN failures
// must surface as 401, never as 403 (which is the authZ verdict).
func writeHTTPUnauthorized(w http.ResponseWriter, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate",
		`Bearer error="invalid_token", error_description="`+desc+`"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"code":16,"message":"` + desc + `"}`))
}

// isClientForgeableIdentityHeader reports whether an inbound header name /
// metadata key carries gateway-derived identity context that a client must never
// supply. The predicate covers the WHOLE reserved `x-kacho-` namespace in both
// surface forms (bare and grpc-gateway `grpc-metadata-`-bridged) — see
// principalmeta.IsClientForgeableKey for the namespace contract and for why this
// is a namespace sweep rather than a list of banned names.
//
// It previously enumerated two families (principal + token) and therefore missed
// `x-kacho-admin` / `x-kacho-project-id`, which kacho-compute and kacho-vpc read
// off the wire: a client set `Grpc-Metadata-X-Kacho-Admin: true`, the REST→gRPC
// bridge forwarded it, and compute raised its cluster-admin flag on the PUBLIC
// listener — bypassing the operation-ownership predicate, whose response carries
// the created resource in full. Enumerating names is what produced the hole; the
// namespace form cannot forget a key that does not exist yet.
func isClientForgeableIdentityHeader(lower string) bool {
	return principalmeta.IsClientForgeableKey(lower)
}

func extractBearer(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, h := range md.Get("authorization") {
		if v, ok := strings.CutPrefix(h, "Bearer "); ok {
			return v
		}
		if v, ok := strings.CutPrefix(h, "bearer "); ok {
			return v
		}
	}
	return ""
}

// HTTP — middleware для grpc-gateway REST mux. Парсит Authorization header
// и прокидывает его как metadata в gRPC ctx (стандартный grpc-gateway-форвард
// в `incomingHeaderMatcher`).
//
// Носителей личности на этом пути ДВА: сессия развёрнутого провайдера
// (`ory_kratos_session`) и подписанный предъявитель в `Authorization`. Третьего —
// переписывания cookie в Bearer — здесь больше нет: его единственным
// производителем был обработчик церемонии снятого провайдера, и он снят вместе
// с ним. Читатель, чей вход никто не может произвести, не остаётся.
func (a *AuthInterceptor) HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip client-forgeable identity headers before any auth path runs, then
		// try each strategy in fail-closed order. The `injected` flag from the
		// Kratos path suppresses the JWT paths (a resolved session wins). The
		// Hydra and dev-JWT paths are terminal when they apply: they either write
		// a 401 or serve `next` themselves and report handled=true.
		a.stripForgeableIdentityHeaders(r)

		// Полоса cookie ТЕРМИНАЛЬНА так же, как полоса предъявителя: она вправе
		// сама ответить отказом. Прежде она умела только «резолвил / не
		// резолвил», и отвергнуть сессию ей было нечем — оттого наш отзыв на ней
		// и не действовал (auth_session_cutoff.go).
		injected, handled := a.tryKratosSession(w, r)
		if handled {
			return
		}

		if !injected {
			// Полоса базового секрета — ПЕРЕД полосами подписанного и
			// терминальная, ровно как на нативной поверхности. Обе поверхности
			// края обязаны отвечать одинаково: расхождение между ними никто бы
			// не решал, оно возникло бы побочным эффектом.
			if a.tryBasicCredential(w, r, next) {
				return
			}
			if a.tryHydraJWT(w, r, next) {
				return
			}
			if a.tryDevSecretJWT(w, r, next) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// tryBasicCredential — полоса базового секрета на REST-поверхности края.
//
// Возвращает true, когда полоса ВЫНЕСЛА вердикт (положительный или
// отрицательный) и вызывающий обязан вернуться. Полоса ТЕРМИНАЛЬНА: строка с
// нашей маркой не уходит дальше как «удостоверения нет вовсе».
func (a *AuthInterceptor) tryBasicCredential(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if a.basicLane == nil {
		return false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !a.basicLane.Owns(tok) {
		return false
	}
	cred, err := a.basicLane.Verify(r.Context(), tok)
	switch {
	case errors.Is(err, ErrCredentialStateUnknown):
		// 503, а не 401: вызывающему нечего исправлять сменой удостоверения.
		http.Error(w, "credential state could not be established", http.StatusServiceUnavailable)
		return true
	case err != nil:
		writeHTTPUnauthorized(w, basicCredentialRefusalText())
		return true
	}
	// ДОСТАТОЧНО ЛИ СИЛЬНО АУТЕНТИФИЦИРОВАН ВЫЗЫВАЮЩИЙ ДЛЯ ЭТОГО ОБРАЩЕНИЯ
	// (#1215). Спрашивается ДО записи личности — ровно там же и по тем же
	// причинам, что на полосах сессии и предъявителя.
	//
	// До #1215 этого вопроса здесь не было вовсе, при том что полоса уровень
	// СТАВИЛА: заголовок для второго замка выставлялся, а первый замок не
	// спрашивался. Наблюдаемое следствие — глагол с полом «2» выполнялся по
	// базовому секрету уровня «1», то есть предъявительское удостоверение
	// чеканило себе смену вопреки объявлению `BasicCredentialLevel`.
	as := a.basicCredentialAssurance(cred, r.URL.Path)
	if _, suErr := a.stepUpVerdictHTTP(r, as, stepUpLaneBasic); suErr != nil {
		// Отказ рендерится ЕДИНЫМ отказом полосы, а не вызовом RFC 9470 —
		// байт-в-байт тем же ответом, что на негодном секрете. Разбор — в
		// auth_basic_stepup.go: различимый исход подтверждал бы годность
		// предъявленного по глаголу, который вызывающему недоступен.
		writeHTTPUnauthorized(w, basicCredentialRefusalText())
		return true
	}
	setPrincipalHeaders(r, cred.PrincipalType, cred.PrincipalID, cred.DisplayName)
	// УРОВЕНЬ УДОСТОВЕРЕНИЯ ДОЕЗЖАЕТ ДО СТРАЖА ПОВЫШЕНИЯ, и это несущее
	// (приёмка BAT-1 §5.2): поле, которое полоса не заполнила, делает
	// нижележащий контроль ПРОЙДЕННЫМ МИМО, а не успешно, — и это неотличимо
	// от исправной работы.
	//
	// Величина — константа «1» (BasicCredentialLevel), и следствие названо:
	// человек, предъявивший базовый секрет, не может ни выпустить новое
	// удостоверение, ни отозвать существующее (обоим глаголам объявлен порог
	// «2»), но обычное чтение с порогом «1» ему ДОСТУПНО. Долгоживущий
	// предъявительский секрет не должен уметь чеканить себе смену.
	//
	// Едет ОТОБРАЖЁННАЯ на ось каталога величина, а не сырая константа полосы:
	// второй замок обязан читать то же значение, по которому вынес вердикт
	// первый. Съехавшая с оси величина приезжает сюда пустой строкой — общее
	// правило ранжирует её нулём, и внутренний замок отказывает по той же
	// причине, по которой отказал бы внешний.
	r.Header.Set(principalmeta.HeaderTokenACR, as.ACR)
	r.Header.Set(principalmeta.HeaderGRPCMetaTokenACR, as.ACR)
	// ИДЕНТИФИКАТОР СТРОКИ УДОСТОВЕРЕНИЯ — ДЛЯ ПЕРЕПРОСА ОТЗЫВА НА ОТКРЫТОМ
	// СОЕДИНЕНИИ (kacho#1450).
	//
	// Перепрос спрашивает нашего авторитета по идентификатору, а не по
	// предъявленной строке: держать секрет живым весь срок соединения значило бы
	// завести поверхность хранения ради контроля. Собрать идентификатор ему
	// неоткуда — он видит только запрос, — поэтому его ставит полоса приёма,
	// единственная, кто его знает.
	//
	// Мостовой формы у ключа НЕТ намеренно: за краем читателя нет, и ключ
	// объявлен остающимся на краю (principalmeta.edgeOnlyKeys). Ставится ПОСЛЕ
	// вердикта полосы — негодное удостоверение сюда не доходит, и перепрос
	// никогда не спрашивает про то, чего край не принимал.
	r.Header.Set(principalmeta.HeaderTokenBasicCredentialID, cred.CredentialID)
	next.ServeHTTP(w, r)
	return true
}

// stripForgeableIdentityHeaders removes incoming X-Kacho-Principal-* (and the
// grpc-gateway `Grpc-Metadata-` / lower-case variants) so a client cannot forge
// `X-Kacho-Principal-Type: user` and bypass auth (full privilege escalation).
// These headers are set ONLY by the auth middleware after a resolved
// Bearer/Kratos credential.
func (a *AuthInterceptor) stripForgeableIdentityHeaders(r *http.Request) {
	for k := range r.Header {
		if isClientForgeableIdentityHeader(strings.ToLower(k)) {
			r.Header.Del(k)
		}
	}
}

// tryKratosSession resolves a Kratos session cookie (ory_kratos_session) to a
// principal BEFORE the JWT paths so SPA users without a Bearer get a principal.
//
// Возвращает ДВА значения, и это не косметика. `injected` — резолвилась ли
// личность (подавляет полосы предъявителя, как прежде). `handled` — полоса
// ответила САМА и вызывающий обязан вернуться. Прежде второго исхода у полосы
// не существовало вовсе, и отвергнуть сессию ей было нечем.
//
// Причин ответить самой ДВЕ, и обе — свой дефект того же класса «полоса не
// спрашивала того, что спрашивает соседняя»:
//
//   - сессия отвергнута НАШИМ отзывом либо вопрос о нём остался без ответа —
//     оттого наш глагол выхода на браузерной полосе не действовал ни разу
//     (auth_session_cutoff.go);
//   - предъявленный уровень уверенности не дотягивает до пола, объявленного
//     каталогом прав для вызываемого глагола, — оттого действие с полом уровня
//     «2» выполнялось из браузера без второго фактора (auth_session_stepup.go).
func (a *AuthInterceptor) tryKratosSession(w http.ResponseWriter, r *http.Request) (injected, handled bool) {
	if a.kratos == nil {
		return false, false
	}
	cookieHdr := r.Header.Get("Cookie")
	if !strings.Contains(cookieHdr, "ory_kratos_session") {
		return false, false
	}
	res := a.kratos.Whoami(r.Context(), cookieHdr)
	if !res.Active || res.IdentityID == "" {
		return false, false
	}
	var subj Subject
	var err error
	// Если lookuper поддерживает lazy-upsert (Kratos new-user path) — используем
	// его; иначе обычный lookup.
	if kl, ok := a.subjectLookup.(KratosSubjectLookuper); ok {
		subj, err = kl.LookupOrUpsertFromKratos(r.Context(), res.IdentityID, res.Email, res.DisplayName)
	} else {
		subj, err = a.subjectLookup.LookupByExternalID(r.Context(), res.IdentityID)
	}
	if err != nil {
		a.logger.Debug("auth.HTTP: Kratos SubjectLookup failed",
			"identity_id", res.IdentityID, "err", err.Error())
		return false, false
	}
	// Отзыв спрашивается ДО того, как личность попадёт в заголовки: принципал,
	// выставленный отвергнутой сессии, доехал бы до прав и до backend прежде,
	// чем отказ успел бы что-то значить.
	switch a.sessionCutoffCheck(r.Context(), subj, res, r.URL.Path) {
	case sessionCutoffEnded:
		// Отказ И окончание носителя — вместе. Порознь первое даёт СТОЯЩИЙ
		// отказ: сессия жива, момент её аутентификации прежний, и повторной
		// аутентификации ничто не запросит.
		endSessionCarrier(w)
		writeHTTPUnauthorized(w, sessionCutoffDenyDescription)
		return false, true
	case sessionCutoffUnanswered:
		// Носителя НЕ гасим: заминка своего же соседа не повод выкидывать тех,
		// кого никто не отзывал.
		writeHTTPUnauthorized(w, sessionCutoffUnavailableReason)
		return false, true
	case sessionCutoffNotAsked, sessionCutoffLive, sessionCutoffUnsupported:
		// Полоса продолжается. `unsupported` — окно раската, а не решение: край
		// впереди службы прав, и отвергать здесь значило бы уронить консоль на
		// время раската. Состояние сходится само и докладывается громко.
	}
	// Достаточно ли СИЛЬНО человек аутентифицировался ДЛЯ ЭТОГО обращения?
	// Спрашивается ровно там же, где на полосе предъявителя, и по тем же
	// причинам: до записи личности, чтобы не прошедший пол запрос не доехал ни
	// до прав, ни до backend.
	//
	// До #1201 этого вопроса здесь не было вовсе, и полоса не могла его задать:
	// уровень уверенности провайдера край не разбирал. Освобождения этой полосы
	// by design нет — пол есть свойство ВСЯКОГО обращения человека.
	assurance := a.sessionAssurance(subj, res, r.URL.Path)
	if a.enforceStepUpHTTP(w, r, assurance, stepUpLaneSession) {
		return false, true
	}
	// Уровень едет вперёд к ВТОРОМУ замку (iam `authzguard.ACRFloor`) — по тем же
	// именам, что у полосы предъявителя. Полоса, не выставляющая его, оставляет
	// внутренний замок без входа.
	reportUnusableAuthMethods(a.logger, a.authMethodsUnusable, stepUpLaneSession, r.URL.Path,
		setSessionAssuranceHeaders(r, assurance, res.AuthenticationMethods))
	r.Header.Set(principalmeta.HeaderPrincipalType, subj.Type)
	r.Header.Set(principalmeta.HeaderPrincipalID, subj.ID)
	r.Header.Set(principalmeta.HeaderPrincipalDisplay, subj.DisplayName)
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalType, subj.Type)
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalID, subj.ID)
	r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, subj.DisplayName)
	a.logger.Info("auth.HTTP: Principal injected (Kratos)",
		"type", subj.Type, "id", subj.ID, "identity_id", res.IdentityID)
	return true, false
}

// tryHydraJWT validates a Hydra-issued asymmetric (RS256/ES256/EdDSA) access JWT
// over REST via the JWKS verifier (parity with the gRPC interceptor path) and
// derives the principal from the verified `kacho_principal_*` claims (top-level
// or ext_claims), falling back to SubjectLookuper on the verified sub. A
// present-but-bad token → 401 fail-closed, never anonymous; a key set that could
// not be fetched → 503 (see keySourceUnanswerable — #1194), also fail-closed.
// Returns true when this path was terminal (401 written, or principal resolved
// and `next` served) — i.e. the caller must return. Returns false when the path
// does not apply (no verifier / not an asymmetric Bearer) so the next strategy
// runs.
func (a *AuthInterceptor) tryHydraJWT(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if a.verifier == nil {
		return false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !isAsymmetricJWT(tok) {
		return false
	}
	vt, verr := a.verifier.Verify(r.Context(), tok)
	if verr != nil {
		// ЧЬЯ ЭТО ОШИБКА — вопрос, который здесь задаётся до выбора кода (#1194).
		// Набор проверочных ключей не добыт ⇒ сбой зависимости: «повтори позже»
		// (503 / 14), и удостоверение предъявителя тут ни при чём. Всё
		// остальное — негодный токен: «войди заново» (401 / 16).
		if keySourceUnanswerable(verr) {
			// Громко: сюда попадает и ответ не по адресу, то есть неверная
			// настройка, которая иначе живёт вечно тихим предупреждением
			// (`security.md` §Hardening инв. 8).
			a.logger.Error("auth.HTTP: token key source unanswerable; refusing Unavailable",
				"path", r.URL.Path, "err", verr.Error())
			writeHTTPServiceUnavailable(w, keySourceUnavailableReason)
			return true
		}
		a.logger.Warn("auth.HTTP: Hydra JWT validate failed (JWKS)", "err", verr.Error())
		writeHTTPUnauthorized(w, "token validation failed")
		return true
	}
	// Machine principals must prove possession. Checked BEFORE the principal is
	// injected, so a violating token never reaches authz or a backend.
	if a.machineBindingViolation(vt) {
		a.logger.Warn("auth.HTTP: machine token is not sender-constrained; rejected",
			"path", r.URL.Path)
		writeHTTPUnauthorized(w, "sender-constrained token required")
		return true
	}
	// Is the token still live? The signature cannot answer that; the provider can.
	// Asked on the token this branch has already verified — never by parsing the
	// bearer a second time.
	//
	// The pre-auth allow-list is exempt, and sign-out is why: a user whose session
	// was revoked elsewhere must still be able to complete a sign-out and clear
	// their cookies. Refusing that would leave the browser holding a session it
	// cannot surrender. The health probes and the interactive login flow are in
	// the same list for the same reason — none of them acts on the credential's
	// authority.
	if !isPublicHTTPPath(r.URL.Path) {
		switch a.revocationCheck(r.Context(), vt, "rest", r.URL.Path) {
		case revocationRevoked:
			a.logger.Warn("auth.HTTP: token reported not live by the provider; rejected",
				"path", r.URL.Path)
			writeHTTPUnauthorized(w, revocationDenyDescription)
			return true
		case revocationUnanswerable:
			writeHTTPServiceUnavailable(w, revocationUnavailableReason)
			return true
		}
	}
	// Did the caller authenticate strongly enough for THIS call? Asked before the
	// principal is written, so a request that has not met the floor never reaches
	// authz or a backend. The floor lives here, not in the sender-constrained
	// middleware, because it is a question about every token — see auth_stepup.go.
	if a.enforceStepUpHTTP(w, r, assuranceFromVerifiedToken(vt), stepUpLaneBearer) {
		return true
	}
	// The credential's own context travels either way: it describes the token, not
	// the person, and the cluster-internal floor decides on the acr it finds here.
	reportUnusableAuthMethods(a.logger, a.authMethodsUnusable, stepUpLaneBearer, r.URL.Path,
		setTokenContextHeaders(r, vt))
	if pType, pID, display, perr := principalFromVerifiedToken(vt); perr == nil {
		setPrincipalHeaders(r, pType, pID, display)
		a.logger.Info("auth.HTTP: Principal injected (Hydra JWT)", "type", pType, "id", pID)
		next.ServeHTTP(w, r)
		return true
	}
	// Claims absent → fall back to SubjectLookuper on the verified sub.
	if vt.Subject == "" {
		a.logger.Warn("auth.HTTP: Hydra JWT has empty sub and no kacho_principal_* claims")
		writeHTTPUnauthorized(w, "token missing subject")
		return true
	}
	if subj, lerr := a.subjectLookup.LookupByExternalID(r.Context(), vt.Subject); lerr != nil {
		a.logger.Debug("auth.HTTP: SubjectLookup failed (Hydra JWT fallback)", "external_id", vt.Subject, "err", lerr.Error())
	} else if a.machineBindingViolationFor(vt, subj.Type) {
		// The lookup resolved a MACHINE principal from a token that carried no
		// kacho_principal_* claims. Same requirement, same rejection.
		a.logger.Warn("auth.HTTP: machine token (resolved by lookup) is not sender-constrained; rejected",
			"path", r.URL.Path)
		writeHTTPUnauthorized(w, "sender-constrained token required")
		return true
	} else {
		setPrincipalHeaders(r, subj.Type, subj.ID, subj.DisplayName)
		a.logger.Info("auth.HTTP: Principal injected (Hydra JWT fallback)", "type", subj.Type, "id", subj.ID)
	}
	next.ServeHTTP(w, r)
	return true
}

// tryDevSecretJWT validates a symmetric dev-secret Bearer JWT and injects the
// principal (service-account or User-lookup). A present-but-invalid token → 401
// fail-closed. Returns true only when the path was terminal (401, or an SA
// principal was injected and `next` served). For the ordinary User-lookup case
// it sets the headers and returns false so the caller serves `next` once at the
// end (preserving the original fall-through).
func (a *AuthInterceptor) tryDevSecretJWT(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if r.Header.Get("Authorization") == "" || len(a.devSecret) == 0 {
		return false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	claims, jwtErr := a.validateJWT(tok)
	if jwtErr != nil {
		// A Bearer header that is present but malformed / expired /
		// signature-invalid is a failed authN attempt → reject with
		// 401 BEFORE authz, never pass it through as anonymous (which
		// surfaces as 403).
		a.logger.Warn("auth.HTTP: JWT validate failed", "err", jwtErr.Error())
		writeHTTPUnauthorized(w, "token validation failed")
		return true
	}
	subjectID, _ := claims["sub"].(string)
	if subjectID == "" {
		a.logger.Warn("auth.HTTP: JWT has empty sub")
		writeHTTPUnauthorized(w, "token missing subject")
		return true
	}
	// Service Account / API-token principals.
	// A client_credentials / API token carries
	// `kacho_principal_type=service_account` + `kacho_sa_id`; `sub`
	// is the SA id, not a User external_id, so the User lookup below
	// would miss and leave the request principal-less → the authz
	// layer then denies it as unauthenticated. Resolve the SA
	// principal directly from the typed claims (parity with the
	// gRPC intercept path).
	if pt, _ := claims["kacho_principal_type"].(string); pt == "service_account" {
		saID, _ := claims["kacho_sa_id"].(string)
		if saID == "" {
			saID = subjectID
		}
		r.Header.Set(principalmeta.HeaderPrincipalType, "service_account")
		r.Header.Set(principalmeta.HeaderPrincipalID, saID)
		r.Header.Set(principalmeta.HeaderPrincipalDisplay, saID)
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalType, "service_account")
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalID, saID)
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, saID)
		a.logger.Info("auth.HTTP: SA principal injected", "id", saID)
		next.ServeHTTP(w, r)
		return true
	}
	subj, lookupErr := a.subjectLookup.LookupByExternalID(r.Context(), subjectID)
	if lookupErr != nil {
		a.logger.Debug("auth.HTTP: SubjectLookup failed", "external_id", subjectID, "err", lookupErr.Error())
	} else {
		// Plain headers — WithMetadata callback in restmux форвардит.
		r.Header.Set(principalmeta.HeaderPrincipalType, subj.Type)
		r.Header.Set(principalmeta.HeaderPrincipalID, subj.ID)
		r.Header.Set(principalmeta.HeaderPrincipalDisplay, subj.DisplayName)
		// Legacy form — grpc-gateway default convention fallback.
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalType, subj.Type)
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalID, subj.ID)
		r.Header.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, subj.DisplayName)
		a.logger.Info("auth.HTTP: Principal injected", "type", subj.Type, "id", subj.ID)
	}
	return false
}
