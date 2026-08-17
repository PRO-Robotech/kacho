// What the VIP picker offers, for the placement the operator chose.
//
// The mode arms are not cosmetic: `public {}` is the ONLY source a load balancer
// on an EXTERNAL placement accepts (services/nlb/.../vip_source.go,
// validateSourceTypeMatrix), and `subnet_id` the only auto arm an INTERNAL one
// does. The picker used to read `type` off the form object — a field the form
// stopped sending because writing it is an explicit reject — so it saw undefined,
// defaulted to INTERNAL, and never offered the public arm at all. That left the
// default (EXTERNAL_REGIONAL) load balancer with no reachable form state whose
// body the server accepts.
//
// The public arm renders its own explanatory line and no selector; the INTERNAL
// auto arm renders a subnet selector with the placement in its placeholder. Both
// are asserted from the rendered output, not from the props of a mocked widget.

import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { NlbVipSourceField } from "./NlbVipSourceField";

const withProviders = (ui: ReactNode) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
};

/** The form object as the create page holds it: placement, and the default picker state. */
function formValue(placement: string, modes: { v4?: string; v6?: string } = {}) {
  return {
    placement,
    vip_source: {
      _v4_mode: modes.v4 ?? "subnet",
      v4: { subnet_id: "", address_id: "" },
      _v6_mode: modes.v6 ?? "subnet",
      v6: { subnet_id: "", address_id: "" },
    },
  };
}

describe("VIP source picker follows the chosen placement", () => {
  it("offers the platform-allocated public VIP on an EXTERNAL placement", () => {
    withProviders(<NlbVipSourceField value={formValue("EXTERNAL_REGIONAL")} onChange={() => {}} />);
    // Rendered only when the resolved mode is `public` — reachable only if the
    // picker resolved the placement to an EXTERNAL load balancer.
    expect(screen.getAllByText(/Публичный VIP выделяется платформой автоматически/).length).toBeGreaterThan(0);
  });

  it("offers subnet auto-allocation on an INTERNAL placement", () => {
    withProviders(<NlbVipSourceField value={formValue("INTERNAL_ZONAL")} onChange={() => {}} />);
    expect(screen.queryByText(/Публичный VIP выделяется платформой автоматически/)).toBeNull();
    // The subnet candidate list is narrowed by the placement of the load balancer;
    // the placeholder names it, so a wrong derivation is visible here.
    //
    // Queried as TEXT, not as a `placeholder` attribute (#418). The module used to
    // carry its own antd double whose `Select` spread every prop onto the DOM
    // `<select>`, so `placeholder` became an attribute — something the real antd
    // never produces: it renders the placeholder as visible text in the selector.
    // The assertion was therefore pinned to the shape of the double. The shared
    // double renders it as the leading option, i.e. as the operator sees it, so
    // this reads the product instead. Same discriminating power — ZONAL still
    // fails a REGIONAL derivation — on a fact that survives the double.
    expect(screen.getAllByText(/Подсеть \(ZONAL\)/).length).toBeGreaterThan(0);
  });

  it("narrows subnet candidates to REGIONAL on a regional internal placement", () => {
    withProviders(<NlbVipSourceField value={formValue("INTERNAL_REGIONAL")} onChange={() => {}} />);
    expect(screen.getAllByText(/Подсеть \(REGIONAL\)/).length).toBeGreaterThan(0);
  });

  // Declining a family is an arm of its own, and it renders no selector at all.
  //
  // On an EXTERNAL placement the `public` arm yields a source UNCONDITIONALLY,
  // so with no `off` arm both families always went on the wire and an IPv4-only
  // load balancer was inexpressible — while the service accepts a source for
  // just one family.
  //
  // This probe asserts what the CHOSEN arm produces. Which arms are OFFERED is
  // asserted separately, below: until #553 the shared double drew no options at
  // all, so that statement was unreachable here and had to be deferred to the
  // browser probe.
  it("declining a family renders an explanation and no selector", () => {
    withProviders(<NlbVipSourceField value={formValue("EXTERNAL_REGIONAL", { v6: "off" })} onChange={() => {}} />);

    expect(screen.getAllByText(/IPv6 Адрес не задаётся/).length).toBeGreaterThan(0);
    // (+) paired positive control: the v4 family, left on its auto arm, still
    // renders its own line. Without it "the off arm renders" could equally mean
    // "the picker collapsed for every family".
    expect(screen.getAllByText(/Публичный VIP выделяется платформой автоматически/).length).toBeGreaterThan(0);
    // The IPv4 line must not be the declined one.
    expect(screen.queryByText(/IPv4 Адрес не задаётся/)).toBeNull();
  });
});

// WHICH ARMS THE OPERATOR IS OFFERED — the statement #543 was about.
//
// The arms live in an antd `Segmented`, whose options arrive as a prop. The
// shared double used to render none of them, so this was not merely hard to
// assert here: it was unobservable by construction, and any probe claiming it
// would have been green whatever the picker offered (#553).
//
// The arm set is not cosmetic. `public {}` is the only source an EXTERNAL
// placement accepts and `subnet_id` the only auto arm an INTERNAL one does, so
// offering the wrong set means the operator can build a body the server rejects
// — or cannot build the one it accepts at all.
//
// Both directions are asserted in every case: the arm that must be there, and
// the arm of the other placement that must NOT be. A one-sided assertion would
// pass just as well on a picker that offers everything to everyone.
describe("VIP source picker: which arms are offered", () => {
  const armNames = (name: RegExp | string) => screen.queryAllByRole("radio", { name });

  it("offers the public arm on an EXTERNAL placement, and not the subnet arm", () => {
    withProviders(<NlbVipSourceField value={formValue("EXTERNAL_REGIONAL")} onChange={() => {}} />);

    // Two families, one picker each — hence two of every arm.
    expect(armNames("Публичный (авто)")).toHaveLength(2);
    expect(armNames("Линк адреса")).toHaveLength(2);
    expect(armNames("Не задавать")).toHaveLength(2);
    expect(armNames("Из подсети (авто)")).toHaveLength(0);
  });

  it("offers the subnet arm on an INTERNAL placement, and not the public one", () => {
    withProviders(<NlbVipSourceField value={formValue("INTERNAL_ZONAL")} onChange={() => {}} />);

    expect(armNames("Из подсети (авто)")).toHaveLength(2);
    expect(armNames("Линк адреса")).toHaveLength(2);
    expect(armNames("Не задавать")).toHaveLength(2);
    expect(armNames("Публичный (авто)")).toHaveLength(0);
  });

  // Offered and SELECTABLE are different claims: an arm that is drawn but does
  // not move the form is exactly the defect the picker had before #543, only
  // one step later in the chain.
  it("picking an arm writes the family mode into the form object", () => {
    let latest: Record<string, unknown> = formValue("EXTERNAL_REGIONAL");
    withProviders(<NlbVipSourceField value={latest} onChange={(next) => (latest = next)} />);

    // The IPv4 picker is the first of the two.
    fireEvent.click(armNames("Не задавать")[0]);

    expect((latest.vip_source as Record<string, unknown>)._v4_mode).toBe("off");
    // (+) paired control: the other family keeps the value it started with, so
    // "writes the mode" is not satisfied by a handler that rewrites both.
    expect((latest.vip_source as Record<string, unknown>)._v6_mode).toBe("subnet");
  });
});
