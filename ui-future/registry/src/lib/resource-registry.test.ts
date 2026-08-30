import { REGISTRY, getResource, resourceServicePrefix, resourceProjectPath } from "./resource-registry";
import { MODULE_SPEC_IDS } from "./module-specs";

describe("registry resource-registry", () => {
  it("раздел монтирует свои три записи, и все три общий реестр несёт (REG-1, #409)", () => {
    // Прежде здесь стояло равенство «ключи реестра = четыре записи домена»: реестр
    // был модульным, и «весь реестр» совпадало со «спеками этого раздела» by
    // construction. После переезда в общий реестр (#409) равенство стало ложным —
    // в реестре лежит вся платформа, — а утверждать надо не его, а ДВЕ вещи:
    // что раздел монтирует именно свои три ресурса и что каждый из них в общем
    // реестре есть. Ссылочная цель `regions` (владелец geo) в перечень монтируемых
    // не входит: её резолвит `RefSelect` по идентификатору, маршрута у неё здесь нет.
    expect([...MODULE_SPEC_IDS].sort()).toEqual(["registries", "repositories", "tags"]);
    expect(MODULE_SPEC_IDS.filter((id) => !REGISTRY[id])).toEqual([]);
    expect(REGISTRY.regions).toBeDefined();
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

  it("repositories — колонки «Класс» (lifecycle) + «Видимость» (visibility) (REG-1 F5/F7)", () => {
    const repo = getResource("repositories")!;
    expect(repo.columns.some((c) => c.header === "Класс" && c.path === "lifecycle")).toBe(true);
    expect(repo.columns.some((c) => c.header === "Видимость" && c.path === "visibility")).toBe(true);
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
    // Колонка размещения присутствует в списке реестров и читает `region_id`.
    // Заголовок — «Размещение», а не «Регион»: ветку ZONAL/REGIONAL рисует общий
    // `PlacementAnchor`, и вид размещения он отдельным словом не называет — вид
    // и есть тип ресурса, на который ведёт ссылка. Что там именно ССЫЛКА, а не
    // плоский текст, утверждается деревом элементов в
    // `@shared/lib/resource-registry.registry-domain.test.tsx`: проба по
    // заголовку осталась бы зелёной на идентификаторе, из которого некуда пойти.
    expect(reg.columns.some((c) => c.header === "Размещение" && c.path === "region_id")).toBe(true);
    // template несёт region_id (skeleton Create-формы).
    expect(reg.template({ projectId: "prj-1" })).toMatchObject({ region_id: "" });
  });

  it("registries default_repository_visibility — enum PRIVATE/PUBLIC, update-only (REG-1 F5)", () => {
    const reg = getResource("registries")!;
    const vis = reg.fields!.find((f) => f.name === "default_repository_visibility")!;
    expect(vis.type).toBe("enum");
    expect((vis as { options: { value: string }[] }).options.map((o) => o.value)).toEqual(["PRIVATE", "PUBLIC"]);
    expect((vis as { default?: string }).default).toBe("PRIVATE");
    // Поле есть ТОЛЬКО в UpdateRegistryRequest (тег 6). CreateRegistryRequest —
    // {project_id, name, description, labels, region_id}. Прежде форма создания
    // его предлагала и сеяла в тело: край выбрасывал ключ молча, реестр выходил
    // PRIVATE, а пользователю показывался успех. Fail-safe приватность —
    // серверный дефолт, а не то, что клиент шлёт при создании.
    expect(vis.updateOnly).toBe(true);
    expect(reg.template({ projectId: "prj-1" })).not.toHaveProperty("default_repository_visibility");
  });

  it("regions — ссылочная цель домена geo: путь и область те, по которым её резолвит RefSelect", () => {
    const regions = getResource("regions")!;
    expect(regions.apiPath).toBe("/geo/v1/regions");
    expect(regions.payloadKey).toBe("regions");
    // Область — глобальная: каталог размещения спрашивается БЕЗ project_id.
    expect(regions.scope).toBe("global");
    // Глаголы здесь НЕ утверждаются, и это решение, а не пропуск. Прежде проба
    // требовала `{create:false, update:false, delete:false}` — верно для копии
    // раздела, где запись заводилась read-only ссылочной целью. Общая запись
    // богаче: у неё есть админская плоскость каталога geo, и глаголы там живут
    // по праву. Утверждать их отсюда значило бы держать раздел registry
    // ответчиком за чужой домен; раздел не монтирует `regions` ни одним
    // маршрутом (см. `MODULE_SPEC_IDS`) и читает её только как цель ссылки.
    expect(MODULE_SPEC_IDS).not.toContain("regions");
  });

  it("service prefix + project path → сегмент /registry/", () => {
    expect(resourceServicePrefix("registries")).toBe("registry");
    expect(resourceServicePrefix("repositories")).toBe("registry");
    expect(resourceServicePrefix("tags")).toBe("registry");
    expect(resourceProjectPath("registries", "prj-1")).toBe("/projects/prj-1/registry/registries");
    expect(resourceProjectPath("registries", null)).toBeNull();
  });
});
