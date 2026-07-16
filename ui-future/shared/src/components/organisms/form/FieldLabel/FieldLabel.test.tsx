import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["FieldLabel"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "FieldLabel.tsx"), "utf8");

describe("FieldLabel", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });
});
