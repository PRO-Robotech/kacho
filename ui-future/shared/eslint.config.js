// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Конфигурация линта пакета `shared`.
//
// Почему СВОЯ, а не расширение области девяти приложений: базовый путь ESLint — каталог
// конфигурации, поэтому `eslint .` из приложения отвергает ../shared целиком («File
// ignored because outside of base path»), а поднятие базового пути каждому из девяти
// заставило бы линтовать один и тот же общий код девять раз, девятикратно дублируя
// находки и не отвечая на вопрос, кто из приложений за них отвечает. Пакет `shared` —
// самостоятельный workspace-пакет; область его линта принадлежит ему.
//
// Набор ПРАВИЛ — побайтово тот же, что у host/dashboard: общий код потребляют все девять
// приложений, и судить его слабее потребителя нет основания. В частности сюда сознательно
// НЕ перенесён блок из семи остальных приложений, гасящий 27 правил для своего `src/**`, —
// иначе включение линта на shared было бы включением его имени, а не его предмета.
//
// Отличия от host/dashboard — ровно три, и все про состав пакета, а не про строгость:
//   • добавлен блок `**/*.cjs` (оснастка тестов на CommonJS, вне tsconfig → без type-aware);
//   • тестовому блоку добавлены node-глобали (тесты пакета читают дерево через node:fs/path/url);
//   • нет блока `vite.config.ts` — shared не собирается своим vite, такого файла у него нет.

import path from "node:path";
import { fileURLToPath } from "node:url";
import js from "@eslint/js";
import prettier from "eslint-config-prettier";
import importX from "eslint-plugin-import-x";
import jsxA11y from "eslint-plugin-jsx-a11y";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default tseslint.config(
  {
    // Тот же список, что у девяти приложений. `jest-singletons.cjs` сюда НЕ попадает
    // намеренно: это не файл конфигурации сборщика, а отслеживаемый исполняемый код
    // тестовой оснастки — он линтуется (блок commonjs ниже).
    ignores: [
      "dist/**",
      "build/**",
      "coverage/**",
      "node_modules/**",
      "package-lock.json",
      "*.config.js",
      "*.config.cjs",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      parserOptions: {
        projectService: true,
        tsconfigRootDir: __dirname,
      },
      globals: {
        ...globals.browser,
        ...globals.es2025,
      },
    },
    plugins: {
      "import-x": importX,
      "jsx-a11y": jsxA11y,
      react,
      "react-hooks": reactHooks,
    },
    settings: {
      react: {
        version: "detect",
      },
    },
    rules: {
      ...react.configs.recommended.rules,
      ...react.configs["jsx-runtime"].rules,
      ...reactHooks.configs.recommended.rules,
      ...jsxA11y.configs.recommended.rules,
      "import-x/first": "warn",
      "import-x/no-duplicates": "warn",
      "import-x/no-self-import": "error",
      "no-alert": "error",
      "no-console": ["warn", { allow: ["warn", "error"] }],
      "no-shadow": "off",
      "react/prop-types": "off",
      "react/jsx-no-target-blank": "error",
      "react/jsx-key": ["warn", { checkFragmentShorthand: true }],
      "react/jsx-no-duplicate-props": "warn",
      "react/no-unescaped-entities": "warn",
      "@typescript-eslint/consistent-type-imports": [
        "warn",
        {
          prefer: "type-imports",
          fixStyle: "separate-type-imports",
        },
      ],
      "@typescript-eslint/no-misused-promises": [
        "error",
        {
          checksVoidReturn: {
            attributes: false,
          },
        },
      ],
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
  {
    // Оснастка тестов на CommonJS (jest-singletons.cjs): свои модульная система и
    // глобали. Type-aware правила к ней не применяются — она вне tsconfig пакета.
    files: ["**/*.cjs"],
    ...tseslint.configs.disableTypeChecked,
    languageOptions: {
      sourceType: "commonjs",
      globals: {
        ...globals.node,
      },
    },
  },
  {
    // Тесты пакета читают дерево (node:fs / node:path / node:url) — им нужны и
    // node-глобали, а не только browser+jest, иначе `no-undef` красит `process`.
    files: ["**/*.test.{ts,tsx}", "src/test/**/*.{ts,tsx}"],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.jest,
        ...globals.node,
      },
    },
  },
  prettier,
);
