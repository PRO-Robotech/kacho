// Whether a resource has any row action at all.
//
// A table that always appends the actions column gives a read-only resource an
// empty column with a menu button that opens nothing. The predicate has to agree
// with the menu itself: the same "can this be moved" list decides both, so it is
// declared once and read by both.
//
// The move dialog is a stub that prints the REST call it would make. Offering it
// for a resource whose API has no such verb advertises an operation that does
// not exist, so every domain that owns resources declares them move-incapable.

import { REGISTRY } from "@shared/lib/resource-registry";
import { resourceHasRowActions } from "./RowActionsMenu";

describe("resourceHasRowActions", () => {
  it("is false for a read-only catalog resource", () => {
    const diskTypes = REGISTRY["disk-types"];
    expect(diskTypes.ops).toEqual({ create: false, update: false, delete: false });
    expect(resourceHasRowActions(diskTypes)).toBe(false);
  });

  it("is true when the resource can be updated or deleted", () => {
    expect(resourceHasRowActions(REGISTRY.networks)).toBe(true);
  });

  it("is true for a move-capable resource even without update or delete", () => {
    const readOnlyButMovable = { ...REGISTRY["disk-types"], id: "some-movable-thing" };
    expect(resourceHasRowActions(readOnlyButMovable)).toBe(true);
  });

  it("counts the domain-declared move-incapable resources", () => {
    // Each remote declared its own domain's resources move-incapable; the list
    // is one closed set here, so a resource is not move-capable in one app and
    // move-incapable in another.
    for (const id of [
      "compute-instances",
      "volumes",
      "snapshots",
      "disk-types",
      "registries",
      "repositories",
      "tags",
    ]) {
      const spec = { ...REGISTRY["disk-types"], id };
      expect({ id, hasActions: resourceHasRowActions(spec) }).toEqual({ id, hasActions: false });
    }
  });
});
