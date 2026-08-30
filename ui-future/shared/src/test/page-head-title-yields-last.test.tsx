// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ЗАГОЛОВОК СТРАНИЦЫ УСТУПАЕТ ШИРИНУ ПОСЛЕДНИМ (#1547).
//
// ПРЕДМЕТ. Шапка страницы — одна строка из двух соседей: колонка заголовка и
// слот ручек. Колонка заголовка объявлена сжимаемой ДО НУЛЯ (`minWidth: 0`
// снимает у flex-ребёнка неявный минимум по содержимому) — иначе длинное имя
// ресурса вылезает за свою зону и обрезка многоточием не работает вовсе. Раз
// так, всякий сосед, отказавшийся уступать, отдаёт колонке заголовка ВЕСЬ
// недостаток ширины: она сжимается до нуля, и предмет страницы пропадает с
// экрана при живой разметке.
//
// ЧТО НАБЛЮДАЛОСЬ. Слот ручек держал `flex: "0 0 auto"` — ширину по содержимому
// и никакого предела. На списке IP-адресов (окно 1280, рабочая область 932)
// ручки занимали 1068 точек, заголовку доставалось 0. Две сквозные пробы
// консоли падали по 60 секунд с «element(s) not found», хотя протокол падения
// говорит обратное: `60 × locator resolved to <h3 …>IP-адреса</h3> — unexpected
// value "hidden"`. Заголовок был в разметке всё это время; у него была нулевая
// коробка. Прочитанное как «страница не открылась» на деле было «страница
// открылась, а её предмет схлопнут».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ РЕНДЕР, А НЕ ЧТЕНИЕ ИСХОДНИКА (переписано на сведении волны)
//
// Первая редакция читала `PageHead.tsx` и `ResourceListPage.tsx` С ДИСКА и
// судила их текст. Это законная форма ровно для того, что пробе исполнить
// нечем (`.proto`, `.go`, объявление сборки), и незаконная здесь: модуль
// консоли проба ЗАГРУЗИТЬ может, а пока она этого не делает, она зелена, пока
// файл существует. Гейт ствола (`internal/repohygiene`,
// `TestUITestsDoNotReadTheirOwnSourceAsText`) назвал это верно.
//
// Рендер оказался не уступкой, а лучшим предикатом сразу по трём осям — и это
// замерено, а не заявлено:
//
//   1. РАЗБОР ЧУЖОЙ, А НЕ СВОЙ. Прежняя редакция сама угадывала формы записи
//      отказа регулярным выражением. CSSOM раскрывает сокращение сам:
//
//        flex: "0 0 auto" → flexShrink "0"   (ловили оба)
//        flex: "none"     → flexShrink "0"   (СЛЕПАЯ ЗОНА прежней редакции)
//        flexShrink: 0    → flexShrink "0"   (ловили оба)
//        flex: 0          → flexShrink "1"   (не отказ — и это ВЕРНО по спеке)
//
//      То есть требование «распознаватель обязан знать ВСЕ законные формы
//      записи предмета» здесь исполняется не полнотой моего перечня, а тем,
//      что перечень больше не мой.
//   2. СУДИТСЯ ЭЛЕМЕНТ, А НЕ ФАЙЛ. Стиль, уехавший в константу, вычисленный
//      или разложенный из переменной, останется под наблюдением; переезд и
//      переименование файла ничего не ломают. Прежняя редакция при таком
//      рефакторинге либо краснела впустую, либо зеленела молча.
//   3. ПРОВЕРЯЕТСЯ СОСТАВ, А НЕ ДВА ФАЙЛА ПОРОЗНЬ. Рендерится ТА САМАЯ
//      страница из происшествия — список IP-адресов, — поэтому цепочка
//      «список отдаёт ручки → шапка их размещает» судится собранной.
//
// ЧЕГО ГЕЙТ НЕ ЗАКРЫВАЕТ, И ЭТО СКАЗАНО ПРЯМО. Он судит ПОРЯДОК уступки, а не
// достаточность остатка: ряд ручек, выросший так, что заголовку остаётся две
// точки, объявлен верно, и гейт промолчит. Настоящую ширину в jsdom не
// измерить — там у всего нулевой размер, — поэтому порог держат сквозные
// пробы списка адресов в браузере, а здесь held то, что от порога не зависит:
// что недостаток ширины вообще делится.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import { HeaderRightSlot, PageHeaderSlotProvider } from "@shared/components/molecules/PageHeaderSlot";
import { ResourceListPage } from "@shared/components/organisms/ResourceListPage/ResourceListPage";
import { REGISTRY } from "@shared/lib/resource-registry";

const realFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = realFetch;
});

/** Одна строка списка. Ряд ручек существует только у НЕПУСТОГО списка: пустое
 *  состояние рисует шапку без правого слота, и проба на нём утверждала бы об
 *  отсутствующем — то есть зеленела бы by construction. */
