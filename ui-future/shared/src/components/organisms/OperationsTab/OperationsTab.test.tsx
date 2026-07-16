import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { matchesOutcome } from "@shared/components/molecules/OperationsTable/opFilter";

const expectedExports = ["OperationsTab"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "OperationsTab.tsx"), "utf8");

describe("OperationsTab", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });

  it("wires an outcome quick-filter (Segmented) into the client-side predicate", () => {
    // Segmented «Все | С ошибкой | Успешные» + matchesOutcome в filtered-предикате.
    expect(source).toContain("Segmented");
    expect(source).toContain("matchesOutcome(o, outcome)");
    expect(source).toContain('setOutcome');
  });

  it("keeps only errored rows when the outcome filter is error", () => {
    const rows = [
      { id: "op-err", done: true, error: { code: 13, message: "boom" } },
      { id: "op-ok", done: true },
      { id: "op-run", done: false },
    ];
    const filtered = rows.filter((o) => matchesOutcome(o, "error"));
    expect(filtered.map((o) => o.id)).toEqual(["op-err"]);
  });
});
