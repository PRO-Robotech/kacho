// IAM-1 UI-support regression — spec-driven registry (shared) + api/iam helpers +
// bespoke-form/detail-extension source-conformance. Мирроит паттерн compute/storage
// resource-registry.test.ts: импортирует REGISTRY/helpers и ассертит форму IAM-1
// ресурсов (Account/Project/Role/AccessBinding) по acceptance
// docs/specs/sub-phase-IAM-1-tenancy-authz-core-acceptance.md (F1..F10).

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { REGISTRY } from "@shared/lib/resource-registry";
import type { FormField } from "@shared/lib/form-schema";
import { roleIsSystem, roleDefinitionTier, targetKind, SYSTEM_ROLE_CANON_ORDER, type Role } from "@shared/api/iam";

const here = path.dirname(fileURLToPath(import.meta.url));
const fieldByName = (fields: FormField[] | undefined, name: string) => (fields ?? []).find((f) => f.name === name);
const colByHeader = (id: string, header: string) => REGISTRY[id].columns.find((c) => c.header === header);

// ─────────────────────────── F1/F2: Account ───────────────────────────
describe("IAM-1 F1/F2 — accounts spec", () => {
  it("ownerUserId° убран из Create-формы (derived-from-caller, output-only)", () => {
    // F1: ownerUserId НЕ принимается в body → не должно быть поля формы.
    expect(fieldByName(REGISTRY.accounts.fields, "owner_user_id")).toBeUndefined();
    const tpl = REGISTRY.accounts.template({}) as Record<string, unknown>;
    expect(tpl).not.toHaveProperty("owner_user_id");
  });

  it("остаётся output-only колонка «Владелец» + deletionProtection field", () => {
    expect(colByHeader("accounts", "Владелец")?.path).toBe("owner_user_id");
    const dp = fieldByName(REGISTRY.accounts.fields, "deletion_protection");
    expect(dp?.type).toBe("bool");
  });
});

// ─────────────────────────── F3: Project ───────────────────────────
describe("IAM-1 F3 — projects spec", () => {
  it("accountId immutable (Move удалён; исключён из update_mask)", () => {
    const acc = fieldByName(REGISTRY.projects.fields, "account_id");
    expect(acc?.immutable).toBe(true);
    expect(acc?.hidden).toBe(true);
  });
  it("name — mutable (только accountId immutable per acceptance IAM-1-08)", () => {
    const name = fieldByName(REGISTRY.projects.fields, "name");
    expect(name?.immutable).toBeFalsy();
  });
});

// ─────────────────────────── F4/F5: Role ───────────────────────────
describe("IAM-1 F4/F5 — roles spec + isSystem° derived", () => {
  it("колонка «Уровень» ключена на definition_tier (не плоский account_id)", () => {
    expect(colByHeader("roles", "Уровень")?.path).toBe("definition_tier");
    expect(colByHeader("roles", "Аккаунт")).toBeUndefined();
  });
  it("template НЕ несёт permissions[] (F5 — compiled output-only, не input)", () => {
    const tpl = REGISTRY.roles.template({}) as Record<string, unknown>;
    expect(tpl).not.toHaveProperty("permissions");
  });
  it("roleIsSystem° derived из definitionTier.tierType==iam.cluster", () => {
    expect(roleIsSystem({ definition_tier: { tier_type: "iam.cluster" } } as Role)).toBe(true);
    expect(roleIsSystem({ definitionTier: { tierType: "iam.account" } } as Role)).toBe(false);
    // legacy fallback (нет definitionTier) — хранимый bool.
    expect(roleIsSystem({ is_system: true } as Role)).toBe(true);
    expect(roleIsSystem({ isSystem: false } as Role)).toBe(false);
  });
  it("roleDefinitionTier читает snake И camel", () => {
    expect(
      roleDefinitionTier({ definition_tier: { tier_type: "iam.account", tier_id: "acc-1" } } as Role)?.tier_id,
    ).toBe("acc-1");
    expect(roleDefinitionTier({ definitionTier: { tierType: "iam.project", tierId: "prj-1" } } as Role)?.tierId).toBe(
      "prj-1",
    );
  });
});

