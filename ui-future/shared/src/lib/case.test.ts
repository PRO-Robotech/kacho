// Direction and opacity contract for the request/response key transformer.
//
// The transformer rewrites FIELD NAMES between the UI's snake_case and the
// camelCase Kachō REST speaks. It must never rewrite MAP KEYS: those are tenant
// data. `labels` and `annotations` were already carved out; `match_labels` is the
// same kind of map — `iam.v1.Rule.match_labels` selects objects by their labels,
// and a label key may legally contain `_` (`[a-z][-_0-9a-z]*`).
//
// This one is invisible to strict parsing at the edge: `matchLabels` IS a declared
// field, so a request whose selector keys were silently renamed parses cleanly and
// simply matches nothing.

import { camelToSnake, snakeToCamel } from "./case";

describe("snakeToCamel (request direction)", () => {
  it("renames field names", () => {
    expect(snakeToCamel({ project_id: "prj-1", v4_cidr_blocks: [] })).toEqual({ projectId: "prj-1", v4CidrBlocks: [] });
  });

  it("leaves the keys of a tenant map alone", () => {
    expect(
      snakeToCamel({
        labels: { team_lead: "ada" },
        annotations: { some_note: "x" },
        rules: [{ match_labels: { team_lead: "ada", env_tier: "prod" }, resource_names: ["net-1"] }],
      }),
    ).toEqual({
      labels: { team_lead: "ada" },
      annotations: { some_note: "x" },
      rules: [{ matchLabels: { team_lead: "ada", env_tier: "prod" }, resourceNames: ["net-1"] }],
    });
  });
});

describe("camelToSnake (response direction)", () => {
  it("renames field names", () => {
    expect(camelToSnake({ projectId: "prj-1", createdAt: "t" })).toEqual({ project_id: "prj-1", created_at: "t" });
  });

  it("leaves the keys of a tenant map alone, whichever spelling the tenant chose", () => {
    expect(camelToSnake({ matchLabels: { teamLead: "ada" }, labels: { teamLead: "ada" } })).toEqual({
      match_labels: { teamLead: "ada" },
      labels: { teamLead: "ada" },
    });
  });
});
