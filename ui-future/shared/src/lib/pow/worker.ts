// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// `Worker` решателя вызова края (замысел `issue-2917` З10, CX2-30).
//
// Один `Worker` — одна отправка формы: главный поток создаёт его на вызов,
// присылает ОДИН запрос и завершает (`terminate()`) по ответу, по новой
// отправке, по размонтированию экрана и по исчерпании бюджета. Своих часов и
// своего срока здесь нет — бюджет отсчитывает главный поток. Сети нет.
//
// Ответ несёт поля запроса целиком: по номеру отправки главный поток отбрасывает
// ответ, пришедший не своей отправке.

import { solve, type SolveAnswer, type SolveRequest } from "./pow";

interface SolverScope {
  onmessage: ((ev: MessageEvent<SolveRequest>) => void) | null;
  postMessage(message: SolveAnswer): void;
}

const scope = self as unknown as SolverScope;

scope.onmessage = (ev) => {
  const request = ev.data;
  scope.postMessage({ ...request, nonce: solve(request.challenge, request.difficultyBits) });
};
