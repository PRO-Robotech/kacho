// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type CDPSession, type Page } from "@playwright/test";

/**
 * Документ, выпустивший обращение, — по записи БРАУЗЕРА о выпуске, а не по
 * адресу кадра в тот момент, когда событие дошло до слушателя.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ (kacho#2922, прогон 36889716883, F6b-25)
 *
 * Прежний помощник брал документ обращения как путь `request.frame().url()` в
 * слушателе события `request`. Адрес кадра меняется, когда новый документ
 * принят, — и прочитанный в слушателе, он отвечает не «кто выпустил», а «какой
 * документ стоял в кадре, когда событие ДОШЛО». Между выпуском и приходом
 * события стоит перехват прогонщика: при включённой подмене (`page.route`)
 * событие `request` рождается из паузы перехвата, а пауза приходит от сетевой
 * службы браузера, позже, чем принятие нового документа. Так проба F6b-25
 * записала за экраном подтверждения разбор отказа потока, который выпустил
 * уходящий документ списка: обращение стоит в трассе ДО первого сценария
 * экрана, а модуль, открывающий поток владельца `vpc`, экран не загружал вовсе.
 *
 * Запись о выпуске ведёт сам браузер: `Network.requestWillBeSent` несёт
 * `documentURL` — адрес документа, для которого обращение загружается, — и
 * `loaderId`, загрузчик этого документа. Оба значения ставятся в момент выпуска
 * и от того, когда событие дошло, не зависят. Один документ — один загрузчик:
 * документ, открытый по тому же адресу повторно, — другой документ, и
 * различается он загрузчиком, а не адресом.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЗДЕСЬ НЕ ВИДНО — названо, чтобы «ноль» не читался шире сказанного
 *
 * Обращения без документа (воркеры: `loaderId` пуст) и переходы документа сами
 * в перепись не входят. Обращения контекста запросов (`page.request`) событий
 * сети страницы не порождают вовсе. Перепись видит обращения, выпущенные ПОСЛЕ
 * того, как помощник позван.
 *
 * Держатель — `issuing-document.spec.ts`: уходящий документ, выпустивший
 * обращение в момент ухода, и его близнец — то же обращение, выпущенное новым.
 */
export interface IssuedCall {
  /** Номер обращения у браузера. */
  requestId: string;
  /** Загрузчик документа, выпустившего обращение: один документ — один загрузчик. */
  loaderId: string;
  /** Кадр документа. */
  frameId: string;
  /** Путь адреса документа в момент выпуска. */
  document: string;
  method: string;
  /** Путь адреса обращения. */
  path: string;
  url: string;
}

export interface IssuingDocuments {
  /** Обращения документов страницы (без переходов документа) — в порядке записи браузера. */
  readonly calls: readonly IssuedCall[];
  /** Запись выпуска обращения по его номеру у браузера; `undefined` — записи пока нет. */
  recordOf(requestId: string): IssuedCall | undefined;
  /** Сеанс протокола, которым запись ведётся: перехват этого же сеанса судит по ней. */
  readonly session: CDPSession;
}

/** Начать запись документов обращений страницы — до того, как они будут выпущены. */
export async function issuingDocuments(page: Page): Promise<IssuingDocuments> {
  const session = await page.context().newCDPSession(page);
  const calls: IssuedCall[] = [];
  const byRequest = new Map<string, IssuedCall>();
  session.on("Network.requestWillBeSent", (e) => {
    // Шаг перенаправления несёт тот же номер: выпуск записан первым шагом.
    if (e.redirectResponse !== undefined || byRequest.has(e.requestId)) return;
    if (e.type === "Document" || e.loaderId === "") return;
    let document: string;
    let path: string;
    try {
      document = new URL(e.documentURL).pathname;
      path = new URL(e.request.url).pathname;
    } catch {
      return;
    }
    const call: IssuedCall = {
      requestId: e.requestId,
      loaderId: e.loaderId,
      frameId: e.frameId ?? "",
      document,
      method: e.request.method,
      path,
      url: e.request.url,
    };
    calls.push(call);
    byRequest.set(e.requestId, call);
  });
  await session.send("Network.enable");
  return { calls, recordOf: (requestId) => byRequest.get(requestId), session };
}

