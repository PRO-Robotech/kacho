// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * САМОПРОВЕРКА УСЛОВИЯ П4: НЕПРИНИМАЕМОЕ ПРОИСХОЖДЕНИЕ — «НЕ ВЫПОЛНИЛОСЬ», А НЕ
 * КРАСНОЕ (приёмка F8-S4, §4 П4, DoD п. 6; kacho#1282).
 *
 * Проба-предусловие `preconditions/access-key-origin.precondition.ts` заводит
 * человеку ключ и снимает его. Отказ службы по происхождению — несозданное
 * условие: пробы ключа не стартуют, и гейт вердикта называет их «без
 * вердикта». Любой ИНОЙ отказ — возможный дефект службы: условие его не
 * останавливает, и о нём красным скажут пробы ключа. Перепутать два случая —
 * значит либо выдать дефект за «не выполнилось» (маска), либо остановить
 * пробы ключа красным там, где условие не создано.
 *
 * Судится ИСПОЛНЕНИЕМ, а не прочтением: отдельный процесс прогонщика исполняет
 * канарейку `access-key-origin-canary/origin.canary.ts` — тело предусловия
 * (`accessKeyOriginCondition`) против края петли, — а исход каждой единицы
 * классифицирует НАСТОЯЩИЙ распознаватель гейта (`outcomes` и `fixture_unmet`
 * `.github/scripts/assert-console-probes-verdict.py`):
 *   • отказ происхождения в той форме, что отдаёт край стенда (`400` · 3 ·
 *     `ORIGIN_NOT_ALLOWED`; форму живьём держит проба F7-44-edge) —
 *     «не выполнилось», условие названо происхождением набора;
 *   • близнец — тот же отказ с другой причиной — условие проходит;
 *   • контроль — происхождение принято, ключ заведён И снят тем же заходом.
 *
 * Стенд и браузер не нужны: край — сервер петли, единицы не берут `page`.
 *
 * Запуск: node scripts/access-key-origin-selftest.ts
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
  MODE_PATH,
  ORIGIN_CANARY,
  ORIGIN_CANARY_EDGE_ENV,
  ORIGIN_CANARY_OUT_ENV,
  type EdgeMode,
  type Outcome,
} from "./access-key-origin-canary/names.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const GATE = path.join(here, "..", "..", "..", ".github", "scripts", "assert-console-probes-verdict.py");

let checked = 0;
let failed = 0;
function check(ok: boolean, what: string): void {
  checked += 1;
  if (!ok) failed += 1;
  console.log(`  ${ok ? "ОК    " : "ПРОВАЛ"} ${what}`);
}

// ─── КРАЙ ПЕТЛИ ─────────────────────────────────────────────────────────────
// Подмножество поверхности края, которое трогает посев К: «кто я», выдача
// испытания регистрации, приём результата регистрации, опрос операции, снятие
// ключа. Режим ставит единица печеньем своего посева — до посева К.

const USER = "usr-origin-canary";
const KEY = "ak-origin-canary";
/** Текст отказа происхождения — тот, что утверждает живая проба F7-44-edge. */
const ORIGIN_REFUSAL_TEXT = "credential origin is not in the declared list";

const revoked: string[] = [];

const edge = http.createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://loop");
  const send = (status: number, body: unknown, headers: Record<string, string> = {}) => {
    res.writeHead(status, { "Content-Type": "application/json", ...headers });
    res.end(JSON.stringify(body));
  };
  const mode = /(?:^|;\s*)canary-mode=([a-z-]+)/.exec(req.headers.cookie ?? "")?.[1] as EdgeMode | undefined;
  req.resume();
  req.on("end", () => {
    if (req.method === "GET" && url.pathname.startsWith(MODE_PATH)) {
      const m = url.pathname.slice(MODE_PATH.length);
      return send(200, {}, { "Set-Cookie": `canary-mode=${m}; Path=/` });
    }
    if (req.method === "GET" && url.pathname === "/iam/v1/auth/me") return send(200, { user: { id: USER } });
    if (req.method === "POST" && url.pathname === `/iam/v1/users/${USER}/accessKeys:beginRegistration`) {
      return send(200, {
        challenge: Buffer.alloc(32, 7).toString("base64"),
        rp: { id: "127.0.0.1" },
        user: { id: Buffer.from(USER).toString("base64") },
      });
    }
    if (req.method === "POST" && url.pathname === `/iam/v1/users/${USER}/accessKeys`) {
      if (mode === "refuse-origin") {
        return send(400, {
          code: 3,
          message: ORIGIN_REFUSAL_TEXT,
          details: [
            { "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "ORIGIN_NOT_ALLOWED", domain: "iam.kacho.cloud" },
          ],
        });
      }
      if (mode === "refuse-other") {
        return send(400, {
          code: 3,
          message: "credential attestation is not accepted",
          details: [
            { "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "ATTESTATION_INVALID", domain: "iam.kacho.cloud" },
          ],
        });
      }
      if (mode === "accept") {
        return send(200, { id: "op-reg", done: true, response: { accessKey: { id: KEY } }, metadata: { accessKeyId: KEY } });
      }
      return send(500, { code: 13, message: `режим края петли не поставлен: ${String(mode)}` });
    }
    if (req.method === "DELETE" && url.pathname === `/iam/v1/users/${USER}/accessKeys/${KEY}`) {
      revoked.push(String(mode));
      return send(200, { id: "op-del", done: true, response: {} });
    }
    return send(404, { code: 5, message: `нет пути ${req.method} ${url.pathname}` });
  });
});

