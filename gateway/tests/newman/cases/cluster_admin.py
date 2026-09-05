# Copyright (c) PRO-Robotech
# SPDX-License-Identifier: BUSL-1.1

"""Case-set for `kacho.cloud.iam.v1.InternalClusterService` — cluster-RBAC admin.

Covered RPCs: Get, GrantAdmin, RevokeAdmin, ListAdmins.

WHY THIS SUITE LIVES IN THE api-gateway REPO AND NOT IN services/iam
--------------------------------------------------------------------
These four RPCs are served ONLY on the api-gateway CLUSTER-INTERNAL REST listener
(`/iam/v1/internal/cluster/...`, :8081). Their gate is a gateway concern — the
permission-catalog entry `required_relation=system_admin` with
`scope_extractor{object_type: cluster, from_request_field: "*"}` — and the whole
point of `Get`/`ListAdmins` being gated is that a non-admin must not be able to
observe the cluster-admin roster. That is an api-gateway contract, so it is tested
from the api-gateway suite.

COVERAGE THIS SUITE UNIQUELY OWNS (verified against the Go tests, 2026-07-26)
----------------------------------------------------------------------------
The repo/handler layer has strong Go integration coverage of the DB invariants
(idempotency row-counts, advisory-lock write-skew, ordering, JOIN enrichment).
What NOTHING else covers, at any level, and therefore only lives here:

  * the REST/HTTP status mapping for the iam error codes on these RPCs —
    InvalidArgument→400 and NotFound→404;
  * the `iop` Operation-id prefix on cluster mutations (asserted elsewhere only
    for a DIFFERENT service, account/create_test.go);
  * `done=true` terminality of a polled cluster Operation;
  * the runtime authz 403 on `/iam/v1/internal/cluster/admins` (the Go middleware
    test drives only `/iam/v1/internal/cluster`);
  * the exact error text of "User <id> is not an active cluster admin" and
    "User <id> not found" (the Go tests assert the CODE only).

Additionally: services/iam/tests/newman/cases/iam-authz-grant-check-propagation.py
removed a tautological Break-Glass case and justified the removal by naming THIS
file as the place where the `cluster_admin_grants` INSERT contract is covered. That
justification is only true if this suite actually runs.

FIXTURES — canonical keys only, no bespoke seeding
--------------------------------------------------
This suite deliberately consumes only fixture keys that BOTH seeding paths already
emit — tests/authz-fixtures/setup.sh (dev posture) and prodseed_matrix.py /
prodseed_all.py (production posture):

  jwtBootstrap           — holds `system_admin` on `cluster:cluster_kacho_root`.
  jwtPureNoBindings      — the dedicated NEVER-granted subject (read-only negatives).
  baseUrl / internalBaseUrl / externalBaseUrl — injected by the newman runner.

The grant/revoke TARGET is NOT a shared fixture. It is a fresh, `{{runId}}`-scoped
user this suite seeds for itself via `InternalUserService.UpsertFromIdentity`.
Granting cluster-admin to a shared subject would be cross-suite contamination:
`jwtPureNoBindings` is precisely the "sees nothing" leak-guard other suites rely on,
and making it a cluster admin would silently invalidate them.

TWO SCENARIOS DELIBERATELY NOT REPRESENTED HERE (see the block at the bottom of this
file for the full argument and the exact Go tests that own them). Both were present
in the never-executed version of this file and both were unreachable as written.

Error-text discipline: messages assert with `.to.eql` (exact). Error text is part of
the Kachō contract. Do NOT weaken an assertion to make a run green — fix the code, or
report the finding.
"""

CASES = []

