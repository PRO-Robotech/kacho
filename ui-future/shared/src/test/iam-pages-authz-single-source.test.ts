import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Управление доступом живёт РОВНО В ОДНОЙ поверхности консоли — в ремоуте iam.
 *
 * Так было не всегда. Экраны выдач/доступа/групп/ролей/пользователей были
 * реализованы ДВАЖДЫ — в `iam` и в `vpc`, — и раздвоение объявлялось допустимым,
 * пока «security-значимые примитивы» (гейт разрешений, обёртка мутаций, типизованный
 * клиент, разбор ошибок) остаются общими. Проверялось при этом ПРОИСХОЖДЕНИЕ
 * импортов, а не поведение, и разъехалось именно поведение:
 *
 *   форма создания роли в копии vpc слала `permissions` — поле выводимое,
 *   только на выход, которое сервис отвергает ПЕРВЫМ ЖЕ стейтментом создания, —
 *   и не слала `rules[]`, без которых роль не создаётся вовсе. Две независимых
 *   причины отказа; создать роль оттуда было нельзя.
 *
 * Копия при этом ещё и недостижима из продукта: хост федерирует `/iam/*` в
 * ремоут iam, а ремоут vpc наружу отдаёт только `./VpcPage`. То есть вторая
 * поверхность существовала, расходилась и ломалась, никого не обслуживая.
 *
 * Поэтому инвариант теперь не «форк допустим, пока импорты общие», а «форка
 * нет»: реализует IAM только `iam`, маршрутизирует `/iam/*` только `host` (и
 * только в ремоут). Прежняя гарантия про общие примитивы сохранена — она
 * по-прежнему нужна оставшейся поверхности.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

// Приложения консоли — поимённо: новое приложение обязано попасть под сверку
// осознанно, а не выпасть из неё молча.
const CONSOLE_APPS = ["host", "dashboard", "shared", "vpc", "compute", "storage", "nlb", "registry", "iam", "system"];

type PageSpec = {
  name: string;
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
    iam: "iam/src/pages/iam/AccessBindingsPage/AccessBindingsPage.tsx",
    requiresPermissions: true,
    iamMutationDelegate: "iam/src/components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx",
  },
  { name: "AccessPage", iam: "iam/src/pages/iam/AccessPage/AccessPage.tsx" },
  { name: "GroupsPage", iam: "iam/src/pages/iam/GroupsPage/GroupsPage.tsx" },
  { name: "RolesPage", iam: "iam/src/pages/iam/RolesPage/RolesPage.tsx" },
  { name: "UsersPage", iam: "iam/src/pages/iam/UsersPage/UsersPage.tsx" },
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

// tsxFiles — исходники приложения, кроме тестов.
function tsxFiles(dir: string, out: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry === "node_modules" || entry === "dist") continue;
    const abs = path.join(dir, entry);
    if (statSync(abs).isDirectory()) tsxFiles(abs, out);
    else if (entry.endsWith(".tsx") && !entry.includes(".test.")) out.push(abs);
  }
  return out;
}

// stripComments — гейт судит по ОБЪЯВЛЕНИЯМ маршрутов, а не по прозе рядом:
// пример в шапке компонента маршрутом не является.
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

describe("IAM management lives in exactly one console surface", () => {
  it("the iam remote implements every IAM management screen", () => {
    for (const page of IAM_PAGES) {
      expect([page.name, existsSync(path.join(repoRoot, page.iam))]).toEqual([page.name, true]);
    }
  });

  it("no other app ships its own copy of the IAM screens", () => {
    const forks = CONSOLE_APPS.filter((app) => app !== "iam").filter((app) =>
      existsSync(path.join(repoRoot, app, "src/pages/iam")),
    );
    expect(forks).toEqual([]);
  });

  it("only the host routes /iam/*, and only into the remote", () => {
    const routing = new Map<string, number>();
    for (const app of CONSOLE_APPS) {
      const hits = tsxFiles(path.join(repoRoot, app, "src")).flatMap((f) =>
        [...stripComments(readFileSync(f, "utf8")).matchAll(/path="\/iam(?:\/|")/g)].map(() =>
          path.relative(repoRoot, f),
        ),
      );
      if (hits.length > 0) routing.set(app, hits.length);
    }
    expect([...routing.keys()]).toEqual(["host"]);
    const hostApp = readFileSync(path.join(repoRoot, "host/src/App.tsx"), "utf8");
    expect(hostApp).toMatch(/path="\/iam\/\*"\s+element=\{<IamRemote/);
  });

  it("приложение, не обслуживающее /iam, туда и не ведёт", () => {
    // Ремоут `iam` — сама поверхность (хост монтирует её на `/iam/*`), `host`
    // до неё федерирует. Остальным приложениям открыть `/iam/...` нечем:
    // предложенный переход упрётся в catch-all и молча вернёт в корень.
    //
    // Граница проверки названа честно: смотрим СОБСТВЕННЫЕ исходники приложения.
    // `shared/` — библиотека, часть её экранов живёт в приложениях, которые
    // ремоут iam видят, поэтому её ссылки судить этим правилом нельзя.
    const offenders: string[] = [];
    for (const app of CONSOLE_APPS) {
      if (app === "iam" || app === "host" || app === "shared") continue;
      for (const file of tsxFiles(path.join(repoRoot, app, "src"))) {
        const src = stripComments(readFileSync(file, "utf8"));
        if (/(?:navigate\(|to=|href=)["'`]\/iam(?:\/|["'`])/.test(src)) {
          offenders.push(path.relative(repoRoot, file));
        }
      }
    }
    expect(offenders).toEqual([]);
  });

  for (const page of IAM_PAGES) {
    it(`iam/${page.name} sources authz/mutation/API from @shared only`, () => {
      const abs = path.join(repoRoot, page.iam);
      expect(existsSync(abs)).toBe(true);
      const src = readFileSync(abs, "utf8");
      if (page.iamMutationDelegate) {
        const delAbs = path.join(repoRoot, page.iamMutationDelegate);
        expect(existsSync(delAbs)).toBe(true);
        assertDelegatedSharedSourced(src, readFileSync(delAbs, "utf8"), !!page.requiresPermissions);
      } else {
        assertPageSharedSourced(src, !!page.requiresPermissions);
      }
    });
  }
});
