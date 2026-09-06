// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// INV-2a — AuthN+AuthZ на :9091 ВЕЗДЕ: ни один RPC internal
// InternalNetworkInterfaceService не проходит на одном mTLS-транспорте.
// Attach/Detach — per-RPC FGA-Check, editor на vpc_network_interface:<nic_id>
// (object-scoped, анти-BOLA). ListByInstance авторизуется на уровне ДАННЫХ: единичного
// объекта, про который можно спросить заранее, у него нет (инстансы называет
// вызывающий, ответ — про чужие по владению интерфейсы), поэтому видимость решается
// per-object в nicinternal, а boot-guard делает фильтр обязательным в production.

// TestPermissionMap_InternalNIC_Attach_EditorScoped — Attach гейтится editor на
// самом NIC (vpc_network_interface:<nic_id> из request-поля nic_id).
func TestPermissionMap_InternalNIC_Attach_EditorScoped(t *testing.T) {
	m := check.PermissionMap()
	e, ok := m.Lookup("/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/Attach")
	require.True(t, ok, "InternalNetworkInterfaceService/Attach должен быть в PermissionMap (INV-2a)")
	require.Equal(t, "editor", e.Relation)
	require.False(t, e.Public, "Attach гейтится Check'ом, не Public-skip")

	objType, objID, err := e.Extract(&vpcv1.AttachNetworkInterfaceRequest{NicId: "nic_x", InstanceId: "epdinst01"})
	require.NoError(t, err)
	require.Equal(t, "vpc_network_interface", objType, "object-scoped на самом NIC (анти-BOLA)")
	require.Equal(t, "nic_x", objID)
}

// TestPermissionMap_InternalNIC_Detach_EditorScoped — Detach гейтится editor на NIC.
func TestPermissionMap_InternalNIC_Detach_EditorScoped(t *testing.T) {
	m := check.PermissionMap()
	e, ok := m.Lookup("/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/Detach")
	require.True(t, ok, "InternalNetworkInterfaceService/Detach должен быть в PermissionMap")
	require.Equal(t, "editor", e.Relation)

	objType, objID, err := e.Extract(&vpcv1.DetachNetworkInterfaceRequest{NicId: "nic_y", InstanceId: "epdinst02"})
	require.NoError(t, err)
	require.Equal(t, "vpc_network_interface", objType)
	require.Equal(t, "nic_y", objID)
}

// TestPermissionMap_InternalNIC_ListByInstance_ScopeFiltered — ListByInstance
// авторизуется на уровне данных, а не единичным per-RPC Check'ом.
//
// Инстансы называет вызывающий, а ответ касается интерфейсов, у каждого из которых
// свой владелец: одного объекта, про который можно спросить заранее, здесь нет.
// Прежний вопрос — `viewer` на singleton `cluster:cluster_root` — относился к
// ГЛОБАЛЬНОМУ СПРАВОЧНИКУ (регионы, зоны, типы дисков), и bootstrap кластера
// намеренно пишет `cluster:<root>#viewer@user:*`, чтобы справочник читал любой
// аутентифицированный субъект. Значит проверка пропускала всех и отдавала привязки
// любых названных инстансов, включая чужие проекты и аккаунты.
//
// Locked здесь: relation cluster-уровня НЕ восстановлен (иначе гейт снова стал бы
// формальностью), и метод НЕ помечен Public (это не exempt — авторизация не исчезла,
// она переехала на данные).
func TestPermissionMap_InternalNIC_ListByInstance_ScopeFiltered(t *testing.T) {
	m := check.PermissionMap()
	e, ok := m.Lookup("/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance")
	require.True(t, ok, "InternalNetworkInterfaceService/ListByInstance должен быть в PermissionMap")
	require.True(t, e.ScopeFiltered, "видимость решается per-object в nicinternal, не единичным Check'ом")
	require.False(t, e.Public, "не exempt: авторизация переехала на данные, а не исчезла")
	require.Empty(t, e.Relation,
		"cluster-scoped relation здесь пропускал каждого аутентифицированного субъекта "+
			"(`cluster:<root>#viewer@user:*` пишет bootstrap) — не восстанавливать")
	require.Nil(t, e.Extract, "объекта, про который можно спросить заранее, у этого RPC нет")
}