function listen(server: http.Server): Promise<string> {
  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve(`http://127.0.0.1:${(server.address() as AddressInfo).port}`));
  });
}

// ─── ПРОЦЕСС КАНАРЕЙКИ ──────────────────────────────────────────────────────

type Classified = { title: string; status: string; runs: number; unmet: string; message: string };

async function runCanary(at: string, out: string): Promise<{ code: number | null; tail: string }> {
  const cli = createRequire(import.meta.url).resolve("@playwright/test/cli");
  const config = path.join(here, "access-key-origin-canary", "canary.playwright.config.ts");
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [cli, "test", "--config", config], {
      cwd: path.join(here, ".."),
      env: { ...process.env, [ORIGIN_CANARY_EDGE_ENV]: at, [ORIGIN_CANARY_OUT_ENV]: out },
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

/** Исход каждой записи отчёта — НАСТОЯЩИМ распознавателем гейта вердикта. */
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
  const at = await listen(edge);
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "kacho-origin-canary-"));
  try {
    console.log(`край петли ${at}`);
    console.log(`распознаватель — ${path.relative(path.join(here, "..", "..", ".."), GATE)} (outcomes, fixture_unmet)`);
    const { code, tail } = await runCanary(at, out);
    const report = path.join(out, "report.json");
    if (!fs.existsSync(report)) {
      check(false, `процесс канарейки не оставил отчёта (код ${code}) — «не выполнилось»:\n${tail}`);
    } else {
      const units = classify(report);
      const declared = Object.values(ORIGIN_CANARY);
      console.log(`=== единиц в отчёте ${units.length}, объявлено ${declared.length} ===`);
      check(
        units.length === declared.length,
        `единиц канарейки исполнено ${units.length} из ${declared.length}: ` +
          (units.map((u) => `«${u.title}» ${u.status}`).join(", ") || "—"),
      );
      for (const { title, want } of declared) {
        const u = units.find((x) => x.title === title);
        const got = u ? outcomeOf(u) : "нет в отчёте";
        // Условие названо ПРОИСХОЖДЕНИЕМ набора и пунктом приёмки: иначе
        // «не выполнилось» могло прийти от чужого условия той же единицы.
        const named = want !== "unmet" || ((u?.unmet ?? "").includes(at) && (u?.unmet ?? "").includes("§4 П4"));
        const why = u && got !== want ? ` · отказ: ${u.message.split("\n")[0].slice(0, 240)}` : "";
        check(
          got === want && named,
          `${title} (ждали ${want}, получили ${got}${want === "unmet" ? `; условие: ${u?.unmet || "не названо"}` : ""})${why}`,
        );
      }
      // Контроль снимает свой ключ тем же заходом, отказы — ключа не заводят.
      check(
        JSON.stringify(revoked) === JSON.stringify(["accept"]),
        `ключ снят ровно у контроля: снятий ${JSON.stringify(revoked)}`,
      );
    }
  } finally {
    fs.rmSync(out, { recursive: true, force: true });
    await new Promise<void>((r) => edge.close(() => r()));
  }
  console.log(`проверок ${checked} · провалено ${failed}`);
  if (failed > 0 || checked === 0) process.exit(1);
}

await main();
