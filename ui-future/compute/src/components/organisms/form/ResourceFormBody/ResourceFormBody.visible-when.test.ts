// `visibleWhen` gates a field on the value of another. The declared comparison
// type is `string | string[]`, but the value it reads can be any form value — a
// bool toggle reads `false`, which is not the string "false". Comparing the two
// with `===` makes the gated field unreachable forever, which is how the manual
// network-interface editor disappeared behind the "use the project default"
// toggle.

import { matchesVisibleWhen } from "./ResourceFormBody";

describe("matchesVisibleWhen", () => {
  it("gates on a bool toggle, which reads as a boolean and not a string", () => {
    expect(matchesVisibleWhen({ use_default_network: false }, { field: "use_default_network", equals: "false" })).toBe(
      true,
    );
    expect(matchesVisibleWhen({ use_default_network: true }, { field: "use_default_network", equals: "false" })).toBe(
      false,
    );
  });

  it("still gates on an enum discriminator", () => {
    expect(matchesVisibleWhen({ instance_kind: "VM" }, { field: "instance_kind", equals: "VM" })).toBe(true);
    expect(matchesVisibleWhen({ instance_kind: "CONTAINER" }, { field: "instance_kind", equals: "VM" })).toBe(false);
    expect(matchesVisibleWhen({ _address_kind: "external" }, { field: "_address_kind", equals: ["external", "internal"] })).toBe(true);
  });

  it("shows a field with no gate, and hides one whose gate reads nothing", () => {
    expect(matchesVisibleWhen({}, undefined)).toBe(true);
    expect(matchesVisibleWhen({}, { field: "instance_kind", equals: "VM" })).toBe(false);
  });
});
