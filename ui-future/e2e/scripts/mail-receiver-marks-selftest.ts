// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА: ЧТЕНИЕ ПРИЁМНИКА ПИСЕМ, НЕ СОСТОЯВШЕЕСЯ ПО ВИНЕ ПРИЁМНИКА, —
 * «НЕ ВЫПОЛНИЛОСЬ», А НЕ КРАСНОЕ (приёмка F6b, Р13, F6b-34; kacho#2901).
 *
 * Набор читает приёмник писем стенда в нескольких местах: снимок писем до
 * регистрации (`register` в `specs/fixtures.ts`, F6b-32, посев П-п, F8-13 и
 * F8-17), число писем (условие F6b-34) и ожидание письма (`awaitLetter`).
 * Приёмник, который не отвечает, — несозданное условие, а не вердикт о
 * продукте, и гейт вердикта (`.github/scripts/assert-console-probes-verdict.py`)
 * узнаёт его по ДВУМ признакам сразу: пометке пробы вида «условие не создано» и
 * тому же тексту в отказе. Прежде пометку ставил только тот, кто ждал
 * (`awaitLetter`, условие прогона), а одиночное чтение отказывало тем же текстом
 * БЕЗ пометки — и гейт подавал несозданное условие красным о продукте (возврат
 * проверки опытом, H5).
 *
 * Судится ИСПОЛНЕНИЕМ, а не прочтением: отдельный процесс прогонщика исполняет
 * канарейку `mail-receiver-canary/reads.canary.ts` против приёмника петли и
 * адреса, на котором никто не слушает, а исход каждой единицы классифицирует
 * НАСТОЯЩИЙ распознаватель гейта (`outcomes` и `fixture_unmet` того же файла) —
 * не копия его предиката:
 *   • каждая форма чтения у неотвечающего приёмника — «не выполнилось», и
 *     названо несозданное условие приёмника;
 *   • близнец — то же сорванное чтение перехвачено, проба упала по существу —
 *     красное: пометка преходящего чтения красного не извиняет;
 *   • контроли у отвечающего приёмника проходят — читатель читает.
 *
 * Стенд и браузер не нужны: приёмник — сервер петли, единицы не берут `page`.
 *
 * Запуск: node scripts/mail-receiver-marks-selftest.ts
 */

import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import type { AddressInfo } from "node:net";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  LETTER_ADDRESS,
  LETTER_CODE,
  MAIL_CANARY,
  MAIL_CANARY_DEAD_ENV,
  MAIL_CANARY_LIVE_ENV,
  MAIL_CANARY_OUT_ENV,
  type Outcome,
} from "./mail-receiver-canary/names.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const GATE = path.join(here, "..", "..", "..", ".github", "scripts", "assert-console-probes-verdict.py");

let checked = 0;
let failed = 0;
function check(ok: boolean, what: string): void {
  checked += 1;
  if (!ok) failed += 1;
  console.log(`  ${ok ? "ОК    " : "ПРОВАЛ"} ${what}`);
}

// ─── ПРИЁМНИК ПЕТЛИ ─────────────────────────────────────────────────────────
// Подмножество поверхности чтения приёмника стенда, которое читает
// `specs/mail-receiver.ts`: число писем, поиск по адресу и тело письма. Одно
// письмо с кодом лежит на `LETTER_ADDRESS`, на `EMPTY_ADDRESS` писем нет.

const LETTER_ID = "mail-canary-1";
const LETTER_TEXT = [
  "Подтвердите адрес почты, чтобы продолжить работу.",
  "",
  "Код подтверждения:",
  "",
  LETTER_CODE,
  "",
  "Код действует 15 мин. с момента отправки и применяется один раз.",
].join("\n");

const receiver = http.createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://loop");
  const send = (status: number, body: unknown) => {
    res.writeHead(status, { "Content-Type": "application/json" });
    res.end(JSON.stringify(body));
  };
  if (url.pathname === "/api/v1/messages") return send(200, { messages_count: 1, total: 1 });
  if (url.pathname === "/api/v1/search") {
    const to = /^to:"(.*)"$/.exec(url.searchParams.get("query") ?? "")?.[1] ?? "";
    const rows =
      to === LETTER_ADDRESS
        ? [{ ID: LETTER_ID, Created: "2026-09-29T00:00:00Z", To: [{ Address: LETTER_ADDRESS }] }]
        : [];
    return send(200, { messages: rows, messages_count: rows.length });
  }
  if (url.pathname === `/api/v1/message/${LETTER_ID}`) return send(200, { ID: LETTER_ID, Text: LETTER_TEXT });
  return send(404, { error: `нет пути ${url.pathname}` });
});

