// Реестр доменных дополнений карточки ресурса. Он решает, что пользователь
// увидит СВЕРХ generic-обзора; ошибка здесь тихая в обе стороны — доменная
// строка исчезает без следа, а пустая ссылка показывается как рабочая. Поэтому
// проба рендерит то, что реестр вернул, и утверждает ВИДИМОЕ.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

jest.unstable_mockModule("@shared/api/client", () => ({
  // Порты возвращают промис по контракту, но ждать заменителю нечего —
  // `Promise.resolve` говорит это прямо, `async` без `await` обещало ожидание.
  api: {
    get: jest.fn(() => Promise.resolve({})),
    list: jest.fn(() => Promise.resolve({})),
    action: jest.fn(),
    post: jest.fn(),
  },
  ApiError,
}));

const { DETAIL_EXTENSIONS, detailExtension, registerDetailExtension } = await import("./ResourceDetailExtensions");
type Ctx = Parameters<NonNullable<(typeof DETAIL_EXTENSIONS)["networks"]["overviewExtra"]>>[0];

function ctx(data: Record<string, unknown>): Ctx {
  return { data, projectId: "prj-1", detailBase: "/d", navigate: jest.fn() };
}

function draw(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>{node}</QueryClientProvider>
    </MemoryRouter>,
  );
}

/** Дополнительные строки обзора, отрисованные как «подпись → значение». */
function drawOverviewExtra(specId: string, data: Record<string, unknown>): string[] {
  const items = detailExtension(specId)!.overviewExtra!(ctx(data));
  const view = draw(
    <dl>
      {items.map((it, i) => (
        <div key={i}>
          <dt>{it.label}</dt>
          <dd>{it.value}</dd>
        </div>
      ))}
    </dl>,
  );
  // Подпись строки — ReactNode (в неё кладут ⓘ-подсказку), поэтому возвращается
  // ПОКАЗАННЫЙ текст, а не сам узел: `String(<узел>)` дал бы «[object Object]»
  // для любой подписи, и сравнения ниже стали бы истинными при любой.
  return [...view.container.querySelectorAll("dt")].map((dt) => dt.textContent ?? "");
}

describe("detailExtension — разрешение дополнения", () => {
  it("у ресурса без дополнения его нет — и это не пустой объект", () => {
    expect(detailExtension("no-such-resource")).toBeUndefined();
  });

  it("встроенное дополнение находится по идентификатору ресурса", () => {
    expect(detailExtension("networks")).toBe(DETAIL_EXTENSIONS.networks);
  });

  it("зарегистрированное приложением перекрывает встроенное", () => {
    // Перекрывается «шлюз» — он не участвует в остальных случаях этого файла:
    // реестр глобален, и перекрытие «сети» испортило бы соседние утверждения.
    expect(DETAIL_EXTENSIONS.gateways).toBeDefined();
    const mine = { title: () => "своё" };

    registerDetailExtension("gateways", mine);

    expect(detailExtension("gateways")).toBe(mine);
    expect(detailExtension("gateways")).not.toBe(DETAIL_EXTENSIONS.gateways);
  });
});

describe("дополнения обзора показывают доменные строки", () => {
  it("сеть называет обе системные привязки", () => {
    const labels = drawOverviewExtra("networks", {
      id: "net-1",
      default_security_group_id: "sg-1",
      default_route_table_id: "rt-1",
    });

    expect(labels).toEqual(["Группа безопасности по умолчанию", "Таблица маршрутов по умолчанию"]);
    expect(screen.getByText("Группа безопасности по умолчанию")).toBeInTheDocument();
  });

  it("отсутствующая привязка показана прочерком, а не пустотой", () => {
    drawOverviewExtra("networks", { id: "net-1", default_security_group_id: "sg-1" });

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  // Зона и регион — ресурсы каталога geo, поэтому якорь размещения не просто
  // подписан, а ВЕДЁТ на свой ресурс. Прежде обе пробы утверждали присутствие
  // текста — и остались бы зелёными на плоском идентификаторе, из которого
  // пользователю некуда пойти. Подпись вида («ZONAL»/«REGIONAL») снята решением
  // владельца: её несёт тип ресурса, на который ведёт ссылка, и его глиф.
  it("зональная подсеть ведёт на карточку своей зоны", () => {
    drawOverviewExtra("subnets", { id: "sub-1", zone_id: "zone-a", placement_type: "ZONAL", network_id: "net-1" });

    expect(screen.getByRole("link", { name: /zone-a/ })).toHaveAttribute("href", "/system/zones/zone-a");
    expect(screen.queryByText("ZONAL")).not.toBeInTheDocument();
  });

  it("региональная подсеть ведёт на карточку своего региона, а не на пустую зону", () => {
    drawOverviewExtra("subnets", {
      id: "sub-1",
      region_id: "ru-central1",
      placement_type: "REGIONAL",
      network_id: "net-1",
    });

    expect(screen.getByRole("link", { name: /ru-central1/ })).toHaveAttribute(
      "href",
      "/system/regions/ru-central1",
    );
    expect(screen.queryByText("REGIONAL")).not.toBeInTheDocument();
  });

  it("подсеть без объявленного типа размещения, но с регионом, читается региональной", () => {
    // Наружу это видно по тому, на КАКОЙ ресурс каталога ведёт ссылка: зоны у
    // такой подсети нет, и якорь обязан указывать на регион, а не молчать.
    drawOverviewExtra("subnets", { id: "sub-1", region_id: "ru-central1", network_id: "net-1" });

    expect(screen.getByRole("link", { name: /ru-central1/ })).toHaveAttribute(
      "href",
      "/system/regions/ru-central1",
    );
  });

  it("таблица маршрутов называет свою сеть", () => {
    const labels = drawOverviewExtra("route-tables", { id: "rt-1", network_id: "net-1" });

    expect(labels).toEqual(["Сеть"]);
  });
});

describe("панели под обзором", () => {
  it("сеть получает управление своими CIDR-блоками", () => {
    draw(detailExtension("networks")!.overviewBelow!(ctx({ id: "net-1", ipv4_cidr_blocks: ["10.30.0.0/16"] })));

    // Вид — тот же, что у CIDR подсети: две секции семейств, заголовок
    // «CIDR» и бейдж IPv4/IPv6 отдельно. Слово то же, что у подсети (решение
    // владельца 2026-08-12): сеть и подсеть держат один предмет, и «супернет»
    // рядом с «CIDR» читались как два разных.
    expect(screen.getAllByText(/^IPv[46] CIDR/)).toHaveLength(2);
    expect(screen.getByText(new RegExp("^IPv4\\b"))).toBeInTheDocument();
    expect(screen.getByText("10.30.0.0/16")).toBeInTheDocument();
    expect(screen.getByText("CIDR-блоков нет")).toBeInTheDocument();
  });

  it("таблица маршрутов получает панель своих статических маршрутов", () => {
    draw(
      detailExtension("route-tables")!.overviewBelow!(
        ctx({ id: "rt-1", static_routes: [{ destination_prefix: "0.0.0.0/0", next_hop_address: "10.0.0.1" }] }),
      ),
    );

    expect(screen.getByText("0.0.0.0/0")).toBeInTheDocument();
  });
});
