# ACR step-up floor — the 41-set (SEC-acr-stepup-refinement)

**Status:** landed (redesign/integration). **Acceptance:** `sub-phase-SEC-acr-stepup-refinement-acceptance.md` (R3, APPROVED by acceptance-reviewer + system-design-reviewer). **Relates:** #59 (production-newman Phase C), #60 (SA-caller user-token seed).

## Decision

`required_acr_min="2"` (step-up MFA / AAL2 floor) is required **iff** the operation:

1. **mints/destroys a credential** (bearer token, SA key); **or**
2. **creates/modifies/removes a privilege-grant OR a live authorization-policy artifact** through which a grant resolves (binding, group-membership, role policy, condition) — i.e. it immediately changes some existing subject's effective privilege, **domain-agnostic**; **or**
3. **irreversibly destroys a tenancy-root** (account/project — cascade + deletion-protection).

Every other non-exempt RPC is routine `required_acr_min="1"` (ordinary AAL1 auth). This follows RFC 9470 (OAuth 2.0 Step-Up Authentication Challenge §1) and NIST SP 800-63B (raise AAL for authenticator/privilege changes, not for routine access).

Before this refinement the generator default `"2"` was inherited by ~372/438 RPC, so step-up MFA was demanded on **every** resource `Get`/`List`/`Create`/verb of every domain — breaking non-interactive automation and the entire production-newman user-subject surface (a user token carries `acr<=1`).

## End-state

> **The counts below are a MEASUREMENT of the embedded catalog, with its revision.**
> They are re-read from the file, not carried forward from the previous edit —
> the numbers this section used to state (`41 × "2"` + `332 × "1"` + `65 × ""` =
> 438) had drifted away from the artifact well before the change that touched it
> last: on `27cc2c4e`, before any of this edit, the catalog held **300** entries,
> not 438. The discrepancy is recorded rather than quietly overwritten, because a
> stale number in a security document is read as a fact about the tree.

Measured on the retirement of the tenant condition surface:

| | entries | `"2"` | `"1"` | `""` |
|---|---|---|---|---|
| before (`27cc2c4e`) | 300 | 26 | 211 | 63 |
| after | 294 | 24 | 207 | 63 |

The six removed entries are exactly `ConditionsService/{Get,List,Create,Update,Delete,Evaluate}`;
nothing was added. Both embedded catalog copies (gateway `gateway/internal/middleware/embed/` + iam `services/iam/internal/apps/kaname/seed/embedded/`) are byte-identical (CI gate `make permission-catalog-check`).

