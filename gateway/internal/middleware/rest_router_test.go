// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rest_router_test.go — REST<->gRPC route resolution.
package middleware

import (
	"strings"
	"testing"
)

func TestRestRouter_Resolve_KnownRoutes(t *testing.T) {
	r := NewRestRouter()
	cases := []struct {
		method, path, wantFQN string
	}{
		// collection POST (Create)
		{"POST", "/iam/v1/accounts", "kaname.cloud.iam.v1.AccountService/Create"},
		// item GET with {id} placeholder
		{"GET", "/iam/v1/accounts/acc0000000000000001", "kaname.cloud.iam.v1.AccountService/Get"},
		// item PATCH (Update)
		{"PATCH", "/iam/v1/projects/prj0000000000000001", "kaname.cloud.iam.v1.ProjectService/Update"},
		// item DELETE
		{"DELETE", "/iam/v1/accounts/acc0000000000000001", "kaname.cloud.iam.v1.AccountService/Delete"},
		// suffix-action `:verb`
		{"POST", "/iam/v1/users:invite", "kaname.cloud.iam.v1.UserService/Invite"},
		// UserService.Update — public async mutation (User labels-only); PATCH on the
		// item path `/iam/v1/users/{user_id}`, same template as GET/DELETE. Must resolve
		// so the catalog gate (record_writer on iam_user, acr 1) fires instead of
		// "no entry for method".
		{"PATCH", "/iam/v1/users/usr0000000000000001", "kaname.cloud.iam.v1.UserService/Update"},
		// AccessBindingService.ListSubjectPrivileges — public read, GET suffix-action;
		// must resolve so the catalog gate (viewer floor) fires.
		{"GET", "/iam/v1/accessBindings:listSubjectPrivileges", "kaname.cloud.iam.v1.AccessBindingService/ListSubjectPrivileges"},
		// AccessBindingService.ListAssignableRoles — public read, GET suffix-action;
		// must resolve so the scope-polymorphic catalog gate (viewer floor + dynamic
		// object_type) fires.
		{"GET", "/iam/v1/accessBindings:listAssignableRoles", "kaname.cloud.iam.v1.AccessBindingService/ListAssignableRoles"},
		// ListByScope — public read, GET suffix-action on the collection; must
		// resolve so the scope-polymorphic catalog gate (viewer floor + dynamic
		// object_type from resource_type) fires.
		{"GET", "/iam/v1/accessBindings:listByScope", "kaname.cloud.iam.v1.AccessBindingService/ListByScope"},
		// ListByRole + ExpandAccess — public reads, GET suffix-actions on the
		// collection; must resolve so the cluster-scoped catalog gate (viewer floor,
		// acr 2) fires.
		{"GET", "/iam/v1/accessBindings:listByRole", "kaname.cloud.iam.v1.AccessBindingService/ListByRole"},
		{"GET", "/iam/v1/accessBindings:expandAccess", "kaname.cloud.iam.v1.AccessBindingService/ExpandAccess"},
		// AccessBindingService.Update — public PATCH on the item path (clears
		// deletion_protection); must resolve so the catalog gate (editor on
		// iam_access_binding, acr 2 — parity with Delete) fires instead of "no entry
		// for method". Same item template as GET/DELETE.
		{"PATCH", "/iam/v1/accessBindings/abd0000000000000001", "kaname.cloud.iam.v1.AccessBindingService/Update"},
		// WhoAmI (GET /iam/v1/me) must resolve so the <exempt> bypass fires;
		// otherwise path->FQN fails → 403 "catalog: no entry for method" breaks the
		// UI permission bootstrap.
		{"GET", "/iam/v1/me", "kaname.cloud.iam.v1.AuthorizeService/WhoAmI"},
		// PermissionCatalogService.ListPermissionCatalog — public read
		// (GET /iam/v1/permissionCatalog) on the EXTERNAL listener; must resolve so
		// the <exempt> catalog bypass fires. The internal-permissions route
		// (GET /iam/v1/internal/iam/permissions) must NOT resolve anymore
		// (see TestRestRouter_TombstonedRoutesGone).
		{"GET", "/iam/v1/permissionCatalog", "kaname.cloud.iam.v1.PermissionCatalogService/ListPermissionCatalog"},
		// list with query string is stripped before matching
		{"GET", "/iam/v1/projects?accountId=acc1", "kaname.cloud.iam.v1.ProjectService/List"},
		// vpc resource
		{"GET", "/vpc/v1/networks/enp0000000000000001", "kacho.cloud.vpc.v1.NetworkService/Get"},
		// Суффиксные действия над CIDR-блоками пула адресов.
		//
		// ЭТУ ПАРУ ОБЪЯВЛЯЮТ ДВА СЕРВИСА, и ожидание сменилось с внутреннего FQN на
		// публичный не подгонкой, а вслед за предметом. Административная поверхность
		// пула опубликована (ADM-1 S1) на ТОМ ЖЕ каноническом пути — второго адреса
		// у ресурса быть не должно, — а внутренний глагол живёт рядом до стадии S3.
		// Разрешение возвращает первое совпадение, и порядок таблицы теперь тотален
		// (шаблон, метод, FQN), поэтому публичный идёт раньше внутреннего.
		//
		// ЧТО ЭТО МЕНЯЕТ ДЛЯ РЕШЕНИЯ О ДОСТУПЕ — НИЧЕГО, и это не совпадение, а
		// охраняемое свойство: обе записи каталога требуют `system_admin` @
		// `cluster` при одном и том же подтверждении личности. Разойдись они хоть в
		// одном поле — покраснеет `TestSharedRestPair_CannotChangeAnAccessDecision`,
		// который для того и заведён.
		{"POST", "/vpc/v1/addressPools/apl0000000000000001:addCidrBlocks", "kacho.cloud.vpc.v1.AddressPoolService/AddCidrBlocks"},
		{"POST", "/vpc/v1/addressPools/apl0000000000000001:removeCidrBlocks", "kacho.cloud.vpc.v1.AddressPoolService/RemoveCidrBlocks"},
	}
	for _, c := range cases {
		fqn, ok := r.Resolve(c.method, c.path)
		if !ok {
			t.Errorf("Resolve(%s %s): no match, want %s", c.method, c.path, c.wantFQN)
			continue
		}
		if fqn != c.wantFQN {
			t.Errorf("Resolve(%s %s) = %s, want %s", c.method, c.path, fqn, c.wantFQN)
		}
	}
}

