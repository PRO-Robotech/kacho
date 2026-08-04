// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Конфигурация линта для scripts/ — исполняемой оснастки ui-future (гейты, проверки).
//
// Отдельный пакет по той же причине, по которой отдельный у shared: базовый путь ESLint —
// каталог конфигурации, поэтому ни одно приложение эти файлы не видит. Гейт покрытия
// (check-lint-coverage.mjs) считает scripts/ полноправным пакетом и покраснеет, если
// конфигурации здесь не станет, — то есть собственная оснастка судится тем же правилом,
// что и продуктовый код.
//
// Правил меньше, чем у приложений, и это не послабление, а другой предмет: здесь нет
// React, нет JSX и нет TypeScript-проекта, поэтому type-aware и react/jsx-a11y-правила
// не имеют, к чему применяться.

import js from "@eslint/js";
import globals from "globals";

export default [
  js.configs.recommended,
  {
    files: ["**/*.mjs"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.node,
      },
    },
    rules: {
      "no-console": "off", // гейт обязан печатать объём осмотренного и находки
      "no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
    },
  },
];
