// Правила группы безопасности. Каждая операция уходит на край ОДНИМ вызовом
// правки набора, и форма тела здесь решающая: правка — это «снять по id и
// добавить заново», поэтому потерянный `deletion_rule_ids` молча ДВОИТ правило,
// а лишний — снимает чужое. Отдельный предмет — пустой набор: он означает
// «трафик заблокирован», и сказать это обязано само окно.
//
// `antd` переопределён локально: общий заменитель рисует `Dropdown` пустым
// узлом (пункты меню недостижимы) и не даёт добраться до подтверждения
// удаления — на нём проба зеленела бы при любом составе меню.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { antdStub } from "@shared/test/antd-stub";

interface MenuItem {
  key?: string;
  label?: React.ReactNode;
  type?: string;
  disabled?: boolean;
  onClick?: () => void;
}

interface ConfirmConfig {
  title?: React.ReactNode;
  content?: React.ReactNode;
  onOk?: () => unknown;
}

const confirms: ConfirmConfig[] = [];

jest.unstable_mockModule("antd", () => {
  const base = antdStub();
  // `antdStub()` объявлен как `Record<string, unknown>`, поэтому `base.Modal`
  // УЖЕ `unknown` — прежнее `as unknown as` содержало лишний первый шаг, ничего
  // не сообщавший компилятору. Второй шаг несущий: без него `Object.assign`
  // ниже не принимает `unknown` целью.
  const ModalRoot = base.Modal as React.FC<{ open?: boolean; children?: React.ReactNode }>;
  return {
    ...base,
    Modal: Object.assign(ModalRoot, {
      confirm: (cfg: ConfirmConfig) => {
        confirms.push(cfg);
      },
      destroyAll: () => {},
    }),
    Dropdown: ({ children, menu }: React.PropsWithChildren<{ menu?: { items?: MenuItem[] } }>) =>
      React.createElement(
        "span",
        null,
        children,
        (menu?.items ?? [])
          .filter((it) => it.type !== "divider")
          .map((it, i) =>
            React.createElement(
              "button",
              { key: it.key ?? i, type: "button", disabled: it.disabled, onClick: () => it.onClick?.() },
              it.label,
            ),
          ),
      ),
  };
});

// Действия панели живут в правом слоте шапки СТРАНИЦЫ — там же, где «Создать»
// у всех списков. Слот настоящий (не заменитель): на странице его даёт
// провайдер, и проба обязана давать его тоже, иначе она рендерит панель в
// условиях, которых на странице не бывает.

const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update, get: jest.fn(), list: jest.fn(), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { SgRulesPanel } = await import("./SgRulesPanel");
const { PageHeaderSlotProvider, HeaderRightSlot } = await import("@shared/components/molecules/PageHeaderSlot");
// Панель живёт ВНУТРИ карточки ресурса, и это условие рендера, а не декорация:
// по нему `FormShell` решает, рисовать ли собственную шапку. Без провайдера
// проба показывала форму в посадке, которой на странице не бывает, — с шапкой,
// которой на экране нет. Тот же довод, по которому здесь настоящий слот шапки.
const { DetailHeaderProvider } = await import("@shared/components/molecules/PanelHeader");
// Роутер здесь НЕ декорация: столбец «Источник» показывает ссылочные цели
// ссылками (канон консоли, правило 2), а `<Link>` без роутера роняет рендер
// целиком — то есть без него проба судила бы о панели, которой на странице нет.
const { MemoryRouter } = await import("react-router");
type Rule = Parameters<typeof SgRulesPanel>[0]["rules"][number];

const RULES: Rule[] = [
  {
    id: "sgr-1",
    direction: "INGRESS",
    protocol_name: "TCP",
    ports: { from_port: 80, to_port: 80 },
    cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
    description: "http",
  },
  {
    id: "sgr-2",
    direction: "EGRESS",
    protocol_number: 47,
    security_group_id: "sg-9",
  },
];