func TestRestRouter_Resolve_UnknownRoute(t *testing.T) {
	r := NewRestRouter()
	if fqn, ok := r.Resolve("GET", "/no/such/route"); ok {
		t.Errorf("Resolve(unknown) = %s, want no match", fqn)
	}
	// Wrong method for an existing path.
	if _, ok := r.Resolve("DELETE", "/iam/v1/accounts"); ok {
		t.Errorf("Resolve(DELETE /iam/v1/accounts): want no match (collection has no DELETE)")
	}
}

// TestRestRouter_TombstonedRoutesGone — the tombstoned RPC must no longer
// resolve to a route: InternalIAMService.ListPermissions
// (GET /iam/v1/internal/iam/permissions), replaced by the public
// PermissionCatalogService.ListPermissionCatalog.
//
// A second FQN stood here — InternalAuthorizeService.RunRegoTest. Its guard is
// gone because its SERVICE is gone: InternalAuthorizeService administered the
// external authorization engine and was removed whole with it, so no route can
// name that method any more and the guard had nothing left to guard.
//
// If the route table is not regenerated after the tombstone, the stale
// /iam/v1/internal/iam/permissions entry keeps resolving → this test fails,
// signalling the sync was skipped.
func TestRestRouter_TombstonedRoutesGone(t *testing.T) {
	r := NewRestRouter()
	if fqn, ok := r.Resolve("GET", "/iam/v1/internal/iam/permissions"); ok {
		t.Errorf("Resolve(GET /iam/v1/internal/iam/permissions) = %s, want no match — ListPermissions tombstoned in proto-G", fqn)
	}
	// No route may still map to the tombstoned FQN.
	for _, rt := range generatedRestRoutes {
		if rt.FQN == "kaname.cloud.iam.v1.InternalIAMService/ListPermissions" {
			t.Errorf("route table still contains tombstoned FQN %q (template %q)", rt.FQN, rt.Template)
		}
	}
}

