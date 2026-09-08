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
		Cache:       authz.NewCache(0),
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

// TestInterceptor_Unary_Unavailable_FailClosed — модель прав не ответила: запрос
// отвергнут (обработчик не вызван) и отвергнут кодом НЕДОСТУПНОСТИ.
//
// Ожидание прежде называло отказ в правах, и утверждение этой правкой не
// ослаблено: fail-closed проверяется прямо. Менялось то, что сказано вызывающему.
// «Тебе нельзя» означает «повторять бессмысленно» — решение зависит от
// вызывающего, отношения и объекта, и повтор не меняет ни одного из трёх; здесь
// же про права не сказано ничего, и через мгновение ответ будет. Полосы целиком —
// pkg/authz/decision_lane_codes_test.go.
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
	require.Equal(t, codes.Unavailable, st.Code(),
		"недоступность модели — не решение о правах: вызывающий обязан прочесть её как повторяемую")
}

func TestInterceptor_Unary_MachineTypeList_ClusterCatalog(t *testing.T) {
	// Catalog object — "cluster:cluster_root": FGA model имеет `type cluster`
	// с user:* viewer cascade.
	intr, _ := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_alice", subject)
		require.Equal(t, "viewer", relation)
		require.Equal(t, "cluster:cluster_root", object, "MachineType/Zone/Region — viewer on cluster:cluster_root")
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

// Здесь стояла проба стримового перехватчика на потоке журнала изменений: она
// закрепляла, что опознанный вызывающий доходит до обработчика без единого
// вопроса про объект, а неопознанный отсекается БЕЗУСЛОВНО.
//
// Поток снят. Утверждение про безусловное отсечение пустого субъекта
// остаётся нормой корпуса и от этого RPC не зависело: за методом, сужаемым по
// данным, нет per-RPC Check, на который можно откатиться, поэтому неопознанный
// вызывающий при выключенном фильтре означал бы исходную дыру. Сегодня у compute
// сужаемых по данным методов нет вовсе — свойство проверяется там, где предмет
// есть, а здесь проба утверждала бы про несуществующий метод.

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
		Cache:       authz.NewCache(0),
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
	//
	// Lowered 25 → 17 when eight InstanceService methods left the contract: seven
	// carried no implementation and answered UNIMPLEMENTED while being exposed on
	// three surfaces at once, and UpdateMetadata went with its subject — the
	// free-form metadata map. Moved by exactly eight, per the rule above: a floor
	// that moves further than the retire would stop guarding what it guards.
	if len(m) < 17 {
		t.Errorf("PermissionMap слишком мала (%d entries): подозрение на drift регистраций", len(m))
	}
}
