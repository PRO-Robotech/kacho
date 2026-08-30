import { repositoryLifecycleLabel } from "./RepositoryLifecycleTag";

// Класс исчезаемости репозитория (REG-1 F7): DURABLE (survives-empty) vs
// EPHEMERAL (register-on-first-push). UNSPECIFIED / пусто → «—».
describe("repositoryLifecycleLabel", () => {
  it("DURABLE → «Постоянный»", () => {
    expect(repositoryLifecycleLabel("DURABLE")).toBe("Постоянный");
  });
  it("EPHEMERAL → «Эфемерный»", () => {
    expect(repositoryLifecycleLabel("EPHEMERAL")).toBe("Эфемерный");
  });
  it("нормализует префикс REPOSITORY_LIFECYCLE_*", () => {
    expect(repositoryLifecycleLabel("REPOSITORY_LIFECYCLE_DURABLE")).toBe("Постоянный");
  });
  it("UNSPECIFIED / пусто / мусор → «—»", () => {
    expect(repositoryLifecycleLabel("REPOSITORY_LIFECYCLE_UNSPECIFIED")).toBe("—");
    expect(repositoryLifecycleLabel(undefined)).toBe("—");
    expect(repositoryLifecycleLabel("")).toBe("—");
    expect(repositoryLifecycleLabel(7)).toBe("—");
  });
});
