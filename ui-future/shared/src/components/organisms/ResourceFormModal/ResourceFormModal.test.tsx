// Модалка формы ресурса живёт в АДРЕСЕ: `?modal=<ресурс>-<действие>` открывает
// её, закрытие обязано убрать из адреса ровно эти два параметра и не тронуть
// контекст страницы. Ошибка здесь тихая: «закрыл» с потерянным `networkId`
// уводит родительскую страницу в другой контекст, а лишний query-параметр,
// проехавший в тело запроса, край выбрасывает молча.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ApiError } from "@shared/api/client";

const apiGet = jest.fn<(path: string) => Promise<Record<string, unknown>>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { get: apiGet },
  ApiError,
}));

// Диспетчер подменён узлом, который ПОКАЗЫВАЕТ полученную проводку: предмет
// пробы — что модалка ему передала и когда вообще открылась.
jest.unstable_mockModule("@shared/components/organisms/InlineResourceForm", () => ({
  InlineResourceForm: (p: Record<string, unknown>) =>
    React.createElement(
      "div",
      null,
      React.createElement(
        "span",
        null,
        `форма ${String((p.spec as { id?: string })?.id)}/${String(p.action)} preset=${JSON.stringify(p.presetFields ?? null)} networkId=${String(p.networkId ?? "")}`,
      ),
      React.createElement("button", { type: "button", onClick: p.onCancel as () => void }, "закрыть"),
    ),
}));

const { ResourceFormModal } = await import("./ResourceFormModal");

function Address() {
  const loc = useLocation();
  return <div data-testid="address">{loc.search}</div>;
}

function at(search: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={[`/p${search}`]}>
      <QueryClientProvider client={client}>
        <ResourceFormModal projectId="prj-1" />
        <Address />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const address = () => screen.getByTestId("address").textContent ?? "";

beforeEach(() => {
  jest.clearAllMocks();
});

describe("ResourceFormModal", () => {
  it("без запроса в адресе не показывает ничего", () => {
    at("");

    expect(screen.queryByText(/^форма /)).not.toBeInTheDocument();
  });

  it("адрес с неизвестным ресурсом окно не открывает", () => {
    at("?modal=no-such-resource-create");

    expect(screen.queryByText(/^форма /)).not.toBeInTheDocument();
  });

  it("создание открывает форму нужного ресурса", () => {
    at("?modal=networks-create");

    expect(screen.getByText(/^форма networks\/create/)).toBeInTheDocument();
  });

  it("контекст создания доезжает до формы и как preset, и как сеть", () => {
    at("?modal=subnets-create&networkId=net-7&network_id=net-7");

    expect(screen.getByText(/^форма subnets\/create preset=\{"network_id":"net-7"\} networkId=net-7$/)).toBeInTheDocument();
  });

  it("посторонний параметр адреса в тело формы не уезжает", () => {
    at("?modal=networks-create&utm_source=letter");

    expect(screen.getByText(/preset=\{\}/)).toBeInTheDocument();
  });

  it("правка ждёт загруженный ресурс и до него окна не показывает", async () => {
    let resolve: (v: Record<string, unknown>) => void = () => {};
    apiGet.mockReturnValue(new Promise((r) => (resolve = r)));

    at("?modal=networks-edit&id=net-1");

    expect(screen.queryByText(/^форма /)).not.toBeInTheDocument();

    resolve({ id: "net-1", name: "frontend" });

    expect(await screen.findByText(/^форма networks\/edit/)).toBeInTheDocument();
    expect(apiGet).toHaveBeenCalledWith("/vpc/v1/networks/net-1");
  });

  it("правка без id ресурс не запрашивает", () => {
    at("?modal=networks-edit");

    expect(apiGet).not.toHaveBeenCalled();
    expect(screen.queryByText(/^форма /)).not.toBeInTheDocument();
  });

  it("закрытие убирает из адреса запрос и id, а контекст страницы оставляет", async () => {
    at("?modal=subnets-create&networkId=net-7&id=sub-1");
    expect(address()).toContain("modal=subnets-create");

    screen.getByRole("button", { name: "закрыть" }).click();

    await waitFor(() => expect(address()).not.toContain("modal="));
    expect(address()).not.toContain("id=");
    expect(address()).toContain("networkId=net-7");
    expect(screen.queryByText(/^форма /)).not.toBeInTheDocument();
  });
});
