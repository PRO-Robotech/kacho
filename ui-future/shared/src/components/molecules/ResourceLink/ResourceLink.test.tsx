// Ссылка на ресурс: копирование стоит РЯДОМ и копирует то, что показано целиком.
//
// Предмет (#446). Владелец: «все где используется иконка+имя+ссылка нужно
// добавить клик копи иконку для копирования значения». Компонент такой уже
// единственный, копирование в нём объявлено — не хватало двух вещей:
//
//   1. вызывающие его `RefNameLink` и `IamRefLink` копирование не включали, то
//      есть на карточке ресурса скопировать значение ссылки было нечем;
//   2. при отсутствии имени копировалось УСЕЧЁННОЕ значение — вместе с
//      многоточием. Скопированный идентификатор с «…» на конце не находится
//      нигде и не вставляется никуда: это тише, чем отсутствие копирования, —
//      заметит уже тот, кто вставил.
//
// Утверждается ИСХОД нажатия (что легло в буфер), а не наличие значка: значок
// присутствует и на сломанном значении.
//
// И отдельно закрепляется то, ради чего компонент вообще сводили в один: кнопка
// копирования стоит ВНЕ ссылки. Внутри — недопустимая вложенность и погашенный
// переход; это уже стоило дня отладки (правило 5 `ui.md`).
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { ResourceLink } from "./ResourceLink";

/** Буфер обмена jsdom не даёт вовсе — подставляем и читаем, что в него легло. */
let copied: string[] = [];

beforeEach(() => {
  copied = [];
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: (v: string) => (copied.push(v), Promise.resolve()) },
  });
});

function show(ui: React.ReactNode) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

/** Кнопка копирования — по подписи, а не по классу: подпись видит пользователь. */
const copyButton = () => screen.queryByRole("button", { name: /[Сс]копировать/ });

describe("ResourceLink — копирование рядом со ссылкой", () => {
  it("в буфер уходит ПОЛНОЕ имя, а не усечённое", async () => {
    const full = "очень-длинное-имя-сети-которое-не-влезает";
    show(<ResourceLink specId="networks" id="net-1" name={full} projectId="prj-1" maxChars={12} copy />);

    const btn = copyButton();
    // Нет кнопки — копировать значение ссылки нечем.
    expect(btn).not.toBeNull();
    fireEvent.click(btn as HTMLElement);

    // Усечение — свойство ПОКАЗА, а не значения.
    await waitFor(() => expect(copied).toEqual([full]));
  });

  it("без имени в буфер уходит полный идентификатор, а не его обрезка", async () => {
    const id = "net-0123456789abcdef";
    show(<ResourceLink specId="networks" id={id} projectId="prj-1" copy />);

    const btn = copyButton();
    expect(btn).not.toBeNull();
    fireEvent.click(btn as HTMLElement);

    await waitFor(() => expect(copied).toEqual([id]));
  });

  it("кнопка копирования стоит ВНЕ ссылки", () => {
    const { container } = show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const anchor = container.querySelector("a");
    // Ссылки нет вовсе — утверждение о вложенности было бы ни о чём.
    expect(anchor).not.toBeNull();
    // Кнопка внутри ссылки гасит клик: переход не происходит, а ссылка выглядит рабочей.
    expect(anchor?.querySelector("button")).toBeNull();
    expect(copyButton()).not.toBeNull();
  });

  it("без требования копирования кнопки нет (отрицательный контроль)", () => {
    show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" />);
    expect(copyButton()).toBeNull();
  });
});

/**
 * Копирование видно ВСЕГДА, а тише его делает ТОН.
 *
 * ЧТО ОТМЕНЕНО И ПОЧЕМУ. #480 прятал значок в покое (opacity-0) и раскрывал его
 * наведением на имя. Владелец правку отменил: наведения нет как события на
 * сенсорном вводе, поэтому там копирование было недоступно ВООБЩЕ, а глазами
 * страница переставала читаться как список возможностей — о действии знал
 * только тот, кто о нём уже знал. Сдержанность осталась, но её держит ТОН
 * (второстепенный цвет текста), а не отсутствие.
 *
 * ПОЧЕМУ ЭТО ВСЁ ЕЩЁ НЕ ПРОБА ПРО ЦВЕТ. Предмет — не значение тона, а два
 * свойства: значок ничем не погашен и его тон меняется от подхода к ИМЕНИ, а не
 * только к нему самому. `group-hover:` разворачивается в селектор ПОТОМКА
 * (`.group:hover .group-hover\:…`), поэтому тон меняет наведение на
 * `.group`-предка; предок, охватывающий имя, — это и есть «на подходе». Без
 * такого предка правило срабатывало бы на двенадцати пикселях самого значка,
 * то есть почти ни на чём.
 *
 * Вычисленный тон в покое и после наведения утверждает сквозная проба
 * `ui-future/e2e/specs/findings.spec.ts`: jsdom каскада не считает и о том, что
 * видит глаз, сказать не может.
 */
