import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { stripComments } from "@shared/test/strip-comments";

/**
 * Гейт: внутренний словарь не выходит на поверхность, которую читает арендатор.
 *
 * # Класс
 *
 * Подсказка поля («Within-service FK → load_balancers»), текст отказа
 * («Требуется FGA-relation admin@cluster:…») и подпись кнопки говорили языком
 * реализации: имя таблицы базы, механика ссылочной целостности, формулировка
 * модели прав. Арендатору это не адресовано ни одним словом — он не заводил ни
 * таблиц, ни отношений, — а вместе с текстом отказа наружу уходит ещё и
 * устройство проверки.
 *
 * Находка тихая по построению: строка непуста, форма собирается, ни одна проба
 * не краснеет. На момент заведения гейта таких мест было **31** в шести пакетах.
 *
 * # Что утверждается
 *
 * Строковый литерал, стоящий значением у ключа или атрибута, который читает
 * пользователь (`description` · `placeholder` · `label` · `title` · `reason` ·
 * `subTitle` · `message` · `aria-label`), не содержит слов из словаря ниже.
 *
 * Читается ИСПОЛНЯЕМАЯ часть: комментарии снимаются, иначе гейт падал бы на
 * объяснении самого запрета — на этом файле в первую очередь. По той же причине
 * из области выведены пробы: разбор в пробе называет предмет и обязан это делать.
 *
 * # Чего гейт НЕ видит — названо, а не умолчано
 *
 * Текст, склеенный из переменных, и текст, приезжающий с сервера, он не
 * рассматривает: первого в этих ключах на момент заведения нет (склейка живёт в
 * `errorText`), второе — контракт сервиса, и правится на сервере. Три
 * задокументированных исключения `security.md` — комментарий у гейта, текст
 * отказа при старте и текст падения пробы — в область не входят by construction:
 * первое снимается разбором, второе живёт в Go, третье выведено вместе с пробами.
 *
 * # Объём осмотренного
 *
 * Числа прочитанных пакетов, файлов и извлечённых литералов утверждаются
 * непустыми: «ноль находок» обязано быть отличимо от «ноль прочитанного».
 * Извлекатель проверяется в обе стороны — он обязан найти запрещённое и обязан
 * не находить законного.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/**
 * Словарь реализации. Каждая запись — не «некрасивое слово», а имя механизма,
 * которого у арендатора нет: устройство хранения, устройство прав, имя вызова.
 */
const INTERNAL_LEXICON: { re: RegExp; why: string; onlyIn?: RegExp }[] = [
  { re: /Within-service|Cross-service|Same-DB/i, why: "механика ссылочной целостности" },
  { re: /\bFK\b/, why: "внешний ключ базы" },
  // Только внутри ПРОЗЫ: литерал, целиком состоящий из такого слова, — это
  // образец значения, которое пользователь и обязан ввести в этой форме (имя
  // роли ограничено `^[a-z][a-z0-9_]{0,40}$`). Запретить его значило бы
  // потребовать показать неверный образец; первая редакция правила требовала
  // именно этого, и поймано это было прогоном, а не чтением.
  {
    re: /\b[a-z][a-z0-9]*_[a-z0-9_]+\b/,
    onlyIn: /[\sЀ-ӿ]/,
    why: "имя таблицы или колонки базы в тексте",
  },
  { re: /\bimmutable\b/i, why: "по-русски — «неизменяем после создания»" },
  { re: /после Create\b|после Update\b|после Delete\b/, why: "имя RPC" },
  { re: /\bFGA\b|\buserset\b|AccessBinding|required_relation/i, why: "устройство модели прав" },
  { re: /\bKAC-\d+\b/, why: "внутренний номер задачи" },
  { re: /\bBackend\b/, why: "по-русски — «сервер»" },
  // Имя чужого продукта в подписи поля говорит арендатору неправду о том, чем он
  // входит, и заодно маскирует смысл самого поля: «идентификатор субъекта у
  // поставщика» — это про роль значения, а не про чью-то торговую марку. Личность
  // в Kachō выдаёт собственный фасад iam, и меняться поставщик под ним может без
  // единой правки интерфейса — если интерфейс его не называет.
  //
  // Словарь ЗАКРЫТЫЙ и перечисляет продукты, а не слова общего языка: «провайдер»,
  // «OIDC», «SSO» — это механизмы, они законны и остаются.
  {
    re: /\bzitadel\b|\bkeycloak\b|\bauth0\b|\bokta\b|\bcognito\b|\bfirebase\b/i,
    why: "имя стороннего поставщика личности — арендатор входит через фасад iam",
  },
];