const ROW = {
  id: "adr-1",
  name: "adr-one",
  project_id: "prj-1",
  external_ipv4_address: { address: "203.0.113.7" },
  used: false,
};

async function addressList(): Promise<HTMLElement> {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ addresses: [ROW] })),
    } as Response);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/addresses"]}>
        <PageHeaderSlotProvider>
          <HeaderRightSlot />
          <ResourceListPage spec={REGISTRY.addresses} panelForms />
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  await screen.findByText(ROW.name);
  return container;
}

/** Отказывается ли элемент отдавать ширину. Сокращение раскрывает CSSOM, а не
 *  этот предикат: незаданное значение (`""`) отказом НЕ является — по спеке
 *  умолчание `flex-shrink` равно единице. */
function refusesToShrink(el: HTMLElement): boolean {
  return el.style.flexShrink === "0";
}

/** Может ли элемент сжаться НИЖЕ своего содержимого. Без `min-width: 0` у
 *  flex-ребёнка остаётся неявный минимум по содержимому, и объявленная
 *  сжимаемость не срабатывает — та же дыра через вторую дверь. */
function collapsesBelowContent(el: HTMLElement): boolean {
  return parseFloat(el.style.minWidth) === 0;
}

describe("шапка страницы — заголовок уступает ширину последним", () => {
  it("перепись: шапка отрисована, оба соседа на месте, колонка заголовка сжимаема до нуля", async () => {
    // «Ноль нарушений» обязано быть отличимо от «ноль отрисованного»: снятый
    // заголовок, неотрисованный ряд ручек или переименованный класс иначе дали
    // бы пустой обход и зелёный вердикт.
    const container = await addressList();
    const heading = container.querySelector("h3");
    const titleColumn = heading?.closest("div[style]") as HTMLElement | null;
    const tools = Array.from(container.querySelectorAll<HTMLElement>(".kc-list-tools"));

    expect({ headingRendered: heading?.textContent }).toEqual({ headingRendered: "IP-адреса" });
    expect({ titleColumnFound: titleColumn !== null }).toEqual({ titleColumnFound: true });

    // Соседей ровно двое и они вложены: внешний — слот шапки, внутренний — ряд,
    // который список в этот слот отдаёт. Утверждается ПАРА, а не число: одно
    // число не отличило бы «ряда нет» от «слот не отрисован».
    expect({ toolsRendered: tools.length, rowInsideSlot: tools.length === 2 && tools[0].contains(tools[1]) }).toEqual({
      toolsRendered: 2,
      rowInsideSlot: true,
    });

    // Предпосылка запрета: класс существует ровно потому, что колонка заголовка
    // сжимаема до нуля. Перестанет быть — предмет исчезнет, и требование к
    // соседу придётся пересматривать, а не молча оставлять в силе.
    expect({ titleCollapsesToZero: collapsesBelowContent(titleColumn!) }).toEqual({ titleCollapsesToZero: true });
  });

  it("слот ручек в шапке ОТДАЁТ ширину: и объявленно, и ниже содержимого", async () => {
    const slot = (await addressList()).querySelectorAll<HTMLElement>(".kc-list-tools")[0];

    expect({ slotRefusesToShrink: refusesToShrink(slot) }).toEqual({ slotRefusesToShrink: false });
    expect({ slotCollapsesBelowContent: collapsesBelowContent(slot) }).toEqual({ slotCollapsesBelowContent: true });
  });

  it("недостаток ширины уходит в сужающие ручки, а не в подписи кнопок", async () => {
    // Решение принято вместе с починкой и записано там же: обрезанная подпись
    // действия («Зарезервир…») не говорит, что произойдёт по нажатию, тогда как
    // более узкое поле поиска остаётся собой. Перенос ряда в две строки отвергнут
    // отдельно — он занимает 72 точки против 44, объявленных высотой шапки, и
    // вылезал бы под крошки. Без этого утверждения обе половины решения
    // отзываются молча.
    const row = (await addressList()).querySelectorAll<HTMLElement>(".kc-list-tools")[1];
    const groups = Array.from(row.children).filter((c): c is HTMLElement => c.tagName === "DIV");

    expect({ rowWrapsToTwoLines: row.style.flexWrap === "wrap" }).toEqual({ rowWrapsToTwoLines: false });
    expect({ rowCollapsesBelowContent: collapsesBelowContent(row) }).toEqual({ rowCollapsesBelowContent: true });
    expect({ groupsInRow: groups.length }).toEqual({ groupsInRow: 2 });

    const [narrowing, actions] = groups;
    expect({ narrowingYields: collapsesBelowContent(narrowing) && !refusesToShrink(narrowing) }).toEqual({
      narrowingYields: true,
    });
    expect({ actionCaptionsKeepTheirWidth: refusesToShrink(actions) }).toEqual({ actionCaptionsKeepTheirWidth: true });
  });
});
