import { render, screen } from "@testing-library/react";
import { KachoLogo } from ".";

describe("KachoLogo", () => {
  it("рисует один только знак в режиме mark", () => {
    render(<KachoLogo variant="mark" size={44} />);

    expect(screen.getByRole("img", { name: "Kachō" })).toBeInTheDocument();
    expect(screen.queryByText("Kachō")).not.toBeInTheDocument();
  });

  it("добавляет вордмарк в режиме full", () => {
    render(<KachoLogo variant="full" size={44} />);

    expect(screen.getByRole("img", { name: "Kachō" })).toBeInTheDocument();
    expect(screen.getByText("Kachō")).toBeInTheDocument();
  });

  it("масштабирует знак заданным размером", () => {
    const { container } = render(<KachoLogo variant="mark" size={64} />);

    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("width", "64");
    expect(svg).toHaveAttribute("height", "64");
  });

  it("красит вордмарк переданным цветом, а знак — нет", () => {
    render(<KachoLogo variant="full" size={24} wordmarkColor="rgb(255, 0, 0)" />);

    expect(screen.getByText("Kachō")).toHaveStyle({ color: "rgb(255, 0, 0)" });
  });
});
