// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, test } from "@playwright/test";

/**
 * Приёмник писем стенда — поверхность чтения набора консоли (приёмка F6b, Р13,
 * F6b-32, F6b-34; kacho#2901).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ НАБОРУ ПОЧТОВЫЙ ЯЩИК
 *
 * Дальше экранов регистрации и входа человек проходит только с подтверждённым
 * адресом почты (решение владельца 2026-09-27), а подтверждает адрес код из
 * письма, которое служба поставила регистрацией. Фикстура набора заводит
 * человека тем же путём, что человек, — значит, читает его письмо. Место
 * почтового ящика человека у набора занимает приёмник писем стенда
 * (`deploy/helm/umbrella/templates/mail-receiver.yaml`): он принимает то, что
 * служба уже сдала почтовому узлу, и отдаёт принятое по HTTP. Здесь он только
 * ЧИТАЕТСЯ — ни службе, ни приёмнику набор ничего не пишет.
 *
 * Код берётся ИЗ ПИСЬМА, а не из хранилища и не из строки очереди службы: проба
 * судит продукт через те поверхности, которые есть у человека.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * АДРЕС — ВХОД ПРОГОНА, А НЕ УМОЛЧАНИЕ
 *
 * Поверхность чтения открывает шаг конвейера пробросом к службе приёмника и
 * отдаёт адрес переменной `KACHO_CONSOLE_MAILBOX_URL`. Умолчания нет по той же
 * причине, что у адреса консоли: непустое умолчание увело бы набор на
 * несуществующий адрес, и «не выполнилось» читалось бы как «красное».
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ИСХОДОВ ТРИ, И ТРЕТИЙ ПОМЕЧАЕТСЯ СТРУКТУРНО
 *
 * Поверхность не читается либо письма нет в срок — это «условие не создано»,
 * а не вердикт о продукте (`e2e-flow.md` §1). Отказ несёт ДВА признака сразу:
 * пометку пробы вида `CONDITION_NOT_CREATED` и тот же текст в тексте отказа.
 * По ним гейт вердикта (`.github/scripts/assert-console-probes-verdict.py`)
 * относит пробу к «не выполнилось»; одного признака ему мало, и проба, упавшая
 * по существу, пометки не получает никогда — её ставит только этот модуль.
 *
 * Пометку ставит ТОТ, КТО ПРОИЗВОДИТ отказ, а не тот, кто его ждёт: получить
 * `ConditionNotCreated` иначе, чем через `conditionNotCreated`, нельзя —
 * конструктор закрыт, и `new ConditionNotCreated(…)` вне класса не проходит
 * проверку типов. Прежде помечал только ожидающий (`awaitLetter`, условие
 * прогона), и одиночное чтение — снимок писем до регистрации — отказывало тем же
 * текстом БЕЗ пометки: гейт подавал несозданное условие красным о продукте
 * (kacho#2901, возврат проверки опытом H5). Пометка преходящего чтения, после
 * которого проба упала по существу, красного не извиняет: её текста нет в
 * отказе, а гейту нужны оба признака. Держит самопроверка
 * `scripts/mail-receiver-marks-selftest.ts` — исполнением каждой формы чтения.
 *
 * Отказ продукта там, где набор получил всё, чего требует контракт, — находка:
 * два письма там, где регистрация ставит одно, и письмо без кода. Такой отказ
 * пометки не несёт и остаётся красным.
 */

/** Переменная, в которой шаг конвейера отдаёт адрес поверхности чтения. */
export const MAILBOX_URL_ENV = "KACHO_CONSOLE_MAILBOX_URL";

/**
 * Вид пометки пробы «условие не создано». Его же читает гейт вердикта
 * (`FIXTURE_UNMET_ANNOTATION`), и совпадение двух объявлений сверяет его
 * самопроверка: разошедшись, они вернули бы несозданное условие в красное.
 */
export const CONDITION_NOT_CREATED = "условие не создано";

export const UNMET_MAILBOX = "условие не создано: приёмник писем не читается";
export const UNMET_LETTER = "условие не создано: письмо подтверждения не дошло до приёмника";

/**
 * Срок ожидания письма регистрации.
 *
 * Письмо служба ставит в очередь той же транзакцией, что заводит человека, а
 * сдаёт узлу дренаж очереди: будит его уведомление базы, и без уведомления он
 * всё равно просыпается не реже раза в 30 с (`PollFallback` дренажа службы,
 * `cmd/kaname/invite_mail_wiring.go` в `PRO-Robotech/kaname`). 45 с — один
 * пропущенный будильник и попытка сдачи сверху. Срок обязан кончаться ВНУТРИ
 * предела пробы (`timeout` в `playwright.config.ts`, 90 с): иначе вместо текста
 * несозданного условия прогонщик назвал бы исход своим «Test timeout», то есть
 * красным.
 */
