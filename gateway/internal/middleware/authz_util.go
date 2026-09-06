// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authz_util.go — transport/FQN/token helper functions extracted from the
// authz.go god-file. Pure movement, no behaviour change: these are the free
// functions the decision path calls (FQN normalisation, peer-addr + forwarded
// -for extraction, verified-token reconstruction from headers/metadata, trace
// correlation, public-path predicate, REST→FQN resolution, scope classification).
package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// isConcreteResourceScope reports whether the catalog entry's scope is a
// CONCRETE per-resource id — i.e. `from_request_field` names a real resource-id
// field (e.g. `network_id`, `access_binding_id`) rather than one of the
// non-id forms the resource-extractor recognises:
//
//   - ""        — no scope field (gateway-default scope);
//   - "*"       — wildcard (List/Search catch-all);
//   - "subject" — the subject is its own scope (AuthorizeService.Check);
//   - "resource"— a ResourceRef{type,id} wrapper (scope id is foreign-typed).
//
// It also excludes the scope-polymorphic path (`object_type_from_request_field`
// set): there the extracted `resource_id` is a scope id of an arbitrary family
// (project / account / cluster) carried for a ListByScope-style RPC, so the
// per-resource-id syntax check does not apply.
func isConcreteResourceScope(entry CatalogEntry) bool {
	if strings.TrimSpace(entry.ScopeExtractor.ObjectTypeFromRequestField) != "" {
		return false
	}
	switch strings.TrimSpace(entry.ScopeExtractor.FromRequestField) {
	case "", "*", "subject", "resource":
		return false
	default:
		return true
	}
}

// normalizeFQN strips the leading `/` from gRPC FullMethod and turns the
// `pkg.Service/Method` portion into the canonical FQN shape used by the
// catalog ("kaname.cloud.iam.v1.AuthorizeService/Check").
func normalizeFQN(full string) string {
	return strings.TrimPrefix(full, "/")
}

// peerAddr returns the client peer.Addr.String() from a gRPC context, or
// "" when no peer is attached.
func peerAddr(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}

// peerAddrToAddr — wraps a raw "ip:port" string in net.Addr (we use a thin
// shim because peer.Peer keeps the original net.Addr; the wrapper avoids
// re-parsing for the metric path).
func peerAddrToAddr(s string) addrShim {
	return addrShim(s)
}

type addrShim string

func (a addrShim) Network() string { return "tcp" }
func (a addrShim) String() string  { return string(a) }

// incomingMD returns the gRPC incoming metadata or nil.
func incomingMD(ctx context.Context) metadata.MD {
	md, _ := metadata.FromIncomingContext(ctx)
	return md
}

// grpcMetaForwardedFor extracts the X-Forwarded-For from grpc-gateway-
// rewritten metadata. Empty when absent.
func grpcMetaForwardedFor(md metadata.MD) string {
	if md == nil {
		return ""
	}
	// grpc-gateway rewrites incoming HTTP headers to `grpcgateway-<lower>`.
	if v := md.Get("grpcgateway-x-forwarded-for"); len(v) > 0 {
		return v[0]
	}
	if v := md.Get("x-forwarded-for"); len(v) > 0 {
		return v[0]
	}
	if v := md.Get("grpcgateway-x-real-ip"); len(v) > 0 {
		return v[0]
	}
	return ""
}

