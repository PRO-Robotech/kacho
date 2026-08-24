import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

import { isTestFile } from "./module-reachability";

/**
 * Обходчик НОСИТЕЛЕЙ ЦЕРЕМОНИИ ЛИЧНОСТИ и их рендера продуктом.
 *
 * Вынесен из пробы отдельным файлом ровно затем же, зачем вынесен обходчик
 * достижимости: им пользуется и проба инъекции, строящая синтетическое дерево.
 * Без общего кода обе стороны мерили бы разными линейками, и зелень гейта над
 * настоящим деревом не говорила бы о его работоспособности ничего.
 *
 * ЧЕМ ЭТОТ ВОПРОС ОТЛИЧАЕТСЯ ОТ ДОСТИЖИМОСТИ. Гейт достижимости идёт по
 * ИМПОРТАМ и `shared` исключает осознанно. Импорт-достижимость здесь и не
 * годится: барель `export * from "./auth"` делает «достижимым» то, чего продукт
 * не рендерит, — именно так семь страниц церемоний и прожили незамеченными.
 * Предикат тут другой: РЕНДЕР, то есть узел JSX.
 */

/** Каталоги верхнего уровня `ui-future`, которые исходниками продукта не являются. */
const NOT_SOURCE = new Set(["node_modules", "deploy", "docs", "scripts", ".git", "e2e"]);

/**
 * Чем опознаётся носитель церемонии: импорт библиотеки ПРОТОКОЛА поставщика
 * личности — единственной двери к его потокам в дереве.
 *
 * Почему протокол, а не обращения к ручкам поставщика: последние берут и те,
 * кто церемонию не ведёт вовсе (гейты прав читают ими состав доступа). Широкий
 * предикат давал ложную находку на таком гейте — а гейт, у которого находки
 * ложные, перестают читать.
 */
export const PROVIDER_PROTOCOL = "@shared/lib/kratos";

export interface CeremonyCarrier {
  file: string;
  rel: string;
  components: string[];
}

export interface CeremonyCensus {
  productFiles: string[];
  carriers: CeremonyCarrier[];
  rendered: CeremonyCarrier[];
  orphaned: CeremonyCarrier[];
}

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (NOT_SOURCE.has(name)) continue;
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(name) && !isTestFile(full)) acc.push(full);
  }
  return acc;
}

/** Компоненты, объявленные файлом: имя с заглавной у `export function|const|class`. */
function exportedComponents(text: string): string[] {
  const found = new Set<string>();
  for (const m of text.matchAll(/export\s+(?:async\s+)?(?:function|const|class)\s+([A-Z][\w$]*)/g)) {
    found.add(m[1]);
  }
  return [...found];
}

export function walkCeremonyCarriers(uiRoot: string): CeremonyCensus {
  const sharedSrc = path.join(uiRoot, "shared", "src");
  const productFiles = sourceFiles(uiRoot);

  /** Имя → файлы, где оно встречается УЗЛОМ JSX. */
  const renderedIn = new Map<string, Set<string>>();
  for (const file of productFiles) {
    const text = readFileSync(file, "utf8");
    for (const m of text.matchAll(/<\s*([A-Z][\w$]*)[\s/>]/g)) {
      if (!renderedIn.has(m[1])) renderedIn.set(m[1], new Set());
      renderedIn.get(m[1])!.add(file);
    }
  }

  const carriers: CeremonyCarrier[] = [];
  for (const file of sourceFiles(sharedSrc)) {
    const text = readFileSync(file, "utf8");
    if (!text.includes(`"${PROVIDER_PROTOCOL}"`)) continue;
    const components = exportedComponents(text);
    if (components.length === 0) continue;
    carriers.push({ file, rel: path.relative(uiRoot, file), components });
  }

  const carrierFiles = new Set(carriers.map((c) => c.file));

  /**
   * Живость считается ДО НЕПОДВИЖНОЙ ТОЧКИ, а не одним проходом.
   *
   * Один проход объявил бы живым носитель, которого поднимают только мёртвые
   * соседи: страница входа отдавала обёртку, и её рендерили страницы
   * восстановления, регистрации и параметров — все мертвы вместе. Такой взаимный
   * рендер («мёртвая гроздь») делает предикат слепым ровно к тому, ради чего он
   * заведён.
   *
   * Основание отсчёта — файлы ПРИЛОЖЕНИЙ: страницу монтирует приложение, а не
   * другая страница библиотеки, поэтому рендер одной страницы `shared` другой
   * такой же свидетельством живости не является. Без этого мёртвая страница, не
   * ведущая церемонию сама, удерживала бы живой обёртку страницы входа — и та не
   * попала бы в находки (наблюдалось при заведении гейта: находок было 3 из 4).
   */
  const sharedPages = path.join(sharedSrc, "pages") + path.sep;
  const liveFiles = new Set(
    productFiles.filter((f) => !carrierFiles.has(f) && !f.startsWith(sharedPages)),
  );
  for (;;) {
    const grown = carriers.filter(
      (c) =>
        !liveFiles.has(c.file) &&
        c.components.some((comp) =>
          [...(renderedIn.get(comp) ?? [])].some((f) => f !== c.file && liveFiles.has(f)),
        ),
    );
    if (grown.length === 0) break;
    for (const c of grown) liveFiles.add(c.file);
  }

  return {
    productFiles,
    carriers,
    rendered: carriers.filter((c) => liveFiles.has(c.file)),
    orphaned: carriers.filter((c) => !liveFiles.has(c.file)),
  };
}