function show(rules: Rule[] = RULES) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/security-groups/sg-1"]}>
        <PageHeaderSlotProvider>
          <DetailHeaderProvider value={{ icon: <span aria-hidden /> }}>
            <div data-testid="header-slot">
              <HeaderRightSlot />
            </div>
            <SgRulesPanel sgId="sg-1" projectId="prj-1" rules={rules} networkId="net-1" />
          </DetailHeaderProvider>
        </PageHeaderSlotProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const rowOf = (text: string) => screen.getByText(text).closest("tr")!;
/** Действия панели живут в шапке; в строках есть одноимённый пункт меню. */
const headerAction = (name: RegExp | string) => within(screen.getByTestId("header-slot")).getByRole("button", { name });
const boxes = () => screen.getAllByRole("checkbox");
/**
 * Основное действие подвала формы — по КОНСТРУКЦИИ, а не по подписи.
 *
 * Подпись подвала теперь называет одно действие («Добавить», «Сохранить»):
 * предмет уже назван заголовком над формой (канон §8). Слово «Добавить» из-за
 * этого перестало быть однозначным — им же подписаны кнопки добавления блока в
 * наборах CIDR внутри самого правила, и `getByRole("button", { name:
 * "Добавить" })` выбрал бы одну из четырёх наугад.
 *
 * Подвал рисует ровно одну `DopplerButton` (`.doppler-btn`) — единственность
 * здесь утверждается, а не предполагается: перестанет она быть единственной —
 * упадёт этот помощник, а не проба, чей предмет совсем другой.
 */
function formSubmit(): HTMLElement {
  const found = screen.getAllByRole("button").filter((b) => b.classList.contains("doppler-btn"));
  expect(found).toHaveLength(1);
  return found[0];
}
/** Заголовок формы: он и называет действие, и доказывает, что форма открыта. */
const formHeading = (name: string) => screen.getByRole("heading", { name });

beforeEach(() => {
  jest.clearAllMocks();
  confirms.length = 0;
  update.mockResolvedValue({});
});

