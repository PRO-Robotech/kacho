// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import http from "node:http";
import type { AddressInfo } from "node:net";
import { expect, type Page } from "@playwright/test";
import { test } from "./fixtures";
import { issuingDocuments, substituteDocumentReads, type IssuingDocuments } from "./issuing-document";

// Держатель помощника `issuing-document.ts` — на странице, которая делает ровно
// то, что список консоли в F6b-25: держит поток изменений, уходит документом, и
// поток, оборванный уходом, выпускает разбор отказа ТЕМ ЖЕ адресом (`hub.ts`,
// `explain`) — уже после начала перехода. Сервер — настоящий, на петле.
//
// Обращения подсаженных экранов — обращения ПРОБЫ и идут её транспортом, а не
// `fetch` окна, который судит страж мест выпуска (`issuance-guard.ts`).

const STREAM = "/subscription/v1/events";
const STREAM_URL = `${STREAM}?owner=vpc`;
const SESSION = "/iam/v1/auth/me";
const READ = "/vpc/v1/networks";
const REFUSAL = '{"code":7,"message":"refused by the probe"}';

const PROBE_FETCH = 'window[Symbol.for("kacho.probe.fetch")]';

// Список: поток открыт; оборванный — разбор тем же адресом; кнопка уводит документом.
const LIST_PAGE = `<!doctype html><button id="go">go</button><script>
const source = new EventSource(${JSON.stringify(STREAM_URL)});
source.addEventListener("opened", () => { document.title = "opened"; });
source.onerror = () => {
  if (source.readyState === EventSource.CLOSED) void ${PROBE_FETCH}(${JSON.stringify(STREAM_URL)}, { headers: { Accept: "text/event-stream" } });
};
document.getElementById("go").onclick = () => window.location.assign(location.search.includes("own") ? "/verification?own=1" : "/verification");
</script>`;

// Экран: своё обращение о сессии; близнец (`own`) выпускает ЕЩЁ и обращение потока — сам.
const SCREEN_PAGE = `<!doctype html><script>
if (location.search.includes("own")) void ${PROBE_FETCH}(${JSON.stringify(STREAM_URL)}, { headers: { Accept: "text/event-stream" } });
void ${PROBE_FETCH}(${JSON.stringify(SESSION)}).then(() => { document.title = "settled"; });
</script>`;

// Один документ, адрес кадра сменён ВПЛОТНУЮ к выпуску: до него (`order=after` — после него).
const ADDRESS_CHANGE_PAGE = `<!doctype html><button id="go">go</button><script>
document.getElementById("go").onclick = () => {
  const read = () => void ${PROBE_FETCH}(${JSON.stringify(`${READ}?from=spa`)}).then(() => { document.title = "read"; });
  if (location.search.includes("after")) { history.pushState({}, "", "/verification"); read(); }
  else { read(); history.pushState({}, "", "/verification"); }
};
</script>`;

// Сводка: поток открыт; уход на список — и чтение списка выпускается за началом перехода.
const DASH_PAGE = `<!doctype html><button id="go">go</button><script>
document.getElementById("go").onclick = () => {
  window.location.assign("/list?visit=1");
  void ${PROBE_FETCH}(${JSON.stringify(`${READ}?from=dash`)});
};
document.title = "ready";
</script>`;

// Список, читающий сети при открытии: заголовок называет код ответа.
const READING_LIST_PAGE = `<!doctype html><script>
void ${PROBE_FETCH}(${JSON.stringify(READ)} + "?from=list&" + location.search.slice(1)).then((r) => { document.title = "read " + r.status; });
</script>`;

async function withServer<T>(body: (origin: string) => Promise<T>): Promise<T> {
  const server = http.createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://loop");
    if (url.pathname === STREAM) {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" });
      res.write('event: opened\ndata: {"opened":{}}\n\n');
      return;
    }
    if (url.pathname === SESSION || url.pathname === READ) {
      res.writeHead(200, { "content-type": "application/json" });
      res.end("{}");
      return;
    }
    res.writeHead(200, { "content-type": "text/html" });
    if (url.pathname === "/verification") res.end(SCREEN_PAGE);
    else if (url.pathname === "/dash") res.end(DASH_PAGE);
    else if (url.pathname === "/spa") res.end(ADDRESS_CHANGE_PAGE);
    else if (url.pathname === "/list" && url.searchParams.has("visit")) res.end(READING_LIST_PAGE);
    else res.end(LIST_PAGE);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    return await body(`http://127.0.0.1:${(server.address() as AddressInfo).port}`);
  } finally {
    // Поток держит соединение открытым: без обрыва закрытие сервера ждало бы его вечно.
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}

/**
 * Перехват прогонщика включён, как у F6b-25 до правки: при нём событие `request`
 * рождается из паузы перехвата, и слушатель получает его позже, чем браузер
 * принял следующий документ. Запись о выпуске от этого не зависит — и держатель
 * судит её именно в этом условии.
 */
async function interceptedByRunner(page: Page): Promise<void> {
  await page.route(
    (url) => url.pathname === "/never",
    (route) => route.fallback(),
  );
}

