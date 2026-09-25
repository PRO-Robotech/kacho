// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Упорядочивающий транспорт вкладки — ЕДИНСТВЕННОЕ место, откуда консоль
// выпускает обращения к краю (приёмка F8, Р10).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ
//
// Восемь глаголов полосы ставят носитель сессии, и пять из них делают прежний
// носитель негодным (смена пароля, подтверждение и снятие второго фактора,
// перечеканка запасных кодов, повышение уровня). Край на носитель, не нашедший
// записи, отвечает «сессия кончилась» и гасит печенье ОТВЕТОМ. Если такой ответ
// приходит браузеру после ответа глагола, гаснет уже НОВЫЙ носитель, и человек
// остаётся без сессии, которую только что обновил. Хватает одного обращения
// вкладки, выпущенного до глагола и дошедшего до края после него.
//
// Край и служба этого не лечат и лечить не вправе: окно годности прежнего
// носителя запрещено (Ф3-19), и носитель, не нашедший записи, край гасит по
// любой причине (Ф3-10). Повтор «с текущим носителем» невыполним: после такого
// ответа носителя у браузера нет вовсе. Поэтому упорядочивает консоль — в
// границах своей вкладки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРАВИЛО — ТРИ ПУНКТА, ЕДИНИЦА — СЕТЕВОЕ ОБРАЩЕНИЕ
//
// Единица — одно сетевое обращение (один `fetch`, поток подписки), а не
// логический вызов клиента. Исход обращения — ответ (заголовки: в этот момент
// браузер применяет печенья ответа), отказ сети или отмена.
//
//   1. ДО ВЫПУСКА глагола у каждого обращения вкладки, выпущенного раньше, есть
//      исход. Чтение в полёте отменяется и после исхода глагола выпускается
//      снова. Мутации в полёте глагол дожидается: её исход нужен человеку, а
//      повтор мутации — второе действие. Открытый поток изменений закрывается и
//      после исхода глагола открывается снова;
//   2. МЕЖДУ ВЫПУСКОМ глагола и его исходом вкладка не выпускает ничего — новые
//      обращения ждут исхода;
//   3. ИСХОД — ЛЮБОЙ: ответ, отказ, «ответа нет». После него отменённое
//      выпускается снова с тем носителем, который есть у браузера.
//
// Чтение отменяется, а не дожидается: зависшее чтение держало бы глагол до
// отказа посредника, а свой срок у консоли — это стенные часы.
//
// ПОВЫШЕНИЕ ИЗ ТРАНСПОРТА — отсюда же, а не особым случаем. Вызов, получивший от
// края требование повышения, ждёт окна повышения внутри себя, но его СЕТЕВОЕ
// обращение к этому моменту уже имеет исход — отказ края. Повышение его не
// ждёт; повтор вызова — новое обращение и выпускается после исхода повышения.
//
// СВОЕГО СРОКА у глагола нет, и это решение. Глагол, брошенный по сроку
// консоли, у службы не отменён: исполни она его, новый носитель пришёл бы в
// ответе, который браузер после отмены не примет. Исход ограничивает край.
//
// ГРАНИЦА — ВКЛАДКА. Обращений другой вкладки этот транспорт не видит; исход для
// них безопасен — носитель погашен, человек входит заново.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ НА `globalThis`
//
// `@shared` собирается в каждый модуль консоли своей копией. Смену пароля ведёт
// экран каркаса, а чтение в полёте может принадлежать клиенту модуля в той же
// вкладке; упорядочение одно на вкладку, поэтому состояние лежит под ключом
// глобального реестра символов — он общий у всех копий в одной вкладке.
//
// ЧТО ДЕРЖИТ ЭТО ПРАВИЛО. Порядок — модульная проба `carrier-order.test.ts`
// (по каждому глаголу, который консоль зовёт); то, что через этот транспорт идёт
// КАЖДОЕ место выпуска консоли, — перепись мест выпуска
// (`test/console-issuance-ordered.test.ts`); исход у страницы — браузерная пара
// F8-46 и F8-47 (`e2e/specs/account-settings.spec.ts`).

const KEY = Symbol.for("kacho.console.carrier-order");

type FlightKind = "read" | "mutation";

/** Обращение в полёте: чем его отменить и когда у него есть исход. */
interface Flight {
  kind: FlightKind;
  cancel(): void;
  /** Исполняется, когда у обращения есть исход — любой. */
  settled: Promise<void>;
}

/**
 * Поток изменений, который глагол закрывает до выпуска и открывает после исхода.
 * `suspend` закрывает соединение СЕЙЧАС; `resume` открывает снова — с тем
 * носителем, который есть у браузера. Приёмник строит клиент потока
 * (`lib/subscription/hub.ts` — дом у него один, #1021), и он же ставит поток на
 * учёт и не открывает его, пока идёт глагол (`carrierVerbPending`).
 */
export interface OrderedStream {
  suspend(): void;
  resume(): void;
}

interface Order {
  /** Исход глагола, ставящего носитель, которого ждёт вкладка; `null` — глагола нет. */
  verb: Promise<void> | null;
  flights: Set<Flight>;
  streams: Set<OrderedStream>;
}

function order(): Order {
  const g = globalThis as unknown as Record<symbol, Order | undefined>;
  let o = g[KEY];
  if (!o) {
    o = { verb: null, flights: new Set(), streams: new Set() };
    g[KEY] = o;
  }
  return o;
}

/** Методы чтения: обращение без действия, его можно отменить и выпустить снова. */
const READ_METHODS: ReadonlySet<string> = new Set(["GET", "HEAD"]);

