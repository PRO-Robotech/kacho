// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Route } from "@playwright/test";
import { identityRefusalFromText, identityRefusalOnPage, register, test } from "./fixtures";

/**
 * ОТКАЗ РЕГИСТРАЦИИ НАЗЫВАЕТСЯ СВОИМ ТЕКСТОМ, А НЕ НАШИМ СИМПТОМОМ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * У регистрации ТРИ наблюдаемых исхода, а не два: печенье сессии выдано ·
 * служба отвергла форму и экран назвал отказ · ещё не готово. Фикстура,
 * различавшая два, читала отказ как «не готово», ждала полный срок и сообщала,
 * что печенья нет, — тогда как причина стояла НА ТОМ ЖЕ ЭКРАНЕ. Это класс
 * `testing.md` §«Диагноз ставится по ТЕКСТУ отказа, а не по имени упавшего
 * шага» (прогон 33352816209: пять падений одним текстом про печенье при
 * разборе отказа в снимке каждой страницы).
 *
 * ПЕРЕЕЗД НА ФОРМУ ОТКАЗА НАШЕЙ СЛУЖБЫ (приёмка F8, F8-44). Посев набора ходит
 * нашим экраном регистрации, поэтому распознаватель узнаёт отказ НАШЕЙ службы —
 * `google.rpc.Status` `{code, message, details}` в ответе глагола и его текст,
 * названный экраном. Предмет этих проб — не поставщик, а ПОРЯДОК РЕШЕНИЯ:
 * печенье и поле формы решают раньше распознавателя, и разбор берётся из
 * текста, а не из разметки. Оба свойства переживают смену поставщика и
 * сохранены; формы отказа чужого поставщика в наборе нет ни в одном месте.
 *
 * ПОЧЕМУ ЭТИ ПРОБЫ НЕ ТРЕБУЮТ СТЕНДА
 *
 * Предмет — поведение пробы, а не продукта. Экран и ответ глагола подаются
 * перехватом, поэтому проба судит ровно распознавание и провязку. Каждая пара
 * держит обе стороны: отказ узнан — и законный близнец не узнан; без второй
 * половины распознаватель, отвечающий «отказ» на что угодно, был бы зелёным.
 */

