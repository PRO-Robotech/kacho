// Идентификатор — единственная внешне-адресуемая координата ресурса (core #15),
// и консоль показывает его ЦЕЛИКОМ именно затем, чтобы его можно было скопировать
// и вставить. Поэтому проверяется не «компонент отрисовался», а то, что в буфер
// уходит полный id, что клик не всплывает (кнопка живёт внутри кликабельной
// строки таблицы — всплытие открыло бы карточку вместо копирования) и что отказ
// буфера виден пользователю, а не проглатывается.

import { readFileSync } from "node:fs";
import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "@shared/lib/toast";
import { PropertyRowProvider } from "@shared/components/organisms/DetailShell/property-row-context";
import { CopyableId } from "./CopyableId";
import { MonoValue } from "./MonoValue";

/** Тело CSS-правила по селектору. jsdom тему не грузит, поэтому «переход задан
 *  токенами» проверяется по ОБЪЯВЛЕНИЮ: вычисленный стиль здесь пуст у любого
 *  узла и доказал бы ровно ничего. */
function ruleBlock(css: string, selector: string): string {
  const i = css.indexOf(selector + " {");
  if (i < 0) return "";
  const j = css.indexOf("}", i);
  return j < 0 ? "" : css.slice(i, j + 1);
}

function themeCss(): string {
  return readFileSync(
    new URL("../../../index.css", import.meta.url).pathname,
    "utf8",
  );
}

describe("CopyableId", () => {
  const writeText = jest.fn<(text: string) => Promise<void>>();

  // Шпионы ставятся на СПЯЩИЕ функции объекта `toast`, а не на оторванные от него
  // ссылки: метод без получателя — свойство чужого тела, на которое проба
  // опираться не должна.
  let successSpy: jest.Spied<typeof toast.success>;
  let errorSpy: jest.Spied<typeof toast.error>;

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    successSpy = jest.spyOn(toast, "success").mockReturnValue("toast-id");
    errorSpy = jest.spyOn(toast, "error").mockReturnValue("toast-id");
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it("показывает идентификатор целиком, без усечения", () => {
    render(<CopyableId id="net01h9zt6k3mqx4vabc" />);
    expect(screen.getByText("net01h9zt6k3mqx4vabc")).toBeInTheDocument();
  });

  it("на пустом идентификаторе рисует прочерк и не даёт кнопки", () => {
    render(<CopyableId id="" />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("кладёт в буфер полный идентификатор и не даёт клику всплыть", async () => {
    const onOuterClick = jest.fn();
    document.addEventListener("click", onOuterClick);
    try {
      render(<CopyableId id="net01h9zt6k3mqx4vabc" />);
      const button = screen.getByRole("button");
      expect(button).toHaveAttribute("title", "Скопировать ID");

      fireEvent.click(button);

      await waitFor(() =>
        expect(writeText).toHaveBeenCalledWith("net01h9zt6k3mqx4vabc"),
      );
      await waitFor(() =>
        expect(successSpy).toHaveBeenCalledWith("ID скопирован"),
      );
      // Кнопка живёт внутри кликабельной строки: всплывший клик открыл бы
      // карточку ресурса вместо копирования.
      expect(onOuterClick).not.toHaveBeenCalled();
      await waitFor(() =>
        expect(button).toHaveAttribute("title", "Скопировано"),
      );
    } finally {
      document.removeEventListener("click", onOuterClick);
    }
  });

  it("показывает отказ буфера, а не молчит", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));
    render(<CopyableId id="net01h9zt6k3mqx4vabc" />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() =>
      expect(errorSpy).toHaveBeenCalledWith("Не удалось скопировать"),
    );
    expect(successSpy).not.toHaveBeenCalled();
    // Отметки «скопировано» после отказа быть не должно — иначе пользователь
    // вставит пустоту, будучи уверен в обратном.
    expect(screen.getByRole("button")).toHaveAttribute(
      "title",
      "Скопировать ID",
    );
  });

  it("без иконки остаётся кликабельным", () => {
    render(<CopyableId id="net01h9zt6k3mqx4vabc" showIcon={false} />);
    const button = screen.getByRole("button");
    expect(button.querySelector("svg")).toBeNull();
    fireEvent.click(button);
    expect(writeText).toHaveBeenCalledWith("net01h9zt6k3mqx4vabc");
  });
});