/** Ключи и атрибуты, значение которых читает пользователь. */
const USER_FACING_KEY = /(?:description|placeholder|label|title|reason|subTitle|message|aria-label)\s*[:=]\s*"([^"]{3,})"/g;

function consolePackages(): string[] {
  return readdirSync(consoleRoot)
    .filter((d) => {
      try {
        return statSync(path.join(consoleRoot, d, "src")).isDirectory();
      } catch {
        return false;
      }
    })
    .sort();
}

function walk(dir: string, out: string[]): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (e.name === "node_modules" || e.name === "dist" || e.name === ".vite") continue;
      walk(full, out);
      continue;
    }
    if (/\.(ts|tsx)$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) out.push(full);
  }
  return out;
}

export interface VocabularyFinding {
  line: number;
  text: string;
  why: string;
}

/** Литералы пользовательских ключей, несущие слово из внутреннего словаря. */
export function internalVocabularyHits(source: string): VocabularyFinding[] {
  const out: VocabularyFinding[] = [];
  stripComments(source, { keepLines: true })
    .split("\n")
    .forEach((line, i) => {
      for (const m of line.matchAll(USER_FACING_KEY)) {
        const value = m[1];
        for (const { re, why, onlyIn } of INTERNAL_LEXICON) {
          if (onlyIn && !onlyIn.test(value)) continue;
          if (re.test(value)) {
            out.push({ line: i + 1, text: value.slice(0, 90), why });
            break;
          }
        }
      }
    });
  return out;
}

describe("внутренний словарь не выходит на пользовательскую поверхность", () => {
  const packages = consolePackages();
  const files = packages.flatMap((p) => walk(path.join(consoleRoot, p, "src"), []));

  it("прочитано непустое дерево", () => {
    expect(packages.length).toBeGreaterThanOrEqual(9);
    expect(files.length).toBeGreaterThan(500);
  });

  it("извлекатель находит непустой набор литералов (иначе «ноль находок» ничего не значит)", () => {
    const total = files.reduce(
      (n, f) => n + [...stripComments(readFileSync(f, "utf8"), { keepLines: true }).matchAll(USER_FACING_KEY)].length,
      0,
    );
    expect(total).toBeGreaterThan(300);
  });

  it("ни одна подпись, подсказка и причина отказа не говорит языком реализации", () => {
    const findings: string[] = [];
    for (const f of files) {
      const rel = path.relative(consoleRoot, f);
      for (const h of internalVocabularyHits(readFileSync(f, "utf8"))) {
        findings.push(`${rel}:${h.line} [${h.why}] ${h.text}`);
      }
    }
    expect(findings).toEqual([]);
  });

  // Инъекция в обе стороны, на синтетике: доказательство не должно зависеть от
  // фикстуры, которая истекает вместе со своим предметом.
  it("предикат краснеет на каждой записи словаря", () => {
    const cases = [
      'description: "Балансировщик-родитель. Within-service FK → load_balancers."',
      'description: "Зона размещения (immutable после Create)."',
      'subTitle="Требуется FGA-relation admin@cluster:cluster_kacho_root."',
      'message: "Backend не вернул ответа"',
      'title: "Смотри KAC-246"',
      'label: "Значение project_id"',
    ];
    for (const c of cases) {
      expect(internalVocabularyHits(c)).toHaveLength(1);
    }
  });

  it("и молчит на законной формулировке той же формы", () => {
    const legal = [
      'description: "Балансировщик, которому принадлежит слушатель. Неизменяем после создания."',
      'description: "Зона размещения машины. Неизменяема после создания."',
      'subTitle="Недостаточно прав для просмотра администраторов облака."',
      'message: "Сервер не вернул ответа"',
      'label: "Идентификатор проекта"',
      // Образец значения, которое форма и требует ввести: имя роли ограничено
      // строчными латинскими буквами, цифрами и подчёркиванием.
      'placeholder="my_role"',
      // Разбор в комментарии — объяснение, а не подпись на экране.
      '// description: "Within-service FK → load_balancers" — так было до правки',
    ];
    for (const c of legal) {
      expect(internalVocabularyHits(c)).toEqual([]);
    }
  });
});
