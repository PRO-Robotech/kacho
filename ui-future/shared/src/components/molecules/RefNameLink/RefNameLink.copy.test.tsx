// Ссылка на ЧУЖОЙ ресурс тоже даёт скопировать значение.
//
// Предмет (#446). `RefNameLink` — самый частый вид ссылки в консоли (58 мест:
// карточки, расширения, колонки реестра). Копирование в общей ссылке было
// объявлено, а здесь не включено — то есть на карточке ресурса скопировать
// значение соседа было нечем ни в одном из этих мест.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { RefNameLink } from "./RefNameLink";

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
  globalThis.fetch = () =>
    jsonOk({ networks: [{ id: "net-7", name: "сеть-семь" }], nextPageToken: "" });
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

describe("RefNameLink — копирование значения", () => {
  it("даёт скопировать имя соседа, и кнопка стоит ВНЕ ссылки", async () => {
    const { container } = show(<RefNameLink specId="networks" refId="net-7" projectId="prj-1" />);

    await waitFor(() => expect(screen.getByText("сеть-семь")).toBeInTheDocument());

    const btn = screen.queryByRole("button", { name: /[Сс]копировать/ });
    expect(btn).not.toBeNull();
    expect(container.querySelector("a")?.querySelector("button")).toBeNull();

    fireEvent.click(btn as HTMLElement);
    await waitFor(() => expect(copied).toEqual(["сеть-семь"]));
  });
});
