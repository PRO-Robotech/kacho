import { visibilityLabel } from "./VisibilityTag";

// Видимость репозитория / реестра-дефолта (REG-1 F5): PRIVATE (доступ по правам)
// vs PUBLIC (anonymous pull, admin-gated). Пусто / мусор → «—».
describe("visibilityLabel", () => {
  it("PRIVATE → «Приватный»", () => {
    expect(visibilityLabel("PRIVATE")).toBe("Приватный");
  });
  it("PUBLIC → «Публичный»", () => {
    expect(visibilityLabel("PUBLIC")).toBe("Публичный");
  });
  it("пусто / undefined / мусор → «—»", () => {
    expect(visibilityLabel(undefined)).toBe("—");
    expect(visibilityLabel("")).toBe("—");
    expect(visibilityLabel("VISIBILITY_UNSPECIFIED")).toBe("—");
    expect(visibilityLabel(3)).toBe("—");
  });
});
