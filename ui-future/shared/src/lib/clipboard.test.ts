import { jest } from "@jest/globals";
import { copyText } from "./clipboard";

// Копирование обязано работать ВНЕ защищённого контекста — иначе оно не работает
// на половине посадок консоли.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТА ПРОБА СТЕРЕЖЁТ
//
// `navigator.clipboard` объявлен доступным только при `https://` либо на
// `localhost`/`127.0.0.1`. По обычному `http://` на имени хоста его НЕТ ВОВСЕ:
// не «он отказывает», а само свойство равно `undefined`, и обращение к его
// методу роняет обработчик события до всякого `catch`.
//
// Так поднята консоль в конвейере (`http://console.kacho.local:<порт>`) и так же
// её открывают на внутреннем стенде. До сведения к этому помощнику копирование
// было объявлено в ДЕСЯТИ местах прямым обращением — и не работало ни в одном,
// причём молча: `TypeError` в обработчике никуда не всплывает.
//
// Дороже всего это стоило бы у одноразового секрета: он показывается один раз, и
// не сработавшая кнопка означает потерянный доступ, а не неудобство.

const реальныйClipboard = Object.getOwnPropertyDescriptor(globalThis.navigator ?? {}, "clipboard");

/** Задать `document.execCommand` и вернуть счётчик вызовов.
 *
 *  Задаётся, а не подменяется наблюдателем: в jsdom этого метода НЕТ вовсе, и
 *  `spyOn` отказывается — «Property `execCommand` does not exist». Само по себе
 *  это и есть предмет: помощник обязан пережить среду, где метода нет. */
function задатьExecCommand(исход: boolean | (() => boolean)) {
  const вызовы: string[] = [];
  Object.defineProperty(document, "execCommand", {
    configurable: true,
    writable: true,
    value: (команда: string) => {
      вызовы.push(команда);
      return typeof исход === "function" ? исход() : исход;
    },
  });
  return вызовы;
}

function подменитьClipboard(значение: unknown) {
  Object.defineProperty(globalThis.navigator, "clipboard", {
    configurable: true,
    value: значение,
  });
}

afterEach(() => {
  if (реальныйClipboard) Object.defineProperty(globalThis.navigator, "clipboard", реальныйClipboard);
  jest.restoreAllMocks();
});

describe("copyText", () => {
  it("в защищённом контексте кладёт значение современным путём", async () => {
    const writeText = jest.fn<(t: string) => Promise<void>>().mockResolvedValue(undefined);
    подменитьClipboard({ writeText });

    await expect(copyText("env=prod")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("env=prod");
  });

  it("БЕЗ защищённого контекста копирует запасным путём, а не падает", async () => {
    // Ровно та среда, где прежний код ронял обработчик: свойства нет вовсе.
    подменитьClipboard(undefined);
    const вызовы = задатьExecCommand(true);

    await expect(copyText("env=prod")).resolves.toBe(true);
    expect(вызовы).toEqual(["copy"]);
  });

  it("значение доходит до запасного пути ЦЕЛИКОМ — иначе в буфер ляжет не то", async () => {
    подменитьClipboard(undefined);
    let вПоле = "";
    задатьExecCommand(() => {
      // На момент команды выделенное поле ещё в документе: читаем его значение.
      вПоле = document.querySelector<HTMLTextAreaElement>("textarea[aria-hidden='true']")?.value ?? "";
      return true;
    });

    await copyText("ключ=значение с пробелами и =вторым знаком");

    expect(вПоле).toBe("ключ=значение с пробелами и =вторым знаком");
  });

  it("отказ современного пути НЕ роняет копирование — есть второй", async () => {
    // Разрешение отозвано, буфер занят другим приложением: это отказ, а не
    // отсутствие. Прежде на нём копирование заканчивалось.
    подменитьClipboard({ writeText: jest.fn<(t: string) => Promise<void>>().mockRejectedValue(new Error("denied")) });
    const вызовы = задатьExecCommand(true);

    await expect(copyText("x")).resolves.toBe(true);
    expect(вызовы).toEqual(["copy"]);
  });

  it("исход ОТДАЁТСЯ вызывающему: не сработало — значит false", async () => {
    // Отрицание в паре с положительными выше. Без него «копирование работает»
    // зеленело бы и на помощнике, который всегда отвечает успехом, — а подпись
    // «Скопировано» над пустым буфером хуже отсутствия кнопки.
    подменитьClipboard(undefined);
    задатьExecCommand(false);

    await expect(copyText("x")).resolves.toBe(false);
  });

  it("временное поле не остаётся в документе", async () => {
    подменитьClipboard(undefined);
    задатьExecCommand(true);

    await copyText("x");

    expect(document.querySelector("textarea[aria-hidden='true']")).toBeNull();
  });
});
