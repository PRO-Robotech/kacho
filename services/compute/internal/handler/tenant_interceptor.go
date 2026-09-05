// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — tenant_interceptor.go: gRPC unary/stream interceptor
// который извлекает caller identity из metadata и кладёт в context.
//
// Это НЕ authz-плоскость. Авторизация ресурсов живёт в permission-модели:
// per-RPC InternalIAMService.Check (FGA) в authz-интерсепторе на ОБОИХ
// листенерах (см. internal/check) + per-object listauthz-фильтр на List
// (list_filter.go). Здесь остаётся только то, что моделью не выражается:
// production-mode AuthN-гейт (anonymous fail-closed) и admin-гейт internal
// :9091 листенера. Зеркалит kacho-vpc/internal/handler/tenant_interceptor.go.
package handler

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// principalForwarded сообщает, несёт ли ctx реальный forwarded end-user principal.
// api-gateway форвардит identity как `x-kacho-principal-*` (без legacy
// `x-kacho-project-id`); UnaryTrustedPrincipalExtract кладёт его в стандартный
// operations-carrier ТОЛЬКО для доверенного forwarder'а (untrusted → scrub до
// SystemPrincipal). Поэтому запрос с реальным principal тут = trusted principal и
// НЕ anonymous, даже если tenant-headers пусты. Project-scoping энфорсится ниже —
// per-object authz-интерсептором (FGA Check) + listFilter, а не этим guard'ом.
// Зеркалит kaname `authzguard.IsAnonymous` и kacho-vpc `principalForwarded`.
func principalForwarded(ctx context.Context) bool {
	p := operations.PrincipalFromContext(ctx)
	if p.ID == "" || p.Type == "" {
		return false
	}
	if p.Type == "anonymous" || p.ID == "anonymous" {
		return false
	}
	if p.Type == "system" && p.ID == "bootstrap" {
		return false
	}
	return true
}

type tenantCtxKey struct{}

// TenantCtx — caller identity. Сейчас populated из gRPC metadata
// (`x-kacho-project-id`, `x-kacho-actor`); future — из validated IAM token.
type TenantCtx struct {
	// ProjectIDs — project-scope, объявленный доверенным forwarder'ом. Доступ к
	// ресурсам им НЕ гейтится (это делает per-RPC FGA Check + listauthz); поле
	// участвует только в IsAnonymous → production-mode AuthN-гейт и admin-гейт.
	ProjectIDs map[string]struct{}
	// Actor — для audit log (admin@kacho, или sub-claim из JWT).
	Actor string
	// Admin — true если caller имеет cluster-wide read/write.
	Admin bool
}

// IsAnonymous — true если caller не предъявил identity, влияющую на AuthZ
// (ни Admin-claim, ни ProjectIDs). Actor — orthogonal audit-trail.
func (t TenantCtx) IsAnonymous() bool {
	return !t.Admin && len(t.ProjectIDs) == 0
}

// TenantFromCtx извлекает TenantCtx из context. Если interceptor не сработал —
// empty TenantCtx (Admin=false, ProjectIDs=nil → anonymous для AuthN-гейта).
// Identity кладётся в ctx интерсептором для downstream-слоёв; ownership-гейты
// (OperationService) её НЕ читают — они резолвят владельца из доверенного
// ctx-principal'а. Паритет с kacho-vpc `internal/tenant.TenantFromCtx`.
func TenantFromCtx(ctx context.Context) TenantCtx {
	if v := ctx.Value(tenantCtxKey{}); v != nil {
		if t, ok := v.(TenantCtx); ok {
			return t
		}
	}
	return TenantCtx{}
}

// TenantUnaryInterceptor — gRPC unary interceptor. Извлекает caller-project
// identity из metadata и кладёт в ctx как TenantCtx.
//
// requireAdmin=true (internal :9091) — отвергает caller'а без admin-flag.
// productionMode=true — fail-closed гейт: anonymous caller → PermissionDenied сразу.
func TenantUnaryInterceptor(requireAdmin, productionMode bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		_, trusted := grpcsrv.TrustedPrincipalFromContext(ctx)
		t := tenantFromMetadata(ctx, trusted, requireAdmin)
		if productionMode && t.IsAnonymous() && !principalForwarded(ctx) {
			return nil, status.Error(codes.PermissionDenied,
				"AuthN required (production mode): set x-kacho-* identity headers via gateway")
		}
		if requireAdmin {
			if err := assertAdminAccess(t, info.FullMethod); err != nil {
				return nil, err
			}
		}
		ctx = context.WithValue(ctx, tenantCtxKey{}, t)
		return handler(ctx, req)
	}
}

