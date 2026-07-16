import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Anti-drift guard (sec-hardening-r3): the generic resource CRUD organisms
 * (ResourceListPage / ResourceCreatePage / ResourceEditPage) MUST live in a
 * single place — shared/src/components/organisms — and be consumed by every
 * micro-frontend via @shared. Previously each of `vpc` and `iam` shipped its
 * own forked copy which had already drifted (ResourceListPage: iam grew a
 * `disableChildRoute` prop + iam-service handling the vpc copy lacked). A
 * bugfix landed in one remote silently missed the other.
 *
 * This test fails if:
 *   - the shared implementation is missing, or
 *   - any per-app copy re-grows a real component implementation instead of a
 *     thin re-export from @shared.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

const COMPONENTS = ["ResourceListPage", "ResourceCreatePage", "ResourceEditPage"] as const;
const APPS = ["vpc", "iam"] as const;

describe("shared resource CRUD organisms are single-source", () => {
  for (const comp of COMPONENTS) {
    const sharedFile = path.join(repoRoot, "shared/src/components/organisms", comp, `${comp}.tsx`);

    it(`${comp} has a real implementation in shared/`, () => {
      expect(existsSync(sharedFile)).toBe(true);
      const src = readFileSync(sharedFile, "utf8");
      expect(src).toContain(`export function ${comp}`);
    });

    for (const app of APPS) {
      const appDir = path.join(repoRoot, app, "src/components/organisms", comp);
      const appImpl = path.join(appDir, `${comp}.tsx`);

      it(`${app}/${comp} does not fork the implementation (re-export from @shared only)`, () => {
        // A forked copy would define the component itself. The only allowed
        // per-app artefact is a thin index re-export from @shared.
        expect(existsSync(appImpl)).toBe(false);

        const indexFile = path.join(appDir, "index.ts");
        if (existsSync(indexFile)) {
          const idx = readFileSync(indexFile, "utf8");
          expect(idx).toContain("@shared/components/organisms/" + comp);
          expect(idx).not.toContain(`export function ${comp}`);
        }
      });
    }
  }
});
