import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// RegistryPage импортирует полный registry-движок (ResourceShell /
// GlobalResourceFormModal / form-subsystem) с order-sensitive циклическими
// import'ами структуры движка — его wiring проверяем по исходнику (тот же приём,
// что для engine-организмов в этом репозитории), а исполнение generic-конвейера
// покрыто тестами resource-registry и list-render'ом через ResourceListPage.
const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "RegistryPage.tsx"), "utf8");

describe("RegistryPage wiring", () => {
  it("routes the registry resources through the generic registry-конвейер", () => {
    for (const token of [
      "ResourceListPage",
      "ResourceShell",
      "ResourceCreatePage",
      "GlobalResourceFormModal",
      "OperationBanner",
      "PageHeaderSlotProvider",
      "registries",
      "repositories",
      "tags",
    ]) {
      expect(source).toContain(token);
    }
  });

  it("mounts the global form modal + operation banner exactly once", () => {
    expect(source.match(/<GlobalResourceFormModal\s*\/>/g)?.length).toBe(1);
    expect(source.match(/<OperationBanner\s*\/>/g)?.length).toBe(1);
  });

  it("default redirect targets the registries list", () => {
    expect(source).toContain("/registry/registries");
  });

  it("wires list / create / detail / edit routes per resource", () => {
    expect(source).toContain("parentField=\"project_id\"");
    expect(source).toContain('mode="edit"');
    expect(source).toContain(":uid");
  });
});