/** Отмена упорядочением — отличима от отмены вызывающим и от отказа сети. */
class CancelledByOrder extends Error {
  constructor() {
    super("обращение отменено упорядочением вокруг глагола, ставящего носитель");
    this.name = "AbortError";
  }
}

/** Дождаться исхода идущего глагола — и следующего, если он встал следом. */
async function afterVerbs(o: Order): Promise<void> {
  while (o.verb) await o.verb;
}

/** Поставить обращение на учёт: у упорядочения есть чем его отменить и чего ждать. */
function inFlight(o: Order, kind: FlightKind, answer: Promise<Response>, abort: () => void): Promise<Response> {
  // Исход, уже полученный, не отменяется: ответ пришёл, и отмена сорвала бы
  // чтение его тела, а не обращение. Отметка о полученном исходе — ПЕРВАЯ
  // реакция на ответ транспорта: раньше неё ни один код вкладки ответа не видит.
  let finished = false;
  const finish = () => {
    finished = true;
    o.flights.delete(flight);
  };
  answer.then(finish, finish);
  let cancelled: (e: unknown) => void = () => undefined;
  const cancelledByOrder = new Promise<never>((_, reject) => {
    cancelled = reject;
  });
  // Исход — ответ транспорта ЛИБО отмена упорядочением, что раньше. Транспорт,
  // не исполняющий сигнал отмены, не держит глагол: исход у обращения есть в
  // момент отмены, а ответ, пришедший позже, никому не отдаётся.
  const outcome = Promise.race([answer, cancelledByOrder]);
  const flight: Flight = {
    kind,
    cancel: () => {
      if (finished) return;
      finished = true;
      o.flights.delete(flight);
      abort();
      cancelled(new CancelledByOrder());
    },
    settled: outcome.then(
      () => undefined,
      () => undefined,
    ),
  };
  o.flights.add(flight);
  return outcome;
}

async function issueRead(o: Order, url: string, init: RequestInit): Promise<Response> {
  for (;;) {
    if (o.verb) await afterVerbs(o);
    const controller = new AbortController();
    const caller = init.signal ?? null;
    const byCaller = () => controller.abort(caller?.reason);
    if (caller?.aborted) byCaller();
    else caller?.addEventListener("abort", byCaller, { once: true });
    let byOrder = false;
    try {
      return await inFlight(o, "read", globalThis.fetch(url, { ...init, signal: controller.signal }), () => {
        byOrder = true;
        controller.abort();
      });
    } catch (e) {
      // Отменённое упорядочением выпускается снова после исхода глагола (п. 3);
      // отменённое вызывающим — его решение, и повтора нет.
      if (byOrder && !caller?.aborted) continue;
      throw e;
    } finally {
      caller?.removeEventListener("abort", byCaller);
    }
  }
}

async function issueMutation(o: Order, url: string, init: RequestInit): Promise<Response> {
  if (o.verb) await afterVerbs(o);
  // Мутация не отменяется: её дожидаются (п. 1).
  return inFlight(o, "mutation", globalThis.fetch(url, init), () => undefined);
}

async function issueCarrierVerb(o: Order, url: string, init: RequestInit): Promise<Response> {
  if (o.verb) await afterVerbs(o);
  let release: () => void = () => undefined;
  o.verb = new Promise<void>((resolve) => {
    release = resolve;
  });
  try {
    const earlier = [...o.flights];
    for (const flight of earlier) if (flight.kind === "read") flight.cancel();
    for (const stream of [...o.streams]) stream.suspend();
    await Promise.all(earlier.map((flight) => flight.settled));
    return await globalThis.fetch(url, init);
  } finally {
    o.verb = null;
    release();
    // Открываются ВСЕ потоки вкладки, а не только закрытые: поток, которому
    // открыться выпало на время глагола, ждал этого исхода (п. 2).
    for (const stream of [...o.streams]) stream.resume();
  }
}

/** Чем обращение отличается для упорядочения. */
export interface OrderedOptions {
  /**
   * Обращение — глагол, ставящий носитель. Перед его выпуском чтения в полёте
   * отменены, мутации в полёте дождались исхода, потоки закрыты; между выпуском
   * и исходом вкладка не выпускает ничего; исход любой — ответ, отказ, «ответа
   * нет» — отпускает удержанное (Р10 пп. 1–3).
   */
  setsCarrier?: boolean;
}

/**
 * Транспорт вкладки: у него та же форма, что у `fetch`, и через него консоль
 * выпускает КАЖДОЕ обращение к краю.
 *
 * Чтение (`GET`, `HEAD`) глагол, ставящий носитель, отменяет и выпускает снова:
 * вызывающий получает ответ повторного выпуска и отмены не видит. Всё прочее —
 * мутация: её глагол дожидается. Пока идёт глагол, обращение ждёт его исхода.
 */
export const orderedTransport = {
  fetch(url: string, init: RequestInit = {}, opts: OrderedOptions = {}): Promise<Response> {
    const o = order();
    if (opts.setsCarrier) return issueCarrierVerb(o, url, init);
    const method = (init.method ?? "GET").toUpperCase();
    return READ_METHODS.has(method) ? issueRead(o, url, init) : issueMutation(o, url, init);
  },
};

/** Идёт ли сейчас глагол, ставящий носитель: новые потоки ждут его исхода (п. 2). */
export function carrierVerbPending(): boolean {
  return order().verb !== null;
}

/** Поставить поток на учёт упорядочения. Возвращает снятие с учёта. */
export function registerOrderedStream(stream: OrderedStream): () => void {
  const o = order();
  o.streams.add(stream);
  return () => {
    o.streams.delete(stream);
  };
}
