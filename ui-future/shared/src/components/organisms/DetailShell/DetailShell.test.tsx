// Оболочка карточки ресурса: какой таб показан, чем он выбирается и что
// происходит с адресом страницы.
//
// Предмет — четыре свойства, каждое из которых ломается молча:
//
//  1. таб берётся из адреса (`?tab=`), а неизвестный идентификатор откатывается
//     к первому. Без отката оболочка показала бы пустую зону — «страница
//     сломалась» вместо «такого таба нет»;
//  2. выбор таба ПО УМОЛЧАНИЮ убирает `?tab=` из адреса, а не пишет его: иначе
//     каждый заход плодил бы две разные ссылки на одну и ту же страницу;
//  3. в управляемом режиме оболочка адрес НЕ трогает вовсе — навигацией
//     распоряжается вызывающий (у него на таб приходится свой путь);
//  4. `HeaderSlotPortal` вне оболочки не рисует НИЧЕГО. Это заявленная мягкая
//     деградация: связанные таблицы поднимают свой тулбар на строку имени и
//     обязаны переживать использование вне карточки.
//
// `Menu` общего стенда-заменителя рисует пункты по-настоящему, поэтому свой
// дублёр здесь больше не нужен — см. примечание у подмены модуля ниже.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

// Своего дублёра `Menu` здесь БОЛЬШЕ НЕТ (#588). Он рисовал пункты `<button>` с
// `aria-current="page"` — ни того, ни другого меню antd не производит, поэтому
// утверждения ниже были прибиты к форме дублёра и пережили бы продукт: переход
// на настоящий рендер оставил бы их зелёными на разметке, которой в консоли нет.
// Общий заменитель даёт форму настоящего — включая РОЛЬ, объявленную
// вызывающим: меню antd кладёт свою `role` до остатка props, поэтому рейл вправе
// объявить себя набором вкладок, и заменитель это доносит так же (см. примечание
// у `Menu` в `antd-stub.ts` и пробу его контракта в `antd-stub.test.tsx`).
jest.unstable_mockModule("antd", () => antdStub());

const { DetailShell, HeaderSlotPortal } = await import("./DetailShell");

const tabs = [
  { id: "overview", label: "Обзор", render: () => <div>содержимое обзора</div> },
  { id: "json", label: "JSON", render: () => <div>содержимое json</div> },
];

/** Печатает текущий адрес: свойство «что стало со ссылкой» иначе ненаблюдаемо. */
function Address() {
  const loc = useLocation();
  return <div data-testid="address">{loc.search || "(без параметров)"}</div>;
}

function show(initial: string, props: Record<string, unknown> = {}) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Address />
      <DetailShell resourceName="web" tabs={tabs} {...props} />
    </MemoryRouter>,
  );
}

/** Пункты рейла — ПО РОЛИ ВКЛАДКИ: рейл переключает вид в зоне 3, и объявлен он
 *  набором вкладок (см. describe «рейл объявлен НАБОРОМ ВКЛАДОК» ниже). */
const railButtons = () => screen.getAllByRole("tab");
/** Какая вкладка выбрана — по `aria-selected`, а не по классу подсветки: класс
 *  говорит о том, как пункт ВЫГЛЯДИТ, и о выборе не утверждает ничего. */
const selectedTab = () => railButtons().find((li) => li.getAttribute("aria-selected") === "true")?.textContent;
const address = () => screen.getByTestId("address").textContent;

