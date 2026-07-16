import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["statusOf", "statusLabel", "OperationsTable"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "OperationsTable.tsx"), "utf8");

describe("OperationsTable", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });
});

// Импортируем чистую фильтр-логику из opFilter (без antd — иначе jest-граф с
// `antd/es/table` подвешивает импорт runtime-модуля OperationsTable).
import { matchesOutcome } from "./opFilter";

describe("matchesOutcome", () => {
  it("keeps only failed operations for the error outcome", () => {
    const errored = { done: true, error: { code: 13, message: "boom" } };
    const succeeded = { done: true };
    const running = { done: false };

    expect(matchesOutcome(errored, "error")).toBe(true);
    expect(matchesOutcome(succeeded, "error")).toBe(false);
    expect(matchesOutcome(running, "error")).toBe(false);
  });

  it("keeps only completed non-errored operations for the ok outcome", () => {
    expect(matchesOutcome({ done: true }, "ok")).toBe(true);
    expect(matchesOutcome({ done: true, error: { message: "x" } }, "ok")).toBe(false);
    expect(matchesOutcome({ done: false }, "ok")).toBe(false);
  });

  it("keeps every operation for the all outcome", () => {
    expect(matchesOutcome({ done: true }, "all")).toBe(true);
    expect(matchesOutcome({ done: true, error: { message: "x" } }, "all")).toBe(true);
    expect(matchesOutcome({ done: false }, "all")).toBe(true);
  });
});