# A syntactically VALID but non-existent user id. The format is
# `^usr[0-9a-hjkmnp-tv-z]{17}$` (services/iam/.../cluster/helpers.go::subjectIDRe) —
# a 3-char `usr` prefix with NO underscore, 20 chars total, Crockford base32 body.
#
# This matters: the previous version of this file used `usr_nonexistent00000`, which
# has an underscore and therefore fails the FORMAT branch in grant_admin.go long
# before the user-existence lookup. It asserted the existence message
# ("User ... not found") against a response that could only ever carry the format
# message — an assertion that could never pass. The Go integration test
# TestCluster_6_12_GrantAdmin_UserNotInDB has the same defect
# (`usr_aaaaaaaaaaaaaaaaa`, 21 chars) and asserts the code only, so it passes while
# testing the wrong branch. UserExistenceChecker.ExistsUser therefore had NO
# coverage at any level; this id is what actually reaches it.
#
# NB the contract used to carry two machine-readable constraints on subject_id
# that CONTRADICTED EACH OTHER: a pattern demanding an underscore after the
# prefix, and a length cap one character shorter than that pattern's own shortest
# match. No input satisfied both, so the pair was unsatisfiable by construction —
# and neither was enforced anywhere, which is the only reason nobody noticed. The
# whole family was retired in kacho#1255; the implementation, which matches the
# real id format (`usrn3948g8m19j2mn8wr`), was and remains the only source.
ABSENT_USER_ID = "usr00000000000000zzz"


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-GET-OK
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="CLUSTER-ADMIN-GET-OK",
    title="GET /iam/v1/internal/cluster as cluster system_admin → 200, singleton id",
    classes=["CRUD", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="get-cluster-as-admin",
            method="GET",
            path="/iam/v1/internal/cluster",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('cluster id is the singleton cluster_kacho_root', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.id, JSON.stringify(j)).to.eql('cluster_kacho_root');",
                "});",
                "pm.test('cluster carries a createdAt timestamp (string, RFC3339)', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.createdAt, JSON.stringify(j)).to.be.a('string');",
                "  pm.expect(j.createdAt, JSON.stringify(j)).to.match(/^\\d{4}-\\d{2}-\\d{2}T/);",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-GET-403-ORDINARY
# ---------------------------------------------------------------------------
# The gateway authz middleware refuses BEFORE the request reaches kaname: the
# catalog entry `required_relation=system_admin` fails the OpenFGA Check against
# `cluster:cluster_kacho_root`. Non-admins must not observe the cluster resource.
CASES.append(Case(
    id="CLUSTER-ADMIN-GET-403-ORDINARY",
    title="GET /iam/v1/internal/cluster as a never-granted user → 403 PermissionDenied",
    classes=["NEG", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="get-cluster-as-ordinary",
            method="GET",
            path="/iam/v1/internal/cluster",
            mux="internal",
            auth="jwtPureNoBindings",
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-LIST-OK
# ---------------------------------------------------------------------------
# Shape contract of the denormalized ListAdmins projection. Every entry is checked
# (not just the first three): subjectId / subjectType / grantedAt are the fields the
# admin UI renders, and `grantedAt` being a STRING is the wire-level proof of the
# proto Timestamp→JSON mapping, which the Go tests (Go structs) cannot observe.
#
# Non-emptiness is deliberately NOT asserted here — it is a property of the seed,
# not of the RPC. CLUSTER-ADMIN-GRANT-OK below proves the list is populated
# dynamically, by putting a known subject into it and reading it back.
CASES.append(Case(
    id="CLUSTER-ADMIN-LIST-OK",
    title="ListAdmins as cluster system_admin → 200, every entry well-formed",
    classes=["CRUD", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="list-admins-as-admin",
            method="GET",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('admins is an array', () => {",
                "  const j = pm.response.json();",
                "  pm.expect(j.admins || [], JSON.stringify(j)).to.be.an('array');",
                "});",
                "pm.test('EVERY admin entry carries subjectId / subjectType / grantedAt', () => {",
                "  const j = pm.response.json();",
                "  (j.admins || []).forEach(a => {",
                "    pm.expect(a.subjectId, JSON.stringify(a)).to.be.a('string');",
                "    pm.expect(a.subjectId, JSON.stringify(a)).to.not.eql('');",
                "    pm.expect(a.subjectType, JSON.stringify(a)).to.be.a('string');",
                "    pm.expect(a.grantedAt, 'grantedAt must be a JSON string timestamp: ' + JSON.stringify(a)).to.be.a('string');",
                "  });",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-LIST-403-ORDINARY
# ---------------------------------------------------------------------------
# The roster itself is sensitive: knowing WHO is a cluster admin is targeting
# information. The gateway catalog is the ONLY gate on ListAdmins (list_admins.go
# carries no in-service requireClusterSystemAdmin, unlike grant/revoke), so this is
# the sole end-to-end proof that the gate fires on this path.
CASES.append(Case(
    id="CLUSTER-ADMIN-LIST-403-ORDINARY",
    title="ListAdmins as a never-granted user → 403 PermissionDenied (roster not observable)",
    classes=["NEG", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="list-admins-as-ordinary",
            method="GET",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtPureNoBindings",
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-GRANT-403-ORDINARY
# ---------------------------------------------------------------------------
# Privilege escalation guard: a non-admin must not be able to grant cluster-admin,
# least of all to itself.
CASES.append(Case(
    id="CLUSTER-ADMIN-GRANT-403-ORDINARY",
    title="GrantAdmin as a never-granted user → 403 PermissionDenied (no self-escalation)",
    classes=["NEG", "AUTHZ", "SEC"],
    priority="P0",
    steps=[
        Step(
            name="grant-as-ordinary",
            method="POST",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtPureNoBindings",
            body={"subjectType": "USER", "subjectId": ABSENT_USER_ID},
            test_script=[
                *assert_status(403),
                *assert_grpc_code(7, "PERMISSION_DENIED"),
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-GRANT-400-INVALID-USER
# ---------------------------------------------------------------------------
# Well-formed id that exists in no `kaname.users` row → the existence check in
# GrantAdmin.Execute returns ErrInvalidArg, mapped to InvalidArgument (grpc 3 →
# HTTP 400) with the canonical text. Synchronous: the check runs BEFORE the
# Operation row is persisted, so this is an HTTP-level 400, not an op.error.
CASES.append(Case(
    id="CLUSTER-ADMIN-GRANT-400-INVALID-USER",
    title="GrantAdmin with a well-formed but non-existent user id → 400 InvalidArgument",
    classes=["VAL", "NEG"],
    priority="P0",
    steps=[
        Step(
            name="grant-nonexistent-user",
            method="POST",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            body={"subjectType": "USER", "subjectId": ABSENT_USER_ID},
            test_script=[
                *assert_status(400),
                *assert_grpc_code(3, "INVALID_ARGUMENT"),
                *assert_error_message_eql(f"User {ABSENT_USER_ID} not found"),
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-SEED-TARGET-USER  (fixture, self-seeded)
# ---------------------------------------------------------------------------
# Provision this run's OWN grant/revoke target. `UpsertFromIdentity` is `<exempt>`
# at the gateway and lives on the same cluster-internal listener, so no extra
# fixture plumbing is needed. `{{runId}}`-scoped ⇒ the suite is re-runnable without
# UNIQUE collisions, and no shared subject's grant-state is ever mutated.
CASES.append(Case(
    id="CLUSTER-ADMIN-SEED-TARGET-USER",
    title="Seed a fresh, run-scoped User as the grant/revoke target (no shared-subject mutation)",
    classes=["SETUP"],
    priority="P0",
    steps=[
        Step(
            name="upsert-target-user",
            method="POST",
            path="/iam/v1/internal/users:upsertFromIdentity",
            mux="internal",
            auth="jwtBootstrap",
            body={
                "externalId": "cluster-admin-target-{{runId}}",
                "email": "cluster-admin-target-{{runId}}@kacho.local",
                "displayName": "Cluster Admin Target {{runId}}",
            },
            test_script=[
                *assert_status(200),
                "pm.test('target user id has the usr prefix', () => {",
                "  const j = pm.response.json();",
                "  const uid = (j.metadata && j.metadata.userId) || (j.user && j.user.id) || '';",
                "  pm.expect(uid, 'user id must match ^usr<17>: ' + JSON.stringify(j)).to.match(/^usr[0-9a-hjkmnp-tv-z]{17}$/);",
                "});",
                *save_from_response("j.id", "opId"),
                *save_from_response(
                    "(j.metadata && j.metadata.userId) || (j.user && j.user.id)",
                    "clusterTargetUserId"),
            ],
        ),
        # Poll to done: the user row must be COMMITTED before GrantAdmin looks it up,
        # otherwise the existence check races the LRO worker.
        poll_operation(op_var="opId", auth="jwtBootstrap", name="poll-upsert-target-user"),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-REVOKE-404-NOT-ADMIN
# ---------------------------------------------------------------------------
# Asymmetry with Grant idempotency: revoking a subject that is not an active admin
# is NOT a no-op, it is NotFound. Runs BEFORE the grant below, so the target is
# guaranteed never to have been an admin.
#
# The exact text is capital-U "User ..." (cluster_admin_grant_writer.go
# ::diagnoseRevokeMiss). The never-executed version of this file asserted lowercase
# "user ..." with `.to.eql` — it could not have passed. No Go test pins the text
# (they assert the code only), so nothing else would have caught the drift either.
CASES.append(Case(
    id="CLUSTER-ADMIN-REVOKE-404-NOT-ADMIN",
    title="RevokeAdmin a user who has never been an admin → 404 NotFound, exact text",
    classes=["NEG", "VAL"],
    priority="P0",
    steps=[
        Step(
            name="revoke-non-admin",
            method="DELETE",
            path="/iam/v1/internal/cluster/admins/{{clusterTargetUserId}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(404),
                *assert_grpc_code(5, "NOT_FOUND"),
                "pm.test('error message names the non-admin user verbatim', () => {",
                "  const j = pm.response.json();",
                "  const u = pm.environment.get('clusterTargetUserId');",
                "  pm.expect(j.message, JSON.stringify(j)).to.eql('User ' + u + ' is not an active cluster admin');",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-GRANT-OK
# ---------------------------------------------------------------------------
CASES.append(Case(
    id="CLUSTER-ADMIN-GRANT-OK",
    title="GrantAdmin → 200 Operation(iop) done=true, target appears in ListAdmins",
    classes=["CRUD", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="grant-target",
            method="POST",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            body={"subjectType": "USER", "subjectId": "{{clusterTargetUserId}}"},
            test_script=[
                *assert_status(200),
                *assert_iam_operation_envelope(),
                "pm.test('Operation metadata echoes the grant id and subject', () => {",
                "  const j = pm.response.json();",
                "  const md = j.metadata || {};",
                "  pm.expect(md.clusterAdminGrantId, JSON.stringify(j)).to.be.a('string');",
                "  pm.expect(md.subjectId, JSON.stringify(j)).to.eql(pm.environment.get('clusterTargetUserId'));",
                "});",
                *save_from_response("j.id", "opId"),
                *save_from_response("j.metadata && j.metadata.clusterAdminGrantId", "clusterGrantId"),
            ],
        ),
        poll_operation(op_var="opId", auth="jwtBootstrap", name="poll-grant"),
        Step(
            name="list-admins-includes-target",
            method="GET",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('ListAdmins includes the newly-granted target', () => {",
                "  const j = pm.response.json();",
                "  const u = pm.environment.get('clusterTargetUserId');",
                "  const hit = (j.admins || []).filter(a => a.subjectId === u);",
                "  pm.expect(hit.length, 'target not in roster: ' + JSON.stringify(j)).to.eql(1);",
                "});",
                "pm.test('the granted entry is enriched and typed USER', () => {",
                "  const j = pm.response.json();",
                "  const u = pm.environment.get('clusterTargetUserId');",
                "  const a = (j.admins || []).find(x => x.subjectId === u) || {};",
                "  pm.expect(a.subjectType, JSON.stringify(a)).to.eql('USER');",
                "  pm.expect(a.subjectEmail, 'subject email JOINed from users: ' + JSON.stringify(a))",
                "    .to.eql('cluster-admin-target-' + pm.environment.get('runId') + '@kacho.local');",
                "  pm.expect(a.grantedAt, JSON.stringify(a)).to.be.a('string');",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-GRANT-OK-IDEMPOTENT
# ---------------------------------------------------------------------------
# Re-granting an already-active subject succeeds and does NOT create a second row.
# The returned grant id must be the id of the EXISTING row — that is what makes it
# idempotent rather than merely tolerant.
CASES.append(Case(
    id="CLUSTER-ADMIN-GRANT-OK-IDEMPOTENT",
    title="GrantAdmin twice → success, SAME grant id, exactly one roster entry",
    classes=["CRUD", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="grant-target-again",
            method="POST",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            body={"subjectType": "USER", "subjectId": "{{clusterTargetUserId}}"},
            test_script=[
                *assert_status(200),
                *assert_iam_operation_envelope(),
                "pm.test('idempotent re-grant returns the EXISTING grant id', () => {",
                "  const j = pm.response.json();",
                "  const md = j.metadata || {};",
                "  pm.expect(md.clusterAdminGrantId, JSON.stringify(j))",
                "    .to.eql(pm.environment.get('clusterGrantId'));",
                "});",
                *save_from_response("j.id", "opId"),
            ],
        ),
        poll_operation(op_var="opId", auth="jwtBootstrap", name="poll-grant-idem"),
        Step(
            name="list-admins-no-duplicate",
            method="GET",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('roster still has exactly ONE entry for the target', () => {",
                "  const j = pm.response.json();",
                "  const u = pm.environment.get('clusterTargetUserId');",
                "  const n = (j.admins || []).filter(a => a.subjectId === u).length;",
                "  pm.expect(n, JSON.stringify(j)).to.eql(1);",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-REVOKE-OK
# ---------------------------------------------------------------------------
# Also this suite's cleanup: the run-scoped target leaves no active grant behind.
CASES.append(Case(
    id="CLUSTER-ADMIN-REVOKE-OK",
    title="RevokeAdmin → 200 Operation(iop) done=true, target no longer in ListAdmins",
    classes=["CRUD", "AUTHZ"],
    priority="P0",
    steps=[
        Step(
            name="revoke-target",
            method="DELETE",
            path="/iam/v1/internal/cluster/admins/{{clusterTargetUserId}}",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                *assert_iam_operation_envelope(),
                "pm.test('Operation metadata echoes the revoked grant id and subject', () => {",
                "  const j = pm.response.json();",
                "  const md = j.metadata || {};",
                "  pm.expect(md.clusterAdminGrantId, JSON.stringify(j))",
                "    .to.eql(pm.environment.get('clusterGrantId'));",
                "  pm.expect(md.subjectId, JSON.stringify(j)).to.eql(pm.environment.get('clusterTargetUserId'));",
                "});",
                *save_from_response("j.id", "opId"),
            ],
        ),
        poll_operation(op_var="opId", auth="jwtBootstrap", name="poll-revoke"),
        Step(
            name="list-admins-excludes-target",
            method="GET",
            path="/iam/v1/internal/cluster/admins",
            mux="internal",
            auth="jwtBootstrap",
            test_script=[
                *assert_status(200),
                "pm.test('revoked target is excluded from the active roster', () => {",
                "  const j = pm.response.json();",
                "  const u = pm.environment.get('clusterTargetUserId');",
                "  const found = (j.admins || []).some(a => a.subjectId === u);",
                "  pm.expect(found, JSON.stringify(j)).to.eql(false);",
                "});",
            ],
        ),
    ],
))


# ---------------------------------------------------------------------------
# CLUSTER-ADMIN-INTERNAL-NOT-ON-EXTERNAL-TLS — REMOVED, and here is the whole reason
# ---------------------------------------------------------------------------
# The invariant (ban #6: Internal* is never published on the advertised external TLS
# endpoint) is NOT dropped. It is checked, over gRPC, by
# deploy/scripts/assert-ban6-external-isolation.py, which enumerates EVERY
# `service Internal*` in proto/ — InternalClusterService among them, by construction
# rather than by a maintained list — dials the external listener under the widest
# principal the seed has, and requires a routing refusal for each. It also carries the
# three controls without which such a verdict means nothing: a public method on the SAME
# listener must answer (otherwise "unknown method" only proves the port is shut), each
# Internal method must answer on its owner's internal endpoint (otherwise the refusal
# proves the method exists nowhere), and a method removed from proto is a harness failure
# rather than a pass.
#
# WHAT THE CASE THAT STOOD HERE ACTUALLY DID. It POSTed to the external endpoint and
# asserted `if (code === undefined) { return; }` above the check — an unanswered request
# counted as a pass, with the comment declaring that unreachable means not exposed. On the
# live stand the request WAS unanswered, for a reason that has nothing to do with
# exposure: `unable to verify the first certificate` — newman was never given the internal
# CA. So the P0 probe passed without a single byte of evidence, and would have passed
# identically had the route been wide open. That the listener itself is fine was shown in
# the same run by 79 gRPC probes against it with full chain verification.
#
# WHY IT IS NOT REPAIRED IN PLACE. Handing newman the CA would fix the certificate, not
# the case: the external listener negotiates ALPN h2 and speaks ONLY gRPC. Measured
# 2026-07-29 on kind-kacho — a plain HTTP request there (including `--http1.1`) is closed
# after the headers with no HTTP response at all. A REST probe on that surface cannot be
# evidence IN PRINCIPLE; it can only ever produce "no answer", which is a broken harness,
# not a proof of isolation. That is precisely the reading this file already rejects for
# the two Go-covered scenarios below: a case that cannot enter its own branch is worse
# than no case.


# ===========================================================================
# NOT REPRESENTED HERE — two scenarios, with the proof of where they ARE covered
# ===========================================================================
#
# Both existed in the never-executed version of this file. Neither is dropped to
# make a run green: each is unreachable BY CONSTRUCTION from a machine-driven
# black-box harness, and each is covered at a strictly stronger level in Go.
#
# 1. RevokeAdmin SELF → FailedPrecondition "cannot revoke own cluster admin grant".
#
#    Unreachable: the guard is `subject_id == principal_id`
#    (cluster_admin_grant_writer.go::diagnoseRevokeMiss). RevokeAdmin only accepts a
#    subject matching `^usr[0-9a-hjkmnp-tv-z]{17}$`, so the caller must itself be a
#    USER principal holding cluster system_admin. Under the production posture this
#    suite targets, every harness principal is a ServiceAccount: prodseed_all.py
#    states it outright — "A human User principal with an `acr` requires the
#    interactive Kratos→Hydra login, which a machine harness cannot drive" — and
#    `jwtBootstrap` is minted for the bootstrap SA (bootstrap_token/mint.go). An SA
#    principal id can never equal a `usr…` subject, so the self-revoke branch cannot
#    be entered. A case that cannot enter its branch would either be permanently red
#    or pass on some OTHER error — the precise failure mode this cleanup exists to
#    remove.
#
#    Covered by: services/iam/internal/apps/kacho/api/cluster/handler_integration_test.go
#    ::TestCluster_6_07_RevokeAdmin_SelfRevoke (codes.FailedPrecondition + message)
#    and services/iam/internal/repo/kacho/pg/cluster_admin_grant_integration_test.go
#    ::TestRevoke_Self (ErrSelfRevoke sentinel + proof no row was mutated — stricter
#    than any black-box check could be).
#
# 2. RevokeAdmin LAST active admin → FailedPrecondition "cannot revoke last active
#    cluster admin".
#
#    Unreachable AND unsafe: migration 0058 seeds a bootstrap-SA grant, so a stock
#    stand always has count(*) > 1 and the guard never fires. The Go tests must call
#    `decommissionBootstrapSeedGrant` to reach it, and
#    cluster_admin_grant_integration_test.go::TestRevoke_LastUserAdmin_
#    BootstrapSAKeepsClusterAdministrable pins that behaviour deliberately. Driving a
#    SHARED dev stand into the last-admin state would mean revoking every other
#    cluster admin — i.e. deliberately making the cluster un-administrable underneath
#    every other suite. Not something an e2e run may do.
#
#    Covered by: cluster_admin_grant_integration_test.go::TestRevoke_LastAdmin_
#    Sequential, ::TestRevoke_ConcurrentLastAdmin and ::TestRevoke_ConcurrentLastAdmin_
#    WriteSkew (advisory-lock write-skew under concurrency — exactly one success, one
#    ErrLastAdmin, never zero admins), plus handler_integration_test.go
#    ::TestCluster_6_08a for the code+message.
#
# What the two of them jointly take with them is the FailedPrecondition→HTTP 400
# mapping on THIS service. That mapping is generic grpc-gateway behaviour, not a
# cluster-admin contract, and it is exercised by FAILED_PRECONDITION negatives across
# the vpc / compute / nlb suites. The cluster-specific REST mappings that ARE unique
# to this file — InvalidArgument→400 and NotFound→404 — are both kept above.