The sensitive set, enumerated from the file rather than recalled (24 entries): 4 credential
(UserToken Issue/Revoke, SAKey Issue/Revoke) · 4 AccessBinding Create/Update/Delete/Revoke ·
3 Group AddMember/RemoveMember/**Delete** · 2 Role Update/Delete · 2 InternalCluster
Grant/RevokeAdmin · 2 Account/Project Delete · 2 ServiceAccount Disable/Enable · 2 User
Block/Unblock · 1 User Invite · 2 compute InstanceService Set/UpdateAccessBindings.

Two corrections this enumeration forces, both about the PREVIOUS text rather than about this
change: the compute grant surface is **2** entries, not the 22 that section claimed, and
`UserService/Invite` was in the set without being named at all. Category F (Conditions
Update/Delete) is now **gone** — the tenant-facing condition resource was retired, so there is
no policy artifact of that kind left to step up for.

## Boundary decisions (ratified)

- **B1 — AccessBindingService/Create = net-strengthening.** `permission="<exempt>"` (the scope-Check stays skipped, handler `requireGrantAuthority` is the precise gate) **and** `required_acr_min="2"` — the two catalog fields are orthogonal; `StepUpGate` keys on FQN+acr, not scope. Adding acr=2 closes the "create-a-new-binding instead of Update/Delete/Revoke" step-up bypass without touching the relation model.
- **B2 — Group membership + Group/Delete = sensitive.** A non-empty group's membership materializes/revokes its bindings' privileges. `GroupService/Delete` is **revoke-by-all** (cascade `group_members` + cleanup of group-targeted `AccessBinding.subject_id`) — strictly more impactful than `RemoveMember`, same destructive-revoke class as `RoleService/Delete`.
- **B3 — subject-delete = routine.** `ServiceAccountService/Delete`, `UserService/Delete` are neither grant, credential-destroy, nor tenancy-root cascade. Lockout symmetry is preserved by keeping `UserTokenService/Revoke` + `SAKeyService/Revoke` sensitive (A).
- **B5 — non-iam `Internal*`-admin (42) = routine.** Admin-curated platform-catalog / data-plane-wiring mutations are posture-neutral; still gated by the `system_admin`/`system_viewer` relation check + mTLS + (for module-SA callers) the O-1 acr-exemption.
- **B6 — author-inert create = routine.** `RoleService/Create`, `GroupService/Create` produce an inert artifact (no holders / no referencing bindings / empty group) — access is conferred only through a now-sensitive grant verb.

## Enforcement & fail-safe

Two enforcement points, **one implementation**: the public gateway `StepUpGate.Check` (RFC 9470 `401` + `WWW-Authenticate: acr_values`) and the iam internal `authzguard.ACRFloor` (:9091, gateway-fronted internal RPCs → `PERMISSION_DENIED` + step-up detail) both call `grpcsrv.EvaluateStepUp`, which owns the ACR ranking, the MFA-freshness window and the machine-principal exemption. Neither point re-derives any arm of it.

It has to live under `pkg/` because `gateway/internal/...` and `services/iam/internal/...` cannot import each other; `grpcsrv` specifically, because it already owns every input of the decision (`ACRRank`/`ACRSatisfies`, `TrustedACRFromContext`/`TrustedPrincipalFromContext`, `MDKeyTokenACR`/`MDKeyPrincipalType`).

Each point carries a verdict-parity guard pinning its REAL entrypoint to the shared rule over `{principal type}×{presented}×{required}` (+ freshness on the public side): `gateway/internal/middleware/stepup_verdict_parity_test.go` and `services/iam/internal/authzguard/acr_floor_stepup_parity_test.go`. Equality with a common reference gives equality with each other.

> Superseded design: the halves previously kept **two separate ranking tables** (`middleware.acrRank` vs `grpcsrv.ACRRank`) with parity asserted only over `{presented}×{required}`. That guard built a non-machine token, so it never walked the one axis on which the halves actually disagreed — the gateway exempted machine principals and the iam floor did not, permanently denying service accounts on the acr-gated credential/grant RPCs. Machine rows are now mandatory in both guards.

**Fail-safe is layered, and scoped to non-exempt RPC:**
- The generator injects an explicit `required_acr_min="2"` for every **non-exempt** un-annotated RPC at gen-time (so a new non-exempt privileged RPC fails closed by default). Downgrade to routine is an **explicit `"1"`**, never deletion of the entry.
- Catalog **completeness** (`"no entry for method" → AUTHZ_DENIED`) is the backstop for genuinely un-cataloged methods.
- The step-up layer itself treats an absent floor as "no floor" — intentional, because the two layers above are what make the net posture fail-closed for non-exempt RPC. The floor layer is not a second gate; it is the gate for RPCs that declare one.
- **Exempt carve-out.** An exempt RPC is outside both backstops **by construction**, not by oversight: it declares no floor, and it relies instead on authN plus the in-handler access verdict plus the deliberate scope-exempt posture. The consequence is what matters for practice: **adding a new exempt RPC is a high-scrutiny action**, because exempting an RPC removes it from the layers that would otherwise carry it, and nothing downstream re-adds it. Where an exempt RPC still needs a floor, set one explicitly — `AccessBindingService/Create` is the pattern.

## Out of scope (unchanged)

Relation-authz / `required_relation` / `scope_extractor` / `permission` values; the machine-principal exemption (O-1) and its predicate; ACR-minting / IdP config; `mfa_max_age`; the acr-floor mechanism (5.4). This refinement changes only `required_acr_min` values + three doc-truthfulness godoc fixes + a verdict-parity lock test.

> [!note] Why the exemption predicate is not printed here
> `security.md` §"Публичные артефакты" uses **this exact mechanism** as its worked example of
> what must not be published — a consolidated statement of which claim value causes a
> step-up floor to be skipped is a bypass recipe regardless of the code being open. The
> exemption itself is deliberate and documented (machine principals cannot perform an
> interactive step-up, so gating them on one would make automation impossible, not safer);
> what is withheld is only the key/value shape. The parity guard below is what keeps both
> enforcement points agreeing about it.