describe("DetailShell — выбор таба", () => {
  it("без параметра показывает первый таб", () => {
    show("/networks/net-1");

    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
    expect(screen.queryByText("содержимое json")).not.toBeInTheDocument();
    expect(selectedTab()).toBe("Обзор");
  });

  it("таб берётся из адреса", () => {
    show("/networks/net-1?tab=json");

    expect(screen.getByText("содержимое json")).toBeInTheDocument();
    expect(screen.queryByText("содержимое обзора")).not.toBeInTheDocument();
    expect(selectedTab()).toBe("JSON");
  });

  it("неизвестный таб откатывается к первому, а не показывает пустоту", () => {
    show("/networks/net-1?tab=такого-нет");

    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
    expect(selectedTab()).toBe("Обзор");
  });

  it("выбор неосновного таба записывается в адрес", () => {
    show("/networks/net-1");

    fireEvent.click(railButtons()[1]);

    expect(address()).toBe("?tab=json");
    expect(screen.getByText("содержимое json")).toBeInTheDocument();
  });

  it("возврат к табу по умолчанию УБИРАЕТ параметр, а не пишет его", () => {
    // Иначе на одну страницу приходилось бы две разные ссылки.
    show("/networks/net-1?tab=json");

    fireEvent.click(railButtons()[0]);

    expect(address()).toBe("(без параметров)");
    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
  });

  it("в управляемом режиме адрес не трогается — навигирует вызывающий", () => {
    const onTabSelect = jest.fn();
    show("/networks/net-1", { activeTabId: "json", onTabSelect });

    expect(screen.getByText("содержимое json")).toBeInTheDocument();

    fireEvent.click(railButtons()[0]);

    expect(onTabSelect).toHaveBeenCalledWith("overview");
    expect(address()).toBe("(без параметров)");
    // Активный таб задаёт вызывающий: сама оболочка его не переключала.
    expect(screen.getByText("содержимое json")).toBeInTheDocument();
  });
});

describe("DetailShell — рейл объявлен НАБОРОМ ВКЛАДОК, а не меню команд", () => {
  // ПРЕДМЕТ (#627). Рейл переключает ВИД в зоне 3 — это вкладки, и оболочка так
  // их и называет во всём своём коде (`DetailTab`, «вертикальные табы»). Рисует
  // его меню antd, и пока роль не объявлена, наружу уходит `role="menu"` +
  // `role="menuitem"`: для того, кто читает страницу не глазами, набор видов
  // выглядит списком КОМАНД, а выбранный вид не помечен вовсе — выбранность
  // несёт класс подсветки, то есть сведение о цвете, а не о состоянии.
  //
  // Это не придирка к разметке: сквозная проба тегов (#627) искала на карточке
  // репозитория вкладку и не находила НИ ОДНОЙ — при том что вкладка строилась и
  // рисовалась. «Связь не доехала до оболочки» и «доехала, но объявлена не тем»
  // с той стороны неразличимы, а лечатся по-разному.
  //
  // Утверждается ФОРМА, доносимая наружу, а не внутренняя разметка меню: роль
  // набора, роль пункта, помеченность выбранного и связь вкладки с её панелью.
  it("рейл — набор вкладок, и каждый пункт объявлен вкладкой", () => {
    show("/networks/net-1");

    const рейл = screen.getByRole("tablist");
    // Направление объявлено ЯВНО, и объявлено ГОРИЗОНТАЛЬНЫМ: вкладки стоят
    // полосой под именем ресурса, и читающий страницу не глазами обязан ждать
    // стрелок влево-вправо, а не вверх-вниз. Прежде здесь стояло «вертикально» —
    // утверждение о вертикальном рейле, который занимал колонку, принадлежащую
    // навигации по модулю. Рейл снят решением владельца, а проба его пережила:
    // она перестала описывать продукт, продолжая падать на верном коде.
    // Утверждение не ослаблено — снятие атрибута по-прежнему роняет пробу.
    expect(рейл).toHaveAttribute("aria-orientation", "horizontal");
    expect(
      within(рейл)
        .getAllByRole("tab")
        .map((t) => t.textContent),
    ).toEqual(["Обзор", "JSON"]);
  });

  it("выбранная вкладка помечена ВЫБРАННОЙ, остальные — явно нет", () => {
    // Парный контроль внутри одного утверждения: «выбрана» без «не выбрана»
    // зеленело бы на оболочке, помечающей выбранными все вкладки сразу.
    show("/networks/net-1?tab=json");

    expect(screen.getAllByRole("tab").map((t) => [t.textContent, t.getAttribute("aria-selected")])).toEqual([
      ["Обзор", "false"],
      ["JSON", "true"],
    ]);
  });

  it("вкладка указывает на свою панель, а панель названа своей вкладкой", () => {
    show("/networks/net-1?tab=json");

    const вкладка = screen.getByRole("tab", { name: "JSON" });
    const панель = screen.getByRole("tabpanel");
    // Ссылка ведёт в существующий узел, а не в пустоту: висячая ссылка на панель
    // выглядит как связь и связью не является.
    expect(вкладка.getAttribute("aria-controls")).toBe(панель.getAttribute("id"));
    expect(панель.getAttribute("aria-labelledby")).toBe(вкладка.getAttribute("id"));
    expect(панель).toHaveTextContent("содержимое json");
  });

  it("в режиме формы НЕТ ни вкладок, ни панели вкладки", () => {
    // Контроль в обратную сторону к утверждению выше — и предмет у него теперь
    // ДРУГОЙ. Прежде проба требовала, чтобы вкладки оставались на месте, а у них
    // лишь пропадала ссылка на панель. Владелец снял вкладки со страницы формы
    // целиком («зачем-то табы»): вкладка переключает ВИД на ресурс, а форма не
    // вид — она его правит. Оставленные рядом, вкладки предлагали уйти с
    // недозаполненной формы, ничего не сказав о судьбе введённого, и при этом ни
    // одна из них не была выбрана — набор без выбранного пункта читается как
    // сломанный.
    //
    // Число названо ЯВНО (ноль вкладок, ноль наборов), а не заменено на «нет
    // ничего»: рядом стоит положительный контроль на той же оболочке и тех же
    // вкладках, поэтому «ноль» здесь означает «сняты формой», а не «их и не
    // было».
    show("/networks/net-1", { mainOverride: <div>форма правки</div> });

    expect(screen.queryAllByRole("tab")).toHaveLength(0);
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
    expect(screen.queryByRole("tabpanel")).not.toBeInTheDocument();
  });

  it("без формы те же вкладки на месте — положительный контроль к утверждению выше", () => {
    // Без него «вкладок ноль» зеленело бы и на оболочке, которая не рисует
    // вкладок ни при каких условиях.
    show("/networks/net-1");

    expect(screen.getAllByRole("tab")).toHaveLength(2);
    expect(screen.getByRole("tablist")).toBeInTheDocument();
    expect(screen.getByRole("tabpanel")).toBeInTheDocument();
  });
});

