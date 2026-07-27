// openForPlacement° is the single actionable placement signal a tenant gets. The
// badge has to be readable as three distinct answers — open, closed, and "the
// server did not tell us" — and must never present the third as the second.

import { render, screen } from "@testing-library/react";
import { PlacementBadge } from "./PlacementBadge";

describe("PlacementBadge", () => {
  it("reads open", () => {
    render(<PlacementBadge open={true} />);
    expect(screen.getByText("Открыт")).toBeInTheDocument();
  });

  it("reads closed", () => {
    render(<PlacementBadge open={false} />);
    expect(screen.getByText("Закрыт")).toBeInTheDocument();
  });

  it("reads unknown when the field is absent — not as closed", () => {
    render(<PlacementBadge open={undefined} />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByText("Закрыт")).not.toBeInTheDocument();
  });

  it("spells out why a closed zone is closed", () => {
    render(<PlacementBadge open={false} reason="REGION_DOWN" />);
    expect(screen.getByText(/егион/)).toBeInTheDocument();
  });

  it("does not attach a cause to an open row", () => {
    // precedence resolves to NONE while the zone is up; showing a cause there
    // would contradict the badge next to it.
    render(<PlacementBadge open={true} reason="NONE" />);
    expect(screen.queryByText(/егион/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Зона/)).not.toBeInTheDocument();
  });

  it("stays silent about a cause it does not recognise", () => {
    render(<PlacementBadge open={false} reason={"SOMETHING_NEW" as never} />);
    expect(screen.getByText("Закрыт")).toBeInTheDocument();
    expect(screen.queryByText(/SOMETHING_NEW/)).not.toBeInTheDocument();
  });
});
