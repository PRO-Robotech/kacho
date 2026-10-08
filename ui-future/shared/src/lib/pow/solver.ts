// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Срок жизни решателя вызова края — сторона главного потока (замысел
// `issue-2917` З10, CX2-30 (а, б); приёмка NTF-2, Р5, NTF2-58).
//
// ─────────────────────────────────────────────────────────────────────────────
// ОДНА ОТПРАВКА — ОДИН `Worker`
//
// Решатель принадлежит ОДНОЙ отправке формы. Он завершается (`terminate()`):
//   • по ответу — доказательство найдено;
//   • по новой отправке той же формы (`begin()`): прежняя отправка больше не
//     ждёт решения, её `Worker` снят сразу, а не по бюджету;
//   • по уходу экрана (`end()`);
//   • по исчерпании бюджета `Bs` — его отсчитывает таймер ЭТОГО потока, а не
//     часы внутри `Worker`: страница управляет своими часами, и только ими.
// Исход каждого случая — свой (`SolveOutcome`): «бюджет исчерпан» отличим от
// «отправку сменили», и клиент полосы поступает с ними по-разному.
//
// Ответ, пришедший не своей отправке либо после завершения, отбрасывается:
// сверяется номер отправки, а завершённый `Worker` не отвечает вовсе.

import { SOLVER_BUDGET_MS, type SolveAnswer, type SolveRequest } from "./pow";

/** Исход поиска доказательства для одной отправки. */
export type SolveOutcome =
  /** Доказательство найдено в бюджете. */
  | { kind: "solved"; nonce: string }
  /** Бюджет `Bs` исчерпан по таймеру главного потока: доказательства нет. */
  | { kind: "expired" }
  /** Решатель не исполнился (скрипт `Worker` не загрузился либо упал). */
  | { kind: "failed" }
  /** Отправку сменила новая либо экран ушёл: решение этой отправке не нужно. */
  | { kind: "superseded" };

interface Running {
  worker: Worker;
  settle: (outcome: SolveOutcome) => void;
}

/** Решатель одной формы: номер текущей отправки и её `Worker`, если он есть. */
export class ProofOfWorkSolver {
  private submission = 0;
  private running: Running | null = null;

  /**
   * Начать новую отправку формы: решатель прежней завершается, её ожидание
   * получает `superseded`. Возвращает номер новой отправки.
   */
  begin(): number {
    this.submission += 1;
    this.running?.settle({ kind: "superseded" });
    return this.submission;
  }

  /** Экран ушёл: решатель текущей отправки завершается, новых отправок не будет. */
  end(): void {
    this.submission += 1;
    this.running?.settle({ kind: "superseded" });
  }

  /** Решить вызов края для отправки `submission` — в своём `Worker`, в пределах бюджета. */
  solve(submission: number, challenge: string, difficultyBits: number): Promise<SolveOutcome> {
    if (submission !== this.submission) return Promise.resolve({ kind: "superseded" });
    return new Promise<SolveOutcome>((resolve) => {
      const worker = new Worker(new URL("./worker.ts", import.meta.url), {
        type: "module",
        name: "kacho-pow-solver",
      });
      const running: Running = {
        worker,
        settle: (outcome) => {
          if (this.running !== running) return;
          this.running = null;
          clearTimeout(timer);
          worker.terminate();
          resolve(outcome);
        },
      };
      // Таймер объявлен до первого возможного `settle`: тот зовут только он сам,
      // ответ `Worker` и новая отправка — все позже этой строки.
      const timer = setTimeout(() => running.settle({ kind: "expired" }), SOLVER_BUDGET_MS);
      this.running = running;
      worker.onmessage = (ev: MessageEvent<SolveAnswer>) => {
        const answer = ev.data;
        if (answer.submission !== submission || answer.challenge !== challenge) return;
        running.settle({ kind: "solved", nonce: answer.nonce });
      };
      worker.onerror = () => running.settle({ kind: "failed" });
      const request: SolveRequest = { submission, challenge, difficultyBits };
      worker.postMessage(request);
    });
  }
}