/** Ответ, которым подменяется чтение: статус, заголовки и байты тела. */
export interface SubstituteAnswer {
  status: number;
  headers: Record<string, string>;
  body: Buffer;
}

export interface DocumentReads {
  /** Подменённые чтения — адреса. */
  substituted: string[];
  /** Чтения других документов, отпущенные к краю, пока подменяемый документ не ушёл. */
  otherDocuments: string[];
  /** Чтения, о выпуске которых браузер записи не дал: решать по ним было не по чему. */
  unattributed: string[];
}

/** Сколько ждать записи о выпуске перехваченного чтения: событие идёт другим путём браузера. */
const RECORD_WAIT_MS = 10_000;

/**
 * Подменить ответом `answer` чтения `GET path`, которые выпустит ПЕРВЫЙ документ
 * с адресом `document`, — и только его, до его ухода.
 *
 * Документ чтения — запись браузера о выпуске (`issuingDocuments`), а не адрес
 * кадра в момент паузы: чтение уходящего документа, выпущенное после начала
 * перехода, остаётся его чтением и идёт к краю. Условие несущее: каркас,
 * оставленный на сводке проекта, сам читает сети, и без него его чтение
 * подменялось бы наравне с чтением списка (kacho#2922: у F6b-25 были подменены
 * два чтения — уходящей сводки и списка). Подменяемый документ —
 * загрузчик первого подменённого чтения; документ, открытый по тому же адресу
 * позже, — другой загрузчик, и его чтения идут к краю. Уход подменяемого
 * документа — переход его кадра; после него подмены нет.
 *
 * Перехват — этим же сеансом протокола (`Fetch.requestPaused.networkId` равен
 * номеру записи о выпуске), а не `page.route`: обращение, перехваченное
 * прогонщиком, доходит до слушателя событий только из паузы перехвата, и
 * запись о нём можно было бы сопоставить только по адресу.
 */
export async function substituteDocumentReads(
  documents: IssuingDocuments,
  read: { path: string; document: string; answer: SubstituteAnswer },
): Promise<DocumentReads> {
  const { session } = documents;
  const reads: DocumentReads = { substituted: [], otherDocuments: [], unattributed: [] };
  let owner: { loaderId: string; frameId: string } | null = null;
  let left = false;
  session.on("Network.requestWillBeSent", (e) => {
    if (owner && e.type === "Document" && e.frameId === owner.frameId) left = true;
  });
  const responseHeaders = Object.entries(read.answer.headers).map(([name, value]) => ({ name, value }));
  const body = read.answer.body.toString("base64");

  const decide = async (e: {
    requestId: string;
    networkId?: string;
    request: { url: string; method: string };
  }): Promise<void> => {
    const pass = async (): Promise<void> => {
      // Отпускается и обращение документа, которого уже нет: тогда отпускать некого.
      await session.send("Fetch.continueRequest", { requestId: e.requestId }).catch(() => undefined);
    };
    if (new URL(e.request.url).pathname !== read.path || e.request.method !== "GET" || left) return pass();
    const networkId = e.networkId;
    let record = networkId === undefined ? undefined : documents.recordOf(networkId);
    if (record === undefined && networkId !== undefined) {
      record = await expect
        .poll(() => documents.recordOf(networkId) !== undefined, { timeout: RECORD_WAIT_MS })
        .toBe(true)
        .then(
          () => documents.recordOf(networkId),
          () => undefined,
        );
    }
    if (record === undefined) {
      reads.unattributed.push(e.request.url);
      return pass();
    }
    const mine = owner === null ? record.document === read.document : record.loaderId === owner.loaderId;
    if (!mine || left) {
      reads.otherDocuments.push(`${record.document} ${e.request.url}`);
      return pass();
    }
    owner ??= { loaderId: record.loaderId, frameId: record.frameId };
    reads.substituted.push(e.request.url);
    await session
      .send("Fetch.fulfillRequest", { requestId: e.requestId, responseCode: read.answer.status, responseHeaders, body })
      .catch(() => undefined);
  };
  session.on("Fetch.requestPaused", (e) => {
    void decide(e);
  });
  await session.send("Fetch.enable", { patterns: [{ urlPattern: `*${read.path}*`, requestStage: "Request" }] });
  return reads;
}

