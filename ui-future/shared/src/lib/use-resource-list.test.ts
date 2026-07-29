// Where the parent id of a list goes: into the path or into the query.
//
// Some list paths are child paths — `/registry/v1/registries/{registryId}/repositories`
// — and the backend scopes them by the path segment, not by a query parameter.
// A hook that only ever appends the parent to the query leaves the `{registryId}`
// literal in the URL, so the request goes out as written and the backend answers
// InvalidArgument. The guard matters just as much: until the parent is known the
// request must not go out at all, or every poll spends a 400.
//
// This lives next to the hook as a pure function so the rule is testable without
// mounting react-query.

import { resolveListPath } from "./use-resource-list";

describe("resolveListPath", () => {
  it("puts the parent id in the query when the path has no placeholder for it", () => {
    expect(resolveListPath("/vpc/v1/subnets", "project_id", "prj-1")).toEqual({
      path: "/vpc/v1/subnets",
      query: { project_id: "prj-1" },
      resolved: true,
    });
  });

  it("substitutes the parent id into the path when the path names it", () => {
    // registry_id → {registryId}: snake_case field, camelCase placeholder.
    expect(resolveListPath("/registry/v1/registries/{registryId}/repositories", "registry_id", "reg-1")).toEqual({
      path: "/registry/v1/registries/reg-1/repositories",
      query: {},
      resolved: true,
    });
  });

  it("reports unresolved while any placeholder is still unfilled", () => {
    // The repositories list is reached with project_id as the parent, so
    // {registryId} stays unfilled — the request must not be issued.
    const out = resolveListPath("/registry/v1/registries/{registryId}/repositories", "project_id", "prj-1");
    expect(out.resolved).toBe(false);
    expect(out.path).toContain("{registryId}");
  });

  it("reports unresolved when there is no parent at all", () => {
    expect(resolveListPath("/registry/v1/registries/{registryId}/repositories", null, null).resolved).toBe(false);
  });

  it("leaves a plain path untouched when there is no parent", () => {
    expect(resolveListPath("/storage/v1/diskTypes", null, null)).toEqual({
      path: "/storage/v1/diskTypes",
      query: {},
      resolved: true,
    });
  });
});
