import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { HeaderActions } from ".";

describe("HeaderActions", () => {
  it("renders only the original theme toggle action", () => {
    render(<HeaderActions dark={false} setDark={jest.fn()} />);

    expect(screen.getByRole("button", { name: "Включить тёмную тему" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Search" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Activity" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Notifications" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "API reachability" })).not.toBeInTheDocument();
  });

  it("toggles theme", async () => {
    const user = userEvent.setup();
    const setDark = jest.fn();
    render(<HeaderActions dark={false} setDark={setDark} />);

    await user.click(screen.getByRole("button", { name: "Включить тёмную тему" }));

    expect(setDark).toHaveBeenCalledTimes(1);
  });
});
