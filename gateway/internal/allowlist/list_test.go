// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// TestGateway_E_Exists_canonical_AllowlistBlocksAllInternalServices — закреплённые
// примеры Internal-методов, которые не проходят через allowlist.
//
// Это ПРИМЕРЫ, а не матрица: раньше заголовок обещал матрицу, и восемь из
// одиннадцати её строк называли сервисы в форме «<Xxx>InternalService», которой
// в дереве не носит НИ ОДИН сервис — то есть большая часть «матрицы» ни при каких
// условиях упасть не могла. Матрицу по всей популяции Internal-методов держит
// TestAllowlist_NoInternalRPCIsRoutable (parity_test.go): она перебирает те 79,
// что реально объявлены в дескрипторах.
func TestGateway_E_Exists_canonical_AllowlistBlocksAllInternalServices(t *testing.T) {
	internalMethods := []string{
		// iam
		"/kaname.cloud.iam.v1.InternalUserService/UpsertFromIdentity",
		"/kaname.cloud.iam.v1.InternalIAMService/LookupSubject",
		"/kaname.cloud.iam.v1.InternalIAMService/Check",
	}

	for _, m := range internalMethods {

		t.Run(m, func(t *testing.T) {
			if allowlist.IsAllowed(m) {
				t.Errorf("метод %q НЕ должен быть в allowlist", m)
			}
			if !allowlist.HasInternalSuffix(m) {
				t.Errorf("метод %q должен определяться как Internal (HasInternalSuffix)", m)
			}
		})
	}
}

// TestGateway_D3_AllowlistPublicMethodsPresent проверяет, что все публичные методы 1.0 API
// присутствуют в allowlist (положительный сценарий).
func TestGateway_D3_AllowlistPublicMethodsPresent(t *testing.T) {
	publicMethods := []string{
		// iam.v1 — Account / Project
		"/kaname.cloud.iam.v1.AccountService/Get",
		"/kaname.cloud.iam.v1.AccountService/List",
		"/kaname.cloud.iam.v1.AccountService/Create",
		"/kaname.cloud.iam.v1.AccountService/Update",
		"/kaname.cloud.iam.v1.AccountService/Delete",
		"/kaname.cloud.iam.v1.ProjectService/Get",
		"/kaname.cloud.iam.v1.ProjectService/List",
		"/kaname.cloud.iam.v1.ProjectService/Create",
		"/kaname.cloud.iam.v1.ProjectService/Update",
		"/kaname.cloud.iam.v1.ProjectService/Delete",
		// vpc.v1
		"/kacho.cloud.vpc.v1.NetworkService/Get",
		"/kacho.cloud.vpc.v1.NetworkService/List",
		"/kacho.cloud.vpc.v1.NetworkService/Create",
		"/kacho.cloud.vpc.v1.NetworkService/Update",
		"/kacho.cloud.vpc.v1.NetworkService/Delete",
		"/kacho.cloud.vpc.v1.SubnetService/Get",
		"/kacho.cloud.vpc.v1.SubnetService/List",
		"/kacho.cloud.vpc.v1.SubnetService/Create",
		"/kacho.cloud.vpc.v1.SubnetService/Update",
		"/kacho.cloud.vpc.v1.SubnetService/Delete",
		"/kacho.cloud.vpc.v1.AddressService/Get",
		"/kacho.cloud.vpc.v1.AddressService/List",
		"/kacho.cloud.vpc.v1.AddressService/Create",
		"/kacho.cloud.vpc.v1.AddressService/Update",
		"/kacho.cloud.vpc.v1.AddressService/Delete",
		"/kacho.cloud.vpc.v1.RouteTableService/Get",
		"/kacho.cloud.vpc.v1.RouteTableService/List",
		"/kacho.cloud.vpc.v1.RouteTableService/Create",
		"/kacho.cloud.vpc.v1.RouteTableService/Update",
		"/kacho.cloud.vpc.v1.RouteTableService/Delete",
		// operation (без v1) — только Get и Cancel
		"/kacho.cloud.operation.OperationService/Get",
		"/kacho.cloud.operation.OperationService/Cancel",
	}

	for _, m := range publicMethods {

		t.Run(m, func(t *testing.T) {
			if !allowlist.IsAllowed(m) {
				t.Errorf("публичный метод %q должен быть в allowlist", m)
			}
			if allowlist.HasInternalSuffix(m) {
				t.Errorf("публичный метод %q не должен определяться как Internal", m)
			}
		})
	}
}