// verifiedTokenFromCtxOrHTTP — the authN layer stores the credential's own
// context in the request headers (X-Kacho-Token-Acr / Jti / Scope / Exp / Amr /
// Mfa-At) and in the gRPC metadata after it ran. We reconstruct a VerifiedToken
// from them when the live one is no longer in hand. When the HTTP request is nil
// we fall back to gRPC metadata.
//
// ЭТО ВХОД РЕШЕНИЯ О ПРАВАХ, А НЕ СЛЕПОК ДЛЯ ЖУРНАЛА. Восстановленное
// удостоверение уходит в `ContextExtractor`, который собирает из него доводы
// условий модели прав, — поэтому величина, которой здесь нет, делает условие,
// её спрашивающее, НЕИСПОЛНИМЫМ при любом входе.
//
// Так и было до #1252: перечня способов и момента подтверждения среди
// проброшенных величин не существовало, обе наполнялись только из утверждений
// настоящего токена, и условие `mfa_fresh` не могло быть выполнено ни на
// браузерной полосе, ни на полосе предъявителя, дошедшей сюда. Прежняя редакция
// этого комментария называла такой вид «ограниченным» и признавала пустые
// способы и отсутствующий момент штатным исходом — то есть описывала неисполнимую
// возможность как решение.
//
// Остальное восстановление по-прежнему частичное и названо им: прочие ext_claims
// (соответствие устройства, опознаватель ключа доступа) сюда не едут, и условия,
// которые их спрашивают, на восстановленном удостоверении не выполняются. Это
// НЕ умолчание: у каждого такого довода свой производитель, и заводится он
// вместе со своим потребителем, а не «про запас».
func verifiedTokenFromCtxOrHTTP(ctx context.Context, r *http.Request) (*VerifiedToken, bool) {
	var (
		acr    string
		jti    string
		scope  string
		sub    string
		pType  string
		amrRaw string
		mfaRaw string
	)
	if r != nil {
		acr = r.Header.Get(principalmeta.HeaderTokenACR)
		jti = r.Header.Get(principalmeta.HeaderTokenJti)
		scope = r.Header.Get(principalmeta.HeaderTokenScope)
		pType = r.Header.Get(principalmeta.HeaderPrincipalType)
		sub = r.Header.Get(principalmeta.HeaderPrincipalID)
		amrRaw = r.Header.Get(principalmeta.HeaderTokenAMR)
		mfaRaw = r.Header.Get(principalmeta.HeaderTokenMfaAt)
	}
	if sub == "" || acr == "" {
		md := incomingMD(ctx)
		if md != nil {
			if v := md.Get(principalmeta.MetaTokenACR); len(v) > 0 {
				acr = v[0]
			}
			if v := md.Get(principalmeta.MetaTokenJti); len(v) > 0 {
				jti = v[0]
			}
			if v := md.Get(principalmeta.MetaTokenScope); len(v) > 0 {
				scope = v[0]
			}
			if v := md.Get(principalmeta.MetaPrincipalID); len(v) > 0 {
				sub = v[0]
			}
			if v := md.Get(principalmeta.MetaPrincipalType); len(v) > 0 {
				pType = v[0]
			}
			if v := md.Get(principalmeta.MetaTokenAMR); len(v) > 0 {
				amrRaw = v[0]
			}
			if v := md.Get(principalmeta.MetaTokenMfaAt); len(v) > 0 {
				mfaRaw = v[0]
			}
		}
	}
	if sub == "" {
		return nil, false
	}
	extClaims := map[string]any{
		"kacho_principal_type": defaultIfEmptyStr(pType, "user"),
		"kacho_principal_id":   sub,
	}
	// Момент подтверждения кладётся под тем же именем, под которым его читает
	// сборка доводов (`kacho_mfa_at`), — а не под вторым, «своим». Второе имя
	// одной величины разошлось бы с первым молча.
	//
	// Непрочитанное и неположительное значение НЕ кладутся: «довода нет» и
	// «подтверждено в 1970 году» — разные утверждения, и второе поддаётся
	// арифметике, которой нечего опровергнуть.
	if at, ok := parseUnixSecondsHeader(mfaRaw); ok {
		extClaims["kacho_mfa_at"] = at
	}
	return &VerifiedToken{
		Subject:   sub,
		JTI:       jti,
		ACR:       acr,
		Scope:     scope,
		AMR:       principalmeta.DecodeAuthMethods(amrRaw),
		ExtClaims: extClaims,
	}, true
}

// parseUnixSecondsHeader читает момент из значения заголовка.
//
// Отдельная функция, а не строка на месте: «нечитаемое», «нулевое» и
// «отрицательное» обязаны давать ОДИН исход — довода нет, — и держать это
// правило в одном месте дешевле, чем сверять две его копии.
func parseUnixSecondsHeader(v string) (int64, bool) {
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// defaultIfEmptyStr — tiny helper.
func defaultIfEmptyStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// resolveRestFQN best-effort maps an incoming HTTP request to a gRPC FQN
// the catalog can look up. Uses the explicit RestRouteResolver first, then
// falls back to the path-prefix heuristic from dpop_http_middleware.
func (m *AuthzMiddleware) resolveRestFQN(r *http.Request) string {
	if m.cfg.RestRouter != nil {
		if fqn, ok := m.cfg.RestRouter.Resolve(r.Method, r.URL.Path); ok {
			return fqn
		}
	}
	return grpcMethodForPath(r.URL.Path)
}

// isPublicHTTPPath returns true for fixed public endpoints (healthz, readyz,
// oauth flows). It is the single source of truth for the pre-auth HTTP
// allow-list: this middleware, DPoPMiddleware.Wrap and the revocation check on
// AuthInterceptor all consult it, so the layers cannot drift (a path admitted by
// one but challenged by another).
//
// Note what membership now means for the third consumer: a listed path is also
// exempt from the "is this token revoked?" question (auth_revocation.go). That is
// required for sign-out — a user whose session was revoked elsewhere must still be
// able to complete it.
//
// The last branch used to be a PREFIX over /iam/v1/auth/, and its own note called
// that out as worth watching: any route added there would INHERIT the exemption
// without anyone deciding so. It covered the interactive-login ceremony routes.
// Those were retired together with the identity provider they addressed, which
// left the prefix guarding exactly one route — so the hazard was all that was
// left of it. The branch is now an EXACT match on that one route: a new route
// under the same path segment gets no exemption unless someone adds it here on
// purpose.
//
// /iam/v1/auth/me is listed because it must answer an anonymous caller (with
// `{"user":null}`) and because it acts on a provider SESSION, never on a
// bearer's authority — so there is no revocation question to ask of it.
func isPublicHTTPPath(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/oauth/logout", "/iam/v1/auth/me":
		return true
	}
	return false
}

// traceFromContext extracts the request-id for correlation, prioritising
// metadata over the gRPC context-key.
func traceFromContext(ctx context.Context, r *http.Request, md metadata.MD) string {
	if r != nil {
		if v := r.Header.Get("X-Request-Id"); v != "" {
			return v
		}
	}
	if md != nil {
		if v := md.Get("x-request-id"); len(v) > 0 {
			return v[0]
		}
		if v := md.Get("grpcgateway-x-request-id"); len(v) > 0 {
			return v[0]
		}
	}
	return RequestIDFromContext(ctx)
}
