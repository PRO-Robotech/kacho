// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_acr_invariant_test.go — SEC-acr-stepup-refinement (R3).
//
// Locks the step-up allowlist invariant (SEC-ACR-13 / I1 / I2): EXACTLY the 28
// named grant/credential/tenancy-root FQNs carry required_acr_min="2"; every
// other non-exempt RPC carries "1" (routine, AAL1 floor); exempt RPCs carry ""
// (no step-up requirement). Both embedded catalog copies (gateway + iam) are
// byte-identical, and the `permission` field of AccessBindingService/Create is
// NOT changed by the acr addition (net-strengthening: exempt-permission + acr=2).
//
// This is the primary RED→GREEN lock: before the proto refinement 372 RPCs carry
// "2" (blanket step-up); after it, exactly 30 do.
//
// Set revision (hardening round): UserService/Invite JOINED category B — it
// creates an AccessBinding atomically (project_id+role_id) and is therefore a
// privilege-grant surface; leaving it at the AAL1 floor was a step-up bypass of
// AccessBindingService/Create. 41 → 42.
//
// Set revision (storage-split, dead-surface removal): the stillborn
// DiskPlacementGroup / Filesystem / SnapshotSchedule contracts were deleted —
// their 6 Set/UpdateAccessBindings grant-surface FQNs left the set. 42 → 36.
//
// Set revision (fga-model drift-gate restoration): the born-dead GpuCluster /
// HostGroup / PlacementGroup / ReservedInstancePool / HostType contracts were
// deleted for the same reason — never served on any listener and absent from the
// enforced authorization model. Six more Set/UpdateAccessBindings grant-surface
// FQNs left the set. 36 → 30.
//
// Set revision (service-account disable is an action): ServiceAccountService
// Disable/Enable JOINED category A. The state they write decides whether a
// machine identity may authenticate at all — one call answers, for the whole
// principal, the question SAKeyService/Revoke answers for a single key, and it
// also stops the next mint. The set literals below moved to 24; the prose
// counts in the paragraphs above are the historical trail, the assertions are
// the contract.
//
// Set revision (tenant condition surface retired): category F
// (ConditionsService Update/Delete) is GONE — not downgraded, removed. The
// resource those two mutated no longer exists, so there is no policy artifact of
// that kind left to step up for. 26 → 24.
//
// Set revision (module-catalog application): category K —
// InternalModuleService/Apply JOINED. It brings the permission-catalog rows of
// one module to what its manifest declares, and doing so resettles the tenant
// projections that named a withdrawn row — a right taken away, irreversibly.
// 32 → 33. The routine band moved with it, and by THREE rather than by nothing:
// the same contract declares "1" explicitly on Plan / Get / List, which moves
// them out of the no-floor band the generator's default would have put them in.
// 287 → 290, catalog total 346 → 350.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// sensitiveACR2Set — the 33 FQNs that MUST carry required_acr_min="2" after the
// refinement (grant-surface + credential + tenancy-root + shared-resource
// ceiling, domain-agnostic). Any drift (an RPC added or dropped) fails this
// test. Categories A–J per the APPROVED acceptance docs.
func sensitiveACR2Set() map[string]struct{} {
	fqns := []string{
		// A — credential mint/destroy (6). ServiceAccount Disable/Enable belong
		// here and not with the routine lifecycle: they decide whether a machine
		// identity may authenticate AT ALL. Disable is every Revoke this account
		// has at once and then some — it also stops the next mint, on every door
		// (client_credentials hook, federated assertion, key issuance, docker
		// token) — and Enable hands all of that back. Classifying the pair with
		// the credential surface keeps the INTERACTIVE cost of the two answers
		// the same: a human who must re-authenticate to revoke one key would
		// otherwise reach the same outcome for the whole principal through the
		// cheaper door.
		//
		// What this does NOT do, stated here because the opposite is easy to
		// assume: it is not a second authorization gate, and it is INERT for
		// machine callers — pkg/grpcsrv.EvaluateStepUp short-circuits to allow
		// for a service-account principal before the floor is read at all. A
		// service account holding `v_update` on the target disables it with no
		// step-up whatsoever. That is the platform's deliberate posture (a
		// machine has no interactive ceremony to perform), and it means the whole
		// of WHO may do this is decided by the model, exactly as it is for
		// Update. The floor raises assurance for humans; it grants nothing and
		// withholds nothing from machines.
		"kacho.cloud.iam.v1.UserTokenService/Issue",
		"kacho.cloud.iam.v1.UserTokenService/Revoke",
		"kacho.cloud.iam.v1.SAKeyService/Issue",
		"kacho.cloud.iam.v1.SAKeyService/Revoke",
		"kacho.cloud.iam.v1.ServiceAccountService/Disable",
		"kacho.cloud.iam.v1.ServiceAccountService/Enable",
		// UserService Block/Unblock belong in the SAME band, for the same reason
		// and with the same caveat. They write the state that decides whether a
		// person may authenticate into an Account at all — the human counterpart
		// of the two above — so a human who must re-authenticate to revoke one
		// personal token would otherwise reach a strictly larger outcome (all of
		// someone's access into a tenancy, at once) through the cheaper door.
		//
		// Scope, stated as it now is rather than as it once was: these two write the
		// state of the WHOLE identity. A person is one `users` row for the entire
		// platform, so the block reaches every Account they belong to at once.
		//
		// > This comment used to say the opposite — "these two write ONE membership
		// > row, not the whole identity; the person keeps working wherever else they
		// > are active". That was true until the row stopped being a membership, and
		// > it outlived its subject. It is corrected rather than deleted because a
		// > stale claim about the radius of a security control is read as an active
		// > boundary.
		//
		// The floor is likewise INTERACTIVE-only and INERT for machine callers,
		// exactly as for the pair above: a service account holding the relation on
		// the target blocks it with no step-up whatsoever. WHO may do it is decided
		// by the model — `identity_suspender` on `iam_user`, which has NO
		// account-level source (#1102): the account administrator is not among the
		// holders, and the cloud administrator reaches it as he reaches everything.
		"kacho.cloud.iam.v1.UserService/Block",
		"kacho.cloud.iam.v1.UserService/Unblock",
		// B — iam binding grant (5; Create is exempt-permission + acr=2, net-strengthening).
		// Invite belongs here: it inlines an AccessBinding create (project_id+role_id)
		// in the invite tx — the same privilege AccessBindingService/Create issues.
		"kacho.cloud.iam.v1.AccessBindingService/Create",
		"kacho.cloud.iam.v1.AccessBindingService/Update",
		"kacho.cloud.iam.v1.AccessBindingService/Delete",
		"kacho.cloud.iam.v1.AccessBindingService/Revoke",
		"kacho.cloud.iam.v1.UserService/Invite",
		// RemoveFromAccount — ПАРА к Invite, и на этой полосе она стоит по тому же
		// доводу, что и он: обе меняют СОСТАВ УЧАСТНИКОВ аккаунта. Invite вводит
		// человека и вместе с ним может выдать право; исключение выводит его и
		// требует, чтобы прав у него в этом аккаунте уже не было (отложенный
		// триггер `membership_carrying_rights_is_kept` отвергает снятие членства,
		// несущего живую выдачу). Соседняя по смыслу `AccessBindingService/Revoke`
		// стоит здесь же, поэтому оставить исключение на нижнем пороге значило бы
		// сделать более дешёвую дверь к тому же исходу (#1127).
		"kacho.cloud.iam.v1.UserService/RemoveFromAccount",
		// C — compute per-resource grant. Поверхность выдачи на самой машине снята
		// целиком вместе с остальной мёртвой: ни `SetAccessBindings`, ни
		// `UpdateAccessBindings` у машины больше нет — выдача на ресурс идёт
		// привязками iam, как у прочих доменов. Пример полосы «чувствительное» у
		// compute здесь поэтому отсутствует, и это не пропуск: у домена не осталось
		// ни одного RPC, поднимающего планку подтверждения.
		// D — group membership grant + group destroy (3; Delete = revoke-by-all, R3/B-2)
		"kacho.cloud.iam.v1.GroupService/AddMember",
		"kacho.cloud.iam.v1.GroupService/RemoveMember",
		"kacho.cloud.iam.v1.GroupService/Delete",
		// E — role policy mutation (2)
		"kacho.cloud.iam.v1.RoleService/Update",
		"kacho.cloud.iam.v1.RoleService/Delete",
		// G — cluster-admin grant (2)
		"kacho.cloud.iam.v1.InternalClusterService/GrantAdmin",
		"kacho.cloud.iam.v1.InternalClusterService/RevokeAdmin",
		// H — tenancy-root destroy (2)
		"kacho.cloud.iam.v1.AccountService/Delete",
		"kacho.cloud.iam.v1.ProjectService/Delete",
		// I — interactive-login client lifecycle (3). IAM-INT-1 scenario 23.
		// Sensitive for the same reason category A is: a redirect target is
		// WHERE AN AUTHORIZATION CODE IS DELIVERED, so creating a client,
		// editing its target list, or removing it decides who may receive a
		// credential. Get/List are deliberately NOT here — reading a client
		// mints nothing — and their exclusion is asserted by the complement
		// test below, because the generator's default floor is "2" and an
		// unstated floor would have put them here by accident rather than by
		// decision.
		"kacho.cloud.iam.v1.InternalInteractiveClientService/Create",
		"kacho.cloud.iam.v1.InternalInteractiveClientService/Update",
		"kacho.cloud.iam.v1.InternalInteractiveClientService/Delete",
		// J — resource-count ceilings (3). Issue #291, S1.
		//
		// Sensitive because a ceiling decides how much of a SHARED resource one
		// tenant may take. Raising one hands out headroom that is not created by
		// the act; lowering one freezes a tenant's creation path; withdrawing one
		// silently moves the decision to another scope. All three are grants in
		// the same sense category B is: they change what a principal may do, and
		// nothing about them is undone by the next read.
		//
		// Get / List / Resolve / ListChangedSince are deliberately NOT here —
		// reading a ceiling grants nothing — and their exclusion is asserted by
		// the complement test below, because the generator's default floor is "2"
		// and an unstated floor would have put them here by accident rather than
		// by decision.
		"kacho.cloud.iam.v1.InternalLimitService/Create",
		"kacho.cloud.iam.v1.InternalLimitService/Update",
		"kacho.cloud.iam.v1.InternalLimitService/Delete",

		// Те же три акта на ПУБЛИЧНОМ адресе (ADM-1 S1, #878). Порог
		// подтверждения личности принадлежит ДЕЙСТВИЮ, а не адресу: сменить
		// потолок через `/iam/v1/limits` — ровно то же изменение доли арендатора
		// в общей платформе, что и через внутренний путь. Разойдись эти два
		// перечня хоть на одну запись, публичный адрес стал бы дешёвым обходом
		// ступени, и обход этот не был бы виден ни в одном диффе.
		"kacho.cloud.iam.v1.LimitService/Create",
		"kacho.cloud.iam.v1.LimitService/Update",
		"kacho.cloud.iam.v1.LimitService/Delete",

		// K — module-catalog application (1). kacho#1034.
		//
		// Sensitive because applying WITHDRAWS TENANT RIGHTS and does so
		// irreversibly: bringing the catalog rows of a module to what its manifest
		// declares resettles the tenant projections that named a withdrawn row —
		// the row of the live projection is MOVED OUT, not copied, and no
		// production reader of the orphan ledger exists. That is a change of the
		// security posture, in the same sense category B is: it changes what a
		// principal may do, and the next read does not undo it.
		//
		// Plan / Get / List are deliberately NOT here — a read withdraws nothing —
		// and they declare "1" EXPLICITLY in the .proto rather than by omission,
		// because the generator stamps "2" for an unstated floor and all three
		// would otherwise have arrived in this band by accident. Their exclusion is
		// asserted by the complement test below.
		//
		// Inert on the path it is served on today, and that is a decision recorded
		// in the acceptance doc rather than an oversight: the interceptor enforcing
		// the floor on the internal listener reads a hand-written roster of
		// gateway-fronted internal RPCs, the same entry ALSO narrows the caller to
		// the edge, and the edge has no REST route to this method yet — entering it
		// now would produce a surface reachable by no one. The catalog entry states
		// what the act COSTS; lowering it to "1" because nothing enforces it today
		// would be a lie about the requirement.
		"kacho.cloud.iam.v1.InternalModuleService/Apply",
	}
	set := make(map[string]struct{}, len(fqns))
	for _, f := range fqns {
		set[f] = struct{}{}
	}
	return set
}