// TestGateway_D6_OperationServiceAllowed проверяет OperationService методы — только Get и Cancel, без List.
func TestGateway_D6_OperationServiceAllowed(t *testing.T) {
	allowed := []string{
		"/kacho.cloud.operation.OperationService/Get",
		"/kacho.cloud.operation.OperationService/Cancel",
	}
	for _, m := range allowed {

		t.Run(m, func(t *testing.T) {
			if !allowlist.IsAllowed(m) {
				t.Errorf("метод %q должен быть в allowlist", m)
			}
		})
	}
}

// Снятые с контракта пути (OperationService/List, 0.x-глаголы Upsert/Watch,
// удалённый resourcemanager.v1, переехавший operation.v1) больше не
// перечисляются здесь поимённо. Их запрет несёт
// TestAllowlist_NoEntryWithoutAnRPC (parity_test.go): он краснеет на ЛЮБОЙ
// записи списка, за которой нет RPC в дескрипторах, — включая те, которых никто
// не предвидел. Перечисление же не могло упасть никогда: чтобы оно упало, надо
// было вписать в список несуществующий метод.

// TestGateway_D8_LoadbalancerActive проверяет, что kacho-nlb активирован —
// публичные методы NetworkLoadBalancer / Listener / TargetGroup в allowlist, а
// InternalResourceLifecycleService (streaming, gRPC-direct only) — НЕ в allowlist
// (блокируется HasInternalSuffix; Internal не публикуется на external).
func TestGateway_D8_LoadbalancerActive(t *testing.T) {
	publicMethods := []string{
		// NetworkLoadBalancerService
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/List",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Move",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/GetTargetStates",
		"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/ListOperations",
		// ListenerService
		"/kacho.cloud.loadbalancer.v1.ListenerService/Get",
		"/kacho.cloud.loadbalancer.v1.ListenerService/List",
		"/kacho.cloud.loadbalancer.v1.ListenerService/Create",
		"/kacho.cloud.loadbalancer.v1.ListenerService/Update",
		"/kacho.cloud.loadbalancer.v1.ListenerService/Delete",
		"/kacho.cloud.loadbalancer.v1.ListenerService/ListOperations",
		// TargetGroupService
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Get",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/List",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Create",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Update",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Delete",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/Move",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/AddTargets",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/RemoveTargets",
		"/kacho.cloud.loadbalancer.v1.TargetGroupService/ListOperations",
	}
	for _, m := range publicMethods {

		t.Run("public/"+m, func(t *testing.T) {
			if !allowlist.IsAllowed(m) {
				t.Errorf("публичный nlb-метод %q должен быть в allowlist", m)
			}
		})
	}

	// Перечень пуст: единственный метод, который здесь стоял, снят вместе со
	// своим потоком (#814) — у него не было ни одного потребителя. Пустой
	// перечень объявляется ЯВНО, а не удаляется вместе с проверкой: свойство
	// «внутренний метод не попадает на внешнюю поверхность» остаётся нормой, и
	// следующий такой метод обязан попасть сюда, а не завестись молча.
	internalMethods := []string{}
	for _, m := range internalMethods {

		t.Run("internal/"+m, func(t *testing.T) {
			if allowlist.IsAllowed(m) {
				t.Errorf("Internal nlb-метод %q НЕ должен быть в allowlist", m)
			}
			if !allowlist.HasInternalSuffix(m) {
				t.Errorf("метод %q должен определяться как Internal (HasInternalSuffix)", m)
			}
		})
	}
}

