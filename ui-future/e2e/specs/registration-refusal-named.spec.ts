// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect } from "@playwright/test";
import { identityRefusalFromText, identityRefusalOnPage, register, test } from "./fixtures";

/**
 * ОТКАЗ РЕГИСТРАЦИИ НАЗЫВАЕТСЯ СВОИМ ТЕКСТОМ, А НЕ НАШИМ СИМПТОМОМ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * У регистрации ТРИ наблюдаемых исхода, а не два: печенье сессии выдано ·
 * служба личности отвергла поток и напечатала свой отказ · ещё не готово.
 * Фикстур входа различал два — «печенье есть» и «печенья нет», — поэтому отказ
 * читался как «не готово», проба ждала полный срок и сообщала, что печенья нет.
 *
 * Причина при этом стояла НА ТОМ ЖЕ ЭКРАНЕ: провайдер печатает разбор отказа
 * (код перехода, состояние, сообщение). Прогон 33352816209 отдал пять падений
 * одним текстом про печенье, а в снимке каждой из пяти страниц лежало
 * `webhook failed with status code 401` — то есть поток был отвергнут на
 * обратном вызове, и до выдачи сессии дело не доходило вовсе.
 *
 * Это класс `testing.md` §«Диагноз ставится по ТЕКСТУ отказа, а не по имени
 * упавшего шага»: имя падения назвало последствие, текст на странице — причину.
 *
 * ПОЧЕМУ ЭТА ПРОБА НЕ ТРЕБУЕТ СТЕНДА
 *
 * Её предмет — РАСПОЗНАВАНИЕ отказа, а не поведение продукта на кластере.
 * Страница подаётся содержимым (`setContent`), поэтому проба судит ровно то,
 * ради чего заведена, и не зависит ни от подъёма стенда, ни от службы личности.
 * Способность упасть доказывается парой: страница отказа обязана быть узнана,
 * ЗАКОННЫЙ БЛИЗНЕЦ — обычная страница консоли — обязан остаться неузнанным.
 * Без второй половины распознаватель, отвечающий «отказ» на что угодно, был бы
 * зелёным.
 */

/** Разметка страницы отказа — снята с падения прогона 33352816209 дословно. */
const REFUSAL_PAGE = `
<main>
  <div>
    <h2>An error occurred</h2>
    <div>Error details</div>
    <div>{ "id": "b07c5b0d-7fb4-442d-8163-6f4cf437bb33", "error": { "code": 502,
      "reason": "A third-party upstream service responded improperly. Please try again later.",
      "status": "Bad Gateway", "message": "webhook failed with status code 401" },
      "created_at": "2026-08-31T03:24:32.346049Z" }</div>
    <a href="welcome">Go Back</a>
  </div>
</main>`;

/** Законный близнец: обычная страница консоли, отказом НЕ являющаяся. */
const ORDINARY_PAGE = `
<main>
  <h1>Сети</h1>
  <p>Ничего не найдено</p>
  <button>Создать сеть</button>
</main>`;