// ─────────────────────────── F6: canonical catalog ───────────────────────────
describe("IAM-1 F6 — canonical system-role order", () => {
  it("порядок viewer → editor → admin → owner", () => {
    expect([...SYSTEM_ROLE_CANON_ORDER]).toEqual(["viewer", "editor", "admin", "owner"]);
  });
});

// ─────────────────────────── F7/F8/F10: AccessBinding ───────────────────────────
describe("IAM-1 F7/F8/F10 — access-bindings spec", () => {
  it("scopeType/scopeId/target колонки заменяют resource_type/resource_id/scope", () => {
    expect(colByHeader("access-bindings", "Область")?.path).toBe("scope_type");
    expect(colByHeader("access-bindings", "Anchor")?.path).toBe("scope_id");
    expect(colByHeader("access-bindings", "Цель")?.path).toBe("target");
    // Старой колонки «Ресурс» (resource_id) больше нет.
    expect(colByHeader("access-bindings", "Ресурс")).toBeUndefined();
  });
  it("create остаётся bespoke (ops.create=false), delete включён", () => {
    expect(REGISTRY["access-bindings"].ops.create).toBe(false);
    expect(REGISTRY["access-bindings"].ops.delete).toBe(true);
  });
  it("targetKind дискриминирует allInScope (snake+camel) vs resources[] vs пусто", () => {
    expect(targetKind({ all_in_scope: {} })).toBe("allInScope");
    expect(targetKind({ allInScope: {} })).toBe("allInScope");
    expect(targetKind({ resources: [{ type: "compute.instance", id: "ins-1" }] })).toBe("resources");
    expect(targetKind(undefined)).toBeUndefined();
    expect(targetKind({})).toBeUndefined();
  });
});

// ─────────────────────── source-conformance: bespoke forms + extensions ───────────────────────
describe("IAM-1 — bespoke forms conformance", () => {
  const roleCreate = readFileSync(
    path.join(here, "components/organisms/iam/InlineRoleCreateForm/InlineRoleCreateForm.tsx"),
    "utf8",
  );
  const bindingCreate = readFileSync(
    path.join(here, "components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx"),
    "utf8",
  );
  const ext = readFileSync(path.join(here, "registerExtensions.tsx"), "utf8");

  it("Role create шлёт definition_tier{tier_type,tier_id}, НЕ плоский account_id", () => {
    expect(roleCreate).toContain("definition_tier: { tier_type:");
    // permissions[] не отправляется (F5).
    expect(roleCreate).not.toContain("permissions:");
  });

  it("AccessBinding create несёт target-дискриминатор allInScope|resources[] (F8)", () => {
    expect(bindingCreate).toContain("_target_kind");
    expect(bindingCreate).toContain("all_in_scope");
    expect(bindingCreate).toContain("resources: rows");
    // target включён в отправляемое тело.
    expect(bindingCreate).toMatch(/target,/);
  });

  it("AccessBinding create требует непустой resources при target=resources (least-priv)", () => {
    expect(bindingCreate).toContain("rows.length === 0");
  });

  it("detail-extension: Role definitionTier + честные verb-наборы (F4/F6)", () => {
    expect(ext).toContain("roleDefinitionTier");
    expect(ext).toContain("effective_verbs");
    expect(ext).toContain("authored_verbs");
  });

  it("detail-extension: AccessBinding scopeType/target + :revoke soft-action (F7/F8/F10)", () => {
    expect(ext).toContain("RevokeBindingButton");
    expect(ext).toContain(":revoke");
    expect(ext).toContain("scope_type");
    expect(ext).toContain("targetView");
  });
});
