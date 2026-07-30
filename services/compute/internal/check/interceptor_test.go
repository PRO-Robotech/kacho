// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
)

func principalCtx(typ, id string) context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{
		Type:        typ,
		ID:          id,
		DisplayName: "test",
	})
}

// newTestInterceptor — фабрика interceptor'а с подменным CheckClient'ом.
func newTestInterceptor(t *testing.T, fn func(ctx context.Context, subject, relation, object string) (bool, error)) (*authz.Interceptor, *int) {
	t.Helper()
	calls := 0
	wrapped := authz.CheckClientFunc(func(ctx context.Context, subject, relation, object string) (bool, error) {
		calls++
		return fn(ctx, subject, relation, object)
	})
	intr := authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: "kacho-compute-test",
		Map:         check.PermissionMap(),
		Client:      wrapped,
	})
	return intr, &calls
}

func TestInterceptor_Unary_Allow_InstanceCreate(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_alice", subject)
		require.Equal(t, "editor", relation)
		require.Equal(t, "project:prj_demo", object)
		return true, nil
	})
	uIntr := intr.Unary()

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Create"}
	ctx := principalCtx("user", "usr_alice")
	req := &computev1.CreateInstanceRequest{ProjectId: "prj_demo", Name: "vm1"}

	resp, err := uIntr(ctx, req, info, handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
	require.Equal(t, 1, *calls)
}

func TestInterceptor_Unary_Deny_InstanceStop(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_bob", subject)
		require.Equal(t, "v_update", relation)
		require.Equal(t, "compute_instance:epd_xxx", object)
		return false, nil
	})
	uIntr := intr.Unary()

	handlerCalled := false
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "should not be returned", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Stop"}
	ctx := principalCtx("user", "usr_bob")
	req := &computev1.StopInstanceRequest{InstanceId: "epd_xxx"}

	_, err := uIntr(ctx, req, info, handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.False(t, handlerCalled)
	require.Equal(t, 1, *calls)
}

func TestInterceptor_Unary_Unavailable_FailClosed(t *testing.T) {
	intr, _ := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
		return false, errors.New("iam unavailable: connection refused")
	})
	uIntr := intr.Unary()

	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler must not be called on Unavailable")
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Create"}
	ctx := principalCtx("user", "usr_alice")
	req := &computev1.CreateInstanceRequest{ProjectId: "prj_demo"}

	_, err := uIntr(ctx, req, info, handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestInterceptor_Unary_MachineTypeList_ClusterCatalog(t *testing.T) {
	// Catalog object — "cluster:cluster_kacho_root": FGA model имеет `type cluster`
	// с user:* viewer cascade.
	intr, _ := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_alice", subject)
		require.Equal(t, "viewer", relation)
		require.Equal(t, "cluster:cluster_kacho_root", object, "MachineType/Zone/Region — viewer on cluster:cluster_kacho_root")
		return true, nil
	})
	uIntr := intr.Unary()
	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.MachineTypeService/List"}
	ctx := principalCtx("user", "usr_alice")

	_, err := uIntr(ctx, &computev1.ListMachineTypesRequest{}, info, handler)
	require.NoError(t, err)
	require.True(t, called)
}

func TestInterceptor_Unary_NoPrincipal_Denied(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
		t.Fatal("Check must not be called when principal is empty")
		return false, nil
	})
	uIntr := intr.Unary()

	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Get"}
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: ""})
	req := &computev1.GetInstanceRequest{InstanceId: "epd_x"}

	_, err := uIntr(ctx, req, info, handler)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.Equal(t, 0, *calls)
}

func TestInterceptor_Unary_UnmappedRPC_Denied(t *testing.T) {
	intr, _ := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
		t.Fatal("Check не должен вызываться для unmapped RPC")
		return false, nil
	})
	uIntr := intr.Unary()
	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler не должен вызываться для unmapped RPC")
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/SomeNewMethodWithoutMapping"}
	ctx := principalCtx("user", "usr_alice")
	_, err := uIntr(ctx, struct{}{}, info, handler)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestInterceptor_Unary_UnmappedInternalRPC_FailClosed(t *testing.T) {
	// Не-замапленный RPC fail-closed даже при "Internal" в имени: interceptor
	// больше не делает name-based исключений (молчаливый пропуск по имени
	// "Internal*" был fail-open вектором на internal-периметре). Exempt-RPC
	// обязан явно стоять в PermissionMap (Public либо relation-gated).
	// InternalMachineTypeService/SomeFutureUnmappedMethod — намеренно не
	// зарегистрированный (нет такого метода в сервисе) путь, используем его как
	// синтетический "ещё не замаплен" пример: запрос отклоняется с
	// PermissionDenied (rpc not mapped), ни handler, ни Check не вызываются.
	intr, calls := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
		t.Fatal("Check не должен вызываться для unmapped Internal* RPC")
		return false, nil
	})
	uIntr := intr.Unary()
	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InternalMachineTypeService/SomeFutureUnmappedMethod"}
	ctx := principalCtx("user", "usr_alice")

	_, err := uIntr(ctx, struct{}{}, info, handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.False(t, called)
	require.Equal(t, 0, *calls)
}

