// Действия ресурса в шапке карточки. Здесь ошибаются молча в обе стороны:
// показанная кнопка обещает операцию, которой API не даёт, а спрятанная делает
// действие недостижимым из консоли вовсе. Отдельный случай — группа
// безопасности по умолчанию: она неудаляема, и кнопка на ней — обещание отказа.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { REGISTRY, type ResourceSpec } from "@shared/lib/resource-registry";

// Порты клиента возвращают промис — заменитель обязан вернуть его же, иначе
// вызывающий не сможет его дождаться. Но ЖДАТЬ заменителю нечего: ответ известен
// сразу, и `Promise.resolve` говорит это прямо. Прежнее `async () => ({})`
// обещало ожидание, которого в теле нет.
jest.unstable_mockModule("@shared/api/client", () => ({
  api: {
    get: jest.fn(),
    list: jest.fn(() => Promise.resolve({})),
    delete: jest.fn(() => Promise.resolve({})),
  },
  ApiError,
}));

const { DetailOverviewActions } = await import("./DetailOverviewActions");

function Address() {
  return <div data-testid="address">{useLocation().pathname}</div>;
}

function show(specId: string, over: Partial<ResourceSpec> = {}, data: Record<string, unknown> = {}) {
  const spec: ResourceSpec = { ...REGISTRY[specId], ...over };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={["/here"]}>
      <QueryClientProvider client={client}>
        <Routes>
          <Route
            path="/*"
            element={
              <>
                <DetailOverviewActions
                  spec={spec}
                  data={{ id: "net-1", name: "frontend", ...data }}
                  projectId="prj-1"
                  detailBase="/projects/prj-1/vpc/networks/net-1"
                />
                <Address />
              </>
            }
          />
        </Routes>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const address = () => screen.getByTestId("address").textContent;
const edit = () => screen.queryByRole("button", { name: /Редактировать/ });
const remove = () => screen.queryByRole("button", { name: /Удалить/ });
const dialogShown = () => screen.queryByText(/Действие необратимо/) !== null;

describe("DetailOverviewActions", () => {
  it("показывает обе кнопки, когда ресурс несёт обе операции", () => {
    show("networks", { ops: { create: true, update: true, delete: true } });

    expect(edit()).toBeInTheDocument();
    expect(remove()).toBeInTheDocument();
  });

  it("не обещает правку, которой у ресурса нет", () => {
    show("networks", { ops: { create: true, update: false, delete: true } });

    expect(edit()).not.toBeInTheDocument();
    expect(remove()).toBeInTheDocument();
  });

  it("не обещает удаление, которого у ресурса нет", () => {
    show("networks", { ops: { create: true, update: true, delete: false } });

    expect(remove()).not.toBeInTheDocument();
  });

  it("правка уводит на форму этого же ресурса", () => {
    show("networks", { ops: { create: true, update: true, delete: true } });

    fireEvent.click(edit()!);

    expect(address()).toBe("/projects/prj-1/vpc/networks/net-1/edit");
  });

  it("до нажатия окно удаления закрыто", () => {
    show("networks", { ops: { create: true, update: true, delete: true } });

    expect(dialogShown()).toBe(false);
  });

  it("нажатие «Удалить» открывает окно подтверждения", () => {
    show("networks", { ops: { create: true, update: true, delete: true } });

    fireEvent.click(remove()!);

    expect(dialogShown()).toBe(true);
  });

  it("группа безопасности по умолчанию удаление не предлагает", () => {
    show("security-groups", { ops: { create: true, update: true, delete: true } }, { default_for_network: true });

    expect(remove()).not.toBeInTheDocument();
    expect(edit()).toBeInTheDocument();
  });

  it("обычная группа безопасности удаление предлагает", () => {
    show("security-groups", { ops: { create: true, update: true, delete: true } }, { default_for_network: false });

    expect(remove()).toBeInTheDocument();
  });

  it("доменные действия расширения показываются рядом со штатными", () => {
    const spec = { ...REGISTRY.networks, ops: { create: true, update: true, delete: true } } as ResourceSpec;
    const client = new QueryClient();
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <DetailOverviewActions
            spec={spec}
            data={{ id: "net-1", name: "frontend" }}
            projectId="prj-1"
            detailBase="/d"
            extActions={<button type="button">Перезапустить</button>}
          />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect(screen.getByRole("button", { name: "Перезапустить" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Редактировать/ })).toBeInTheDocument();
  });
});
