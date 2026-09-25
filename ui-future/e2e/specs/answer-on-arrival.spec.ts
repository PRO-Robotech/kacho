// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import http from "node:http";
import type { AddressInfo } from "node:net";
import { expect } from "@playwright/test";
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

async function withServer<T>(body: (origin: string) => Promise<T>): Promise<T> {
  const server = http.createServer((req, res) => {
    if (req.url === VERB && req.method === "POST") {
      res.writeHead(200, { "content-type": "application/json", "set-cookie": "answer_on_arrival=1; Path=/" });
      res.end(ANSWER);
      return;
    }
    res.writeHead(200, { "content-type": "text/html" });
    res.end(req.url === "/next" ? "<p>next</p>" : LEAVING_PAGE);
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

test("F8 · без перехвата тело такого ответа браузер уже не отдаёт — помощнику есть что держать", async ({ page }) => {
  // verifies #1274 — проба ПРЕДПОСЫЛКИ помощника. Покраснела — браузер стал
  // держать тело ушедшего документа, и помощник держит то, чего больше нет.
  await withServer(async (origin) => {
    await page.goto(`${origin}/`);
    const [res] = await Promise.all([
      page.waitForResponse((r) => new URL(r.url()).pathname === VERB && r.request().method() === "POST"),
      page.click("#go"),
    ]);
    await expect.poll(() => new URL(page.url()).pathname, { message: "страница не ушла документом" }).toBe("/next");
    const read = await res.body().then(
      () => "прочитано",
      (e: unknown) => (e instanceof Error ? e.message : String(e)),
    );
    expect(read).toContain("No resource with given identifier found");
  });
});
