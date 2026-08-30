// Одиночное действие строки НЕ прячется за кебабом (#687).
//
// Предмет — ФОРМА столбца действий, а не состав меню (его держит соседний
// файл): ресурс, у которого действие ровно одно, показывает его кнопкой с
// подписью, и нажатие остаётся одно вместо двух.
//
// Счёт ведётся по НАСТОЯЩИМ действиям, и два пункта в него не входят:
//   · «Просмотр»/«Открыть» повторяет ссылку в колонке идентичности — столбец
//     ради него был бы столбцом без содержания (та же граница, что у
//     `resourceHasRowActions`);
//   · «Переместить» — окно-заглушка, печатающее REST-вызов, которого консоль не
//     делает. Продвинуть её в главное действие строки значит выдать заглушку за
//     возможность, и ровно этим было опасно прежнее правило, снятое при
//     сведении копий (#686): из девяти его срабатываний восемь приходились на
//     заглушку.
//
// Решение принимается по СПЕКЕ, а не по строке, и это не мелочь: столбец
// заводится спекой на всю таблицу, а «у одной строки кнопка, у соседней
// значок» читается как «действие есть не у всех» — тот самый дефект, против
// которого написан `RowActionsMenu.trigger.test.tsx`. Поэтому у группы
// безопасности по умолчанию (её строка теряет удаление) значок остаётся.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { REGISTRY } = await import("@shared/lib/resource-registry");
const { RowActionsMenu, resourceHasRowActions, specRowActionCount } = await import("./RowActionsMenu");

function Harness({ children }: React.PropsWithChildren) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/subnets"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

function renderRow(specId: string, row: Record<string, unknown>) {
  return render(
    <Harness>
      <RowActionsMenu spec={REGISTRY[specId]} row={row} basePath="/projects/prj-1/x" projectId="prj-1" />
    </Harness>,
  );
}

/** Значок «⋮» — то, чем меню открывается. Его отсутствие и есть «одно нажатие». */
function kebab(): HTMLElement | null {
  return screen.queryByRole("button", { name: "Действия" });
}

describe("одно настоящее действие — кнопка, а не меню", () => {
  it("у тега единственное действие стоит подписанной кнопкой, значка нет", () => {
    // Теги пишет `docker push`; в консоли у них одна мутация — удаление.
    renderRow("tags", { id: "tag-1", tag: "v1", name: "v1" });

    expect(screen.getByRole("button", { name: "Удалить" })).toBeTruthy();
    expect(kebab()).toBeNull();
  });

  it("заглушка перемещения главным действием строки не становится", () => {
    // Отрицание с предметом: у каталога типов машин настоящих действий нет
    // вовсе, а «Переместить» ему предлагается. Продвинуть её инлайн значило бы
    // обещать операцию, которой нет.
    renderRow("machine-types", { id: "mt-1", name: "s1.medium" });

    expect(screen.queryByRole("button", { name: "Переместить" })).toBeNull();
  });

  it("у ресурса с двумя действиями значок остаётся", () => {
    // Положительный контроль к обоим отрицаниям: без него «кнопки нет» было бы
    // истинно и на сплошь неотрисованном столбце.
    renderRow("subnets", { id: "sub-1", name: "frontend" });

    expect(kebab()).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Редактировать" })).toBeNull();
  });

  it("строка, потерявшая одно из двух действий, форму столбца не меняет", () => {
    // Группа по умолчанию не удаляется, то есть на ЭТОЙ строке действие одно.
    // Форму решает спека: иначе в одной таблице соседние строки выглядели бы
    // по-разному, а это и есть «действие есть не у всех».
    renderRow("security-groups", { id: "sg-1", name: "default", default_for_network: true });

    expect(kebab()).toBeTruthy();
  });
});

describe("счёт действий — один источник", () => {
  it("«нужен ли столбец» выводится из того же счёта", () => {
    // Два предиката об одном предмете разошлись бы молча; здесь второй
    // ВЫРАЖЕН через первый, и проба это фиксирует по всему реестру.
    const ids = Object.keys(REGISTRY);
    expect(ids.length).toBeGreaterThan(0);
    for (const id of ids) {
      expect(resourceHasRowActions(REGISTRY[id])).toBe(specRowActionCount(REGISTRY[id]) > 0);
    }
  });

  it("перепись реестра: спек, из них с единственным действием", () => {
    const ids = Object.keys(REGISTRY);
    const single = ids.filter((id) => specRowActionCount(REGISTRY[id]) === 1);
    const none = ids.filter((id) => specRowActionCount(REGISTRY[id]) === 0);
    // Объём осмотренного печатается всегда: «ноль находок» обязано быть
    // отличимо от «ноль прочитанного».
    process.stdout.write(
      `\n[столбец действий] спек ${ids.length} · с одним действием ${single.length} (${single.join(", ") || "—"})` +
        ` · без действий ${none.length} (${none.join(", ") || "—"})\n`,
    );
    expect(ids.length).toBeGreaterThan(20);
    // Ресурс с единственным действием в реестре ЕСТЬ — иначе положительный
    // случай выше проверял бы форму, которой не бывает.
    expect(single).toContain("tags");
  });
});
