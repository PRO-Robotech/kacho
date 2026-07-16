import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { LabelsCell } from "./LabelsCell";

describe("LabelsCell", () => {
  const writeText = jest.fn<(text: string) => Promise<void>>();

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    jest.spyOn(toast, "success").mockReturnValue("toast-id");
    jest.spyOn(toast, "error").mockReturnValue("toast-id");
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("renders an empty placeholder when labels are empty", () => {
    render(<LabelsCell labels={{}} />);

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders visible labels and collapses the remaining count", () => {
    render(
      <LabelsCell
        max={2}
        labels={{
          env: "prod",
          owner: "network",
          region: "eu",
        }}
      />,
    );

    expect(screen.getByText("env=prod")).toBeInTheDocument();
    expect(screen.getByText("owner=network")).toBeInTheDocument();
    expect(screen.queryByText("region=eu")).not.toBeInTheDocument();
    expect(screen.getByText("+1")).toBeInTheDocument();
  });

  it("copies a label without bubbling the click", async () => {
    const onClick = jest.fn();

    render(
      <div onClick={onClick}>
        <LabelsCell labels={{ env: "prod" }} />
      </div>,
    );

    fireEvent.click(screen.getByText("env=prod"));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("env=prod"));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Скопировано: env=prod"));
    expect(onClick).not.toHaveBeenCalled();
  });

  it("shows an error toast when copying fails", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));

    render(<LabelsCell labels={{ env: "prod" }} />);

    fireEvent.click(screen.getByText("env=prod"));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Не удалось скопировать"));
  });
});