describe("SgRulesPanel — список", () => {
  it("пустой набор объясняет, что это запрет, а не «ничего не настроено»", () => {
    show([]);

    expect(screen.getByText("Правил нет — трафик блокируется (default-deny).")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("пустой набор предлагает первый шаг, а не только называет предмет", () => {
    show([]);

    // Действие стоит РЯДОМ с объяснением: вкладка, которая называет предмет и не
    // даёт с ним ничего сделать, отправляет читателя искать действие глазами по
    // всему экрану.
    const empty = screen.getByText("Правил нет — трафик блокируется (default-deny).").parentElement as HTMLElement;
    fireEvent.click(within(empty).getByRole("button", { name: "Добавить правило" }));

    // Здесь стояло `getByText("Создание")` — след прежней шапки формы, где
    // действие было заголовком, а предмет надзаголовком. Надзаголовки сняты
    // решением владельца, действие и предмет стоят одной строкой, и старое
    // утверждение искало узел, которого продукт больше не рисует нигде.
    expect(formHeading("Добавить правило")).toBeInTheDocument();
  });

  it("групповое действие не предлагается там, где выбирать нечего", () => {
    show([]);

    // Выключенный флажок и выключенное «Удалить» над пустой вкладкой обещают
    // действие над набором, которого не существует.
    const header = within(screen.getByTestId("header-slot"));
    expect(header.getByRole("button", { name: /Добавить правило/ })).toBeInTheDocument();
    expect(header.queryByRole("button", { name: /Удалить/ })).not.toBeInTheDocument();
    expect(header.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("направление показано словом, а не машинным значением", () => {
    show();

    expect(screen.getByText("Входящий")).toBeInTheDocument();
    expect(screen.getByText("Исходящий")).toBeInTheDocument();
    expect(screen.queryByText("INGRESS")).not.toBeInTheDocument();
  });

  it("«любой протокол» назван по-русски — одно значение не носит двух имён", () => {
    // Здесь стояло английское `Any` — единственное английское слово на русской
    // вкладке, и означало оно ровно то же, что «Любой» в поле «Протокол» самой
    // формы правила. Одно значение, названное на одном экране двумя языками,
    // читается как два разных значения.
    show([
      { id: "sgr-any", direction: "INGRESS", cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] }, description: "всё" },
      { id: "sgr-tcp", direction: "INGRESS", protocol_name: "TCP", description: "по имени" },
    ]);

    expect(within(rowOf("всё")).getByText("Любой")).toBeInTheDocument();
    expect(screen.queryByText("Any")).not.toBeInTheDocument();
    // Положительный контроль: названный протокол показан СВОИМ именем, а не
    // подменён «Любым». Без него отрицание зеленело бы на панели, печатающей
    // «Любой» в каждой строке.
    expect(within(rowOf("по имени")).getByText("TCP")).toBeInTheDocument();
  });

  it("протокол по номеру подписан номером, а не пустотой", () => {
    show();

    expect(screen.getByText("proto 47")).toBeInTheDocument();
  });

  it("правило без портов и без описания показывает прочерки, а не «undefined»", () => {
    show();

    const cells = [...rowOf("proto 47").querySelectorAll("td")].map((td) => td.textContent);
    expect(cells[3]).toBe("—");
    expect(cells[6]).toBe("—");
  });

  it("источник назван вместе с его типом", () => {
    show();

    expect(within(rowOf("http")).getByText("CIDR")).toBeInTheDocument();
    expect(within(rowOf("http")).getByText("0.0.0.0/0")).toBeInTheDocument();
    // Тип назван словом предмета, а не аббревиатурой; сама цель — ссылка на
    // группу, а не моноширинный идентификатор. Имя резолвится списком проекта,
    // и пока он не приехал, ссылка подписана усечённым идентификатором.
    expect(within(rowOf("proto 47")).getByText("Группа безопасности")).toBeInTheDocument();
    expect(within(rowOf("proto 47")).getByRole("link")).toHaveAttribute(
      "href",
      "/projects/prj-1/vpc/security-groups/sg-9",
    );
  });

  it("массовое удаление закрыто, пока ничего не выбрано", () => {
    show();

    expect(headerAction(/^Удалить$/)).toBeDisabled();
  });

  it("выбор считается в подписи кнопки", () => {
    show();

    fireEvent.click(boxes()[1]);

    expect(headerAction("Удалить (1)")).toBeEnabled();
  });

  it("выбор всех выбирает все правила разом", () => {
    show();

    fireEvent.click(boxes()[0]);

    expect(headerAction("Удалить (2)")).toBeInTheDocument();
  });
});

describe("SgRulesPanel — удаление", () => {
  it("удаление спрашивает подтверждение и до него на край не ходит", () => {
    show();

    fireEvent.click(boxes()[1]);
    fireEvent.click(headerAction("Удалить (1)"));

    expect(confirms).toHaveLength(1);
    expect(confirms[0].title).toBe("Удалить выбранные правила (1)");
    expect(update).not.toHaveBeenCalled();
  });

  it("подтверждённое массовое удаление снимает ровно выбранные id", async () => {
    show();

    fireEvent.click(boxes()[1]);
    fireEvent.click(headerAction("Удалить (1)"));
    await confirms[0].onOk!();

    expect(update).toHaveBeenCalledWith("/vpc/v1/securityGroups/sg-1/rules", { deletion_rule_ids: ["sgr-1"] });
  });

  it("удаление одного правила из его меню снимает только его", async () => {
    show();

    fireEvent.click(within(rowOf("proto 47")).getByRole("button", { name: "Удалить" }));
    expect(confirms[0].title).toBe("Удалить правило");
    await confirms[0].onOk!();

    expect(update).toHaveBeenCalledWith("/vpc/v1/securityGroups/sg-1/rules", { deletion_rule_ids: ["sgr-2"] });
  });

  it("подтверждение удаления называет правило ТЕМ ЖЕ видом, что и строка списка", () => {
    // Спрашивают у человека — значит и называют по-человечески. Прежде в запрос
    // подставлялось машинное значение контракта («INGRESS»), которого на экране
    // нет больше нигде: строка списка показывает направление словом.
    show();

    fireEvent.click(within(rowOf("http")).getByRole("button", { name: "Удалить" }));

    const { container, unmount } = render(<>{confirms[0].content}</>);
    expect(container.textContent).toContain("Входящий");
    expect(container.textContent).toContain("0.0.0.0/0");
    expect(container.textContent).not.toContain("INGRESS");
    unmount();
  });

  it("отказ края показан текстом сервера, без кода протокола", async () => {
    update.mockRejectedValue(new ApiError(400, 9, null, "rule is referenced"));
    show();

    fireEvent.click(within(rowOf("proto 47")).getByRole("button", { name: "Удалить" }));
    await confirms[0].onOk!();

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Правило группы безопасности: rule is referenced"));
  });
});

describe("SgRulesPanel — правка и добавление", () => {
  it("добавление открывает пустое правило, а не правку существующего", () => {
    show();

    fireEvent.click(headerAction(/Добавить правило/));

    // Форма правила называет ДЕЙСТВИЕ НАД ПРЕДМЕТОМ одной строкой — той же
    // конструкцией, что все формы консоли («Создать подсеть», «Изменить сеть»).
    // Прежде здесь утверждались два отдельных узла, «Создание» и «Правило»:
    // действие заголовком, предмет надзаголовком. Надзаголовки сняты решением
    // владельца — предмет ушёл в ту же строку, что действие.
    //
    // Утверждение действительно только потому, что панель отрисована В УСЛОВИЯХ
    // СТРАНИЦЫ (`DetailHeaderProvider`, см. `show`): внутри карточки ресурса
    // общая оболочка своей шапки не рисует, и эту показывает сама панель.
    expect(formHeading("Добавить правило")).toBeInTheDocument();
    // Подвал называет ДЕЙСТВИЕ, без повтора предмета: «Добавить», а не
    // «Добавить правило» — предмет уже назван заголовком над кнопкой (канон §8).
    // Утверждается дословно: `toHaveTextContent` прошло бы и на прежней подписи,
    // потому что она этой начинается.
    expect(formSubmit().textContent).toBe("Добавить");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("новое правило уходит одним добавлением, без снятия чужого", async () => {
    show();

    fireEvent.click(headerAction(/Добавить правило/));
    fireEvent.click(formSubmit());

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    const body = update.mock.calls[0][1] as Record<string, unknown>;
    expect(body).not.toHaveProperty("deletion_rule_ids");
    expect((body.addition_rule_specs as unknown[]).length).toBe(1);
  });

  it("правка существующего снимает ЕГО и добавляет заново — иначе правило раздвоится", async () => {
    show();

    fireEvent.click(within(rowOf("http")).getByRole("button", { name: "Редактировать" }));
    // Заголовок правки называет ТО ЖЕ действие тем же глаголом, каким его
    // называет любая другая форма консоли («Изменить …»). Прежде утверждалось
    // «Изменение» — отдельный узел снятого надзаголовка.
    expect(formHeading("Изменить правило")).toBeInTheDocument();
    expect(formSubmit().textContent).toBe("Сохранить");
    fireEvent.click(formSubmit());

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    const body = update.mock.calls[0][1] as Record<string, unknown>;
    expect(body.deletion_rule_ids).toEqual(["sgr-1"]);
    expect((body.addition_rule_specs as Record<string, unknown>[])[0]).not.toHaveProperty("id");
  });

  it("отмена правки возвращает список и на край ничего не шлёт", () => {
    show();

    fireEvent.click(headerAction(/Добавить правило/));
    fireEvent.click(screen.getByRole("button", { name: "Отменить" }));

    expect(update).not.toHaveBeenCalled();
    expect(screen.getByText("Входящий")).toBeInTheDocument();
  });
});
