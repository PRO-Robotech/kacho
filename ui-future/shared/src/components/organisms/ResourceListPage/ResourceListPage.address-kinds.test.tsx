import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY } from "@shared/lib/resource-registry";
import { ResourceListPage } from "./ResourceListPage";

// Раздел «IP-адреса» показывает ОБА вида — внутренние и публичные.
//
// Здесь стоял отбор по наличию внешнего адреса: строка без него отбрасывалась
// молча. Отбор при этом не считался сужением, поэтому список уходил не в
// «ничего не найдено», а в приветственное состояние — и консоль утверждала
// арендатору «адресов нет» ровно там, где край ответил «есть».
//
// Дефект тихий: у арендатора с обоими видами список просто короче настоящего, и
// по экрану этого не понять. Заметили его сквозные пробы, дважды.
//
// Три места дерева говорили обратное коду, и все три говорили одно: раздел
// назван «IP-адреса» (не «Публичные IP»); его пустое состояние обещает, что
// «IP-адрес можно зарезервировать в подсети (внутренний) или выделить публичный
// (внешний)»; а функция вида умеет печатать «Внутренний» — значение, которого на
// этой странице не бывало by construction.

const realFetch = globalThis.fetch;

function stubList(payloadKey: string, rows: Record<string, unknown>[]) {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ [payloadKey]: rows })),
    } as Response);
}

afterEach(() => {
  globalThis.fetch = realFetch;
});

function renderAddresses() {
  const spec = REGISTRY.addresses;
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/addresses"]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={spec} panelForms />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const internalRow = {
  id: "adr-internal",
  name: "внутренний",
  project_id: "prj-1",
  internal_ipv4_address: { address: "10.80.1.148", subnet_id: "sub-1" },
  used: false,
};

const externalRow = {
  id: "adr-external",
  name: "публичный",
  project_id: "prj-1",
  external_ipv4_address: { address: "203.0.113.7" },
  used: false,
};

describe("список IP-адресов показывает оба вида", () => {
  it("строка с одним внутренним адресом видна, а не заменена приветственным экраном", async () => {
    stubList(REGISTRY.addresses.payloadKey, [internalRow]);
    renderAddresses();

    expect(await screen.findByText("10.80.1.148")).toBeInTheDocument();
    // Приветственное состояние утверждает «адресов нет» — при непустом ответе
    // края это ложь, и именно её видел арендатор.
    expect(screen.queryByText("Зарезервируйте первый IP-адрес")).toBeNull();
  });

  // Положительный контроль к утверждению выше: без него проба зеленела бы и на
  // странице, не отрисовавшей ни одной строки вообще.
  it("строка с публичным адресом видна — контроль к предыдущему", async () => {
    stubList(REGISTRY.addresses.payloadKey, [externalRow]);
    renderAddresses();

    expect(await screen.findByText("203.0.113.7")).toBeInTheDocument();
    expect(screen.queryByText("Зарезервируйте первый IP-адрес")).toBeNull();
  });

  it("оба вида в одном ответе показаны обоими строками", async () => {
    stubList(REGISTRY.addresses.payloadKey, [internalRow, externalRow]);
    renderAddresses();

    expect(await screen.findByText("10.80.1.148")).toBeInTheDocument();
    expect(screen.getByText("203.0.113.7")).toBeInTheDocument();
  });

  // Фильтр зоны для внутренних адресов стал достижим ВПЕРВЫЕ: `rowZone` читает
  // их зону первой, но до снятия отбора такие строки до фильтра не доходили.
  it("внутренний адрес несёт зону — фильтр зоны читает её, а не пропускает строку", async () => {
    stubList(REGISTRY.addresses.payloadKey, [
      { ...internalRow, internal_ipv4_address: { address: "10.80.1.148", subnet_id: "sub-1", zone_id: "zone-a" } },
    ]);
    renderAddresses();

    expect(await screen.findByText("10.80.1.148")).toBeInTheDocument();
  });

  // Пустой ответ края — единственный случай, когда приветственное состояние
  // говорит правду. Без этой пробы починка могла бы снять его вовсе.
  it("на пустом ответе приветственное состояние остаётся", async () => {
    stubList(REGISTRY.addresses.payloadKey, []);
    renderAddresses();

    expect(await screen.findByText("Зарезервируйте первый IP-адрес")).toBeInTheDocument();
  });
});
