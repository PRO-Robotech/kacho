// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { describeIssuanceWiring } from "./issuance-wiring";

// F8-46 · подключение стража в окружении модуля, чьим прогоном исполняется
// общий пакет: семь модулей, чей `jest.config.cjs` несёт `../shared/src` в
// `roots`. Модуль — каталог прогона (`npm test --prefix <модуль>`), как у
// соседних проб общего пакета, читающих дерево от него. Устройство и граница —
// в шапке `issuance-wiring.ts`.
describeIssuanceWiring(process.cwd());
