# Known divergences — kacho-ui-future

Deliberate, reviewed deviations from a lint/style default. Each entry explains
why the deviation is intentional and not latent tech-debt, so audits do not
re-flag it.

## Client-side HIBP breach check: the console no longer has one

**Status:** subject removed (#1225); the CSP decision below stands on its own.

The console **used to** carry a debounced client-side have-i-been-pwned (HIBP)
k-anonymity check on its own registration page — it `fetch`ed
`https://api.pwnedpasswords.com/range/<SHA1-prefix>` and warned before submit.
That page is gone: the console never mounted it by any route, and registration
belongs to the identity provider, which serves the address (see
`shared/src/pages/auth/README.md`). So there is nothing left to fail open, and
this entry is kept only because the reasoning under it is still load-bearing:
it is the recorded argument for **not** widening the egress allow-list. The
app's CSP is `connect-src 'self'`
(`deploy/values.yaml` / the five `*/nginx.conf` that emit it — count measured
2026-08-12 by `git grep -lI Content-Security-Policy -- '*nginx.conf'`, not
remembered: here stood «four», and it was already wrong when written — the same
commit that carried the sentence in also carried the fifth file), so **in the deployed image this
cross-origin fetch would be blocked** in the deployed image anyway — which is
why removing the page cost no enforcement.

**Where the control actually lives:** the authoritative breach
rejection is enforced **server-side** by Kratos —
`deploy/.../kratos-config-configmap.yaml` sets
`password.config.haveibeenpwned_enabled: true` (host `api.pwnedpasswords.com`).
A breached password is rejected on submit and the Kratos flow message surfaces
through the provider's own flow UI. The client check never was the enforcement
point — it was a hint that fired only where CSP is absent (local `vite` dev, no
nginx header).

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

**Запись гейта:** `csp:style-src-unsafe-inline` — держит `scripts/check-csp-divergence-records.mjs`.
**Измерено против:** `antd@^6.3.7`.

Запись держится гейтом, а не памятью автора: он краснеет, когда послабления в
дереве больше нет (принимать нечего — снять запись) и когда пин UI-набора уехал
(измерение про рантайм-движок стилей той версии больше не относится к дереву).
Обоснование ниже — про **v6**; пин и есть срок годности этого обоснования.

> [!important] Перемерено дважды, и оба раза гейтом, а не памятью: одно из оснований УСТАРЕЛО
> Пин уезжал в `^6.6.0` вместе с подъёмом сборочной цепочки и вернулся в `^6.3.7`, когда
> подъём был откачен (сквозные пробы консоли краснели, см. #309). Гейт покраснел на ОБА
> движения — как и задуман. Существо записи от этого не изменилось: утверждение ниже
> перепроверено на разрешённой версии диапазона (`antd@6.5.4`, движок стилей
> `@ant-design/cssinjs@2.1.2`) — интеграция nonce на месте.
>
> Перемер показал, что запись держалась
> отчасти на утверждении, которого дерево больше не подтверждает. Прежняя редакция говорила:
> «antd v6 не выставляет интеграцию nonce, которую nginx мог бы накормить через `sub_filter`».
> **Выставляет:** `ConfigProvider` принимает `csp?: { nonce?: string }`
> (`node_modules/antd/lib/config-provider/context.d.ts`, интерфейс `CSPConfig`), значение
> доходит до движка стилей `@ant-design/cssinjs@^2.1.2`, где его применяет `injectCSPNonce`
> (`lib/util/index.js`), а хуки регистрации стилей объявляют `nonce?: string | (() => string)`.
>
> Отступление тем не менее **остаётся принятым**, потому что не сделана НАША половина:
> per-response nonce никто не выпускает и не подставляет — ни в заголовок политики, ни в
> страницу. То есть препятствие переехало из чужой библиотеки в наш конвейер доставки, и
> условие пересмотра ниже переписано под это. Само послабление в дереве не тронуто: правка
> политики содержимого — предмет отдельного изменения со своей приёмкой, а не побочный
> эффект бампа зависимости.

The console's Content-Security-Policy (`deploy/values.yaml` → `security.contentSecurityPolicy`)
is otherwise strict — `script-src 'self'`, `object-src 'none'`, `base-uri 'self'`,
`frame-ancestors 'none'`, `form-action 'self'`, `connect-src 'self'`. Only
`style-src` is relaxed to `'self' 'unsafe-inline'`.

**Why it is required:** antd v6 styles components through a runtime CSS-in-JS
engine that injects `<style>` elements at runtime, so a hash-based `style-src` is
not available (the hashes are not build-time-stable). A *nonce*-based `style-src`
is now technically reachable — `ConfigProvider csp={{ nonce }}` forwards a nonce to
`@ant-design/cssinjs` — but nothing in this repository produces or propagates a
per-response nonce: the CSP header in `deploy/values.yaml` is static, and the host
nginx does not inject one into the served document. Until that plumbing exists,
tightening `style-src` would break antd's runtime styling.

**Why the risk is bounded:** `script-src` remains `'self'`, so no
attacker-controlled JavaScript can execute regardless of the style relaxation.
The residual is limited to CSS-only vectors (restyle/overlay of controls) and is
only reachable if a separate DOM-injection sink is introduced elsewhere — none is
known. The DPoP token flow, auth ceremony and API calls are unaffected.

**Revisit trigger:** the antd-side precondition is **already met** (see the
re-measurement note above), so what remains is ours: have the host nginx mint a
per-response nonce, emit it both in the `style-src` directive and into the served
document, feed it to `ConfigProvider csp={{ nonce }}`, and only then drop
`'unsafe-inline'`. That is a change to the delivery pipeline and to the security
posture — it carries its own acceptance, and it is deliberately not folded into a
dependency bump.

## Политика консоли применяется к проксируемой странице входа и блокирует встроенный скрипт провайдера

**Статус:** принято / ограниченный остаток; измерено 2026-08-11, держится гейтом.

**Запись гейта:** `csp:login-page-inline-script` — держит `scripts/check-csp-divergence-records.mjs`.
**Измерено против:** `oryd/kratos-selfservice-ui-node:v1.3.1`, `oryd/kratos-selfservice-ui-node:v26.2.0`.

Страницы входа и регистрации отдаёт **сторонний** self-service UI провайдера
личности, а край консоли проксирует их полосу через себя
(`deploy/templates/configmap-nginx.yaml`, полоса `^/(login|registration|…)`).
nginx наследует `add_header` с внешнего уровня, пока у полосы нет своих, — значит
политика консоли применяется и к странице, которую консоль не писала. Одну
конструкцию этой страницы политика отвергает, и браузер печатает об этом ошибку
на каждой загрузке входа.

**Что именно отвергается — измерено, а не предположено:** один встроенный скрипт
в 123 байта, вешающий пустой обработчик `load` на узлы passkey, которых на
странице нет. Отказ не меняет ничего: регистрация проходит сквозь
(форма → пароль → `/dashboard`), проверено на стенде 2026-08-11.

**Почему это не «чинится» ослаблением политики.** `script-src` не ослабляется и
хеш не добавляется: `'unsafe-inline'` ради пустого обработчика открыл бы
исполнение произвольных встроенных скриптов на страницах **ввода пароля** — там,
где цена ошибки максимальна, — а хеш протух бы при первом же обновлении
провайдера и вернул бы ту же ошибку.

**Почему исход «объявить», а не «снять» — с разбором отвергнутых способов снять:**

- вырезать чужой скрипт при проксировании — правка чужой разметки по образцу: она
  либо промахнётся мимо будущего настоящего скрипта, либо вырежет его;
- подставлять чужому скрипту `nonce` — то же ослабление, названное иначе: nonce,
  выдаваемый любому встроенному скрипту, который прислал провайдер, доверяет всей
  его разметке, и именно на странице ввода пароля;
- отдавать вход собственными страницами консоли — смена того, кто отдаёт вход;
  это отдельный предмет со своей приёмкой, а не правка политики. Заготовки под
  него в дереве больше нет: страницы, лежавшие здесь мёртвыми, сняты (#1225), и
  такой ход означал бы написать их заново — уже под собственного поставщика, а
  не под протокол внешнего.

**Цена бездействия названа честно:** постоянная ошибка в журнале приучает не
смотреть на нарушения политики, и настоящее нарушение потеряется среди этого.
Гейт существует ровно затем, чтобы запись не пережила своё основание молча.

**Условие снятия — и чем каждое держится.** Гейт роняет прогон, когда:

- пин стороннего UI в дереве отличается от названного выше — разметку страницы
  входа определяет только он, поэтому его смена означает «перемерить и либо снять
  запись, либо обновить пин»;
- полоса входа больше не проксируется стороннему UI — предмета у записи нет;
- у полосы появились собственные заголовки — политика консоли на страницу входа
  больше не наследуется, и запись описывает не то, что происходит;
- `script-src` ослаблен — исход, который запись прямо запрещает.

**Про два пина, а не один.** В дереве провайдер закреплён дважды: умолчание
чарта (`deploy/helm/umbrella/charts/kratos-selfservice-ui/values.yaml`) и
переопределение профиля разработки (`deploy/helm/umbrella/values.dev.yaml`).
Профили расходятся: боевой включает UI, тега не переопределяет и едет на
умолчании чарта, разработка — на более новом. Какой из двух был на стенде
2026-08-11, в исходной записи не зафиксировано, а восстановить это задним числом
нельзя, — поэтому запись привязана к **обоим**: любое движение любого из них
требует перемерить. Предикат: `grep -rn -A3 'repository:.*kratos-selfservice-ui'
deploy/helm/umbrella`.

Пин — **прокси**-предикат: он ломается раньше своего предмета (образ могли поднять
по причине, к встроенному скрипту не относящейся). Направление неточности выбрано
осознанно: ложное срабатывание требует перемерить, а не оставляет запись тихо жить
после того, как её основание исчезло.

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

## Server-side narrowing of the related-child tab lands in `@shared` only

**Status:** accepted / bounded residual — measured per package, one live gap
(nlb `listeners`), with a stated closure that is not "copy it four times".

A resource card's related-child tab used to read the child's list **page** for
the whole project and narrow it in the browser (`all.filter(…)` on
`spec.related[].filterField`). What the tab then showed was the intersection
«children of this parent» × «first page of the project's list», presented as the
whole list: a network whose subnets did not make the first page rendered an
incomplete tab and said nothing about it, while the child's own list page has had
a cursor continuation all along — one question with two answers.

Two things now close that in `@shared`:

- `spec.related[].serverFilterField` (declared in `shared/src/lib/resource-spec.ts`)
  names the field the child's **owner** accepts in its list `filter` expression;
  `shared/src/components/organisms/ResourceShell/ResourceShell.tsx` sends it as
  `filter=<field>="<parentId>"`, so the server narrows and the page is exact. The
  declaration is held against the owner's whitelist — read out of the service's
  production code — by `shared/src/lib/related-server-filter-parity.test.ts`; the
  request shape is pinned by
  `shared/src/components/organisms/ResourceShell/ResourceShell.related.test.tsx`.
- Where no such field exists, the cursor continuation is rendered in the same form
  the list page uses, and the "create the first one" invitation is withheld while a
  cursor remains — an empty read page is not evidence of an empty child list.

**What the other four copies do.** Five packages carry a `ResourceShell`
(`git ls-files 'ui-future/*/src/components/organisms/ResourceShell/ResourceShell.tsx'`
→ compute, nlb, registry, shared, storage); only the `@shared` one has the two
behaviours above. Measured 2026-08-13, per package `related` edges:

- **compute, storage** — zero related edges. Nothing to truncate.
- **registry** — two edges, both **path-scoped** children (`apiPath` carries
  `{registryId}` / `{repository}`), so the owner narrows by the path segment and no
  foreign parent's rows are involved. `repositories` additionally declares
  `loadAllPages` (every page is read); `tags` does not, so a repository with more
  tags than one page still shows a truncated tab there.
- **nlb** — one edge (`load-balancers` → `listeners`), project-scoped and narrowed
  in the browser. Its owner's filter whitelist accepts `name` only
  (`services/nlb/internal/apps/kacho/api/shared/namefilter.go`), so there is no
  server field to declare: the fix for that tab is the continuation, and it is not
  in this change.

**Why the change was not copied into the forks.** The console rule for this class
says the real closure is de-forking, not tiling: a fourth and fifth copy means the
next edit again reaches exactly one of them, silently. The residual is written
here with its predicate instead of being inherited unnoticed.

**Revisit trigger:** when `nlb`'s `ResourceShell` is folded into `@shared` (or its
listeners tab is touched for any other reason), the continuation arrives with it.
If `ListListeners` ever accepts `load_balancer_id` in its filter whitelist, that
edge should declare `serverFilterField` — the parity probe holds it from then on.

## Сборочная цепочка консоли закреплена на седьмом мажоре `vite`

**Status:** accepted / пин с названной причиной; предмет держат два гейта, а не текст.

**Что закреплено.** Девять корней из одиннадцати объявляют `vite ^7.3.6`,
`@vitejs/plugin-react ^4.7.0` и `@originjs/vite-plugin-federation ^1.4.1`; `shared` и
`e2e` не объявляют ни того, ни другого. Перепись (база `0a07fae2`, единица счёта —
отслеживаемый манифест):

```sh
git ls-files 'ui-future/*/package.json'   # 11
node ui-future/scripts/check-federation-pin-still-needed.mjs
```

### Почему седьмой, а не шестой (#337)

До 2026-08-14 пин стоял на **шестом** мажоре — то есть был **шире своей причины на целый
мажор**: препятствие названо ниже и живёт в **восьмом**, а седьмой от него свободен.
Разойтись эти две границы могли молча, потому что проверять их совпадение было нечем.
Теперь есть чем — проверка 5 гейта причины (см. «Чем держится»); на состоянии до этой
правки она даёт находку, на состоянии после — молчит, и оба исхода закреплены её
самопроверкой, а не этим абзацем.

Подъём выбран по четырём критериям, и у каждого — предикат, а не намерение:

| Критерий | Что установлено | Предикат |
|---|---|---|
| **безопасность** | ни одна опубликованная уязвимость не задевает ни `6.4.3`, ни `7.3.6` — на день подъёма это **не** различитель. Различитель — **окно поддержки**: у сборщика сопровождаются `latest` и `previous`, и шестой мажор не входит ни в тот, ни в другой. Следующая заплата приедет в восьмой и седьмой; попадёт ли она в шестой — усмотрение сопровождающего, а не обязательство | `npm view vite dist-tags` → `latest: 8.2.1`, `previous: 7.3.6`; запрос к сводке предупреждений реестра по трём версиям — пусто |
| **перспективность** | шестая линия выдыхается: её последний выпуск — **2026-06-01**, тогда как седьмая получила выпуск **2026-06-25** без ответной заплаты в шестую. Парные выпуски (`7.3.2`/`6.4.2`, `7.3.5`/`6.4.3` — с разницей в минуты) прекратились | `npm view vite time` |
| **производительность** | **различителя нет, и он не заявляется.** Седьмой мажор собирает тем же ядром `rollup` + `esbuild`, что и шестой; выигрыш в скорости принадлежит восьмому с его `rolldown`, а он закрыт названным препятствием. Число здесь было бы украшением | состав зависимостей: `6` и `7` → `rollup`+`esbuild`, `8` → `rolldown` |
| **конкурентоспособность** | измеримого различителя нет; довод исчерпывается двумя верхними | — |

**Цена подъёма — одна строка на корень, вторая поверхность не заводится.**
`@vitejs/plugin-react` четвёртой линии принимает сборщик по седьмой мажор включительно
(`^4.2.0 || ^5.0.0 || ^6.0.0 || ^7.0.0` начиная с `4.7.0`), поэтому переход **не**
требует шестой линии плагина и её новых пиров — а под восьмой мажор потребовал бы.
Требование к среде тоже покрыто: седьмой мажор просит `^20.19.0 || >=22.12.0`, а
конвейер и все девять образов стоят на `node:26`.

**Чем подъём доказан, а не объявлен** (девять корней, каждый — свой прогон):

| Что проверено | Как | Охват |
|---|---|---|
| федерация **исполняется** | `node scripts/check-federation-executes.mjs --self-test` | самопроверка ловит инъекцию, законный близнец молчит |
| метка стилей **подставлена в настоящем бандле** — то самое, что ломается под восьмым | `grep -c '__v__css__' <корень>/dist/assets/remoteEntry.js` | **8** remote'ов, у каждого **0** неподставленных меток |
| сборка продукта | `npm run build` | **9** из 9 корней |
| типы | `npm run typecheck` | **10** пакетов (девять корней + `shared`) |
| модульные пробы | `npm test` | **9** корней, **4 656** проб, падений 0 |

Чего локальный прогон **не** даёт и дать не может: хост в браузере, `antd`,
маршрутизация и живой обмен между хостом и remote'ами. За это отвечают сквозные пробы
консоли (`console-e2e`) на поднятом стенде — второй вердикт предиката снятия #337; у них
нет локального прогонщика, поэтому здесь они честно названы, а не зачтены.

### Почему не восьмой

Восьмой мажор `vite` заменяет сборщик: у `6` и `7` в
зависимостях `rollup` + `esbuild`, у `8` — `rolldown`, и ни `rollup`, ни `esbuild`
там больше нет. Плагин федерации этот переход не переживает, и место названо точно.

Плагин передаёт список стилей выставленного модуля из одного своего этапа в другой
**меткой внутри уже собранного кода** и находит её выражением, привязанным к виду
кавычки — одинарной или двойной. Минификатор нового сборщика перекавычивает строковые
литералы в шаблонные, поэтому метка не находится и **не подставляется**: в бандл
уезжает строка там, где рантайм точки входа ждёт список. Первый же `get()` падает,
и падает он у **каждого** выставленного имени каждого remote'а.

Со стороны это выглядит так, как и было записано в #309: точка входа федерации
загружается (запросы к `*-remote/assets/remoteEntry.js` видны), а дальше ничего —
ни одного вызова к API, пустой экран, четыре пробы из четырёх.

**Замер, из-за которого пин остаётся** (стенд не нужен; проба той же формы, что
у настоящего remote: выставленный модуль, общая зависимость, свой стиль,
`cssCodeSplit: false`):

| сборочная цепочка | точка входа | `get()` | браузером: вызов к API |
|---|---|---|---|
| `vite` 6.4.3 (rollup + esbuild) | грузится | отдаёт модуль | есть, экран непуст |
| `vite` 7.3.6 (rollup + esbuild) | грузится | отдаёт модуль | — |
| `vite` 8.2.1 (rolldown) | грузится | **бросает `TypeError`** | **нет, экран пуст** |
| `vite` 8.2.1, минификация выключена | грузится | отдаёт модуль | — |
| `vite` 8.2.1, поиск метки принимает и шаблонную кавычку | грузится | отдаёт модуль | есть, экран непуст |

Две нижние строки — контроль в обе стороны: они показывают, что ломает именно
перекавычивание, а не «восьмой мажор вообще».

**Почему тогда не почини́ть плагин у себя.** Потому что цена названа, а не
предположена, и она не в одной строке:

- у плагина **нет** объявленной совместимости со сборщиком вовсе (в его манифесте
  нет `peerDependencies`), поэтому установка под любым мажором проходит молча —
  предупреждать некому;
- у плагина одна опубликованная линия, и последний её выпуск — **2025-04-12**, тогда
  как восьмой мажор сборщика вышел **2026-03-12**: релиза под него не было и нет, а
  значит чинить пришлось бы форком, который мы носим сами по девяти корням
  (предикат: `npm view @originjs/vite-plugin-federation time`, `npm view vite time`);
- подъём до восьмого тянет за собой **вторую поверхность риска в том же изменении**:
  `@vitejs/plugin-react` четвёртой линии кончается седьмым мажором, под восьмым нужна
  шестая линия плюс два новых пира. Именно поэтому шаг до седьмого дёшев (одна строка
  на корень), а следующий — нет;
- **найденное место — первое, а не единственное.** Замер ниже сделан на пробе, которая
  не несёт ни `antd`, ни маршрутизатора и собирает один remote без хоста; утверждать по
  ней, что подъём состоится, значило бы выдать зелёное меньшей области за большее.
  Она отвечает на «что именно ломается», а не на «всё ли остальное цело».

Поэтому верхняя граница пина остаётся там же, где была, — под восьмым мажором; #337
подвинул только нижнюю. Куда двигаться дальше, сказано в «Revisit trigger».

**Чем держится** (два гейта, оба доказаны инъекцией в обе стороны, оба идут в `ui.yml`):

- `scripts/check-federation-executes.mjs` — **исполняет федерацию**: собирает
  маленький remote той же оснасткой, что и продукт (она резолвится подъёмом, второго
  объявления версий не заводится by construction), затем вызывает `get()` у каждого
  выставленного имени и требует, чтобы модуль **выполнился**, общая зависимость
  приехала из shared-области, а объявленный стиль существовал в бандле. Это та
  проверка, которой на участке не было вовсе: типы и модульные пробы федерацию не
  исполняют и остаются зелёными при мёртвом рантайме. Секунды, без стенда;
- `scripts/check-federation-pin-still-needed.mjs` — держит **причину**: цепочка одна
  на все корни, у пина нет дыр, и то место у плагина, из-за которого пин заведён, ещё
  на месте. Когда его не станет — гейт покраснеет и потребует перемерить, вместо того
  чтобы дать причине пережить свой предмет. С #337 он держит и **ширину**: пин обязан
  дотягиваться ровно до мажора, свободного от препятствия, и не дальше. Границу гейт
  **выводит** из состава зависимостей сборщика в реестре (ядро `rollup`+`esbuild`
  против `rolldown`), а не хранит числом — иначе число пережило бы свой предмет ровно
  так же, как пережила его причина в тексте этой записи. Реестр недоступен — проверка
  пропускается **вслух**, а не зачитывается в «ноль находок».

**Revisit trigger:** гейт причины краснеет сам — при появлении у плагина совместимого
поведения, при его замене, **и при выходе мажора, свободного от препятствия** (пин
станет уже причины и это будет названо в тексте отказа). Движение к восьмому мажору —
не форк плагина, а его замена: у `@module-federation/vite` объявлена совместимость со
сборщиком с пятого по восьмой мажор. Это отдельная работа со своим предметом. Вердикт по
девяти корням в любом случае дают `check-federation-executes.mjs` и сквозные пробы
консоли, а не эта таблица.

## Один судья стилей на девять модулей

**Status:** решено (#1801); сведено к одной конфигурации, отступление от стандарта осталось одно.

**Что было измерено.** Конфигураций `.stylelintrc.json` было **две** на девять
модулей: строгую несли `host` и `dashboard`, послабленную — остальные семь.
Решения об этом не принимал никто — обе приехали **одним коммитом** переезда в
монорепо (`78b0c50f0`), то есть расхождение унаследовано, а не выбрано. Форма
различия при этом была не случайной: послабленную несли ровно те семь, что
настраивают Tailwind. Случайными были **последствия** — следующий, кто заводит
модуль, копировал бы один из двух вариантов наугад, и какой именно, не решало
ничто.

**Решение: судья один, и это строгий.** Свести к послабленному значило бы снять
у двух модулей три действующих правила (`no-descending-specificity`,
`rule-empty-line-before`, `declaration-block-single-line-max-declarations`) — то
есть заплатить за единообразие проверкой. Сведено к строгому: он никому ничего
не снимает, а семи модулям правила **добавляет**.

**Цена измерена, а не предположена.** Строгий вариант прогнан живым stylelint по
CSS всех девяти модулей: красным стал **один** файл на **одном** правиле
(`registry/src/index.css:37`, `rule-empty-line-before`, автопочинимо) — он
починен тем же изменением. Остальные восемь зелены.

**Что осталось отступлением от стандарта.** Ровно одно: `at-rule-no-unknown` с
перечнем at-правил Tailwind (`apply`, `config`, `layer`, `screen`, `tailwind`).
Оно оставлено **платформенно**, включая `host` и `dashboard`, которые Tailwind не
настраивают: общий лист стилей (`shared/src/index.css`) написан на его
at-правилах, модули импортируют его, и правило, запрещающее `@apply` в одном
модуле и разрешающее в соседнем, — ровно та развилка, которую эта запись и
снимает.

**Чем держится** — гейтом, а не памятью автора:
`internal/repohygiene/consolestylelintconfig_test.go`. Он краснеет на **втором**
варианте судьи, называя отклонившиеся модули и большинство; сверяет шов в обе
стороны (объявленная команда без конфигурации и конфигурация без команды);
печатает объём осмотренного и падает на пустом обходе. Способность падать и
молчать доказана инъекцией **настоящей** прежней конфигурации
(`consolestylelintconfig_injection_test.go`).

**Послабление истекает само.** Второй судья того же гейта краснеет, когда
конфигурация прощает at-правила Tailwind, а Tailwind не настраивает **ни один**
модуль консоли: тогда перечень прощает то, чего больше нет, и его снимают, а не
носят вечно.

**Revisit trigger:** гейт истечения краснеет сам при уходе Tailwind из консоли.
Отдельный остаток, гейтом **не** покрытый и потому названный здесь: `shared`
несёт единственный в дереве CSS с at-правилами Tailwind
(`shared/src/index.css`), но не объявляет `lint:css` и не несёт
`.stylelintrc.json` — его стили не читает **ни один** судья. Это не следствие
этого решения, а предмет, существовавший до него; заведён отдельно.