// TenantStreamInterceptor — то же для server-stream RPC (для Watch).
func TenantStreamInterceptor(requireAdmin, productionMode bool) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		_, trusted := grpcsrv.TrustedPrincipalFromContext(ss.Context())
		t := tenantFromMetadata(ss.Context(), trusted, requireAdmin)
		if productionMode && t.IsAnonymous() && !principalForwarded(ss.Context()) {
			return status.Error(codes.PermissionDenied,
				"AuthN required (production mode): set x-kacho-* identity headers via gateway")
		}
		if requireAdmin {
			if err := assertAdminAccess(t, info.FullMethod); err != nil {
				return err
			}
		}
		ctx := context.WithValue(ss.Context(), tenantCtxKey{}, t)
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

// assertAdminAccess — internal :9091 listener gate. Отвергает не-admin caller'а.
// Anonymous (нет AuthN) → пропускается в dev-mode (backward-compat).
func assertAdminAccess(t TenantCtx, fullMethod string) error {
	if t.IsAnonymous() {
		return nil
	}
	if !t.Admin {
		if !strings.HasPrefix(fullMethod, "/kacho.cloud.compute.v1.Internal") {
			return status.Error(codes.NotFound, "not found")
		}
		return status.Error(codes.PermissionDenied, "Permission denied")
	}
	return nil
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

// tenantFromMetadata — internal helper, извлекает TenantCtx из gRPC md.
//
// Два независимых условия, оба обязательны для authz-влияющих заголовков:
//
//   - trusted — решение trust-aware principal-extract'а
//     (grpcsrv.TrustedPrincipalFromContext): метадата доверяется ⟺ peer предъявил
//     verified client-cert trusted forwarder'а (api-gateway); insecure-листенер =
//     back-compat trusted. Отвечает на вопрос «КТО со мной говорит по проводу» —
//     защищает от peer'а, дотянувшегося до листенера напрямую.
//   - internalListener — листенер, на котором эти заголовки вообще осмысленны
//     (= requireAdmin, т.е. только :9091). Отвечает на другой вопрос — «ОТКУДА
//     взялось значение».
//
// Trust-гейта одного НЕДОСТАТОЧНО, и это ядро дефекта: api-gateway — доверенный
// peer, но заголовок в него приносит tenant. Пока gateway пробрасывал
// `Grpc-Metadata-X-Kacho-Admin` (мост REST→gRPC форвардит любой `Grpc-Metadata-*`),
// подлог приезжал на ПУБЛИЧНЫЙ листенер как метадата доверенного форвардера,
// поднимал t.Admin и снимал ownership-предикат OperationService (ответ операции
// несёт созданный ресурс целиком). Доверяя такому значению на tenant-facing
// поверхности, сервис доверяет произвольному клиенту.
//
// Отсюда — паритет с kacho-vpc (`honorAdmin = requireAdmin`): публичный листенер
// admin-полномочий не выдаёт вообще, нет заголовка, которым их можно попросить.
// Trust-гейт compute при этом сохранён (он строже vpc и снимает второй,
// независимый вектор — прямой dial в обход gateway).
//
// x-kacho-project-id гейтится тем же листенер-условием (compute строже vpc):
// api-gateway его вообще не производит (buildPrincipalMetadata форвардит только
// x-kacho-principal-* + x-kacho-token-acr), поэтому на публичной поверхности он
// может быть только подлогом. Он не выдаёт доступ к объектам (это per-RPC FGA
// Check + listauthz), но питает IsAnonymous — то есть подложенный project-id
// удовлетворял бы production-mode AuthN-гейт без единого credential'а. На
// публичном листенере не-anonymous теперь следует ИСКЛЮЧИТЕЛЬНО из trust-gated
// principal'а (principalForwarded) — ровно то, что форвардит gateway.
//
// Заголовочная гигиена шлюза остаётся первым слоем, но НЕ основанием доверять:
// gateway — не единственный способ дотянуться до листенера.
//
// x-kacho-actor — audit-only, не влияет на authz → читается всегда.
func tenantFromMetadata(ctx context.Context, trusted, internalListener bool) TenantCtx {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return TenantCtx{}
	}
	t := TenantCtx{}
	if v := md.Get("x-kacho-actor"); len(v) > 0 {
		t.Actor = v[0]
	}
	if !trusted {
		// Untrusted peer: игнорируем forgeable authz-заголовки. TenantCtx остаётся
		// anonymous для authz (Admin=false, ProjectIDs=nil) — production-mode gate
		// отобьёт его как anonymous, а authoritative per-RPC FGA Check (на trust-gated
		// principal) остаётся основным гейтом.
		return t
	}
	if !internalListener {
		// Публичный листенер (:9090): authz-влияющие заголовки не читаются вообще —
		// gateway их не производит, значит любое значение здесь — клиентский подлог.
		return t
	}
	if v := md.Get("x-kacho-admin"); len(v) > 0 && v[0] == "true" {
		t.Admin = true
	}
	if projects := md.Get("x-kacho-project-id"); len(projects) > 0 {
		t.ProjectIDs = make(map[string]struct{}, len(projects))
		for _, p := range projects {
			if p != "" {
				t.ProjectIDs[p] = struct{}{}
			}
		}
	}
	return t
}
