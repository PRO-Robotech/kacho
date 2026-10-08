// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Дублёр `Worker` решателя вызова края — для проб клиента полосы и экранов
 * (приёмка NTF-2, Р5; замысел `issue-2917` З10, CX2-30).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ДУБЛЁР ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ
 *
 * В `jsdom` своего `Worker` нет, а предмет проб — СРОК ЖИЗНИ решателя и то, что
 * клиент делает с его ответом: сколько решателей создано, завершён ли каждый,
 * ушёл ли повтор с доказательством. Поэтому дублёр подменяет ровно конструктор
 * `Worker` и записывает каждую его жизнь; когда отвечать, решает проба
 * (`answer()`), а не время.
 *
 * Ответ дублёра — НАСТОЯЩЕЕ решение: `nonce` ищется перебором по SHA-256 из
 * `node:crypto`, независимой от решателя консоли реализацией, поэтому повтор с
 * доказательством, собранный клиентом из этого ответа, проходит ту же проверку,
 * что у края. Дублёр не снисходительнее настоящего `Worker`: завершённый
 * (`terminate()`) не отвечает ни на что — как и настоящий.
 *
 * Протокол сообщений дублёр знает только в той мере, в какой его требует замысел:
 * запрос к решателю несёт поля `challenge` и `difficultyBits` вызова; ответ — те
 * же поля запроса целиком (что бы клиент в них ни положил, например номер
 * отправки) и найденный `nonce`. Запрос без этих двух полей — отказ ДУБЛЁРА с
 * названным полем, а не молчаливое «решения нет».
 */

export interface PowWorkerLife {
  /** Аргументы конструктора — как их передал клиент. */
  readonly args: readonly unknown[];
  /** Сообщения, отправленные клиентом решателю, по порядку. */
  readonly posted: unknown[];
  /** Завершён ли решатель клиентом. */
  terminated: boolean;
  /** Ответить настоящим решением на последнее сообщение клиента. Завершённый не отвечает. */
  answer(): void;
}

/** Решение вызова: первый `nonce` (десятичной строкой), дающий не меньше `bits` ведущих нулевых битов. */
export function solveIndependently(challenge: string, bits: number): string {
  for (let n = 0; ; n++) {
    const nonce = String(n);
    if (leadingZeroBitsIndependently(challenge, nonce) >= bits) return nonce;
  }
}

/** Число ведущих нулевых битов SHA-256 от `challenge + ":" + nonce` — по `node:crypto`. */
export function leadingZeroBitsIndependently(challenge: string, nonce: string): number {
  const digest = createHash("sha256").update(`${challenge}:${nonce}`, "utf8").digest();
  let zeros = 0;
  for (const byte of digest) {
    if (byte === 0) {
      zeros += 8;
      continue;
    }
    zeros += Math.clz32(byte) - 24;
    break;
  }
  return zeros;
}

function field(message: unknown, name: "challenge" | "difficultyBits"): unknown {
  if (!message || typeof message !== "object" || !(name in message)) {
    throw new Error(
      `дублёр Worker: сообщение решателю не несёт поля \`${name}\` вызова — ` +
        `решать нечего (получено ${JSON.stringify(message)})`,
    );
  }
  return (message as Record<string, unknown>)[name];
}

/**
 * Поставить дублёр вместо `globalThis.Worker`. Возвращает перепись жизней
 * решателя и снятие дублёра.
 */
