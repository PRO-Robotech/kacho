import { getByPath, setByPath, deleteByPath } from "./path";

// lib/path.getByPath is the single nested-path primitive. resource-registry's
// getByPath now delegates to it (previously a naive split('.').reduce that could
// not resolve bracket-indexed array segments). This locks the superset contract
// both surfaces rely on.
describe("path.getByPath", () => {
  const obj = {
    spec: { rules: [{ direction: "INGRESS" }, { direction: "EGRESS" }] },
    used_by: [{ referrer: { id: "nic-1" } }],
    name: "net-a",
  };

  it("resolves plain dotted paths", () => {
    expect(getByPath(obj, "name")).toBe("net-a");
    expect(getByPath(obj, "spec")).toEqual(obj.spec);
  });

  it("resolves bracket-indexed array paths", () => {
    expect(getByPath(obj, "spec.rules[0].direction")).toBe("INGRESS");
    expect(getByPath(obj, "spec.rules[1].direction")).toBe("EGRESS");
    expect(getByPath(obj, "used_by[0].referrer.id")).toBe("nic-1");
  });

  it("returns undefined for missing paths", () => {
    expect(getByPath(obj, "spec.rules[9].direction")).toBeUndefined();
    expect(getByPath(obj, "nope.here")).toBeUndefined();
    expect(getByPath(null, "a.b")).toBeUndefined();
  });
});

describe("path.setByPath / deleteByPath round-trip", () => {
  it("sets a nested bracket path immutably", () => {
    const src = { spec: { rules: [{ direction: "INGRESS" }] } };
    const next = setByPath(src, "spec.rules[0].direction", "EGRESS");
    expect(getByPath(next, "spec.rules[0].direction")).toBe("EGRESS");
    // original untouched
    expect(getByPath(src, "spec.rules[0].direction")).toBe("INGRESS");
  });

  it("deletes a nested key", () => {
    const src = { a: { b: 1, c: 2 } };
    const next = deleteByPath(src, "a.b");
    expect(getByPath(next, "a.b")).toBeUndefined();
    expect(getByPath(next, "a.c")).toBe(2);
  });
});