// TestScopeFilteredRPCs_CoversListByInstance — метод обязан попасть в список, который
// composition root отдаёт в config.ValidateListFilter: в production сервис не
// стартует без включённого и резолвимого list-filter'а. Без этого защита держалась бы
// на одной конфигурационной ручке, которую можно выключить и не заметить.
func TestScopeFilteredRPCs_CoversListByInstance(t *testing.T) {
	require.Contains(t, check.ScopeFilteredRPCs(),
		"/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance",
		"ScopeFiltered RPC обязан быть покрыт production boot-guard'ом ValidateListFilter")
}

// TestInterceptor_InternalNIC_ListByInstance_NoSingleObjectCheck — behaviour-level:
// interceptor НЕ задаёт единичный вопрос модели за этот RPC и пропускает его к
// handler'у, который сузит ответ per-object. Assert `calls==0` ловит регрессию, при
// которой сюда вернули бы cluster-scoped Check (он снова пропускал бы всех).
func TestInterceptor_InternalNIC_ListByInstance_NoSingleObjectCheck(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, _, _, _ string) (bool, error) {
		return false, nil // deny — если Check вообще вызовут, вызов не дойдёт до handler'а
	})
	uIntr := intr.Unary()

	handlerCalled := false
	handler := func(_ context.Context, _ any) (any, error) {
		handlerCalled = true
		return &vpcv1.ListNetworkInterfacesByInstanceResponse{}, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance"}
	ctx := principalCtx("user", "usr_alice")
	req := &vpcv1.ListNetworkInterfacesByInstanceRequest{InstanceIds: []string{"epdinst03"}}

	_, err := uIntr(ctx, req, info, handler)
	require.NoError(t, err)
	require.True(t, handlerCalled, "handler обязан отработать — он и есть точка авторизации")
	require.Equal(t, 0, *calls, "единичный Check за этот RPC не задаётся: спрашивать нечего")
}

// TestPermissionMap_InternalNIC_NoExemptGuard — блокирующий drift-guard: КАЖДЫЙ метод
// InternalNetworkInterfaceService (из proto-дескриптора) обязан быть в Map и не быть
// exempt (Public=false). Забытый RPC → гейт упадёт здесь, а не пропустит запрос.
func TestPermissionMap_InternalNIC_NoExemptGuard(t *testing.T) {
	m := check.PermissionMap()
	for _, md := range vpcv1.InternalNetworkInterfaceService_ServiceDesc.Methods {
		full := "/" + vpcv1.InternalNetworkInterfaceService_ServiceDesc.ServiceName + "/" + md.MethodName
		e, ok := m.Lookup(full)
		require.Truef(t, ok, "%s обязан быть в PermissionMap (INV-2a drift-guard)", full)
		require.Falsef(t, e.Public, "%s не должен быть exempt (Public=false)", full)
	}
}

// TestInterceptor_InternalNIC_Attach_Deny_ObjectScoped — behaviour-level INV-2a: на
// internal listener'е per-RPC Check РЕАЛЬНО вызывается с object-scope
// (editor @ vpc_network_interface:<nic_id>) и deny → PERMISSION_DENIED, handler НЕ
// вызывается. Assert `calls==1` ловит регрессию, где метод выпал из PermissionMap:
// тогда interceptor вернул бы PermissionDenied по unmapped-fail-closed (calls==0),
// а не по настоящей object-scoped авторизации.
func TestInterceptor_InternalNIC_Attach_Deny_ObjectScoped(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_mallory", subject)
		require.Equal(t, "editor", relation)
		require.Equal(t, "vpc_network_interface:nic_victim", object, "Check против ЦЕЛЕВОГО NIC (анти-BOLA)")
		return false, nil // deny
	})
	uIntr := intr.Unary()

	handlerCalled := false
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return "should not be returned", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/Attach"}
	ctx := principalCtx("user", "usr_mallory")
	req := &vpcv1.AttachNetworkInterfaceRequest{NicId: "nic_victim", InstanceId: "epdinst_attacker"}

	_, err := uIntr(ctx, req, info, handler)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, st.Code(), "deny → PERMISSION_DENIED (INV-2a)")
	require.False(t, handlerCalled, "handler НЕ вызывается на DENY")
	require.Equal(t, 1, *calls, "Check РЕАЛЬНО вызван (не unmapped-fail-closed): метод в PermissionMap")
}
