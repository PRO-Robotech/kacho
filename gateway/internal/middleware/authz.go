// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz.go — per-RPC AuthZ middleware for the api-gateway.
//
// Position in the middleware chain (mounted after the JWT-verifier):
//
//	DPoP → JWT verifier → mTLS-bound → Step-up → AUTHZ → handler
//
// Pipeline per request:
//
//  1. Resolve the gRPC FQN (gRPC-server path: trivially `info.FullMethod`;
//     HTTP path: best-effort REST-to-FQN mapping via the explicit route
//     table — see RestRouter below).
//
//  2. Lookup permission catalog entry (`PermissionCatalog`). Missing entry
//     OR `IsExempt` → bypass. Public allow-list is independent (login,
//     health, recovery) and overrides catalog absence.
//
//  3. Extract subject from verified JWT (SubjectExtractor). No subject →
//     deny unless the entry is on the explicit anonymous allowlist
//     (per-route override or `<exempt>` catalog).
//
//  4. Extract resource id (ResourceExtractor) using the catalog's
//     `scope_extractor` directive.
//
//  5. Build Conditions context (ContextExtractor).
//
//  6. Decision cache lookup — LRU 10k entries / 5s TTL, keyed on
//     (subject, action, resource_type:resource_id, acr, mfa_at, source_ip).
//     Hit → reuse decision. Miss → call IAM AuthorizeService.Check.
//
//  7. On allow → pass through. On deny → build PermissionDenied with
//     PreconditionFailure violations + WWW-Authenticate when reasons
//     suggest step-up. On IAM error → fail-closed (Unavailable) unless
//     `KACHO_API_GATEWAY_AUTHZ_FAIL_OPEN=true`.
//
//  8. Always emit metric + structured log.
//
// Configuration is supplied via `AuthzMiddlewareConfig` constructed in
// main.go from `config.Config`. No global state.
package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
)

// AuthzCheckInput — caller-friendly Check arguments. Mirrors
// `clients.AuthorizeCheckInput` to avoid a middleware → clients import
// cycle (clients itself imports middleware for Subject / ResolvedSubject
// types). Adapters convert between the two shapes.
type AuthzCheckInput struct {
	Subject string
	Action  string
	// RequiredRelation — explicit FGA relation from the catalog. When
	// non-empty, IAM honors this as-is instead of
	// deriving from action verb. Required for admin-only RPCs whose
	// `*.list`/`*.get` verb would resolve to `viewer` and slip through
	// the `cluster.viewer = user:*` cascade.
	RequiredRelation string
	ResourceType     string
	ResourceID       string
	Context          map[string]any
	TraceID          string
}

// AuthzCheckResult — caller-friendly Check result.
type AuthzCheckResult struct {
	Allowed     bool
	DenyReasons []string
	CheckedAt   time.Time
}

// AuthorizeChecker — narrowed dependency (mock-able). The clients package
// supplies a thin adapter; tests pass a closure / mock struct.
type AuthorizeChecker interface {
	Check(ctx context.Context, in AuthzCheckInput) (AuthzCheckResult, error)
}

// AuthzMiddlewareConfig — DI bag.
type AuthzMiddlewareConfig struct {
	// Enabled — master toggle. When false the middleware is a no-op
	// pass-through (legacy compatibility).
	Enabled bool

	// FailOpen — when true, IAM unreachable / Check returning error
	// permits the request (logged as ERROR). Default false (production
	// fail-closed — never leak access on failure).
	FailOpen bool

	// Catalog — permission lookup. Required when Enabled=true.
	Catalog *PermissionCatalog

	// Subjects — JWT → ResolvedSubject. Required when Enabled=true.
	Subjects *SubjectExtractor

	// Context — JWT + request → Condition-context. Required when Enabled=true.
	Context *ContextExtractor

	// Resources — request → ResourceID. Required when Enabled=true.
	Resources *ResourceExtractor

	// Checker — IAM AuthorizeService client (or test mock). Required when
	// Enabled=true.
	Checker AuthorizeChecker

	// Overrides — per-route override registry (file-based, SIGHUP reload).
	// Optional.
	Overrides *AuthzOverrides

	// Metrics — counters + histograms. When nil a fresh sink is allocated.
	Metrics *AuthzMetrics

	// Logger — slog. Required when Enabled=true.
	Logger *slog.Logger

	// Now — clock injection for tests. Defaults to time.Now.
	Now func() time.Time

	// CacheTTL — decision-cache TTL, i.e. the edge's revocation window. Left at
	// or below zero it takes authz.RevocationPolicy.Default, which is the ONE
	// place that number is chosen; see RevocationPolicy for what the window
	// means and why it is a security parameter rather than a tuning constant.
	CacheTTL time.Duration

	// CacheMaxEntries — LRU cap. Default 10000 (spec).
	CacheMaxEntries int

	// PublicAllowlist — gRPC FQNs that ALWAYS pass without subject /
	// catalog check. Login flow, health, recovery — set this in main.go.
	PublicAllowlist []string

	// RestRouter — best-effort REST-path → gRPC-FQN mapping. nil → only
	// path-prefix-based mapping (see grpcMethodForPath in dpop_http_middleware).
	RestRouter RestRouteResolver
}

// RestRouteResolver — interface to map an HTTP path/method to a gRPC FQN.
// Implementations may parse google.api.http annotations or a hand-rolled
// route table.
type RestRouteResolver interface {
	Resolve(httpMethod, httpPath string) (fqn string, ok bool)
}

// AuthzMiddleware — gRPC + HTTP middleware orchestrator.
type AuthzMiddleware struct {
	cfg     AuthzMiddlewareConfig
	cache   *decisionCache
	allow   map[string]struct{}
	metrics *AuthzMetrics
	now     func() time.Time
}

