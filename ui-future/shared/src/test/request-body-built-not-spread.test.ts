// Anti-regression guard for the form → wire boundary.
//
// The helpers (buildUpdateBody / buildCreateBody) are unit-tested elsewhere. What
// they cannot prove is that the submit paths CALL them: put `{...parsed,
// update_mask: …}` back into any generic form and every behavioural test stays
// green, because none of them render a form. So this asserts the call, and
// forbids the shape it replaced, across every vendored copy at once — the copies
// are the reason a fix landed in one place used to miss the others.
//
// Why the shape matters: the edge parses request bodies with protojson
// DiscardUnknown, so a key that is not a field of the request message is dropped
// without a word and the caller still gets 200. Spreading a hydrated GET
// projection into a PATCH is exactly that, and it reads as working.

import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Every generic submit path, per app, with the helper it must route through. */
const EDIT_PATHS = [
  ["shared", "shared/src/components/organisms/ResourceEditPage/ResourceEditPage.tsx"],
  ["shared", "shared/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["compute", "compute/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["nlb", "nlb/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["registry", "registry/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["storage", "storage/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["compute", "compute/src/components/organisms/ResourceFormDialog/ResourceFormDialog.tsx"],
  ["nlb", "nlb/src/components/organisms/ResourceFormDialog/ResourceFormDialog.tsx"],
  ["registry", "registry/src/components/organisms/ResourceFormDialog/ResourceFormDialog.tsx"],
  ["storage", "storage/src/components/organisms/ResourceFormDialog/ResourceFormDialog.tsx"],
] as const;

const CREATE_PATHS = [
  ["shared", "shared/src/components/organisms/ResourceCreatePage/ResourceCreatePage.tsx"],
  ["shared", "shared/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["compute", "compute/src/components/organisms/ResourceCreatePage/ResourceCreatePage.tsx"],
  ["compute", "compute/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["nlb", "nlb/src/components/organisms/ResourceCreatePage/ResourceCreatePage.tsx"],
  ["nlb", "nlb/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["registry", "registry/src/components/organisms/ResourceCreatePage/ResourceCreatePage.tsx"],
  ["registry", "registry/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["storage", "storage/src/components/organisms/ResourceCreatePage/ResourceCreatePage.tsx"],
  ["storage", "storage/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
] as const;

const read = (rel: string) => readFileSync(path.join(uiRoot, rel), "utf8");

describe("generic submit paths build the request body", () => {
  it("covers every copy that exists on disk — a new remote must be listed here", () => {
    const listed = new Set<string>([...EDIT_PATHS, ...CREATE_PATHS].map(([, rel]) => rel));
    const found: string[] = [];
    for (const app of ["shared", "compute", "nlb", "registry", "storage", "vpc", "iam", "system"]) {
      for (const comp of [
        "ResourceEditPage/ResourceEditPage",
        "InlineResourceEditForm/InlineResourceEditForm",
        "ResourceCreatePage/ResourceCreatePage",
        "InlineResourceCreateForm/InlineResourceCreateForm",
        "ResourceFormDialog/ResourceFormDialog",
      ]) {
        const rel = `${app}/src/components/organisms/${comp}.tsx`;
        if (existsSync(path.join(uiRoot, rel))) found.push(rel);
      }
    }
    expect(found.filter((rel) => !listed.has(rel))).toEqual([]);
  });

  for (const [app, rel] of EDIT_PATHS) {
    it(`${app}: ${path.basename(rel)} sends only the masked fields`, () => {
      const src = read(rel);
      expect(src).toContain("buildUpdateBody(");
      // The shape this replaced: the hydrated GET projection spread into the PATCH.
      expect(src).not.toMatch(/\.\.\.\s*\(?\s*parsed[\s\S]{0,80}update_mask/);
      expect(src).not.toMatch(/update_mask:\s*mask\.map/);
    });
  }

  for (const [app, rel] of CREATE_PATHS) {
    it(`${app}: ${path.basename(rel)} strips form-only keys before sending`, () => {
      const src = read(rel);
      expect(src).toContain("buildCreateBody(");
      // A create that hands `parsed` straight to the mutation leaks the
      // `_`-discriminators, which the request-side case transform renames into
      // `Placement` / `AddressKind` / `BootSource` on the way out.
      expect(src).not.toMatch(/mutation\.mutate\(parsed\)/);
    });
  }
});

describe("vendored helper copies stay in step", () => {
  // The form → wire helpers exist once in shared and once per remote that does not
  // consume @shared. A behavioural fix to one must reach all of them (the
  // labels-carve-out fix had to touch five files). Compare each block by name,
  // ignoring comments — including the recursive `strip`, which is where the
  // carve-out actually lives and where a drift would hide.
  const BLOCKS = ["OPAQUE_FIELDS", "stripFormOnlyKeys", "strip", "buildUpdateBody", "buildCreateBody"] as const;

  /** Extract a top-level `const`/`function` block by name, comments stripped. */
  function block(src: string, name: string): string {
    const re = new RegExp(`^(?:export )?(?:const|function) ${name}\\b`, "m");
    const m = re.exec(src);
    if (!m || m.index === undefined) return `<missing ${name}>`;
    // m.index, NOT indexOf(m[0]): "function strip" occurs first inside
    // "export function stripFormOnlyKeys", and searching by text would extract
    // that block instead — silently comparing the wrong function.
    const i = m.index;
    let depth = 0;
    let started = false;
    let end = src.length;
    for (let k = i; k < src.length; k++) {
      const c = src[k];
      if (c === "{") {
        depth++;
        started = true;
      } else if (c === "}") {
        depth--;
        if (started && depth === 0) {
          end = k + 1;
          break;
        }
      } else if (!started && c === ";") {
        end = k + 1;
        break;
      }
    }
    return src
      .slice(i, end)
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith("//"))
      .join("\n");
  }

  const shared = read("shared/src/lib/update-mask.ts");

  for (const app of ["compute", "nlb", "registry", "storage"]) {
    const copy = read(`${app}/src/components/organisms/ResourceFormDialog/ResourceFormDialog.tsx`);
    for (const name of BLOCKS) {
      it(`${app} carries the same ${name}`, () => {
        const mine = block(copy, name);
        expect(mine).not.toMatch(/^<missing/);
        expect(mine).toBe(block(shared, name));
      });
    }
  }
});
