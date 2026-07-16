import { render, screen } from "@testing-library/react";
import { HomePage } from ".";

describe("HomePage", () => {
  it("renders cloud services page copy", () => {
    render(<HomePage />);

    expect(screen.getByRole("heading", { name: "Сервисы облака" })).toBeInTheDocument();
    expect(screen.getByText("Host shell for future federated modules")).toBeInTheDocument();
  });
});