// TestInterceptor_Stream_InternalWatch_ReachesHandlerOnlyWithASubject — the outbox
// stream reaches its handler only when the request names a caller.
//
// # Why this test says the opposite of what it used to
//
// Its previous name was "PublicExempt" and it asserted that a stream with an EMPTY
// context reaches the handler. That is precisely the defect: the interceptor answered
// allow before reading the subject, so the handler ran for a caller nobody had
// identified — and the handler then streamed every tenant's resource snapshots.
//
// What is genuinely true, and is still asserted here, is that no SINGLE-object Check
// happens: the rows belong to individually-owned objects the request never names, so
// one question about one object cannot gate this RPC. What replaces it is not
// "nothing" but two things: an unconditional subject requirement, asserted here, and
// per-row narrowing in the handler, asserted in
// internal/handler/internal_watch_authorization_test.go on what the caller receives.
//
// The subject cut must be UNCONDITIONAL — not "when the mode is production". There is
// no per-RPC Check underneath a scope-filtered RPC to fall back on, so an
// unrecognised caller plus a missing filter is the original hole again.
func TestInterceptor_Stream_InternalWatch_ReachesHandlerOnlyWithASubject(t *testing.T) {
	const watchMethod = "/kacho.cloud.compute.v1.InternalWatchService/Watch"

	t.Run("identified caller reaches the handler without a single-object Check", func(t *testing.T) {
		intr, calls := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
			t.Fatal("a scope-filtered RPC must not be gated by a single-object Check")
			return false, nil
		})
		called := false
		handler := func(srv any, ss grpc.ServerStream) error { called = true; return nil }
		ss := &fakeServerStream{ctx: principalCtx("user", "usr_alice")}

		err := intr.Stream()(nil, ss, &grpc.StreamServerInfo{FullMethod: watchMethod}, handler)

		require.NoError(t, err)
		require.True(t, called, "an identified caller must reach the handler, which narrows the stream per row")
		require.Equal(t, 0, *calls, "no single-object Check: the caller names no object to ask about")
	})

	t.Run("caller the request does not identify never reaches the handler", func(t *testing.T) {
		intr, _ := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
			t.Fatal("the model must not be consulted for a request that names nobody")
			return false, nil
		})
		called := false
		handler := func(srv any, ss grpc.ServerStream) error { called = true; return nil }
		ss := &fakeServerStream{ctx: context.Background()} // names nobody

		err := intr.Stream()(nil, ss, &grpc.StreamServerInfo{FullMethod: watchMethod}, handler)

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.PermissionDenied, st.Code())
		require.False(t, called,
			"the handler must not run for an unidentified caller: it would stream every tenant's snapshots")
	})
}

// fakeServerStream — minimal grpc.ServerStream fake carrying only a Context();
// Stream() reads no other method before delegating to handler.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func TestInterceptor_Unary_CacheHit(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
		return true, nil
	})
	uIntr := intr.Unary()
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Get"}
	ctx := principalCtx("user", "usr_alice")
	req := &computev1.GetInstanceRequest{InstanceId: "epd_x"}

	_, err := uIntr(ctx, req, info, handler)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	_, err = uIntr(ctx, req, info, handler)
	require.NoError(t, err)
	require.Equal(t, 1, *calls, "повторный Check на ту же тройку — cache hit")
}

func TestInterceptor_Unary_Breakglass_AllowsAll(t *testing.T) {
	intr := authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: "kacho-compute-test",
		Map:         check.PermissionMap(),
		Breakglass:  true,
	})
	uIntr := intr.Unary()
	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InstanceService/Delete"}
	ctx := principalCtx("user", "usr_bob")
	req := &computev1.DeleteInstanceRequest{InstanceId: "epd_x"}

	resp, err := uIntr(ctx, req, info, handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
}

func TestPermissionMap_CoverageSnapshot(t *testing.T) {
	m := check.PermissionMap()
	// Floor lowered 35 → 25 when compute's duplicate block storage was retired:
	// Disk (10) + Image (10) + Snapshot (9) + DiskType (2) + InternalDiskType (3)
	// left the map, taking 34 of its entries. What remains is Instance + MachineType
	// + InternalMachineType + Operation + InternalWatch. The floor guards against a
	// silent drift in registrations, so a deliberate retire moves it — by exactly
	// what the retire removed, and no further.
	if len(m) < 25 {
		t.Errorf("PermissionMap слишком мала (%d entries): подозрение на drift регистраций", len(m))
	}
}

func TestFactory_NoIAMConn_NoBreakglass_Error(t *testing.T) {
	_, err := check.NewInterceptor(check.Options{
		ServiceName: "kacho-compute-test",
		IAMConn:     nil,
		Breakglass:  false,
	})
	require.ErrorIs(t, err, check.ErrIAMConnNotConfigured)
}

func TestFactory_Breakglass_NoIAMConn_OK(t *testing.T) {
	intr, err := check.NewInterceptor(check.Options{
		ServiceName: "kacho-compute-test",
		IAMConn:     nil,
		Breakglass:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, intr)
}