// Идентификатор в ОДНУ строку — свойство ячейки, а не колонки.
//
// Наблюдалось на живом стенде: в списке таблиц маршрутов `rtb6qc1500147672jdm`
// разрывался посреди хвоста, последняя буква уезжала во вторую строку, и строка
// списка становилась выше соседних. Расширение колонки это не лечит: перенос
// разрешал сам атом (`break-all`), и при любой ширине нашлось бы значение длиннее.
//
// Проба утверждает ПАРУ, а не одно свойство: значение обрезается ПОКАЗОМ
// (nowrap + многоточие) и при этом НЕ усекается по существу — в разметке, в
// подсказке и в буфере остаётся полная строка. Без второй половины проба
// зеленела бы на «починке» через `id.slice(0, 12)`, то есть на потере данных.
describe("CopyableId — одна строка", () => {
  const ID = "rtb6qc1500147672jdmp";
  const writeText = jest.fn<(text: string) => Promise<void>>();

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    jest.spyOn(toast, "success").mockReturnValue("toast-id");
    jest.spyOn(toast, "error").mockReturnValue("toast-id");
  });

  afterEach(() => {
    jest.restoreAllMocks();
    writeText.mockReset();
  });

  /** Узел значения: тот, в котором лежит сам идентификатор. */
  function valueNode(): HTMLElement {
    return screen.getByText(ID);
  }

  it("значение не переносится и обрезается многоточием", () => {
    render(<CopyableId id={ID} />);
    const v = valueNode();

    expect(v.style.whiteSpace).toBe("nowrap");
    expect(v.style.textOverflow).toBe("ellipsis");
    expect(v.style.overflow).toBe("hidden");
    // Без этого flex-элемент не становится уже своего содержимого, и обрезка не
    // наступает НИКОГДА — правило выглядело бы исполненным и ничего не делало.
    // Отсутствие свойства даёт пустую строку → NaN, то есть проба падает и на нём.
    expect(parseFloat(v.style.minWidth)).toBe(0);
    expect(v.getAttribute("class") ?? "").not.toContain("break-all");
  });

  it("обрезка — только показ: полное значение остаётся в подсказке и в буфере", async () => {
    render(<CopyableId id={ID} />);

    expect(valueNode()).toHaveAttribute("title", ID);
    expect(valueNode().textContent).toBe(ID);

    fireEvent.click(screen.getByRole("button"));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(ID));
  });

  // ПРЕЖДЕ здесь утверждалось, что длительность и кривая перехода объявлены
  // ИНЛАЙНОМ. Владелец это место сменил: инлайн не переопределить снаружи без
  // `!important`, и строка свойств не могла подвинуть значок копирования к
  // общему правому краю — три значка стояли на разных вертикалях. Поэтому
  // отступ, радиус и переход уехали в класс темы.
  //
  // Предмет утверждения при этом ТОТ ЖЕ: движение по консоли — одно вещество,
  // числа приходят токенами, а не набором утилит. Изменилось место, где это
  // проверяется, и добавилось новое требование — инлайн о них не заявляет
  // ничего, иначе переопределение снова стало бы невозможным.
  it("кнопка не растягивает ячейку, а переход и отступы объявлены КЛАССОМ", () => {
    render(<CopyableId id={ID} />);
    const button = screen.getByRole("button");
    const classes = button.getAttribute("class") ?? "";

    expect(button.style.maxWidth).toBe("100%");
    expect(parseFloat(button.style.minWidth)).toBe(0);

    // Инлайн молчит — иначе место не переопределит.
    expect(button.style.transition).toBe("");
    expect(button.style.padding).toBe("");
    expect(button.style.borderRadius).toBe("");
    // ...но и не рассыпан по утилитам со своими числами.
    expect(classes).not.toContain("transition-colors");
    expect(classes).toContain("kc-copyable-id");

    const css = themeCss();
    // Контроль разбора в обе стороны: без него «правило содержит токен» было бы
    // верно и на разборе, возвращающем весь файл, и ложно — на сломанном.
    expect(ruleBlock(css, ".kc-selector-which-does-not-exist")).toBe("");
    const rule = ruleBlock(css, ".kc-copyable-id");
    expect(rule).not.toBe("");

    expect(rule).toMatch(/transition:/);
    expect(rule).toContain("var(--kc-duration)");
    expect(rule).toContain("var(--kc-ease)");
    expect(rule).toMatch(/padding:/);
    expect(rule).toMatch(/border-radius:/);
  });
});

