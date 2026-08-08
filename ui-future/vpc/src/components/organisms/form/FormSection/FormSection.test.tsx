import { fireEvent, render, screen } from "@testing-library/react";
import { FormSection } from ".";

describe("FormSection", () => {
  it("рисует заголовок и содержимое", () => {
    render(
      <FormSection title="Идентичность">
        <div data-testid="fields" />
      </FormSection>,
    );

    expect(screen.getByText("Идентичность")).toBeInTheDocument();
    expect(screen.getByTestId("fields")).toBeInTheDocument();
  });

  it("несворачиваемая секция не притворяется кнопкой", () => {
    render(
      <FormSection title="Идентичность">
        <div data-testid="fields" />
      </FormSection>,
    );

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText("Идентичность").parentElement).not.toHaveAttribute("aria-expanded");
  });

  it("сворачиваемая секция объявляет своё состояние", () => {
    render(
      <FormSection title="Расширенное" collapsible>
        <div data-testid="fields" />
      </FormSection>,
    );

    expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "true");
  });

  it("по умолчанию может быть закрыта — тогда содержимого нет вовсе", () => {
    render(
      <FormSection title="Расширенное" collapsible defaultOpen={false}>
        <div data-testid="fields" />
      </FormSection>,
    );

    expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "false");
    // Именно НЕТ, а не спрятано: закрытая секция не должна оставлять в форме
    // поля, которые пользователь не видит, но которые уедут в тело запроса.
    expect(screen.queryByTestId("fields")).not.toBeInTheDocument();
  });

  it("клик по заголовку раскрывает и снова сворачивает", () => {
    render(
      <FormSection title="Расширенное" collapsible defaultOpen={false}>
        <div data-testid="fields" />
      </FormSection>,
    );

    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByTestId("fields")).toBeInTheDocument();
    expect(screen.getByRole("button")).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(screen.getByRole("button"));
    expect(screen.queryByTestId("fields")).not.toBeInTheDocument();
  });

  it("работает с клавиатуры: Enter и пробел переключают, прочие клавиши — нет", () => {
    render(
      <FormSection title="Расширенное" collapsible defaultOpen={false}>
        <div data-testid="fields" />
      </FormSection>,
    );

    const header = screen.getByRole("button");
    expect(header).toHaveAttribute("tabindex", "0");

    fireEvent.keyDown(header, { key: "Enter" });
    expect(screen.getByTestId("fields")).toBeInTheDocument();

    fireEvent.keyDown(header, { key: " " });
    expect(screen.queryByTestId("fields")).not.toBeInTheDocument();

    fireEvent.keyDown(header, { key: "a" });
    expect(screen.queryByTestId("fields")).not.toBeInTheDocument();
  });

  it("несворачиваемую секцию клик не закрывает", () => {
    render(
      <FormSection title="Идентичность">
        <div data-testid="fields" />
      </FormSection>,
    );

    fireEvent.click(screen.getByText("Идентичность"));

    expect(screen.getByTestId("fields")).toBeInTheDocument();
  });
});