// TestPermissionCatalog_ACR_SetInvariant — SEC-ACR-13 / I1: the set of FQNs
// carrying required_acr_min="2" is EXACTLY the named sensitive RPCs.
func TestPermissionCatalog_ACR_SetInvariant(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	sensitive := sensitiveACR2Set()
	// Было 27; стало 25 — с машины снята поверхность выдачи прав (два RPC полосы
	// «чувствительное» ушли вместе с ней). Прежняя история числа: 27, up from 24:
	// category F (ConditionsService Update/Delete) had its
	// subject retired together with the tenant-facing condition surface (26→24),
	// and category I (InternalInteractiveClientService Create/Update/Delete)
	// then joined with IAM-INT-1 (24→27). The number is asserted rather than
	// derived from the list on purpose — a silent shrink is exactly what would
	// happen if an entry were dropped by accident.
	// 25 → 28: потолки на число ресурсов (issue #291) добавили три мутации полосы
	// «чувствительное» — назначение, изменение и отзыв предела. Число утверждается,
	// а не выводится из списка: молчаливое сокращение — ровно то, что произошло бы
	// при случайно выпавшей записи.
	require.Len(t, sensitive, 33, "the acceptance-doc sensitive set must contain exactly 33 FQNs")

	got2 := map[string]struct{}{}
	for _, fqn := range c.FQNs() {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		if e.RequiredACRMin == "2" {
			got2[fqn] = struct{}{}
		}
	}

	// Every named sensitive FQN carries "2", and NOTHING else does.
	for fqn := range sensitive {
		e, ok := c.Lookup(fqn)
		require.True(t, ok, "sensitive FQN missing from catalog: %s", fqn)
		assert.Equal(t, "2", e.RequiredACRMin, "sensitive FQN must carry acr=2: %s", fqn)
	}
	for fqn := range got2 {
		_, want := sensitive[fqn]
		assert.True(t, want, "FQN carries acr=2 but is NOT in the sensitive allowlist (over-inclusion): %s", fqn)
	}
	assert.Len(t, got2, 33, "exactly 33 FQNs must carry required_acr_min=2")
}