/** Документы обращений к `path` — в порядке записи браузера. */
function documentsOf(documents: IssuingDocuments, path: string): string[] {
  return documents.calls.filter((c) => c.path === path).map((c) => c.document);
}

test("F6b · адрес кадра, сменённый вплотную за выпуском, документа обращения не меняет", async ({ page }) => {
  // verifies #2922 — адрес кадра, прочитанный в слушателе, при перехвате прогонщика уже сменён (замер: 19 из 20).
  await withServer(async (origin) => {
    const documents = await issuingDocuments(page);
    await interceptedByRunner(page);
    await page.goto(`${origin}/spa`);
    await page.click("#go");
    await expect(page, "чтение не получило ответа").toHaveTitle("read");
    expect(documentsOf(documents, READ), "чтение, выпущенное до смены адреса, записано не за прежним адресом").toEqual([
      "/spa",
    ]);
  });
});

test("F6b · то же чтение, выпущенное после смены адреса, — нового адреса", async ({ page }) => {
  // verifies #2922 — близнец: помощник, записывающий за адресом загрузки документа, здесь краснеет.
  await withServer(async (origin) => {
    const documents = await issuingDocuments(page);
    await interceptedByRunner(page);
    await page.goto(`${origin}/spa?order=after`);
    await page.click("#go");
    await expect(page, "чтение не получило ответа").toHaveTitle("read");
    expect(documentsOf(documents, READ), "чтение, выпущенное после смены адреса, записано не за новым").toEqual([
      "/verification",
    ]);
  });
});

test("F6b · обращение, выпущенное уходящим документом в момент ухода, — его, а не принятого экрана", async ({
  page,
}) => {
  // verifies #2922 — F6b-25 записал разбор отказа потока уходящего списка за экраном подтверждения.
  await withServer(async (origin) => {
    const documents = await issuingDocuments(page);
    await interceptedByRunner(page);
    await page.goto(`${origin}/list`);
    await expect(page, "поток списка не открылся").toHaveTitle("opened");
    await page.click("#go");
    await expect(page, "экран не выпустил своего обращения").toHaveTitle("settled");

    expect(
      documentsOf(documents, STREAM),
      "записей обращений потока не две — поток списка и его разбор при уходе: либо запись потеряла обращение " +
        "уходящего документа, либо браузер больше не роняет поток уходом, и тогда у пробы нет предмета",
    ).toHaveLength(2);
    expect(documentsOf(documents, STREAM), "обращение потока уходящего списка записано не за ним").toEqual([
      "/list",
      "/list",
    ]);
    expect(documentsOf(documents, SESSION), "обращение экрана записано не за ним").toEqual(["/verification"]);
  });
});

test("F6b · то же обращение, выпущенное принятым экраном, — экрана", async ({ page }) => {
  // verifies #2922 — близнец: помощник, записывающий всё за уходящим документом, здесь краснеет.
  await withServer(async (origin) => {
    const documents = await issuingDocuments(page);
    await interceptedByRunner(page);
    await page.goto(`${origin}/list?own=1`);
    await expect(page, "поток списка не открылся").toHaveTitle("opened");
    await page.click("#go");
    await expect(page, "экран не выпустил своего обращения").toHaveTitle("settled");

    const stream = documentsOf(documents, STREAM);
    expect(stream.filter((d) => d === "/verification"), "обращение потока, выпущенное экраном, записано не за ним").toEqual([
      "/verification",
    ]);
    expect(stream.filter((d) => d !== "/verification"), "обращения потока списка записаны не за ним").toEqual([
      "/list",
      "/list",
    ]);
  });
});

test("F6b · подмена берёт чтения первого документа списка — не уходящей сводки и не списка, открытого снова", async ({
  page,
}) => {
  // verifies #2922 — у F6b-25 подменялись два чтения: уходящей сводки и списка.
  await withServer(async (origin) => {
    const documents = await issuingDocuments(page);
    const reads = await substituteDocumentReads(documents, {
      path: READ,
      document: "/list",
      answer: { status: 403, headers: { "content-type": "application/json" }, body: Buffer.from(REFUSAL) },
    });
    await page.goto(`${origin}/dash`);
    await expect(page, "сводка не открылась").toHaveTitle("ready");
    await page.click("#go");
    await expect(page, "чтение первого документа списка не подменено").toHaveTitle("read 403");
    await page.goto(`${origin}/list?visit=2`);
    await expect(page, "чтение списка, открытого снова, подменено").toHaveTitle("read 200");

    const issued = documents.calls.filter((c) => c.path === READ).map((c) => `${c.document} ${new URL(c.url).search}`);
    expect(issued, "предпосылка: сводка выпустила чтение за началом перехода, список — по чтению на документ").toEqual([
      "/dash ?from=dash",
      "/list ?from=list&visit=1",
      "/list ?from=list&visit=2",
    ]);
    expect(
      reads.substituted.map((u) => new URL(u).search),
      `подменено не только чтение первого документа списка; мимо подмены: ${reads.otherDocuments.join(" | ") || "нет"}`,
    ).toEqual(["?from=list&visit=1"]);
    expect(reads.unattributed, "чтения без записи о выпуске").toEqual([]);
  });
});