// NewAuthzMiddleware constructs the middleware from cfg. Returns an error
// when required fields are missing.
func NewAuthzMiddleware(cfg AuthzMiddlewareConfig) (*AuthzMiddleware, error) {
	if !cfg.Enabled {
		// no-op stand-in: callers may still wire it into the chain and rely
		// on the pass-through; missing deps are silently tolerated.
		if cfg.Logger == nil {
			cfg.Logger = slog.Default()
		}
		return &AuthzMiddleware{cfg: cfg, metrics: NewAuthzMetrics(), now: time.Now}, nil
	}
	if cfg.Catalog == nil {
		return nil, errors.New("authz middleware: Catalog is required")
	}
	if cfg.Subjects == nil {
		return nil, errors.New("authz middleware: Subjects is required")
	}
	if cfg.Context == nil {
		return nil, errors.New("authz middleware: Context is required")
	}
	if cfg.Resources == nil {
		return nil, errors.New("authz middleware: Resources is required")
	}
	if cfg.Checker == nil {
		return nil, errors.New("authz middleware: Checker is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("authz middleware: Logger is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	// Read the declared policy, never a literal here. A positive verdict is
	// cached, so this number IS the edge's revocation window — how long a grant
	// that was taken away keeps working when the proactive drop has not arrived.
	// While it was an anonymous literal on this line, the edge had a second,
	// undeclared source for the same security parameter, and changing it would
	// have moved the window with nothing going red.
	//
	// Разрешение «неположительное ⇒ умолчание политики» живёт в САМОЙ политике и
	// зовётся отсюда: ту же функцию зовёт страж старта, судящий эту величину.
	// Две копии правила отвечали бы одинаково на всяком положительном входе и
	// по-разному на нуле — то есть расходились бы ровно на посадке, где ручку не
	// трогали.
	cfg.CacheTTL = authz.RevocationPolicy.Resolve(cfg.CacheTTL)
	if cfg.CacheMaxEntries <= 0 {
		cfg.CacheMaxEntries = 10000
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = NewAuthzMetrics()
	}
	// Посадка объявляется ЗДЕСЬ — в единственном месте, где она известна.
	// Выключенная проверка пропускает всё, накопитель при этом собран, и все
	// полосы стоят нулями: без этой строки «проверка выключена» и «трафика не
	// было» на поверхности сбора неразличимы (#798).
	metrics.SetEnforcing(cfg.Enabled)

	allow := make(map[string]struct{}, len(cfg.PublicAllowlist))
	for _, fqn := range cfg.PublicAllowlist {
		allow[fqn] = struct{}{}
	}

	return &AuthzMiddleware{
		cfg:     cfg,
		cache:   newDecisionCache(cfg.CacheMaxEntries, cfg.CacheTTL, cfg.Now),
		allow:   allow,
		metrics: metrics,
		now:     cfg.Now,
	}, nil
}

// Metrics returns the metrics sink.
//
// Величины ВЫХОДЯТ наружу: их читает коллектор `gateway/internal/observability/metrics`,
// зарегистрированный композиционным корнем на диагностической поверхности края
// (`KACHO_API_GATEWAY_METRICS_ADDR`, умолчание `:9095`). Прежняя редакция этой
// строки утверждала обратное — «эндпоинта нет, накопитель живёт только в
// процессе» — и пережила свой предмет: поверхность появилась, а комментарий
// остался и читался как объяснение, почему числа края смотреть негде.
func (m *AuthzMiddleware) Metrics() *AuthzMetrics { return m.metrics }

// subjectChangingFQNs — полные имена методов, чей успех меняет права субъекта.
//
// На ответе 2xx край гасит свой кэш решений НЕМЕДЛЕННО, поэтому реплика,
// обслужившая мутацию, отвечает по новому состоянию уже на следующем запросе.
// Соседние реплики сходятся отдельной полосой — чтением журнала смены субъекта
// курсором, которое край открывает сам.
//
// # Перечень ОБЯЗАН совпадать с производителями журнала
//
// Полосы у механизма две, и перечни у них ведутся врозь: здесь — имена методов,
// там — вызовы записи в очередь. Заведя шестого производителя, легко не вспомнить
// об этом наборе; тогда соседние реплики сойдутся, а обслужившая мутацию
// продолжит отвечать по закешированному вердикту — то есть по ОТОЗВАННОМУ праву,
// и дольше всего именно там, где пользователь только что нажал «отозвать».
//
// Сходимость перечней держит гейт дерева `internal/repohygiene`
// `TestSelfFlushCoversEveryProducerOfTheSubjectChangeQueue`: он считает ОБЕ
// величины и печатает их обе. Одно число скрыло бы ровно тот случай, ради
// которого он заведён.
//
// Каждому имени ниже отвечает вызов `EmitSubjectChangeEvent` в use-case владельца
// прав. `AccessBindingService/Update` в набор НЕ входит намеренно: он правит метки
// и защиту от удаления, прав не меняет и в журнал не пишет.
//
// Набор доступен только на чтение после инициализации пакета — не менять в рантайме.
var subjectChangingFQNs = map[string]struct{}{
	"kaname.cloud.iam.v1.AccessBindingService/Create": {},
	"kaname.cloud.iam.v1.AccessBindingService/Delete": {},
	// Мягкий отзыв: строка привязки остаётся, набор отношений снимается — для
	// вердикта это то же, что удаление, и кэш обязан погаснуть так же.
	"kaname.cloud.iam.v1.AccessBindingService/Revoke": {},
	// Членство в группе меняет права, не трогая ни одной привязки: право выдано
	// ГРУППЕ, а состав её здесь и меняется.
	"kaname.cloud.iam.v1.GroupService/AddMember":    {},
	"kaname.cloud.iam.v1.GroupService/RemoveMember": {},
}

// MaybeFlushOnMutation flushes the decision cache when fqn is a grant-changing
// RPC and the proxied response was successful (HTTP 2xx). Safe to call on every
// request — it is a no-op for non-mutating FQNs and non-2xx responses.
func (m *AuthzMiddleware) MaybeFlushOnMutation(fqn string, httpStatus int) {
	if m.cache == nil || httpStatus < 200 || httpStatus >= 300 {
		return
	}
	if _, ok := subjectChangingFQNs[normalizeFQN(fqn)]; !ok {
		return
	}
	m.cache.Invalidate()
	m.cfg.Logger.Info("authz decision-cache flushed on grant mutation", "fqn", fqn)
}

// InvalidateCache flushes the whole authz decision cache. Used by the
// subject-change watcher to converge this replica after a grant change
// observed on another replica. No-op when authz is disabled (cache is nil).
func (m *AuthzMiddleware) InvalidateCache() {
	if m.cache != nil {
		m.cache.Invalidate()
	}
}

// Reload re-reads the permission catalog and authz overrides from the on-disk
// paths remembered at startup (LoadFromFile), then flushes the decision cache
// so the next request re-evaluates against the fresh config. It is the reload
// primitive the SIGHUP handler drives (ConfigMap staged rollout / emergency
// override), so an operator's config edit applies without a pod restart.
//
// Components backed by the embedded asset (no file path — the default catalog,
// an unset overrides file) are skipped: there is nothing on disk to re-read.
// Individual reload failures preserve the previous-good config (see
// LoadFromFile) and are returned joined; the caller keeps serving. No-op when
// authz is disabled.
func (m *AuthzMiddleware) Reload() error {
	if m == nil || !m.cfg.Enabled {
		return nil
	}
	var (
		errs     []error
		reloaded bool
	)
	if c := m.cfg.Catalog; c != nil && c.path.Load() != nil {
		reloaded = true
		if err := c.Reload(); err != nil {
			errs = append(errs, fmt.Errorf("permission catalog: %w", err))
		}
	}
	if o := m.cfg.Overrides; o != nil && o.path.Load() != nil {
		reloaded = true
		if err := o.Reload(); err != nil {
			errs = append(errs, fmt.Errorf("authz overrides: %w", err))
		}
	}
	if reloaded {
		m.InvalidateCache()
	}
	return errors.Join(errs...)
}

// ПОРТА ИНВАЛИДАЦИИ ИЗВНЕ ЗДЕСЬ НЕТ — снят вместе со своим единственным
// потребителем (задача #1024).
//
// Он отдавал наружу две ручки кэша решений: пообъектный сброс по субъекту и
// сброс целиком. Звала их ОДНА служба — та, которой iam гасил кэш края,
// дозваниваясь до его внутреннего слушателя. Направление развёрнуто: край сам
// читает журнал смены субъекта и гасит свой кэш сам, изнутри
// (`InvalidateCache`), а модулей, зовущих край, не осталось ни одного.
//
// Порт, у которого нет вызывающего, не бесплатен: он объявляет способность,
// которой никто не пользуется, и следующий читатель принимает её за живой
// механизм отзыва.

// Unary returns a gRPC UnaryServerInterceptor enforcing per-RPC authz.
func (m *AuthzMiddleware) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !m.cfg.Enabled {
			return handler(ctx, req)
		}
		fqn := normalizeFQN(info.FullMethod)
		decision := m.decide(ctx, decisionRequest{
			FQN:      fqn,
			ProtoReq: req,
			GRPCPeer: peerAddr(ctx),
			GRPCMeta: incomingMD(ctx),
		})
		switch decision.outcome {
		case outcomeAllow:
			resp, hErr := handler(ctx, req)
			if hErr == nil {
				m.MaybeFlushOnMutation(fqn, 200)
			}
			return resp, hErr
		case outcomeDeny:
			return nil, decision.gRPCStatus().Err()
		case outcomeUnauthenticated:
			// No credentials → Unauthenticated(16), not PermissionDenied(7).
			return nil, decision.gRPCStatus().Err()
		case outcomeInvalidArgument:
			// Request rejected on its own shape (malformed resource id, ambiguous
			// scope) → InvalidArgument(3), Check not run.
			return nil, decision.gRPCStatus().Err()
		case outcomeNotFound:
			// Hide existence: read-deny on a verb-bearing IAM read → NotFound(5).
			return nil, decision.gRPCStatus().Err()
		case outcomeError:
			if m.cfg.FailOpen {
				m.metrics.RecordErrorPassed()
				m.cfg.Logger.Error("authz middleware fail-open: passing request despite error",
					"fqn", fqn, "err", decision.checkErr)
				return handler(ctx, req)
			}
			m.metrics.RecordErrorRefused()
			// Redact the raw backend/transport detail from the client message —
			// leaking it aids fabric mapping. The code is preserved (retryable,
			// fail-closed) and the detail is already logged in decide().
			return nil, status.Error(codes.Unavailable, "authz service unavailable")
		default:
			return handler(ctx, req)
		}
	}
}

