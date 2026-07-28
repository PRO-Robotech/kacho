// vpc.v1.StaticRoute.next_hop is a oneof: `next_hop_address` XOR `gateway_id`.
// The panel saves by REPLACING the whole static_routes list, so whatever it loaded
// into its drafts is what gets written back for every row — including rows the
// operator never touched.
//
// It used to load a gateway route's id into the address draft
// (`next_hop_address: r.next_hop_address ?? r.gateway_id ?? ""`). Saving after
// editing an unrelated row therefore rewrote that route's arm: a gateway id was
// stored as if it were an IP. Both names are declared fields, so the edge accepts
// the body — strict parsing would not catch it either.

import { draftsFromRoutes, routesFromDrafts } from "./RoutesPanel";

describe("routes drafts round-trip", () => {
  it("keeps a gateway route on its own arm through load and save", () => {
    const fromServer = [
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
    ];
    const drafts = draftsFromRoutes(fromServer);
    expect(drafts[0]).toMatchObject({ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1", next_hop_address: "" });
    expect(routesFromDrafts(drafts)).toEqual([
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
    ]);
  });

  it("emits exactly one arm per route, never both", () => {
    for (const route of routesFromDrafts(draftsFromRoutes([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]))) {
      expect(Object.keys(route).sort()).toEqual(["destination_prefix", "gateway_id"]);
    }
  });

  it("drops a row with a destination but no next hop, and one with neither", () => {
    expect(
      routesFromDrafts([
        { destination_prefix: "10.0.0.0/8", next_hop_address: "  " },
        { destination_prefix: "  ", next_hop_address: "10.0.0.1" },
        { destination_prefix: " 10.1.0.0/16 ", next_hop_address: " 10.1.0.1 " },
      ]),
    ).toEqual([{ destination_prefix: "10.1.0.0/16", next_hop_address: "10.1.0.1" }]);
  });

  it("an address typed over a gateway row replaces the arm, deliberately", () => {
    const drafts = draftsFromRoutes([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]);
    drafts[0].next_hop_address = "10.0.0.1";
    expect(routesFromDrafts(drafts)).toEqual([{ destination_prefix: "0.0.0.0/0", next_hop_address: "10.0.0.1" }]);
  });
});
