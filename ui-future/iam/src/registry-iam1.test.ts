// IAM-1 UI-support regression — spec-driven registry (shared) + api/iam helpers.
// Импортирует REGISTRY/helpers и утверждает форму IAM-1 ресурсов
// (Account/Project/Role/AccessBinding) по acceptance
// docs/specs/sub-phase-IAM-1-tenancy-authz-core-acceptance.md (F1..F10).
//
// Здесь был третий блок — «source-conformance»: он читал с диска три `.tsx`
// формы и расширения и искал в их тексте подстроки. Такое утверждение говорит о
// СИМВОЛАХ файла: перепиши ту же отправку другой формой записи — и оно осталось
// бы зелёным, ничего не проверив. Предмет каждого его пункта закрыт по
// поведению и без него: форму тела гранта держат утверждения о значении,
// которое возвращает `buildCreateAccessBindingBody` (ниже) и провод
// `shared/src/api/iam.wire.test.ts`; что показывает карточка привязки —
// `access-binding-field-names.test.tsx`, где расширение ИСПОЛНЯЕТСЯ.

import { REGISTRY } from "@shared/lib/resource-registry";
import type { FormField } from "@shared/lib/form-schema";
import {
  buildCreateAccessBindingBody,
  roleIsSystem,
  roleDefinitionTier,
  targetKind,
  targetResources,
  SYSTEM_ROLE_CANON_ORDER,
  type CreateAccessBindingInput,
  type Role,
} from "@shared/api/iam";

/** Минимальный валидный грант — база для проверки арма target'а. */
const grantInput: CreateAccessBindingInput = {
  subjects: [{ type: "SUBJECT_TYPE_USER", id: "usr-1" }],
  roleId: "rol-1",
  scopeTier: "ACCOUNT",
  scopeId: "acc-1",
};

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

  it("остаётся output-only колонка «Владелец»; deletionProtection у Account нет вовсе", () => {
    expect(colByHeader("accounts", "Владелец")?.path).toBe("owner_user_id");
    // deletion_protection есть у AccessBinding, но НЕ у Account: ни в
    // CreateAccountRequest {name, description, labels, owner_user_id}, ни в
    // UpdateAccountRequest, ни в самом Account. Форма его предлагала, шаблон сеял,
    // а край выбрасывал ключ молча — галочка «защитить от удаления» не делала
    // ничего и отвечала успехом.
    expect(fieldByName(REGISTRY.accounts.fields, "deletion_protection")).toBeUndefined();
    expect(REGISTRY.accounts.template({})).not.toHaveProperty("deletion_protection");
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
    expect(colByHeader("access-bindings", "Якорь")?.path).toBe("scope_id");
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
    // AccessTarget.resources is itself AccessTargetResources{repeated ResourceRef
    // resources}, so a read carries target.resources.resources[] — the same
    // nesting buildCreateAccessBindingBody writes. Reading it flat made every
    // per-object binding render as «no target».
    expect(targetKind({ resources: { resources: [{ type: "compute.instance", id: "ins-1" }] } })).toBe("resources");
    expect(targetResources({ resources: { resources: [{ type: "compute.instance", id: "ins-1" }] } })).toEqual([
      { type: "compute.instance", id: "ins-1" },
    ]);
    expect(targetKind({ resources: { resources: [] } })).toBeUndefined();
    expect(targetKind(undefined)).toBeUndefined();
    expect(targetKind({})).toBeUndefined();
  });
});

// ─────────────────────── тело гранта: значение, а не текст формы ───────────────────────
describe("IAM-1 F8 — тело создания привязки", () => {
  it("без перечня объектов target — «вся область»", () => {
    expect(buildCreateAccessBindingBody({ ...grantInput, targetResources: undefined }).target).toEqual({
      all_in_scope: {},
    });
  });

  it("с перечнем объектов target — именно они", () => {
    expect(
      buildCreateAccessBindingBody({ ...grantInput, targetResources: [{ type: "compute.instance", id: "ins-1" }] })
        .target,
    ).toEqual({ resources: { resources: [{ type: "compute.instance", id: "ins-1" }] } });
  });
});