export function installPowWorker() {
  const lives: PowWorkerLife[] = [];
  const g = globalThis as unknown as { Worker?: unknown };
  const original = g.Worker;

  class FakeWorker {
    onmessage: ((ev: MessageEvent) => void) | null = null;
    onerror: ((ev: ErrorEvent) => void) | null = null;
    private readonly listeners: Array<(ev: MessageEvent) => void> = [];
    private readonly life: PowWorkerLife;

    constructor(...args: unknown[]) {
      const posted: unknown[] = [];
      const life: PowWorkerLife = {
        args,
        posted,
        terminated: false,
        answer: () => {
          if (life.terminated) return;
          const request = posted.at(-1);
          if (request === undefined) throw new Error("дублёр Worker: клиент не отправил решателю ни одного сообщения");
          const challenge = field(request, "challenge");
          // Край пишет `difficultyBits` строкой (`ErrorInfo.metadata` — карта строк);
          // клиент вправе передать решателю и число, и ту же строку.
          const rawBits = field(request, "difficultyBits");
          const bits = typeof rawBits === "string" && /^\d+$/.test(rawBits) ? Number(rawBits) : rawBits;
          if (typeof challenge !== "string" || typeof bits !== "number" || !Number.isInteger(bits)) {
            throw new Error(`дублёр Worker: поля вызова не той формы: ${JSON.stringify(request)}`);
          }
          const data = {
            ...(request as Record<string, unknown>),
            nonce: solveIndependently(challenge, bits),
          };
          const ev = { data } as MessageEvent;
          this.onmessage?.(ev);
          for (const l of this.listeners) l(ev);
        },
      };
      this.life = life;
      lives.push(life);
    }

    postMessage(message: unknown) {
      this.life.posted.push(message);
    }

    terminate() {
      this.life.terminated = true;
    }

    addEventListener(type: string, listener: (ev: MessageEvent) => void) {
      if (type === "message") this.listeners.push(listener);
    }

    removeEventListener(type: string, listener: (ev: MessageEvent) => void) {
      if (type !== "message") return;
      const i = this.listeners.indexOf(listener);
      if (i >= 0) this.listeners.splice(i, 1);
    }
  }

  g.Worker = FakeWorker;
  return {
    lives,
    restore: () => {
      g.Worker = original;
    },
  };
}

/**
 * Общий файл векторов края и консоли (замысел З10, CX2-30 (в)): одни входы и одни
 * ответы для проверки края (Go) и решателя консоли (TS), плюс срок вызова и
 * бюджет решателя. Читается из дерева, а не копией: копия разошлась бы с файлом
 * края молча.
 */
export interface PowVectors {
  challengeTtlSeconds: number;
  solverBudgetSeconds: number;
  vectors: Array<{
    challenge: string;
    nonce: string;
    leadingZeroBits: number;
    bits: number;
    accepted: boolean;
  }>;
}

export const POW_VECTORS_PATH = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../../../gateway/internal/middleware/anonmail/testdata/pow_vectors.json",
);

/**
 * Прочитать файл векторов и проверить САМ ФАЙЛ до любого вопроса к консоли:
 * сломанная фикстура не вправе выдать себя за отсутствующую возможность.
 */
export function loadPowVectors(): PowVectors {
  const raw = JSON.parse(readFileSync(POW_VECTORS_PATH, "utf8")) as PowVectors;
  if (!Number.isInteger(raw.challengeTtlSeconds) || !Number.isInteger(raw.solverBudgetSeconds)) {
    throw new Error(`фикстура: в ${POW_VECTORS_PATH} нет целых challengeTtlSeconds/solverBudgetSeconds`);
  }
  if (!Array.isArray(raw.vectors) || raw.vectors.length === 0) {
    throw new Error(`фикстура: в ${POW_VECTORS_PATH} нет векторов — пустой предмет зелёного не даёт`);
  }
  const accepted = raw.vectors.filter((v) => v.accepted).length;
  if (accepted === 0 || accepted === raw.vectors.length) {
    throw new Error(`фикстура: векторы обязаны нести обе стороны — принятых ${accepted} из ${raw.vectors.length}`);
  }
  for (const v of raw.vectors) {
    const measured = leadingZeroBitsIndependently(v.challenge, v.nonce);
    if (measured !== v.leadingZeroBits || measured >= v.bits !== v.accepted) {
      throw new Error(
        `фикстура: вектор ${v.challenge}:${v.nonce} не сходится с SHA-256 node:crypto — ` +
          `измерено ${measured} битов, в файле ${v.leadingZeroBits}, bits ${v.bits}, accepted ${v.accepted}`,
      );
    }
  }
  return raw;
}
