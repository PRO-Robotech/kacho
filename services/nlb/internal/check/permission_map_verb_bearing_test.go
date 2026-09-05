// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/check"
)

// Целевая (Design-B) карта enforcement-relation'ов для kacho-nlb: per-RPC Check
// резолвит action на verb-bearing relation (`v_get`/`v_update`/`v_delete`/`v_list`),
// а не на tier. object-self RPC (Extract → id ресурса) флипается на v_*; Create
// (parent-scoped) остаётся tier `editor`; top-level project-List остаётся `viewer`
// (visibility — через per-object iam-union viewer ∪ v_list). Инертный `Permission`-string не
// меняется (fine-grained энфорс — отдельная под-фаза, deferred).

var verbGetRPCs = []string{
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get",
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/GetTargetStates",
	"/kacho.cloud.loadbalancer.v1.ListenerService/Get",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Get",
}

var verbUpdateRPCs = []string{
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update",
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Move",
	"/kacho.cloud.loadbalancer.v1.ListenerService/Update",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Update",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Move",
}

// membershipRPCs — NLB-TGT-1: управление СОСТАВОМ группы целей энфорсится
// собственными отношениями, а не отношением изменения самой группы.
//
// Пока обе точки решения требовали `v_update`, выдать право управлять составом, не
// выдав права менять саму группу, было НЕЛЬЗЯ — а роль `loadbalancer.target_manager`
// именно это и обещает. Оба отношения объявлены НАДМНОЖЕСТВОМ `v_update`
// (proto/…/iam/v1/fga_model.fga), поэтому сегодняшний редактор ничего не теряет:
// его прямой кортеж `v_update` разрешает и новый вопрос.
//
// Имена отношений не выбраны, а выведены: реконсайлер kaname собирает их из
// авторского глагола правила роли, поэтому `addTargets` даёт ровно `v_addtargets`.
// Прежняя редакция этого замка требовала `v_update` для обоих RPC — она пинила
// состояние ДО раскола и обновлена вместе с ним.
var membershipRPCs = map[string]string{
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/AddTargets":    "v_addtargets",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/RemoveTargets": "v_removetargets",
}

var verbDeleteRPCs = []string{
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete",
	"/kacho.cloud.loadbalancer.v1.ListenerService/Delete",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Delete",
}

var verbListOnResourceRPCs = []string{
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/ListOperations",
	"/kacho.cloud.loadbalancer.v1.ListenerService/ListOperations",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/ListOperations",
}

// createRPCs — parent-scoped Create: NLB/TG на project, Listener на parent LB.
// Все остаются tier `editor` (F-7; create-authority = write-authz на parent).
var createRPCs = []string{
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create",
	"/kacho.cloud.loadbalancer.v1.ListenerService/Create",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/Create",
}

var projectListRPCs = []string{
	"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/List",
	"/kacho.cloud.loadbalancer.v1.ListenerService/List",
	"/kacho.cloud.loadbalancer.v1.TargetGroupService/List",
}

func TestPermissionMap_VerbBearing_Get_VGet(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbGetRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_get", e.Relation, "%s: object-self read must enforce v_get (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_Update_VUpdate(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbUpdateRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_update", e.Relation, "%s: object-self mutation must enforce v_update (Design B)", rpc)
	}
}

// TestPermissionMap_VerbBearing_Membership_OwnRelations — NLB-TGT-1.
//
// Несущее утверждение положительное: каждый RPC управления составом энфорсит СВОЁ
// отношение. Парное отрицание — в том же прогоне: ни один из них не остался на
// отношении изменения группы, иначе выдать управление составом отдельно по-прежнему
// было бы нельзя, а замок зеленел бы на неизменившемся дереве.
func TestPermissionMap_VerbBearing_Membership_OwnRelations(t *testing.T) {
	m := check.PermissionMap()
	require.NotEmpty(t, membershipRPCs, "перечень пуст — утверждать нечего")
	for rpc, want := range membershipRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, want, e.Relation,
			"%s: управление составом обязано энфорсить %s, а не отношение изменения самой группы — "+
				"иначе право управлять составом невыдаваемо отдельно", rpc, want)
	}
	// Парный положительный контроль на ту же карту: изменение самой группы с места
	// не сдвинулось. Без него «переключили» неотличимо от «переключили всё подряд».
	e, ok := m["/kacho.cloud.loadbalancer.v1.TargetGroupService/Update"]
	require.True(t, ok, "TargetGroupService/Update must be mapped")
	require.Equal(t, "v_update", e.Relation,
		"изменение самой группы съехало с v_update — под-фаза трогает только управление составом")
}

func TestPermissionMap_VerbBearing_Delete_VDelete(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbDeleteRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_delete", e.Relation, "%s: object-self delete must enforce v_delete (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_ListOnResource_VList(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range verbListOnResourceRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "v_list", e.Relation, "%s: object-self list-on-resource must enforce v_list (Design B)", rpc)
	}
}

func TestPermissionMap_VerbBearing_Create_StaysEditor(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range createRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, "editor", e.Relation, "%s: create stays tier editor on parent (F-7)", rpc)
	}
}

// TestPermissionMap_VerbBearing_ProjectList_IsNarrowedNotTierChecked — top-level
// список не гейтится ярусом, он сужается пообъектно.
//
// Здесь закреплялось отношение `viewer` на этих записях. Полоса сужения
// отношение не читает, поэтому утверждение было вакуумным: оно осталось бы
// зелёным, если бы там стояло `admin`, и покраснело бы только от отсутствия
// записи вовсе.
func TestPermissionMap_VerbBearing_ProjectList_IsNarrowedNotTierChecked(t *testing.T) {
	m := check.PermissionMap()
	for _, rpc := range projectListRPCs {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Truef(t, e.ScopeFiltered, "%s: top-level project List сужается пообъектно", rpc)
		require.Emptyf(t, e.Relation, "%s: на полосе сужения отношение не читается", rpc)
	}
}

// TestPermissionMap_VerbBearing_PermissionStringPreserved — инертный
// `Permission`-string не теряется при флипе Relation (fine-grained энфорс
// активируется отдельной под-фазой; catalog parity сохраняется).
func TestPermissionMap_VerbBearing_PermissionStringPreserved(t *testing.T) {
	m := check.PermissionMap()
	cases := map[string]string{
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get":    "loadbalancer.networkLoadBalancers.get",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete": "loadbalancer.networkLoadBalancers.delete",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/AddTargets":     "loadbalancer.targetGroups.addTargets",
	}
	for rpc, want := range cases {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.Equalf(t, want, e.Permission, "%s: Permission-string preserved", rpc)
	}
}

func TestPermissionMap_VerbBearing_NoTierLeftOnObjectSelf(t *testing.T) {
	m := check.PermissionMap()
	objectSelf := append(append(append(append([]string{}, verbGetRPCs...), verbUpdateRPCs...), verbDeleteRPCs...), verbListOnResourceRPCs...)
	for _, rpc := range objectSelf {
		e, ok := m[rpc]
		require.Truef(t, ok, "%s must be mapped", rpc)
		require.NotEqualf(t, "viewer", e.Relation, "%s: object-self must not stay on tier viewer", rpc)
		require.NotEqualf(t, "editor", e.Relation, "%s: object-self must not stay on tier editor", rpc)
	}
}
