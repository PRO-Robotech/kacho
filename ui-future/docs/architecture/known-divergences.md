# Known divergences — kacho-ui-future

Deliberate, reviewed deviations from a lint/style default. Each entry explains
why the deviation is intentional and not latent tech-debt, so audits do not
re-flag it.

## Client-side HIBP breach check is a progressive enhancement (not the enforcement point)

**Status:** accepted / by-design (best-effort UX; server-side is authoritative).

`shared/src/pages/auth/Register.tsx` runs a debounced client-side
have-i-been-pwned (HIBP) k-anonymity check (`checkHibp`) that `fetch`es
`https://api.pwnedpasswords.com/range/<SHA1-prefix>` and warns the user before
submit. The app's own CSP is `connect-src 'self'`
(`deploy/values.yaml` / the four `*/nginx.conf`), so **in the deployed image this
cross-origin fetch is blocked** and `checkHibp` fail-opens (its `catch` returns
`false`), so the inline warning does not render in production.

**Why this is intended, not a broken control:** the authoritative breach
rejection is enforced **server-side** by Kratos —
`kacho-deploy/.../kratos-config-configmap.yaml` sets
`password.config.haveibeenpwned_enabled: true` (host `api.pwnedpasswords.com`).
A breached password is rejected on submit and the Kratos flow message surfaces
through the existing error path (`err.ui?.messages?.[0]?.text` → `setError`). The
client check is a *progressive enhancement*: it fires only where CSP is absent
(local `vite` dev — no nginx header) to give an earlier hint, and degrades
silently where CSP is present because the server still rejects.

**Why the CSP is deliberately not relaxed for `pwnedpasswords.com`:** granting a
`connect-src` exception would (a) widen the strict egress allow-list of an
authenticated console to a third-party host and (b) leak SHA-1 password prefixes
from the app origin on every keystroke. Keeping `connect-src 'self'` and letting
Kratos (server-to-server) perform the HIBP lookup is the stronger posture. The
k-anonymity prefix scheme itself is correct (only 5 hex chars leave the browser,
never the password), so fail-open on the *hint* leaks nothing and loses no
enforcement.

**Revisit trigger:** if the client-side pre-warning is ever required to function
in production (e.g. a product decision to show it before submit), route the HIBP
lookup through a same-origin gateway endpoint rather than adding
`api.pwnedpasswords.com` to `connect-src`.

## CSP `style-src 'unsafe-inline'`

**Status:** accepted / bounded residual (not an exploitable defect).

The console's Content-Security-Policy (`deploy/values.yaml` → `security.contentSecurityPolicy`)
is otherwise strict — `script-src 'self'`, `object-src 'none'`, `base-uri 'self'`,
`frame-ancestors 'none'`, `form-action 'self'`, `connect-src 'self'`. Only
`style-src` is relaxed to `'self' 'unsafe-inline'`.

**Why it is required:** antd v6 styles components through a runtime CSS-in-JS
engine that injects `<style>` elements without a per-response nonce or a
build-time-stable hash. A nonce/hash-based `style-src` would break antd's runtime
styling. antd v6 does not currently expose a `StyleProvider` nonce integration
that nginx could feed via `sub_filter`.

**Why the risk is bounded:** `script-src` remains `'self'`, so no
attacker-controlled JavaScript can execute regardless of the style relaxation.
The residual is limited to CSS-only vectors (restyle/overlay of controls) and is
only reachable if a separate DOM-injection sink is introduced elsewhere — none is
known. The DPoP token flow, auth ceremony and API calls are unaffected.

**Revisit trigger:** drop `'unsafe-inline'` from `style-src` and adopt a
per-response nonce injected by the host nginx once antd exposes nonce-capable
style injection.

## `react-hooks/exhaustive-deps` line-level suppressions

**Status:** accepted / by-design (localized, not a blanket disable).

A number of `useEffect` / `useMemo` hooks in the remotes carry a
`// eslint-disable-next-line react-hooks/exhaustive-deps` comment with an
explicit, hand-picked dependency array. These are **deliberate** and are kept
line-scoped (never a file- or project-wide disable):

- **Keyed re-run effects** — e.g. operation pollers keyed on `[opId]` or
  `[op?.done, op?.error?.code, isError, opId]` (`lib/toast.ts`,
  `OperationToastWatcher`): the effect must re-run only when the operation
  identity/terminal-state changes, not when the (stable) toast/callback closures
  it references change. Listing those closures would re-fire the toast on every
  render.
- **Filter-derived list effects** — e.g. `ResourceListPage` keyed on
  `[items, query, zone, hasZoneFilter, spec.id]`: the effect derives view state
  from the current filter inputs; the omitted setter is React-stable.
- **Mount / URL-sync effects** in the auth and inline-form components that must
  run once for the initial hydrate and are intentionally not re-run on every
  dependency change.