// TestGateway_D8b_ComputeActive проверяет, что публичные compute-RPC в allowlist,
// а Internal*-методы compute — НЕ в allowlist (и блокируются HasInternalSuffix).
func TestGateway_D8b_ComputeActive(t *testing.T) {
	publicMethods := []string{
		"/kacho.cloud.compute.v1.InstanceService/Get",
		"/kacho.cloud.compute.v1.InstanceService/Start",
		"/kacho.cloud.compute.v1.InstanceService/AttachDisk",
	}
	for _, m := range publicMethods {

		t.Run("public/"+m, func(t *testing.T) {
			if !allowlist.IsAllowed(m) {
				t.Errorf("публичный compute-метод %q должен быть в allowlist", m)
			}
		})
	}

	internalMethods := []string{
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Create",
	}
	for _, m := range internalMethods {

		t.Run("internal/"+m, func(t *testing.T) {
			if allowlist.IsAllowed(m) {
				t.Errorf("Internal compute-метод %q НЕ должен быть в allowlist", m)
			}
			if !allowlist.HasInternalSuffix(m) {
				t.Errorf("Internal compute-метод %q должен ловиться HasInternalSuffix", m)
			}
		})
	}
}

// Опознание суффиксной конвенции «<Xxx>InternalService» проверяется на
// синтетическом входе в TestHasInternalSuffix_BothNamingConventions
// (named_surfaces_test.go), где он ЗАЯВЛЕН синтетическим и записан с причиной.
// Здесь такая проверка была одета в имя удалённого сервиса, из-за чего читалась
// как проба про живую поверхность.

// TestGateway_KAC105_IamActive проверяет публичные iam.v1 RPC:
//   - все 7 публичных сервисов (Account/Project/User/ServiceAccount/Group/Role/AccessBinding)
//     зарегистрированы в allowlist;
//   - InternalIAMService.* / InternalUserService.* — НЕ в allowlist и блокируются
//     HasInternalSuffix (Internal не публикуется на external);
//   - User не имеет публичного Create (создание через InternalUserService.UpsertFromIdentity).
func TestGateway_KAC105_IamActive(t *testing.T) {
	publicMethods := []string{
		// AccountService
		"/kaname.cloud.iam.v1.AccountService/Get",
		"/kaname.cloud.iam.v1.AccountService/Create",
		"/kaname.cloud.iam.v1.AccountService/Update",
		"/kaname.cloud.iam.v1.AccountService/Delete",
		// ProjectService (без Move)
		"/kaname.cloud.iam.v1.ProjectService/Get",
		"/kaname.cloud.iam.v1.ProjectService/Create",
		// UserService (read + delete + labels-only Update; Create остается
		// internal-only через InternalUserService.UpsertFromIdentity).
		"/kaname.cloud.iam.v1.UserService/Get",
		"/kaname.cloud.iam.v1.UserService/List",
		"/kaname.cloud.iam.v1.UserService/Delete",
		// Public async mutation: User.labels mutable (identity-поля immutable),
		// возвращает Operation; parity с RoleService/ServiceAccountService Update.
		"/kaname.cloud.iam.v1.UserService/Update",
		// ServiceAccountService
		"/kaname.cloud.iam.v1.ServiceAccountService/Create",
		"/kaname.cloud.iam.v1.ServiceAccountService/Delete",
		// GroupService (+ AddMember/RemoveMember/ListMembers)
		"/kaname.cloud.iam.v1.GroupService/Create",
		"/kaname.cloud.iam.v1.GroupService/AddMember",
		"/kaname.cloud.iam.v1.GroupService/RemoveMember",
		"/kaname.cloud.iam.v1.GroupService/ListMembers",
		// RoleService
		"/kaname.cloud.iam.v1.RoleService/Create",
		"/kaname.cloud.iam.v1.RoleService/Delete",
		// AccessBindingService (+ ListByScope/ListBySubject/ListByAccount/ListSubjectPrivileges)
		"/kaname.cloud.iam.v1.AccessBindingService/Create",
		// RBAC explicit model — Update clears
		// deletion_protection so a protected binding can be deleted; public mutation
		// (NOT Internal; goes on external), editor relation, same surface as Delete.
		"/kaname.cloud.iam.v1.AccessBindingService/Update",
		"/kaname.cloud.iam.v1.AccessBindingService/Delete",
		// ListByScope — scope-scoped list of bindings (sync read).
		"/kaname.cloud.iam.v1.AccessBindingService/ListByScope",
		"/kaname.cloud.iam.v1.AccessBindingService/ListBySubject",
		"/kaname.cloud.iam.v1.AccessBindingService/ListByAccount",
		// public, sync read (NOT Internal; goes on external).
		"/kaname.cloud.iam.v1.AccessBindingService/ListSubjectPrivileges",
		// public, sync read (NOT Internal; goes on external).
		"/kaname.cloud.iam.v1.AccessBindingService/ListAssignableRoles",
		// public, sync reads (NOT Internal; on external).
		// ListByRole: bindings of a role; ExpandAccess: principals expansion. Both
		// cluster-scoped viewer floor (catalog), acr 2.
		"/kaname.cloud.iam.v1.AccessBindingService/ListByRole",
		"/kaname.cloud.iam.v1.AccessBindingService/ExpandAccess",
		// public, sync read (NOT Internal; on external).
		// PermissionCatalogService.ListPermissionCatalog is an authenticated-floor
		// read (<exempt> permission in the generated catalog), reachable via REST
		// GET /iam/v1/permissionCatalog on the external listener so the UI can build
		// its role/permission palette without an Internal* RPC.
		"/kaname.cloud.iam.v1.PermissionCatalogService/ListPermissionCatalog",
	}
	for _, m := range publicMethods {

		t.Run("public/"+m, func(t *testing.T) {
			if !allowlist.IsAllowed(m) {
				t.Errorf("публичный iam-метод %q должен быть в allowlist", m)
			}
			if allowlist.HasInternalSuffix(m) {
				t.Errorf("публичный iam-метод %q не должен ловиться HasInternalSuffix", m)
			}
		})
	}

	// UserService.Create в contract'е ОТСУТСТВУЕТ вовсе (Users появляются через
	// InternalUserService.UpsertFromIdentity и публичный Invite), поэтому
	// утверждение «его нет в списке» не могло упасть: чтобы упасть, кто-то должен
	// был вписать в список несуществующий RPC — а это ловит
	// TestAllowlist_NoEntryWithoutAnRPC (parity_test.go) для ЛЮБОГО такого пути.

	// InternalIAMService / InternalUserService — internal-only (не на external).
	// auth-interceptor api-gateway зовет kaname:9091 напрямую через gRPC-client.
	// ListPermissions из этого перечня убран: такого метода у InternalIAMService
	// нет, то есть строка проверяла отсутствие того, чего не существует.
	internalMethods := []string{
		"/kaname.cloud.iam.v1.InternalIAMService/LookupSubject",
		"/kaname.cloud.iam.v1.InternalUserService/UpsertFromIdentity",
		"/kaname.cloud.iam.v1.InternalUserService/Get",
	}
	for _, m := range internalMethods {

		t.Run("internal/"+m, func(t *testing.T) {
			if allowlist.IsAllowed(m) {
				t.Errorf("Internal iam-метод %q НЕ должен быть в allowlist", m)
			}
			if !allowlist.HasInternalSuffix(m) {
				t.Errorf("Internal iam-метод %q должен ловиться HasInternalSuffix", m)
			}
		})
	}
}

