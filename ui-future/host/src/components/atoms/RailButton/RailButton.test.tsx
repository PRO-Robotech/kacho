import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Home } from "lucide-react";
import { jest } from "@jest/globals";
import { RailButton } from ".";

describe("RailButton", () => {
  it("renders an accessible rail button", () => {
    render(<RailButton label="Все сервисы" icon={<Home size={18} />} active />);

    expect(screen.getByRole("button", { name: "Все сервисы" })).toHaveAttribute("data-active", "true");
  });

  it("handles clicks", async () => {
    const user = userEvent.setup();
    const onClick = jest.fn();
    render(<RailButton label="Все сервисы" icon={<Home size={18} />} onClick={onClick} />);

    await user.click(screen.getByRole("button", { name: "Все сервисы" }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
