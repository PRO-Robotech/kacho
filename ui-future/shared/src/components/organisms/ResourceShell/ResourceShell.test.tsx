import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["ResourceShell"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "ResourceShell.tsx"), "utf8");

describe("ResourceShell", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });
});
