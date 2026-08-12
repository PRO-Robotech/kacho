// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Конфигурация линта для e2e/ — сквозных проб консоли через браузер.
//
// Отдельный пакет по той же причине, что scripts/ и shared/: базовый путь ESLint —
// каталог конфигурации, поэтому ни одно приложение эти файлы не видит, и без
// собственной конфигурации пакет не линтуется НИЧЕМ. Гейт покрытия
// (scripts/check-lint-coverage.mjs) считает e2e полноправным пакетом и краснеет,
// если конфигурации здесь не станет, — проба судится тем же правилом, что и код,
// который она проверяет.
//
// Почему набор БЕЗ type-aware правил, в отличие от девяти приложений: у них есть
// tsconfig.json, у e2e его нет — playwright разбирает TypeScript сам, проектом
// пакет не описан. `recommendedTypeChecked` требует программы TypeScript и без
// tsconfig либо падает, либо тихо не применяется. Заводить tsconfig ради линта —
// отдельный предмет с собственным решением, а не побочный эффект этой правки.
// Поэтому здесь ровно то, что реально исполняется: рекомендованный набор без
// типов. Появится tsconfig — набор поднимается до type-checked вместе с ним.
//
// React-плагинов нет и не должно быть: браузером здесь управляют снаружи, JSX в
// пакете отсутствует, поэтому применяться им не к чему.

import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["node_modules/**", "test-results/**", "playwright-report/**", "results.json"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.ts"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        // Пробы исполняются в Node (playwright-runner), а не в браузере: обращения
        // к странице идут через переданный объект page, а не через глобальный window.
        ...globals.node,
      },
    },
    rules: {
      // Проба обязана печатать, что она делала: вердикт без объёма осмотренного
      // неотличим от «ничего не выполнялось».
      "no-console": "off",
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],
    },
  },
);
