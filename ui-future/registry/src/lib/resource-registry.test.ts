import { REGISTRY, getResource, resourceServicePrefix, resourceProjectPath } from "./resource-registry";

describe("registry resource-registry", () => {
  it("registers the registry resources + geo regions ref-target (REG-1)", () => {
    // REG-1: registry становится REGIONAL — regions добавлен read-only ref-целью
    // (owner geo) для Registry.region_id.
    expect(Object.keys(REGISTRY).sort()).toEqual(["regions", "registries", "repositories", "tags"].sort());
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

  it("registries region_id — ref→regions, required + immutable (REG-1 F4 REGIONAL)", () => {
    const reg = getResource("registries")!;
    const region = reg.fields!.find((f) => f.name === "region_id")!;
    expect(region.type).toBe("ref");
    expect((region as { refResource: string }).refResource).toBe("regions");
    expect(region.required).toBe(true);
    // regionId immutable после Create (перенос региона сломал бы storage-locality блобов).
    expect(region.immutable).toBe(true);
    // Колонка «Регион» присутствует в списке реестров.
    expect(reg.columns.some((c) => c.header === "Регион" && c.path === "region_id")).toBe(true);
    // template несёт region_id (skeleton Create-формы).
    expect(reg.template({ projectId: "prj-1" })).toMatchObject({ region_id: "" });
  });

  it("registries default_repository_visibility — enum PRIVATE/PUBLIC, дефолт PRIVATE (REG-1 F5)", () => {
    const reg = getResource("registries")!;
    const vis = reg.fields!.find((f) => f.name === "default_repository_visibility")!;
    expect(vis.type).toBe("enum");
    expect((vis as { options: { value: string }[] }).options.map((o) => o.value)).toEqual(["PRIVATE", "PUBLIC"]);
    expect((vis as { default?: string }).default).toBe("PRIVATE");
    // fail-safe дефолт — приватная видимость в skeleton Create-формы.
    expect(reg.template({ projectId: "prj-1" })).toMatchObject({ default_repository_visibility: "PRIVATE" });
  });

  it("regions — read-only geo ref-цель (apiPath /geo/v1/regions), не навигируется как реестр", () => {
    const regions = getResource("regions")!;
    expect(regions.apiPath).toBe("/geo/v1/regions");
    expect(regions.payloadKey).toBe("regions");
    expect(regions.scope).toBe("global");
    expect(regions.ops).toEqual({ create: false, update: false, delete: false });
  });

  it("service prefix + project path → сегмент /registry/", () => {
    expect(resourceServicePrefix("registries")).toBe("registry");
    expect(resourceServicePrefix("repositories")).toBe("registry");
    expect(resourceServicePrefix("tags")).toBe("registry");
    expect(resourceProjectPath("registries", "prj-1")).toBe("/projects/prj-1/registry/registries");
    expect(resourceProjectPath("registries", null)).toBeNull();
  });
});
