// Потребитель-СЕТЬ показывается ссылкой с именем, а не сырым типом и id.
//
// Предмет (#446). Владелец: на карточке группы безопасности раздел «Потребители»
// задублирован — «оставить иконку с именем и ссылкой».
//
// Раздел объявлен один раз; двоится сама СЕТЬ. Сервер отдаёт в `used_by` группы
// два вида потребителей: сетевой интерфейс и **сеть** — та, для которой группа
// назначена группой по умолчанию. Тип `network` в справочнике консоли
// отсутствовал, поэтому строка «Потребители» уходила в запасную ветку и рисовала
// ту же сеть сырым `network net-…`, тогда как строкой выше она же стоит
// нормальной ссылкой. Один предмет назван дважды подряд и в двух разных видах —
// это и читается как дублирование раздела.
//
// Отрицательный контроль обязателен: запасная ветка нужна и остаётся — тип,
// которого консоль не знает, показывается без ссылки, а не выдумывает адрес.
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { ReferrerLink } from "./spec-columns";

const realFetch = globalThis.fetch;

function jsonOk(body: unknown): Promise<Response> {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

beforeEach(() => {
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

describe("ReferrerLink", () => {
  it("сеть-потребитель рисуется ссылкой с именем", async () => {
    const { container } = show(<ReferrerLink projectId="prj-1" referrer={{ type: "network", id: "net-7" }} />);

    await waitFor(() => expect(screen.getByText("сеть-семь")).toBeInTheDocument());
    expect(container.querySelector("a")).toHaveAttribute("href", "/projects/prj-1/vpc/networks/net-7");
  });

  it("неизвестный тип остаётся без ссылки (отрицательный контроль)", () => {
    const { container } = show(<ReferrerLink projectId="prj-1" referrer={{ type: "нечто", id: "xyz-1" }} />);

    expect(screen.getByText("xyz-1")).toBeInTheDocument();
    expect(container.querySelector("a")).toBeNull();
  });
});
