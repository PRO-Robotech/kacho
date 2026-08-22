// Вкладка JSON: ответ края копируется ЦЕЛИКОМ.
//
// ПРЕДМЕТ. Полотно доступно только на чтение и прокручивается: выделить его
// мышью целиком нельзя, а «скопировать видимое» отдало бы кусок. Ответ края —
// то, что уносят в обращение в поддержку, в задачу и в конфигурацию; без
// копирования вкладка показывает и не отдаёт.
//
// ЧТО УТВЕРЖДАЕТСЯ — наблюдаемое: кнопка стоит там, где вкладка держит СВОИ
// ручки (правый слот строки имени, тот же, что у связанных таблиц и операций),
// и в буфер уезжает вся сериализация, а не её начало и не адрес страницы.
// Отрицательный контроль — вкладка, которая ничего в слот не ставит: без него
// проба зеленела бы на оболочке, рисующей кнопку копирования всем вкладкам.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { DetailShell } = await import("./DetailShell");
const { JsonTab } = await import("./JsonTab");
// Полотно спрашивает тему приложения (в светлой оно светлое). Поставщик темы
// обязателен, иначе проба падает на СРЕДЕ, а не на предмете.
const { ThemeProvider } = await import("@shared/lib/theme-context");

const RESOURCE = { id: "net-1", name: "core", labels: { env: "prod" } };
const EXPECTED = JSON.stringify(RESOURCE, null, 2);

let copied: string[] = [];

beforeEach(() => {
  copied = [];
  Object.defineProperty(globalThis.navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: (t: string) => {
        copied.push(t);
        return Promise.resolve();
      },
    },
  });
});

function show(tab: React.ReactNode) {
  return render(
    <ThemeProvider>
      <MemoryRouter initialEntries={["/networks/net-1"]}>
        <DetailShell
          resourceName="core"
          tabs={[{ id: "json", label: "JSON", render: () => tab }]}
        />
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("вкладка JSON", () => {
  it("копирует ответ края целиком, а не показанную часть", async () => {
    show(<JsonTab data={RESOURCE} />);

    await userEvent.click(screen.getByRole("button", { name: /Скопировать/ }));

    expect(copied).toEqual([EXPECTED]);
  });

  it("сериализация — с отступами, как её рисует полотно", async () => {
    // Иначе в буфер уезжала бы строка в одну линию: то, что вставит читатель,
    // перестало бы совпадать с тем, что он видел на экране.
    show(<JsonTab data={RESOURCE} />);

    await userEvent.click(screen.getByRole("button", { name: /Скопировать/ }));

    expect(copied[0]).toContain('\n  "id": "net-1"');
  });

  it("вкладка без своих ручек кнопки копирования не даёт (парное отрицание)", () => {
    show(<div>обычное содержимое</div>);

    expect(screen.queryByRole("button", { name: /Скопировать/ })).toBeNull();
  });
});