func TestRestRouter_PathTemplates(t *testing.T) {
	r := NewRestRouter()
	tmpls := r.PathTemplates()
	if got := tmpls["kaname.cloud.iam.v1.AccountService/Get"]; got != "/iam/v1/accounts/{account_id}" {
		t.Errorf("PathTemplates[AccountService/Get] = %q, want /iam/v1/accounts/{account_id}", got)
	}
}

// TestRestRouter_Resolve_RegeneratedCoverage — guard against the route-table
// drifting behind the proto tree. When a domain adds a REST-exposed RPC but the
// route table is not regenerated, the path fails to resolve → the authz gate
// returns "catalog: no entry for method" (403) even though the method is
// legitimate. These routes existed only in proto until the table was
// regenerated from `google.api.http`; asserting them here fails loudly if a
// future edit hand-maintains the table and re-introduces the lag.
func TestRestRouter_Resolve_RegeneratedCoverage(t *testing.T) {
	r := NewRestRouter()
	cases := []struct {
		method, path, wantFQN string
	}{
		// VPC RouteTable mutation verbs (kebab-case suffix-actions).
		// VPC InternalNetworkService read (internal-only `:internal` action).
		{"GET", "/vpc/v1/networks/enp0000000000000001:internal", "kacho.cloud.vpc.v1.InternalNetworkService/GetNetwork"},
		// IAM SAKeyService — service-account key subpaths under the SA item.
		{"POST", "/iam/v1/serviceAccounts/sva0000000000000001/keys", "kaname.cloud.iam.v1.SAKeyService/Issue"},
		{"GET", "/iam/v1/serviceAccounts/sva0000000000000001/keys", "kaname.cloud.iam.v1.SAKeyService/List"},
		{"DELETE", "/iam/v1/serviceAccounts/sva0000000000000001/keys/key0000000000000001", "kaname.cloud.iam.v1.SAKeyService/Revoke"},
		// IAM UserTokenService — personal API-token subpaths under the user item
		// (mirrors SAKeyService, parent-scoped on iam_user).
		{"POST", "/iam/v1/users/usr0000000000000001/tokens", "kaname.cloud.iam.v1.UserTokenService/Issue"},
		{"GET", "/iam/v1/users/usr0000000000000001/tokens", "kaname.cloud.iam.v1.UserTokenService/List"},
		{"DELETE", "/iam/v1/users/usr0000000000000001/tokens/uoc0000000000000001", "kaname.cloud.iam.v1.UserTokenService/Revoke"},
		// IAM AccessBindingService scope-polymorphic reads (GET suffix-actions).
		{"GET", "/iam/v1/accessBindings:listByScope", "kaname.cloud.iam.v1.AccessBindingService/ListByScope"},
		{"GET", "/iam/v1/accessBindings:listBySubject", "kaname.cloud.iam.v1.AccessBindingService/ListBySubject"},
		// Registry — first REST-exposed registry resource.
		{"POST", "/registry/v1/registries", "kacho.cloud.registry.v1.RegistryService/Create"},
		{"DELETE", "/registry/v1/registries/reg0000000000000001/repositories/app/tags/v1", "kacho.cloud.registry.v1.RegistryService/DeleteTag"},
	}
	for _, c := range cases {
		fqn, ok := r.Resolve(c.method, c.path)
		if !ok {
			t.Errorf("Resolve(%s %s): no match, want %s — route table stale (regenerate scripts/gen-rest-route-table.sh)", c.method, c.path, c.wantFQN)
			continue
		}
		if fqn != c.wantFQN {
			t.Errorf("Resolve(%s %s) = %s, want %s", c.method, c.path, fqn, c.wantFQN)
		}
	}

	// Services deleted from proto must not leave dangling routes. If the table
	// is regenerated from the current tree, no route may resolve to them.
	for _, rt := range generatedRestRoutes {
		switch {
		case strings.HasPrefix(rt.FQN, "kaname.cloud.iam.v1.TrustPolicyService/"),
			strings.HasPrefix(rt.FQN, "kaname.cloud.iam.v1.OpaBundleService/"),
			strings.HasPrefix(rt.FQN, "kaname.cloud.iam.v1.FederationExchangeService/"):
			t.Errorf("route table still contains %q — service removed from proto, stale entry", rt.FQN)
		}
	}
}

