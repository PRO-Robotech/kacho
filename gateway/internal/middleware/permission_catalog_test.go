// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestPermissionCatalog_LoadFromBytes_ArrayShape(t *testing.T) {
	raw := []byte(`[
		{
			"fqn": "kacho.cloud.vpc.v1.NetworkService/Create",
			"permission": "vpc.networks.create",
			"required_relation": "editor",
			"scope_extractor": {"object_type": "project", "from_request_field": "project_id"},
			"required_acr_min": "2"
		}
	]`)
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(raw))

	entry, ok := c.Lookup("kacho.cloud.vpc.v1.NetworkService/Create")
	require.True(t, ok)
	assert.Equal(t, "vpc.networks.create", entry.Permission)
	assert.Equal(t, "editor", entry.RequiredRelation)
	assert.Equal(t, "project", entry.ScopeExtractor.ObjectType)
	assert.Equal(t, "project_id", entry.ScopeExtractor.FromRequestField)
	assert.Equal(t, "2", entry.RequiredACRMin)
}

func TestPermissionCatalog_LoadFromBytes_ObjectShape(t *testing.T) {
	raw := []byte(`{
		"entries": [
			{
				"fqn": "kaname.cloud.iam.v1.AuthorizeService/Check",
				"permission": "iam.authorize.check",
				"required_relation": "viewer",
				"risk_level": "MEDIUM"
			}
		],
		"critical": {"permissions": ["audit.RewindMerkle"]}
	}`)
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(raw))

	entry, ok := c.Lookup("kaname.cloud.iam.v1.AuthorizeService/Check")
	require.True(t, ok)
	assert.Equal(t, "iam.authorize.check", entry.Permission)
	assert.Equal(t, "MEDIUM", entry.RiskLevel)
}

