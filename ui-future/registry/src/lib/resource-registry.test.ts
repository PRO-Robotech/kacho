import { REGISTRY, getResource, resourceServicePrefix, resourceProjectPath } from "./resource-registry";

describe("registry resource-registry", () => {
  it("registers the three registry resources", () => {
    expect(Object.keys(REGISTRY).sort()).toEqual(["registries", "repositories", "tags"].sort());
  });

  it("registries spec — apiPath / payloadKey / full CRUD ops + репозитории child", () => {
    const reg = getResource("registries")!;
    expect(reg.apiPath).toBe("/registry/v1/registries");
    expect(reg.payloadKey).toBe("registries");
    expect(reg.scope).toBe("project");
    expect(reg.ops).toEqual({ create: true, update: true, delete: true });
    // Wire-id ребёнка = repositories (OCI/REST-контракт), tenant-facing label — «Репозитории».
    expect(reg.related).toEqual([{ childId: "repositories", filterField: "registry_id", label: "Репозитории" }]);
  });

  it("repositories (репозитории) — read-only (нет create/update/delete), nested apiPath, без fields", () => {
    const repo = getResource("repositories")!;
    expect(repo.apiPath).toBe("/registry/v1/registries/{registryId}/repositories");
    expect(repo.payloadKey).toBe("repositories");
    expect(repo.singular).toBe("Репозиторий");
    expect(repo.plural).toBe("Репозитории");
    expect(repo.ops).toEqual({ create: false, update: false, delete: false });
    expect(repo.fields).toBeUndefined();
  });

  it("repositories — facet artifact_types (docker/helm/иные, include-match) + load-all + колонка «Тип»", () => {
    const repo = getResource("repositories")!;
    // Facet-фильтр по массиву типов артефакта (смешанный репозиторий → include).
    expect(repo.facet?.path).toBe("artifact_types");
    expect(repo.facet?.options.map((o) => o.value)).toEqual([
      "ARTIFACT_TYPE_CONTAINER_IMAGE",
      "ARTIFACT_TYPE_HELM_CHART",
      "ARTIFACT_TYPE_OTHER",
    ]);
    // load-all: facet должен видеть полный набор (handler пагинирует).
    expect(repo.loadAllPages).toBe(true);
    // Колонка «Тип» присутствует (artifact_types, multi-icon).
    expect(repo.columns.some((c) => c.header === "Тип" && c.path === "artifact_types")).toBe(true);
  });

  it("tags — единственная мутация delete, nested apiPath, без create/update-полей", () => {
    const tag = getResource("tags")!;
    expect(tag.apiPath).toBe("/registry/v1/registries/{registryId}/repositories/{repository}/tags");
    expect(tag.payloadKey).toBe("tags");
    expect(tag.ops).toEqual({ create: false, update: false, delete: true });
    expect(tag.fields).toBeUndefined();
  });

  it("registries name-поле — required + mutable (переименование; OCI-путь по id)", () => {
    const reg = getResource("registries")!;
    const name = reg.fields!.find((f) => f.name === "name")!;
    expect(name.type).toBe("string");
    expect(name.required).toBe(true);
    // Имя реестра mutable — редактируется и после создания (OCI-путь по id, не по имени).
    expect(name.immutable).toBeFalsy();
    expect(name.createOnly).toBeFalsy();
  });

  it("service prefix + project path → сегмент /registry/", () => {
    expect(resourceServicePrefix("registries")).toBe("registry");
    expect(resourceServicePrefix("repositories")).toBe("registry");
    expect(resourceServicePrefix("tags")).toBe("registry");
    expect(resourceProjectPath("registries", "prj-1")).toBe("/projects/prj-1/registry/registries");
    expect(resourceProjectPath("registries", null)).toBeNull();
  });
});