describe("DetailShell — содержимое главной зоны", () => {
  it("mainOverride заменяет содержимое активного таба и уносит вкладки", () => {
    // Предмет изменён решением владельца: прежде проба требовала, чтобы рейл
    // оставался «для контекста». Контекст даёт шапка — она называет ТИП и ИМЯ
    // ресурса, — а вкладки на странице правки предлагали уйти с формы.
    show("/networks/net-1", { mainOverride: <div>форма правки</div> });

    expect(screen.getByText("форма правки")).toBeInTheDocument();
    expect(screen.queryByText("содержимое обзора")).not.toBeInTheDocument();
    expect(screen.queryAllByRole("tab")).toHaveLength(0);
  });

  it("без mainOverride показано содержимое таба, а вкладки на месте", () => {
    // Парный положительный: без него утверждения выше были бы одинаково зелены
    // и у оболочки, которая не показывает НИЧЕГО.
    show("/networks/net-1");

    expect(screen.getByText("содержимое обзора")).toBeInTheDocument();
    expect(screen.queryByText("форма правки")).not.toBeInTheDocument();
    expect(railButtons()).toHaveLength(2);
  });

  it("ресурс без имени подписан явно, а не пустотой", () => {
    show("/networks/net-1", { resourceName: "" });

    expect(screen.getByText("(без имени)")).toBeInTheDocument();
  });
});

describe("HeaderSlotPortal", () => {
  it("внутри оболочки показывает содержимое слота", () => {
    render(
      <MemoryRouter initialEntries={["/networks/net-1"]}>
        <DetailShell
          resourceName="web"
          tabs={[
            {
              id: "overview",
              label: "Обзор",
              render: () => <HeaderSlotPortal>поиск по списку</HeaderSlotPortal>,
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("поиск по списку")).toBeInTheDocument();
  });

  it("вне оболочки не рисует ничего и не падает", () => {
    // Заявленная мягкая деградация: связанные таблицы переиспользуются вне
    // карточки, и отсутствие слота не должно ронять страницу.
    const { container } = render(<HeaderSlotPortal>поиск по списку</HeaderSlotPortal>);

    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByText("поиск по списку")).not.toBeInTheDocument();
  });
});
