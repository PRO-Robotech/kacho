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
