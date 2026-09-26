// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { describe, expect, it } from "@jest/globals";
import { spawn } from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * ПОДКЛЮЧЕНИЕ СТРАЖА МЕСТ ВЫПУСКА — судится ИСПОЛНЕНИЕМ окружения модуля
 * (приёмка F8, Р10, F8-46; возврат проверки F9).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Страж (`issuance-guard.ts`) записывает обход и отказывает ему. Пробу же роняет
 * не он, а строка окружения — вызов `failOnIssuanceBreaches` в `afterEach` и
 * `afterAll` общего `setup.ts`. Проба стража зовёт эту функцию САМА, поэтому
 * оставалась зелёной и тогда, когда строки в окружении не было: снятый вызов
 * давал 179 из 179 зелёных проб стража, а проглоченный обход в dashboard — 6 из
 * 6 (опыт E1 проверки). Заголовок пробы был шире её тела.
 *
 * Здесь судится то, что она пропускала: ОТДЕЛЬНЫЙ процесс jest с НАСТОЯЩЕЙ
 * конфигурацией модуля — его `setupFilesAfterEnv`, его цепочкой окружений —
 * исполняет канарейку `issuance-guard.canary.ts`, и её исход сверяется по отчёту
 * процесса. Ожидается ровно одно:
 *   • проглоченный обход — единица ПАДАЕТ текстом `[F8-46]` со своим адресом
 *     (держит `afterEach` окружения);
 *   • обход в разборе после последней единицы — ФАЙЛ падает тем же текстом со
 *     своим адресом, а единица блока проходит (держит `afterAll`);
 *   • тот же выпуск упорядочивающим транспортом — единица ПРОХОДИТ, и сеть его
 *     получила (близнец: изменён один факт — чем выпущено).
 *
 * Процесс, не оставивший отчёта, — «не выполнилось», а не зелёное: вердикта о
 * подключении тогда нет, и проба падает, называя это.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ГДЕ СТОИТ
 *
 * В прогоне КАЖДОГО из девяти модулей консоли и под его собственной
 * конфигурацией: семь исполняют пробу общего пакета (`issuance-wiring.test.ts`,
 * модуль — каталог прогона), `host` и `dashboard`, общего пакета не
 * исполняющие, — свою (`src/test/issuance-wiring.test.ts`). Поэтому пакет, чьё
 * окружение до общего не достаёт, краснеет своим прогоном, а не молчит.
 */

const here = path.dirname(fileURLToPath(import.meta.url));

/** Канарейка — вход пробы; прогон модуля её не собирает (не `*.test.ts`). */
export const ISSUANCE_CANARY_FILE = path.join(here, "issuance-guard.canary.ts");

/** Единицы канарейки и адреса их выпуска: по ним исход читается из отчёта. */
export const ISSUANCE_CANARY = {
  swallowed: {
    title: "канарейка F8-46 · обход, проглоченный кодом продукта, роняет пробу",
    path: "/kacho-canary/swallowed",
  },
  twin: {
    title: "близнец F8-46 · тот же выпуск упорядочивающим транспортом проходит",
    path: "/kacho-canary/twin",
  },
  late: {
    title: "канарейка F8-46 · обход в разборе после последней единицы роняет файл",
    unit: "единица блока без выпуска проходит",
    path: "/kacho-canary/late",
  },
} as const;

/** Предел процесса канарейки: зависший процесс — «не выполнилось», а не зелёное. */
const CANARY_BUDGET_MS = 180_000;

interface UnitResult {
  title: string;
  status: string;
  failureMessages: string[];
}

interface FileResult {
  name: string;
  status: string;
  message: string;
  assertionResults: UnitResult[];
}

/** Исход процесса канарейки: код, отчёт jest (если он есть) и хвост потока ошибок. */
export interface CanaryRun {
  moduleDir: string;
  exitCode: number | null;
  signal: string | null;
  files: FileResult[] | null;
  stderrTail: string;
}

