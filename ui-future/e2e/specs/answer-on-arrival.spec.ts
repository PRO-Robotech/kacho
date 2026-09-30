// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import http from "node:http";
import type { AddressInfo } from "node:net";
import { expect, type CDPSession, type Page } from "@playwright/test";
import { captureAnswers, lanePostAnswer } from "./answer-on-arrival";
import { test } from "./fixtures";

// Держатель помощника `answer-on-arrival.ts` — на странице, которая делает ровно
// то, что экраны церемоний: отправляет глагол, читает ответ и сразу уходит
// документом. Сервер — настоящий, на петле: ответ, подставленный `route.fulfill`,
// тело держит сам прогонщик, и потери, ради которой помощник заведён, на нём нет.

const VERB = "/iam/v1/auth/login";
const ANSWER = '{"session":{"assuranceLevel":"2"}}';

// Экран подсажен пробой, и его обращение — обращение ПРОБЫ, а не консоли: оно
// идёт транспортом пробы, а не `fetch` окна, который судит страж мест выпуска
// (`issuance-guard.ts`).
const LEAVING_PAGE = `<!doctype html><button id="go">go</button><script>
document.getElementById("go").onclick = async () => {
  const res = await window[Symbol.for("kacho.probe.fetch")]("${VERB}", { method: "POST", body: "{}" });
  await res.text();
  window.location.replace("/next");
};
</script>`;

// Близнец уходящего экрана: тот же глагол, то же чтение ответа — и документ
// остаётся. Он отличается от уходящего ровно одним фактом, уходом.
const STAYING_PAGE = LEAVING_PAGE.replace('window.location.replace("/next");', "");
if (STAYING_PAGE === LEAVING_PAGE) {
  throw new Error("близнец уходящего экрана не собран: строки ухода в экране нет, и «остающийся» экран ушёл бы тоже");
}
const STAYING = "/stay";

async function withServer<T>(body: (origin: string) => Promise<T>): Promise<T> {
  const server = http.createServer((req, res) => {
    if (req.url === VERB && req.method === "POST") {
      res.writeHead(200, { "content-type": "application/json", "set-cookie": "answer_on_arrival=1; Path=/" });
      res.end(ANSWER);
      return;
    }
    res.writeHead(200, { "content-type": "text/html" });
    res.end(req.url === "/next" ? "<p>next</p>" : req.url === STAYING ? STAYING_PAGE : LEAVING_PAGE);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    return await body(`http://127.0.0.1:${(server.address() as AddressInfo).port}`);
  } finally {
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}

test("F8 · тело ответа, за которым страница уходит документом, снято до страницы и ответ дошёл неизменным", async ({
  page,
}) => {
  // verifies #1274
  await withServer(async (origin) => {
    await captureAnswers(page, [VERB]);
    await page.goto(`${origin}/`);
    const [answer] = await Promise.all([lanePostAnswer(page, VERB), page.click("#go")]);
    await expect.poll(() => new URL(page.url()).pathname, { message: "страница не ушла документом" }).toBe("/next");
    expect(answer.status()).toBe(200);
    expect(await answer.text(), "снятое тело не то, что отдал сервер").toBe(ANSWER);
    const cookies = await page.context().cookies(origin);
    expect(
      cookies.some((c) => c.name === "answer_on_arrival"),
      "печенье ответа не поставлено: перехват изменил ответ",
    ).toBe(true);
  });
});

/**
 * Сеть браузера, спрошенная СВОИМ сеансом протокола, — а не `Response.body()`
 * прогонщика.
 *
 * ПОЧЕМУ НЕ `Response.body()` (#1274, прогон 36201294380). Тело ответа прогонщик
 * читает ОДИН раз и хранит исход в самом объекте ответа. Первым его читает не
 * проба, а запись трассы: фикстура `page` пишет трассу со снимками, и её запись
 * сети зовёт `body()` на окончании загрузки (playwright-core 1.56.1,
 * `server/har/harTracer.js`, `_onRequestFinished`) — ДО ухода документа, если
 * экран уходит хоть немного позже. Тогда `body()` пробы отдаёт уже снятое записью, и
 * проба читает «прочитано» о документе, которого нет. Так она упала на e305bcb:
 * трасса того падения хранит тело ответа глагола, а `body()` пробы вернулся за
 * 0,14 мс — без обращения к браузеру. Замер вне стенда (Chrome 152, по 10
 * повторов): экран уходит сразу — тела нет 10 из 10; экран уходит через 20 мс —
 * со страницей фикстуры «прочитано» 10 из 10, со страницей своего контекста без
 * трассы тела нет 10 из 10.
 *
 * Свой сеанс держит своё: чтение трассы идёт другим сеансом, и снятого им здесь
 * нет. Спрашивает сеанс пробы, и спрашивает он ровно тогда, когда проба решила.
 */
async function browserNetwork(page: Page): Promise<{
  arrived(what: string): Promise<string>;
  body(requestId: string): Promise<string>;
  detach(): Promise<void>;
}> {
  const cdp: CDPSession = await page.context().newCDPSession(page);
  const sent: string[] = [];
  const finished = new Set<string>();
  cdp.on("Network.requestWillBeSent", (e) => {
    if (e.request.method === "POST" && new URL(e.request.url).pathname === VERB) sent.push(e.requestId);
  });
  cdp.on("Network.loadingFinished", (e) => {
    finished.add(e.requestId);
  });
  await cdp.send("Network.enable");
  return {
    // Признак прибытия — событие БРАУЗЕРА «загрузка ответа закончена»: с него
    // тело у браузера целиком. Ждётся условие, а не время.
    async arrived(what) {
      const seen = sent.length;
      await expect
        .poll(() => sent.length > seen && finished.has(sent[sent.length - 1]), {
          message: `${what}: ответ глагола ${VERB} браузер не получил`,
        })
        .toBe(true);
      return sent[sent.length - 1];
    },
    async body(requestId) {
      const got = await cdp.send("Network.getResponseBody", { requestId });
      return got.base64Encoded ? Buffer.from(got.body, "base64").toString("utf8") : got.body;
    },
    detach: () => cdp.detach(),
  };
}

test("F8 · без перехвата тело такого ответа браузер уже не отдаёт — помощнику есть что держать", async ({ page }) => {
  // verifies #1274 — проба ПРЕДПОСЫЛКИ помощника. Покраснела — браузер стал
  // держать тело ушедшего документа, и помощник держит то, чего больше нет.
  await withServer(async (origin) => {
    const network = await browserNetwork(page);
    try {
      // Близнец: документ жив — тот же вопрос тем же сеансом тело получает.
      // Без него «тела нет» ниже не отличалось бы от сеанса, который тел не
      // держит вовсе.
      await page.goto(`${origin}${STAYING}`);
      const kept = network.arrived("документ остаётся");
      await page.click("#go");
      expect(await network.body(await kept), "документ жив, а тело ответа браузер не отдал").toBe(ANSWER);

      await page.goto(`${origin}/`);
      const left = network.arrived("документ уходит");
      await page.click("#go");
      const requestId = await left;
      await expect.poll(() => new URL(page.url()).pathname, { message: "страница не ушла документом" }).toBe("/next");
      const read = await network.body(requestId).then(
        () => "прочитано",
        (e: unknown) => (e instanceof Error ? e.message : String(e)),
      );
      expect(read).toContain("No resource with given identifier found");
    } finally {
      await network.detach();
    }
  });
});
