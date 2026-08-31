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
  // Пропсы прокидываются: без этого КАЖДЫЙ приглушённый текст получал бы один и
  // тот же признак, и проба не отличала бы оговорку о неоднозначности от чего-то
  // другого, стоящего рядом.
  const Text = ({ children, ...rest }: React.PropsWithChildren<Record<string, unknown>>) =>
    React.createElement("span", { "data-testid": "note", ...rest }, children);
  return {
    __esModule: true,
    Typography: Object.assign(({ children }: React.PropsWithChildren) => React.createElement("div", null, children), {
      Text,
    }),
    Result: ({
      status,
      title,
      subTitle,
      extra,
    }: {
      status?: string;
      title?: React.ReactNode;
      subTitle?: React.ReactNode;
      extra?: React.ReactNode;
    }) =>
      React.createElement(
        "div",
        { "data-status": status ?? "", role: "alert" },
        React.createElement("h3", null, title),
        React.createElement("div", { "data-testid": "subtitle" }, subTitle),
        extra,
      ),
  };
});

const { ApiError } = await import("@shared/api/client");
const { NOT_FOUND_IS_AMBIGUOUS } = await import("@shared/lib/error-presentation");
const { ErrorResult } = await import("./ErrorResult");
const { MemoryRouter } = await import("react-router");

function statusOf(): string | null {
  return screen.getByRole("alert").getAttribute("data-status");
}