/**
 * Исполнить канарейку отдельным процессом jest под конфигурацией модуля `moduleDir`.
 *
 * Отчёт читается из потока вывода процесса (`--json` без `--outputFile`), а не из
 * файла: проба не пишет в файловую систему ничего, что пережило бы её суиту.
 */
export async function runIssuanceCanary(moduleDir: string): Promise<CanaryRun> {
  const config = path.join(moduleDir, "jest.config.cjs");
  if (!fs.existsSync(config)) {
    throw new Error(
      `${config} не найден: проба подключения судит окружение МОДУЛЯ и берёт его из каталога прогона ` +
        "(`npm test --prefix <модуль>`). Прогон из другого каталога — несозданное условие, а не вердикт.",
    );
  }
  // Тот же jest, что у `npm test` модуля: разрешается от каталога модуля.
  const jestBin = path.join(path.dirname(createRequire(config).resolve("jest/package.json")), "bin", "jest.js");
  const args = [
    "--no-warnings",
    "--experimental-vm-modules",
    jestBin,
    "--config",
    config,
    "--ci",
    "--runInBand",
    "--silent",
    "--forceExit",
    "--testTimeout=30000",
    "--json",
    "--roots",
    here,
    "--testMatch",
    ISSUANCE_CANARY_FILE,
  ];
  const env = { ...process.env };
  delete env.JEST_WORKER_ID;
  const { exitCode, signal, stdout, stderr } = await new Promise<{
    exitCode: number | null;
    signal: string | null;
    stdout: string;
    stderr: string;
  }>((resolve, reject) => {
    const child = spawn(process.execPath, args, { cwd: moduleDir, env, stdio: ["ignore", "pipe", "pipe"] });
    let out = "";
    let err = "";
    child.stdout.on("data", (chunk: Buffer) => {
      out += chunk.toString("utf8");
    });
    child.stderr.on("data", (chunk: Buffer) => {
      err = (err + chunk.toString("utf8")).slice(-4000);
    });
    const timer = setTimeout(() => child.kill("SIGKILL"), CANARY_BUDGET_MS);
    child.on("error", (e) => {
      clearTimeout(timer);
      reject(e);
    });
    child.on("close", (code, sig) => {
      clearTimeout(timer);
      resolve({ exitCode: code, signal: sig, stdout: out, stderr: err });
    });
  });
  let files: FileResult[] | null = null;
  let tail = stderr;
  try {
    files = (JSON.parse(stdout) as { testResults: FileResult[] }).testResults;
  } catch {
    tail = `поток вывода не разобран как отчёт jest: ${stdout.slice(0, 400)}\n${stderr}`;
  }
  return { moduleDir, exitCode, signal, files, stderrTail: tail };
}

const names = (text: string, where: string): boolean => text.includes("[F8-46]") && text.includes(where);

/**
 * Суждение об исходе канарейки — отдельно от исполнения. Пустой перечень —
 * окружение модуля роняет пробу с обходом и пропускает законный выпуск.
 */
