// Сигнал без показа — молчание. Проба держит вторую половину нормы.
//
// # Класс
//
// Механизм сигнала может быть исправен целиком — разбор ответа, опрос операции,
// текст сообщения — и не показать НИЧЕГО, если в модуле не примонтирован
// `Toaster`. Очередь тостов это модульный синглтон: `toast.error()` кладёт запись
// и возвращает управление, а читает её только подписанный показ. Нет показа —
// запись оседает в памяти, и отличить это от «сигнала не было» на экране нельзя.
//
// Наблюдалось: модуль `system` не нёс показа вовсе, при том что маршрутизирует
// общие страницы создания/правки/удаления (`/system/regions/create` и соседние).
// Отказ края (403) доезжал до `toast.error` и не был виден никак — «ничего не
// происходит, но выдаёт 403», находка владельца 2026-08-15.
//
// # Почему проба читает ДЕРЕВО, а не рендер
//
// Утверждение здесь — про набор модулей, а не про один компонент: смонтировать
// девять приложений в jest дороже и хрупче, чем прочитать их исходники, а
// предмет («в каждом мутирующем модуле есть показ») — свойство дерева. Тот же
// приём, которым проверяется объявление чарта в `deploy`-пробах.
//
// # Чего проба НЕ утверждает
//
// Что показ примонтирован в правильном месте дерева компонентов. Это видно
// глазом и держится обзором; здесь ловится случай, который глазом НЕ виден —
// отсутствие показа целиком.

import { readdirSync, readFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";

/**
 * Корень консоли ищется ВВЕРХ от рабочего каталога, а не собирается из `__dirname`:
 * суита исполняется как ESM, где `__dirname` не определён, и прогон падал бы
 * поломкой разбора — то есть «не выполнилось», а не вердиктом.
 *
 * Признак корня — каталог, в котором лежит и `shared`, и хотя бы один модуль.
 */
function findUiRoot(): string {
  let dir = process.cwd();
  for (let i = 0; i < 6; i++) {
    if (existsSync(join(dir, "shared", "src")) && existsSync(join(dir, "vpc", "src"))) return dir;
    const up = dirname(dir);
    if (up === dir) break;
    dir = up;
  }
  throw new Error(`корень консоли не найден вверх от ${process.cwd()} — проба не знает, что читать`);
}

const UI_ROOT = findUiRoot();

/**
 * Модули, которые мутируют и потому обязаны показывать сигнал.
 *
 * `host` и `dashboard` сюда не входят намеренно: они собираются из своего дерева
 * (их образ не копирует `shared/`) и мутирующих страниц не несут. Если это
 * изменится, их надо внести сюда — и об этом скажет перепись ниже, а не память.
 */
const MUTATING_MODULES = ["vpc", "iam", "compute", "storage", "nlb", "registry", "system"] as const;

function sourceFilesOf(module: string): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) {
        if (e.name === "node_modules") continue;
        walk(p);
      } else if (/\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) {
        out.push(p);
      }
    }
  };
  const src = join(UI_ROOT, module, "src");
  if (existsSync(src)) walk(src);
  return out;
}

/** Рендерится ли `<Toaster` хотя бы в одном не-тестовом исходнике модуля. */
function rendersToaster(module: string): boolean {
  return sourceFilesOf(module).some((f) => readFileSync(f, "utf8").includes("<Toaster"));
}

describe("показ уведомлений примонтирован в каждом мутирующем модуле", () => {
  // Перепись: «все несут показ» обязано быть отличимо от «ничего не прочитано».
  it("исходники модулей прочитаны", () => {
    const counts = MUTATING_MODULES.map((m) => sourceFilesOf(m).length);
    for (const [i, n] of counts.entries()) {
      expect({ module: MUTATING_MODULES[i], files: n }).toEqual({
        module: MUTATING_MODULES[i],
        files: expect.any(Number),
      });
      expect(n).toBeGreaterThan(0);
    }
  });

  it("ни одного мутирующего модуля без показа", () => {
    const silent = MUTATING_MODULES.filter((m) => !rendersToaster(m));
    expect(silent).toEqual([]);
  });

  /**
   * Положительный контроль к предыдущему: без него «ни одного без показа» было бы
   * выполнено и предикатом, который всегда отвечает «да». Модуль, показа не
   * несущий, обязан быть распознан именно как такой.
   */
  it("предикат отличает модуль без показа", () => {
    expect(rendersToaster("dashboard")).toBe(false);
  });
});