/** Отказ регистрации нашей службы — тело, как его отдаёт полоса формы. */
const REFUSAL = {
  code: 9,
  message: "registration refused",
  details: [
    { "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "REGISTRATION_REFUSED", domain: "iam.kaname.cloud" },
  ],
};

/**
 * Экран регистрации — в той форме, какую видит человек: подписи полей, кнопка,
 * отказ предупреждением. Отправка идёт глаголом и показывает `message` ответа
 * дословно, как это делает экран консоли.
 */
function registrationScreen(extra = ""): string {
  return `
<main>
  <form aria-label="Новая учётная запись" id="f">
    <label for="e">Адрес электронной почты</label><input id="e" type="email">
    <label for="p">Пароль</label><input id="p" type="password">
    <button type="submit">Завести учётную запись</button>
    <div id="out"></div>
  </form>
  ${extra}
  <script>
    document.getElementById("f").addEventListener("submit", async (ev) => {
      ev.preventDefault();
      // Экран подсажен пробой: его обращение идёт транспортом пробы, а не
      // \`fetch\` окна, который судит страж мест выпуска (\`issuance-guard.ts\`).
      const res = await window[Symbol.for("kacho.probe.fetch")]("/iam/v1/auth/register", { method: "POST", body: "{}" });
      if (!res.ok) {
        const body = await res.json();
        const a = document.createElement("div");
        a.setAttribute("role", "alert");
        a.textContent = body.message;
        document.getElementById("out").appendChild(a);
      }
    });
  </script>
</main>`;
}

/** Экран, назвавший отказ вместо формы, — отказ на первом шаге. */
const REFUSAL_INSTEAD_OF_FORM = `<main><h1>Новая учётная запись</h1><div role="alert">${REFUSAL.message}</div></main>`;

/** Законный близнец: обычная страница консоли, отказом НЕ являющаяся. */
const ORDINARY_PAGE = `
<main>
  <h1>Сети</h1>
  <p>Ничего не найдено</p>
  <button>Создать сеть</button>
</main>`;

/** Подать экран и ответ глагола; `cookie` — выдаёт ли ответ носитель сессии. */
async function serve(route: Route, screen: string, answer: { status: number; body: unknown; cookie?: boolean }) {
  const url = new URL(route.request().url());
  // Кодировка объявляется явно: подписи экрана — кириллица, и без неё браузер
  // прочтёт документ однобайтной кодировкой, а доступные имена полей разойдутся
  // с теми, что видит человек.
  if (url.pathname === "/registration") {
    await route.fulfill({ contentType: "text/html; charset=utf-8", body: screen });
    return;
  }
  if (url.pathname === "/iam/v1/auth/register") {
    await route.fulfill({
      status: answer.status,
      contentType: "application/json; charset=utf-8",
      headers: answer.cookie ? { "set-cookie": "kaname_session=probe; Path=/" } : {},
      body: JSON.stringify(answer.body),
    });
    return;
  }
  await route.fulfill({ contentType: "text/html; charset=utf-8", body: ORDINARY_PAGE });
}

test.describe("отказ регистрации назван своим текстом", () => {
  test("F8-44 · экран, назвавший отказ, узнаётся, и разбор несёт текст службы дословно", async ({ page }) => {
    // verifies #1740
    // verifies #2780
    await page.setContent(REFUSAL_INSTEAD_OF_FORM);

    const named = await identityRefusalOnPage(page);
    expect(
      named,
      "экран, на котором продукт назвал отказ, обязан быть узнан: иначе проба входа " +
        "сообщит про отсутствующее печенье, а причина останется лежать в артефакте прогона",
    ).not.toBe("");
    expect(named, "разбор обязан нести текст службы дословно").toBe("registration refused");
  });

  test("F8-44 · обычная страница консоли отказом НЕ считается", async ({ page }) => {
    // verifies #1740
    // verifies #2780
    await page.setContent(ORDINARY_PAGE);

    expect(
      await identityRefusalOnPage(page),
      "распознаватель, отвечающий «отказ» на обычную страницу, остановил бы вход там, " +
        "где он исправен: без этой половины пара односторонняя",
    ).toBe("");
  });

  test("F8-44 · фикстура сообщает отказ службы, а не отсутствие печенья", async ({ page }) => {
    // verifies #1740
    // verifies #2780
    //
    // Утверждается ПРОВЯЗКА, а не распознаватель: экран подаётся перехватом, ответ
    // глагола — отказом нашей формы, и утверждается ТЕКСТ, с которым падает сама
    // фикстура.
    await page.route("**/*", (route) => serve(route, registrationScreen(), { status: 400, body: REFUSAL }));

    const failure = await register(page).then(
      () => "",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );

    expect(failure, "отвергнутая регистрация обязана уронить фикстуру").not.toBe("");
    expect(
      failure,
      "вердикт обязан нести отказ СЛУЖБЫ, снятый с экрана: без него читатель идёт " +
        "разбирать ожидание печенья, тогда как регистрация отвергнута",
    ).toContain("registration refused");
    expect(failure, "и назвать, чей это отказ").toContain("ОТВЕРГНУТА службой");
    expect(failure, "и код из ответа глагола — по нему видно, что это отказ службы").toContain(
      "9 · registration refused",
    );
  });

  test("F8-44 · печенье сессии решает РАНЬШЕ распознавателя отказа", async ({ page }) => {
    // verifies #1740
    // verifies #2780
    //
    // ЗАКОННЫЙ БЛИЗНЕЦ провязки выше, и он несущий: распознаватель стоит на пути
    // КАЖДОГО входа. Экран несёт И печенье, И отказ ОДНОВРЕМЕННО, поэтому вердикт
    // решает ПОРЯДОК проверок — и перестановка его роняет.
    await page.route("**/*", (route) =>
      serve(route, registrationScreen(), { status: 400, body: REFUSAL, cookie: true }),
    );

    const failure = await register(page).then(
      () => "",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );
    expect(
      failure,
      "регистрация, завершившаяся печеньем сессии, обязана пройти, даже если на экране " +
        "остался отказ: ложное срабатывание здесь остановило бы весь набор",
    ).toBe("");
  });

  test("F8-44 · фикстура сообщает отказ НА ШАГЕ ЭКРАНА, а не отсутствие формы", async ({ page }) => {
    // verifies #1740
    // verifies #2780
    //
    // ВТОРАЯ ПОЛОСА ТОГО ЖЕ МЕХАНИЗМА: у фикстуры ДВА ожидания — форма
    // регистрации на экране и печенье сессии после отправки, — и отказ представим
    // на каждом.
    await page.route("**/*", (route) => serve(route, REFUSAL_INSTEAD_OF_FORM, { status: 400, body: REFUSAL }));

    const failure = await register(page).then(
      () => "",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );

    expect(failure, "отказ на шаге экрана обязан уронить фикстуру").not.toBe("");
    expect(failure, "вердикт обязан нести текст отказа с экрана").toContain("registration refused");
    expect(failure, "и назвать, чей это отказ").toContain("ОТВЕРГНУТА службой");
    expect(failure, "и назвать, ГДЕ он случился: у двух ожиданий разные предметы").toContain(
      "на шаге экрана регистрации",
    );
  });

  test("F8-44 · форма регистрации решает РАНЬШЕ распознавателя отказа", async ({ page }) => {
    // verifies #1740
    // verifies #2780
    //
    // Близнец полосы выше: экран несёт И форму, И прежний отказ одновременно —
    // вердикт решает порядок проверок. Форма есть — шаг пройден, и регистрация
    // доходит до сессии.
    await page.route("**/*", (route) =>
      serve(route, registrationScreen(`<div role="alert">${REFUSAL.message}</div>`), {
        status: 200,
        body: { user: {}, session: {} },
        cookie: true,
      }),
    );

    const failure = await register(page).then(
      () => "",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );
    expect(
      failure,
      "экран, предложивший форму, обязан быть пройден, даже если на нём остался отказ: " +
        "ложное срабатывание здесь остановило бы весь набор",
    ).toBe("");
  });

  test("F8-44 · разбор ответа берётся из ТЕКСТА, и обе величины обязательны", async () => {
    // verifies #1740
    // verifies #2780
    expect(identityRefusalFromText(JSON.stringify(REFUSAL))).toBe("9 · registration refused");
    expect(
      identityRefusalFromText('{"code":3,"message":"Illegal argument password: required","details":[]}'),
      "любой отказ службы, а не только один его код",
    ).toBe("3 · Illegal argument password: required");
    expect(identityRefusalFromText("Сети · Ничего не найдено"), "проза отказом не является").toBe("");
    expect(identityRefusalFromText('{"message":"registration refused","details":[]}'), "без кода — не отказ").toBe("");
    expect(identityRefusalFromText('{"user":{},"session":{}}'), "ответ успеха — не отказ").toBe("");
    // Отказ края на глаголе с носителем приходит БЕЗ `details` (F4d-23) — это тот
    // же отказ, что с пустым `details` у службы (условие C3); `details` не
    // массивом — не отказ.
    expect(
      identityRefusalFromText('{"code":16,"message":"session ended; sign in again"}'),
      "отказ края без details — отказ",
    ).toBe("16 · session ended; sign in again");
    expect(identityRefusalFromText('{"code":16,"message":"x","details":{}}'), "details не массивом — не отказ").toBe(
      "",
    );
  });
});