export const LETTER_BUDGET_MS = 45_000;

/** Строка письма, за которой стоит код (тело письма — `RenderVerificationMail` службы). */
export const CODE_LINE = "Код подтверждения:";

/**
 * Строка письма восстановления доступа, за которой стоит код (тело письма —
 * `RenderRecoveryMail` службы, приёмка Ф5). Строка своя, а не общая с письмом
 * подтверждения: письмо одного вида не годится за письмо другого, и проба,
 * ждущая код восстановления, не примет код подтверждения адреса.
 */
export const RECOVERY_CODE_LINE = "Код восстановления:";

/** Алфавит кода — Крокфорд, 10 значащих знаков (Р7 службы); дефисы и пробелы не значимы. */
const CODE_SIGNIFICANT = /^[0-9A-HJKMNP-TV-Z]{10}$/i;

/**
 * Отказ «условие не создано» — несёт пометку, по которой его узнаёт гейт вердикта.
 *
 * Конструктор закрыт: отказ без пометки непредставим. Единственный путь к нему —
 * `ConditionNotCreated.raise` (он же `conditionNotCreated`), который сперва
 * помечает пробу.
 */
export class ConditionNotCreated extends Error {
  private constructor(text: string) {
    super(text);
  }

  /**
   * Пометить текущую пробу несозданным условием и отказать тем же текстом. Вне
   * пробы помечать нечего — тогда остаётся только отказ.
   */
  static raise(text: string): never {
    try {
      test.info().annotations.push({ type: CONDITION_NOT_CREATED, description: text });
    } catch (_outsideTest) {
      // `test.info()` вне пробы бросает — пометке там не к чему крепиться.
    }
    throw new ConditionNotCreated(text);
  }
}

/** Пометить текущую пробу несозданным условием и отказать тем же текстом. */
export function conditionNotCreated(text: string): never {
  return ConditionNotCreated.raise(text);
}

/** Письмо приёмника: идентификатор, момент приёма, текст тела и код, как он написан в письме. */
export interface Letter {
  id: string;
  created: string;
  text: string;
  code: string;
}

/**
 * Код письма так, как его видит человек: первая непустая строка после строки
 * «Код подтверждения:». Пусто — кода в теле нет. Код не приводится: экран
 * отправляет его как введён, приведение делает служба (Р7 службы).
 */
export function codeOf(text: string, codeLine: string = CODE_LINE): string {
  const lines = text.split(/\r?\n/);
  const at = lines.findIndex((l) => l.trim() === codeLine);
  if (at < 0) return "";
  const line = lines.slice(at + 1).find((l) => l.trim() !== "")?.trim() ?? "";
  return CODE_SIGNIFICANT.test(line.replace(/[-\s]/g, "")) ? line : "";
}

/**
 * Поверхность не прочиталась — несозданное условие, и проба помечается ЗДЕСЬ,
 * при любом чтении: снимке писем до регистрации, числе писем, ожидании письма.
 * Чтение, которое вызывающий перехватил и повторил (условие прогона), оставляет
 * пометку преходящего отказа — и она безвредна: гейт засчитывает «не
 * выполнилось», только если тот же текст стоит в отказе пробы.
 */
function unreadable(base: string, detail: string): never {
  return conditionNotCreated(`${UNMET_MAILBOX}: ${base} — ${detail}`);
}

export class Mailbox {
  constructor(readonly base: string) {}

  private async json(path: string): Promise<Record<string, unknown>> {
    let res: Response;
    try {
      res = await fetch(`${this.base}${path}`, {
        headers: { Accept: "application/json" },
        signal: AbortSignal.timeout(15_000),
      });
    } catch (err) {
      const cause = (err as { cause?: { code?: string; message?: string } }).cause;
      return unreadable(this.base, `${path} не ответил (${cause?.code ?? cause?.message ?? String(err)})`);
    }
    const text = await res.text();
    if (!res.ok) return unreadable(this.base, `${path} ответил ${res.status}: ${text.slice(0, 200)}`);
    let body: unknown;
    try {
      body = JSON.parse(text);
    } catch (_notJson) {
      return unreadable(this.base, `${path} ответил ${res.status} не JSON: ${text.slice(0, 200)}`);
    }
    if (body === null || typeof body !== "object" || Array.isArray(body)) {
      return unreadable(this.base, `${path} ответил не объектом`);
    }
    return body as Record<string, unknown>;
  }

