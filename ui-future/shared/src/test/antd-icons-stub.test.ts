// Guard for the icon stubs' completeness.
//
// A glyph imported in product code but missing from the stub does not fail
// loudly: the ESM linker cannot resolve the binding and the whole jest process
// leaves with code 0 and no report at all. Silence then reads as success — which
// is how an entire set of shared suites sat "green" without ever executing. This
// test turns that class into an ordinary red.
//
// Scope is DERIVED from the jest configs, never hand-listed. The tree holds six
// stubs (shared, plus one each in host/compute/storage/nlb/registry) and only
// the shared one was guarded: a glyph added in compute/src would have killed
// that package's whole run silently, with no gate anywhere to see it. A
// hand-written list of roots is what produced that blind spot, so here every
// stub and every source tree comes out of `<pkg>/jest.config.cjs` itself —
// `moduleNameMapper` says which stub a package links against, `roots` says
// whether the package also runs shared/src.
//
// Stubs are read statically (their exported names), because a test living in
// shared/ cannot import a module that exists only inside another package's jest
// module registry. The runtime shape — a real component, a default export — is
// asserted on the shared stub, which this file does import.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import * as sharedStub from "./antd-icons-stub";

const here = path.dirname(fileURLToPath(import.meta.url));
const uiFuture = path.resolve(here, "../../..");
const SHARED_STUB = "shared/src/test/antd-icons-stub.tsx";

const NAMED_IMPORT = /import\s*(?:type\s*)?\{([^}]*)\}\s*from\s*["']@ant-design\/icons["']/gs;
const ANY_IMPORT = /from\s*["']@ant-design\/icons["']/;
// moduleNameMapper entry redirecting the icon package onto a stub file.
const STUB_MAPPING = /"\^@ant-design\/icons\$":\s*"<rootDir>\/([^"]+)"/;
// `roots` entry pulling shared/src into the package's run.
const SHARED_ROOT = '"<rootDir>/../shared/src"';
const STUB_EXPORT = /export const (\w+)\s*=/g;

interface Target {
  // stub path, relative to ui-future/
  stub: string;
  // source trees whose imports this stub has to satisfy
  trees: string[];
}

function sourceFiles(dir: string): string[] {
  if (!existsSync(dir)) return [];
  let out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) out = out.concat(sourceFiles(full));
    else if (/\.tsx?$/.test(entry)) out.push(full);
  }
  return out;
}

// productFiles — sources of a tree, minus the stub and this gate itself.
function productFiles(tree: string): string[] {
  return sourceFiles(path.join(uiFuture, tree)).filter((file) => !path.basename(file).startsWith("antd-icons-stub"));
}

// packagesWithJest — every console package that runs jest.
function packagesWithJest(): string[] {
  return readdirSync(uiFuture)
    .filter((entry) => existsSync(path.join(uiFuture, entry, "jest.config.cjs")))
    .sort();
}

// targets — stub → trees jest links against it; plus packages that import the
// glyphs while mapping nothing, which is the original hang (kacho#7).
function targets(): { list: Target[]; unmapped: string[] } {
  const byStub = new Map<string, Target>();
  const unmapped: string[] = [];
  for (const pkg of packagesWithJest()) {
    const cfg = readFileSync(path.join(uiFuture, pkg, "jest.config.cjs"), "utf8");
    const trees = [`${pkg}/src`];
    if (cfg.includes(SHARED_ROOT)) trees.push("shared/src");
    const mapping = STUB_MAPPING.exec(cfg);
    if (!mapping) {
      if (trees.some((tree) => productFiles(tree).some((f) => ANY_IMPORT.test(readFileSync(f, "utf8"))))) {
        unmapped.push(pkg);
      }
      continue;
    }
    const stub = path.relative(uiFuture, path.resolve(uiFuture, pkg, mapping[1]));
    const target = byStub.get(stub) ?? { stub, trees: [] };
    byStub.set(stub, target);
    for (const tree of trees) if (!target.trees.includes(tree)) target.trees.push(tree);
  }
  return { list: [...byStub.values()].sort((a, b) => a.stub.localeCompare(b.stub)), unmapped };
}

// importedIcons — glyph → the file naming it, over the given trees.
function importedIcons(trees: string[]): Map<string, string> {
  const byIcon = new Map<string, string>();
  for (const tree of trees) {
    for (const file of productFiles(tree)) {
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

function stubExports(stub: string): Set<string> {
  const src = readFileSync(path.join(uiFuture, stub), "utf8");
  return new Set([...src.matchAll(STUB_EXPORT)].map((m) => m[1]));
}

// missingIn — glyphs the stub does not export. The verdict below and the
// self-check share this helper, so the self-check exercises the real comparison.
function missingIn(exports: Set<string>, imports: Map<string, string>): string[] {
  return [...imports.entries()].filter(([name]) => !exports.has(name)).map(([name, file]) => `${name} (${file})`);
}

const { list, unmapped } = targets();
const scanned = list.map((t) => ({ ...t, exports: stubExports(t.stub), imports: importedIcons(t.trees) }));

describe("@ant-design/icons test stubs", () => {
  it("every stub exports each glyph the trees linked against it import by name", () => {
    const bad = scanned.flatMap((t) => missingIn(t.exports, t.imports).map((m) => `  ${t.stub}: ${m}`));
    expect(`${bad.length} glyph(s) imported by product code and absent from the stub:\n${bad.join("\n")}`).toBe(
      "0 glyph(s) imported by product code and absent from the stub:\n",
    );
  });

  it("no package links the real icon package while importing from it", () => {
    expect(unmapped).toEqual([]);
  });

  it("covers every stub in the tree — an empty scan would pass vacuously", () => {
    // Census of what was read, so "no findings" stays distinguishable from
    // "nothing was opened". Stubs on disk and stubs reached through a config are
    // the same set: a stub nobody links against is dead weight, and a linked
    // stub outside this scan is a blind spot.
    const onDisk = packagesWithJest()
      .concat("shared")
      .map((pkg) => `${pkg}/src/test/antd-icons-stub.tsx`)
      .filter((rel) => existsSync(path.join(uiFuture, rel)))
      .sort();
    expect(scanned.map((t) => t.stub).sort()).toEqual(onDisk);
    expect(scanned.reduce((n, t) => n + t.imports.size, 0)).toBeGreaterThan(200);
    expect(scanned.reduce((n, t) => n + t.trees.length, 0)).toBeGreaterThan(8);
  });

  it("the comparison reddens on an absent glyph and stays silent on a present one", () => {
    const exports = stubExports(SHARED_STUB);
    expect(missingIn(exports, new Map([["NoSuchOutlined", "probe.tsx"]]))).toEqual(["NoSuchOutlined (probe.tsx)"]);
    expect(missingIn(exports, new Map([["PlusOutlined", "probe.tsx"]]))).toEqual([]);
  });

  it("renders each glyph as an inert element", () => {
    expect(typeof sharedStub.PlusOutlined).toBe("function");
    expect(typeof sharedStub.default).toBe("function");
  });
});
