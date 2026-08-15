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

import { render, screen } from "@testing-library/react";
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
function formValue(placement: string) {
  return {
    placement,
    vip_source: {
      _v4_mode: "subnet",
      v4: { subnet_id: "", address_id: "" },
      _v6_mode: "subnet",
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
});
