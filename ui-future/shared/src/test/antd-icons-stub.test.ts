// Guard for the icon stub's completeness.
//
// A glyph imported in product code but missing from the stub does not fail
// loudly: the ESM linker cannot resolve the binding and the whole jest process
// leaves with code 0 and no report at all. Silence then reads as success — which
// is how an entire set of shared suites sat "green" without ever executing. This
// test turns that class into an ordinary red.

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import * as stub from "./antd-icons-stub";

const here = path.dirname(fileURLToPath(import.meta.url));
const uiFuture = path.resolve(here, "../../..");
// The jest configs that run shared/src map @ant-design/icons onto this stub.
const ROOTS = ["shared/src", "system/src", "vpc/src", "iam/src"];

const NAMED_IMPORT = /import\s*(?:type\s*)?\{([^}]*)\}\s*from\s*["']@ant-design\/icons["']/gs;

function sourceFiles(dir: string): string[] {
  let out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) out = out.concat(sourceFiles(full));
    else if (/\.tsx?$/.test(entry)) out.push(full);
  }
  return out;
}

function importedIcons(): Map<string, string> {
  const byIcon = new Map<string, string>();
  for (const root of ROOTS) {
    const abs = path.join(uiFuture, root);
    for (const file of sourceFiles(abs)) {
      if (file.endsWith("antd-icons-stub.tsx") || file.endsWith("antd-icons-stub.test.ts")) continue;
      const src = readFileSync(file, "utf8");
      for (const m of src.matchAll(NAMED_IMPORT)) {
        for (const raw of m[1].split(",")) {
          const name = raw.trim().split(" as ")[0].trim();
          if (name) byIcon.set(name, path.relative(uiFuture, file));
        }
      }
    }
  }
  return byIcon;
}

describe("@ant-design/icons test stub", () => {
  it("exports every glyph the product code imports by name", () => {
    const missing = [...importedIcons().entries()]
      .filter(([name]) => !(name in stub))
      .map(([name, file]) => `${name} (${file})`);
    expect(missing).toEqual([]);
  });

  it("covers a non-trivial number of glyphs — an empty scan would pass vacuously", () => {
    expect(importedIcons().size).toBeGreaterThan(20);
  });

  it("renders each glyph as an inert element", () => {
    expect(typeof stub.PlusOutlined).toBe("function");
    expect(typeof stub.default).toBe("function");
  });
});
