import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Anti-drift authorization guard (sec-hardening-r3).
 *
 * The IAM management screens (Access Bindings / Access / Groups / Roles /
 * Users) are implemented independently in the `vpc` and `iam` micro-frontends
 * because their create/edit UX legitimately differs (vpc uses in-place modals;
 * iam uses dedicated `/iam/<x>/create` routes registered only in the iam app
 * router). See docs/architecture/known-divergences.md.
 *
 * That per-app presentational fork is acceptable ONLY as long as every
 * security-relevant primitive — the permission-gating hook, the IAM mutation
 * wrapper, the typed API layer and the error mapping — stays single-sourced in
 * `@shared`. If one remote were to fork the authorization logic locally, a
 * security fix to the shared path could silently miss it (the exact failure
 * scenario flagged by the 3rd audit).
 *
 * This test fails if any IAM page in either remote:
 *   - stops importing the IAM API / mutation layer from `@shared`, or
 *   - defines a local permission-gating hook or a local IAM mutation wrapper,
 *   - calls the network directly (raw fetch / apiFetch) instead of the shared
 *     typed IAM client.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

type PageSpec = {
  name: string;
  vpc: string;
  iam: string;
  requiresPermissions?: boolean;
  /**
   * Scope-first refactor (80d1fc1): the iam AccessBindingsPage moved its
   * create-mutation out of the page body into a dedicated form component and
   * delegates delete to the shared `RowActionsMenu`. The page therefore no
   * longer imports the `IamCommon` mutation wrapper directly. The anti-drift
   * guarantee is preserved by FOLLOWING the mutation into this delegate — it
   * must stay shared-sourced (typed API + error mapping from `@shared`) and
   * must not fork authz locally. Unset → classic in-page pattern is required.
   */
  iamMutationDelegate?: string;
};

const IAM_PAGES: PageSpec[] = [
  {
    name: "AccessBindingsPage",
    vpc: "vpc/src/pages/iam/AccessBindingsPage.tsx",
    iam: "iam/src/pages/iam/AccessBindingsPage/AccessBindingsPage.tsx",
    requiresPermissions: true,
    iamMutationDelegate: "iam/src/components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx",
  },
  { name: "AccessPage", vpc: "vpc/src/pages/iam/AccessPage.tsx", iam: "iam/src/pages/iam/AccessPage/AccessPage.tsx" },
  { name: "GroupsPage", vpc: "vpc/src/pages/iam/GroupsPage.tsx", iam: "iam/src/pages/iam/GroupsPage/GroupsPage.tsx" },
  { name: "RolesPage", vpc: "vpc/src/pages/iam/RolesPage.tsx", iam: "iam/src/pages/iam/RolesPage/RolesPage.tsx" },
  { name: "UsersPage", vpc: "vpc/src/pages/iam/UsersPage.tsx", iam: "iam/src/pages/iam/UsersPage/UsersPage.tsx" },
];

// The gating hook and the IAM mutation wrapper must never be re-declared
// locally — they stay defined only in @shared. Applies to any file that
// participates in an IAM page's authz/mutation path (page OR mutation delegate).
function assertNoLocalAuthzFork(src: string) {
  expect(src).not.toMatch(/\b(function|const)\s+usePermissions\b/);
  expect(src).not.toMatch(/\b(function|const)\s+useIamMutation\b/);
}

// Classic in-page pattern: the page body itself owns the mutation via IamCommon.
function assertPageSharedSourced(src: string, requiresPermissions: boolean) {
  expect(src).toContain('from "@shared/api/iam"');
  expect(src).toContain('from "@shared/components/organisms/iam/IamCommon"');
  if (requiresPermissions) {
    expect(src).toContain('from "@shared/lib/permissions"');
  }
  assertNoLocalAuthzFork(src);
}

// Delegated pattern: the page reads via the shared typed IAM client and
// delegates its write path (create → mutation delegate, delete → shared
// RowActionsMenu). The mutation delegate must itself source the API and the
// error-mapping / permission layer from @shared, so a shared-path security fix
// still reaches it. Neither page nor delegate may fork authz locally.
function assertDelegatedSharedSourced(pageSrc: string, delegateSrc: string, requiresPermissions: boolean) {
  expect(pageSrc).toContain('from "@shared/api/iam"');
  expect(pageSrc).toContain('from "@shared/components/molecules/RowActionsMenu"');
  assertNoLocalAuthzFork(pageSrc);
  expect(delegateSrc).toMatch(/from "@shared\/api\/(client|iam)"/);
  if (requiresPermissions) {
    expect(delegateSrc).toContain('from "@shared/lib/permissions"');
  }
  assertNoLocalAuthzFork(delegateSrc);
}

describe("IAM pages keep authorization logic single-sourced in @shared", () => {
  for (const page of IAM_PAGES) {
    for (const app of ["vpc", "iam"] as const) {
      const rel = page[app];
      it(`${app}/${page.name} sources authz/mutation/API from @shared only`, () => {
        const abs = path.join(repoRoot, rel);
        expect(existsSync(abs)).toBe(true);
        const src = readFileSync(abs, "utf8");
        if (app === "iam" && page.iamMutationDelegate) {
          const delAbs = path.join(repoRoot, page.iamMutationDelegate);
          expect(existsSync(delAbs)).toBe(true);
          assertDelegatedSharedSourced(src, readFileSync(delAbs, "utf8"), !!page.requiresPermissions);
        } else {
          assertPageSharedSourced(src, !!page.requiresPermissions);
        }
      });
    }
  }
});
