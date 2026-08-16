// registry.v1.CreateRegistryRequest is {project_id, name, description, labels,
// region_id}. `default_repository_visibility` exists only on
// UpdateRegistryRequest (tag 6) and on the Registry resource itself (tag 9).
//
// Offering it at Create meant the operator could pick PUBLIC, the edge — parsing
// with protojson DiscardUnknown — dropped the key, and the registry came back
// PRIVATE behind a success toast.

import { REGISTRY, applyFieldDefaults } from "./resource-registry";
import { buildCreateBody, computeUpdateMask } from "@shared/lib/update-mask";

const asObj = (v: unknown) => v as Record<string, unknown>;

/**
 * The body a Create actually sends. NOT `spec.template(ctx)` — the form seeds
 * defaults over the template first, so asserting the template alone is green over
 * a live defect: `applyFieldDefaults` was putting the update-only visibility back.
 */
function createBody(specId: string): Record<string, unknown> {
  const spec = REGISTRY[specId];
  const seeded = applyFieldDefaults(spec.fields, asObj(spec.template({ projectId: "prj-1" })));
  return buildCreateBody(spec.sanitize ? spec.sanitize(seeded) : seeded);
}

describe("registries spec vs the request messages", () => {
  const spec = REGISTRY["registries"];

  it("does not send an Update-only field in a Create body", () => {
    // CreateRegistryRequest is {project_id, name, description, labels, region_id}.
    // It does not read this field at all, so the edge drops it in silence.
    expect(createBody("registries")).not.toHaveProperty("default_repository_visibility");
    expect(asObj(spec.template({ projectId: "prj-1" }))).not.toHaveProperty("default_repository_visibility");
  });

  it("no spec seeds a Create body with any update-only field, of any type", () => {
    // applyFieldDefaults has one branch per field type; a guard in only one of them
    // fixes today's enum and leaves the next update-only string/int/bool open.
    for (const [id, spec] of Object.entries(REGISTRY)) {
      const updateOnly = (spec.fields ?? []).filter((f) => f.updateOnly).map((f) => f.name.split(".")[0]);
      if (updateOnly.length === 0) continue;
      const body = createBody(id);
      for (const name of updateOnly) expect(body).not.toHaveProperty(name);
    }
  });

  it("marks it update-only, so the Create form does not offer it", () => {
    const f = (spec.fields ?? []).find((x) => x.name === "default_repository_visibility");
    expect(f).toBeDefined();
    expect(f!.updateOnly).toBe(true);
    // …and it still reaches the mask, because Update does carry it.
    const mask = computeUpdateMask(
      { default_repository_visibility: "PRIVATE" },
      { default_repository_visibility: "PUBLIC" },
      spec.fields ?? [],
    );
    expect(mask).toContain("default_repository_visibility");
  });

  it("region_id is Create-only and never masked", () => {
    const fields = spec.fields ?? [];
    const before: Record<string, unknown> = {};
    const after: Record<string, unknown> = {};
    for (const f of fields) {
      before[f.name.split(".")[0]] = "before";
      after[f.name.split(".")[0]] = "after";
    }
    expect(computeUpdateMask(before, after, fields)).not.toContain("region_id");
  });
});
