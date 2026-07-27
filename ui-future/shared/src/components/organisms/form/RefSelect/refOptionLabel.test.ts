// The zone picker is where a tenant actually meets geo. Every placeable resource
// takes its zoneId from here, and a zone that is closed to placement looks
// exactly like an open one unless the option says so — the request then fails
// later, at Create, with no hint that the choice was the problem.
//
// The closed option stays selectable on purpose: the server decides what is
// allowed, and greying it out here would be the UI narrowing rights it does not
// own. It is labelled, not removed.

import { refOptionExtra } from "./refOptionLabel";

describe("refOptionExtra — zones", () => {
  it("shows the region a zone belongs to", () => {
    expect(refOptionExtra("zones", { id: "ru-central1-a", region_id: "ru-central1" })).toBe("ru-central1");
  });

  it("marks a zone that is closed to placement", () => {
    const extra = refOptionExtra("zones", {
      id: "ru-central1-a",
      region_id: "ru-central1",
      open_for_placement: false,
    });
    expect(extra).toContain("ru-central1");
    expect(extra).toContain("закрыта для размещения");
  });

  it("adds no marker to an open zone", () => {
    expect(refOptionExtra("zones", { id: "ru-central1-a", region_id: "ru-central1", open_for_placement: true })).toBe(
      "ru-central1",
    );
  });

  it("adds no marker when the server said nothing about placement", () => {
    // Absent is not closed. Claiming otherwise would push the operator away from
    // a zone that may well be usable.
    expect(refOptionExtra("zones", { id: "ru-central1-a", region_id: "ru-central1" })).not.toContain("закрыта");
  });
});

describe("refOptionExtra — regions", () => {
  it("shows the region id and marks a closed one", () => {
    expect(refOptionExtra("regions", { id: "ru-central1" })).toBe("ru-central1");
    expect(refOptionExtra("regions", { id: "ru-central1", open_for_placement: false })).toContain(
      "закрыт для размещения",
    );
  });
});

describe("refOptionExtra — unrelated resources are untouched", () => {
  it("keeps the existing address-pool hint", () => {
    expect(refOptionExtra("address-pools", { cidr_blocks: ["10.0.0.0/24"], is_default: true })).toBe(
      "10.0.0.0/24 · default",
    );
  });

  it("returns nothing for a resource with no hint", () => {
    expect(refOptionExtra("projects", { id: "prj-1" })).toBe("");
  });
});
