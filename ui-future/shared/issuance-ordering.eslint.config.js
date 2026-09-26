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
// не видны. Поэтому для `fetch` линт — второй держатель, а не единственный, и полноты не
// обещает.
//
// ПЕРЕХОД ДОКУМЕНТА на путь края — тоже обращение к краю мимо упорядочивающего
// транспорта, но страж исполнения его не видит: он стоит на `fetch`, а переход идёт
// навигацией документа. Держит его это правило (`kacho-issuance/document-navigation`):
// исполнения у перехода нет, а перепись о части тех же форм лишь подсказывает. Судит
// правило по записи адреса — см. ниже.
//
// Что запрещено вне своего дома:
//   • `fetch` — глобальным именем и членом любого объекта, кроме `orderedTransport`
//     (`window.fetch`, `globalThis["fetch"]`, `const { fetch } = window`); `Reflect.get`
//     по ключу "fetch". Дом — упорядочивающий транспорт;
//   • `EventSource` — дом один: приёмник потока (`shared/src/lib/subscription/hub.ts`),
//     поток стоит на учёте упорядочения;
//   • `XMLHttpRequest`, `WebSocket`, `sendBeacon` — дома у них нет вовсе;
//   • ключ собственного транспорта ПРОБЫ (`kacho.probe.fetch`): в продукте его нет;
//   • переход документа на путь края — дома у него нет. Формы: атрибут JSX `href`,
//     `xlinkHref`, `action`, `formAction` любого элемента (`<a href>`, `<form action>`,
//     `<button formAction>`); присвоение члену `href`/`action`/`formAction`
//     (`a.href = …; a.click()`, `location.href = …`) и `location`; `setAttribute` тех же
//     имён; `location.assign/replace(…)`, в том числе через постоянную-псевдоним
//     (`const loc = window.location`); `open(…)` под любым именем.
//
// Путь края — `EDGE_PATH` ниже, тем же выражением, что у переписи мест выпуска
// (`src/test/issuance-census.ts`); одно ли оно, сверяет гейт
// `ui-future/scripts/check-issuance-ordering-lint.mjs` по узлам разбора переписи. Адрес
// судится по ЗАПИСИ: строка, шаблон (его голова до первой подстановки; подстановка
// происхождения — `….origin` либо имя `origin` — пропускается), левое плечо сцепления `+`
// (правое, если левое — происхождение), обе ветви
// `?:` и `??`/`||`, постоянная `const` того же файла. Адрес, собранный вне записи, —
// импортированная постоянная, возврат функции, `new URL(…)`, путь из данных, — правилу не
// виден, и держателя у такого перехода нет: это граница, а не покрытие.
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

/** Путь края: API доменов (`/<домен>/v<N>/…`), операции, проверка живости края. */
export const EDGE_PATH = /^\/(?:[a-z][a-z0-9-]*\/v\d+(?:[/?]|$)|operations(?:[/?]|$)|healthz$)/;
/** Происхождение перед путём: `https://console.test/iam/v1/…` — тоже путь края. */
export const EDGE_ORIGIN = /^[a-z][a-z0-9+.-]*:\/\/[^/]+/i;

/** Адрес — путь края, в том числе с происхождением перед ним. */
function isEdgePath(text) {
  return EDGE_PATH.test(text.replace(EDGE_ORIGIN, ""));
}

const NAVIGATION_WHY =
  "переход документа на путь края — обращение к краю мимо упорядочивающего транспорта; " +
  "ответ края берётся orderedTransport, а документу отдаётся результат (Blob, адрес объекта): " +
  "ответ края на прежний носитель гасит перевыпущенный (приёмка F8, Р10, F8-46)";

/** Атрибут JSX, уводящий документ по адресу: имя в нижнем регистре. */
const NAVIGATION_ATTRIBUTES = new Set(["href", "xlinkhref", "action", "formaction"]);
/** Член, присвоение которому уводит документ (`a.href`, `form.action`, `location.href`). */
const NAVIGATION_MEMBERS = new Set(["href", "action", "formaction"]);
/** Обёртки выражения TypeScript, не меняющие значения. */
const TS_WRAPPERS = new Set(["TSAsExpression", "TSNonNullExpression", "TSSatisfiesExpression", "TSTypeAssertion"]);
/** Глубина раскрытия постоянных: `const A = B; const B = "…"`. */
const CONST_DEPTH = 4;

/** Имя члена, если оно записано: `x.href`, `x["href"]`; иначе `null`. */
function memberName(node) {
  if (node?.type !== "MemberExpression") return null;
  if (!node.computed && node.property.type === "Identifier") return node.property.name;
  if (node.computed && node.property.type === "Literal" && typeof node.property.value === "string")
    return node.property.value;
  return null;
}

/** `location`, `window.location`, `document.location`, `x["location"]`, псевдоним `const loc = …location`. */
function isLocation(node, sourceCode, depth = 0) {
  if (!node || depth > CONST_DEPTH) return false;
  if (TS_WRAPPERS.has(node.type)) return isLocation(node.expression, sourceCode, depth);
  if (node.type === "Identifier")
    return node.name === "location" || isLocation(constInit(node, sourceCode), sourceCode, depth + 1);
  return memberName(node) === "location";
}

/** Происхождение окна: `window.location.origin`, `location.origin`, `self.origin`. */
function isOrigin(node) {
  return memberName(node) === "origin" || (node?.type === "Identifier" && node.name === "origin");
}

