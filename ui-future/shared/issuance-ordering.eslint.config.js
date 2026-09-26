// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ПРАВИЛО ЛИНТА МЕСТ ВЫПУСКА (приёмка F8, Р10, F8-46) — ОДНО на все десять пакетов
// консоли: девять приложений и `shared` берут его отсюда, а не копией.
//
// Каждое обращение консоли к сети выпускается упорядочивающим транспортом
// (`shared/src/api/carrier-order.ts`, экспорт `orderedTransport`). Держит это ИСПОЛНЕНИЕ:
// страж `shared/src/test/issuance-guard.ts` в пробах jest и в браузере сквозных проб.
// Исполнение судит только исполненное; линт судит ВЕСЬ прод-код — и быстрее, — но только
// по записи: вычисленный ключ (`window[k]`) и окно, отданное значением API браузера, ему
// не видны. Поэтому линт — второй держатель, а не единственный, и полноты не обещает.
//
// Что запрещено вне своего дома:
//   • `fetch` — глобальным именем и членом любого объекта, кроме `orderedTransport`
//     (`window.fetch`, `globalThis["fetch"]`, `const { fetch } = window`); `Reflect.get`
//     по ключу "fetch". Дом — упорядочивающий транспорт;
//   • `EventSource` — дом один: приёмник потока (`shared/src/lib/subscription/hub.ts`),
//     поток стоит на учёте упорядочения;
//   • `XMLHttpRequest`, `WebSocket`, `sendBeacon` — дома у них нет вовсе;
//   • ключ собственного транспорта ПРОБЫ (`kacho.probe.fetch`): в продукте его нет.
//
// Пробы и их оснастка (`*.test.*`, `src/test/**`) из области правила выведены: они
// подставляют сеть (`globalThis.fetch = …`) под стражем исполнения, который их и судит.
//
// Имя файла кончается на `.config.js` намеренно: это часть конфигурации линта, и
// объявленный `ignores` каждого пакета (`*.config.js`) выводит его из области
// type-aware правил, как и сами `eslint.config.js`.

const WHY =
  "каждое обращение консоли к сети выпускается упорядочивающим транспортом " +
  "(orderedTransport из @shared/api/carrier-order): обращение мимо него ответом края " +
  "на прежний носитель гасит перевыпущенный (приёмка F8, Р10, F8-46)";

const TRANSPORTS = {
  fetch: `fetch вне упорядочивающего транспорта: ${WHY}`,
  EventSource: `EventSource вне приёмника потока (lib/subscription/hub.ts): ${WHY}`,
  XMLHttpRequest: `XMLHttpRequest: ${WHY}`,
  WebSocket: `WebSocket: ${WHY}`,
};

/**
 * Блоки правила для пакета.
 *
 * @param {{ sender?: string[], stream?: string[] }} homes — файлы пакета, где транспорт
 *   законен: `sender` — упорядочивающий транспорт (`fetch`), `stream` — приёмник потока
 *   (`EventSource`). Пути — от каталога пакета; у девяти приложений домов нет.
 */
export function issuanceOrdering({ sender = [], stream = [] } = {}) {
  const rulesFor = (allowed) => {
    const names = Object.keys(TRANSPORTS).filter((n) => !allowed.includes(n));
    return {
      "no-restricted-globals": ["error", ...names.map((name) => ({ name, message: TRANSPORTS[name] }))],
      "no-restricted-properties": [
        "error",
        ...names.map((property) =>
          property === "fetch"
            ? { property, allowObjects: ["orderedTransport"], message: TRANSPORTS.fetch }
            : { property, message: TRANSPORTS[property] },
        ),
        { property: "sendBeacon", message: `sendBeacon: ${WHY}` },
      ],
      "no-restricted-syntax": [
        "error",
        ...(allowed.includes("fetch")
          ? []
          : [
              {
                selector: "CallExpression[callee.object.name='Reflect'][arguments.1.value='fetch']",
                message: TRANSPORTS.fetch,
              },
            ]),
        {
          selector: "Literal[value='kacho.probe.fetch']",
          message: "транспорт пробы (kacho.probe.fetch) — оснастка проб, в продукте его нет: " + WHY,
        },
      ],
    };
  };
  const tests = ["**/*.test.{ts,tsx}", "src/test/**"];
  const blocks = [{ files: ["**/*.{ts,tsx}"], ignores: tests, rules: rulesFor([]) }];
  if (sender.length > 0) blocks.push({ files: sender, ignores: tests, rules: rulesFor(["fetch"]) });
  if (stream.length > 0) blocks.push({ files: stream, ignores: tests, rules: rulesFor(["EventSource"]) });
  return blocks;
}
