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

## IAM management pages forked per remote (`vpc` vs `iam`) — RESOLVED

**Status:** resolved / fork removed. Управление доступом реализует **только**
ремоут `iam`; `/iam/*` маршрутизирует только `host` и только в этот ремоут.

Раздвоение было записано здесь как by-design: экраны выдач/доступа/групп/ролей/
пользователей существовали двумя независимыми реализациями (`vpc/src/pages/iam/…`
и `iam/src/pages/iam/…`), а риск считался снятым тем, что security-значимые
примитивы (`@shared/lib/permissions`, `@shared/…/IamCommon`, `@shared/api/iam`)
единственны и потребляются обеими копиями одинаково.

**Почему запись снята.** Инвариант проверял ПРОИСХОЖДЕНИЕ импортов, а разошлось
ПОВЕДЕНИЕ, и разошлось до неработоспособности: форма создания роли в копии `vpc`
слала `permissions` — поле выводимое, только на выход, которое сервис отвергает
первым же стейтментом создания, — и не слала `rules[]`, без которых роль не
создаётся вовсе. Две независимые причины отказа; создать роль оттуда было
нельзя. Правильная форма (редактор правил + каталог разрешений) всё это время
жила в ремоуте `iam`.

При этом копия была недостижима из продукта: хост федерирует `/iam/*` в ремоут
`iam`, а ремоут `vpc` отдаёт наружу только `./VpcPage`, так что экраны
открывались лишь в standalone-сборке `/vpc-remote/index.html`. То есть вторая
поверхность существовала, расходилась и ломалась, никого не обслуживая, —
довод «convenience surface» её не оправдывал.

Довод «свести в один компонент нельзя, потому что модели маршрутизации разные»
верен, но решает не ту задачу: раз поверхности две быть не должно, сводить
нечего — лишняя удалена целиком (страницы, `IamLayout`, `IamScopedListShell`,
раздел `/iam/*` в роутере `vpc`, лаунчер IAM в его сайдбаре).

**Инвариант теперь** — `shared/src/test/iam-pages-authz-single-source.test.ts`:
IAM реализует только `iam`; никакое другое приложение не держит своей копии;
`/iam/*` маршрутизирует только `host` и только в ремоут. Прежняя гарантия про
единственность security-примитивов сохранена — она нужна оставшейся поверхности.

## `resource-registry.tsx` size (single central REGISTRY)

**Status:** accepted / deferred residual (cosmetic size; no security or
behavioral defect).

`shared/src/lib/resource-registry.tsx` is ~2840 lines, dominated by one
`REGISTRY: Record<string, ResourceSpec>` object literal (~lines 187-2612). It is
the single source of truth that drives every list column, detail view and create
/edit form across **both** the vpc and iam remotes.

**Why it is not split in this security pass:** every REGISTRY entry references a
shared set of in-file primitives (`COL_NAME`/`COL_ID`/`COL_CREATED`,
`FIELD_NAME`/`FIELD_PROJECT_ID`/`FIELD_ACCOUNT_ID`/…, and the `sanitizeSgRule` /
`sanitizeInstanceCreate` / `fmtBytesGiB` helpers). Splitting the object per
domain (`vpc.ts` / `iam.ts` / `compute.ts` / `nlb.ts`) requires exporting all of
those primitives and re-wiring imports across the most safety-critical shared
file in the codebase. The change is purely organizational (CWE-1121 size, not a
defect) and carries no security or behavioral benefit, while a mis-wired spec
reference would regress rendering in a way the current export-name smoke tests
would not catch and which cannot be validated without an end-to-end federation
UI harness. Under the "keep build green" mandate of the hardening pass the
risk/value trade does not justify it here.

**Planned split (follow-up, behavior-preserving):**
1. `resource-registry/primitives.ts` — export the shared column/field consts.
2. `resource-registry/sanitizers.ts` — `sanitizeSgRule`, `sanitizeInstanceCreate`,
   `fmtBytesGiB`, `gibToBytes`.
3. `resource-registry/{vpc,iam,compute,nlb}.ts` — each exports its slice of specs.
4. `resource-registry.tsx` (or `index.ts`) — composes the slices into `REGISTRY`
   and keeps the public helpers (`getResource`, `resourceServicePrefix`,
   `resourceProjectPath`, `applyFieldDefaults`, `getByPath`) so importers are
   unchanged. Land behind snapshot tests of the composed `REGISTRY` keys.

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
