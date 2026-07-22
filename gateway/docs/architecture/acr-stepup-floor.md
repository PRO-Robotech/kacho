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

`41 × "2"` + `332 × "1"` + `65 × ""` = 438. Both embedded catalog copies (gateway `internal/middleware/embed/` + iam `services/iam/.../seed/embedded/`) are byte-identical (CI gate `make permission-catalog-check`).

The sensitive-41 (categories A–H): 4 credential (UserToken/SAKey Issue+Revoke) · 4 iam AccessBinding Create/Update/Delete/Revoke · 22 compute `Set/UpdateAccessBindings` (non-iam grant surface — refutes any "no non-iam acr=2" assumption) · 3 Group AddMember/RemoveMember/**Delete** · 2 Role Update/Delete · 2 Conditions Update/Delete · 2 InternalCluster Grant/RevokeAdmin · 2 Account/Project Delete.

## Boundary decisions (ratified)

- **B1 — AccessBindingService/Create = net-strengthening.** `permission="<exempt>"` (FGA scope-Check stays skipped, handler `requireGrantAuthority` is the precise gate) **and** `required_acr_min="2"` — the two catalog fields are orthogonal; `StepUpGate` keys on FQN+acr, not scope. Adding acr=2 closes the "create-a-new-binding instead of Update/Delete/Revoke" step-up bypass without touching FGA.
- **B2 — Group membership + Group/Delete = sensitive.** A non-empty group's membership materializes/revokes its bindings' privileges. `GroupService/Delete` is **revoke-by-all** (cascade `group_members` + cleanup of group-targeted `AccessBinding.subject_id`) — strictly more impactful than `RemoveMember`, same destructive-revoke class as `RoleService/Delete`.
- **B3 — subject-delete = routine.** `ServiceAccountService/Delete`, `UserService/Delete` are neither grant, credential-destroy, nor tenancy-root cascade. Lockout symmetry is preserved by keeping `UserTokenService/Revoke` + `SAKeyService/Revoke` sensitive (A).
- **B5 — non-iam `Internal*`-admin (42) = routine.** Admin-curated platform-catalog / data-plane-wiring mutations are posture-neutral; still gated by `system_admin`/`system_viewer` ReBAC + mTLS + (for module-SA callers) the O-1 acr-exemption.
- **B6 — author-inert create = routine.** `RoleService/Create`, `ConditionsService/Create`, `GroupService/Create` produce an inert artifact (no holders / no referencing bindings / empty group) — access is conferred only through a now-sensitive grant verb.

## Enforcement & fail-safe

Two runtime readers of the SAME catalog value: the public gateway `StepUpGate.Check` (RFC 9470 `401` + `WWW-Authenticate: acr_values`) and the iam internal `authzguard.ACRFloor` (:9091, gateway-fronted internal RPCs → `PERMISSION_DENIED` + step-up detail). They use **two separate, functionally-identical ranking tables** (`middleware.acrRank` vs `grpcsrv.ACRRank`), NOT a shared function — parity is locked by a verdict-parity test over the full `{presented}×{required}` matrix (`SEC-ACR-16`), so they cannot drift.

**Fail-safe is layered, and scoped to non-exempt RPC:**
- The generator injects an explicit `required_acr_min="2"` for every **non-exempt** un-annotated RPC at gen-time (so a new non-exempt privileged RPC fails closed by default). Downgrade to routine is an **explicit `"1"`**, never deletion of the entry.
- Catalog **completeness** (`"no entry for method" → AUTHZ_DENIED`) is the backstop for genuinely un-cataloged methods.
- The step-up layer itself **fails open** on an empty `RequiredACRMin` (`if req.RequiredACRMin != ""` guard) — this is intentional; the two layers above provide net fail-closed for non-exempt RPC.
- **Exempt carve-out:** an exempt un-annotated RPC gets an EMPTY acr (the generator's exempt short-circuit returns before default-injection), so neither backstop fires for it — it relies on authN + in-handler ReBAC + the deliberate FGA-exempt posture. **Adding a new exempt RPC is a high-scrutiny action** (see AccessBindingService/Create for the explicit-acr pattern).

## Out of scope (unchanged)

FGA relation-authz / `required_relation` / `scope_extractor` / `permission` values; SA acr-exemption (O-1, `kacho_principal_type=="service_account"`); ACR-minting / IdP config; `mfa_max_age`; the acr-floor mechanism (5.4). This refinement changes only `required_acr_min` values + three doc-truthfulness godoc fixes + a verdict-parity lock test.