func TestPermissionCatalog_LoadFromBytes_EmptyError(t *testing.T) {
	c := middleware.NewPermissionCatalog()
	err := c.LoadFromBytes([]byte{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestPermissionCatalog_LoadFromBytes_DuplicateError(t *testing.T) {
	raw := []byte(`[
		{"fqn": "X/Y", "permission": "a.b.c"},
		{"fqn": "X/Y", "permission": "x.y.z"}
	]`)
	c := middleware.NewPermissionCatalog()
	err := c.LoadFromBytes(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestPermissionCatalog_LoadFromBytes_MissingFQNError(t *testing.T) {
	raw := []byte(`[{"permission": "a.b.c"}]`)
	c := middleware.NewPermissionCatalog()
	err := c.LoadFromBytes(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty fqn")
}

func TestPermissionCatalog_LookupMiss(t *testing.T) {
	raw := []byte(`[{"fqn": "X/Y", "permission": "a.b.c"}]`)
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(raw))
	_, ok := c.Lookup("nope/nope")
	assert.False(t, ok)
}

func TestPermissionCatalog_EmbeddedAsset_Loads(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	// The embedded asset is the full per-RPC permission catalog: every RPC is
	// annotated, so it carries ~264 entries and EVERY entry must be classified
	// (no empty permission).
	assert.GreaterOrEqual(t, c.Size(), 240)
	for _, fqn := range c.FQNs() {
		e, _ := c.Lookup(fqn)
		assert.NotEmpty(t, e.Permission, "catalog entry %s has empty permission", fqn)
	}

	// Spot-check a known-populated entry from the catalog.
	entry, ok := c.Lookup("kacho.cloud.vpc.v1.NetworkService/Create")
	require.True(t, ok)
	assert.Equal(t, "vpc.networks.create", entry.Permission)
	assert.Equal(t, "editor", entry.RequiredRelation)
}

// TestPermissionCatalog_RegistryV1Present_EntryFloor — hermetic regression
// guard for the stale-permission-catalog prod bug: a stale
// `make sync-permission-catalog` shipped an embedded catalog that had dropped
// the registry.v1 RPCs, so those methods hit "no entry for method" → denied.
// The generic embed test's floor (>=240) sits well below the buggy value and
// therefore cannot catch a re-regression. This test pins the actual floor
// (330 entries) AND asserts registry.v1 RPCs are present, so any drop of the
// registry surface or a shrink of the catalog fails CI without Postgres.
//
// The floor tracks DELIBERATE surface removals — it is re-pinned to the exact
// regenerated size, never loosened to make a run pass. 434 → 398 when the
// stillborn DiskPlacementGroup / Filesystem / SnapshotSchedule contracts went;
// 333 → 330 when the compute MaintenanceService was withdrawn — three REST routes
// and three catalog entries in front of a service implemented nowhere, so every
// one of those paths answered 404. The messages it shares with Instance stay:
// Instance really does carry a maintenance policy. Before that, 334 → 333 when
// iam GetJWKSStatus was withdrawn — a read RPC reporting on a key
// table migration 0065 dropped, so it could only ever answer an empty set, and no
// caller anywhere asked it. Before that, 357 → 334 when the compute InstanceGroup
// service was withdrawn — declared in
// proto, routed at the edge, implemented nowhere, and naming another cloud in its
// own field names. Before that, 398 → 357 when the born-dead GpuCluster / HostGroup / PlacementGroup /
// ReservedInstancePool / HostType contracts went with the fga-model drift-gate
// restoration (41 RPCs that were never served on any listener and had no type in
// the enforced authorization model). Authority for "is the catalog correct" is
// `make permission-catalog-check` (byte-diff against a fresh generation from
// proto); this floor is the hermetic backstop against a silent shrink.
func TestPermissionCatalog_RegistryV1Present_EntryFloor(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	// Floor lowered 330 → 296 when compute's duplicate block storage was retired
	// (34 entries: Disk 10 + Image 10 + Snapshot 9 + DiskType 2 + InternalDiskType 3),
	// then 296 → 290 when the tenant-facing condition surface went (6 entries:
	// ConditionsService Get/List/Create/Update/Delete/Evaluate),
	// then 290 → 285 when four services declared without a single implementation were
	// retired (5 entries: compute.v1 + vpc.v1 InternalResourceLifecycleService/Subscribe,
	// vpc.v1 InternalWatchService/Watch, iam.v1 InternalIamHooksService/{TokenHook,
	// RefreshTokenHook}) — none of the four was mounted on any listener.
	// A floor is a guard against a SILENT shrink; a deliberate retire moves it, and
	// only by the number of entries the retire actually removed.
	assert.GreaterOrEqual(t, c.Size(), 285,
		"embedded catalog shrank below the known floor — stale `make sync-permission-catalog`?")

	// registry.v1 methods MUST be present (the regressed surface).
	for _, want := range []struct{ fqn, perm string }{
		// Строка перестала быть `<exempt>` и назвала полосу сужения: реестр
		// действительно сужает страницу пообъектно в хендлере, а `<exempt>` этого не
		// выражал — он говорил лишь «край не спрашивает», ничего не обещая о том,
		// спрашивает ли кто-нибудь.
		{"kacho.cloud.registry.v1.RegistryService/List", "registry.registries.list"},
		{"kacho.cloud.registry.v1.RegistryService/Get", "registry.registries.get"},
		{"kacho.cloud.registry.v1.RegistryService/Create", "registry.registries.create"},
		{"kacho.cloud.registry.v1.RegistryService/Delete", "registry.registries.delete"},
	} {
		entry, ok := c.Lookup(want.fqn)
		require.True(t, ok, "registry.v1 RPC missing from embedded catalog (stale sync?): %s", want.fqn)
		assert.Equal(t, want.perm, entry.Permission, "permission drift on %s", want.fqn)
	}
}

func TestPermissionCatalog_LoadFromFile_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	require.NoError(t, os.WriteFile(path,
		[]byte(`[{"fqn":"A/X","permission":"a.x.c"}]`), 0o600))

	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromFile(path))
	assert.Equal(t, 1, c.Size())

	// Modify file, reload.
	require.NoError(t, os.WriteFile(path,
		[]byte(`[
			{"fqn":"A/X","permission":"a.x.c"},
			{"fqn":"B/Y","permission":"b.y.d"}
		]`), 0o600))
	require.NoError(t, c.Reload())
	assert.Equal(t, 2, c.Size())
}

func TestPermissionCatalog_LoadFromFile_Missing(t *testing.T) {
	c := middleware.NewPermissionCatalog()
	err := c.LoadFromFile("/no/such/file.json")
	require.Error(t, err)
}

func TestPermissionCatalog_Reload_NoPrevious(t *testing.T) {
	c := middleware.NewPermissionCatalog()
	err := c.Reload()
	require.Error(t, err)
}

func TestPermissionCatalog_FQNs_Sorted(t *testing.T) {
	raw := []byte(`[
		{"fqn":"Z/Y", "permission":"z.y.c"},
		{"fqn":"A/X", "permission":"a.x.c"},
		{"fqn":"M/N", "permission":"m.n.c"}
	]`)
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(raw))
	got := c.FQNs()
	require.Equal(t, []string{"A/X", "M/N", "Z/Y"}, got)
}

func TestPermissionCatalog_IsExempt(t *testing.T) {
	raw := []byte(`[
		{"fqn":"A/X", "permission":"<exempt>"},
		{"fqn":"B/Y", "permission":"vpc.networks.get"}
	]`)
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(raw))
	ex, _ := c.Lookup("A/X")
	assert.True(t, ex.IsExempt())
	ne, _ := c.Lookup("B/Y")
	assert.False(t, ne.IsExempt())
}

func TestPermissionCatalog_EmbedBytes_Stable(t *testing.T) {
	b := middleware.EmbeddedPermissionCatalogJSON()
	require.NotEmpty(t, b)
	// Ensure returned slice is a copy — mutating it must not affect future calls.
	b[0] = '!'
	b2 := middleware.EmbeddedPermissionCatalogJSON()
	assert.NotEqual(t, b[0], b2[0])
}

func TestPermissionCatalog_ReloadAfterParseError_Preserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	require.NoError(t, os.WriteFile(path,
		[]byte(`[{"fqn":"A/X","permission":"a.x.c"}]`), 0o600))

	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromFile(path))
	require.Equal(t, 1, c.Size())

	// Corrupt the file: invalid JSON.
	require.NoError(t, os.WriteFile(path, []byte(`not json {{`), 0o600))
	err := c.Reload()
	require.Error(t, err)
	// Previous good state preserved.
	assert.Equal(t, 1, c.Size())
	entry, ok := c.Lookup("A/X")
	require.True(t, ok)
	assert.Equal(t, "a.x.c", entry.Permission)
}