// Пакета resourcemanager.v1 в дереве нет ни одного файла, поэтому перечисление
// его путей как «заблокированных» описывало не запрет, а собственное отсутствие.
// Запрет на любой путь без RPC несёт TestAllowlist_NoEntryWithoutAnRPC
// (parity_test.go), и он покрывает не пять имён, а все возможные.

// TestAllowlist_UserBlockUnblock — административный запрет участию проходит
// директора, и проходит СИММЕТРИЧНО.
//
// Отдельная проба, потому что молчаливый исход дороже громкого: метода нет в
// списке → директор отвергает его ДО каталога и таблицы маршрутов, то есть
// каталог полон, маршрут отрендерен, а RPC недостижим. И односторонний
// недосмотр здесь хуже двустороннего: заблокировать смогли, снять — нет.
func TestAllowlist_UserBlockUnblock(t *testing.T) {
	for _, m := range []string{
		"/kaname.cloud.iam.v1.UserService/Block",
		"/kaname.cloud.iam.v1.UserService/Unblock",
	} {
		if !allowlist.IsAllowed(m) {
			t.Errorf("%s должен быть в allowlist: без записи gRPC-директор отвергает метод "+
				"раньше каталога, и действие недостижимо при полном каталоге", m)
		}
		if allowlist.HasInternalSuffix(m) {
			t.Errorf("%s — публичный RPC, он не должен считаться Internal*", m)
		}
	}
}
