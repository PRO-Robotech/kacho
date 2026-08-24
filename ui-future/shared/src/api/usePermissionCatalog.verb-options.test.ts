// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// usePermissionCatalog.verb-options.test.ts — выпадающий список глаголов берётся
// У РЕСУРСА, а не из общего пересечения (#1128).
//
// # Предмет
//
// `closed_verbs` каталога — ПЕРЕСЕЧЕНИЕ наборов всех типов, и список строился из
// него. Пока набор глаголов был платформенной константой, различие не
// наблюдалось; с набором, ставшим атрибутом ТИПА, оно наблюдается в обе стороны:
//
//   - расширение у одного типа не доходило до него самого — глагол энфорсился,
//     но не предлагался (`addTargets`/`removeTargets` у групп целей, `create` у
//     реестров);
//   - сужение у одного типа вынимало глагол из списка у ВСЕХ остальных.
//
// # Что здесь утверждается
//
// Обе стороны, плюс поведение на нескольких выбранных ресурсах: правило несёт
// ОДИН модуль и НЕСКОЛЬКО типов, поэтому предлагать надо то, что даёт каждый из
// них, — иначе глагол, выбранный человеком, молча не материализуется на части
// названных типов.

import { verbOptions, WILDCARD } from "./usePermissionCatalog";
import type { PermissionCatalog } from "./iam";

// catalog — форма ответа края, урезанная до предмета пробы. Наборы взяты такими
// же, какими их отдаёт backend: у групп целей — шире общего, у записи человека —
// уже, у сети — обычный.
const catalog: PermissionCatalog = {
  closed_verbs: ["get", "list", "delete"],
  wildcard_policy: { verb_wildcard_allowed_custom: true, module_resource_wildcard_system_only: true },
  modules: [
    {
      module: "vpc",
      resources: [
        { resource: "network", has_verb_relations: true, verbs: ["get", "list", "update", "delete"] },
        { resource: "subnet", has_verb_relations: true, verbs: ["get", "list", "update", "delete"] },
      ],
    },
    {
      module: "loadbalancer",
      resources: [
        {
          resource: "targetGroups",
          has_verb_relations: true,
          verbs: ["get", "list", "update", "delete", "addtargets", "removetargets"],
        },
      ],
    },
    {
      module: "iam",
      resources: [
        { resource: "user", has_verb_relations: true, verbs: ["get", "list", "delete"] },
        { resource: "project", has_verb_relations: false, verbs: [] },
      ],
    },
  ],
};

// без `*` — сравнивать состав удобнее без хвоста политики подстановки.
const plain = (module: string, resources: string[]): string[] =>
  verbOptions(catalog, false, module, resources).filter((v) => v !== WILDCARD);

describe("verbOptions — словарь глаголов по ресурсу", () => {
  it("ресурс с ОБЫЧНЫМ набором предлагает свои четыре глагола", () => {
    expect(plain("vpc", ["network"])).toEqual(["get", "list", "update", "delete"]);
  });

  it("ресурс с РАСШИРЕННЫМ набором предлагает и свои собственные глаголы", () => {
    // Прежде их не предлагал никто: в пересечение они не входили by construction.
    expect(plain("loadbalancer", ["targetGroups"])).toEqual([
      "get",
      "list",
      "update",
      "delete",
      "addtargets",
      "removetargets",
    ]);
  });

  it("СУЖЕННЫЙ ресурс не предлагает снятого глагола", () => {
    expect(plain("iam", ["user"])).toEqual(["get", "list", "delete"]);
  });

  it("сужение у одного ресурса НЕ отнимает глагол у соседа", () => {
    // Предмет #1128 целиком: та же правка, тот же каталог, соседний тип.
    expect(plain("vpc", ["subnet"])).toContain("update");
    expect(plain("iam", ["user"])).not.toContain("update");
  });

  it("несколько выбранных типов дают ПЕРЕСЕЧЕНИЕ их наборов", () => {
    // Глагол, которого нет у одного из названных типов, на нём не материализуется
    // — предлагать его значило бы обещать право, которого правило не даст.
    expect(plain("vpc", ["network", "subnet"])).toEqual(["get", "list", "update", "delete"]);
    expect(plain("loadbalancer", ["targetGroups"])).toContain("addtargets");
  });

  it("порядок — тот, в котором глаголы пришли от края", () => {
    // Порядок показа — часть контракта поля; UI его не пересортировывает.
    expect(plain("loadbalancer", ["targetGroups"])[0]).toBe("get");
    expect(plain("loadbalancer", ["targetGroups"]).slice(-2)).toEqual(["addtargets", "removetargets"]);
  });

  it("тип без глаголов (ярусный предок) не предлагает ничего", () => {
    expect(plain("iam", ["project"])).toEqual([]);
  });

  it("ресурс не выбран — предлагается общий набор модуля", () => {
    // Пока тип не назван, обещать глагол конкретного типа не на чем; общее —
    // единственный честный ответ.
    expect(plain("vpc", [])).toEqual(["get", "list", "update", "delete"]);
  });

  it("модуль не выбран — общий словарь каталога", () => {
    expect(plain("", [])).toEqual(["get", "list", "delete"]);
  });

  it("verb-`*` добавляется по политике каталога и в system-роли", () => {
    expect(verbOptions(catalog, false, "vpc", ["network"])).toContain(WILDCARD);
    expect(verbOptions(catalog, true, "vpc", ["network"])).toContain(WILDCARD);
    const noWildcard: PermissionCatalog = {
      ...catalog,
      wildcard_policy: { verb_wildcard_allowed_custom: false, module_resource_wildcard_system_only: true },
    };
    expect(verbOptions(noWildcard, false, "vpc", ["network"])).not.toContain(WILDCARD);
    // В system-роли `*` доступен всегда (seed-path).
    expect(verbOptions(noWildcard, true, "vpc", ["network"])).toContain(WILDCARD);
  });

  it("старый край без поля `verbs` не оставляет редактор пустым", () => {
    // Совместимость названа, а не подразумевается: глагольный ресурс, чей набор
    // край не прислал, откатывается на общий словарь. Пустой список здесь означал
    // бы «правило нельзя дописать», то есть поломку редактора на старом крае.
    const legacy: PermissionCatalog = {
      closed_verbs: ["get", "list", "update", "delete"],
      wildcard_policy: { verb_wildcard_allowed_custom: true, module_resource_wildcard_system_only: true },
      modules: [{ module: "vpc", resources: [{ resource: "network", has_verb_relations: true }] }],
    };
    expect(verbOptions(legacy, false, "vpc", ["network"]).filter((v) => v !== WILDCARD)).toEqual([
      "get",
      "list",
      "update",
      "delete",
    ]);
  });

  it("каталога нет вовсе — список пуст, а не выдуман", () => {
    expect(verbOptions(undefined, false, "vpc", ["network"]).filter((v) => v !== WILDCARD)).toEqual([]);
  });
});