export function judgeIssuanceCanary(run: CanaryRun): string[] {
  const where = path.basename(run.moduleDir);
  if (run.files === null) {
    return [
      `${where}: процесс канарейки не оставил отчёта (код ${run.exitCode}, сигнал ${run.signal}) — ` +
        `вердикта о подключении стража нет, это «не выполнилось».\n${run.stderrTail}`,
    ];
  }
  const own = run.files.filter((f) => path.resolve(f.name) === ISSUANCE_CANARY_FILE);
  if (run.files.length !== 1 || own.length !== 1) {
    return [
      `${where}: процесс канарейки исполнил файлов ${run.files.length}, из них канарейка — ${own.length}; ` +
        `ожидался ровно ${ISSUANCE_CANARY_FILE}.\n${run.stderrTail}`,
    ];
  }
  const file = own[0];
  const out: string[] = [];
  const units = new Map(file.assertionResults.map((u) => [u.title, u]));
  const expected = [ISSUANCE_CANARY.swallowed.title, ISSUANCE_CANARY.twin.title, ISSUANCE_CANARY.late.unit];
  if (file.assertionResults.length !== expected.length || expected.some((t) => !units.has(t))) {
    out.push(
      `${where}: единиц канарейки исполнено ${file.assertionResults.length} (${file.assertionResults
        .map((u) => `«${u.title}» ${u.status}`)
        .join(", ")}), ожидались ровно ${expected.length}: ${expected.map((t) => `«${t}»`).join(", ")}.\n` +
        (file.message || run.stderrTail),
    );
    return out;
  }

  const swallowed = units.get(ISSUANCE_CANARY.swallowed.title) as UnitResult;
  if (swallowed.status !== "failed" || !names(swallowed.failureMessages.join("\n"), ISSUANCE_CANARY.swallowed.path)) {
    out.push(
      `${where}: обход ${ISSUANCE_CANARY.swallowed.path}, проглоченный кодом, — единица ${swallowed.status}, ` +
        "а не отказ текстом [F8-46] с этим адресом: `afterEach` окружения модуля не роняет пробу, " +
        "за которой записан обход (`failOnIssuanceBreaches` в `shared/src/test/setup.ts`).\n" +
        swallowed.failureMessages.join("\n"),
    );
  }

  const twin = units.get(ISSUANCE_CANARY.twin.title) as UnitResult;
  if (twin.status !== "passed") {
    out.push(
      `${where}: выпуск ${ISSUANCE_CANARY.twin.path} упорядочивающим транспортом — единица ${twin.status}: ` +
        "окружение роняет и законный выпуск, то есть не отличает его от обхода.\n" +
        twin.failureMessages.join("\n"),
    );
  }

  const lateUnit = units.get(ISSUANCE_CANARY.late.unit) as UnitResult;
  const lateInUnits = file.assertionResults.some((u) => names(u.failureMessages.join("\n"), ISSUANCE_CANARY.late.path));
  if (lateUnit.status !== "passed" || lateInUnits || !names(file.message, ISSUANCE_CANARY.late.path)) {
    out.push(
      `${where}: обход ${ISSUANCE_CANARY.late.path} в разборе после последней единицы не уронил файл текстом ` +
        "[F8-46] с этим адресом: `afterAll` окружения модуля не судит находки, записанные после " +
        "последнего `afterEach`.\n" +
        file.message,
    );
  }

  if (out.length === 0 && run.exitCode !== 1) {
    out.push(
      `${where}: исход единиц верен, но процесс канарейки вышел с кодом ${run.exitCode}, а не 1.\n${run.stderrTail}`,
    );
  }
  return out;
}

/** Строка отчёта: что исполнено и с каким исходом — объём осмотренного виден и на зелёном. */
export function describeIssuanceCanary(run: CanaryRun): string {
  const units = (run.files ?? []).flatMap((f) => f.assertionResults.map((u) => `«${u.title}» ${u.status}`));
  return (
    `\n[F8-46] подключение стража · модуль ${path.basename(run.moduleDir)} · процесс канарейки: код ${run.exitCode}` +
    ` · единиц ${units.length}: ${units.join(", ") || "—"}\n`
  );
}

/**
 * Проба подключения стража в окружении модуля `moduleDir` — одна на девять
 * модулей: каждый зовёт её из своего прогона.
 */
export function describeIssuanceWiring(moduleDir: string): void {
  describe("F8-46 · подключение стража мест выпуска: окружение модуля роняет пробу с обходом", () => {
    it(
      `F8-46 · ${path.basename(moduleDir)}: проглоченный обход роняет единицу, обход в разборе — файл, законный выпуск проходит`,
      async () => {
        const run = await runIssuanceCanary(moduleDir);
        process.stdout.write(describeIssuanceCanary(run));
        expect(judgeIssuanceCanary(run)).toEqual([]);
      },
      CANARY_BUDGET_MS + 30_000,
    );
  });
}
