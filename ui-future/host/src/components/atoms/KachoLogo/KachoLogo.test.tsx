import { render, screen } from "@testing-library/react";
import { KachoLogo } from ".";

describe("KachoLogo", () => {
  it("renders the mark", () => {
    render(<KachoLogo variant="mark" size={44} />);

    expect(screen.getByRole("img", { name: "Kacho" })).toBeInTheDocument();
  });

  it("renders the wordmark in full mode", () => {
    render(<KachoLogo variant="full" size={44} />);

    expect(screen.getByRole("img", { name: "Kacho" })).toBeInTheDocument();
    expect(screen.getByText("Kacho")).toBeInTheDocument();
  });
});