**Why not "fix" them:** mechanically adding the missing dependencies (or wrapping
every referenced value in `useCallback`/`useRef`) would, for these specific
effects, re-introduce exactly the failure modes the suppression prevents —
duplicate toasts, redundant refetch storms, and in a couple of cases an infinite
render loop. Each site was reviewed and the dependency array is the intended
contract. New suppressions must stay line-scoped and come with a real,
intentional dependency array; a blanket rule-off is not permitted.

This entry supersedes audit finding "47 blanket eslint-disable
react-hooks/exhaustive-deps suppressions" (the count is now lower after the
vpc/iam shared-source extraction collapsed the duplicated copies to a single
source in `shared/src`).

## IAM management pages forked per remote (`vpc` vs `iam`)

**Status:** accepted / by-design (presentational fork), with an authorization
single-source invariant enforced by test.

The IAM screens — Access Bindings, Access, Groups, Roles, Users — exist as
independent component implementations in both remotes:
`vpc/src/pages/iam/<Page>.tsx` and `iam/src/pages/iam/<Page>/<Page>.tsx`.

**Why they are not one shared component:** the two remotes use deliberately
different create/edit UX wired to different route tables:

- The **iam** remote registers dedicated `/iam/<resource>/create` and
  `/iam/<resource>/:id/edit` routes (see `iam/src/pages/IamPage/IamPage.tsx`) and
  its pages `navigate()` to them; it also integrates the IAM account-selector
  context (`selectedAccount`) that only exists in the iam shell.
- The **vpc** remote has **no** such create/edit routes; its IAM pages create and
  edit in-place via antd `Modal`s (`GroupCreateModal`, `AccessBindingCreateModal`,
  …). It hosts IAM screens only as a convenience surface.

Collapsing both into a single `shared/` component would force one remote to adopt
the other's routing model (e.g. vpc would `navigate()` to a create route it never
registers → catch-all redirect), a runtime behavior change that cannot be
validated without an end-to-end federation harness. The fork is therefore
intentional and scoped to **presentation/routing only**.

**Why the security risk is neutralized:** every security-relevant primitive is
already single-sourced in `@shared` and consumed identically by both copies:

- permission gating — `@shared/lib/permissions` (`usePermissions`),
- IAM mutations + typed API — `@shared/components/organisms/iam/IamCommon`
  (`useIamMutation`) and `@shared/api/iam`,
- error mapping — `@shared/lib/permissions`
  (`isAlreadyExistsError`, `mapApiErrorToMessage`),
- session — `@shared/contexts/AuthContext`.

A fix to any of those lands once and applies to both remotes. The audit failure
scenario ("security fix applied to one copy, missed in the other") is prevented
by `shared/src/test/iam-pages-authz-single-source.test.ts`, which fails CI if any
IAM page in either remote stops sourcing the gating/mutation/API from `@shared`
or re-declares a local `usePermissions` / `useIamMutation`. The remaining
per-app difference is limited to the modal-vs-route create shell and the
iam-only `selectedAccount` gate, neither of which is an authorization decision
(the backend enforces authz; the UI gate is defense-in-depth/UX).

**Revisit trigger:** if a future task unifies the two remotes' IAM routing model
(both route-based or both modal-based), extract the shared page bodies into
`shared/src/pages/iam/` behind a thin per-app create-shell and drop the fork.

## `resource-registry.tsx` size (single central REGISTRY)

**Status:** accepted / deferred residual (cosmetic size; no security or
behavioral defect).

`shared/src/lib/resource-registry.tsx` is 3805 lines, dominated by one
`REGISTRY: Record<string, ResourceSpec>` object literal. Its **contents** drive
every list column, detail view and create/edit form of **four** packages — iam,
system, vpc and shared itself.

