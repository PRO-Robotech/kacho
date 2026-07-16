import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["DetailHeaderProvider", "useDetailHeaderIcon", "PanelHeader"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "PanelHeader.tsx"), "utf8");

describe("PanelHeader", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });
});
