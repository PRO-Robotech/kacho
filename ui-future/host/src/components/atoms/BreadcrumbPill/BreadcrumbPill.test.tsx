import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { theme } from "antd";
import { jest } from "@jest/globals";
import { BreadcrumbPill } from ".";

const TestHarness = ({ active = false, onClick }: { active?: boolean; onClick?: () => void }) => {
  const { token } = theme.useToken();
  return (
    <BreadcrumbPill token={token} active={active} placeholder="Выберите аккаунт" chevron onClick={onClick}>
      personal-cloud
    </BreadcrumbPill>
  );
};

describe("BreadcrumbPill", () => {
  it("renders placeholder while inactive", () => {
    render(<TestHarness />);

    expect(screen.getByRole("button", { name: /выберите аккаунт/i })).toBeInTheDocument();
  });

  it("renders selected value and handles click while active", async () => {
    const user = userEvent.setup();
    const onClick = jest.fn();
    render(<TestHarness active onClick={onClick} />);

    await user.click(screen.getByRole("button", { name: /personal-cloud/i }));

    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
