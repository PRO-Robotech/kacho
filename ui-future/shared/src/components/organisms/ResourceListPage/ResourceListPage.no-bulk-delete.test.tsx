// Группового выделения и удаления по флажкам в списке НЕТ — канон консоли, §9.
//
// ПРЕДМЕТ. Возможность существовала: столбец флажков, счётчик «Выделено: N»,
// кнопка «Удалить выделенные» и окно подтверждения, называвшее число. Решением
// владельца «удаление по чекбоксам убрать для всех ресурсов» она снята целиком,
// и вместе с ней снята её проба — проба снятого предмета зеленела бы вечно,
// ничего не сторожа. Имя снятого файла здесь не пишется: его в дереве нет, и
// координата читалась бы как утверждение о живом.
//
// ЗАЧЕМ ТОГДА ЭТА. Снятая возможность отличается от несуществующей ровно тем,
// что её уже один раз сочли нужной: следующий читатель видит список без
// групповых действий, принимает это за упущение и заводит их заново — тем
// охотнее, что общая таблица групповое выделение УМЕЕТ (antd `rowSelection`).
// Отсутствие держится не памятью о решении, а этой парой утверждений.
//
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — наблюдаемое, а не разметка: флажок в СТРОКЕ таблицы
// и кнопка группового действия.
//
// ГРАНИЦА, названная честно. Возврат через СВОЙ столбец (`Column.cell`) доказан
// инъекцией: столбец с флажком даёт три флажка в теле таблицы, и эта строка
// краснеет. Возврат через `rowSelection` самой таблицы ловится, только пока его
// рисует общий дублёр antd; сегодня ту его ветку не исполняет НИ ОДНА проба —
// она осталась от снятой возможности. Снимут ветку дублёра как мёртвую — этот
// путь возврата перестанет ловиться, и тогда сюда нужна прямая проба таблицы.
//
// ОТРИЦАНИЕ ЗДЕСЬ НЕ ВЫРОЖДЕНО, и вот чем это закрыто — тремя контролями:
//   1. удаление у ресурса ЕСТЬ (`ops.delete`), иначе «группового удаления нет»
//      выполнялось бы по построению, а не решением;
//   2. удалять можно, и способ показан — меню действий строки на месте;
//   3. флажки в этом харнессе вообще находятся: настройка видимости столбцов —
//      законные флажки, и на странице они ЕСТЬ, а внутри таблицы их нет. Без
//      третьего контроля первое утверждение было бы выполнено и на харнессе,
//      который флажков не рисует НИКОГДА.

import { render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { REGISTRY, type ResourceSpec } from "@shared/lib/resource-registry";
import { ResourceListPage } from "./ResourceListPage";

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
  localStorage.clear();
});

function renderList(spec: ResourceSpec, at: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[at]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={spec} panelForms />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("список не предлагает группового выделения и удаления (канон §9)", () => {
  it("предпосылка: у ресурса удаление ЕСТЬ — значит его отсутствие в группе решено, а не выведено", () => {
    // Без этой строки всё утверждение ниже выполнялось бы и у справочника, где
    // удаления не существует вовсе, — то есть говорило бы не про свой предмет.
    expect(REGISTRY.networks.ops.delete).toBe(true);
  });

  it("флажков выделения в таблице нет, а построчное удаление на месте", async () => {
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [
      { id: "net-1", name: "первая" },
      { id: "net-2", name: "вторая" },
      { id: "net-3", name: "третья" },
    ]);
    renderList(spec, `/projects/p1/vpc/${spec.route}`);
    await screen.findAllByText("первая");

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: строки отрисованы и способ удалить показан —
    // меню действий строки. Иначе «флажков нет» было бы верно на пустом экране.
    expect(within(screen.getByRole("table")).getAllByText("первая").length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText("Действия").length).toBeGreaterThan(0);

    // ОТРИЦАНИЯ. Флажок ищется В ТАБЛИЦЕ, а не по всей странице: настройка
    // видимости колонок и отборы-переключатели — тоже флажки, и они законны.
    expect(within(screen.getByRole("table")).queryAllByRole("checkbox")).toEqual([]);
    expect(screen.queryByRole("button", { name: /Удалить выделенные/ })).toBeNull();
    expect(screen.queryByText(/Выделено:/)).toBeNull();
  });

  it("контроль механики: флажки на странице находятся — но это настройка столбцов, не строки", async () => {
    // Третий контроль. «В таблице флажков нет» обязано быть отличимо от «этот
    // харнесс флажков не рисует вовсе»: до того как общий дублёр antd научился
    // рисовать содержимое выпадающего блока, поиск флажка по всей странице был
    // однозначен СЛУЧАЙНО, и утверждение говорило не про свой предмет.
    //
    // Настройка видимости столбцов — законные флажки, и они есть на КАЖДОМ
    // списке. Значит пара «на странице есть, в таблице нет» опровержима с обеих
    // сторон: вернут выделение строк — покраснеет вторая половина; сломается
    // поиск флажков — первая.
    const spec = REGISTRY.networks;
    stubList(spec.payloadKey, [{ id: "net-1", name: "первая" }]);
    renderList(spec, `/projects/p1/vpc/${spec.route}`);
    await screen.findAllByText("первая");

    expect(screen.getAllByRole("checkbox").length).toBeGreaterThan(0);
    expect(within(screen.getByRole("table")).queryAllByRole("checkbox")).toEqual([]);
  });
});
