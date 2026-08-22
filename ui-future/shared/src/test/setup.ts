import { configure } from "@testing-library/dom";
import "@testing-library/jest-dom";
import React from "react";
import { jest } from "@jest/globals";
import { antdStub } from "./antd-stub";
import { TextDecoder, TextEncoder } from "node:util";

Object.assign(globalThis, {
  TextDecoder,
  TextEncoder,
});

// jsdom ships no ResizeObserver, and ResourceTable measures its own body with
// one to size the scroll area. Without a stub every render of a list throws
// before a single row exists, so nothing above the table can be tested.
if (!("ResizeObserver" in globalThis)) {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  Object.assign(globalThis, { ResizeObserver: ResizeObserverStub });
}

// Ещё два пробела в API браузера, которых у jsdom нет, а у antd-организмов
// (Dropdown, Tabs) — есть на пути монтирования. Без них render страницы падает
// AggregateError'ом, в котором ИМЯ причины не печатается вовсе: список
// вложенных ошибок React 19 в отчёт jest не выводит, и падение читается как
// «страница не монтируется», хотя не хватает ровно этих заглушек.
//
// Приехало сюда из окружений nlb и registry при сведении (#418): они это несли,
// общее окружение — нет. Заглушка НЕ подменяет поведение продукта — она даёт
// среде ответ той же ФОРМЫ, какую даёт браузер: медиазапрос, который не совпал.
// Проба, которой понадобится настоящий замер, обязана это назвать, а не молча
// получить нули.
//
// Обёртки над `getComputedStyle` рядом НЕ переехало намеренно. В исходных копиях
// она подменяла метод функцией, которая зовёт его же и ТЕРЯЕТ второй аргумент
// (псевдоэлемент). Прогон всех семи модулей одинаково зелёный без неё, поэтому
// сюда приехала бы не заглушка, а сужение чужого API без предмета.
if (typeof window !== "undefined" && typeof window.matchMedia !== "function") {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}

// @ant-design/icons мокается через moduleNameMapper (стаб ./antd-icons-stub.tsx
// с реальными статическими named-экспортами), а НЕ Proxy-моком: Proxy их не
// даёт, и под --experimental-vm-modules ESM-линкер не находит binding для
// `import { XOutlined }` — процесс jest уходит молча, с кодом 0 и без отчёта,
// то есть весь прогон читается как зелёный, ни разу не выполнившись. Тот же
// корень и то же лечение, что в host.

jest.unstable_mockModule("@monaco-editor/react", () => ({
  __esModule: true,
  default: (props: React.HTMLAttributes<HTMLDivElement>) => React.createElement("div", props),
  // Настоящий модуль отдаёт `loader`, и компонент зовёт `loader.config`, чтобы
  // редактор грузился со своего origin. Заменитель без него ронял бы пробы на
  // отсутствии метода — то есть на форме дублёра, а не на предмете.
  loader: { config: () => undefined },
}));

jest.unstable_mockModule("antd", () => antdStub());

// БЮДЖЕТ ОЖИДАНИЯ УСЛОВИЯ — один на все девять модулей консоли.
//
// Умолчание testing-library — 1000 мс, и его не хватало: проба витрины квот
// падала на конвейере с «Unable to find an element», хотя страница исправна.
// Витрина спрашивает ПЯТЬ владельцев и рисует таблицу после последнего ответа;
// локально это 18–36 мс (замерено тремя прогонами), но конвейер гонит 1870 проб
// в ОДНОМ процессе (`--runInBand`), и в пике на них приходится неизвестная доля
// машины.
//
// Это НЕ пауза и не подпорка: `waitFor` ждёт УСЛОВИЕ и возвращается, как только
// оно выполнено, — на здоровом прогоне бюджет не тратится вовсе. Он задаёт лишь
// границу, за которой отказ становится вердиктом. Величина — двузначный запас к
// замеренному максимуму; она не скрывает медленную страницу, потому что
// медленная страница проявится ростом времени прогона, а не этим числом.
//
// Место ОДНО на все модули: выписывать бюджет в каждом ожидании значило бы
// заводить его копии, которые разойдутся молча.
configure({ asyncUtilTimeout: 5_000 });
