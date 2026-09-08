// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"fmt"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/check"
)

// permissionRegex —
//
//	^loadbalancer\.[a-z]+\.[a-z][A-Za-z]+$
//
// Group 1 = resource (lowercase + digits ok, no separator); group 2 = verb
// (starts с lowercase letter, может содержать camelCase для длинных глаголов
// типа `attachTargetGroup`, `getTargetStates`, `listOperations`).
var permissionRegex = regexp.MustCompile(`^loadbalancer\.[a-z][a-zA-Z0-9]*\.[a-z][a-zA-Z]+$`)

// allPublicServiceDescs — gRPC-сервисы NLB на public listener'е
// (см. `cmd/kacho-loadbalancer/main.go`).
//
// Источник истины: proto-generated `<Service>_ServiceDesc` variables.
var allPublicServiceDescs = []grpc.ServiceDesc{
	lbv1.NetworkLoadBalancerService_ServiceDesc,
	lbv1.ListenerService_ServiceDesc,
	lbv1.TargetGroupService_ServiceDesc,
	// Чтение квот арендатором: предел, потребление и источник величины.
	lbv1.QuotaService_ServiceDesc,
	operationpb.OperationService_ServiceDesc,
}

// allInternalServiceDescs — gRPC-сервисы NLB на cluster-internal listener'е (9091).
// Internal-листенер гоняет ТОТ ЖЕ per-RPC authz Check, что и public (security.md
// «authN+authZ на ОБОИХ listener'ах»; «Internal = trusted» — запрещённое допущение),
// поэтому их RPC ТОЖЕ обязаны быть в PermissionMap (с реальным Relation, не Public).
var allInternalServiceDescs = []grpc.ServiceDesc{
	lbv1.InternalLoadBalancerAnnounceService_ServiceDesc,
}

// allServiceDescs — public ∪ internal (все listener'ы гоняют один authz-interceptor).
func allServiceDescs() []grpc.ServiceDesc {
	out := make([]grpc.ServiceDesc, 0, len(allPublicServiceDescs)+len(allInternalServiceDescs))
	out = append(out, allPublicServiceDescs...)
	out = append(out, allInternalServiceDescs...)
	return out
}

// internalRPCSet — множество full-method'ов internal-RPC. Их Permission намеренно
// пуст (cluster-floor relation-based gate, НЕ tenant-facing каталожный permission).
func internalRPCSet() map[string]struct{} {
	s := make(map[string]struct{})
	for _, sd := range allInternalServiceDescs {
		for _, mi := range sd.Methods {
			s[fullMethodName(sd, mi.MethodName)] = struct{}{}
		}
		for _, si := range sd.Streams {
			s[fullMethodName(sd, si.StreamName)] = struct{}{}
		}
	}
	return s
}

// fullMethodName собирает gRPC FullMethod из ServiceDesc.ServiceName + method name:
// "/<package>.<Service>/<Method>".
func fullMethodName(sd grpc.ServiceDesc, methodName string) string {
	return "/" + sd.ServiceName + "/" + methodName
}

// TestDrift_EveryRPCMapped — каждый зарегистрированный публичный RPC покрыт
// PermissionMap (либо как RPCEntry либо как Public: true). Любая «новая»
// RPC, добавленная в proto, без соответствующей регистрации здесь — fail CI.
//
// Этот тест — (drift-test catches unmapped RPC).
func TestDrift_EveryRPCMapped(t *testing.T) {
	m := check.PermissionMap()

	var missing []string
	for _, sd := range allServiceDescs() {
		for _, mi := range sd.Methods {
			fm := fullMethodName(sd, mi.MethodName)
			if _, ok := m[fm]; !ok {
				missing = append(missing, fm)
			}
		}
		for _, si := range sd.Streams {
			fm := fullMethodName(sd, si.StreamName)
			if _, ok := m[fm]; !ok {
				missing = append(missing, fm)
			}
		}
	}
	sort.Strings(missing)
	require.Empty(t, missing,
		"PermissionMap drift: %d RPC(s) not registered in permission_map.go: %v\n"+
			"Add either an RPCEntry (Relation/Permission/Extract) or `{Public: true}` for exempt RPCs.",
		len(missing), missing)
}