// TestPermissionCatalog_ACR_ComplementNotTwo — SEC-ACR-13 / I1: explicit
// regression points that MUST NOT carry "2" (they were downgraded to "1").
func TestPermissionCatalog_ACR_ComplementNotTwo(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	routine := []string{
		// B6 author-inert create → routine
		"kacho.cloud.iam.v1.RoleService/Create",
		"kacho.cloud.iam.v1.GroupService/Create",
		// per-resource ListAccessBindings — reads → routine. Compute-пример снят
		// вместе с поверхностью выдачи на машине (см. набор «чувствительных» выше).
		"kacho.cloud.iam.v1.AccessBindingService/ListByScope",
		"kacho.cloud.iam.v1.AccessBindingService/ListAssignableRoles",
		"kacho.cloud.iam.v1.AccessBindingService/ListBySubject",
		// B3 subject-delete → routine
		"kacho.cloud.iam.v1.ServiceAccountService/Delete",
		"kacho.cloud.iam.v1.UserService/Delete",
		// B5 non-iam Internal*-admin (sample) → routine
		"kacho.cloud.geo.v1.InternalRegionService/Create",
		"kacho.cloud.vpc.v1.InternalAddressPoolService/Create",
		"kacho.cloud.compute.v1.InternalMachineTypeService/Create",
		// B4 cluster reads → routine
		"kacho.cloud.iam.v1.InternalClusterService/Get",
		"kacho.cloud.iam.v1.InternalClusterService/ListAdmins",
		// interactive-login client READS (IAM-INT-1 scenario 23, negative pair).
		// These two exist in this list precisely because the generator stamps
		// "2" when proto omits required_acr_min: if the declaration were ever
		// dropped from the .proto, the default would silently promote a read to
		// the sensitive band and BOTH this assertion and the over-inclusion
		// guard would fire. Reading a client issues no credential.
		"kacho.cloud.iam.v1.InternalInteractiveClientService/Get",
		"kacho.cloud.iam.v1.InternalInteractiveClientService/List",
		// routine resource lifecycle
		"kacho.cloud.vpc.v1.NetworkService/Create",
		"kacho.cloud.compute.v1.InstanceService/Create",
		// group non-destructive lifecycle (Delete is sensitive, these are not)
		"kacho.cloud.iam.v1.GroupService/AddMember", // sanity: this IS "2" (asserted below, negative-control excluded)
	}
	for _, fqn := range routine {
		if fqn == "kacho.cloud.iam.v1.GroupService/AddMember" {
			continue // control — AddMember is sensitive; asserted in the set-invariant test
		}
		e, ok := c.Lookup(fqn)
		require.True(t, ok, "routine FQN missing from catalog: %s", fqn)
		assert.NotEqual(t, "2", e.RequiredACRMin, "routine FQN must NOT carry acr=2: %s (got %q)", fqn, e.RequiredACRMin)
		assert.Equal(t, "1", e.RequiredACRMin, "routine non-exempt FQN must carry acr=1 (AAL1 floor): %s", fqn)
	}
}