// Stream returns a gRPC StreamServerInterceptor enforcing per-RPC authz.
// Streaming RPCs are gated once before the stream runs; the messages flowing
// through it inherit the decision.
func (m *AuthzMiddleware) Stream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !m.cfg.Enabled {
			return handler(srv, ss)
		}
		fqn := normalizeFQN(info.FullMethod)
		decision := m.decide(ss.Context(), decisionRequest{
			FQN:      fqn,
			ProtoReq: nil, // stream requests aren't materialised yet
			GRPCPeer: peerAddr(ss.Context()),
			GRPCMeta: incomingMD(ss.Context()),
			Stream:   true,
		})
		switch decision.outcome {
		case outcomeAllow:
			return handler(srv, ss)
		case outcomeDeny:
			return decision.gRPCStatus().Err()
		case outcomeUnauthenticated:
			// No credentials → Unauthenticated(16), not PermissionDenied(7).
			return decision.gRPCStatus().Err()
		case outcomeInvalidArgument:
			// Request rejected on its own shape (malformed resource id, ambiguous
			// scope) → InvalidArgument(3), Check not run.
			return decision.gRPCStatus().Err()
		case outcomeNotFound:
			// Hide existence: read-deny on a verb-bearing IAM read → NotFound(5).
			return decision.gRPCStatus().Err()
		case outcomeError:
			if m.cfg.FailOpen {
				m.metrics.RecordErrorPassed()
				m.cfg.Logger.Error("authz middleware fail-open: passing stream despite error",
					"fqn", fqn, "err", decision.checkErr)
				return handler(srv, ss)
			}
			m.metrics.RecordErrorRefused()
			// Redact the raw backend/transport detail from the client message —
			// leaking it aids fabric mapping. The code is preserved (retryable,
			// fail-closed) and the detail is already logged in decide().
			return status.Error(codes.Unavailable, "authz service unavailable")
		default:
			return handler(srv, ss)
		}
	}
}

