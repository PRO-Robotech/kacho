// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package tenant — нейтральный (transport-free) носитель caller-identity. Это
// leaf-пакет: identity в ctx кладет `internal/handler` через WithTenant из своих
// gRPC-интерсепторов, а читают её transport-independent потребители.
//
// AuthZ здесь НЕ живёт: авторизация выражена в permission-модели и энфорсится
// per-RPC authz-интерсептором (`internal/apps/kacho/check.PermissionMap` →
// `InternalIAMService.Check`) на обоих листенерах, fail-closed. Единственный
// оставшийся здесь потребитель identity — `IsAnonymous`, на который опираются
// production-mode AuthN-guard и admin-gate в `internal/handler`.
package tenant

import (
	"context"
)

type tenantCtxKey struct{}

// TenantCtx — caller identity. Заполняется из gRPC metadata интерсептором
// (`internal/handler`); в перспективе — из validated IAM token.
type TenantCtx struct {
	// ProjectIDs — projects, заявленные caller'ом в metadata. НЕ авторизация:
	// доступ решает per-RPC FGA-Check. Здесь поле нужно как признак предъявленных
	// claims — пустое множество (и не Admin) = anonymous, см. IsAnonymous.
	ProjectIDs map[string]struct{}
	// Admin — true, если caller предъявил cluster-admin claim (honored только на
	// internal-листенере). Питает admin-gate, не объектную авторизацию.
	Admin bool
}

// IsAnonymous — true, если caller не предъявил authorization-claims (ни Admin,
// ни ProjectIDs).
func (t TenantCtx) IsAnonymous() bool {
	return !t.Admin && len(t.ProjectIDs) == 0
}

// WithTenant кладет TenantCtx в context (вызывается gRPC-интерсептором).
func WithTenant(ctx context.Context, t TenantCtx) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, t)
}

// TenantFromCtx извлекает TenantCtx; если интерсептор не сработал — возвращает
// empty (ProjectIDs=nil → IsAnonymous()==true; в production-mode такой caller
// отвергается AuthN-guard'ом интерсептора).
func TenantFromCtx(ctx context.Context) TenantCtx {
	if v := ctx.Value(tenantCtxKey{}); v != nil {
		if t, ok := v.(TenantCtx); ok {
			return t
		}
	}
	return TenantCtx{}
}