// TestDrift_PermissionUnique — все Permission строки в PermissionMap уникальны
// (нет двух RPC, делящих один permission). Design.
func TestDrift_PermissionUnique(t *testing.T) {
	m := check.PermissionMap()
	seen := make(map[string]string, len(m))
	for fm, e := range m {
		if e.Permission == "" {
			continue // Public RPC — допускается пустой Permission.
		}
		if prev, dup := seen[e.Permission]; dup {
			t.Errorf("Permission %q used by both %q and %q (must be unique)",
				e.Permission, prev, fm)
		}
		seen[e.Permission] = fm
	}
}

// TestDrift_PermissionRegex — все Permission соответствуют regex
// `^loadbalancer\.[a-z]+\.[a-z][A-Za-z]+$`. Design.
func TestDrift_PermissionRegex(t *testing.T) {
	for fm, e := range check.PermissionMap() {
		if e.Permission == "" {
			continue // Public — skip.
		}
		require.Regexpf(t, permissionRegex, e.Permission,
			"RPC %q: Permission %q does not match %s",
			fm, e.Permission, permissionRegex.String())
	}
}

// TestDrift_PermissionNonEmpty — каждый РЕАЛЬНЫЙ (не-Public) RPCEntry имеет
// non-empty Permission (для будущего fine-grained Check / для каталога iam).
// Design.
func TestDrift_PermissionNonEmpty(t *testing.T) {
	internal := internalRPCSet()
	for fm, e := range check.PermissionMap() {
		if e.Public {
			continue
		}
		if _, ok := internal[fm]; ok {
			// Internal cluster-floor RPC: relation-based gate (system_viewer @ cluster),
			// НЕ tenant-facing каталожный permission — пустой Permission допустим.
			continue
		}
		require.NotEmptyf(t, e.Permission,
			"RPC %q is not Public but has empty Permission — fill from catalog `loadbalancer.<resource>.<verb>`", fm)
	}
}

// TestDrift_PublicRPCsHaveNoRelation — Public RPC не должны иметь Extract /
// Relation (защита от случайной частичной заливки — иначе interceptor
// проигнорирует Relation, что вызовёт путаницу при ревью).
func TestDrift_PublicRPCsHaveNoRelation(t *testing.T) {
	for fm, e := range check.PermissionMap() {
		if !e.Public {
			continue
		}
		require.Empty(t, e.Relation,
			"RPC %q marked Public — Relation must be empty (got %q)", fm, e.Relation)
		require.Nil(t, e.Extract,
			"RPC %q marked Public — Extract must be nil", fm)
		require.Empty(t, e.Permission,
			"RPC %q marked Public — Permission must be empty (exempt из per-RPC Check)", fm)
	}
}

// TestDrift_EveryEntryIsExactlyOneLane — у каждой записи РОВНО одна полоса, и
// поля заполнены ровно те, которые эта полоса читает.
//
// Прежде здесь стояло «у не-Public RPC должен быть Extract», и это было верно на
// две полосы из трёх. Полоса сужения (ScopeFiltered) объекта не имеет by
// construction: интерсептор ветвится на неё РАНЬШЕ, чем доходит до отношения, —
// поэтому отношение и экстрактор на ней не читаются, а заполненные означали бы
// второе объявление о том же вызове.
func TestDrift_EveryEntryIsExactlyOneLane(t *testing.T) {
	lanes := map[string]int{}
	for fm, e := range check.PermissionMap() {
		require.Falsef(t, e.Public && e.ScopeFiltered,
			"RPC %q объявлен сразу двумя полосами — полос ровно три и они исключают друг друга", fm)
		switch {
		case e.Public:
			lanes["exempt"]++
			require.Emptyf(t, e.Relation, "RPC %q: exempt не читает отношение", fm)
			require.Nilf(t, e.Extract, "RPC %q: exempt не читает экстрактор", fm)
		case e.ScopeFiltered:
			lanes["scope-filtered"]++
			require.Emptyf(t, e.Relation, "RPC %q: полоса сужения не читает отношение", fm)
			require.Nilf(t, e.Extract, "RPC %q: у сужаемого списка нет единого объекта", fm)
		default:
			lanes["edge-checks"]++
			require.NotNilf(t, e.Extract, "RPC %q: Extract must be non-nil", fm)
			require.NotEmptyf(t, e.Relation, "RPC %q: Relation must be non-empty", fm)
		}
	}
	require.NotZero(t, lanes["edge-checks"], "предпосылка: хотя бы одна запись спрашивает отношение")
	require.NotZero(t, lanes["scope-filtered"], "предпосылка: полоса сужения в домене есть")
	t.Logf("перепись полос: %v", lanes)
}