// HTTP returns an http.Handler middleware enforcing per-RPC authz on the
// REST surface.
func (m *AuthzMiddleware) HTTP(next http.Handler) http.Handler {
	if !m.cfg.Enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The DPoP middleware already short-circuits health/auth-flow URLs. We
		// still guard here for completeness when this middleware is mounted
		// without it (dev mode).
		if isPublicHTTPPath(r.URL.Path) {
			// СВОЯ полоса. Раньше этот проход не попадал НИ В ОДНУ, и число
			// «решений в секунду», снятое с края, было занижено на весь публичный
			// трафик — а среди путей списка есть продуктовые, а не только пробы
			// живости. Полоса нужна ровно затем, чтобы путь, которому в списке не
			// место, был виден, а не растворялся в тишине (#798).
			m.metrics.RecordPublicPath()
			next.ServeHTTP(w, r)
			return
		}
		fqn := m.resolveRestFQN(r)
		decision := m.decide(r.Context(), decisionRequest{
			FQN:     fqn,
			HTTPReq: r,
		})
		switch decision.outcome {
		case outcomeAllow:
			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r)
			m.MaybeFlushOnMutation(fqn, rw.statusCode)
		case outcomeDeny:
			challenge := ""
			if shouldStepUpChallenge(decision.reasons) {
				// Build a step-up challenge using the catalog's
				// required_acr_min as the target.
				challenge = `Bearer error="insufficient_user_authentication", acr_values="` +
					decision.requiredACRMin() + `"`
			}
			writeHTTPDeny(w, decision.descriptor, decision.reasons, challenge)
		case outcomeUnauthenticated:
			// No credentials → 401 Unauthorized + code 16,
			// not 403 Forbidden + code 7.
			writeHTTPUnauth(w, decision.descriptor, decision.reasons)
		case outcomeInvalidArgument:
			// Request rejected on its own shape (malformed resource id, ambiguous
			// scope) → 400 + code 3, Check not run.
			writeHTTPInvalidArg(w, decision.invalidArgMessage)
		case outcomeNotFound:
			// Hide existence: read-deny on a verb-bearing IAM read → 404, no reasons.
			writeHTTPNotFound(w, decision.descriptor)
		case outcomeError:
			if m.cfg.FailOpen {
				m.metrics.RecordErrorPassed()
				m.cfg.Logger.Error("authz middleware fail-open: passing http request despite error",
					"path", r.URL.Path, "err", decision.checkErr)
				next.ServeHTTP(w, r)
				return
			}
			m.metrics.RecordErrorRefused()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":14,"message":"authz service unavailable"}`))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// ---- decide() — single decision path used by Unary / Stream / HTTP ----

type decisionRequest struct {
	FQN      string
	ProtoReq any
	HTTPReq  *http.Request
	GRPCPeer string
	GRPCMeta metadata.MD
	// Stream marks the stream-interceptor lane, where the client request message
	// is not read before the RPC is gated (ProtoReq is therefore nil). Set ONLY
	// by the Stream interceptor, so phaseResource can tell an unmaterialised
	// request apart from one it can read fields off, and fail closed for a
	// concrete-resource scope instead of collapsing to the wildcard scope.
	//
	// It does NOT mean "the RPC is a stream": the gateway forwards domain traffic
	// via UnknownServiceHandler, which grpc-go dispatches through the streaming
	// path, so proxied UNARY RPCs set this too. See phaseResource.
	Stream bool
}

type decisionOutcome int

const (
	outcomeAllow decisionOutcome = iota
	outcomeDeny
	outcomeError
	// outcomeUnauthenticated — request carries NO credentials at all (no valid
	// JWT / no authenticated subject). Maps to gRPC Unauthenticated(16) / HTTP
	// 401. Distinct from outcomeDeny, which is reserved for authenticated
	// subjects whose FGA check was denied (→ 7/403).
	//
	// gRPC/HTTP convention (RFC 7235, gRPC status code guide):
	//   missing/invalid credentials → 16 UNAUTHENTICATED → HTTP 401
	//   authenticated subject, access denied → 7 PERMISSION_DENIED → HTTP 403
	outcomeUnauthenticated
	// outcomeInvalidArgument — the extracted per-resource scope id is
	// syntactically malformed (unknown 3-char prefix / wrong length). Maps to
	// gRPC InvalidArgument(3) / HTTP 400. The FGA Check
	// must NOT run for a malformed id — a no-FGA-path deny would surface as 403,
	// masking the Kachō-convention 400 the handler itself returns first. The
	// gateway cannot tell malformed from well-formed-but-nonexistent, so ONLY the
	// malformed (wrong-prefix / wrong-length) case is short-circuited here;
	// well-formed-nonexistent stays a 403 deny (existence-leak protection).
	outcomeInvalidArgument
	// outcomeNotFound — an authz deny on a hide-existence read RPC (catalog
	// HideExistence / IAM verb-bearing `v_get` read). Maps to gRPC NotFound(5) /
	// HTTP 404 with NO deny reasons. The gateway Check runs BEFORE the resource
	// owner (kaname), which itself returns NotFound for a denied read; surfacing
	// a 403 here would override that hide-existence contract and leak both existence
	// and the deny reasons. Enforcement is unchanged — the deny still blocks the
	// request and the handler is never reached; only the surfaced code/body differ.
	// A well-formed-but-nonexistent id and an existing-but-denied id both yield the
	// same FGA deny → the same NotFound → no enumeration leak.
	outcomeNotFound
)

type decision struct {
	outcome    decisionOutcome
	reasons    []string
	descriptor permissionDeniedDescriptor
	checkErr   error
	entry      CatalogEntry
	// invalidArgMessage — the flat message rendered for outcomeInvalidArgument,
	// on both the gRPC and the REST arm. Set by whichever phase produced the
	// outcome (malformed resource id, contradictory scope sources).
	invalidArgMessage string
}

func (d decision) gRPCStatus() *status.Status {
	switch d.outcome {
	case outcomeUnauthenticated:
		return buildGRPCUnauthStatus(d.descriptor, d.reasons)
	case outcomeInvalidArgument:
		return buildGRPCInvalidArgStatus(d.invalidArgMessage)
	case outcomeNotFound:
		return buildGRPCNotFoundStatus(d.descriptor)
	default:
		return buildGRPCDenyStatus(d.descriptor, d.reasons)
	}
}

// requiredACRMin returns the catalog-declared ACR floor (default "2").
func (d decision) requiredACRMin() string {
	if d.entry.RequiredACRMin == "" {
		return "2"
	}
	return d.entry.RequiredACRMin
}

// isExternalRequest reports whether the request must be treated as arriving
// from the external edge (fail-closed default). For the HTTP path the origin
// marker lives in the request context (set per-listener by
// listenerorigin.InternalConnContext, which marks ONLY the dedicated
// cluster-internal admin listener). For the gRPC path there is no HTTP request
// and Internal* RPCs never reach this
// middleware via the gateway (the proxy routing blocks them), so we fail closed —
// treat an unknown origin as external, meaning the internal-origin gate does NOT
// admit it and the normal catalog/authN path decides.
func (m *AuthzMiddleware) isExternalRequest(dr decisionRequest) bool {
	if dr.HTTPReq != nil {
		return listenerorigin.IsExternal(dr.HTTPReq.Context())
	}
	// No HTTP request → gRPC path. Fail closed for the internal-origin gate.
	return true
}

func (m *AuthzMiddleware) decide(ctx context.Context, dr decisionRequest) decision {
	start := m.now()
	defer func() {
		m.metrics.ObserveCheckDuration(m.now().Sub(start))
	}()

	// The decision pipeline is a sequence of phases, each of which may return a
	// terminal decision (handled=true) or defer to the next. Ordering is
	// security-critical and preserved exactly; see each phase's doc.

	// 1. Public-allowlist short-circuit.
	if dec, handled := m.phaseAllowlist(dr); handled {
		return dec
	}
	// 1b. Internal-listener-origin exempt gate.
	if dec, handled := m.phaseInternalOriginExempt(dr); handled {
		return dec
	}
	// 2. Per-route file override (allow/deny).
	if dec, handled := m.phaseOverride(dr); handled {
		return dec
	}
	// 3. Catalog lookup (exempt-allow / exempt-unauth / catalog-miss deny).
	entry, dec, handled := m.phaseCatalog(ctx, dr)
	if handled {
		return dec
	}
	// 4. Subject extraction (unauthenticated → 401).
	verified, subj, dec, handled := m.phaseSubject(ctx, dr, entry)
	if handled {
		return dec
	}
	// 4b. Scope-filtered short-circuit — AFTER subject extraction, on purpose.
	if dec, handled := m.phaseScopeFiltered(dr, entry); handled {
		return dec
	}
	// 5. Resource-scope resolution (+ 5b malformed-id short-circuit).
	resourceID, resourceType, descriptor, dec, handled := m.phaseResource(dr, entry, subj)
	if handled {
		return dec
	}
	// 6–8. Context build, decision-cache read, IAM Check.
	return m.phaseCheck(ctx, dr, entry, verified, subj, resourceID, resourceType, descriptor)
}

// phaseAllowlist admits FQNs on the public allowlist (login, health, recovery)
// without any subject/catalog check.
func (m *AuthzMiddleware) phaseAllowlist(dr decisionRequest) (decision, bool) {
	if _, ok := m.allow[dr.FQN]; ok {
		// СВОЯ полоса, не `allow`: модель прав не спрашивали, и обойдена не только
		// она, но и authN. Слитое в `allow`, это выглядело бы выдачей (#798).
		m.metrics.RecordAllowlist()
		return decision{outcome: outcomeAllow, descriptor: permissionDeniedDescriptor{FQN: dr.FQN}}, true
	}
	return decision{}, false
}

// phaseInternalOriginExempt admits an `<exempt>` Internal* RPC ONLY when it
// arrived on the cluster-internal listener (not the advertised external TLS
// listener). Internal callers (api-gateway self-call, kaname drainer,
// port-forward admin) carry no external user JWT, so the catalog's authN-
// enforcing exempt path would otherwise 401 them. Gated Internal* RPCs (a real
// `required_relation`, e.g. InternalClusterService) are NOT bypassed — they run
// the full subject-extraction + FGA Check below even on the internal listener.
// An external caller of any Internal* RPC is not admitted here and falls through
// to the normal catalog/authN path (deny / 401); the REST dispatcher
// independently 404s these on the external listener.
//
// SECURITY — be exact about what this phase is: it admits the request WITHOUT
// EXTRACTING A PRINCIPAL, and the listener it trusts is plain HTTP/1.1 with no
// TLS and no client certificate (cmd/api-gateway/main.go). The only thing it
// verifies is WHERE the connection arrived — i.e. NETWORK POSITION, which is not
// a credential ("internal = trusted" is a forbidden premise, security.md). Any
// RPC routed through this phase is effectively callable by anything that can
// reach the `internal-rest` Service port or hold a port-forward.
//
// Therefore: an RPC that MINTS CREDENTIALS or GRANTS PRIVILEGE must never be
// registered on the REST mux at all — no route, rather than an exempt route.
// InternalBootstrapTokenService/MintBootstrapToken (a cluster `system_admin`
// token mint) is deliberately unrouted for exactly this reason; it lives only on
// iam :9091 behind a client-certificate SAN allow-list. See restmux/mux.go and
// restmux/bootstrap_token_no_rest_route_test.go. The RPCs that legitimately pass
// here are identity-bootstrap / hot-path lookups that necessarily run before a
// principal exists and that hand out nothing on their own.
func (m *AuthzMiddleware) phaseInternalOriginExempt(dr decisionRequest) (decision, bool) {
	if allowlist.HasInternalSuffix("/"+dr.FQN) && !m.isExternalRequest(dr) {
		if entry, found := m.cfg.Catalog.Lookup(dr.FQN); found && entry.IsExempt() {
			// СВОЯ полоса: допуск держится на СЕТЕВОЙ ПОЗИЦИИ, которая
			// удостоверением не является, и принципал здесь не извлекался вовсе.
			// Ровно этот поток обязан быть виден отдельно (#798).
			m.metrics.RecordInternalOrigin()
			return decision{
				outcome:    outcomeAllow,
				descriptor: permissionDeniedDescriptor{FQN: dr.FQN, Action: entry.Permission},
				entry:      entry,
			}, true
		}
	}
	return decision{}, false
}

// phaseOverride applies a file-based per-route override (explicit allow/deny).
func (m *AuthzMiddleware) phaseOverride(dr decisionRequest) (decision, bool) {
	if m.cfg.Overrides == nil {
		return decision{}, false
	}
	dec, ok := m.cfg.Overrides.Lookup(dr.FQN)
	if !ok {
		return decision{}, false
	}
	switch dec {
	case OverrideAllow:
		// СВОЯ полоса: это правка файла руками. Её след обязан быть отличим от
		// выдачи сильнее всех прочих (#798).
		m.metrics.RecordOverrideAllow()
		m.cfg.Logger.Info("authz override allow", "fqn", dr.FQN)
		return decision{outcome: outcomeAllow, descriptor: permissionDeniedDescriptor{FQN: dr.FQN}}, true
	case OverrideDeny:
		m.metrics.RecordDeny()
		m.cfg.Logger.Info("authz override deny", "fqn", dr.FQN)
		return decision{
			outcome:    outcomeDeny,
			reasons:    []string{"override: explicit deny"},
			descriptor: permissionDeniedDescriptor{FQN: dr.FQN},
		}, true
	}
	return decision{}, false
}

// phaseCatalog looks up the catalog entry. It returns handled=true for the two
// terminal catalog outcomes: an `<exempt>` entry (allow, or 401 when
// unauthenticated) and a catalog miss (deny). Otherwise it returns the resolved
// entry with handled=false so the pipeline continues to subject extraction.
func (m *AuthzMiddleware) phaseCatalog(ctx context.Context, dr decisionRequest) (CatalogEntry, decision, bool) {
	entry, found := m.cfg.Catalog.Lookup(dr.FQN)
	if found && entry.IsExempt() {
		// `<exempt>` skips the FGA authz check, NOT authentication.
		// An exempt RPC (scope-filter List, tenant-wide catalog read) still
		// requires an authenticated principal — the handler's own scope-filter
		// is meaningless for an anonymous caller. Without this gate an
		// anonymous request (no Bearer → injected system:anonymous principal)
		// would reach exempt List RPCs and get a 200 empty page instead of 401.
		exemptVerified, _ := verifiedTokenFromCtxOrHTTP(ctx, dr.HTTPReq)
		if _, authned := m.cfg.Subjects.Extract(exemptVerified); !authned {
			// No credentials on an exempt RPC → Unauthenticated(16),
			// not PermissionDenied(7). The request was not even authenticated; a
			// "deny" response would mislead callers into thinking they are
			// authenticated but forbidden.
			m.metrics.RecordDeny()
			return entry, decision{
				outcome: outcomeUnauthenticated,
				reasons: []string{"subject: unauthenticated request"},
				descriptor: permissionDeniedDescriptor{
					FQN:    dr.FQN,
					Action: entry.Permission,
				},
				entry: entry,
			}, true
		}
		// СВОЯ полоса: каталог снял вопрос МОДЕЛИ (принципал при этом установлен —
		// см. ветку выше). «Права есть» здесь никто не утверждал (#798).
		m.metrics.RecordExempt()
		return entry, decision{
			outcome:    outcomeAllow,
			descriptor: permissionDeniedDescriptor{FQN: dr.FQN, Action: entry.Permission},
			entry:      entry,
		}, true
	}
	if !found {
		// Production policy: deny when catalog has no entry — every RPC must
		// be classified. Dev / staging may surface this differently via the
		// overrides file (explicit allow).
		//
		// Classify the denial reason based on authentication status
		// so the caller (and observability) can distinguish:
		//   - authenticated caller hitting an uncatalogued method →
		//     PermissionDenied ("catalog: no entry for method") — 403
		//   - unauthenticated caller hitting an uncatalogued method →
		//     PermissionDenied ("catalog: no entry for method; unauthenticated")
		// Both are code 7 (PermissionDenied) — we never reveal internal
		// resource existence to unauthenticated callers, and we don't upgrade
		// to Unauthenticated (16) for uncatalogued methods because the method
		// itself is unknown/denied regardless of auth state.
		missVerified, _ := verifiedTokenFromCtxOrHTTP(ctx, dr.HTTPReq)
		_, isAuthed := m.cfg.Subjects.Extract(missVerified)
		missReason := "catalog: no entry for method"
		if !isAuthed {
			missReason = "catalog: no entry for method; unauthenticated"
		}
		m.metrics.RecordDeny()
		m.cfg.Logger.Warn("authz catalog miss, denying",
			"fqn", dr.FQN,
			"authenticated", isAuthed)
		return entry, decision{
			outcome: outcomeDeny,
			reasons: []string{missReason},
			descriptor: permissionDeniedDescriptor{
				FQN: dr.FQN,
			},
		}, true
	}
	return entry, decision{}, false
}

// phaseSubject extracts the authenticated subject. A missing/invalid credential
// terminates with Unauthenticated(16)/401. On success it returns the verified
// token and resolved subject for the downstream phases.
func (m *AuthzMiddleware) phaseSubject(ctx context.Context, dr decisionRequest, entry CatalogEntry) (*VerifiedToken, ResolvedSubject, decision, bool) {
	verified, _ := verifiedTokenFromCtxOrHTTP(ctx, dr.HTTPReq)
	subj, ok := m.cfg.Subjects.Extract(verified)
	if !ok {
		// No authenticated subject (no JWT / invalid JWT) →
		// Unauthenticated(16) / 401, not PermissionDenied(7) / 403.
		// gRPC convention: UNAUTHENTICATED means "the caller is not identified";
		// PERMISSION_DENIED means "identified caller has no access to the resource".
		m.metrics.RecordDeny()
		return verified, subj, decision{
			outcome: outcomeUnauthenticated,
			reasons: []string{"subject: unauthenticated request"},
			descriptor: permissionDeniedDescriptor{
				FQN:    dr.FQN,
				Action: entry.Permission,
			},
			entry: entry,
		}, true
	}
	return verified, subj, decision{}, false
}

// phaseScopeFiltered admits an RPC whose catalog row declares that the OWNING
// SERVICE authorizes the call over the data it answers with — the caller names the
// inputs (a list of instance ids, a page of bindings, an arbitrary
// subject/resource pair) and the answer concerns many objects with different
// owners, so there is no single object for the edge to ask about in advance. The
// service reads its page and asks the model per element, with the same predicate
// its own Get is gated on.
//
// It runs AFTER subject extraction, and that ordering is the whole point: a
// per-object filter downstream is meaningless without a principal, so an
// unauthenticated caller has already been refused by phaseSubject. The step-up
// floor (`required_acr_min`) is likewise unaffected — it is evaluated on the
// credential, elsewhere.
//
// Why this is NOT the `<exempt>` path. `<exempt>` carries a second meaning that
// must not leak in here: phaseInternalOriginExempt admits an `Internal*` exempt
// RPC arriving on the cluster-internal listener WITHOUT extracting a principal at
// all, on the strength of network position — which is not a credential, and which
// anything holding a port-forward can produce. Keeping the lanes separate is what
// lets an Internal* batched read (vpc ListByInstance, storage ListAttachments)
// declare "the edge does not narrow this" without also declaring "and it need not
// know who is calling".
//
// The alternative this replaces was worse than nothing: those rows named `viewer`
// on the `cluster` singleton, a relation the cluster bootstrap deliberately grants
// to `user:*` so every tenant can read the global reference catalog (regions,
// zones, disk types). The Check ran, cost a round-trip, and admitted every
// authenticated subject — a control with the shape of authorization and none of
// the substance, plus a liveness coupling to a tuple that has nothing to do with
// the RPC.
func (m *AuthzMiddleware) phaseScopeFiltered(dr decisionRequest, entry CatalogEntry) (decision, bool) {
	if !entry.ScopeFiltered {
		return decision{}, false
	}
	// СВОЯ полоса: авторизует ВЛАДЕЮЩИЙ СЕРВИС над данными, которыми отвечает.
	// Допуск края здесь — не суждение о правах, и складывать его с выдачей значит
	// утверждать проверку, которой край не делал (#798).
	m.metrics.RecordScopeFiltered()
	return decision{
		outcome:    outcomeAllow,
		descriptor: permissionDeniedDescriptor{FQN: dr.FQN, Action: entry.Permission},
		entry:      entry,
	}, true
}

// phaseResource resolves the FGA resource scope (type + id) and applies the
// malformed-id short-circuit (5b). It returns handled=true only for the
// malformed-id case (InvalidArgument/400); otherwise it returns the resolved
// scope + descriptor for the Check phase.
func (m *AuthzMiddleware) phaseResource(dr decisionRequest, entry CatalogEntry, subj ResolvedSubject) (ResourceID, string, permissionDeniedDescriptor, decision, bool) {
	var resourceID ResourceID
	// scopeConflict — set when the raw HTTP request named its scope in two
	// sources that disagree. Refused below, once the descriptor exists.
	var scopeConflict *ScopeConflict
	switch {
	case dr.HTTPReq != nil:
		resourceID, scopeConflict = m.cfg.Resources.ExtractFromHTTP(dr.HTTPReq, dr.FQN, entry)
	case dr.ProtoReq != nil:
		resourceID, _ = m.cfg.Resources.ExtractFromProto(dr.ProtoReq, entry)
	default:
		// Unmaterialised request: gated before any client message was read, so no
		// request field can be extracted. For a CONCRETE per-resource scope the
		// scope id is therefore unresolvable — defaulting to the wildcard "*" would
		// collapse the FGA Check to `<type>:*` and authorize the caller for EVERY
		// resource of that type. Fail closed: deny (Check does not run) rather than
		// authorize against an over-broad wildcard scope. Wildcard / subject /
		// scope-polymorphic entries have no concrete id to resolve and legitimately
		// keep "*".
		//
		// BE EXACT ABOUT WHO ARRIVES HERE — it is not only server-streaming RPCs.
		// The gateway forwards domain traffic through `grpc.UnknownServiceHandler`,
		// and grpc-go dispatches an unregistered service through its STREAMING
		// path: an ordinary unary RPC proxied to a backend reaches the stream
		// interceptor, marked bidirectional, before its single client message is
		// read (pinned by TestAuthz_ProxiedUnaryRPC_ArrivesOnTheStreamLane). So on
		// the gateway's own gRPC listener this branch is the branch that ORDINARY
		// UNARY RPCs take, and every concrete-scope catalog row is denied there —
		// not a guard held in reserve for a hypothetical future stream.
		//
		// That is a liveness gap on the gRPC edge, and it is fail-closed by
		// construction: nothing is over-granted, the surface simply does not serve
		// concrete-scope RPCs. Closing it means resolving the scope from the first
		// forwarded client message (peek + replay in the proxy), which is a change
		// to the forwarding layer, not to this decision. Until that lands, the deny
		// stands: authorizing against `<type>:*` to make the surface work would
		// trade the whole point of a per-resource scope for reachability. The REST
		// surface is unaffected — grpc-gateway dials the backends directly and
		// never re-enters this server, so it arrives with a materialised request.
		if dr.Stream && isConcreteResourceScope(entry) {
			resourceType := entry.ScopeExtractor.ObjectType
			if resourceType == "" {
				resourceType = "project"
			}
			descriptor := permissionDeniedDescriptor{
				FQN:          dr.FQN,
				Subject:      subj.FGA,
				Action:       entry.Permission,
				ResourceType: resourceType,
				ResourceID:   "*",
			}
			m.metrics.RecordDeny()
			m.cfg.Logger.Warn("authz stream scope unresolved — failing closed",
				"fqn", dr.FQN,
				"subject", subj.FGA,
				"action", entry.Permission,
				"resource_type", resourceType,
			)
			return resourceID, resourceType, descriptor, decision{
				outcome:    outcomeDeny,
				reasons:    []string{"streaming RPC with a concrete-resource scope cannot be authorized: scope id unresolvable on the unmaterialised stream path"},
				descriptor: descriptor,
				entry:      entry,
			}, true
		}
		resourceID = ResourceID("*")
	}
	resourceType := entry.ScopeExtractor.ObjectType
	// Scope-polymorphic RPCs (e.g. AccessBindingService.ListByScope) carry
	// the FGA object type in a request field (catalog
	// `object_type_from_request_field`, value project|account|cluster). When
	// declared + present, it overrides the static `object_type` — otherwise an
	// account/cluster-scoped read would check `project:<id>` → 403.
	// `object_type` remains the fallback when the field is absent/empty.
	if otField := strings.TrimSpace(entry.ScopeExtractor.ObjectTypeFromRequestField); otField != "" {
		var dynType string
		if dr.HTTPReq != nil {
			var typeConflict *ScopeConflict
			dynType, typeConflict = m.cfg.Resources.ScopeTypeFromHTTP(dr.HTTPReq, otField)
			if scopeConflict == nil {
				scopeConflict = typeConflict
			}
		} else if dr.ProtoReq != nil {
			dynType = m.cfg.Resources.ScopeTypeFromProto(dr.ProtoReq, otField)
		}
		if dynType != "" {
			resourceType = dynType
		}
	}
	if resourceType == "" {
		// Project-level scope is the platform default (every
		// permission has a project scope unless overridden — top-level
		// `cluster` / `organization` types use explicit object_type).
		resourceType = "project"
	}

	// redesign-2026 F4 (Role definition_tier MIGRATE): the `definition_tier`
	// anchor is the CANONICAL authz scope and SUPERSEDES the legacy
	// account_id/project_id catalog extraction when present + resolvable (proto
	// precedence semantics). It resolves BOTH the FGA object type (account|project
	// from the dotted tier_type) and the id (tier_id) — a definition_tier-only
	// Create would otherwise leave account_id empty → `account:*` wildcard →
	// `no path: unscoped resource` 403. Absent/unresolvable (iam.cluster / unknown)
	// → keep the legacy scope; the iam handler surfaces the canonical
	// INVALID_ARGUMENT. Code-driven (like the nested ResourceRef `.id` handling),
	// so the byte-identical permission-catalog is untouched.
	//
	// The resolution is BOUND TO THE FQN (definitionTierScopedFQNs): the anchor is
	// read from the raw request payload — on the REST arm straight out of the JSON
	// body, before grpc-gateway drops unknown keys — so an unbound override would
	// let a caller pick their own authz scope for any JSON-bodied RPC while the
	// handler acted on the real one (BOLA, security.md §object-scoped authz).
	//
	// Below it, the LEGACY `project_id` alternative closes the second half of the
	// same scope contract: a custom Role is account- XOR project-scoped and the
	// proto keeps the legacy account_id/project_id pair accepted for back-compat
	// when definition_tier is omitted — but the catalog `scope_extractor` can name
	// only ONE field, so a legacy project-scoped Create left `account:*` and was
	// 403'd as "no path: unscoped resource" BEFORE the backend that honours the
	// promise. It is consulted ONLY when the canonical anchor did not resolve AND
	// the catalog extraction came up wildcard (i.e. account_id is absent) — that
	// ordering IS the documented precedence (definition_tier > account_id >
	// project_id) and keeps a present account_id from being displaced by a project
	// the caller happens to own. Same FQN binding, same reason.
	switch {
	case dr.ProtoReq != nil:
		if ot, id, ok := m.cfg.Resources.ResolveDefinitionTierScope(dr.FQN, dr.ProtoReq); ok {
			resourceType, resourceID = ot, ResourceID(id)
		} else if resourceID.IsWildcard() {
			if id, ok := m.cfg.Resources.ResolveLegacyProjectScope(dr.FQN, dr.ProtoReq); ok {
				resourceType, resourceID = "project", ResourceID(id)
			}
		}
	case dr.HTTPReq != nil:
		if ot, id, ok := m.cfg.Resources.ResolveDefinitionTierScopeHTTP(dr.FQN, dr.HTTPReq); ok {
			resourceType, resourceID = ot, ResourceID(id)
		} else if resourceID.IsWildcard() {
			if id, ok := m.cfg.Resources.ResolveLegacyProjectScopeHTTP(dr.FQN, dr.HTTPReq); ok {
				resourceType, resourceID = "project", ResourceID(id)
			}
		}
	}

	// cluster — это singleton; его написание объявлено константой
	// catalogderive.ClusterSingletonID и берётся ОТТУДА, а не повторяется
	// строкой: переход написания правит объявление, а повторённая рукой строка
	// продолжила бы спрашивать про прежний объект молча.
	// Catalog для reference-data (compute.Region/Zone, etc.) задает
	// scope_extractor: {object_type: cluster, from_request_field: '*'}.
	// Extractor выдает ResourceID("*") → object="cluster:*" → kaname
	// AuthorizeService.Check отбивает с "no path: unscoped resource"
	// (authorize_service.go блокирует req.Resource.ID == "*"). Тут
	// substitute'им wildcard на канонический singleton id, чтобы Check
	// шел на объект якоря, где tuple-cascade
	// `define viewer: [user, user:*, ...]` действительно работает.
	if resourceType == "cluster" && resourceID.IsWildcard() {
		resourceID = ResourceID(catalogderive.ClusterSingletonID)
	}

	descriptor := permissionDeniedDescriptor{
		FQN:          dr.FQN,
		Subject:      subj.FGA,
		Action:       entry.Permission,
		ResourceType: resourceType,
		ResourceID:   resourceID.String(),
	}

	// 5a. Scope-source contradiction short-circuit.
	//
	// The request named its authorization scope twice, in two sources that
	// disagree, and only one of them is the source grpc-gateway will bind the
	// handler's message from. Resolving that by precedence would decide, silently
	// and on the caller's behalf, which of the two objects the platform treats as
	// the subject of the request — and any answer makes the checked object and the
	// acted-on object potentially different. There is no correct winner, so the
	// request is refused: InvalidArgument, naming the field, before the Check runs.
	if scopeConflict != nil {
		m.metrics.RecordDeny()
		m.cfg.Logger.Info("authz scope named in two disagreeing sources — refusing",
			"fqn", dr.FQN,
			"subject", subj.FGA,
			"action", entry.Permission,
			"field", scopeConflict.Field,
		)
		return resourceID, resourceType, descriptor, decision{
			outcome:           outcomeInvalidArgument,
			reasons:           []string{"request names its scope in two sources that disagree"},
			descriptor:        descriptor,
			entry:             entry,
			invalidArgMessage: scopeSourceConflictMessage(scopeConflict.Field),
		}, true
	}

	// 5b. Malformed-id short-circuit.
	//
	// For an entry whose scope is a CONCRETE per-resource id (the
	// `from_request_field` names a real resource-id field, not the wildcard /
	// subject-as-scope / scope-polymorphic forms), a syntactically-invalid id
	// (unknown 3-char prefix / wrong length) must surface as InvalidArgument(3)
	// /400 — the Kachō convention — instead of reaching the FGA Check, where a
	// no-path deny would mask it as PermissionDenied(7)/403. We deliberately do
	// NOT validate the scope-polymorphic path (`object_type_from_request_field`),
	// where `resource_id` is a foreign-family scope id, nor the wildcard /
	// subject forms. Well-formed-but-nonexistent ids still pass through to the
	// FGA Check → 403 (existence-leak protection, by design). The result is NOT
	// cached: it is a property of the request input, not of subject↔resource
	// authz state.
	if isConcreteResourceScope(entry) && !resourceID.IsWildcard() && resourceID.String() != "" {
		if err := corevalidate.ResourceID("resource", "", resourceID.String()); err != nil {
			m.metrics.RecordDeny()
			m.cfg.Logger.Info("authz invalid resource id",
				"fqn", dr.FQN,
				"subject", subj.FGA,
				"action", entry.Permission,
				"resource", descriptor.ResourceType+":"+descriptor.ResourceID,
			)
			return resourceID, resourceType, descriptor, decision{
				outcome:           outcomeInvalidArgument,
				reasons:           []string{"resource id is malformed"},
				descriptor:        descriptor,
				entry:             entry,
				invalidArgMessage: invalidResourceIDMessage(resourceID.String()),
			}, true
		}
	}
	return resourceID, resourceType, descriptor, decision{}, false
}

// phaseCheck builds the Conditions context, consults the decision cache
// (write-after-invalidate epoch-guarded) and, on a miss, runs the IAM FGA Check.
// It is the terminal phase — it always returns a decision.
func (m *AuthzMiddleware) phaseCheck(
	ctx context.Context,
	dr decisionRequest,
	entry CatalogEntry,
	verified *VerifiedToken,
	subj ResolvedSubject,
	resourceID ResourceID,
	resourceType string,
	descriptor permissionDeniedDescriptor,
) decision {
	// 6. Context build.
	var contextMap map[string]any
	if dr.HTTPReq != nil {
		contextMap = m.cfg.Context.BuildHTTP(verified, dr.HTTPReq, subj)
	} else if dr.GRPCMeta != nil || dr.GRPCPeer != "" {
		contextMap = m.cfg.Context.BuildPeerAddr(verified, peerAddrToAddr(dr.GRPCPeer), grpcMetaForwardedFor(dr.GRPCMeta), subj)
	} else {
		contextMap = m.cfg.Context.BuildHTTP(verified, nil, subj)
	}

	// 7. Decision cache lookup.
	traceID := traceFromContext(ctx, dr.HTTPReq, dr.GRPCMeta)
	cacheKey := buildCacheKey(subj.FGA, entry.Permission, resourceType, resourceID.String(), contextMap)
	// Snapshot the invalidation generation BEFORE reading the cache. Any
	// Invalidate that races the upcoming Check will move the
	// generation, and the put below is dropped so a grant revoked mid-Check is
	// never re-cached (write-after-invalidate guard; CWE-362 + CWE-613).
	cacheGen := m.cache.generation()
	// Кеш хранит ТОЛЬКО положительный вердикт (см. decisionCacheEntry): попадание
	// означает «разрешено», промах — «спросить авторитетный источник». Отказ сюда
	// не кладётся, поэтому свежая выдача видна сразу, а ждёт один лишь отзыв —
	// та же асимметрия, что у общей библиотеки.
	if m.cache.allowed(cacheKey) {
		m.metrics.RecordCacheHit()
		m.metrics.RecordAllow()
		return decision{outcome: outcomeAllow, descriptor: descriptor, entry: entry}
	}
	m.metrics.RecordCacheMiss()

	// 8. IAM Check.
	result, err := m.cfg.Checker.Check(ctx, AuthzCheckInput{
		Subject: subj.FGA,
		Action:  entry.Permission,
		// Pass catalog's required_relation through to
		// IAM so admin-only RPCs (system_admin) gate correctly even when
		// the verb (`list`/`get`) would otherwise derive to `viewer`.
		RequiredRelation: entry.RequiredRelation,
		ResourceType:     resourceType,
		ResourceID:       resourceID.String(),
		Context:          contextMap,
		TraceID:          traceID,
	})
	if err != nil {
		// PermissionDenied surfaced as an error from the gRPC stub is a
		// real deny (the AuthorizeService returns `allowed=false` with
		// reasons, but defensive code handles both shapes).
		if code := status.Code(err); code == codes.PermissionDenied {
			st, _ := status.FromError(err)
			m.metrics.RecordDeny()
			reasons := []string{st.Message()}
			// Отказ НЕ кешируется: он и так fail-closed, а запись сделала бы из
			// окна отзыва ещё и задержку выдачи (см. decisionCacheEntry).
			return denyDecision(dr.FQN, entry, descriptor, reasons)
		}
		// Полоса исхода здесь НЕ пишется: отсюда не видно, чем несостоявшаяся
		// проверка кончится для запроса — отклонением или объявленным мягким
		// проходом. Пишет её то звено, которое этот выбор делает
		// (AuthzMetrics.RecordErrorRefused / RecordErrorPassed), иначе два
		// противоположных исхода снова слились бы в одно число.
		m.cfg.Logger.Error("authz check failed",
			"fqn", dr.FQN,
			"subject", subj.FGA,
			"action", entry.Permission,
			"resource", descriptor.ResourceType+":"+descriptor.ResourceID,
			"err", err,
		)
		return decision{outcome: outcomeError, checkErr: err, descriptor: descriptor, entry: entry}
	}

	if result.Allowed {
		m.cache.putIfGen(cacheKey, cacheGen)
		m.metrics.RecordAllow()
		m.cfg.Logger.Info("authz allow",
			"fqn", dr.FQN,
			"subject", subj.FGA,
			"action", entry.Permission,
			"resource", descriptor.ResourceType+":"+descriptor.ResourceID,
			"risk", entry.RiskLevel,
		)
		return decision{outcome: outcomeAllow, descriptor: descriptor, entry: entry}
	}

	reasons := result.DenyReasons
	if len(reasons) == 0 {
		reasons = []string{"no path"}
	}
	// Отказ НЕ кешируется (см. decisionCacheEntry): иначе первый же запрос,
	// проигравший гонку материализации права, сам держал бы свой отказ весь срок
	// жизни записи — уже после того, как право появилось.
	m.metrics.RecordDeny()
	m.cfg.Logger.Info("authz deny",
		"fqn", dr.FQN,
		"subject", subj.FGA,
		"action", entry.Permission,
		"resource", descriptor.ResourceType+":"+descriptor.ResourceID,
		"reasons", reasons,
		"risk", entry.RiskLevel,
		"hide_existence", entry.HidesExistenceOnDeny(dr.FQN),
	)
	return denyDecision(dr.FQN, entry, descriptor, reasons)
}

// denyDecision builds the terminal decision for an authz deny on a known catalog
// entry. For a hide-existence read RPC (HidesExistenceOnDeny) it returns
// outcomeNotFound — NotFound(5)/404 with NO deny reasons — so the gateway does
// not override the resource owner's hide-existence contract or leak the reasons.
// Otherwise it returns the regular outcomeDeny — PermissionDenied(7)/403 with
// reasons. Enforcement is identical in both cases: the request is blocked.
func denyDecision(fqn string, entry CatalogEntry, descriptor permissionDeniedDescriptor, reasons []string) decision {
	if entry.HidesExistenceOnDeny(fqn) {
		return decision{
			outcome:    outcomeNotFound,
			descriptor: descriptor,
			entry:      entry,
		}
	}
	return decision{
		outcome:    outcomeDeny,
		reasons:    reasons,
		descriptor: descriptor,
		entry:      entry,
	}
}