// TestPermissionCatalog_ACR_GroupDeleteSensitive — R3/B-2: GroupService/Delete
// is revoke-by-all → sensitive ("2"); the non-destructive group lifecycle is not.
func TestPermissionCatalog_ACR_GroupDeleteSensitive(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	del, ok := c.Lookup("kacho.cloud.iam.v1.GroupService/Delete")
	require.True(t, ok)
	assert.Equal(t, "2", del.RequiredACRMin, "GroupService/Delete is revoke-by-all → sensitive (R3/B-2)")

	for _, fqn := range []string{
		"kacho.cloud.iam.v1.GroupService/Create",
		"kacho.cloud.iam.v1.GroupService/ListMembers",
	} {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		assert.NotEqual(t, "2", e.RequiredACRMin, "non-destructive group lifecycle must be routine: %s", fqn)
	}
}

// TestPermissionCatalog_ACR_CreateNetStrengthening — SEC-ACR-06 / B1 / I4:
// AccessBindingService/Create carries acr="2" WHILE permission stays "<exempt>"
// (orthogonal fields; StepUpGate enforces, FGA scope-Check stays skipped).
func TestPermissionCatalog_ACR_CreateNetStrengthening(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	e, ok := c.Lookup("kacho.cloud.iam.v1.AccessBindingService/Create")
	require.True(t, ok)
	assert.Equal(t, "2", e.RequiredACRMin, "Create must gain acr=2 (close create-instead-of-Update bypass)")
	assert.Equal(t, "<exempt>", e.Permission, "Create permission must stay <exempt> (acr/permission are orthogonal — net-strengthening)")
	assert.True(t, e.IsExempt(), "Create must remain FGA-exempt")
}

