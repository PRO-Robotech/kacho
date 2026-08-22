// Панель статических маршрутов правит их целиком: сохранение ЗАМЕНЯЕТ весь
// список, поэтому в теле уезжают и те строки, которых оператор не касался.
// Предмет пробы — что показано в режиме чтения, что уходит на край при
// сохранении и что отмена действительно отменяет.
//
// Чистый круг «загрузили → сохранили» для ветви шлюза проверяет соседний
// RoutesPanel.routes.test.ts; здесь — наблюдаемая часть.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";

const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update, get: jest.fn(), list: jest.fn(), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { RoutesPanel } = await import("./RoutesPanel");
// Линия строки берётся из ОБЩЕГО источника геометрии, а не выписывается здесь:
// проба, повторившая литерал, разошлась бы с продуктом молча — ровно тем, ради
// чего числа редактора собраны в одном месте.
const { editorRowStyle, EDITOR_ROW_HEIGHT } = await import("@shared/components/organisms/form/editor-surface");

type Route = {
  destination_prefix?: string;
  next_hop_address?: string;
  gateway_id?: string;
  labels?: Record<string, string>;
};

function show(routes: Route[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RoutesPanel routeTableId="rt-1" projectId="prj-1" routes={routes} />
    </QueryClientProvider>,
  );
}

const startEdit = () => fireEvent.click(screen.getByRole("button", { name: /Редактировать/ }));
const save = () => fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
const cancel = () => fireEvent.click(screen.getByRole("button", { name: "Отменить" }));
const rowInputs = () => screen.getAllByRole("textbox");
/** Строки набора — сам набор, а не подпись над ним; в обоих режимах, читающем и
 *  правящем. Заведено на месте снятого счётчика: число строк осталось предметом
 *  проб, а место, где оно читалось, сменилось с заголовка секции на саму
 *  таблицу. */
const editRows = () => Array.from(screen.getByRole("table").querySelectorAll<HTMLElement>("tbody tr"));
/**
 * Объявленная линия строки, приведённая к той форме, в какой её хранит DOM.
 *
 * Сравнивать значение стиля с литералом нельзя: CSSOM нормализует запись, и
 * проба ловила бы нормализацию, а не расхождение. Эталон прогоняется через тот
 * же CSSOM — и берётся из ОБЩЕГО источника геометрии, а не выписывается здесь.
 */
function declaredRowLine(): string {
  const ref = document.createElement("div");
  ref.style.borderTop = String(editorRowStyle.borderTop);
  return ref.style.borderTop;
}
/**
 * «Линии сверху нет» читается как ПУСТОЕ объявление: `border-top: none` CSSOM не
 * хранит вовсе (проверено — свойство возвращает пустую строку, атрибут не
 * появляется). Поэтому одного этого признака мало: пустым он будет и у строки,
 * которую геометрия вообще не тронула. Рядом обязателен признак того, что
 * строка ОФОРМЛЕНА общей геометрией, — её высота.
 */
function expectStitchedWithoutLine(row: HTMLElement): void {
  expect(row.style.height).toBe(`${EDITOR_ROW_HEIGHT}px`);
  expect(row.style.borderTop).toBe("");
}
/** Поле, помеченное как незаполненное, — по признаку, который видит и читалка
 *  экрана, а не по цвету рамки. */
const invalidFields = () => screen.getAllByRole("textbox").filter((el) => el.getAttribute("aria-invalid") === "true");

beforeEach(() => {
  jest.clearAllMocks();
  update.mockResolvedValue({});
});

describe("RoutesPanel — режим чтения", () => {
  it("пустой список объясняет, как добавить маршрут, и таблицы не рисует", () => {
    show([]);

    expect(
      screen.getByText("Статических маршрутов нет — нажмите «Редактировать», чтобы добавить."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("показывает маршрут адресом следующего узла", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    expect(screen.getByText("10.0.0.0/8")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
  });

  it("шлюзовой маршрут показан шлюзом, а не пустой клеткой", () => {
    show([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]);

    expect(screen.getByText("gtw-1")).toBeInTheDocument();
  });

  // Здесь стояла проба «счётчик в подписи считает показанные маршруты». Её
  // предмета больше нет: решением владельца («отображать кол-во элементов не
  // нужно») число снято из заголовка секции — он теперь просто «Статические
  // маршруты». Проба снята ВМЕСТЕ с предметом, а не ослаблена до «подпись
  // есть»: утверждение о числе, которого продукт не печатает, зеленело бы
  // вечно. Сколько строк показано, сторожат пробы правки — они считают сами
  // строки, а не подпись над ними.
  it("показаны ВСЕ маршруты, а не первый из них", () => {
    // Положительный контроль на месте снятой пробы: она была единственной,
    // утверждавшей что-либо о списке ИЗ ДВУХ строк, и без неё «показывает
    // маршрут» зеленело бы на реализации, рисующей ровно одну.
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
    ]);

    const body = screen.getByRole("table").querySelector("tbody");
    expect(body?.querySelectorAll("tr")).toHaveLength(2);
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
    expect(screen.getByText("gtw-1")).toBeInTheDocument();
  });

  it("первая строка стыкуется с шапкой ОДНОЙ линией, у остальных линия своя", () => {
    // Решение владельца: линия РАЗДЕЛЯЕТ, а значит первой строке не нужна — над
    // ней уже стоит нижняя граница шапки секции. Две линии вплотную давали
    // тёмную полосу в две точки, и на глаз это читалось как щель между шапкой и
    // таблицей при нулевом зазоре.
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" },
    ]);

    const rows = editRows();
    expect(rows).toHaveLength(2);
    expectStitchedWithoutLine(rows[0]);
    // Положительный контроль: у второй строки линия ЕСТЬ и она — объявленная
    // общим источником. Без него «у первой нет линии» зеленело бы на таблице,
    // где линий нет ни у кого, то есть на снятом разделителе.
    expect(rows[1].style.borderTop).toBe(declaredRowLine());
  });

  it("до перехода в правку полей ввода нет", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    expect(screen.queryAllByRole("textbox")).toHaveLength(0);
  });
});