/** Ответ обращения, выпущенного документом: статус и байты тела — снятые браузером. */
export interface DocumentAnswer {
  status: number;
  body: string;
}

/**
 * Ответ обращения `method path`, которое выпустит СЛЕДУЮЩИЙ документ главного
 * кадра, — первое такое его обращение.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ (kacho#3238, прогон на t3238-sc @63500f861, F8-17)
 *
 * Ожидание ответа, поставленное перед `page.reload()` (`page.waitForResponse`),
 * ловит первый подходящий ответ ЛЮБОГО документа — и обращение о сессии
 * (`/iam/v1/auth/me`) выпускает каждый документ каркаса. Уходящая панель
 * выпустила своё за 32 мс до перезагрузки, ожидание отдало его, а тело
 * уходящего документа браузер снял вместе с ним: `Network.getResponseBody` —
 * «No resource with given identifier found». Проба читала не тот документ, о
 * котором утверждала, и падала тем, как быстро уходит панель.
 *
 * Документ — загрузчик (`loaderId`) записи браузера о выпуске, а не момент
 * прихода события: следующий документ — загрузчик первого перехода главного
 * кадра после вызова. Тело снимается по окончании загрузки обращения — документ,
 * выпустивший его, к этому моменту жив: он и есть следующий.
 *
 * Слушатели ставятся ДО перехода: помощник зовётся и дожидается до
 * `page.reload()` / `page.goto()`, его ответ читается после.
 */
export async function answerOfNextDocument(
  documents: IssuingDocuments,
  read: { method: string; path: string },
): Promise<{ answer: Promise<DocumentAnswer> }> {
  const { session } = documents;
  const { frameTree } = await session.send("Page.getFrameTree");
  const mainFrameId = frameTree.frame.id;
  let loader: string | null = null;
  let target: string | null = null;
  let status: number | null = null;
  const answer = new Promise<DocumentAnswer>((resolve, reject) => {
    session.on("Network.requestWillBeSent", (e) => {
      if (loader === null) {
        if (e.type === "Document" && e.frameId === mainFrameId) loader = e.loaderId;
        return;
      }
      if (target !== null || e.loaderId !== loader || e.request.method !== read.method) return;
      let path: string;
      try {
        path = new URL(e.request.url).pathname;
      } catch {
        return;
      }
      if (path === read.path) target = e.requestId;
    });
    session.on("Network.responseReceived", (e) => {
      if (e.requestId === target) status = e.response.status;
    });
    session.on("Network.loadingFinished", (e) => {
      if (e.requestId !== target) return;
      session
        .send("Network.getResponseBody", { requestId: e.requestId })
        .then((got) => {
          resolve({
            status: status ?? 0,
            body: got.base64Encoded ? Buffer.from(got.body, "base64").toString("utf8") : got.body,
          });
        })
        .catch((err: unknown) => {
          reject(
            new Error(
              `${read.method} ${read.path} следующего документа: тело не снято — ${err instanceof Error ? err.message : String(err)}`,
            ),
          );
        });
    });
    session.on("Network.loadingFailed", (e) => {
      if (e.requestId === target) {
        reject(new Error(`${read.method} ${read.path} следующего документа: ответа нет — ${e.errorText}`));
      }
    });
  });
  // Ответ читает зовущий; отказ до чтения не должен всплыть неперехваченным.
  answer.catch(() => undefined);
  return { answer };
}
