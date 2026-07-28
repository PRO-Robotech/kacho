// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// forgedAdminCtx — ctx как он выглядит для compute, когда клиент подложил
// `Grpc-Metadata-X-Kacho-Admin: true` на публичном REST-эндпоинте: api-gateway
// (доверенный форвардер, verified client-cert) пробросил заголовок дальше, так что
// с точки зрения compute метадата приходит от TRUSTED peer'а. Trust-гейт на этом
// векторе бесполезен by construction — доверенный форвардер ретранслирует чужой
// подлог.
func forgedAdminCtx() context.Context {
	md := metadata.New(map[string]string{
		"x-kacho-admin":      "true",
		"x-kacho-project-id": "prj-victim",
		"x-kacho-actor":      "attacker",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	return grpcsrv.WithTrustedACR(ctx, "", true) // trusted=true (как gateway-mTLS-ребро)
}

// TestTenantFromMetadata_AdminOnlyOnInternalListener — `x-kacho-admin` признаётся
// ТОЛЬКО на internal-листенере (:9091), даже от доверенного форвардера.
//
// Зеркалит kacho-vpc (`honorAdmin = requireAdmin`). Разница принципиальная:
// trust-гейт отвечает на вопрос «кто со мной говорит по проводу», а не «откуда
// это значение взялось». api-gateway — доверенный peer, но заголовок в него
// приносит tenant; доверяя ему на публичной поверхности, сервис доверяет
// произвольному клиенту. Публичный листенер admin-полномочий не выдаёт вообще —
// нет заголовка, которым их можно попросить.
func TestTenantFromMetadata_AdminOnlyOnInternalListener(t *testing.T) {
	ctx := forgedAdminCtx()

	pub := tenantFromMetadata(ctx, true /*trusted*/, false /*public :9090*/)
	assert.False(t, pub.Admin,
		"public listener must not grant Admin from a client-supplied x-kacho-admin, even via a trusted forwarder")
	assert.Empty(t, pub.ProjectIDs,
		"public listener must not substitute project-scope from a client-supplied x-kacho-project-id")
	assert.True(t, pub.IsAnonymous(),
		"forged scope headers must not satisfy the production-mode AuthN gate on the public listener")
	assert.Equal(t, "attacker", pub.Actor,
		"x-kacho-actor stays audit-only and readable — the gate is scoped to authz-bearing keys")

	internal := tenantFromMetadata(ctx, true /*trusted*/, true /*internal :9091*/)
	assert.True(t, internal.Admin,
		"internal listener still honours x-kacho-admin from a trusted peer (admin tooling path)")
	assert.Contains(t, internal.ProjectIDs, "prj-victim",
		"internal listener still honours x-kacho-project-id from a trusted peer")

	// Регрессия: trust-гейт не ослаблен — untrusted peer не получает ничего,
	// ни на публичном, ни на internal листенере.
	assert.False(t, tenantFromMetadata(ctx, false, true).Admin,
		"untrusted peer must not gain Admin on the internal listener")
	assert.Empty(t, tenantFromMetadata(ctx, false, true).ProjectIDs,
		"untrusted peer must not gain project-scope")
}

// TestTenantUnaryInterceptor_PublicListenerNeverAdmin — тот же инвариант на
// уровне наблюдаемого: реальный interceptor публичного листенера
// (requireAdmin=false), handler читает TenantFromCtx.
func TestTenantUnaryInterceptor_PublicListenerNeverAdmin(t *testing.T) {
	var seen TenantCtx
	_, err := TenantUnaryInterceptor(false /*requireAdmin → public*/, false)(
		forgedAdminCtx(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.operation.OperationService/Get"},
		func(c context.Context, _ any) (any, error) {
			seen = TenantFromCtx(c)
			return nil, nil
		})
	require.NoError(t, err)
	assert.False(t, seen.Admin,
		"forged x-kacho-admin must not raise TenantCtx.Admin on the public listener")
	assert.Equal(t, "attacker", seen.Actor,
		"x-kacho-actor is audit-only and stays readable")
}

// TestTenantUnaryInterceptor_InternalListenerKeepsAdmin — регрессия: легитимный
// admin-путь на internal-листенере не сломан (иначе admin-gate :9091 и
// cluster-scoped Internal*-RPC перестали бы работать).
func TestTenantUnaryInterceptor_InternalListenerKeepsAdmin(t *testing.T) {
	var seen TenantCtx
	_, err := TenantUnaryInterceptor(true /*requireAdmin → internal*/, false)(
		forgedAdminCtx(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InternalMachineTypeService/Create"},
		func(c context.Context, _ any) (any, error) {
			seen = TenantFromCtx(c)
			return nil, nil
		})
	require.NoError(t, err)
	assert.True(t, seen.Admin, "internal listener must keep honouring a trusted x-kacho-admin")
	assert.Contains(t, seen.ProjectIDs, "prj-victim", "internal listener keeps project-scope")
}

// TestTenantStreamInterceptor_PublicListenerNeverAdmin — паритет stream-ветки:
// admin-гейт не должен обходиться через server-stream RPC.
func TestTenantStreamInterceptor_PublicListenerNeverAdmin(t *testing.T) {
	var seen TenantCtx
	err := TenantStreamInterceptor(false /*public*/, false)(
		nil, &fakeServerStream{ctx: forgedAdminCtx()},
		&grpc.StreamServerInfo{FullMethod: "/kacho.cloud.compute.v1.InternalWatchService/Watch"},
		func(_ any, ss grpc.ServerStream) error {
			seen = TenantFromCtx(ss.Context())
			return nil
		})
	require.NoError(t, err)
	assert.False(t, seen.Admin, "forged x-kacho-admin must not raise Admin on the public stream path")
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }
