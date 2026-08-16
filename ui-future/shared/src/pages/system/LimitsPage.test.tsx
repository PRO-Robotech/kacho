// Величины пределов меняются ТОЛЬКО из раздела администратора (#364).
//
// ПРЕДМЕТ. Две поверхности с разными правами: арендатор читает свои пределы,
// администратор облака меняет величины. Смешать их нельзя в обе стороны —
// кнопка изменения на тенантской странице обещала бы возможность, которой у
// арендатора нет (и заканчивалась бы отказом), а раздел администратора без неё
// не решал бы свою единственную задачу.

import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { requestBody, requestUrl } from "@shared/test/fetch-capture";
import LimitsPage, { LIMITS_PATH } from "./LimitsPage";

const realFetch = globalThis.fetch;
interface Call {
  method: string;
  url: string;
  body: Record<string, unknown> | null;
}
let calls: Call[] = [];

const пределы = [
  { id: "lim-1", scope: "DEFAULT", scope_id: "", kind: "vpc.network", value: 5 },
  { id: "lim-2", scope: "PROJECT", scope_id: "prj-1", kind: "vpc.subnet", value: 20 },
];

function stub(failPatch = false) {
  calls = [];
  globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    calls.push({ method, url: requestUrl(input), body: requestBody(init?.body) });
    if (method === "PATCH" && failPatch) {
      return Promise.resolve({
        ok: false,
        status: 403,
        statusText: "Forbidden",
        text: () => Promise.resolve(JSON.stringify({ code: 7, message: "step-up required: acr too low" })),
      } as Response);
    }
    if (method === "PATCH") {
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        text: () => Promise.resolve(JSON.stringify({ operation: { id: "op-1", done: true } })),
      } as Response);
    }
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ limits: пределы })),
    } as Response);
  };
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/system/limits"]}>
        <LimitsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function openEditor(kind: string) {
  const cell = await screen.findByText(kind);
  const row = cell.closest("tr")!;
  fireEvent.click(within(row as HTMLElement).getByRole("button", { name: "Изменить" }));
}

describe("раздел администратора: пределы", () => {
  it("читает пределы с ВНУТРЕННЕГО слушателя, а не с публичной поверхности", async () => {
    // Публичного пути к величинам нет вовсе, и спросить его значило бы получать
    // отказ на каждом открытии страницы.
    stub();
    renderPage();
    await waitFor(() => expect(calls.length).toBeGreaterThan(0));
    expect(new URL(calls[0].url, "http://x").pathname).toBe(LIMITS_PATH);
    expect(LIMITS_PATH).toContain("/internal/");
  });

  it("меняет ТОЛЬКО величину и называет её маской", async () => {
    // Тройка «область · идентификатор · вид» есть идентичность предела: уехав в
    // тело без маски, она означала бы другой предел, а не правку этого.
    stub();
    renderPage();
    await openEditor("vpc.network");
    fireEvent.change(screen.getByLabelText("Величина предела"), { target: { value: "42" } });
    fireEvent.click(screen.getByText("Сохранить"));

    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true));
    const patch = calls.find((c) => c.method === "PATCH")!;
    expect(patch.url).toContain("/lim-1");
    expect(patch.body?.value).toBe(42);
    expect(String(patch.body?.updateMask ?? patch.body?.update_mask)).toBe("value");
    expect(patch.body).not.toHaveProperty("kind");
    expect(patch.body).not.toHaveProperty("scope");
  });

  it("ноль отправляется как величина, а не как «не задано»", async () => {
    // 0 означает «создавать нельзя»; выброшенный как пустота, он оставил бы
    // предел прежним, а администратор считал бы, что запретил создание.
    stub();
    renderPage();
    await openEditor("vpc.subnet");
    fireEvent.change(screen.getByLabelText("Величина предела"), { target: { value: "0" } });
    fireEvent.click(screen.getByText("Сохранить"));
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true));
    expect(calls.find((c) => c.method === "PATCH")!.body?.value).toBe(0);
  });

  it("отказ по давности входа не выдаётся за отказ в правах", async () => {
    // Иначе администратор пойдёт проверять права, которые у него есть, — и не
    // найдёт причины, потому что причина в другом.
    stub(true);
    renderPage();
    await openEditor("vpc.network");
    fireEvent.click(screen.getByText("Сохранить"));
    expect(await screen.findByText(/подтвердить личность заново/i)).toBeTruthy();
  });

  it("обычный отказ показывается отказом — положительный контроль", async () => {
    // Без него «узнаёт про подтверждение» могло бы означать «всё называет
    // подтверждением», и настоящая ошибка пряталась бы за ложным советом.
    calls = [];
    globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method === "PATCH") {
        return Promise.resolve({
          ok: false,
          status: 400,
          statusText: "Bad Request",
          text: () => Promise.resolve(JSON.stringify({ code: 3, message: "value must not be negative" })),
        } as Response);
      }
      void input;
      return Promise.resolve({
        ok: true,
        status: 200,
        statusText: "OK",
        text: () => Promise.resolve(JSON.stringify({ limits: пределы })),
      } as Response);
    };
    renderPage();
    await openEditor("vpc.network");
    fireEvent.click(screen.getByText("Сохранить"));
    expect(await screen.findByText(/Величина не изменена/i)).toBeTruthy();
    expect(screen.queryByText(/подтвердить личность заново/i)).toBeNull();
  });
});