// TestPermissionCatalog_ACR_CountsAndByteIdentity — SEC-ACR-13 / I2: the whole
// catalog splits 26×"2" / 211×"1" / 63×"" = 300, and both embedded copies
// (gateway + iam) are byte-identical. (NLB CONTRACT removed the 4 routine
// loadbalancer RPCs Start/Stop/AttachTargetGroup/DetachTargetGroup: 332→328;
// the UserService/Invite grant-surface correction moved one more 1→2: 328→327.
// The storage-split dead-surface removal dropped the 36 RPCs of the stillborn
// DiskPlacementGroup / Filesystem / SnapshotSchedule services plus
// Instance.{Attach,Detach}Filesystem and Disk.ListSnapshotSchedules:
// 42→36 sensitive, 327→297 routine, 434→398 total. The fga-model drift-gate
// restoration then dropped the 41 RPCs of the born-dead GpuCluster / HostGroup /
// PlacementGroup / ReservedInstancePool / HostType services:
// 36→30 sensitive, 297→262 routine, 398→357 total. Then 30→28 / 262→241 /
// 357→334 when the compute InstanceGroup service was withdrawn: it was declared
// in proto and routed at the edge, but no implementation existed anywhere, so
// its 23 entries pointed at paths that answered 404 — and its field names named
// another cloud on our own wire, which ban #2 does not allow.)
func TestPermissionCatalog_ACR_CountsAndByteIdentity(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	var n2, n1, nEmpty int
	for _, fqn := range c.FQNs() {
		e, _ := c.Lookup(fqn)
		switch e.RequiredACRMin {
		case "2":
			n2++
		case "1":
			n1++
		case "":
			nEmpty++
		default:
			t.Fatalf("unexpected required_acr_min %q on %s", e.RequiredACRMin, fqn)
		}
	}
	// Retiring compute's duplicate block storage removed 34 entries in two steps.
	// Disk/Image/Snapshot took 29: 28→22 sensitive (the six *AccessBindings RPCs of
	// the three resources, privilege-granting and so acr=2) and 238→215 routine (the
	// other 23). DiskType then took 5 more, all routine (2 public reads + 3 internal
	// admin mutations): 215→210. The exempt count is untouched throughout — none of
	// the retired RPCs was exempt.
	// ServiceAccountService Disable/Enable are net-new entries and both land in
	// the sensitive band (the state they write is what decides whether a machine
	// identity may authenticate): 22→24 sensitive, 296→298 total. Routine and
	// exempt are untouched — neither action displaced an existing RPC.
	// UserService Block/Unblock then did the same for the human counterpart —
	// administrative suspension of a membership, previously reachable only by
	// editing the database: 24→26 sensitive, 298→300 total. Routine and exempt
	// untouched again, for the same reason.
	// InternalSessionRevocationsService/ListByUser then moved exempt→routine
	// (64→63 exempt, 210→211 routine, total untouched — nothing was added or
	// removed). Its response is one named user's session history, so its record now
	// states the per-object lane it is actually decided on (`v_get` on
	// `iam_user:<user_id>`) instead of declaring that nothing decides it. Routine
	// and not sensitive: it reads, it grants nothing and destroys nothing.
	// Retiring the tenant-facing condition surface removed the six ConditionsService
	// entries: Update/Delete were sensitive (category F, which no longer has a
	// subject), Get/List/Create/Evaluate were routine. 26→24 sensitive, 211→207
	// routine, 300→294 total. Exempt untouched — none of the six was exempt.
	// Retiring four services declared without a single implementation removed five
	// entries, ALL of them exempt: 63→58 exempt, 294→289 total. Sensitive and routine
	// are untouched — every one of the five carried `<exempt>`, which is what a lane
	// declared for a method that no listener serves looks like. The four:
	// compute.v1 + vpc.v1 InternalResourceLifecycleService/Subscribe (the live
	// lifecycle feed is loadbalancer.v1's), vpc.v1 InternalWatchService/Watch (the live
	// event stream is compute.v1's), and iam.v1 InternalIamHooksService/{TokenHook,
	// RefreshTokenHook} (Hydra's hooks are served over HTTP with their own
	// request-body structs — these proto types were read by no non-generated line).
	// IAM-INT-1 then ADDED the five InternalInteractiveClientService entries:
	// Create/Update/Delete are sensitive (category I — they decide where an
	// authorization code may be delivered), Get/List are routine. 24→27
	// sensitive, 207→209 routine, 289→294 total. Exempt untouched at 58 — none of
	// the five is exempt.
	//
	// NOTE for the next reader, because THREE different totals for this same +5
	// are in circulation and only one of them describes this tree. The APPROVED
	// acceptance doc states the delta as 26→29 sensitive / 300→305 total, which
	// was correct on ITS base (bb26d905, 114 commits behind this one). The
	// carried branch stated 24→27 / 294→299, correct on ITS base (4bfe367c).
	// Between those bases the catalog lost six ConditionsService entries and
	// then five more `<exempt>` phantom-service entries, so the same +5 lands on
	// 289 here. The numbers below are measured on THIS tree; the prose above is
	// the historical trail, the assertions are the contract.
	//
	// Then vpc.v1 InternalAddressService/CreateOwnedAddress ADDED one exempt entry:
	// 58→59 exempt, 294→295 total. Sensitive and routine are untouched. `<exempt>`
	// here does NOT mean "ungated" — it means THE GATEWAY does not gate it, which is
	// the only thing this catalog speaks about. The method lives on the cluster-internal
	// listener :9091 and is never routed through the gateway, so the gateway has no
	// request of its own to check. Its actual gate is the vpc interceptor, which is
	// wired on BOTH listeners and resolves this method through
	// services/vpc/internal/check/permission_map.go: `editor` on the project
	// taken from the creation body — the very same requirement the public
	// AddressService/Create carries. All nine InternalAddressService methods sit in this
	// catalog the same way, so the new one follows the established shape rather than
	// introducing an exception to it.
	// 2026-08-09: 25 строк ушли из полосы `<exempt>` — не потому, что кто-то решил
	// их ужесточить, а потому, что каталог начал называть то, что СЕРВИС и без него
	// исполнял. Одиннадцать из них проверяли отношение там, где каталог не
	// проверял ничего (девять внутренних RPC адресов vpc, поток жизненного цикла
	// nlb, публичный список сетей vpc); четырнадцать требовали названного
	// принципала там, где каталог освобождал вызов целиком (списки nlb, поток
	// compute, поверхность реестра) — им объявлена полоса `scope_filtered`.
	//
	// Полоса ACR у всех двадцати пяти — «1» (рутинная): это ТОТ ЖЕ порог, что у
	// остальных чтений и рутинных мутаций дерева, и он не поднимает планку ни для
	// одного вызывающего, который сегодня проходит хоть какой-нибудь обычный RPC.
	// Прецедент ровно этого перехода уже описан абзацем выше —
	// InternalSessionRevocationsService/ListByUser, exempt→routine.
	//
	// Итог: 59→34 exempt, 209→234 routine, sensitive и total не тронуты.
	//
	// 2026-08-13, целевой вид storage: +14 записей, ВСЕ рутинные. Отсчёт ведётся
	// от состояния ствола ПОСЛЕ производственной формы compute (см. ниже), с
	// которым эта ветка слита: 244→258 routine, 303→317 total; sensitive (25) и
	// exempt (34) не тронуты — ни одна из четырнадцати не освобождена от проверки
	// и ни одна не поднимает планку аутентификации.
	//
	// Числа получены ЗАМЕРОМ по дереву после слияния, а не сложением двух
	// переписей: две независимые правки каталога, сведённые арифметикой в уме,
	// дали бы совпадение, которое нечем проверить.
	//
	// Состав: четыре публичных глагола (Volume/ChangeDiskType — v_update;
	// Snapshot/ListOperations — v_list; Snapshot/Copy и Image/Copy — editor@project)
	// и десять административных на :9091 (StorageBackend ×5, DiskTypeBinding ×3,
	// DiskType/SetLifecycle, Image/Register), все — system_admin на кластерном
	// синглтоне.
	//
	// Почему десять административных остались РУТИННЫМИ, хотя соблазн поднять их
	// велик. Полоса «чувствительных» в этом дереве собрана по одному признаку:
	// метод решает, КАК И КУДА доставляется аутентификация (адреса возврата
	// авторизационного кода, выдача токенов). Регистрация кластера хранения к
	// этому признаку не относится: она требует привилегии, а не более сильного
	// доказательства личности, и её привилегию уже держит system_admin. Подняв
	// планку одному административному RPC из десятков однотипных в дереве, мы
	// получили бы расхождение, невидимое ниоткуда, кроме этой переписи. Решение
	// названо здесь, чтобы следующий читатель видел выбор, а не пропуск.
	//
	// Отдельно про Copy: обе копии гейтятся `editor@project`, а НЕ `v_get` на
	// источник. Копия — новый ресурс (квота, имя, деньги), а роль наблюдателя
	// материализует v_get на каждый объект проекта: гейт на чтение отдал бы
	// наблюдателю право порождать ресурсы. Пообъектного `v_create` в платформе
	// нет by construction (authzmap: «создать» спрашивают у родителя), поэтому
	// форма у Copy та же, что у всякого Create.
	//
	// Итог того перехода: 59→34 exempt, 209→234 routine.
	//
	// Числа ниже перемерены 2026-08-13 и изменились по двум причинам, обе
	// названы: (а) с машины снята поверхность выдачи прав — два RPC полосы
	// «чувствительное» и один рутинный ушли вместе с ней (27→25 sensitive);
	// (б) заведён ключ входа в машину — шесть рутинных записей (234→235 с учётом
	// ушедшего); (в) заведено владение машиной узлом на внутреннем слушателе — три
	// рутинных записи (235→238); (г) заведена группа размещения — шесть рутинных
	// записей (238→244). Итог 295→294→297→303.

	// СЛИЯНИЕ: эта ветка добавляет к каталогу ещё две группы, обе рутинные.
	// (а) шов с исполнителем датаплейна (`vpc.v1.InternalDataplaneService`): поток
	// намерения `WatchIntent` (`system_viewer` @ cluster) и подтверждение применения
	// `ReportIntentApplied` (`system_admin` @ cluster).
	//
	// ОБЕ ЗАПИСИ СНЯТЫ (kacho#400) — вместе со всем швом: исполнителя не
	// существует, вызывающей стороны у подтверждения не было ни одной. Абзац
	// оставлен, а не удалён, потому что он объясняет, откуда брались числа
	// предыдущего замера; сегодня он читается как история, а не как описание
	// каталога. Обе записи были рутинными, поэтому снятие вычитается целиком из
	// рутинной полосы: 265→263 routine, итог 327→325; sensitive (28) и exempt (34)
	// не тронуты. Имена обеих RPC перешли в перепись снятой поверхности
	// (`internal/repohygiene`), чтобы не вернуться молча.
	//
	// (б) ресурс «именованный набор префиксов» (`vpc.v1.CidrGroupService`, восемь
	// публичных методов: чтение, список, история операций, создание, правка, два
	// глагола состава, удаление) — ни один не меняет посадку безопасности
	// вызывающего: набор адресуется его же проектом и гейтится пообъектно.
	//
	// Числа ниже сняты ЗАМЕРОМ слитого дерева, а не сложением двух переписей.
	// Сложение приведено ОТДЕЛЬНО и только как контроль замера — оно сошлось, и
	// это единственное, ради чего его стоит называть: 317 записей ствола + 10
	// заведённых этой веткой − 8 снятых ею с поверхности сети = 319. Разойдись
	// оно с замером — верным был бы замер, а расхождение означало бы, что одна из
	// сторон слияния потеряла записи молча.
	// #291 S1 — потолки на число ресурсов: семь записей одного внутреннего
	// сервиса. Три мутации (назначить · изменить · отозвать) уходят в полосу
	// «чувствительное»: предел решает, сколько ОБЩЕГО ресурса берёт один
	// арендатор, и ни одна из трёх не отменяется следующим чтением. Четыре
	// чтения — рутинные: два административных (Get/List) и два служебных
	// (Resolve/ListChangedSince), которыми владелец считаемого типа узнаёт
	// действующий потолок и его дельту.
	//
	// 25→28 sensitive, 260→264 routine, exempt не тронут (34): ни одна из семи не
	// освобождена от проверки — у двух служебных чтений отношение УЖЕ своё
	// (`quota_reader`), потому что кластерный ярус чтения был бы для них выдачей
	// много шире самой способности. Итог 319→326, замер по дереву.
	//
	// #365 — арендаторское чтение квот: ОДНА запись, `vpc.v1.QuotaService/List`,
	// рутинная. Рутинная потому, что это чтение и ничего не решает: величины
	// по-прежнему меняет только администратор облака, на внутреннем слушателе.
	// Ярус — `viewer` НА ПРОЕКТЕ, а не на кластере: кластерное отношение чтения
	// выполнимо подстановочным кортежем глобальных справочников и ответило бы
	// «да» каждому аутентифицированному, а эти числа принадлежат одному
	// арендатору. 264→265 routine, итог 326→327; sensitive и exempt не тронуты.
	// #439 — снятие аренды адреса: ОДНА запись,
	// `vpc.v1.InternalAddressService/ReleaseOwnedAddress`, рутинная.
	//
	// Рутинная потому, что это освобождение СВОЕЙ аренды, а не изменение того,
	// сколько ресурса доступно арендатору: следующее заведение аренды отменяет
	// её действие целиком.
	//
	// Ярус — `editor` НА ПРОЕКТЕ, в точности как у парного заведения аренды
	// (`CreateOwnedAddress`). Пообъектный якорь на самом адресе был бы здесь не
	// строгостью, а дефектом: он включает пробу существования, а её отказ
	// платформа обязана делать неотличимым от «объекта нет» — и вызывающий,
	// прочитав этот ответ как «аренда снята», строил на нём необратимый шаг.
	// Якорь-коллекция такой пробы не запускает, поэтому неоднозначность не
	// различается, а перестаёт порождаться. 263→264 routine, итог 325→326;
	// sensitive и exempt не тронуты.
	//
	// ADM-1 S1 — публикация административной поверхности пула адресов:
	// ОДИННАДЦАТЬ записей публичного `vpc.v1.AddressPoolService`, все рутинные.
	//
	// Рутинные потому, что переезд НЕ МЕНЯЕТ требования к подтверждению личности:
	// те же одиннадцать глаголов несли «1» на внутреннем сервисе и несут «1» на
	// публичном. Полоса «чувствительное» — про поверхность повышения привилегий, а
	// управление пулом адресов ею не является: оно не выдаёт и не отзывает прав.
	// Число записей с «2» обязано остаться 28 — сдвиг здесь означал бы «заодно
	// поднятое» требование, которого никто не решал.
	//
	// Ярус — `system_admin` @ `cluster`, а не `viewer`: кластерное отношение
	// чтения выполнимо подстановочным кортежем глобальных справочников и
	// ответило бы «да» каждому аутентифицированному, тогда как здесь закрывается
	// именно вызывающий без права администратора.
	//
	// Внутренние одиннадцать ОСТАЮТСЯ на время окна расширения (S1→S3) — отсюда
	// прирост ровно на 11, а не замена: 263→274 routine, итог 325→336; sensitive
	// и exempt не тронуты. Стадия S3 снимет внутренние и вернёт 274→263.
	//
	// Арендаторское чтение квот — ЧЕТЫРЕ рутинные записи `QuotaService.List`
	// (compute, storage, loadbalancer, registry) вдобавок к уже стоявшей записи
	// vpc.
	//
	// Рутинные потому, что чтение своих пределов не меняет посадку безопасности и
	// не выдаёт прав: это `viewer` на проекте, названном запросом, то есть тот же
	// ярус, что у прочих списков этих доменов. Полоса «чувствительное» — про
	// поверхность повышения привилегий; поднять свой потолок здесь нельзя, величины
	// назначает администратор облака на внутреннем слушателе владельца величин.
	// Число записей с «2» обязано остаться 28 — сдвиг здесь означал бы «заодно
	// поднятое» требование, которого никто не решал.
	//
	// Cluster-scoped отношение для этих записей было бы дефектом, а не строгостью:
	// `viewer @ cluster` выполним подстановочным кортежем глобальных справочников
	// и ответил бы «да» каждому аутентифицированному, тогда как числа здесь
	// принадлежат одному арендатору. Отсюда project-scope и извлечение области по
	// `project_id`. 275→279 routine, итог 337→341; sensitive и exempt не тронуты.
	//
	// Чтение квот ЛИЧНОСТИ — ОДНА рутинная запись `IdentityQuotaService.List`.
	//
	// Рутинная, хотя проверка области у неё снята: снят per-RPC Check, а не
	// требование к подтверждению личности. Ответ выводится из проверенного
	// принципала и ниоткуда больше — поля, которым можно было бы назвать чужую
	// личность, у запроса НЕТ, — поэтому единственный вопрос, который здесь можно
	// задать, отвечается аутентификацией. Кластерное отношение было бы не строже,
	// а ХУЖЕ: `viewer @ cluster` выполним подстановочным кортежем и ответил бы
	// «да» каждому, выглядя при этом гейтом.
	//
	// Полоса «чувствительное» не тронута: прочитать свой потолок — не повышение
	// привилегий. Число записей с «2» обязано остаться 28.
	//
	// Снятие внешнего движка прав (S6) убрало ПЯТЬ записей, и они из двух разных
	// полос — поэтому сдвиг идёт по двум числам, а не по одному:
	//
	//   - ОДНА рутинная: `AuthorizeService.ListObjects`. Глагол снят с контракта
	//     целиком. Он перечислял всё, что доступно субъекту, и не был постраничен
	//     by construction: продолжения у ответа не существовало, поэтому объекты
	//     сверх потолка были недостижимы при живых правах. Заменителя в каталоге
	//     не появилось — «что мне видно» отвечает постраничный `List` ресурсной
	//     службы, у которого своя запись уже есть. 280→279.
	//
	//   - ЧЕТЫРЕ освобождённые: все глаголы `InternalAuthorizeService`
	//     (`WriteTuples`, `ReadTuples`, `ReloadModel`, `GetFGAStoreInfo`). Служба
	//     администрировала сам движок — его хранилище кортежей и пин
	//     идентификатора модели, — и снята вместе с ним целым файлом контракта.
	//     Все четыре стояли `<exempt>` на внутреннем слушателе, отсюда сдвиг
	//     именно у этой полосы. 34→30.
	//
	// Полоса «чувствительное» НЕ тронута и обязана остаться 28: снятие поверхности
	// не поднимает и не опускает требование к подтверждению личности ни у одной
	// уцелевшей записи, а сдвиг здесь означал бы «заодно поднятое» требование,
	// которого никто не решал.
	//
	// ЧИСЛА НИЖЕ СНЯТЫ ЗАМЕРОМ ДЕРЕВА, а не сложением переписей. Сложение
	// приведено ОТДЕЛЬНО и только как контроль замера: 263 рутинных ствола + 1
	// снятие аренды адреса + 11 публичных глаголов пула + 4 чтения квот проекта
	// + 1 чтение квот личности − 1 снятое перечисление объектов = 279; освобождённых
	// 34 − 4 глагола администрирования движка − 1 снятая запись кортежа создателя
	// = 29; итог 342 − 6 = 336. Сошлось — и это единственное, ради чего его стоит
	// называть. Разойдись оно с замером, верным был бы замер.
	//
	// ЗДЕСЬ СОШЛИСЬ ТРИ ЛИНИИ, И НИ ОДНА НЕ БЫЛА ПРАВА ЦЕЛИКОМ.
	//
	//	снятие движка   — вычло записи администрирования хранилища отношений;
	//	снятие двух дверей (#788) — вычло две записи полосы «освобождённых»;
	//	публикация пределов (#878) — прибавила пять записей публичного
	//	                `iam.v1.LimitService`.
	//
	// Числа ниже — НЕ сумма трёх поправок в уме и НЕ выбор одной стороны: они
	// сняты ЗАМЕРОМ сгенерированного каталога ПОСЛЕ слияния (регенерация
	// `make -C gateway permission-catalog`). Арифметика в уме дала бы совпадение,
	// которое нечем проверить.
	//
	// ПОЧЕМУ ТРИ МУТАЦИИ ПРЕДЕЛОВ ЧУВСТВИТЕЛЬНЫ, А ПЕРЕЕЗД ПУЛА АДРЕСОВ БЫЛ ВЕСЬ
	// РУТИННЫМ. Полоса про поверхность повышения привилегий. Управление пулом
	// адресов ею не является — оно не выдаёт и не отзывает прав. Потолок числа
	// ресурсов — да: поднять его значит расширить долю арендатора в общей
	// платформе, опустить — заморозить ему создание, снять — молча передать
	// решение другой области. Ровно поэтому те же три глагола несли «2» и на
	// внутреннем сервисе: порог принадлежит ДЕЙСТВИЮ, а не адресу.
	//
	// ВНУТРЕННИЕ ПЯТЬ глаголов пределов ОСТАЮТСЯ на окно расширения (S1→S3) —
	// отсюда прирост, а не замена.
	// #893/#895 вернул ДЕВЯТЬ чтений глобальных справочников (регионы, зоны, типы
	// машин, типы дисков, словарь прав) из полосы без требования обратно в полосу
	// с порогом «1»: у отношения, на котором они стоят, появился производитель —
	// системная выдача с подстановочным субъектом. Отсюда +9 у обычной полосы и
	// −9 у полосы без требования; общее число записей не меняется.
	//
	// ЧИСЛО БЕЗ ТРЕБОВАНИЯ — НЕ ЧИСЛО ОСВОБОЖДЁННЫХ, и подпись это теперь
	// говорит: множество записей с пустым порогом есть СТРОГОЕ ПОДМНОЖЕСТВО
	// освобождённых (две освобождённые несут порог явно). Прежняя подпись
	// называла его «exempt count» — верно для предиката и неверно по составу.
	// 31→32: заведено исключение человека из аккаунта
	// (`UserService/RemoveFromAccount`, #1127) — вторая половина пары к Invite,
	// и порог у неё тот же, что у Invite и у отзыва выдачи: обе меняют СОСТАВ
	// участников аккаунта.
	assert.Equal(t, 33, n2, "sensitive count")
	// ТРИ линии завели по одной записи каждая, и объяснения всех трёх остаются —
	// они про разные глаголы. Числа ниже ЗАМЕРЕНЫ по дереву после слияния,
	// а не сложены в уме: арифметика трёх переписей даёт совпадение, которое
	// нечем проверить.
	//
	// Линия identity: заведён `InternalSessionRevocationsService/SessionCutoffOf`
	// (#1122) — вопрос края к НАШЕМУ авторитету отзыва про субъекта браузерной
	// сессии. Запись освобождённая и БЕЗ порога подтверждения личности, как и её
	// соседи по этому сервису: край задаёт вопрос НА СЛОЕ АУТЕНТИФИКАЦИИ, до
	// того как у запроса появится решённая личность, — порог там требовать не у
	// кого. Полосы «чувствительное» и «рутина» от неё не двигаются: снятие сессии
	// прав не выдаёт и не отзывает, а спрашивает про уже принятое решение.
	//
	// Линия watch: объявлен ЕДИНЫЙ глагол подписки платформы
	// (`kacho.cloud.subscription.InternalSubscriptionService/Subscribe`, #1018) —
	// запись каталога одна на всех владельцев журналов, потому что одно и полное
	// имя метода: сервис объявлен однажды и регистрируется каждым владельцем на
	// своём внутреннем слушателе. Порог «1», а не «2»: подписка ЧИТАЕТ и не
	// меняет посадку прав — поверхности повышения привилегий у неё нет. Отношения
	// в записи нет вовсе: метод `scope_filtered`, край пообъектного вопроса не
	// задаёт, а владелец сужает поток по правам вызывающего НА КАЖДОЙ отдаваемой
	// строке.
	//
	// Предыстория обеих: два прежних частных потока сняты ДО этих задач
	// (285→284 — журнал изменений compute, #813; 286→285 — поток жизненного цикла
	// nlb, #814), у обоих не было ни одного потребителя. Общий заведён взамен двух
	// частных.
	//
	// ИТОГ 342, а не 341: три ветки по отдельности писали «итог» каждая для СВОЕЙ
	// — и каждая была права. Общий предок нёс 339, три линии добавили по одной
	// записи: подписка платформы, отсечка сессии, исключение из аккаунта. Число
	// ЗАМЕРЕНО прогоном после слияния; сложение переписей в уме совпало бы со
	// всеми тремя сторонами, будучи неверным.
	// #1142 добавил ОДНУ запись — `InternalIAMService/ResolveBasicCredential`,
	// авторитет о предъявленном базовом секрете. Она `<exempt>` по тому же
	// основанию, что и её соседи по этому сервису (INTERNAL_LISTENER), и порога
	// повышения не несёт: край спрашивает её на КАЖДОМ запросе, предъявившем
	// базовый секрет, а порог отсекал бы ровно тот вид удостоверения, ради
	// которого глагол заведён. Числа ЗАМЕРЕНЫ прогоном, а не сложены в уме:
	// 25→26 и 342→343.
	//
	// #1450 добавил ОДНУ запись — `InternalIAMService/CheckBasicCredentialLive`,
	// тот же авторитет, спрошенный по ИДЕНТИФИКАТОРУ удостоверения, без
	// предъявления секрета: длинному соединению предъявлять нечего, а держать
	// секрет весь его срок значило бы завести поверхность хранения ради контроля.
	// Основание освобождения то же (INTERNAL_LISTENER), и порога повышения она
	// не несёт по той же причине, что соседний резолв: вопрос задаётся НА СЛОЕ
	// АУТЕНТИФИКАЦИИ, где решённой личности ещё нет и порог требовать не у кого.
	// Полоса «рутина» не двигается: запись освобождённая, а `n1` считает
	// неосвобождённые. Числа ЗАМЕРЕНЫ прогоном: 26→27 и 343→344.
	//
	// IAM-ID-2 S1 добавил ДВЕ записи — `MembershipService/Get` и
	// `MembershipService/List`, чтения принадлежности человека аккаунту. Обе в
	// полосе РУТИНЫ (порог «1»): чтение посадки прав не меняет, поверхности
	// повышения привилегий у него нет, и порог подтверждения личности отсекал бы
	// обычный просмотр карточки сотрудника. Отношение у обеих — `viewer` @
	// `account` из ПУТИ, то есть запись сужает, а не означает
	// «аутентифицирован»: подстановочный кортеж это отношение не выполняет.
	// Числа ЗАМЕРЕНЫ прогоном, а не сложены в уме: 285→287 и 343→345; полоса
	// «без порога» не сдвинулась — 26, потому что обе записи порог несут.
	//
	// ИТОГ 346, а не 344 и не 345: обе линии по отдельности писали «итог» каждая
	// для СВОЕЙ — и каждая была права. Общий предок нёс 343; ствол добавил одну
	// освобождённую запись (#1450), линия — две записи полосы рутины (IAM-ID-2 S1).
	// Числа ниже ЗАМЕРЕНЫ по вшитому каталогу ПОСЛЕ слияния, а не сложены в уме:
	// n2=32 (не двигалась), n1=287, nEmpty=27, итог 346.
	assert.Equal(t, 290, n1, "routine count")
	assert.Equal(t, 27, nEmpty, "no-acr-requirement count (подмножество `<exempt>`, не равное ему)")
	assert.Equal(t, 350, n2+n1+nEmpty, "catalog total")

	// Byte-identity of the two embedded copies.
	gw := middleware.EmbeddedPermissionCatalogJSON()
	iamPath := iamCatalogPath(t)
	iamBytes, err := os.ReadFile(iamPath)
	require.NoError(t, err, "read iam embedded catalog copy")
	assert.Equal(t, string(gw), string(iamBytes),
		"gateway and iam embedded permission_catalog.json copies must be byte-identical")
}

// iamCatalogPath resolves the iam embedded catalog copy relative to THIS test
// source file (robust to the test's working directory).
func iamCatalogPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// this file: <repo>/gateway/internal/middleware/permission_catalog_acr_invariant_test.go
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	return filepath.Join(repoRoot, "services", "iam", "internal", "apps", "kacho",
		"seed", "embedded", "permission_catalog.json")
}