describe("ResourceLink — копирование видно всегда, тише его делает тон", () => {
  /** Значок внутри кнопки копирования — то, что глаз видит или не видит. */
  const copyIcon = (): Element | null => copyButton()?.querySelector("svg") ?? null;

  /** Базовые (безвариантные) классы прозрачности элемента. Вариантные
   *  (`group-hover:…`) сюда не попадают — они про другое состояние. */
  const baseOpacity = (el: Element): string[] =>
    (el.getAttribute("class") ?? "").split(/\s+/).filter((c) => /^opacity-\d+$/.test(c));

  const classes = (el: Element): string[] => (el.getAttribute("class") ?? "").split(/\s+/);

  /** Предки узла, несущие класс `group`: правило тона срабатывает от наведения
   *  на ЛЮБОГО из них — это селектор потомка, а не дочернего. */
  const toneAnchors = (node: Element): HTMLElement[] => {
    const out: HTMLElement[] = [];
    for (let n = node.parentElement; n; n = n.parentElement) {
      if (n.classList.contains("group")) out.push(n);
    }
    return out;
  };

  it("в покое значок ничем не погашен и наведением не раскрывается", () => {
    show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const icon = copyIcon();
    // Значка нет — утверждать не о чем, и «видно всегда» читалось бы как
    // «копирования нет вовсе».
    expect(icon).not.toBeNull();
    // Ни одного гасящего класса: значок стоит видимым при любом состоянии.
    expect(baseOpacity(icon as Element)).toEqual([]);
    // И его не раскрывают наведением — иначе с сенсорного ввода он недоступен.
    expect(classes(icon as Element).filter((c) => c.startsWith("group-hover:opacity"))).toEqual([]);
  });

  it("тише значок делает ТОН, и меняет его подход к ИМЕНИ", () => {
    const { container } = show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const icon = copyIcon();
    expect(icon).not.toBeNull();
    // В покое — тон второстепенного текста. Без него значок звучал бы наравне
    // с именем, к которому он приставлен.
    expect(classes(icon as Element)).toContain("text-[var(--kc-text-tertiary)]");
    // Тона не меняет ничто — значок остаётся одинаково тусклым и на подходе.
    expect(classes(icon as Element)).toContain("group-hover:text-[var(--kc-text)]");

    const anchor = container.querySelector("a");
    // Ссылки нет — «якорь охватывает имя» было бы утверждением ни о чём.
    expect(anchor).not.toBeNull();

    const enclosing = toneAnchors(icon as Element).filter((g) => g.contains(anchor as Element));
    // Ни один якорь не содержит имени: тон менялся бы только от наведения на
    // сам значок — на двенадцать пикселей.
    expect(enclosing.length).toBeGreaterThan(0);
  });

  it("тон касается ТОЛЬКО значка: имя показано в полную силу (положительный контроль)", () => {
    const { container } = show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const anchor = container.querySelector("a");
    expect(anchor).not.toBeNull();
    // Без этого контроля «значок приглушён» зеленело бы и на разметке, где
    // приглушено ВСЁ.
    expect(baseOpacity(anchor as Element)).toEqual([]);
    expect(classes(anchor as Element)).not.toContain("text-[var(--kc-text-tertiary)]");
    expect(anchor).toHaveTextContent("сеть");
  });
});

