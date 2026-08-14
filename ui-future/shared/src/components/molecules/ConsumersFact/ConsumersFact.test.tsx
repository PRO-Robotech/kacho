// Потребители — обе половины и обе стороны потолка.
//
// Поле, у которого нет производителя, показывать нельзя (правило 9 канона
// консоли), но и производитель без честного вида ничего не чинит: усечённый
// список, поданный как полный, — та же неправда о ресурсе, только с другой
// стороны. Поэтому подпись про усечение проверяется В ОБЕ СТОРОНЫ: она есть,
// когда получено больше предела, и её НЕТ, когда список полон.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import { ConsumersFact } from "./ConsumersFact";

const realFetch = globalThis.fetch;

// Имя потребителя ссылка резолвит запросом к списку интерфейсов — отвечаем ей
// теми же именами, что кладём во вход, чтобы утверждения читались по имени.
function stubNicList(n: number) {
  const nics = Array.from({ length: n }, (_, i) => ({ id: `nic-${i}`, name: `nic-${i}` }));
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ network_interfaces: nics })),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function draw(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/security-groups/sgr-1"]}>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

function nics(n: number, from = 0) {
  return Array.from({ length: n }, (_, i) => ({
    referrer: { type: "network_interface", id: `nic-${i + from}`, name: `nic-${i + from}` },
  }));
}

describe("ConsumersFact", () => {
  it("без потребителей — прочерк, а не пустое место", () => {
    draw(<ConsumersFact usedBy={[]} projectId="prj-1" />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("не заданное поле читается так же, как пустое", () => {
    draw(<ConsumersFact usedBy={undefined} projectId="prj-1" />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("полный список показывается целиком и БЕЗ подписи про усечение", async () => {
    stubNicList(40);
    draw(<ConsumersFact usedBy={nics(3)} projectId="prj-1" limit={32} />);
    expect(await screen.findByText("nic-0")).toBeInTheDocument();
    expect(screen.getByText("nic-2")).toBeInTheDocument();
    expect(screen.queryByText(/Показаны первые/)).toBeNull();
  });

  it("ровно на пределе подписи ещё нет — иначе она врала бы на полном списке", async () => {
    stubNicList(40);
    draw(<ConsumersFact usedBy={nics(32)} projectId="prj-1" limit={32} />);
    expect(await screen.findByText("nic-31")).toBeInTheDocument();
    expect(screen.queryByText(/Показаны первые/)).toBeNull();
  });

  it("сверх предела — показаны первые limit и сказано, что потребителей больше", async () => {
    stubNicList(40);
    draw(<ConsumersFact usedBy={nics(33)} projectId="prj-1" limit={32} />);
    expect(await screen.findByText("nic-31")).toBeInTheDocument();
    expect(screen.queryByText("nic-32")).toBeNull();
    expect(screen.getByText("Показаны первые 32 — потребителей больше")).toBeInTheDocument();
  });

  it("без потолка список не усекается и подписи не появляется", async () => {
    stubNicList(40);
    draw(<ConsumersFact usedBy={nics(40)} projectId="prj-1" />);
    expect(await screen.findByText("nic-39")).toBeInTheDocument();
    expect(screen.queryByText(/Показаны первые/)).toBeNull();
  });
});
