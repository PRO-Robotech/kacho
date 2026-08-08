// ErrorResult — единственное место, где отказ сервера превращается в экран.
// Существенны три решения, и каждое можно испортить, не сломав ничего видимо:
//
//   * 404 сопровождается оговоркой о неоднозначности. Край прячет существование
//     (`security.md` §hide-existence): недоступный ресурс отвечает промахом,
//     дословно совпадающим с настоящим. Экран, утверждающий «не существует»,
//     выдумывает факт; «нет доступа» — выдумывает обратный;
//   * оговорка НЕ показывается, когда статус или текст подставлены сверху —
//     иначе она относилась бы не к тому ответу;
//   * текст сервера идёт дословно: тон сообщений — часть контракта.
//
// antd переопределён локально: общий стенд подменяет `Result` пустым div'ом,
// который своих пропсов не рисует, — на нём ни одно из трёх решений не
// наблюдаемо, и проба зеленела бы при любом.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";

jest.unstable_mockModule("antd", () => {
  const Text = ({ children }: React.PropsWithChildren<Record<string, unknown>>) =>
    React.createElement("span", { "data-testid": "note" }, children);
  return {
    __esModule: true,
    Typography: Object.assign(({ children }: React.PropsWithChildren) => React.createElement("div", null, children), {
      Text,
    }),
    Result: ({ status, title, subTitle, extra }: Record<string, React.ReactNode>) =>
      React.createElement(
        "div",
        { "data-status": String(status), role: "alert" },
        React.createElement("h3", null, title),
        React.createElement("div", { "data-testid": "subtitle" }, subTitle),
        extra,
      ),
  };
});

const { ApiError } = await import("@shared/api/client");
const { NOT_FOUND_IS_AMBIGUOUS } = await import("@shared/lib/error-presentation");
const { ErrorResult } = await import("./ErrorResult");

function statusOf(): string | null {
  return screen.getByRole("alert").getAttribute("data-status");
}

describe("ErrorResult", () => {
  it("на 404 показывает текст сервера дословно и оговорку о неоднозначности", () => {
    render(<ErrorResult error={new ApiError(404, "NOT_FOUND", null, "Subnet sub-1 not found")} />);

    expect(statusOf()).toBe("404");
    expect(screen.getByTestId("subtitle")).toHaveTextContent("NOT_FOUND: Subnet sub-1 not found");
    expect(screen.getByTestId("note")).toHaveTextContent(NOT_FOUND_IS_AMBIGUOUS);
  });

  it("на 403 оговорки нет — там неоднозначности не возникает", () => {
    render(<ErrorResult error={new ApiError(403, "PERMISSION_DENIED", null, "no path")} />);

    expect(statusOf()).toBe("403");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("на 5xx показывает отказ сервера без оговорки", () => {
    render(<ErrorResult error={new ApiError(503, "UNAVAILABLE", null, "peer unavailable")} />);

    expect(statusOf()).toBe("500");
    expect(screen.getByTestId("subtitle")).toHaveTextContent("UNAVAILABLE: peer unavailable");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("подставленный сверху статус снимает оговорку", () => {
    // Оговорка относится к ОТВЕТУ; под чужим статусом она была бы утверждением
    // не о нём.
    render(<ErrorResult error={new ApiError(404, "NOT_FOUND", null, "Subnet sub-1 not found")} status="warning" />);

    expect(statusOf()).toBe("warning");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("подставленный сверху текст снимает оговорку", () => {
    render(
      <ErrorResult
        error={new ApiError(404, "NOT_FOUND", null, "Subnet sub-1 not found")}
        subTitle="Раздел ещё не реализован"
      />,
    );

    expect(screen.getByTestId("subtitle")).toHaveTextContent("Раздел ещё не реализован");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("заголовок вызывающего перебивает вычисленный", () => {
    render(<ErrorResult error={new ApiError(404, "NOT_FOUND", null, "x")} title="Нет такой страницы" />);
    expect(screen.getByRole("heading", { name: "Нет такой страницы" })).toBeInTheDocument();
  });

  it("сетевой отказ отличает от ответа сервера", () => {
    const netFail = new TypeError("Failed to fetch");
    render(<ErrorResult error={netFail} />);

    expect(statusOf()).toBe("500");
    expect(screen.getByRole("heading", { name: "Сеть недоступна" })).toBeInTheDocument();
  });

  it("центрирует по умолчанию и не центрирует по просьбе", () => {
    const centered = render(<ErrorResult error={new ApiError(404, "NOT_FOUND", null, "x")} />);
    expect((centered.container.firstElementChild as HTMLElement).style.display).toBe("flex");

    const plain = render(<ErrorResult error={new ApiError(404, "NOT_FOUND", null, "x")} centered={false} />);
    expect(plain.container.firstElementChild).toHaveAttribute("role", "alert");
  });

  it("показывает действия, переданные вызывающим", () => {
    render(
      <ErrorResult
        error={new ApiError(403, "PERMISSION_DENIED", null, "no path")}
        extra={<button type="button">К списку</button>}
      />,
    );
    expect(screen.getByRole("button", { name: "К списку" })).toBeInTheDocument();
  });
});
