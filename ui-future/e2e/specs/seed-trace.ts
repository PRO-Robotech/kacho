// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { APIRequestContext, TestInfo, Tracing } from "@playwright/test";

/**
 * ТРАССА ПОСЕВА — вложение «trace» упавшей пробы, которая страницы не берёт
 * (kacho#3200).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Трассу пишет разбор фикстуры `page` (`specs/fixtures.ts`), и гейт отчёта
 * (`.github/scripts/assert-console-probes-verdict.py`, вложение `trace`) требует
 * её у КАЖДОЙ упавшей пробы. Пробы полосы через край (`session-lane-edge`,
 * `second-factor-wrap-rotation` и др.) страницы не берут: все их обращения идут
 * контекстом посева (`newSeed`), и упавшая такая проба оставалась без записи
 * того, что она слала и что получала. Наблюдалось на прогоне 38408151116:
 * Ф12-20 упала `503` на заведении группы, и гейт сказал «у упавшей пробы нет
 * вложения «trace»».
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * УСТРОЙСТВО
 *
 * Контекст посева записывается тем же механизмом, которым прогонщик пишет свой
 * штатный контекст запросов: запись `_tracing` контекста. Публичного свойства
 * у контекста запросов нет — его использует сам прогонщик для фикстуры
 * `request`; нет записи у установленной версии — `newSeed` отказывает громко,
 * а не теряет трассу молча.
 *
 * Запись живёт до разбора ПРОБЫ, а не до `seed.dispose()`: решить, нужен ли
 * архив, можно только по исходу пробы, а он известен лишь после её тела.
 * Поэтому `dispose()` посева здесь откладывает закрытие контекста до разбора
 * (`settleSeedTraces`); не закрытый пробой посев разбор закрывает сам. Проба
 * прошла — запись останавливается без архива (как `retain-on-failure`); упала —
 * архив на каждый посев, вложением `trace`.
 *
 * Регистр ведёт фикстура набора (`seedTraces`, `specs/fixtures.ts`); посев,
 * заведённый вне неё (условия прогона, `preconditions/`), не пишется и
 * закрывается сразу.
 */

interface TracedSeed {
  api: APIRequestContext;
  tracing: Tracing;
}

const tracedByTest = new Map<string, TracedSeed[]>();

/** Запись контекста запросов — та, которой прогонщик пишет свой `request`. */
function tracingOf(api: APIRequestContext): Tracing {
  const tracing = (api as unknown as { _tracing?: Tracing })._tracing;
  if (!tracing || typeof tracing.start !== "function" || typeof tracing.stop !== "function") {
    throw new Error(
      "контекст запросов посева без записи `_tracing`: у установленной версии playwright её нет, " +
        "и упавшая проба посева осталась бы без вложения «trace» (specs/seed-trace.ts)",
    );
  }
  return tracing;
}

/** Открывает регистр записей посева пробы; зовёт разбор `seedTraces`. */
export function beginSeedTraces(testId: string): void {
  tracedByTest.set(testId, []);
}

/**
 * Записывает контекст посева, если проба под регистром. Возвращает закрытие
 * для `seed.dispose()`: под регистром оно откладывается до разбора пробы.
 */
export async function traceSeed(api: APIRequestContext, testInfo: TestInfo): Promise<() => Promise<void>> {
  const traced = tracedByTest.get(testInfo.testId);
  if (!traced) return () => api.dispose();
  const tracing = tracingOf(api);
  await tracing.start({ snapshots: true, screenshots: false, sources: false, title: testInfo.title });
  traced.push({ api, tracing });
  return async () => {};
}

/**
 * Разбор пробы: упала — архив каждого посева вложением `trace`; прошла — запись
 * без архива. Контексты закрываются в обоих случаях.
 */
export async function settleSeedTraces(testInfo: TestInfo): Promise<void> {
  const traced = tracedByTest.get(testInfo.testId) ?? [];
  tracedByTest.delete(testInfo.testId);
  const failed = testInfo.status !== testInfo.expectedStatus;
  const problems: string[] = [];
  for (const [i, { api, tracing }] of traced.entries()) {
    try {
      if (failed) {
        const path = testInfo.outputPath(`seed-${i + 1}-trace.zip`);
        await tracing.stop({ path });
        testInfo.attachments.push({ name: "trace", path, contentType: "application/zip" });
      } else {
        await tracing.stop();
      }
    } catch (e) {
      problems.push(`посев ${i + 1}: запись не остановлена — ${e instanceof Error ? e.message : String(e)}`);
    }
    try {
      await api.dispose();
    } catch (e) {
      problems.push(`посев ${i + 1}: контекст не закрыт — ${e instanceof Error ? e.message : String(e)}`);
    }
  }
  if (problems.length > 0) throw new Error(`разбор посева пробы не завершён:\n${problems.join("\n")}`);
}