function listen(server: http.Server): Promise<string> {
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`));
  });
}

/** Адрес, на котором только что закрыт слушатель: соединение отвергается. */
async function deadAddress(): Promise<string> {
  const probe = http.createServer();
  const at = await listen(probe);
  await new Promise<void>((r) => probe.close(() => r()));
  return at;
}

// ─── ПРОЦЕСС КАНАРЕЙКИ ──────────────────────────────────────────────────────

type Classified = { title: string; status: string; runs: number; unmet: string; message: string };

async function runCanary(live: string, dead: string, out: string): Promise<{ code: number | null; tail: string }> {
  const cli = createRequire(import.meta.url).resolve("@playwright/test/cli");
  const config = path.join(here, "mail-receiver-canary", "canary.playwright.config.ts");
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [cli, "test", "--config", config], {
      cwd: path.join(here, ".."),
      env: { ...process.env, [MAIL_CANARY_LIVE_ENV]: live, [MAIL_CANARY_DEAD_ENV]: dead, [MAIL_CANARY_OUT_ENV]: out },
      stdio: ["ignore", "pipe", "pipe"],
    });
    let text = "";
    const keep = (chunk: Buffer) => {
      text = (text + chunk.toString("utf8")).slice(-4000);
    };
    child.stdout.on("data", keep);
    child.stderr.on("data", keep);
    const timer = setTimeout(() => child.kill("SIGKILL"), 120_000);
    child.on("error", (e) => {
      clearTimeout(timer);
      reject(e);
    });
    child.on("close", (c) => {
      clearTimeout(timer);
      resolve({ code: c, tail: text });
    });
  });
}

/**
 * Исход каждой записи отчёта — НАСТОЯЩИМ распознавателем гейта вердикта: тот же
 * файл, те же `outcomes` и `fixture_unmet`, которыми гейт судит прогон набора.
 */
function classify(report: string): Classified[] {
  const program = [
    "import importlib.util, json, sys",
    "spec = importlib.util.spec_from_file_location('gate', sys.argv[1])",
    "gate = importlib.util.module_from_spec(spec)",
    "spec.loader.exec_module(gate)",
    "report = json.loads(open(sys.argv[2], encoding='utf-8').read())",
    "print(json.dumps([{'title': p.title, 'status': p.status, 'runs': p.runs,",
    "  'unmet': gate.fixture_unmet(p), 'message': gate.plain(p.message)} for p in gate.outcomes(report)],",
    "  ensure_ascii=False))",
  ].join("\n");
  const run = spawnSync("python3", ["-c", program, GATE, report], { encoding: "utf8" });
  if (run.status !== 0) {
    throw new Error(`распознаватель гейта не ответил (код ${run.status}): ${run.stderr || run.error?.message || ""}`);
  }
  return JSON.parse(run.stdout) as Classified[];
}

function outcomeOf(u: Classified): Outcome | string {
  if (u.status === "passed") return "passed";
  if (u.runs === 0) return "не исполнена";
  return u.unmet ? "unmet" : "red";
}

async function main(): Promise<void> {
  const live = await listen(receiver);
  const dead = await deadAddress();
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "kacho-mail-canary-"));
  try {
    console.log(`приёмник петли ${live} · неотвечающий адрес ${dead}`);
    console.log(`распознаватель — ${path.relative(path.join(here, "..", "..", ".."), GATE)} (outcomes, fixture_unmet)`);
    const { code, tail } = await runCanary(live, dead, out);
    const report = path.join(out, "report.json");
    if (!fs.existsSync(report)) {
      check(false, `процесс канарейки не оставил отчёта (код ${code}) — «не выполнилось»:\n${tail}`);
    } else {
      const units = classify(report);
      const declared = Object.values(MAIL_CANARY);
      console.log(`=== единиц в отчёте ${units.length}, объявлено ${declared.length} ===`);
      check(
        units.length === declared.length,
        `единиц канарейки исполнено ${units.length} из ${declared.length}: ` +
          (units.map((u) => `«${u.title}» ${u.status}`).join(", ") || "—"),
      );
      for (const { title, want } of declared) {
        const u = units.find((x) => x.title === title);
        const got = u ? outcomeOf(u) : "нет в отчёте";
        // Условие обязано быть названо АДРЕСОМ неотвечающего приёмника: иначе
        // «не выполнилось» могло прийти от чужого условия той же единицы.
        const named = want !== "unmet" || (u?.unmet ?? "").includes(dead);
        const why = u && got !== want ? ` · отказ: ${u.message.split("\n")[0].slice(0, 240)}` : "";
        check(
          got === want && named,
          `${title} (ждали ${want}, получили ${got}${want === "unmet" ? `; условие: ${u?.unmet || "не названо"}` : ""})${why}`,
        );
      }
    }
  } finally {
    fs.rmSync(out, { recursive: true, force: true });
    await new Promise<void>((r) => receiver.close(() => r()));
  }
  console.log(`проверок ${checked} · провалено ${failed}`);
  if (failed > 0 || checked === 0) process.exit(1);
}

await main();