// catalogCountRationale — почему имён 30, а не 26.
//
// 26 было числом РУКОПИСНОЙ карты, и оно расходилось с каталогом края в трёх
// местах. Два имени — `loadbalancer.networkLoadBalancers.{getAnnounceState,
// reportAnnounceState}` — каталог нёс всё это время, а рукописная карта
// оставляла их поле permission пустым «намеренно, это не tenant-facing». Третье
// — `loadbalancer.resourceLifecycle.subscribe` — появилось вместе с тем, что
// строка потока жизненного цикла перестала быть `<exempt>` и назвала отношение,
// которое сервис и без того требовал.
//
// Тридцатое — `loadbalancer.quotas.list`: арендаторское чтение квот. До него вся
// поверхность квот этого домена была административной, и арендатор, встретив
// отказ на пределе, не мог узнать ни своего потолка, ни своего расхода.
//
// Теперь имена берутся из аннотаций, то есть ровно из того, из чего собирается
// каталог, и «домен назвал одно, каталог другое» перестало быть выразимым.
const catalogCountRationale = "имён прав домена loadbalancer — 30: 26 прежних + два имени announce, " +
	"которые каталог нёс, а рукописная карта не называла, + поток жизненного цикла, " +
	"+ арендаторское чтение квот"

// TestDrift_CatalogCompleteness — Catalog возвращает ровно 26 уникальных
// permission строк (NLB CONTRACT удалил loadbalancer.networkLoadBalancers.
// {start,stop,attachTargetGroup,detachTargetGroup} → 30−4=26). Acceptance
func TestDrift_CatalogCompleteness(t *testing.T) {
	cat := check.Catalog()
	uniq := make(map[string]struct{}, len(cat))
	for _, p := range cat {
		uniq[p] = struct{}{}
	}
	// 30→29: снят вид прав снятого потока подписки (#814).
	require.Lenf(t, uniq, 29,
		catalogCountRationale+"; got %d: %v", len(uniq), sortedKeys(uniq))
	require.Equal(t, len(cat), len(uniq), "Catalog() contains duplicates")
}

// TestDrift_CatalogRegex — все 30 имён каталога соответствуют regex
// (включая catalog-only `loadbalancer.operations.{get,cancel,list}`).
func TestDrift_CatalogRegex(t *testing.T) {
	for _, p := range check.Catalog() {
		require.Regexpf(t, permissionRegex, p,
			"Catalog permission %q does not match %s", p, permissionRegex.String())
	}
}

// TestDrift_RPCMethodCount — sanity-check: общее число RPCEntries в map'е равно
// сумме методов всех gRPC-сервисов на ОБОИХ listener'ах (public + internal). Если
// refactor proto добавил/удалил RPC, оба теста (этот + EveryRPCMapped) ловят drift.
//
// Ожидание (после NLB CONTRACT — NLB service = 8 RPC, минус start/stop/attach/detach):
//
//	8 NLB + 6 Listener + 9 TG + 1 Quota + 2 Operation = 26 public + 1 internal
//	Subscribe + 2 internal Announce (Get/Report) = 29 entries.
func TestDrift_RPCMethodCount(t *testing.T) {
	got := len(check.PermissionMap())

	var expected int
	descs := allServiceDescs()
	for _, sd := range descs {
		expected += len(sd.Methods) + len(sd.Streams)
	}
	require.Equalf(t, expected, got,
		"PermissionMap has %d entries; sum of %d ServiceDesc methods = %d. "+
			"Either drift in proto-generated RPC set or PermissionMap, recheck.",
		got, len(descs), expected)
}

