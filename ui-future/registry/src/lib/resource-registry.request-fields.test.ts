// registry.v1.CreateRegistryRequest is {project_id, name, description, labels,
// region_id}. `default_repository_visibility` exists only on
// UpdateRegistryRequest (tag 6) and on the Registry resource itself (tag 9).
//
// Offering it at Create meant the operator could pick PUBLIC, the edge — parsing
// with protojson DiscardUnknown — dropped the key, and the registry came back
// PRIVATE behind a success toast.

import { REGISTRY } from "./resource-registry";
import { computeUpdateMask } from "@/components/organisms/ResourceFormDialog";

const asObj = (v: unknown) => v as Record<string, unknown>;

describe("registries spec vs the request messages", () => {
  const spec = REGISTRY["registries"];

  it("does not seed a Create body with an Update-only field", () => {
    expect(asObj(spec.template({ projectId: "prj-1" }))).not.toHaveProperty("default_repository_visibility");
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
