import { render, screen } from "@testing-library/react";
import { NlbVipCell } from "./NlbVipCell";

describe("NlbVipCell", () => {
  it("renders both VIP address ids", () => {
    render(<NlbVipCell v4AddressId="adr-v4-000000000000000" v6AddressId="adr-v6-000000000000000" />);
    expect(screen.getByText("adr-v4-000000000000000")).toBeInTheDocument();
    expect(screen.getByText("adr-v6-000000000000000")).toBeInTheDocument();
  });

  it("renders a dash when no VIP is allocated yet", () => {
    render(<NlbVipCell />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
