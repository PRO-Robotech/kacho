import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { CopyableName } from "./CopyableName";

describe("CopyableName", () => {
  const writeText = jest.fn<(text: string) => Promise<void>>();

  // Утверждения ссылаются на СПЯЩИЕ ФУНКЦИИ, а не на `toast.success`/`toast.error`
  // по имени владельца: метод, оторванный от своего объекта, теряет получателя —
  // сегодня тела этих методов `this` не читают, но проба не должна зависеть от
  // свойства чужого тела.
  let successSpy: jest.Spied<typeof toast.success>;
  let errorSpy: jest.Spied<typeof toast.error>;

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    successSpy = jest.spyOn(toast, "success").mockReturnValue("toast-id");
    errorSpy = jest.spyOn(toast, "error").mockReturnValue("toast-id");
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

    document.addEventListener("click", onClick);
    try {
      render(<CopyableName name="frontend-subnet" />);

      const button = screen.getByRole("button", { name: "frontend-subnet" });

      fireEvent.click(button);

      await waitFor(() => expect(writeText).toHaveBeenCalledWith("frontend-subnet"));
      await waitFor(() => expect(successSpy).toHaveBeenCalledWith("Имя скопировано"));
      expect(onClick).not.toHaveBeenCalled();
      expect(button).toHaveAttribute("title", "Скопировано");
    } finally {
      document.removeEventListener("click", onClick);
    }
  });

  it("copies the fallback id when the name is empty", async () => {
    render(<CopyableName name="" fallback="subnet-123" />);

    const button = screen.getByRole("button", { name: "subnet-123" });

    expect(button).toHaveAttribute("title", "Имя не задано — скопировать ID");

    fireEvent.click(button);

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("subnet-123"));
    await waitFor(() => expect(successSpy).toHaveBeenCalledWith("ID скопирован"));
  });

  it("shows an error toast when copying fails", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));

    render(<CopyableName name="frontend-subnet" />);

    fireEvent.click(screen.getByRole("button", { name: "frontend-subnet" }));

    await waitFor(() => expect(errorSpy).toHaveBeenCalledWith("Не удалось скопировать"));
  });
});