test.describe("отказ регистрации назван своим текстом", () => {
  test("страница отказа службы личности узнаётся и отдаёт СВОЙ разбор", async ({ page }) => {
    // verifies #1740
    await page.setContent(REFUSAL_PAGE);

    const named = await identityRefusalOnPage(page);
    expect(
      named,
      "страница, на которой продукт напечатал разбор отказа, обязана быть узнана: " +
        "иначе проба входа сообщит про отсутствующее печенье, а причина останется " +
        "лежать в артефакте прогона",
    ).not.toBe("");
    expect(named, "разбор обязан нести сообщение провайдера дословно").toContain(
      "webhook failed with status code 401",
    );
    expect(named, "разбор обязан нести код перехода — по нему видно, чей это отказ").toContain(
      "502",
    );
  });

  test("обычная страница консоли отказом НЕ считается", async ({ page }) => {
    // verifies #1740
    await page.setContent(ORDINARY_PAGE);

    expect(
      await identityRefusalOnPage(page),
      "распознаватель, отвечающий «отказ» на обычную страницу, остановил бы вход " +
        "там, где он исправен: без этой половины пара односторонняя",
    ).toBe("");
  });

  test("фикстур входа сообщает отказ провайдера, а не отсутствие печенья", async ({ page }) => {
    // verifies #1740
    //
    // Утверждается ПРОВЯЗКА, а не распознаватель: пробы выше доказывают, что
    // страница отказа узнаётся, и молчат о том, спрашивает ли её `register`.
    // Здесь поток регистрации подаётся перехватом — два шага и отказ на втором,
    // ровно как это выглядело в прогоне 33352816209, — и утверждается ТЕКСТ, с
    // которым падает сам фикстур. Стенд для этого не нужен: предмет — поведение
    // пробы, а не продукта.
    await page.route("**/*", async (route) => {
      // Шаги различаются ПУТЁМ, а не запросом: форма GET отбрасывает строку
      // запроса из своего адреса и подставляет собственные поля, поэтому
      // «шаг» в запросе до обработчика не доезжает — на этом первая редакция
      // подставы и обожглась.
      const url = new URL(route.request().url());
      if (url.pathname === "/registration") {
        await route.fulfill({
          contentType: "text/html",
          body:
            '<form action="/step2" method="GET">' +
            '<input name="traits.email"><input name="traits.display_name">' +
            '<button type="submit">Далее</button></form>',
        });
        return;
      }
      if (url.pathname === "/step2") {
        await route.fulfill({
          contentType: "text/html",
          body:
            '<form action="/error" method="GET">' +
            '<input type="password" name="password">' +
            '<button type="submit">Готово</button></form>',
        });
        return;
      }
      await route.fulfill({ contentType: "text/html", body: REFUSAL_PAGE });
    });

    const failure = await register(page).then(
      () => "",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );

    expect(failure, "отвергнутая регистрация обязана уронить фикстур").not.toBe("");
    expect(
      failure,
      "вердикт обязан нести отказ ПРОВАЙДЕРА: без него читатель идёт разбирать " +
        "ожидание печенья, тогда как поток до выдачи не дошёл вовсе",
    ).toContain("webhook failed with status code 401");
    expect(failure, "и назвать, чей это отказ").toContain("ОТВЕРГНУТА службой личности");
  });

  test("печенье сессии решает РАНЬШЕ распознавателя отказа", async ({ page }) => {
    // verifies #1740
    //
    // ЗАКОННЫЙ БЛИЗНЕЦ провязки выше, и он несущий: распознаватель отказа стоит
    // на пути КАЖДОГО входа, поэтому проба «отказ назван» без этой половины
    // зеленела бы и на фикстуре, который роняет исправный вход.
    //
    // ПОЧЕМУ СТРАНИЦА НЕСЁТ И ПЕЧЕНЬЕ, И РАЗБОР ОТКАЗА. Первая редакция этой
    // пробы подавала чистую страницу успеха — и оставалась зелёной, когда
    // распознаватель заменяли на «отказ на что угодно»: до него дело просто не
    // доходило. То есть проба не могла упасть и доказывала не порядок, а
    // собственную беспредметность. Здесь оба признака стоят ОДНОВРЕМЕННО,
    // поэтому вердикт решает ПОРЯДОК проверок — и перестановка его роняет.
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === "/registration") {
        await route.fulfill({
          contentType: "text/html",
          body:
            '<form action="/step2" method="GET">' +
            '<input name="traits.email"><input name="traits.display_name">' +
            '<button type="submit">Далее</button></form>',
        });
        return;
      }
      if (url.pathname === "/step2") {
        await route.fulfill({
          contentType: "text/html",
          body:
            '<form action="/welcome" method="GET">' +
            '<input type="password" name="password">' +
            '<button type="submit">Готово</button></form>',
        });
        return;
      }
      await route.fulfill({
        contentType: "text/html",
        headers: { "set-cookie": "ory_kratos_session=probe; Path=/" },
        body: REFUSAL_PAGE,
      });
    });

    const failure = await register(page).then(
      () => "",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );
    expect(
      failure,
      "вход, который завершился печеньем сессии, обязан пройти, даже если на " +
        "странице остался разбор отказа: распознаватель стоит на пути каждого " +
        "входа, и ложное срабатывание здесь остановило бы весь набор",
    ).toBe("");
  });

  test("разбор берётся из ТЕКСТА страницы, а не из её разметки", async () => {
    // verifies #1740
    // Форма страницы отказа принадлежит провайдеру и меняется с его версией;
    // разбор внутри неё — часть контракта отказа. Поэтому распознаватель судит
    // текст, и проба утверждает это отдельно от разметки выше.
    expect(
      identityRefusalFromText('{"error": {"code": 502, "message": "webhook failed with status code 401"}}'),
    ).toContain("webhook failed with status code 401");
    expect(identityRefusalFromText("Сети · Ничего не найдено"), "проза отказом не является").toBe(
      "",
    );
    expect(
      identityRefusalFromText('{"error": {"code": 400, "message": "traits: invalid"}}'),
      "любой отказ провайдера, а не только один его код",
    ).toContain("traits: invalid");
  });
});
