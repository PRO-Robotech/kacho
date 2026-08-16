// Ссылка на ресурс IAM — тот же вид, что у всех остальных ссылок консоли.
//
// Предмет (#446). `IamRefLink` собирал разметку руками, мимо `ResourceLink`, —
// то есть был ВТОРОЙ реализацией «иконка + имя + ссылка». Два места об одном
// предмете уже разошлись: у общей ссылки появилось копирование значения, у этой
// не появилось, и на карточке IAM скопировать значение было нечем.
//
// Утверждается наблюдаемое: адрес карточки, показанное имя и исход нажатия на
// копирование. Тождество реализаций проверяется не сравнением текстов файлов, а
// тем, что обе дают одинаковый набор наблюдаемого.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { IamRefLink } from "./IamRefLink";

const realFetch = globalThis.fetch;
let copied: string[] = [];

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

beforeEach(() => {
  copied = [];
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: (v: string) => (copied.push(v), Promise.resolve()) },
  });
  globalThis.fetch = () => jsonOk({ id: "prj-7", name: "проект-семь" });
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

function show(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("IamRefLink", () => {
  it("ведёт на карточку ресурса IAM и показывает его имя", async () => {
    const { container } = show(<IamRefLink specId="projects" refId="prj-7" />);

    await waitFor(() => expect(screen.getByText("проект-семь")).toBeInTheDocument());
    expect(container.querySelector("a")).toHaveAttribute("href", "/iam/projects/prj-7");
  });

  it("даёт скопировать значение, и кнопка стоит ВНЕ ссылки", async () => {
    const { container } = show(<IamRefLink specId="projects" refId="prj-7" />);

    await waitFor(() => expect(screen.getByText("проект-семь")).toBeInTheDocument());

    const btn = screen.queryByRole("button", { name: /[Сс]копировать/ });
    expect(btn).not.toBeNull();
    // Кнопка внутри ссылки гасит клик — переход не происходит (правило 5 `ui.md`).
    expect(container.querySelector("a")?.querySelector("button")).toBeNull();

    fireEvent.click(btn as HTMLElement);
    await waitFor(() => expect(copied).toEqual(["проект-семь"]));
  });

  it("без идентификатора ссылки нет вовсе (отрицательный контроль)", () => {
    const { container } = show(<IamRefLink specId="projects" refId={undefined} />);
    expect(container.querySelector("a")).toBeNull();
  });
});
