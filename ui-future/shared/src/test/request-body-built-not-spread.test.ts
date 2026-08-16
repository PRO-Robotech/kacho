// Anti-regression guard for the form → wire boundary.
//
// The helpers (buildUpdateBody / buildCreateBody) are unit-tested elsewhere. What
// they cannot prove is that the submit paths CALL them: put `{...parsed,
// update_mask: …}` back into any generic form and every behavioural test stays
// green, because none of them render a form. So this asserts the call, and
// forbids the shape it replaced, across every submit path at once.
//
// The four vendored copies of the helpers are gone (#405): they lived in a
// `ResourceFormDialog` whose component was never rendered — zero occurrences of
// `<ResourceFormDialog` in the tree — and only its helpers were consumed. The
// `describe` that kept those copies "in step" went with them: a check whose
// subject no longer exists is a finding, not a safety net. Single source is now
// held by shared-organisms-single-source.test.ts and the fork ledger.
//
// Why the shape matters: the edge parses request bodies with protojson
// DiscardUnknown, so a key that is not a field of the request message is dropped
// without a word and the caller still gets 200. Spreading a hydrated GET
// projection into a PATCH is exactly that, and it reads as working.

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/**
 * Top-level directories that are not apps of this tree.
 *
 * `shared` is deliberately NOT here: it ships submit paths of its own and is
 * listed below like any other consumer.
 */
const NOT_APPS = new Set(["node_modules", "deploy", "docs", "scripts", ".git"]);

/**
 * Every app on disk, discovered BY FACT — the sibling guard
 * (shared-organisms-single-source.test.ts) already enumerates this way, and for
 * the same reason.
 *
 * The previous revision spelled eight names inline while its own census claimed
 * to cover "every copy that exists on disk". Two directories of this tree were
 * absent from that list (`host`, which already ships an `organisms/` folder, and
 * `dashboard`), so a submit path appearing in either would have been invisible to
 * a check whose entire purpose is to notice exactly that. A list of names cannot
 * make that claim; reading the directory can.
 */
function discoverApps(): string[] {
  return readdirSync(uiRoot)
    .filter((name) => !NOT_APPS.has(name))
    .filter((name) => {
      const dir = path.join(uiRoot, name);
      return statSync(dir).isDirectory() && existsSync(path.join(dir, "src"));
    })
    .sort();
}

const APPS = discoverApps();

/** Every generic submit path, per app, with the helper it must route through. */
const EDIT_PATHS = [
  ["shared", "shared/src/components/organisms/ResourceEditPage/ResourceEditPage.tsx"],
  ["shared", "shared/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["compute", "compute/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["nlb", "nlb/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["registry", "registry/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
  ["storage", "storage/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx"],
] as const;

// ResourceCreatePage is no longer listed per remote: the four vendored copies
// were folded into the shared one (see shared-organisms-single-source.test.ts),
// so the shared entry below covers every app that renders it.
const CREATE_PATHS = [
  ["shared", "shared/src/components/organisms/ResourceCreatePage/ResourceCreatePage.tsx"],
  ["shared", "shared/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["compute", "compute/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["nlb", "nlb/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["registry", "registry/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
  ["storage", "storage/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx"],
] as const;

const read = (rel: string) => readFileSync(path.join(uiRoot, rel), "utf8");

describe("generic submit paths build the request body", () => {
  it("lists nothing that no longer exists — a stale entry covers nothing", () => {
    // The lists above are the inverse of the census below: that one catches a
    // copy nobody listed, this one catches an entry with nothing left to cover.
    // Without it, folding a copy away turns the guard into an ENOENT crash
    // instead of a statement about the tree.
    const missing = [...EDIT_PATHS, ...CREATE_PATHS]
      .map(([, rel]) => rel)
      .filter((rel) => !existsSync(path.join(uiRoot, rel)));
    expect(missing).toEqual([]);
  });

  it("own premise: apps are enumerated by fact, and the sweep read something", () => {
    // "Nothing unlisted" must be distinguishable from "nothing scanned": a moved
    // uiRoot or a renamed layout would otherwise make the census below vacuously
    // true. The known consumers are pinned so a discovery regression cannot
    // shrink the sweep unnoticed — but they are a FLOOR, not the list: an app
    // added tomorrow is covered without editing this file, which is the whole
    // point of reading the directory instead of naming eight of them.
    expect(APPS.length).toBeGreaterThan(0);
    expect(APPS).toEqual(
      expect.arrayContaining(["compute", "iam", "nlb", "registry", "shared", "storage", "system", "vpc"]),
    );
    // The two directories the previous inline list omitted. Pinned by name on
    // purpose: if either is ever removed this fails and the entry goes with it,
    // rather than quietly becoming a claim about nothing.
    expect(APPS).toEqual(expect.arrayContaining(["dashboard", "host"]));
  });

  it("covers every copy that exists on disk — a new remote must be listed here", () => {
    const listed = new Set<string>([...EDIT_PATHS, ...CREATE_PATHS].map(([, rel]) => rel));
    const found: string[] = [];
    for (const app of APPS) {
      for (const comp of [
        "ResourceEditPage/ResourceEditPage",
        "InlineResourceEditForm/InlineResourceEditForm",
        "ResourceCreatePage/ResourceCreatePage",
        "InlineResourceCreateForm/InlineResourceCreateForm",
      ]) {
        const rel = `${app}/src/components/organisms/${comp}.tsx`;
        if (existsSync(path.join(uiRoot, rel))) found.push(rel);
      }
    }
    // Census in the message: the number of apps swept and copies found, so a
    // green run states what it looked at rather than only that it passed.
    expect({
      apps: APPS.length,
      copiesFound: found.length,
      unlisted: found.filter((rel) => !listed.has(rel)),
    }).toEqual({ apps: APPS.length, copiesFound: found.length, unlisted: [] });
    expect(found.length).toBeGreaterThan(0);
  });

  for (const [app, rel] of EDIT_PATHS) {
    it(`${app}: ${path.basename(rel)} sends only the masked fields`, () => {
      const src = read(rel);
      expect(src).toContain("buildUpdateBody(");
      // The shape this replaced: the hydrated GET projection spread into the PATCH.
      expect(src).not.toMatch(/\.\.\.\s*\(?\s*parsed[\s\S]{0,80}update_mask/);
      expect(src).not.toMatch(/update_mask:\s*mask\.map/);
      // The mask is computed against the SANITIZED form object, so the stored
      // original must be in that same shape — otherwise a spec whose sanitize
      // rewrites a value (number → Duration "300s") looks changed on every save
      // and pushes an untouched field into update_mask.
      expect(src).toMatch(/originalRef\.current = .*sanitize/s);
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