> [!warning] Here stood "**eight** remotes … compute, iam, nlb, registry,
> storage, system, vpc and shared itself" — and four of those eight were false
> (KAC #132, corrected 2026-08-10)
>
> compute, nlb, registry and storage do **not** consume this REGISTRY. Each
> carries its own (`compute` 471 lines, `nlb` 754, `registry` 348, `storage`
> 507), with its own spec contents — and `snapshots` / `images` / `registries` /
> `repositories` / `tags` exist **only** there, so the central file was never
> their source in any sense.
>
> The claim mattered, and not as a typo. While the doc said one registry drove
> all eight, the **form** of a resource (`ResourceSpec` / `ResourceColumn` /
> `ListFilter`, and the `FormField` family behind them) was in fact declared
> five times — once centrally and once per private copy — and the copies drifted
> in **both** directions: `admin` / `mutationsReturnOperation` / `listFilters`
> only centrally, `facet` / `loadAllPages` only privately, and the comment on
> `immutable?` in three mutually inconsistent revisions of which one was right.
> Nobody looked, because the doc said there was nothing to look at.
>
> Measure it, don't remember it: `wc -l`; consumers by
> `grep -rln '@shared/lib/resource-registry' ui-future/*/src`; owners of a
> private registry by `git ls-files | grep 'src/lib/resource-registry\.tsx$'`.
> The previous revision already carried this warning about *its* predecessor
> ("named two remotes … drifted in the direction of smaller and narrower than it
> is") and then drifted the other way, naming four consumers it did not have.
> A count in prose has no producer; the predicate does.

**The form is single-sourced; the contents are not.** Since KAC #132 the types
live in `shared/src/lib/resource-spec.ts` (spec/column/filter) and
`shared/src/lib/form-schema.ts` (field family); all five registries import them
and re-export for their own consumers. Re-declaring any of those names anywhere
else is a finding of `scripts/check-resource-spec-single-source.mjs`, which walks
the tracked tree, derives the guarded names **from** the canonical modules (a
hand-written list would be a sixth copy of the form) and reads the AST, so a
re-export — no body, cannot drift — stays legal while a declaration does not.
Splitting the *contents* remains deferred, below.

**Why it is not split in this security pass:** every REGISTRY entry references a
shared set of in-file primitives (`COL_NAME`/`COL_ID`/`COL_CREATED`,
`FIELD_NAME`/`FIELD_PROJECT_ID`/`FIELD_ACCOUNT_ID`/…, and the `sanitizeSgRule` /
`sanitizeInstanceCreate` / `fmtBytesGiB` helpers). Splitting the object per
domain (one module per remote) requires exporting all of those primitives and
re-wiring imports across the most safety-critical shared file in the codebase. The change is purely organizational (CWE-1121 size, not a
defect) and carries no security or behavioral benefit, while a mis-wired spec
reference would regress rendering in a way the current export-name smoke tests
would not catch and which cannot be validated without an end-to-end federation
UI harness. Under the "keep build green" mandate of the hardening pass the
risk/value trade does not justify it here.

**Planned split (follow-up, behavior-preserving).** File names below are written
as prose on purpose: none of these modules exists yet, and a name in backticks
reads as a coordinate that the next contributor will go looking for.

1. a primitives module exporting the shared column/field consts;
2. a sanitizers module holding `sanitizeSgRule`, `sanitizeInstanceCreate`,
   `fmtBytesGiB`, `gibToBytes` (these four functions do exist today, in the single
   file);
3. one module per remote, each exporting its slice of specs;
4. the current file (or an index beside it) composing the slices back into
   `REGISTRY` and keeping the public helpers — `getResource`,
   `resourceServicePrefix`, `resourceProjectPath`, `applyFieldDefaults`,
   `getByPath` — so importers stay unchanged. Land behind snapshot tests of the
   composed `REGISTRY` keys.

## `getByPath` re-exported by a registry: two implementations, one latent gap

**Status:** measured, latent (no input in the tree reaches the difference), not
fixed here — it is behaviour, and KAC #132 unified the *form* only.

Each of the five registries re-exports a `getByPath`, and they are **not** the
same function. `shared` delegates to `getByPath` in its own `path.ts`, which
parses the path through `parsePath` and therefore resolves array indices
(`spec.rules[0].direction` — the very shape `FormField.name` documents). The four
private registries (compute, nlb, registry, storage) instead carry an **inline**
re-implementation that does `path.split(".")` and so cannot resolve an index at
all: it would return `undefined` where `shared` returns the value.

**Why it is not a live defect today, stated as a measurement rather than a
reassurance:** no spec in any of the five registries uses an indexed path —
`grep -oE '(path|name): "[^"]*\[[0-9]+\][^"]*"' <pkg>/src/lib/resource-registry.tsx`
returns 0 for all five, `shared` included (2026-08-10). Both implementations
agree on every input the tree actually produces, which is exactly why the
difference has never shown up and why it will not announce itself.

**Why it is written down instead of left alone:** this is the same class KAC #132
closed for the form — a duplicated surface nobody compares — and the fix there
does not cover it, because the single-source gate guards *type declarations*, not
function bodies. The trap arms itself the day someone adds an indexed `path` to a
compute/nlb/registry/storage spec: the column silently renders empty in four apps
and correctly in the other four. Note also that `path.ts` itself is byte-identical
in all five packages (`md5` of the `getByPath` body agrees), so the divergence is
not between the `path.ts` copies — it is the registry's inline duplicate that
drifted away from the helper sitting next to it.

**Closing it** means deleting the inline body and delegating to `./path`, in all
four, behind a probe that feeds an indexed path and asserts the resolved value —
a failing test first, per the test-first rule, since this changes behaviour.

## `react-hooks/exhaustive-deps` count after shared-source extraction

The prior audit's "pervasive exhaustive-deps suppressions" finding remains
covered by the dedicated entry above. The sec-hardening-r3 extraction of the
Resource CRUD organisms into `shared/src/components/organisms/*` further reduced
the suppression count by collapsing the duplicated vpc/iam copies to one source.

## Destructive/move operations are read-only stubs in the console

**Status:** accepted / by-design (scoped, non-destructive-by-default console).

The console deliberately does **not** perform destructive or ownership-moving
mutations from the UI. Three surfaces are intentional, fully-implemented "stub"
components that render the equivalent REST/`kachoctl` invocation instead of
issuing the call:

- `vpc/src/components/molecules/DeleteConfirmStub` — "Удаление через UI отключено";
  shows the `DELETE <apiPath>` the operator can run.
- `shared/src/components/molecules/MoveStubDialog` — "Перемещение через UI пока не
  реализовано"; shows the `POST <apiPath>:move` body.
- `shared/src/components/organisms/OperationsTab` — renders a 404 sub-title when a
  resource's `ListOperations` is not yet exposed, rather than faking a list.

**Why it is not latent tech-debt:** this is a deliberate safety posture for the
current console, not deferred work smuggled past rule #11. Delete and move are the
highest-blast-radius mutations; keeping them out of the point-and-click surface
means an operator must reach for the API or `kachoctl` (auditable, scriptable,
harder to fat-finger) while the read/create/edit flows the console does own stay
fully functional. Server-side authz still gates every one of those API calls; the
UI omission is a UX/blast-radius decision, not an authorization one.

**Why these are complete, not half-built:** each stub is a finished component with
its own unit test (`DeleteConfirmStub.test.tsx`, `MoveStubDialog.test.tsx`) — they
have no `TODO`/`FIXME` and no dead branches. They are the intended terminal state
for this console iteration, not a placeholder awaiting wiring.

**Revisit trigger:** when a destructive-op UX (typed-name confirm for delete, a
target-Project picker for move) is scoped as its own tracked task, replace the
corresponding stub with the real flow behind that ticket; until then the stub is
the reviewed, intended behavior.

## Shell apps (`host`, `dashboard`) keep private auth/api helpers outside `@shared`

**Status:** accepted / bounded residual (tiny identical logic, drift-guarded by
per-copy unit tests).

The two federation **shell** apps — `host` (the outer console shell) and
`dashboard` — are not members of the `@shared` remote-source workspace (root
`package.json` `workspaces` = `shared`/`vpc`/`iam`) and deliberately do **not**
consume the `@shared/*` alias. Each carries a small private copy of:

- `src/utils/auth.ts` — Kratos login-redirect + `isAuthRoute` guard
  (byte-identical between host and dashboard).
- `src/utils/api-client.ts` — minimal `apiGet`/`apiList` fetch wrapper with a
  401→login redirect and a defensive JSON parse (identical between host and
  dashboard).
- `src/utils/host-context.ts` — host-context bootstrap; the host copy is a
  **superset** (adds `localStorage` persistence + a shell-context reset on
  `/` and `/accounts`). The two are legitimately different, not a fork.

**Why not one `@shared` module:** `@shared` is the source-of-truth for the
`vpc`/`iam` **remotes**, wired via the `@shared/*` vite/tsconfig/jest alias into
those packages only. The shells ship a *different, minimal* fetch helper (a
lightweight `apiGet`, not the typed `ApiError` client in
`shared/src/api/client.ts`) for their narrow needs (reachability probe,
host-context bootstrap). Extending the `@shared` alias into two federation
**host** apps (vite + tsconfig + jest × 2) to fold in ~25 lines of identical
redirect code is a structural change to the shells' build wiring that cannot be
validated without an end-to-end federation harness, and the full typed client
would over-serve the shells.

**Why the residual is bounded:** the duplicated logic is security-sensitive but
tiny and identical, and the 401→login redirect is unit-tested in **both** copies
(`host/src/utils/api-client.test.ts`, `dashboard/src/utils/api-client.test.ts`,
plus `auth.test.ts` in each), so a drift in the redirect/parse behavior fails CI.
The sec-hardening-r8b pass reconciled the one real drift that had crept in — the
`dashboard` `api-client.ts` ran `JSON.parse` **before** the `401` branch, so a
non-JSON 401 body (an nginx/gateway HTML error page) threw a `SyntaxError` and
masked the login redirect; both copies now carry the host's defensive
parse-after-redirect and the matching regression test.

**Revisit trigger:** if a third shell app appears, or the shells grow to need the
typed `ApiError` client, promote `auth.ts` + `api-client.ts` into
`shared/src/utils` and extend the `@shared` alias to `host`/`dashboard`, deleting
the private copies.