  /** Число писем у приёмника — вопрос, которым проверяется, что поверхность читается. */
  async total(): Promise<number> {
    const d = await this.json("/api/v1/messages?limit=1");
    const n = d.messages_count ?? d.total;
    if (typeof n !== "number") return unreadable(this.base, "в ответе нет числа писем");
    return n;
  }

  /** Письма на адрес — в порядке приёма. */
  async letters(address: string): Promise<Letter[]> {
    const query = encodeURIComponent(`to:"${address}"`);
    const found = await this.json(`/api/v1/search?query=${query}&limit=50`);
    const rows = Array.isArray(found.messages) ? (found.messages as Array<Record<string, unknown>>) : [];
    const out: Letter[] = [];
    for (const m of rows) {
      const to = Array.isArray(m.To) ? (m.To as Array<{ Address?: unknown }>) : [];
      if (!to.some((r) => String(r?.Address ?? "").toLowerCase() === address.toLowerCase())) continue;
      const id = String(m.ID ?? "");
      const full = await this.json(`/api/v1/message/${encodeURIComponent(id)}`);
      const text = String(full.Text ?? "");
      out.push({ id, created: String(m.Created ?? ""), text, code: codeOf(text) });
    }
    return out.sort((a, b) => a.created.localeCompare(b.created));
  }
}

/** Поверхность чтения, чей адрес отдал прогон. Адреса нет — условие не создано. */
export function stationMailbox(): Mailbox {
  const base = (process.env[MAILBOX_URL_ENV] ?? "").trim().replace(/\/+$/, "");
  if (!base) {
    return conditionNotCreated(
      `${UNMET_MAILBOX}: адрес поверхности чтения не задан (${MAILBOX_URL_ENV} пуст) — ` +
        "его открывает шаг конвейера «приёмник писем стенда открыт прогону»",
    );
  }
  return new Mailbox(base);
}

/**
 * Письмо на `address`, принятое ПОСЛЕ `before` — перечня писем, лежавших у
 * приёмника до регистрации. Письмо после неё ровно одно: первое письмо ставит
 * регистрация (Р15 службы), а экран подтверждения при открытии письма не просит
 * (F6b-18). Ждётся письмо, а не время: `expect.poll` спрашивает приёмник, пока
 * письмо не пришло либо не кончился срок.
 */
export async function awaitLetter(
  mailbox: Mailbox,
  address: string,
  before: ReadonlySet<string>,
  budgetMs: number = LETTER_BUDGET_MS,
  codeLine: string = CODE_LINE,
): Promise<Letter> {
  // Исход последнего опроса — объектом, а не переменными: присваивания внутри
  // опроса разбор потока управления снаружи не видит.
  const last: { fresh: Letter[]; failure: unknown } = { fresh: [], failure: null };
  let arrived = true;
  try {
    await expect
      .poll(
        async () => {
          try {
            last.fresh = (await mailbox.letters(address)).filter((l) => !before.has(l.id));
            last.failure = null;
          } catch (err) {
            last.failure = err;
            return "поверхность не читается";
          }
          return last.fresh.length > 0 ? "письмо есть" : "письма нет";
        },
        { timeout: budgetMs, intervals: [500, 1_000] },
      )
      .not.toBe("письма нет");
  } catch (_budgetSpent) {
    arrived = false;
  }
  // Отказ чтения уже помечен тем, кто его произвёл (`unreadable`), — и
  // пробрасывается как есть; любой иной отказ пометки не несёт и остаётся красным.
  if (last.failure !== null) throw last.failure;
  if (!arrived) {
    return conditionNotCreated(
      `${UNMET_LETTER}: адрес ${address}, приёмник ${mailbox.base}, ждали ${budgetMs / 1000} с после регистрации`,
    );
  }
  const fresh = last.fresh;
  expect(
    fresh.length,
    `писем на ${address} после регистрации ${fresh.length}, ждали ровно одно: первое письмо ставит ` +
      "регистрация (Р15 службы), а экран подтверждения при открытии письма не просит",
  ).toBe(1);
  // Код читается по строке ТОГО вида письма, которого ждёт вызывающий: письмо
  // другого вида кода под этой строкой не несёт и отвергается здесь же.
  const letter = { ...fresh[0], code: codeOf(fresh[0].text, codeLine) };
  expect(
    letter.code,
    `письмо на ${address} принято приёмником, но кода под строкой «${codeLine}» в его теле нет:\n${letter.text.slice(0, 600)}`,
  ).not.toBe("");
  return letter;
}
