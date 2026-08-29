// Единственный клиент потока изменений в консоли.
//
// ---------------------------------------------------------------------------
// ПОЧЕМУ ОДИН МЕХАНИЗМ НА ВСЮ КОНСОЛЬ, А НЕ ПО КЛИЕНТУ НА МОДУЛЬ
//
// Консоль форкнута по девяти микрофронтендам, и соблазн завести подписку в
// каждом велик: это дало бы девять копий одного кода, которые разошлись бы
// молча — класс, уже измеренный в этом дереве (`ui.md` §«Незакрытый форк»: из
// ста парных файлов разошлась четверть). Признак нарушения назван прямо в
// задаче #1021: приёмник событий браузера встречается больше чем в одном месте
// дерева консоли.
//
// ---------------------------------------------------------------------------
// ЧТО ЭТОТ ХАБ РЕШАЕТ
//
// 1. ОДИН поток на пару «владелец × проект». Потолок потоков на вызывающего —
//    восемь (объявлен посадкой края), а страница показывает несколько списков
//    одного домена сразу: поток на список исчерпал бы потолок на второй-третьей
//    вкладке, и остальные получили бы отказ.
// 2. ПОКРЫТИЕ ЧИТАЕТСЯ ПО ПРОВОДУ. Опрос снимается только там, где владелец САМ
//    назвал вид в своём словаре (`knownKinds` служебного сообщения открытия).
//    Решение по выписанной таблице разошлось бы со словарём владельца молча — и
//    разошлось бы в сторону, где список замирает навсегда.
// 3. ОТКАЗ ВОЗВРАЩАЕТ ОПРОС. Поток закрылся — покрытие снимается, и опрос,
//    который его сменил, включается обратно. Иначе список замер бы тихо: ни
//    ошибки, ни пустого ответа, просто ничего не меняется.
//
// ---------------------------------------------------------------------------
// ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО
//
// ВОЗОБНОВЛЕНИЯ. Позиция едет полем `id:` кадра, приёмник событий браузера
// запоминает её сам и шлёт заголовком `Last-Event-ID` при переподключении —
// ради этого свойства край и выбрал SSE. Свой носитель позиции здесь был бы
// ВТОРЫМ носителем одного значения, и разошлись бы они на первом же обрыве.
//
// ОСИ `kinds`. Словарь принадлежит владельцу; назови мы вид, которого он не
// знает, — поток отвергается целиком `400`, и замолчали бы ВСЕ списки домена, а
// не один. Незаданная ось не сужает ничем, зато первый же кадр приносит словарь
// целиком.
//
// ПРИМЕНЕНИЯ СОСТОЯНИЯ ИЗ СОБЫТИЯ. Хаб раздаёт событие как ФАКТ ИЗМЕНЕНИЯ, а не
// как новое состояние строки: состояние несут не все владельцы (у
// балансировщика его нет ни у одного вида) и не все рода изменения (у снятия —
// ни у кого). Разбор — в `use-resource-stream.ts`.

/** Адрес единственной проекции потока. Тот же, что объявлен краем. */
export const STREAM_PATH = "/subscription/v1/events";

/**
 * Приёмник событий в том объёме, каким пользуется хаб.
 *
 * Объявлен здесь, а не взят из библиотеки браузера, ровно ради проб: настоящий
 * приёмник открывает соединение в конструкторе, и подставить его нечем.
 */
export interface EventSourceLike {
  readyState: number;
  addEventListener(name: string, fn: (ev: MessageEvent<string>) => void): void;
  close(): void;
  onerror: ((ev: Event) => void) | null;
}