// Идентификатор, который И ЕСТЬ имя, показывается целиком (#716).
//
// # Класс
//
// Обрезка запасного идентификатора до 12 знаков написана против ДЛИННОГО
// МАШИННОГО id: у крокфордова `net01H2X…` хвост читателю ничего не говорит.
// У каталога размещения geo идентификатор назначает человек, и значение несёт
// как раз хвост — `ru-central1-a` обрезалось до `ru-central1-…`, то есть все
// шесть зон региона выглядели на экране ОДИНАКОВО и ссылка переставала
// называть, куда ведёт. Пока у зоны была подпись, запасной путь не включался
// вовсе, и промах был невидим; подпись сняли — и он включился.
//
// Каждое утверждение идёт с близнецом ДРУГОЙ конструкции, а не с копией:
// длинный машинный id (обрезается) против длинного слага (не обрезается).
describe("идентификатор как имя — показывается целиком", () => {
  it("слаг зоны не обрезается: хвост и есть то, что различает зоны", () => {
    show(<ResourceLink specId="zones" id="ru-central1-a" name="" />);
    expect(screen.getByRole("link")).toHaveTextContent("ru-central1-a");
  });

  it("близнец: машинный идентификатор по-прежнему обрезается", () => {
    // Другая конструкция, а не копия предыдущей: ресурс, у которого подпись
    // есть, но в этой строке её нет. Без этого подслучая правка читалась бы
    // как «обрезку сняли везде», и её предмет — сузить приём, а не убрать.
    show(<ResourceLink specId="networks" id="net01H2X3Y4Z5A6B7C8D9" name="" projectId="prj-1" />);
    expect(screen.getByRole("link")).toHaveTextContent("net01H2X3Y4Z…");
  });

  it("подпись, где она есть, остаётся сильнее идентификатора", () => {
    // Флаг не должен подменять имя там, где имя пришло.
    show(<ResourceLink specId="networks" id="net-1" name="боевая сеть" projectId="prj-1" />);
    expect(screen.getByRole("link")).toHaveTextContent("боевая сеть");
  });

  it("копируется то, что показано целиком", () => {
    // Смежный класс из шапки файла: усечённое значение в буфере не находится
    // нигде. Слаг обязан лечь в буфер без многоточия.
    show(<ResourceLink specId="zones" id="ru-central1-a" name="" copy />);
    const btn = copyButton();
    expect(btn).not.toBeNull();
    fireEvent.click(btn as Element);
    return waitFor(() => expect(copied).toEqual(["ru-central1-a"]));
  });
});

// Обрезка имени ссылки работает ТОЛЬКО как тройка свойств.
//
// Прежде здесь стояло одно `textOverflow: "ellipsis"` — правило, не делавшее
// ничего: переносить было куда (`white-space` по умолчанию разрешает перенос),
// поэтому длинное имя уезжало во вторую строку и поднимало строку списка над
// соседними, а многоточие не появлялось ни разу. Форма проверки была, содержания
// не было.
//
// Проба утверждает все три сразу и рядом — законный близнец: значок типа НЕ
// ужимается. Без него «minWidth: 0» на общем ряду сплющил бы иконку в полоску,
// то есть обрезка имени оплачивалась бы порчей соседа.
describe("ResourceLink — имя в одну строку", () => {
  const NAME = "боевая сеть периметра восточного региона";

  function nameNode(): HTMLElement {
    return screen.getByText(NAME);
  }

  it("имя не переносится, обрезается многоточием и умеет стать уже содержимого", () => {
    render(
      <MemoryRouter>
        <ResourceLink specId="networks" id="net-1" name={NAME} projectId="prj-1" />
      </MemoryRouter>,
    );
    const v = nameNode();

    expect(v.style.whiteSpace).toBe("nowrap");
    expect(v.style.textOverflow).toBe("ellipsis");
    expect(v.style.overflow).toBe("hidden");
    expect(parseFloat(v.style.minWidth)).toBe(0);
  });

  it("значок типа при этом не ужимается (близнец)", () => {
    const { container } = render(
      <MemoryRouter>
        <ResourceLink specId="networks" id="net-1" name={NAME} projectId="prj-1" icon />
      </MemoryRouter>,
    );

    const iconWrapper = container.querySelector<HTMLElement>("[style*='flex-shrink']");
    expect(iconWrapper).not.toBeNull();
    expect(iconWrapper!.style.flexShrink).toBe("0");
  });

  it("переход ссылки берёт длительность и кривую токенами продукта", () => {
    render(
      <MemoryRouter>
        <ResourceLink specId="networks" id="net-1" name={NAME} projectId="prj-1" />
      </MemoryRouter>,
    );
    const anchor = screen.getByRole("link");

    expect(anchor.style.transition).toContain("var(--kc-duration)");
    expect(anchor.style.transition).toContain("var(--kc-ease)");
    expect(anchor.getAttribute("class") ?? "").not.toContain("transition-colors");
  });
});