// В СТРОКЕ СВОЙСТВ идентификатор — ТЕКСТ, а не кнопка (решение владельца).
//
// Различие принадлежит МЕСТУ: в строке свойств копирование общее — одна кнопка
// столбцом у правого края, — а в ячейке таблицы столбца действий нет, и там
// значение копирует себя само. Пока это выражалось пропом у каждого вызова,
// правило приходилось помнить: за один заход три значения приехали со своим
// значком, прилипшим к тексту. Теперь решает признак места
// (`property-row-context`), и новый компонент подчиняется строке, ничего о ней
// не зная.
//
// Проба держит ПАРУ, а не одну сторону: в строке — текст, вне строки —
// по-прежнему кнопка. Без второй половины утверждение зеленело бы на атоме,
// который перестал копировать вообще везде, то есть на противоположном дефекте.
describe("CopyableId — признак места", () => {
  const ID = "rtb6qc1500147672jdmp";
  const writeText = jest.fn<(text: string) => Promise<void>>();

  beforeEach(() => {
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    jest.spyOn(toast, "success").mockReturnValue("toast-id");
    jest.spyOn(toast, "error").mockReturnValue("toast-id");
  });

  afterEach(() => {
    jest.restoreAllMocks();
    writeText.mockReset();
  });

  function inPropertyRow(ui: React.ReactNode) {
    return render(<PropertyRowProvider value={true}>{ui}</PropertyRowProvider>);
  }

  it("в строке свойств значение не кнопка и на клик не отзывается", () => {
    inPropertyRow(<CopyableId id={ID} />);

    expect(screen.queryByRole("button")).toBeNull();

    const value = screen.getByText(ID);
    // Обрезка остаётся показом: полное значение по-прежнему в подсказке —
    // копировать его будет общая кнопка строки, и ей нужно ИСХОДНОЕ.
    expect(value).toHaveAttribute("title", ID);
    fireEvent.click(value);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("вне строки свойств — по-прежнему кнопка, копирующая по клику (парный положительный)", () => {
    render(<CopyableId id={ID} />);

    fireEvent.click(screen.getByRole("button"));
    expect(writeText).toHaveBeenCalledWith(ID);
  });

  it("рисует ровно то же, что MonoValue: у одного вида одна реализация", () => {
    // Канон §9: двух реализаций одного вида не бывает — форк отстаёт молча.
    // Утверждается РАЗМЕТКА целиком, а не «оба моноширинные»: разойдясь хоть
    // подсказкой, хоть тоном, они дадут в одной карточке два поведения.
    const { container: viaId } = inPropertyRow(
      <CopyableId id={ID} />,
    );
    const { container: viaValue } = inPropertyRow(
      <MonoValue value={ID} />,
    );

    expect(viaId.innerHTML).toBe(viaValue.innerHTML);
    // Контроль: сравниваются не две пустоты.
    expect(viaId.innerHTML).toContain(ID);
  });

  it("пустое значение и там, и там — прочерк без кнопки (близнец)", () => {
    const { container: viaId } = inPropertyRow(
      <CopyableId id="" />,
    );
    const { container: viaValue } = inPropertyRow(<MonoValue value="" />);

    expect(viaId.innerHTML).toBe(viaValue.innerHTML);
    expect(screen.getAllByText("—")).toHaveLength(2);
    expect(screen.queryByRole("button")).toBeNull();
  });
});
