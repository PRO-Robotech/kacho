import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { stripComments } from "./strip-comments";

/**
 * Гейт на ЗАИМСТВОВАННУЮ СХЕМУ ИМЕНОВАНИЯ в непробном коде консоли.
 *
 * Ядро #2 запрещает упоминания чужих облаков. Проверка на БРЕНД этого запрета не
 * закрывает: названий чужих платформ в дереве ноль, а заимствование прошло уровнем
 * ниже — на схеме. Подсказка в поле ввода показывала администратору образец
 * идентификатора региона в схеме чужой платформы, и по этому образцу он называл бы
 * СВОИ регионы; `id` неизменяем на всю жизнь ресурса (ядро #15), то есть ошибка
 * навсегда. Рядом имя по умолчанию для подсети собиралось со словом чужой
 * терминологии, тогда как ресурс в контракте Kachō называется иначе.
 *
 * Предмет гейта — не слово, а КЛАСС: схема идентификатора, отраслевое слово чужой
 * таксономии, продуктовое имя чужой платформы. Поэтому запись несёт не только
 * образец, но и то, чем это называется у нас: находка обязана говорить, что писать
 * вместо, иначе её снимут как непонятную.
 *
 * Что гейт читает: ИСПОЛНЯЕМУЮ часть. Комментарий, объясняющий, почему схема чужая
 * и как её отличить, — это разбор, а не подсказка пользователю; падать на нём
 * значило бы запретить объяснять. Отсюда `stripComments` для TypeScript и снятие
 * `<!-- -->` для страниц входа.
 *
 * Перепись: число осмотренных приложений, файлов и байт печатается в имени пробы и
 * утверждается ненулевым — «ноль находок» обязано быть отличимо от «ноль
 * прочитанного». Смещённый корень или переименованная раскладка роняют гейт громко,
 * а не проходят вхолостую.
 *
 * Собственная предпосылка (вторая проба): каждый образец класса ищется в синтетике
 * и обязан находиться, а законный близнец той же формы — имя по схеме Kachō, наш
 * заголовок без чужого уточнения — обязан молчать. Без второй половины гейт ловил
 * бы форму, а не существо, и первый же ложный срабат его отключил бы.
 */

const repoRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);

interface BorrowedScheme {
  /** Короткое имя класса — оно печатается в находке. */
  id: string;
  /** Что именно заимствовано (в тексте находки — после двоеточия). */
  what: string;
  /** Чем это называется в Kachō — находка обязана назвать замену. */
  instead: string;
  /** Признак в исполняемой части. */
  pattern: RegExp;
  /** Строка, на которой класс обязан находиться (проба собственной предпосылки). */
  sample: string;
  /** Законный близнец той же формы — на нём гейт обязан молчать. */
  twin: string;
}

const SCHEMES: readonly BorrowedScheme[] = [
  {
    id: "geo-id-scheme",
    what: "схема идентификатора региона/зоны чужой платформы",
    instead:
      "собственная схема Kachō — `region-1` / `region-1-a` (документация geo)",
    pattern: /\bru-central\d/i,
    sample: 'placeholder: "ru-central1-a"',
    twin: 'placeholder: "region-1-a"',
  },
  {
    id: "geo-id-scheme-foreign-regions",
    what: "схема кода региона чужой платформы (континент-сторона-номер)",
    instead:
      "собственная схема Kachō — `region-1` / `region-1-a` (документация geo)",
    pattern:
      /\b(?:us|eu|ap|sa|ca|me|af)-(?:east|west|north|south|central|northeast|northwest|southeast|southwest)-\d\b/i,
    sample: 'const fallbackRegion = "eu-north-1";',
    twin: 'const fallbackRegion = "region-1";',
  },
  {
    id: "subnet-term",
    what: "слово чужой терминологии для ресурса, который в контракте Kachō называется Subnet",
    instead: "`subnet` — имя ресурса в контракте Kachō",
    pattern: /\bsubnetwork/i,
    sample: "return `subnetwork-${n}`;",
    twin: "return `subnet-${n}`;",
  },
  {
    id: "product-title",
    what: "продуктовое имя чужой платформы вместо имени домена Kachō",
    instead: "имя домена Kachō — Compute · Storage · Registry",
    pattern:
      /\b(?:Compute Cloud|Container Registry|Object Storage|Managed Service for)\b/,
    sample: 'serviceTitle: "Compute Cloud",',
    twin: 'serviceTitle: "Compute",',
  },
  {
    id: "load-balancer-taxonomy",
    what: "уточнение при балансировщике, отличающее его от типов, которых у Kachō НЕТ",
    instead:
      "`Load Balancer` / «Балансировщики нагрузки» — тип у нас один, отличать не от чего",
    pattern: /\b(?:Network|Application|Gateway) Load Balanc(?:er|ing)\b/,
    sample: 'serviceTitle: "Network Load Balancer",',
    twin: 'serviceTitle: "Load Balancer",',
  },
  {
    id: "machine-type-scheme",
    what: "схема имени типа машины чужой платформы",
    instead:
      "собственная схема Kachō — имена типов машин задаёт каталог compute",
    pattern:
      /\b(?:standard-v\d|e2-(?:micro|small|medium|standard)|n1-standard|t[234]\.(?:nano|micro|small|medium|large)|m5\.[a-z]+)\b/,
    sample: 'placeholder: "e2-micro"',
    twin: 'placeholder: "m-2-8"',
  },
] as const;

