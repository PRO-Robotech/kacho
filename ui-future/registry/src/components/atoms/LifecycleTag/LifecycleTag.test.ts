import { lifecycleLabel } from "./LifecycleTag";

// Класс исчезаемости репозитория (REG-1 F7): DURABLE (survives-empty) vs
// EPHEMERAL (register-on-first-push). UNSPECIFIED / пусто → «—».
describe("lifecycleLabel", () => {
  it("DURABLE → «Постоянный»", () => {
    expect(lifecycleLabel("DURABLE")).toBe("Постоянный");
  });
  it("EPHEMERAL → «Эфемерный»", () => {
    expect(lifecycleLabel("EPHEMERAL")).toBe("Эфемерный");
  });
  it("нормализует префикс REPOSITORY_LIFECYCLE_*", () => {
    expect(lifecycleLabel("REPOSITORY_LIFECYCLE_DURABLE")).toBe("Постоянный");
  });
  it("UNSPECIFIED / пусто / мусор → «—»", () => {
    expect(lifecycleLabel("REPOSITORY_LIFECYCLE_UNSPECIFIED")).toBe("—");
    expect(lifecycleLabel(undefined)).toBe("—");
    expect(lifecycleLabel("")).toBe("—");
    expect(lifecycleLabel(7)).toBe("—");
  });
});
