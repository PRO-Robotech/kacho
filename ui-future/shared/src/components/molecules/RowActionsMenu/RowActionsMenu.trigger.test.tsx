// Значок действий стоит у КАЖДОЙ строки и выглядит одинаково.
//
// Наблюдалось обратное: в списке таблиц маршрутов значок читался как «есть не у
// всех строк». Действия же одинаковы у всей таблицы — столбец заводится по
// СПЕКЕ ресурса, а не по строке, — поэтому «видно не у всех» может означать
// только одно: вид значка от чего-то зависит. Здесь это и проверяется, с обеих
// сторон: он есть при любом составе меню и его вид не зависит ни от строки, ни
// от ресурса.
//
// Предмет пробы — сам значок (то, чем меню ОТКРЫВАЕТСЯ), а не состав меню: его
// держат соседние файлы, и пересказывать их здесь значило бы завести два места
// об одном предмете.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { REGISTRY } = await import("@shared/lib/resource-registry");
const { RowActionsMenu, ROW_ACTION_TRIGGER } = await import("./RowActionsMenu");

function Harness({ children }: React.PropsWithChildren) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/route-tables"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

function trigger(specId: string, row: Record<string, unknown>): HTMLElement {
  const { unmount } = render(
    <Harness>
      <RowActionsMenu spec={REGISTRY[specId]} row={row} basePath="/projects/prj-1/vpc/route-tables" projectId="prj-1" />
    </Harness>,
  );
  const button = screen.getByRole("button", { name: "Действия" });
  const snapshot = button.cloneNode(true) as HTMLElement;
  unmount();
  return snapshot;
}

// Восемь ресурсов VPC — те же, что перечисляет `VPC_SCOPED_IDS` у модуля.
// Перечислены поимённо намеренно: правка общего значка обязана доходить до
// всех, и «дошла до сетей» — недоделанная работа.
const VPC = [
  "networks",
  "subnets",
  "addresses",
  "route-tables",
  "security-groups",
  "network-interfaces",
  "gateways",
  "cidr-groups",
];

describe("значок действий строки", () => {
  it("есть у строки каждого ресурса VPC", () => {
    for (const specId of VPC) {
      expect(trigger(specId, { id: `${specId}-1`, name: "строка" })).toBeTruthy();
    }
  });

  it("выглядит одинаково при РАЗНОМ составе меню", () => {
    // Группа по умолчанию не удаляется, у сети в меню есть «Создать подсеть», у
    // подсети — ни того, ни другого: три разных меню, один и тот же значок.
    // Идентификаторы РАЗНЫЕ намеренно: одинаковые сделали бы утверждение
    // нечувствительным к виду, зависящему от строки (проверено инъекцией —
    // на одинаковых хвостах она зеленела).
    const defaultSg = trigger("security-groups", { id: "sg-1", name: "default", default_for_network: true });
    const network = trigger("networks", { id: "net-2", name: "core" });
    const subnet = trigger("subnets", { id: "sub-3", name: "front" });

    expect(network.getAttribute("style")).toBe(defaultSg.getAttribute("style"));
    expect(subnet.getAttribute("style")).toBe(defaultSg.getAttribute("style"));
  });

  it("виден без наведения: тон из палитры, прозрачности нет", () => {
    // «Появляется по наведению» — это и есть «видно не у всех строк»: наведение
    // указывает на одну строку, а на сенсорном экране его нет вовсе.
    expect(ROW_ACTION_TRIGGER.opacity).toBeUndefined();
    expect(ROW_ACTION_TRIGGER.visibility).toBeUndefined();
    expect(String(ROW_ACTION_TRIGGER.color)).toMatch(/^var\(--kc-/);
  });

  it("занимает размер ячейки, а не размер элемента управления", () => {
    // 36 — общая высота кнопок консоли; в строке списка она подняла бы КАЖДУЮ
    // строку ради столбца, в котором нет данных.
    expect(ROW_ACTION_TRIGGER.width).toBe(30);
    expect(ROW_ACTION_TRIGGER.height).toBe(30);
  });
});
