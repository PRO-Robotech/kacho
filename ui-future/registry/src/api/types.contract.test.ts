// TS-типы реестра образов против контракта ствола.
//
// Ground truth: proto/kacho/cloud/registry/v1/registry.proto и
// registry_service.proto.
//
// Два слоя, как и в nlb, и оба — КОМПИЛЯТОРНЫЕ:
//
//  1) положительный — присваивание с полным набором полей роняет суиту, если
//     интерфейс отстал от контракта (отсутствует обязательное) или разошёлся с
//     ним (лишнее поле в литерале);
//  2) отрицательный — снятое в стволе имя интерфейс обязан ОТВЕРГАТЬ,
//     утверждается через `@ts-expect-error`: вернут поле — подавление станет
//     лишним, и ts-jest уронит суиту.
//
// Прежняя редакция вместо второго слоя читала СВОЙ ЖЕ `types.ts` с диска и
// искала в нём подстроки. Это утверждение о символах файла, а не о том, что
// тип принимает: оно переживало любую смену формы записи и не могло покраснеть
// на изменении поведения.

import type { Registry, Repository, Tag } from "./types";

describe("типы реестра образов против контракта ствола", () => {
  it("Registry несёт регион, тип размещения и видимость по умолчанию", () => {
    const r: Registry = {
      id: "reg-000000000000000",
      project_id: "prj-1",
      // region_id обязателен на Create (registry_service.proto), immutable,
      // peer-validate у geo; placement_type всегда REGIONAL и на вход не подаётся.
      region_id: "ru-central1",
      placement_type: "REGIONAL",
      default_repository_visibility: "PRIVATE",
      endpoint: "registry.kacho.local/reg-000000000000000",
      repository_count: 0,
      status: "REGISTRY_STATUS_ACTIVE",
    };
    expect(r.region_id).toBe("ru-central1");
    expect(r.default_repository_visibility).toBe("PRIVATE");
  });

  it("Repository несёт класс исчезаемости и собственную видимость", () => {
    const repo: Repository = {
      name: "team/app",
      registry_id: "reg-000000000000000",
      lifecycle: "EPHEMERAL",
      visibility: "PRIVATE",
    };
    expect(repo.lifecycle).toBe("EPHEMERAL");
  });

  it("Tag адресуется реестром по id, а не по имени реестра", () => {
    const t: Tag = { tag: "v1", registry_id: "reg-000000000000000", repository: "team/app", digest: "sha256:00" };
    expect(t.registry_id).toMatch(/^reg-/);
  });

  it("снятое в стволе имя интерфейс отвергает", () => {
    // Отрицание проверяет КОМПИЛЯТОР и оно парное к присваиваниям выше: без
    // них оно зеленело бы и на пустом типе.
    const r: Registry = {
      id: "reg-000000000000000",
      project_id: "prj-1",
      region_id: "ru-central1",
      status: "REGISTRY_STATUS_ACTIVE",
      // @ts-expect-error видимость репозитория по умолчанию названа полностью
      default_visibility: "PRIVATE",
    };
    expect(r.region_id).toBe("ru-central1");

    const repo: Repository = {
      name: "team/app",
      registry_id: "reg-000000000000000",
      // @ts-expect-error размер хранения — поле снятого дубля блочного хранения
      storage_size: 1,
    };
    expect(repo.name).toBe("team/app");
  });
});
