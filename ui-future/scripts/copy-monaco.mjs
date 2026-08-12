// Кладёт редактор Monaco в `public/monaco/vs` пакета, откуда его отдаёт СВОЙ
// origin консоли.
//
// Зачем: `@monaco-editor/react` по умолчанию грузит редактор с внешнего CDN, а
// CSP консоли разрешает только `script-src 'self'` — браузер блокирует загрузку,
// и вкладка JSON навсегда остаётся в «Loading…». Наблюдалось на живом стенде
// 2026-08-12: вкладка не работала НИ У ОДНОГО ресурса. Ослаблять CSP ради
// стороннего домена нельзя, поэтому файлы едут вместе с образом.
//
// Каталог произведённый: он в `.gitignore` и пересоздаётся перед каждой сборкой.

import { cpSync, existsSync, mkdirSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const pkgDir = process.cwd();
const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

// Пакеты-workspace берут зависимость из корневого node_modules, standalone — из
// своего. Ищем в обоих, чтобы скрипт не зависел от способа установки.
const candidates = [
  path.join(pkgDir, "node_modules/monaco-editor/min/vs"),
  path.join(uiRoot, "node_modules/monaco-editor/min/vs"),
];
const src = candidates.find((p) => existsSync(p));

if (!src) {
  // Отказ, а не молчание: без файлов вкладка JSON собралась бы «успешно» и не
  // работала бы в рантайме — ровно то состояние, ради выхода из которого скрипт
  // и написан.
  console.error(
    "[copy-monaco] monaco-editor не найден ни в одном из:\n  " +
      candidates.join("\n  ") +
      "\nВкладка JSON без него не работает — сборка остановлена.",
  );
  process.exit(1);
}

const dest = path.join(pkgDir, "public/monaco/vs");
rmSync(dest, { recursive: true, force: true });
mkdirSync(path.dirname(dest), { recursive: true });
cpSync(src, dest, { recursive: true });
console.log(`[copy-monaco] ${path.relative(uiRoot, src)} → ${path.relative(uiRoot, dest)}`);
