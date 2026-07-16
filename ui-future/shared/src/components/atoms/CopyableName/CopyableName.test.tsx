import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { CopyableName } from "./CopyableName";

describe("CopyableName", () => {
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

  it("renders an unnamed placeholder when no value is available", () => {
    render(<CopyableName name="" />);

    expect(screen.getByText("(unnamed)")).toBeInTheDocument();
  });

  it("copies the provided name without bubbling the click", async () => {
    const onClick = jest.fn();

    render(
      <div onClick={onClick}>
        <CopyableName name="frontend-subnet" />
      </div>,
    );

    const button = screen.getByRole("button", { name: "frontend-subnet" });

    fireEvent.click(button);

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("frontend-subnet"));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Имя скопировано"));
    expect(onClick).not.toHaveBeenCalled();
    expect(button).toHaveAttribute("title", "Скопировано");
  });

  it("copies the fallback id when the name is empty", async () => {
    render(<CopyableName name="" fallback="subnet-123" />);

    const button = screen.getByRole("button", { name: "subnet-123" });

    expect(button).toHaveAttribute("title", "Имя не задано — скопировать ID");

    fireEvent.click(button);

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("subnet-123"));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("ID скопирован"));
  });

  it("shows an error toast when copying fails", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));

    render(<CopyableName name="frontend-subnet" />);

    fireEvent.click(screen.getByRole("button", { name: "frontend-subnet" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Не удалось скопировать"));
  });
});