describe("ErrorResult", () => {
  it("на 404 показывает текст сервера дословно и оговорку о неоднозначности", () => {
    render(<ErrorResult error={new ApiError(404, 5, null, "Subnet sub-1 not found")} />);

    expect(statusOf()).toBe("404");
    expect(screen.getByTestId("subtitle")).toHaveTextContent("Subnet sub-1 not found");
    expect(screen.getByTestId("note")).toHaveTextContent(NOT_FOUND_IS_AMBIGUOUS);
  });

  it("на 403 оговорки нет — там неоднозначности не возникает", () => {
    render(<ErrorResult error={new ApiError(403, 7, null, "no path")} />);

    expect(statusOf()).toBe("403");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("на 5xx показывает отказ сервера без оговорки", () => {
    render(<ErrorResult error={new ApiError(503, 14, null, "peer unavailable")} />);

    expect(statusOf()).toBe("500");
    expect(screen.getByTestId("subtitle")).toHaveTextContent("peer unavailable");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("подставленный сверху статус снимает оговорку", () => {
    // Оговорка относится к ОТВЕТУ; под чужим статусом она была бы утверждением
    // не о нём.
    render(<ErrorResult error={new ApiError(404, 5, null, "Subnet sub-1 not found")} status="warning" />);

    expect(statusOf()).toBe("warning");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("подставленный сверху текст снимает оговорку", () => {
    render(
      <ErrorResult error={new ApiError(404, 5, null, "Subnet sub-1 not found")} subTitle="Раздел ещё не реализован" />,
    );

    expect(screen.getByTestId("subtitle")).toHaveTextContent("Раздел ещё не реализован");
    expect(screen.queryByTestId("note")).not.toBeInTheDocument();
  });

  it("код протокола не читается в тексте — он лежит в подсказке при наведении", () => {
    render(<ErrorResult error={new ApiError(404, 5, null, "Subnet sub-1 not found")} />);

    const subtitle = screen.getByTestId("subtitle");
    expect(subtitle).toHaveTextContent("Subnet sub-1 not found");
    expect(subtitle.textContent ?? "").not.toMatch(/\bNOT_FOUND\b|\b5\b/);
    expect(subtitle.querySelector("span[title]")?.getAttribute("title")).toBe("NOT_FOUND (5) · HTTP 404");
  });

  // Положительный контроль к предыдущей: под подставленным сверху текстом
  // подсказки нет — она относилась бы не к этому ответу.
  it("подставленный сверху текст снимает и подсказку", () => {
    render(<ErrorResult error={new ApiError(404, 5, null, "x")} subTitle="Раздел ещё не реализован" />);
    expect(screen.getByTestId("subtitle").querySelector("span[title]")).toBeNull();
  });

  it("заголовок вызывающего перебивает вычисленный", () => {
    render(<ErrorResult error={new ApiError(404, 5, null, "x")} title="Нет такой страницы" />);
    expect(screen.getByRole("heading", { name: "Нет такой страницы" })).toBeInTheDocument();
  });

  it("сетевой отказ отличает от ответа сервера", () => {
    const netFail = new TypeError("Failed to fetch");
    render(<ErrorResult error={netFail} />);

    expect(statusOf()).toBe("500");
    expect(screen.getByRole("heading", { name: "Сеть недоступна" })).toBeInTheDocument();
  });

  it("центрирует по умолчанию и не центрирует по просьбе", () => {
    const centered = render(<ErrorResult error={new ApiError(404, 5, null, "x")} />);
    expect((centered.container.firstElementChild as HTMLElement).style.display).toBe("flex");

    const plain = render(<ErrorResult error={new ApiError(404, 5, null, "x")} centered={false} />);
    expect(plain.container.firstElementChild).toHaveAttribute("role", "alert");
  });

  it("показывает действия, переданные вызывающим", () => {
    render(
      <ErrorResult error={new ApiError(403, 7, null, "no path")} extra={<button type="button">К списку</button>} />,
    );
    expect(screen.getByRole("button", { name: "К списку" })).toBeInTheDocument();
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// ОТКАЗ ПО ПРЕДЕЛУ ВЕДЁТ НА ВИТРИНУ (#1605)
//
// Отказ без адреса витрины оставляет упёршегося ровно там, где он упёрся:
// он не знает ни каков предел, ни кто его задал. Ссылка — и есть следующий шаг.
describe("отказ по пределу уводит на витрину квот (#1605)", () => {
  function quotaError(metadata?: Record<string, string>) {
    return new ApiError(
      429,
      8,
      [
        {
          "@type": "type.googleapis.com/google.rpc.ErrorInfo",
          reason: "QUOTA_EXCEEDED",
          domain: "vpc.kacho.cloud",
          ...(metadata ? { metadata } : {}),
        },
      ],
      "project prj-1 has reached its limit of 5 vpc.network",
    );
  }

  it("носитель назван — на экране ссылка на квоты этого проекта", () => {
    render(
      <MemoryRouter>
        <ErrorResult error={quotaError({ kind: "vpc.network", carrier_type: "project", carrier_id: "prj-1" })} />
      </MemoryRouter>,
    );

    const link = screen.getByRole("link", { name: /Квоты/ });
    expect(link).toHaveAttribute("href", "/projects/prj-1/quotas");
    // Текст производителя на экран не идёт — он в подсказке.
    expect(screen.getByTestId("subtitle")).toHaveTextContent("Облачные сети");
    expect(screen.getByTestId("subtitle").textContent ?? "").not.toContain("has reached its limit");
  });

  it("носителя не назвали — ссылки нет, но экран всё равно называет раздел", () => {
    // Адрес не подделывается. Положительный контроль к утверждению выше: без
    // него «ссылка есть» зеленело бы на компоненте, который её рисует всегда.
    render(
      <MemoryRouter>
        <ErrorResult error={quotaError()} />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByTestId("note")).toHaveTextContent("Квоты");
  });

  it("отказ НЕ про предел ссылки не получает", () => {
    render(
      <MemoryRouter>
        <ErrorResult error={new ApiError(429, 8, null, "too many authorization checks; retry later")} />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByTestId("subtitle")).toHaveTextContent("too many authorization checks");
  });

  it("вызывающий, подставивший своё дополнение, его и получает", () => {
    render(
      <MemoryRouter>
        <ErrorResult
          error={quotaError({ carrier_type: "project", carrier_id: "prj-1" })}
          extra={<button type="button">Повторить</button>}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("button", { name: "Повторить" })).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
