// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { jest } from "@jest/globals";
import { stubNetworkOnce } from "./issuance-guard";

/**
 * Сеть на одну пробу — на место `jest.spyOn(global, "fetch").mockImplementation(…)`.
 *
 * Шпион подменял САМ `fetch` окна и тем снимал страж исполнения мест выпуска со
 * всех вызовов пробы (`issuance-guard.ts`): обращение мимо упорядочивающего
 * транспорта доходило до заменителя так же, как законное. Здесь заменитель
 * встаёт ПОД стражем и получает только то, что выпустил упорядочивающий
 * транспорт; после пробы общее окружение возвращает сеть по умолчанию.
 *
 * Возвращает `jest.fn` — суждения о нём прежние (`mock.calls`,
 * `toHaveBeenCalledWith`); те же суждения о `global.fetch` судят его же.
 */
export function stubNetwork(impl: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>) {
  const mock = jest.fn<typeof fetch>(impl);
  stubNetworkOnce(globalThis, mock);
  return mock;
}