/** Событие потока в том виде, в каком его читает страница. */
export interface StreamEvent {
  position: string;
  kind: string;
  resourceId: string;
  projectId: string;
  /**
   * Род изменения. Владелец шлёт `CREATED`, `UPDATED`, `DELETED`.
   *
   * Тип — `string`, и перечня здесь нет намеренно. Словарь родов принадлежит
   * владельцу и пополняется им; кадр приезжает `JSON.parse`-ом без проверки,
   * поэтому перечень был бы УТВЕРЖДЕНИЕМ о проводе, а не проверенным фактом, —
   * и читатель, ему поверивший, написал бы исчерпывающий разбор, молча
   * пропускающий новый род.
   *
   * Прежняя запись `"CREATED" | "UPDATED" | "DELETED" | string` не давала даже
   * этого: строка поглощает литералы, компилятор видел ровно `string`, а
   * человек читал закрытый перечень. Два прочтения одного объявления, из
   * которых верно одно.
   *
   * Потребителя у рода здесь нет ни одного: хаб раздаёт событие фактом
   * изменения и род не различает (см. шапку модуля).
   */
  change: string;
  /** Состояние предмета, если владелец его отдаёт. Читается как ПОЛНОЕ. */
  state?: Record<string, unknown>;
  /** Приходит ВМЕСТО состояния. Пустого объекта вместо состояния не бывает. */
  stateUnavailable?: { reason?: string };
}

export interface StreamTarget {
  owner: string;
  kind: string;
  projectId: string | null;
}

export interface HubDeps {
  open: (url: string) => EventSourceLike;
  /**
   * Разбор отказа — ОДНИМ запросом и только после отказа потока.
   *
   * Приёмник событий браузера кода ответа не отдаёт вовсе: он умеет «открылось»
   * и «сбой». Значит «владелец не объявлен посадкой» (состояние поставки, `501`)
   * и «край недоступен» (дефект) выглядят из страницы ОДИНАКОВО — тишиной. Без
   * этого запроса мягкий возврат к опросу не отличал бы настройку от сбоя, а
   * такой контроль остаётся включённым навсегда и молча.
   */
  diagnose: (url: string) => Promise<{ status: number; contentType: string; body: string }>;
  log: (message: string, detail?: unknown) => void;
  /** Часы. Вход, а не обращение к глобальным: иначе окно молчания непроверяемо. */
  now?: () => number;
  /**
   * Сколько молчать после отказа, прежде чем пробовать снова.
   *
   * Молчание нужно: отказ до заголовков (`501` без объявленного владельца,
   * `403`, `429`) повтором не лечится, а страница перерисовывается часто —
   * без окна край получал бы попытку открытия на каждый переход между списками
   * одного домена.
   *
   * Окно КОНЕЧНО, и это важнее его величины: владельца объявляют посадкой
   * (kacho#1388), и бессрочная память об отказе означала бы, что включённая
   * возможность не подхватывается до перезагрузки вкладки. Послабление обязано
   * истекать само.
   */
  reopenAfterMs?: number;
  /**
   * Есть ли в этой среде приёмник событий ВООБЩЕ.
   *
   * Это не «край отказал», а «принимать нечем»: среда без `EventSource`
   * (старый обозреватель, харнесс проб) не отличается от отказа края ничем,
   * кроме того, что повтор ей не поможет НИКОГДА. Без этого входа хаб звал бы
   * конструктор, которого нет, и страница падала бы целиком — вместо того
   * чтобы остаться на опросе, ради чего мягкий возврат и заведён.
   *
   * Вход, а не обращение к глобальным: зависимости подставляются ЦЕЛИКОМ
   * (см. конструктор), поэтому проба со своим приёмником не наследует эту
   * проверку и не обязана её объявлять — умолчание `true` относится к тому,
   * кто приёмник назвал сам.
   */
  available?: () => boolean;
}

interface Channel {
  source: EventSourceLike | null;
  knownKinds: Set<string>;
  /** Открыт ли поток: служебное сообщение открытия получено и не отозвано. */
  open: boolean;
  subscribers: Map<number, { kind: string; handler: (e: StreamEvent) => void }>;
}