func TestPermissionCatalog_LookupKnownEntries_FromEmbed(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	for _, want := range []struct {
		fqn     string
		perm    string
		scopeF  string
		scopeOT string
	}{
		// AuthorizeService/Check carries NO edge scope — see the dedicated case
		// below. What must never come back is a scope derived from the request
		// `subject` (the query target, not the reader), which would re-derive the
		// FGA check from tenant-controlled input.
		{"kaname.cloud.iam.v1.AuthorizeService/BatchCheck", "iam.authorize.batchCheck", "scope_id", "project"},
	} {
		t.Run(want.fqn, func(t *testing.T) {
			entry, ok := c.Lookup(want.fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", want.fqn)
			assert.Equal(t, want.perm, entry.Permission)
			assert.Equal(t, want.scopeF, entry.ScopeExtractor.FromRequestField)
			assert.Equal(t, want.scopeOT, entry.ScopeExtractor.ObjectType)
		})
	}

	// The three AuthorizeService reads that answer "may X do Y to Z" and
	// "who can reach Z" declare the scope-filtered lane: the subject arrives
	// as an ARN and the resource as a nested ref carrying its own type, so the edge
	// can build no object at all. kaname decides — self-query, cluster
	// administrator, or `admin` on the resource actually named.
	//
	// A fourth read used to stand here — "what can X reach". It is gone from the
	// contract: it enumerated the universe instead of narrowing a page, and it was
	// not paginated by construction.
	//
	// They used to declare `viewer` on the cluster singleton, which the bootstrap
	// grants to `user:*` for the global reference catalogue: a check that admitted
	// every authenticated subject on the surface that describes who can reach what
	// across the platform.
	for _, fqn := range []string{
		"kaname.cloud.iam.v1.AuthorizeService/Check",
		"kaname.cloud.iam.v1.AuthorizeService/ListSubjects",
		"kaname.cloud.iam.v1.AuthorizeService/ExpandRelations",
	} {
		t.Run(fqn+"/scope-filtered", func(t *testing.T) {
			entry, ok := c.Lookup(fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", fqn)
			assert.True(t, entry.ScopeFiltered)
			assert.False(t, entry.IsExempt(), "an authenticated principal is still required")
			assert.Empty(t, entry.RequiredRelation,
				"no relation is checked at the edge — naming one that is not checked is the defect")
			assert.Empty(t, entry.ScopeExtractor.ObjectType)
			assert.Empty(t, entry.ScopeExtractor.FromRequestField,
				"no scope may be derived from tenant-controlled request input")
		})
	}
}

// TestPermissionCatalog_ListAssignableRoles_ScopePolymorphic — the embedded
// catalog MUST carry AccessBindingService/ListAssignableRoles as a
// scope-polymorphic viewer-floor entry, exactly like ListByScope: the FGA
// object type is derived from the request `resource_type` field (the
// `object_type_from_request_field` directive), not the static `object_type`.
// Without `object_type_from_request_field`, an account/cluster-scoped grant
// palette read would be checked as `project:<id>` → 403.
func TestPermissionCatalog_ListAssignableRoles_ScopePolymorphic(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	entry, ok := c.Lookup("kaname.cloud.iam.v1.AccessBindingService/ListAssignableRoles")
	require.True(t, ok, "ListAssignableRoles missing from embedded catalog")
	assert.Equal(t, "iam.access_bindings_by_resources.listAssignableRoles", entry.Permission)
	assert.Equal(t, "viewer", entry.RequiredRelation,
		"catalog floor must be viewer (handler requireGrantAuthority is the precise gate, D-5)")
	assert.Equal(t, "project", entry.ScopeExtractor.ObjectType,
		"static object_type is the fallback (parity with ListByScope)")
	assert.Equal(t, "resource_id", entry.ScopeExtractor.FromRequestField)
	assert.Equal(t, "resource_type", entry.ScopeExtractor.ObjectTypeFromRequestField,
		"object_type must be derived from request resource_type (scope-polymorphic, Bug A)")
	assert.Equal(t, "1", entry.RequiredACRMin,
		"ListAssignableRoles is a READ → routine AAL1 floor (SEC-acr-stepup-refinement, was 2)")
}

// TestPermissionCatalog_ListByScope_ScopePolymorphic — the embedded catalog
// MUST carry ListByScope as a scope-polymorphic viewer-floor entry: the FGA
// object type is derived from the request `resource_type` field via
// `object_type_from_request_field`, with static `project` only as fallback.
// The permission key matches in lockstep (…listByScope).
func TestPermissionCatalog_ListByScope_ScopePolymorphic(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	entry, ok := c.Lookup("kaname.cloud.iam.v1.AccessBindingService/ListByScope")
	require.True(t, ok, "ListByScope missing from embedded catalog (RBAC rules-model F)")
	assert.Equal(t, "iam.access_bindings_by_resources.listByScope", entry.Permission)
	assert.Equal(t, "viewer", entry.RequiredRelation,
		"catalog floor must be viewer (handler is the precise gate)")
	assert.Equal(t, "project", entry.ScopeExtractor.ObjectType,
		"static object_type is the fallback (scope-polymorphic)")
	assert.Equal(t, "resource_id", entry.ScopeExtractor.FromRequestField)
	assert.Equal(t, "resource_type", entry.ScopeExtractor.ObjectTypeFromRequestField,
		"object_type must be derived from request resource_type (scope-polymorphic, Bug A)")
	assert.Equal(t, "1", entry.RequiredACRMin,
		"ListByScope is a READ → routine AAL1 floor (SEC-acr-stepup-refinement, was 2)")

	// The removed target/selector RPCs must NOT be present.
	for _, gone := range []string{
		"kaname.cloud.iam.v1.AccessBindingService/AddTargetResources",
		"kaname.cloud.iam.v1.AccessBindingService/RemoveTargetResources",
		"kaname.cloud.iam.v1.AccessBindingService/ReplaceTargetSelector",
		"kaname.cloud.iam.v1.AccessBindingService/ListGrantableResources",
		"kaname.cloud.iam.v1.AccessBindingService/ListByResource",
	} {
		_, present := c.Lookup(gone)
		assert.False(t, present, "removed/renamed RPC must NOT be in catalog: %s", gone)
	}
}

// TestPermissionCatalog_AccessBindingUpdate_VerbBearing — AccessBindingService.Update
// is an object-self mutation on the `iam_access_binding` object. Enforcement
// references the verb-bearing relation `v_update` (not the coarse `editor`
// tier): the gate resolves on the verb, so a v_update-grant satisfies it while
// a v_list/v_get grant does not (see-in-selector-without-content). Object/field
// scope is unchanged: object `iam_access_binding` resolved from
// `access_binding_id`, acr floor 2. Without this embedded entry the per-RPC
// authz middleware has "no entry for method" → the PATCH is denied (catalog miss).
func TestPermissionCatalog_AccessBindingUpdate_VerbBearing(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	entry, ok := c.Lookup("kaname.cloud.iam.v1.AccessBindingService/Update")
	require.True(t, ok, "AccessBindingService/Update missing from embedded catalog")
	assert.Equal(t, "iam.access_bindings.update", entry.Permission)
	assert.Equal(t, "v_update", entry.RequiredRelation, "Update is an object-self mutation — verb-bearing v_update (Design B), not editor tier")
	assert.Equal(t, "iam_access_binding", entry.ScopeExtractor.ObjectType)
	assert.Equal(t, "access_binding_id", entry.ScopeExtractor.FromRequestField)
	assert.Equal(t, "2", entry.RequiredACRMin, "anti-anon ACR floor")
}

// TestPermissionCatalog_InternalClusterService_LockedSystemAdmin —
// regression guard: every RPC of `InternalClusterService` must be gated by
// the FGA relation `system_admin` on `cluster:<cluster-singleton>` in the
// embedded catalog. Non-admin callers MUST NOT be able to even observe
// these RPCs — `Get` / `ListAdmins` would otherwise leak the existence and
// roster of cluster admins. Regressing any of these entries to `<exempt>` /
// `viewer` / non-`cluster` scope would re-open the leak.
func TestPermissionCatalog_InternalClusterService_LockedSystemAdmin(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	want := []struct {
		fqn  string
		perm string
	}{
		{"kaname.cloud.iam.v1.InternalClusterService/Get", "iam.cluster_admins.get"},
		{"kaname.cloud.iam.v1.InternalClusterService/ListAdmins", "iam.cluster_admins.list"},
		{"kaname.cloud.iam.v1.InternalClusterService/GrantAdmin", "iam.cluster_admins.grant"},
		{"kaname.cloud.iam.v1.InternalClusterService/RevokeAdmin", "iam.cluster_admins.revoke"},
	}

	for _, w := range want {
		t.Run(w.fqn, func(t *testing.T) {
			entry, ok := c.Lookup(w.fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", w.fqn)
			assert.False(t, entry.IsExempt(),
				"InternalClusterService.%s must NOT be <exempt> — non-admins would observe cluster-admin roster",
				w.fqn)
			assert.Equal(t, w.perm, entry.Permission,
				"permission identifier drift on %s", w.fqn)
			assert.Equal(t, "system_admin", entry.RequiredRelation,
				"required_relation must be system_admin on %s (acceptance D-11, item-2b)", w.fqn)
			assert.Equal(t, "cluster", entry.ScopeExtractor.ObjectType,
				"scope object_type must be cluster on %s", w.fqn)
			assert.Equal(t, "*", entry.ScopeExtractor.FromRequestField,
				"scope from_request_field must be '*' (cluster singleton) on %s", w.fqn)
		})
	}
}

// TestPermissionCatalog_ListPermissionCatalog_ExemptAndTombstones — the
// embedded catalog MUST:
//   - carry PermissionCatalogService.ListPermissionCatalog as an
//     authenticated-floor read that STANDS ON A PRODUCED RELATION (#893/#895) —
//     `viewer` on the cluster singleton, satisfied for every authenticated caller
//     by the wildcard subject of a SYSTEM GRANT. The floor is the same as before;
//     what changed is that the access is now visible on the grant surface and
//     revocable there. The `<exempt>` lane gave the same floor and left the access
//     invisible to any listing and closable only by a release;
//   - NOT carry the two tombstoned RPCs InternalIAMService.ListPermissions
//     and InternalAuthorizeService.RunRegoTest.
//
// The embedded catalog is kept in sync with proto-gen via
// `make sync-permission-catalog`.
func TestPermissionCatalog_ListPermissionCatalog_ExemptAndTombstones(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	entry, ok := c.Lookup("kaname.cloud.iam.v1.PermissionCatalogService/ListPermissionCatalog")
	require.True(t, ok, "ListPermissionCatalog missing from embedded catalog (resync not run?)")
	assert.False(t, entry.IsExempt(),
		"ListPermissionCatalog must stand on a produced relation, not on the exempt lane")
	assert.Equal(t, "viewer", entry.RequiredRelation,
		"the floor is the relation a system grant produces for every authenticated caller")
	assert.Equal(t, "cluster", entry.ScopeExtractor.ObjectType,
		"the platform permission dictionary is anchored on the cluster singleton")

	for _, gone := range []string{
		"kaname.cloud.iam.v1.InternalIAMService/ListPermissions",
		"kaname.cloud.iam.v1.InternalAuthorizeService/RunRegoTest",
	} {
		_, present := c.Lookup(gone)
		assert.False(t, present, "tombstoned RPC must NOT be in embedded catalog (proto-G): %s", gone)
	}
}

// TestPermissionCatalog_VBC22_VerbBearingFlip — the embedded catalog must
// mirror the verb-bearing proto-gen source: object-self get/list/update/delete
// RPCs are gated by the verb-bearing relations v_get/v_list/v_update/v_delete,
// while create-child stays `editor` on the parent (create on a not-yet-existing
// object is meaningless), Internal.* admin RPCs stay `system_admin` (no
// surface/relation downgrade on cluster-internal admin methods), and exempt
// RPCs stay exempt. The embedded catalog is kept in sync with proto-gen via
// `make sync-permission-catalog`.
//
// The dividing line is the SCOPE, not the verb in the method name. A method scoped
// on the object itself takes the object's verb; a method scoped on the parent
// project takes a project TIER — `editor` to create into it, `viewer` to list what
// is in it. Reading the line off the verb alone is how seven top-level List RPCs
// came to demand the project's own `v_list`, which the model defines as access to
// the project object and explicitly "never to its contents".
//
// # What a table of names can and cannot hold
//
// Every row here is a name someone wrote down, so the table proves the convention
// only for the domains it happens to name. It named storage once — VolumeService/
// List, a row that is TRUE and about the tier half of the rule — while the whole
// object-self surface of that domain sat on tiers, out of reach of the form. The
// storage rows below close that instance; the CLASS is held elsewhere, by a probe
// that derives its population from the catalog itself and asks the emitter what a
// one-verb grant actually resolves:
// services/iam/internal/apps/kaname/api/access_binding/reconcile,
// TestCreateOnlyGrantOpensNoObjectSelfRPC. A new domain landing on tiers is caught
// there without anyone remembering to add a row here.
func TestPermissionCatalog_VBC22_VerbBearingFlip(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	cases := []struct {
		fqn      string
		relation string
		note     string
	}{
		// object-self reads → v_get / v_list
		{"kaname.cloud.iam.v1.UserService/Get", "v_get", "object-self get"},
		{"kacho.cloud.vpc.v1.NetworkService/Get", "v_get", "object-self get"},
		{"kacho.cloud.compute.v1.InstanceService/Get", "v_get", "object-self get"},
		// Object-self LIST means "list what hangs off THIS object", so its scope is the
		// object (compute_instance/instance_id). This row used to name
		// InstanceService/List — a top-level list scoped on project/project_id, i.e. not
		// object-self at all — and so asserted the verb-bearing rule about a method
		// outside its reach. ListOperations is the real instance of the rule.
		{"kacho.cloud.compute.v1.InstanceService/ListOperations", "v_list", "object-self list"},
		{"kacho.cloud.vpc.v1.SubnetService/ListOperations", "v_list", "object-self list"},
		{"kacho.cloud.storage.v1.VolumeService/ListOperations", "v_list", "object-self list"},
		{"kacho.cloud.storage.v1.ImageService/ListOperations", "v_list", "object-self list"},
		// Top-level project list → READ TIER on the parent project, the same axis
		// create-child takes with `editor`. The project's own `v_list` is object-level
		// access to the PROJECT ITSELF, never to its contents, so it is not the question
		// this method asks; rows are narrowed per object by the owning service's filter.
		{"kacho.cloud.compute.v1.InstanceService/List", "viewer", "top-level project list → read tier"},
		{"kacho.cloud.vpc.v1.SubnetService/List", "viewer", "top-level project list → read tier"},
		{"kacho.cloud.storage.v1.VolumeService/List", "viewer", "top-level project list → read tier (unchanged, the convention this follows)"},
		// account/project get → v_get (List stays use-case viewer∪v_list)
		{"kaname.cloud.iam.v1.AccountService/Get", "v_get", "account get → v_get (R6)"},
		{"kaname.cloud.iam.v1.ProjectService/Get", "v_get", "project get → v_get (R6)"},
		// object-self mutations → v_update / v_delete
		// User/Update ЗДЕСЬ БОЛЬШЕ НЕ СТОИТ, и это не пропуск (#1102). Конвенция
		// «пообъектная мутация → v_update» описывает ресурсы, которыми распоряжается
		// АККАУНТ. Человек им не является: его строка глобальна — одна на все его
		// аккаунты, — поэтому правку записи спрашивает `record_writer`, у которого
		// источников уровня аккаунта нет вовсе. Оставить строку значило бы требовать
		// конвенцию от метода, выведенного из-под неё намеренно; заменить отношение
		// в этой же строке — объявить `record_writer` частью конвенции, которой он
		// не принадлежит. Форму его гейта держит своя проба:
		// services/iam/internal/authzmap/governing_the_identity_is_not_an_account_right_test.go.
		{"kaname.cloud.iam.v1.GroupService/Update", "v_update", "object-self update"},
		{"kacho.cloud.vpc.v1.NetworkService/Update", "v_update", "object-self update"},
		{"kacho.cloud.vpc.v1.NetworkService/Delete", "v_delete", "object-self delete"},
		{"kaname.cloud.iam.v1.AccessBindingService/Delete", "v_delete", "object-self delete"},
		// storage — the domain this table used to describe by a single row (List),
		// which is the one storage RPC the convention leaves on a tier. Everything
		// object-self was outside the table's reach, so the gate declared the
		// convention while the whole domain sat on tiers underneath it.
		{"kacho.cloud.storage.v1.VolumeService/Get", "v_get", "object-self get"},
		{"kacho.cloud.storage.v1.VolumeService/Update", "v_update", "object-self update"},
		{"kacho.cloud.storage.v1.VolumeService/Delete", "v_delete", "object-self delete"},
		{"kacho.cloud.storage.v1.SnapshotService/Get", "v_get", "object-self get"},
		{"kacho.cloud.storage.v1.SnapshotService/Update", "v_update", "object-self update"},
		{"kacho.cloud.storage.v1.SnapshotService/Delete", "v_delete", "object-self delete"},
		{"kacho.cloud.storage.v1.ImageService/Get", "v_get", "object-self get"},
		{"kacho.cloud.storage.v1.ImageService/Update", "v_update", "object-self update"},
		{"kacho.cloud.storage.v1.ImageService/Delete", "v_delete", "object-self delete"},
		// create-child stays editor on parent
		{"kacho.cloud.vpc.v1.NetworkService/Create", "editor", "create-child → editor on parent (F-7)"},
		{"kacho.cloud.compute.v1.InstanceService/Create", "editor", "create-child → editor on parent (F-7)"},
		// Internal.* admin RPCs unchanged — system_admin
		{"kaname.cloud.iam.v1.InternalClusterService/Get", "system_admin", "Internal admin — no downgrade"},
		{"kacho.cloud.geo.v1.InternalRegionService/Create", "system_admin", "Internal admin — no downgrade"},
		// scope-polymorphic AB reads stay viewer (handler is the precise gate)
		{"kaname.cloud.iam.v1.AccessBindingService/ListByScope", "viewer", "scope-polymorphic read floor"},
		{"kaname.cloud.iam.v1.AccessBindingService/ListAssignableRoles", "viewer", "scope-polymorphic read floor"},
	}
	for _, tc := range cases {
		t.Run(tc.fqn, func(t *testing.T) {
			entry, ok := c.Lookup(tc.fqn)
			require.True(t, ok, "fqn missing from embedded catalog: %s", tc.fqn)
			assert.Equal(t, tc.relation, entry.RequiredRelation, "%s (%s)", tc.fqn, tc.note)
		})
	}

	// Списки iam ушли из полосы освобождения в полосу сужения на данных (#914).
	//
	// Здесь стояло «AccountService/List остаётся освобождённым». Это перестало быть
	// верным намеренно: у освобождения есть ВТОРОЙ смысл — оно допускает вызов
	// внутреннего слушателя ВООБЩЕ БЕЗ ПРИНЦИПАЛА, на основании сетевого положения.
	// Пообъектный фильтр ниже по стеку без принципала бессмыслен, поэтому лоток
	// обязан быть тот, который идёт ПОСЛЕ его извлечения.
	//
	// ListPermissionCatalog ушёл отсюда раньше и по другой причине: его пол перевели
	// на отношение, которое производит СИСТЕМНАЯ ВЫДАЧА (#893/#895), — доступ стал
	// виден в перечне выдач и отзываем там же.
	for _, fqn := range []string{
		"kaname.cloud.iam.v1.AccountService/List",
		"kaname.cloud.iam.v1.GroupService/List",
		"kaname.cloud.iam.v1.ProjectService/List",
		"kaname.cloud.iam.v1.RoleService/List",
		"kaname.cloud.iam.v1.ServiceAccountService/List",
		"kaname.cloud.iam.v1.UserService/List",
	} {
		entry, ok := c.Lookup(fqn)
		require.True(t, ok, "запись каталога отсутствует: %s", fqn)
		assert.False(t, entry.IsExempt(),
			"%s больше не освобождён: освобождение допускает вызов без принципала", fqn)
		assert.True(t, entry.ScopeFiltered,
			"%s обязан объявлять сужение на данных", fqn)
		assert.NotEmpty(t, entry.Permission,
			"%s обязан нести НАСТОЯЩЕЕ имя права, а не пустую строку", fqn)
	}

	// Invariant: an Internal.* RPC anchored on the CLUSTER SINGLETON must not carry
	// a v_* verb-bearing relation. Such a method is administrative — it answers
	// about the deployment, not about anybody's object — and a verb relation there
	// is one a tenant can hold, which is how infra reads end up tenant-readable.
	//
	// The invariant used to say "no Internal.* RPC, full stop", and that was too
	// broad by exactly one case: an internal method anchored on a TENANT OBJECT,
	// which acts for the tenant under the initiator's forwarded identity. vpc's
	// internal address path (allocate / set-reference / mark-in-use) is that case —
	// compute calls it while attaching an interface, on the caller's own address,
	// and the relation it needs is the same `v_update` the public path needs. A
	// tier relation there would refuse every legitimate use.
	//
	// The old wording read as true only because those rows carried `<exempt>` — the
	// catalog named no relation at all, while kacho-vpc required `v_update` all
	// along. The invariant held over the catalog and was false about what was
	// enforced; now that both come from one source, it says what it means.
	verbOnCluster, verbOnObject := 0, 0
	for _, fqn := range c.FQNs() {
		if !strings.Contains(fqn, "Internal") {
			continue
		}
		entry, _ := c.Lookup(fqn)
		if !strings.HasPrefix(entry.RequiredRelation, "v_") {
			continue
		}
		if entry.ScopeExtractor.ObjectType == "cluster" {
			verbOnCluster++
			assert.Fail(t, "verb-bearing relation on a cluster-anchored Internal RPC",
				"%s carries %q on the cluster singleton: that relation is tenant-holdable, and the "+
					"method answers about the deployment", fqn, entry.RequiredRelation)
			continue
		}
		verbOnObject++
	}
	assert.Zero(t, verbOnCluster)
	// Положительный контроль: случай, ради которого инвариант сужен, обязан в
	// дереве БЫТЬ. Ноль таких строк означал бы, что утверждение выше не отличает
	// сужение от отсутствия предмета.
	assert.NotZero(t, verbOnObject,
		"ни один Internal.* RPC не якорится на объекте тенанта с глагольным отношением — "+
			"сужение инварианта стало беспредметным, и его надо либо вернуть к прежнему виду, "+
			"либо перепроверить распознавание")
	t.Logf("перепись: Internal.* с глаголом на кластере %d, на объекте тенанта %d",
		verbOnCluster, verbOnObject)
}

func TestPermissionCatalog_RejectBadVersionFlavour(t *testing.T) {
	// Truncated input — must fail with descriptive error.
	raw := []byte(`{"entries":`)
	c := middleware.NewPermissionCatalog()
	err := c.LoadFromBytes(raw)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "decode")
}
