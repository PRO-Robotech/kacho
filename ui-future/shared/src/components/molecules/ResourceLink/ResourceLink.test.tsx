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
 * Сдержанность копирования (#480): значок не виден в покое и раскрывается
 * наведением НА ИМЯ.
 *
 * ПОЧЕМУ ЭТО НЕ ПРОБА ПРО ЦВЕТ. Предмет здесь — не значение прозрачности, а
 * ЯКОРЬ правила появления. `group-hover:opacity-100` разворачивается в селектор
 * ПОТОМКА (`.group:hover .group-hover\:opacity-100`), поэтому раскрыть значок
 * может только наведение на предка с классом `group`. До #446 таким предком
 * была сама кнопка: текст имени лежал ВНУТРИ неё, и наведение на имя раскрывало
 * значок. #446 развела ссылку и кнопку (кнопка внутри `<a>` гасила переход) —
 * текст перестал быть потомком, якорь исчез вместе с ним, и значку назначили
 * постоянную видимость. Сдержанность потерялась не решением, а как побочный
 * эффект разведения.
 *
 * Отсюда два утверждения, и второе важнее первого: у значка обязан быть
 * `.group`-предок, СОДЕРЖАЩИЙ имя. Без него правило появления либо не
 * срабатывает вовсе — и значок невидим НАВСЕГДА, что тише нынешнего дефекта, —
 * либо срабатывает от наведения на сам значок, то есть на двенадцать пикселей,
 * которых в покое не видно.
 *
 * Наблюдаемое (вычисленная прозрачность в покое и после наведения) утверждает
 * сквозная проба `ui-future/e2e/specs/findings.spec.ts`: jsdom каскада не
 * считает и о том, что видит глаз, сказать не может.
 */
describe("ResourceLink — копирование сдержанно (#480)", () => {
  /** Значок внутри кнопки копирования — то, что глаз видит или не видит. */
  const copyIcon = (): Element | null => copyButton()?.querySelector("svg") ?? null;

  /** Базовые (безвариантные) классы прозрачности элемента. Вариантные
   *  (`group-hover:…`) сюда не попадают — они про другое состояние. */
  const базоваяПрозрачность = (el: Element): string[] =>
    (el.getAttribute("class") ?? "").split(/\s+/).filter((c) => /^opacity-\d+$/.test(c));

  const классы = (el: Element): string[] => (el.getAttribute("class") ?? "").split(/\s+/);

  /** Предки узла, несущие класс `group`: правило появления срабатывает от
   *  наведения на ЛЮБОГО из них — это селектор потомка, а не дочернего. */
  const якоряПоявления = (node: Element): HTMLElement[] => {
    const out: HTMLElement[] = [];
    for (let n = node.parentElement; n; n = n.parentElement) {
      if (n.classList.contains("group")) out.push(n);
    }
    return out;
  };

  it("в покое значок не виден вовсе, а не приглушён", () => {
    show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const icon = copyIcon();
    // Значка нет — утверждать не о чем, и «сдержанно» читалось бы как «нет копирования».
    expect(icon).not.toBeNull();
    // Ровно `opacity-0`: приглушённая постоянная видимость (`opacity-40`) — это
    // ровно та находка владельца, ради которой проба написана.
    expect(базоваяПрозрачность(icon as Element)).toEqual(["opacity-0"]);
  });

  it("правило появления привязано к области, которая содержит ИМЯ", () => {
    const { container } = show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const icon = copyIcon();
    expect(icon).not.toBeNull();
    // Значок ничем не раскрывается — тогда он невидим навсегда.
    expect(классы(icon as Element)).toContain("group-hover:opacity-100");

    const anchor = container.querySelector("a");
    // Ссылки нет — «якорь охватывает имя» было бы утверждением ни о чём.
    expect(anchor).not.toBeNull();

    const охватывающие = якоряПоявления(icon as Element).filter((g) => g.contains(anchor as Element));
    // Ни один якорь не содержит имени: раскрыть значок можно только наведением
    // на него самого — на двенадцать невидимых пикселей.
    expect(охватывающие.length).toBeGreaterThan(0);
  });

  it("сдержанность касается ТОЛЬКО значка: имя видно всегда (положительный контроль)", () => {
    const { container } = show(<ResourceLink specId="networks" id="net-1" name="сеть" projectId="prj-1" copy />);

    const anchor = container.querySelector("a");
    expect(anchor).not.toBeNull();
    // Без этого контроля «значок скрыт» зеленело бы и на разметке, где скрыто ВСЁ.
    expect(базоваяПрозрачность(anchor as Element)).toEqual([]);
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