/** Выражение `const` того же файла, связанное с именем, — либо `null`. */
function constInit(identifier, sourceCode) {
  for (let scope = sourceCode.getScope(identifier); scope; scope = scope.upper) {
    const variable = scope.set.get(identifier.name);
    if (!variable) continue;
    const def = variable.defs.length === 1 ? variable.defs[0] : null;
    const declarator = def?.type === "Variable" ? def.node : null;
    return declarator?.parent?.kind === "const" && declarator.id.type === "Identifier" ? declarator.init : null;
  }
  return null;
}

/**
 * Адреса, которые запись несёт сама: строка, голова шаблона, левое плечо `+`, ветви
 * `?:`, `??`, `||`, постоянная файла. Подстановка происхождения окна в начале адреса
 * пропускается: `${location.origin}/iam/v1/…` — путь края с происхождением.
 */
function addressHeads(node, sourceCode, depth = 0) {
  if (!node || depth > CONST_DEPTH) return [];
  if (TS_WRAPPERS.has(node.type) || node.type === "JSXExpressionContainer" || node.type === "ChainExpression")
    return addressHeads(node.expression, sourceCode, depth);
  switch (node.type) {
    case "Literal":
      return typeof node.value === "string" ? [node.value] : [];
    case "TemplateLiteral": {
      const [head, next] = node.quasis.map((q) => q.value.cooked ?? q.value.raw);
      return head === "" && next !== undefined && isOrigin(node.expressions[0]) ? [next] : [head];
    }
    case "BinaryExpression":
      if (node.operator !== "+") return [];
      return isOrigin(node.left)
        ? addressHeads(node.right, sourceCode, depth)
        : addressHeads(node.left, sourceCode, depth);
    case "ConditionalExpression":
      return [...addressHeads(node.consequent, sourceCode, depth), ...addressHeads(node.alternate, sourceCode, depth)];
    case "LogicalExpression":
      return [...addressHeads(node.left, sourceCode, depth), ...addressHeads(node.right, sourceCode, depth)];
    case "Identifier":
      return addressHeads(constInit(node, sourceCode), sourceCode, depth + 1);
    default:
      return [];
  }
}

/**
 * Правило перехода документа на путь края. Судит место, где адрес уходит документу:
 * атрибут, присвоение, `setAttribute`, `location.assign/replace`, `open`.
 */
const documentNavigation = {
  meta: {
    type: "problem",
    docs: { description: "переход документа на путь края мимо упорядочивающего транспорта (F8-46)" },
    schema: [],
    messages: { edge: "{{form}} — адрес «{{address}}»: {{why}}" },
  },
  create(context) {
    const { sourceCode } = context;
    const judge = (node, form, value) => {
      const address = addressHeads(value, sourceCode).find(isEdgePath);
      if (address !== undefined)
        context.report({ node, messageId: "edge", data: { form, address, why: NAVIGATION_WHY } });
    };
    return {
      JSXAttribute(node) {
        const name = node.name.type === "JSXNamespacedName" ? node.name.name.name : node.name.name;
        if (!NAVIGATION_ATTRIBUTES.has(name.toLowerCase())) return;
        const element = node.parent.name;
        const tag = element.type === "JSXIdentifier" ? element.name : sourceCode.getText(element);
        judge(node, `<${tag} ${name}>`, node.value);
      },
      AssignmentExpression(node) {
        if (node.operator !== "=") return;
        const member = memberName(node.left);
        if (member !== null && NAVIGATION_MEMBERS.has(member.toLowerCase()))
          judge(node, `присвоение .${member}`, node.right);
        else if (isLocation(node.left, sourceCode)) judge(node, "присвоение location", node.right);
      },
      CallExpression(node) {
        const callee = node.callee.type === "ChainExpression" ? node.callee.expression : node.callee;
        const method = callee.type === "Identifier" ? callee.name : memberName(callee);
        const [first, second] = node.arguments;
        if (method === "setAttribute" || method === "setAttributeNS") {
          const [name, value] = method === "setAttribute" ? [first, second] : [second, node.arguments[2]];
          const attr =
            name?.type === "Literal" && typeof name.value === "string" ? name.value.replace(/^xlink:/i, "") : "";
          if (NAVIGATION_MEMBERS.has(attr.toLowerCase())) judge(node, `${method}("${name.value}")`, value);
        } else if ((method === "assign" || method === "replace") && isLocation(callee.object, sourceCode)) {
          judge(node, `location.${method}(…)`, first);
        } else if (method === "open") {
          judge(node, "open(…)", first);
        }
      },
    };
  },
};

/** Собственные правила набора: один объект на процесс — flat config сверяет плагин по ссылке. */
const PLUGIN = { meta: { name: "kacho-issuance" }, rules: { "document-navigation": documentNavigation } };

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
  const blocks = [
    { files: ["**/*.{ts,tsx}"], ignores: tests, rules: rulesFor([]) },
    // Переход документа на путь края: дома нет ни в одном пакете.
    {
      files: ["**/*.{ts,tsx}"],
      ignores: tests,
      plugins: { "kacho-issuance": PLUGIN },
      rules: { "kacho-issuance/document-navigation": "error" },
    },
  ];
  if (sender.length > 0) blocks.push({ files: sender, ignores: tests, rules: rulesFor(["fetch"]) });
  if (stream.length > 0) blocks.push({ files: stream, ignores: tests, rules: rulesFor(["EventSource"]) });
  return blocks;
}
