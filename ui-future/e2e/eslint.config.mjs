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
// Набор БЕЗ type-aware правил, в отличие от девяти приложений. Прежняя редакция
// объясняла это отсутствием tsconfig и обещала поднять набор, когда тот появится.
// TSCONFIG ПОЯВИЛСЯ (`./tsconfig.json`, работа `typecheck` в `ui.yml`), поэтому
// обещание переписано на то, что верно сегодня: подъём набора до
// `recommendedTypeChecked` — ОТДЕЛЬНЫЙ предмет со своей переписью находок, и
// делать его побочным эффектом правки, чинившей красную пробу, значило бы
// смешать два предмета в одном изменении.
//
// Чего этот набор НЕ ловит и не может: `typescript-eslint` в `recommended`
// СНИМАЕТ правило `no-undef` на файлах TypeScript — ровно потому, что его работу
// делает `tsc`. Пока `tsc` по пакету не ходил, неразрешимое имя не ловил никто:
// так `ReferenceError` из #601 доехал до браузера. Разрешимость имён держит
// теперь `tsconfig.json`, а не этот файл, — и это записано здесь, чтобы
// следующий читатель не принял зелёный линт за проверку типов.
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
    // Скрипты оснастки прогона — тот же Node, но расширение .mjs: без этой
    // записи их не покрывает НИ один блок, и `no-undef` объявляет `process`
    // неразрешимым именем. Записаны отдельно от проб, потому что предмет у
    // них другой: они не утверждают о продукте, а создают условие прогона.
    files: ["scripts/**/*.mjs"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: { ...globals.node },
    },
    rules: {
      "no-console": "off",
    },
  },
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