const defaultDeps: HubDeps = {
  // Утверждения типа здесь нет: приёмник браузера объявленному подмножеству
  // отвечает как есть, и компилятор это видит сам. Стоявшее прежде
  // `as unknown as EventSourceLike` не сужало и не расширяло ничего — зато
  // сняло бы проверку, начни объявление и браузер расходиться.
  open: (url) => new EventSource(url, { withCredentials: true }),
  diagnose: async (url) => {
    const res = await fetch(url, { headers: { Accept: "text/event-stream" }, credentials: "same-origin" });
    const body = await res.text();
    return { status: res.status, contentType: res.headers.get("content-type") ?? "", body: body.slice(0, 300) };
  },
  log: (message, detail) => console.info(`[подписка] ${message}`, detail ?? ""),
  available: () => typeof EventSource !== "undefined",
};

/** Умолчание окна молчания после отказа. */
const REOPEN_AFTER_MS = 60_000;

/** Приёмник, у которого соединения больше нет и повтора не будет. */
const SOURCE_CLOSED = 2;

export class SubscriptionHub {
  private channels = new Map<string, Channel>();
  /**
   * Когда поток этой пары отказал в последний раз.
   *
   * Живёт ОТДЕЛЬНО от канала намеренно: канал снимается вместе с последним
   * подписчиком, и память об отказе, лежи она внутри, исчезала бы вместе с ним.
   * Тогда переход между двумя списками одного домена открывал бы поток заново
   * на каждой странице — по разу на переход, и все с одним и тем же отказом.
   * Проба на это стоит рядом (`hub.test.ts`) и нашла ровно этот дефект.
   */
  private failedAt = new Map<string, number>();
  private watchers = new Set<() => void>();
  private nextId = 1;

  constructor(private readonly deps: HubDeps = defaultDeps) {}

  /** Ключ канала: поток открывается к ОДНОМУ владельцу и одному проекту. */
  private static key(owner: string, projectId: string | null): string {
    return `${owner} ${projectId ?? ""}`;
  }

  private url(owner: string, projectId: string | null): string {
    const params = new URLSearchParams({ owner });
    if (projectId) params.set("projectId", projectId);
    return `${STREAM_PATH}?${params.toString()}`;
  }

  /**
   * Покрыт ли вид потоком ПРЯМО СЕЙЧАС.
   *
   * Три условия вместе, и ни одного лишнего: канал есть, служебное сообщение
   * открытия получено, вид назван словарём владельца. Ослабь любое — и опрос
   * снимется там, где событий не будет.
   */
  covers(target: StreamTarget): boolean {
    const ch = this.channels.get(SubscriptionHub.key(target.owner, target.projectId));
    return !!ch && ch.open && ch.knownKinds.has(target.kind);
  }

  /** Наблюдатель смены покрытия. Покрытие ОБЪЯВЛЯЕТСЯ, а не опрашивается. */
  onCoverageChange(fn: () => void): () => void {
    this.watchers.add(fn);
    return () => {
      this.watchers.delete(fn);
    };
  }

  private announce(): void {
    for (const fn of [...this.watchers]) fn();
  }

  /**
   * Подписаться на события вида. Возвращает снятие подписки.
   *
   * Снятие идемпотентно и закрывает поток, когда ушёл последний подписчик:
   * поток, переживший свою страницу, занимает место в потолке вызывающего,
   * которого потом не хватит соседней вкладке.
   */
  subscribe(target: StreamTarget, handler: (e: StreamEvent) => void): () => void {
    const key = SubscriptionHub.key(target.owner, target.projectId);
    let ch = this.channels.get(key);
    if (!ch) {
      ch = { source: null, knownKinds: new Set(), open: false, subscribers: new Map() };
      this.channels.set(key, ch);
    }
    const id = this.nextId++;
    ch.subscribers.set(id, { kind: target.kind, handler });
    this.ensureOpen(key, ch, target.owner, target.projectId);

    let released = false;
    return () => {
      if (released) return;
      released = true;
      const live = this.channels.get(key);
      if (!live) return;
      live.subscribers.delete(id);
      if (live.subscribers.size === 0) {
        live.source?.close();
        this.channels.delete(key);
      }
    };
  }