/** Каталоги верхнего уровня, которые приложениями этого дерева не являются. */
const NOT_APPS = new Set([
  "node_modules",
  "deploy",
  "docs",
  "scripts",
  "e2e",
  ".git",
]);

/** Каждый каталог верхнего уровня, несущий `src/`, — приложение; выводится из дерева. */
function discoverApps(): string[] {
  return readdirSync(repoRoot)
    .filter((name) => !NOT_APPS.has(name))
    .filter((name) => {
      const dir = path.join(repoRoot, name);
      try {
        return (
          statSync(dir).isDirectory() &&
          statSync(path.join(dir, "src")).isDirectory()
        );
      } catch {
        return false;
      }
    })
    .sort();
}

/** Пробный файл: гейт судит прод-код, синтетика проб под него не подпадает. */
function isTestFile(file: string): boolean {
  const rel = file.split(path.sep).join("/");
  return /\.test\.tsx?$/.test(rel) || /\/src\/test\//.test(rel);
}

function walk(dir: string, acc: string[]): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, acc);
    else if (/\.(tsx?|html)$/.test(entry.name)) acc.push(full);
  }
  return acc;
}

/** Исполняемая часть: TypeScript — без комментариев, страница входа — без `<!-- -->`. */
function executablePart(file: string, src: string): string {
  return file.endsWith(".html")
    ? src.replace(/<!--[\s\S]*?-->/g, "")
    : stripComments(src);
}

interface Finding {
  file: string;
  line: number;
  scheme: BorrowedScheme;
  text: string;
}

function scan(): {
  findings: Finding[];
  apps: string[];
  files: number;
  bytes: number;
} {
  const apps = discoverApps();
  const findings: Finding[] = [];
  let files = 0;
  let bytes = 0;

  for (const app of apps) {
    const roots = [path.join(repoRoot, app, "src")];
    const indexHtml = path.join(repoRoot, app, "index.html");
    const candidates: string[] = [];
    for (const root of roots) walk(root, candidates);
    try {
      if (statSync(indexHtml).isFile()) candidates.push(indexHtml);
    } catch {
      /* у приложения может не быть страницы входа */
    }

    for (const file of candidates) {
      if (isTestFile(file)) continue;
      const src = readFileSync(file, "utf8");
      files += 1;
      bytes += src.length;
      const code = executablePart(file, src);
      const lines = code.split("\n");
      for (const scheme of SCHEMES) {
        lines.forEach((text, i) => {
          if (scheme.pattern.test(text)) {
            findings.push({
              file: path.relative(repoRoot, file),
              line: i + 1,
              scheme,
              text: text.trim(),
            });
          }
        });
      }
    }
  }
  return { findings, apps, files, bytes };
}

const result = scan();

describe("заимствованные схемы именования в консоли", () => {
  it(`осмотрено: приложений ${result.apps.length}, файлов ${result.files}, байт ${result.bytes} — перепись непуста`, () => {
    expect(result.apps.length).toBeGreaterThan(5);
    expect(result.files).toBeGreaterThan(100);
    expect(result.bytes).toBeGreaterThan(100_000);
  });

  it("непробный код консоли не показывает пользователю чужую схему именования", () => {
    const report = result.findings.map(
      (f) =>
        `${f.file}:${f.line} [${f.scheme.id}] ${f.scheme.what}\n    строка: ${f.text}\n    вместо: ${f.scheme.instead}`,
    );
    expect(report).toEqual([]);
  });
});

describe("собственная предпосылка гейта", () => {
  it.each(SCHEMES.map((s) => [s.id, s] as const))(
    "%s: образец ловится, законный близнец молчит",
    (_id, scheme) => {
      expect(scheme.pattern.test(scheme.sample)).toBe(true);
      expect(scheme.pattern.test(scheme.twin)).toBe(false);
    },
  );

  it("разбор снимает комментарий и сохраняет строковый литерал", () => {
    expect(
      executablePart(
        "x.ts",
        "// ru-central1 — схема чужой платформы\nconst a = 1;",
      ),
    ).not.toMatch(/ru-central/);
    expect(executablePart("x.ts", 'const a = "ru-central1";')).toMatch(
      /ru-central/,
    );
    expect(
      executablePart(
        "x.html",
        "<!-- Container Registry -->\n<title>Kachō</title>",
      ),
    ).not.toMatch(/Container Registry/);
  });
});