func TestMatchTemplate(t *testing.T) {
	cases := []struct {
		tmpl, path string
		want       bool
	}{
		{"/iam/v1/accounts", "/iam/v1/accounts", true},
		{"/iam/v1/accounts/{account_id}", "/iam/v1/accounts/acc1", true},
		{"/iam/v1/accounts/{account_id}", "/iam/v1/accounts", false},
		{"/iam/v1/accounts/{account_id}", "/iam/v1/accounts/acc1/extra", false},
		{"/iam/v1/users:invite", "/iam/v1/users:invite", true},
		{"/iam/v1/users:invite", "/iam/v1/users", false},
		{"/vpc/v1/networks/{network_id}", "/vpc/v1/networks/enp1", true},
	}
	for _, c := range cases {
		if got := matchTemplate(c.tmpl, c.path); got != c.want {
			t.Errorf("matchTemplate(%q, %q) = %v, want %v", c.tmpl, c.path, got, c.want)
		}
	}
}

// TestRestRouter_Resolve_DeepWildcardRepository — regression lock for the authz
// middleware deep-wildcard bug (#64 follow-up). A `{field=**}` template must match
// a MULTI-segment value (e.g. repository "backend/api"), NOT just one segment. The
// old matchTemplate required len(tparts)==len(pparts) and treated `{repo=**}` as a
// single placeholder, so `GET …/repositories/backend/api` failed to resolve →
// resolveRestFQN fell back to the raw path → "catalog: no entry for method" →
// AUTHZ_DENIED for every multi-segment repository RPC. (Exposed once #64 Defect A/B +
// geo unblocked the repository surface; the deep-wildcard resolution never fired on a
// multi-segment repo before.)
func TestRestRouter_Resolve_DeepWildcardRepository(t *testing.T) {
	r := NewRestRouter()
	const R = "/registry/v1/registries/reg-1/repositories"
	cases := []struct{ method, path, wantFQN string }{
		// single-segment repo (already worked)
		{"GET", R + "/web", "kacho.cloud.registry.v1.RegistryService/GetRepository"},
		// MULTI-segment repo — the bug
		{"GET", R + "/backend/api", "kacho.cloud.registry.v1.RegistryService/GetRepository"},
		{"PATCH", R + "/backend/api", "kacho.cloud.registry.v1.RegistryService/UpdateRepository"},
		{"DELETE", R + "/backend/api", "kacho.cloud.registry.v1.RegistryService/DeleteRepository"},
		{"GET", R + "/backend/api/referrers", "kacho.cloud.registry.v1.RegistryService/ListReferrers"},
		{"POST", R + "/backend/api:rename", "kacho.cloud.registry.v1.RegistryService/RenameRepository"},
		// deeper (3-segment) repo
		{"GET", R + "/team/backend/api", "kacho.cloud.registry.v1.RegistryService/GetRepository"},
	}
	for _, c := range cases {
		fqn, ok := r.Resolve(c.method, c.path)
		if !ok {
			t.Errorf("Resolve(%s %s): no match, want %s", c.method, c.path, c.wantFQN)
			continue
		}
		if fqn != c.wantFQN {
			t.Errorf("Resolve(%s %s) = %s, want %s", c.method, c.path, fqn, c.wantFQN)
		}
	}
	// `**` must still require ≥1 segment: the bare `…/repositories` GET is ListRepositories, not GetRepository.
	if fqn, ok := r.Resolve("GET", R); !ok || fqn != "kacho.cloud.registry.v1.RegistryService/ListRepositories" {
		t.Errorf("Resolve(GET %s) = %q,%v; want ListRepositories — deep wildcard must not swallow the empty tail", R, fqn, ok)
	}
}
