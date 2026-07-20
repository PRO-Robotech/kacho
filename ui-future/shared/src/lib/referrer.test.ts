import { referrerHref, referrerMeta } from "./referrer";

// kacho.cloud.reference.Referrer.type приходит в ДВУХ формах:
//  - legacy underscore  (`compute_instance`)   — vpc/compute/nlb remotes
//  - canonical dotted   (`compute.instance`)    — storage-remote (Volume.used_by)
// Cross-remote deep-link для dotted-формы обязан строиться через referrerHref
// (прямой host-route), т.к. RefNameLink резолвит цель из ЛОКАЛЬНОГО registry
// remote'а, а cross-remote-цель там отсутствует. Эти тесты локают, что обе формы
// дают идентичный route/label.
describe("referrerHref — dotted + underscore referrer types", () => {
  const projectId = "prj-1";
  const id = "ins-abc";
  const instanceHref = `/projects/${projectId}/compute/instances/${id}`;

  it("links a dotted storage.used_by → compute.instance referrer (cross-remote)", () => {
    expect(referrerHref(projectId, { type: "compute.instance", id })).toBe(instanceHref);
  });

  it("still links the legacy underscore compute_instance referrer", () => {
    expect(referrerHref(projectId, { type: "compute_instance", id })).toBe(instanceHref);
  });

  it("returns null without a project (forward-compat plain-text fallback)", () => {
    expect(referrerHref(null, { type: "compute.instance", id })).toBeNull();
    expect(referrerHref(undefined, { type: "compute.instance", id })).toBeNull();
  });

  it("returns null for unsupported types so caller renders plain text", () => {
    expect(referrerHref(projectId, { type: "some.unknown.thing", id })).toBeNull();
    expect(referrerHref(projectId, { type: "compute.instance" })).toBeNull(); // no id
  });
});

describe("referrerMeta — dotted + underscore parity", () => {
  it("labels a dotted compute.instance the same as underscore compute_instance", () => {
    expect(referrerMeta("compute.instance")).toEqual(referrerMeta("compute_instance"));
    expect(referrerMeta("compute.instance").label).toBe("VM");
  });

  it("keeps the original (non-normalized) type in the unknown fallback label", () => {
    expect(referrerMeta("storage.mystery")).toEqual({ label: "storage.mystery" });
    expect(referrerMeta(undefined)).toEqual({ label: "?" });
  });
});
