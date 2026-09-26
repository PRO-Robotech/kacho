// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { fileURLToPath } from "node:url";
import { describeIssuanceWiring } from "@shared/test/issuance-wiring";

// F8-46 · подключение стража в окружении host: пакет общего набора не исполняет,
// поэтому судит своё окружение сам — той же пробой, что и семь остальных
// (`shared/src/test/issuance-wiring.ts`). Модуль — каталог этого файла, а не
// каталог прогона: окружение host надстраивается над общим, и именно цепочка
// его надстройки здесь и судится.
describeIssuanceWiring(path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../.."));