// TestExtract_AllRPCEntries — table-driven: для каждого non-Public RPCEntry
// прогоняем Extract с подходящим типизированным request и проверяем что:
//   - возвращённый objectType непустой;
//   - возвращённый objectID == ожидаемый "extracted-id";
//   - err == nil.
//
// Покрытие СВЕРЯЕТСЯ С САМОЙ КАРТОЙ, а не с длиной этой таблицы. Утверждение
// «покрыты все» обязано опираться на то, что покрывается, иначе оно измеряет
// собственный размер: пока сверка шла с захардкоженным числом, три внутренних
// извлечения (Subscribe + обе announce-ветки) не проверялись ни разу, а тест
// заявлял исчерпывающее покрытие и был зелёным.
//
// Сверка ДВУСТОРОННЯЯ: запись карты без строки в таблице — пробел покрытия;
// строка таблицы без записи в карте — тест, стреляющий по несуществующему RPC.
func TestExtract_AllRPCEntries(t *testing.T) {
	m := check.PermissionMap()

	type tc struct {
		fm       string
		req      any
		wantType string
		wantID   string
	}
	const id = "x-id"
	cases := []tc{
		// NLB
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Get",
			&lbv1.GetNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
			"nlb_network_load_balancer", id},
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Create",
			&lbv1.CreateNetworkLoadBalancerRequest{ProjectId: id},
			"project", id},
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Update",
			&lbv1.UpdateNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
			"nlb_network_load_balancer", id},
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Delete",
			&lbv1.DeleteNetworkLoadBalancerRequest{NetworkLoadBalancerId: id},
			"nlb_network_load_balancer", id},
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/Move",
			&lbv1.MoveNetworkLoadBalancerRequest{NetworkLoadBalancerId: id, DestinationProjectId: "p2"},
			"nlb_network_load_balancer", id},
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/GetTargetStates",
			&lbv1.GetTargetStatesRequest{NetworkLoadBalancerId: id},
			"nlb_network_load_balancer", id},
		{"/kacho.cloud.loadbalancer.v1.NetworkLoadBalancerService/ListOperations",
			&lbv1.ListNetworkLoadBalancerOperationsRequest{NetworkLoadBalancerId: id},
			"nlb_network_load_balancer", id},
		// Listener
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Get",
			&lbv1.GetListenerRequest{ListenerId: id},
			"nlb_listener", id},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Create",
			&lbv1.CreateListenerRequest{LoadBalancerId: id},
			"nlb_network_load_balancer", id},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Update",
			&lbv1.UpdateListenerRequest{ListenerId: id},
			"nlb_listener", id},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/Delete",
			&lbv1.DeleteListenerRequest{ListenerId: id},
			"nlb_listener", id},
		{"/kacho.cloud.loadbalancer.v1.ListenerService/ListOperations",
			&lbv1.ListListenerOperationsRequest{ListenerId: id},
			"nlb_listener", id},
		// TG
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Get",
			&lbv1.GetTargetGroupRequest{TargetGroupId: id},
			"nlb_target_group", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Create",
			&lbv1.CreateTargetGroupRequest{ProjectId: id},
			"project", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Update",
			&lbv1.UpdateTargetGroupRequest{TargetGroupId: id},
			"nlb_target_group", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Delete",
			&lbv1.DeleteTargetGroupRequest{TargetGroupId: id},
			"nlb_target_group", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/Move",
			&lbv1.MoveTargetGroupRequest{TargetGroupId: id, DestinationProjectId: "p2"},
			"nlb_target_group", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/AddTargets",
			&lbv1.AddTargetsRequest{TargetGroupId: id},
			"nlb_target_group", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/RemoveTargets",
			&lbv1.RemoveTargetsRequest{TargetGroupId: id},
			"nlb_target_group", id},
		{"/kacho.cloud.loadbalancer.v1.TargetGroupService/ListOperations",
			&lbv1.ListTargetGroupOperationsRequest{TargetGroupId: id},
			"nlb_target_group", id},
		// Чтение квот сужается ПРОЕКТОМ, а не объектом: строка учёта — свойство
		// проекта, а не объект с владельцем, поэтому сужать здесь нечего. Случай
		// пинит именно это: извлечение, начавшее резолвить что-то иное, сделало бы
		// числа арендатора видимыми по чужому вопросу.
		{"/kacho.cloud.loadbalancer.v1.QuotaService/List",
			&lbv1.ListQuotasRequest{ProjectId: id},
			"project", id},
		// Internal listener (:9091) — the SAME authz interceptor runs there, so
		// these entries decide access exactly as the public ones do.
		//
		// GetAnnounceState якорится на КЛАСТЕРЕ, а не на балансировщике, и это
		// намеренно: announce-state — инфра-данные, `v_get` на балансировщике
		// держит его владелец-тенант, а `system_viewer` на кластере — нет.
		// Рукописная карта якорила пообъектно и расходилась с каталогом; сторона
		// каталога выбрана по разбору «кто удовлетворяет отношение», а не «про
		// какой объект спрашивают».
		{"/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/GetAnnounceState",
			&lbv1.GetLoadBalancerAnnounceStateRequest{NetworkLoadBalancerId: id},
			"cluster", "cluster_root"},
		{"/kacho.cloud.loadbalancer.v1.InternalLoadBalancerAnnounceService/ReportAnnounceState",
			&lbv1.ReportLoadBalancerAnnounceStateRequest{NetworkLoadBalancerId: id},
			"nlb_network_load_balancer", id},
	}
	// Три top-level `List` в таблице отсутствуют намеренно: они несут полосу
	// сужения, у которой объекта нет by construction, поэтому извлекать нечего.
	// Сверка ниже идёт по полосе edge-checks и потому их не требует.

	// Reconcile the table against the map itself, in both directions.
	wantCovered := map[string]struct{}{}
	for fm, e := range m {
		// Только полоса edge-checks: у exempt и у полосы сужения экстрактора нет
		// by construction, и требовать для них случая значило бы требовать
		// проверку извлечения объекта, которого не существует.
		if !e.Public && !e.ScopeFiltered {
			wantCovered[fm] = struct{}{}
		}
	}
	covered := map[string]struct{}{}
	for _, c := range cases {
		require.NotContainsf(t, covered, c.fm, "duplicate table row for %q", c.fm)
		covered[c.fm] = struct{}{}
	}
	var missing, extra []string
	for fm := range wantCovered {
		if _, ok := covered[fm]; !ok {
			missing = append(missing, fm)
		}
	}
	for fm := range covered {
		if _, ok := wantCovered[fm]; !ok {
			extra = append(extra, fm)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	require.Emptyf(t, missing,
		"non-Public PermissionMap entries with no Extract case — their object extraction is unverified: %v", missing)
	require.Emptyf(t, extra,
		"Extract cases for methods that are not non-Public PermissionMap entries (renamed? made Public? removed?): %v", extra)
	// The count is reported, not asserted: it is a consequence of the reconciliation
	// above, and asserting it separately would only make the map harder to extend.
	t.Logf("Extract coverage: %d/%d non-Public PermissionMap entries", len(covered), len(wantCovered))

	for _, c := range cases {
		t.Run(c.fm, func(t *testing.T) {
			e, ok := m[c.fm]
			require.Truef(t, ok, "PermissionMap missing entry %q", c.fm)
			require.False(t, e.Public, "case %q is Public — should not be in this table", c.fm)
			require.NotNil(t, e.Extract)
			gotType, gotID, err := e.Extract(c.req)
			require.NoError(t, err)
			require.Equal(t, c.wantType, gotType)
			require.Equal(t, c.wantID, gotID)
		})
	}
}

// sortedKeys — helper для error-сообщений.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Sanity — чтобы fmt всегда был "использован" в этой test-сборке (drift_test
// помогает рендерить помощь — оставляем placeholder для будущих error
// сообщений).
var _ = fmt.Sprintf
