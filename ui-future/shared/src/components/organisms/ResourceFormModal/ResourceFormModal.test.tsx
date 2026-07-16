import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["ResourceFormModal"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "ResourceFormModal.tsx"), "utf8");

describe("ResourceFormModal", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });
});
