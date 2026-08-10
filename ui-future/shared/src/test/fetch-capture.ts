// Чтение того, что клиент ДЕЙСТВИТЕЛЬНО отправил, для проб, подменяющих `fetch`.
//
// Зачем отдельный модуль. Проба, подменяющая `fetch`, обязана записать адрес и тело
// запроса — иначе утверждать «ушло вот это» нечем. Записывались они приведением
// `String(input)` / `String(init.body)`, и оба приведения БАЗОВЫЕ: их аргументы по
// типу шире строки.
//
//   • `RequestInfo | URL` — это и `Request`, у которого нет своего представления
//     строкой: `String(new Request(...))` даёт `[object Request]`. Адрес при этом
//     не «портится частично» — он пропадает целиком, а на его месте оказывается
//     текст, одинаковый для ЛЮБОГО запроса. Проба, отбирающая вызовы по адресу
//     (`calls.find(c => c.url.includes("/subnets"))`), не нашла бы ни одного —
//     либо, что хуже, нашла бы все.
//   • `BodyInit | null` — это и `Blob`/`FormData`/поток: `String(blob)` даёт
//     `[object Blob]`, и следующий за ним `JSON.parse` падает уже не про свой
//     предмет.
//
// Сегодня ни один такой вход не возникает: `api/client.ts` зовёт `fetch` со строковым
// адресом и телом `JSON.stringify(...)`. Но это свойство ВЫЗЫВАЮЩЕГО, а не типа, и
// держаться на нём молча нельзя: смени клиент форму тела — пробы не покраснели бы, а
// стали бы разбирать заглушку. Поэтому обе функции ниже читают то, что названо, а на
// форме, которую прочитать нельзя, ОТКАЗЫВАЮТ вслух.

/**
 * Адрес запроса — тем полем, которое его действительно несёт.
 *
 * `Request` хранит адрес в `.url`, `URL` — в `.href`; строка сама себе адрес.
 */
export function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/**
 * Тело запроса как строка JSON — та самая, которую собрал `api/client.ts`.
 *
 * Отсутствие тела (GET) — это `undefined`, а не пустая строка: «тела не было» и
 * «тело было пустым» — разные события, и проба вправе их различать.
 *
 * @throws если тело есть, но не строка — читать его как JSON нельзя, и молчаливое
 *   `null` здесь означало бы зелёную пробу о неотправленном запросе.
 */
export function requestBodyText(body: BodyInit | null | undefined): string | undefined {
  if (body == null) return undefined;
  if (typeof body !== "string") {
    throw new Error(
      `тело запроса не строка JSON, а ${Object.prototype.toString.call(body)} — ` +
        `проба читать его не умеет (api/client.ts отправляет JSON.stringify)`,
    );
  }
  return body;
}

/** Разобранное тело запроса; `null` — тела не было (GET). */
export function requestBody(body: BodyInit | null | undefined): Record<string, unknown> | null {
  const text = requestBodyText(body);
  return text === undefined ? null : (JSON.parse(text) as Record<string, unknown>);
}