  private ensureOpen(key: string, ch: Channel, owner: string, projectId: string | null): void {
    if (ch.source) return;
    const failedAt = this.failedAt.get(key);
    const now = (this.deps.now ?? Date.now)();
    if (failedAt !== undefined && now - failedAt < (this.deps.reopenAfterMs ?? REOPEN_AFTER_MS)) return;
    // Принимать нечем — остаёмся на опросе. Отмечается ТЕМ ЖЕ окном молчания,
    // что и отказ края: оно конечно, поэтому среда, где приёмник появится,
    // подхватится сама, а окно не даёт писать в журнал на каждый перерисов.
    if (!(this.deps.available?.() ?? true)) {
      if (failedAt === undefined) {
        this.deps.log("приёмник событий недоступен — списки остаются на опросе", { owner });
      }
      this.failedAt.set(key, now);
      return;
    }
    this.failedAt.delete(key);
    const url = this.url(owner, projectId);
    const source = this.deps.open(url);
    ch.source = source;

    source.addEventListener("opened", (ev) => {
      const opened = parseFrame(ev.data)?.opened;
      if (!opened) {
        // Кадр открытия, который не разобрался, — не «пустой словарь», а
        // отсутствие ответа на вопрос о покрытии. Опрос остаётся.
        this.deps.log("служебное сообщение открытия не разобрано — опрос остаётся", { owner });
        return;
      }
      const live = this.channels.get(key);
      if (!live) return;
      live.knownKinds = new Set(opened.knownKinds ?? []);
      live.open = true;
      this.announce();
    });

    source.addEventListener("event", (ev) => {
      const event = parseFrame(ev.data)?.event;
      if (!event) return;
      const live = this.channels.get(key);
      if (!live) return;
      // Событие раздаётся ФАКТОМ ИЗМЕНЕНИЯ и всем подписчикам своего вида: род
      // изменения здесь не различается намеренно — снятие предмета единственное
      // сообщает, что строки больше нет, и состояния оно не несёт ни у одного
      // владельца.
      for (const sub of live.subscribers.values()) {
        if (sub.kind === event.kind) sub.handler(event);
      }
    });

    source.onerror = () => {
      const live = this.channels.get(key);
      if (!live) return;
      // Приёмник переподключается сам, пока соединение живо (`CONNECTING`);
      // закрытым он становится там, где повтора не будет — отказ до заголовков
      // (`501` без объявленного владельца, `403`, `429`) либо неверный тип.
      if (source.readyState !== SOURCE_CLOSED) return;
      live.open = false;
      live.source = null;
      this.failedAt.set(key, (this.deps.now ?? Date.now)());
      this.announce();
      this.explain(url, owner);
    };
  }

  /**
   * Назвать причину отказа СЛОВАМИ КРАЯ — ровно один раз на канал.
   *
   * Отказ здесь не роняет страницу: она возвращается к опросу и работает. Но
   * молчаливый возврат сделал бы «возможность не включена на этой установке»
   * неотличимым от «край сломан», а в журнале браузера не осталось бы ни того,
   * ни другого.
   */
  private explain(url: string, owner: string): void {
    void this.deps
      .diagnose(url)
      .then(({ status, contentType, body }) => {
        this.deps.log(
          `поток владельца «${owner}» не открылся: край ответил ${status} ${contentType}. ` +
            `Списки этого домена остаются на опросе. ${body}`,
        );
      })
      .catch((err: unknown) => {
        this.deps.log(`поток владельца «${owner}» не открылся, и причину узнать не удалось`, err);
      });
  }
}

interface Frame {
  opened?: { position?: string; caughtUp?: boolean; knownKinds?: string[]; honoredFilters?: string[] };
  event?: StreamEvent;
}

function parseFrame(raw: string): Frame | null {
  try {
    return JSON.parse(raw) as Frame;
  } catch {
    return null;
  }
}

/**
 * Хаб консоли — ОДИН на страницу.
 *
 * Ленивый: пока ни один список не подписался, приёмника событий не создаётся
 * вовсе, и на установке без объявленного владельца край не получает ни одного
 * запроса сверх обычных.
 */
let shared: SubscriptionHub | null = null;
export function subscriptionHub(): SubscriptionHub {
  shared ??= new SubscriptionHub();
  return shared;
}