describe("RoutesPanel — правка", () => {
  it("правка открывает поля с текущими значениями", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();

    expect(rowInputs()[0]).toHaveValue("10.0.0.0/8");
    expect(rowInputs()[1]).toHaveValue("10.0.0.1");
  });

  // Здесь стояла проба «строка со шлюзом объясняет пустое поле адреса»: она
  // закрепляла ОБХОД. Ветвь шлюза нельзя было выбрать вовсе, поэтому её
  // существование объясняли подсказкой в поле ЧУЖОЙ ветви, а сменить её можно
  // было только набрав адрес поверх — обратной дороги не существовало (#375).
  it("строка со шлюзом открывается на СВОЕЙ ветви, а не полем адреса с подсказкой", () => {
    show([{ destination_prefix: "0.0.0.0/0", gateway_id: "gtw-1" }]);

    startEdit();

    // Выбор ветви назван и стоит на «Шлюз»; поля адреса у такой строки нет
    // вовсе — вместо него выбор шлюза.
    expect(screen.getByLabelText("Вид следующего узла")).toBeInTheDocument();
    expect(screen.getByText("Шлюз")).toBeInTheDocument();
    expect(rowInputs()).toHaveLength(1);
    expect(rowInputs()[0]).toHaveValue("0.0.0.0/0");
  });

  it("строка с адресом открывается полем адреса — положительный контроль", () => {
    // Без него «у строки со шлюзом нет поля адреса» могло бы означать «поля
    // адреса нет ни у кого».
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();

    expect(screen.getByText("Адрес")).toBeInTheDocument();
    expect(rowInputs()).toHaveLength(2);
    expect(rowInputs()[1]).toHaveValue("10.0.0.1");
  });

  it("правка пустого списка сразу даёт куда писать", () => {
    // Утверждение о счётчике «(1)» снято вместе со счётчиком (решение
    // владельца, см. выше). Число строк утверждается по самим строкам — так же
    // строго и без посредника в виде подписи.
    show([]);

    startEdit();

    expect(rowInputs()).toHaveLength(2);
    expect(editRows()).toHaveLength(1);
  });

  it("правка не меняет стык первой строки с шапкой", () => {
    // Вход в правку не двигает содержимое — это несущее свойство панели (одна и
    // та же таблица в обоих режимах). Стык первой строки его часть: появись у
    // неё линия при переходе, таблица поехала бы на точку вниз.
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" },
    ]);

    startEdit();

    const rows = editRows();
    expect(rows).toHaveLength(2);
    expectStitchedWithoutLine(rows[0]);
    expect(rows[1].style.borderTop).toBe(declaredRowLine());
  });

  it("добавленная строка появляется пустой", () => {
    // Прежний заголовок обещал ещё и «увеличивает счётчик» — счётчика нет,
    // поэтому обещание снято, а не оставлено ложным. Прирост набора при этом
    // утверждается точным числом строк, а не «стало больше».
    show([]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));

    expect(rowInputs()).toHaveLength(4);
    expect(editRows()).toHaveLength(2);
    expect(rowInputs()[2]).toHaveValue("");
  });

  it("сохранение уходит полной заменой списка и маской своего поля", async () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.change(rowInputs()[1], { target: { value: "10.0.0.2" } });
    save();

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
        static_routes: [{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.2" }],
        update_mask: "staticRoutes",
      }),
    );
  });

  // Здесь стояла проба «недописанная строка на край не уезжает». Она закрепляла
  // молчаливое удаление: сохранение ЗАМЕНЯЕТ весь список, поэтому «не уехала» и
  // «удалена» — одно и то же, а оператору при этом отвечали успехом.
  it("недописанная строка выключает «Сохранить» и названа под таблицей", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));

    expect(screen.getByRole("button", { name: "Сохранить" })).toBeDisabled();
    expect(screen.getByText("Строка 2: не указан префикс назначения и следующий узел")).toBeInTheDocument();
    expect(update).not.toHaveBeenCalled();
  });

  it("незаполненное поле помечено САМО, а не только названо в сводке", () => {
    // Решение владельца: место ошибки и место исправления совпадают. Прежде
    // нехватка называлась ТОЛЬКО строкой под таблицей — оператор пересчитывал
    // строки глазами, чтобы понять, куда смотреть.
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));

    // Помечены ровно ДВА поля — оба поля второй строки, и ни одно из первой.
    // Число здесь несущее: «есть хоть одно помеченное» зеленело бы на пометке
    // всей таблицы разом, то есть на указателе, который никуда не указывает.
    const marked = invalidFields();
    expect(marked).toHaveLength(2);
    const second = editRows()[1];
    for (const field of marked) expect(second).toContainElement(field);

    // Положительный контроль: заполненные поля первой строки НЕ помечены.
    // Без него отрицание выше зеленело бы на продукте, помечающем всё подряд.
    const firstRowFields = within(editRows()[0]).getAllByRole("textbox");
    expect(firstRowFields).toHaveLength(2);
    for (const field of firstRowFields) expect(field).not.toHaveAttribute("aria-invalid");

    // Сводка под таблицей осталась — она называет строку целиком; пометка на
    // поле её не заменяет, а показывает, КУДА смотреть.
    expect(screen.getByText("Строка 2: не указан префикс назначения и следующий узел")).toBeInTheDocument();
  });

  it("дописанное поле перестаёт быть помеченным", () => {
    // Вторая половина того же предмета: пометка ОБЯЗАНА сниматься. Без этой
    // пробы «помечено» зеленело бы на пометке, которая, раз появившись, стоит
    // до перезагрузки страницы, — и оператор чинил бы уже исправленное.
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));
    expect(invalidFields()).toHaveLength(2);

    fireEvent.change(rowInputs()[2], { target: { value: "192.168.0.0/16" } });
    expect(invalidFields()).toHaveLength(1);

    fireEvent.change(rowInputs()[3], { target: { value: "10.0.0.2" } });
    expect(invalidFields()).toHaveLength(0);
  });

  it("стёртый адрес существующего маршрута не удаляет его молча", () => {
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" },
    ]);

    startEdit();
    fireEvent.change(rowInputs()[1], { target: { value: "" } });

    expect(screen.getByRole("button", { name: "Сохранить" })).toBeDisabled();
    expect(screen.getByText("Строка 1: не указан следующий узел")).toBeInTheDocument();
  });

  it("дописанная строка снова включает «Сохранить» и уезжает целиком", async () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.click(screen.getByRole("button", { name: /Добавить маршрут/ }));
    fireEvent.change(rowInputs()[2], { target: { value: "192.168.0.0/16" } });
    fireEvent.change(rowInputs()[3], { target: { value: "10.0.0.2" } });

    expect(screen.getByRole("button", { name: "Сохранить" })).toBeEnabled();
    save();

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
        static_routes: [
          { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
          { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" },
        ],
        update_mask: "staticRoutes",
      }),
    );
  });

  it("правка одной строки не стирает метки соседней", async () => {
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2", labels: { env: "prod" } },
    ]);

    startEdit();
    fireEvent.change(rowInputs()[1], { target: { value: "10.0.0.9" } });
    save();

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
        static_routes: [
          { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.9" },
          { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2", labels: { env: "prod" } },
        ],
        update_mask: "staticRoutes",
      }),
    );
  });

  it("снятая строка в сохранение не попадает", async () => {
    show([
      { destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" },
      { destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" },
    ]);

    startEdit();
    fireEvent.click(within(screen.getByRole("table")).getAllByRole("button", { name: "Удалить маршрут" })[0]);
    save();

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("/vpc/v1/routeTables/rt-1", {
        static_routes: [{ destination_prefix: "192.168.0.0/16", next_hop_address: "10.0.0.2" }],
        update_mask: "staticRoutes",
      }),
    );
  });

  it("отмена возвращает показанное и на край ничего не шлёт", () => {
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    fireEvent.change(rowInputs()[1], { target: { value: "10.0.0.9" } });
    cancel();

    expect(update).not.toHaveBeenCalled();
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
    expect(screen.queryByText("10.0.0.9")).not.toBeInTheDocument();
  });

  it("отказ края показан текстом сервера, без кода протокола, правка не теряется", async () => {
    update.mockRejectedValue(new ApiError(400, 3, null, "destination_prefix is not a CIDR"));
    show([{ destination_prefix: "10.0.0.0/8", next_hop_address: "10.0.0.1" }]);

    startEdit();
    save();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("Статические маршруты: destination_prefix is not a CIDR"),
    );
    expect(screen.getByRole("button", { name: "Сохранить" })).toBeInTheDocument();
  });
});
