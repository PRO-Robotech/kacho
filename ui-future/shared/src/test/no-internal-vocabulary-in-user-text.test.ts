import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { collectLabels } from "../../../scripts/label-resolver.mjs";

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
 * Текст, который пользователь ВИДИТ, не содержит слов из словаря ниже. Видимым
 * считается значение ключа или атрибута из `USER_FACING_KEYS` и текст элемента.
 *
 * # Что здесь изменилось и почему (#1259)
 *
 * Прежняя редакция читала исходник ПОСТРОЧНО регулярным выражением по форме
 * `ключ: "литерал"`. Отсюда две слепоты, и обе она объявляла отсутствующими:
 *
 *   1. **вычисленный текст** — подпись, вынесенная в переменную, собранная
 *      шаблоном, склейкой `+`, тернарником или умолчанием `??`/`||`, регулярным
 *      выражением не видна вовсе. Шапка утверждала, что такого текста в этих
 *      ключах нет; замер на том же дереве давал шаблонов 67, склеек 10, имён 90,
 *      тернарников 56 — то есть утверждение о прошлом пережило свой предмет;
 *   2. **текст элемента** — «Cluster admin получает все права (FGA-relation …)»
 *      стоит не значением ключа, а текстом абзаца, и потому не судился ничем.
 *
 * Обе закрыты ОДНИМ разбором: `collectLabels` из `scripts/label-resolver.mjs` —
 * тот самый, которым читает подписи гейт языка (`scripts/check-ui-language.mjs`).
 * Второго разбора здесь намеренно нет: два разбора одного предмета расходятся
 * молча, и в этом дереве такое уже случалось.
 *
 * Комментарии из области выведены by construction: разбор их не видит, а не
 * снимает текстом. Пробы выведены обходом: разбор в пробе называет предмет и
 * обязан это делать.
 *
 * # Граница — числами, а не утверждением о прошлом
 *
 * Позиция подписи, значение которой приходит из ДАННЫХ (`title={row.name}`),
 * текста не несёт: резолвить нечего. Число таких позиций утверждается отдельно —
 * «ноль находок» обязано быть отличимо и от «ноль прочитанного», и от «прочитано
 * не то». Каждая полоса — разметка, вычисление, текст элемента — считается
 * отдельно: одно число скрыло бы ровно ту потерю покрытия, ради которой полосы и
 * заведены.
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
const USER_FACING_KEYS = new Set([
  "description",
  "placeholder",
  "label",
  "title",
  "reason",
  "subTitle",
  "message",
  "aria-label",
]);

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
  /** Откуда текст пришёл: «разметка» — литерал на месте, «вычислено» — резолвер. */
  origin: string;
}

/**
 * Тексты одного файла, видимые пользователю, — вместе с тем, как они собраны.
 *
 * Имя файла нужно разбору только для координаты; фикстуре достаточно умолчания.
 */
function userFacingTexts(source: string, rel = "fixture.tsx") {
  return collectLabels(rel, source, { labelKeys: USER_FACING_KEYS, jsxText: true });
}

/** Тексты пользовательской поверхности, несущие слово из внутреннего словаря. */
export function internalVocabularyHits(source: string, rel = "fixture.tsx"): VocabularyFinding[] {
  const out: VocabularyFinding[] = [];
  for (const label of userFacingTexts(source, rel).labels) {
    for (const { re, why, onlyIn } of INTERNAL_LEXICON) {
      if (onlyIn && !onlyIn.test(label.value)) continue;
      if (re.test(label.value)) {
        out.push({ line: label.line, text: label.value.slice(0, 90), why, origin: label.origin });
        break;
      }
    }
  }
  return out;
}

describe("внутренний словарь не выходит на пользовательскую поверхность", () => {
  const packages = consolePackages();
  const files = packages.flatMap((p) => walk(path.join(consoleRoot, p, "src"), []));

  /** Перепись по полосам: одно число скрыло бы потерю покрытия в любой из них. */
  const census = { markup: 0, computed: 0, jsxText: 0, dataSites: 0, valueSites: 0 };
  for (const f of files) {
    const rel = path.relative(consoleRoot, f);
    const read = userFacingTexts(readFileSync(f, "utf8"), rel);
    census.dataSites += read.dataSites;
    census.valueSites += read.valueSites;
    for (const l of read.labels) {
      if (l.kind === "текст JSX") census.jsxText++;
      else if (l.origin === "вычислено") census.computed++;
      else census.markup++;
    }
  }

  it("прочитано непустое дерево", () => {
    expect(packages.length).toBeGreaterThanOrEqual(9);
    expect(files.length).toBeGreaterThan(500);
  });

  it("каждая полоса непуста — иначе «ноль находок» означало бы «ноль прочитанного»", () => {
    // Литералом в разметке — то единственное, что читала прежняя редакция.
    expect(census.markup).toBeGreaterThan(1000);
    // Вычислением — полоса, которой прежняя редакция не видела и объявляла пустой.
    expect(census.computed).toBeGreaterThan(100);
    // Текстом элемента — вторая невидимая полоса; в ней и стояла живая находка.
    expect(census.jsxText).toBeGreaterThan(300);
    // Граница названа числом: позиция подписи есть, текста в ней нет.
    expect(census.dataSites).toBeGreaterThan(100);
  });

  it("ни одна подпись, подсказка, причина отказа и надпись не говорит языком реализации", () => {
    const findings: string[] = [];
    for (const f of files) {
      const rel = path.relative(consoleRoot, f);
      for (const h of internalVocabularyHits(readFileSync(f, "utf8"), rel)) {
        findings.push(`${rel}:${h.line} [${h.why}] (${h.origin}) ${h.text}`);
      }
    }
    expect(findings).toEqual([]);
  });

  // Инъекция в обе стороны. Форма фикстуры — объявление объекта либо элемент:
  // разбор читает СИНТАКСИС, и строка «ключ: значение» сама по себе свойством
  // объекта не является.
  it("предикат краснеет на каждой записи словаря", () => {
    const cases = [
      'export const C = { description: "Балансировщик-родитель. Within-service FK → load_balancers." };',
      'export const C = { description: "Зона размещения (immutable после Create)." };',
      'export const C = () => <Result subTitle="Требуется FGA-relation admin@cluster:cluster_root." />;',
      'export const C = { message: "Backend не вернул ответа" };',
      'export const C = { title: "Смотри KAC-246" };',
      'export const C = { label: "Значение project_id" };',
    ];
    for (const c of cases) {
      expect(internalVocabularyHits(c)).toHaveLength(1);
    }
  });

  it("и молчит на законной формулировке той же формы", () => {
    const legal = [
      'export const C = { description: "Балансировщик, которому принадлежит слушатель. Неизменяем после создания." };',
      'export const C = { description: "Зона размещения машины. Неизменяема после создания." };',
      'export const C = () => <Result subTitle="Недостаточно прав для просмотра администраторов облака." />;',
      'export const C = { message: "Сервер не вернул ответа" };',
      'export const C = { label: "Идентификатор проекта" };',
      // Образец значения, которое форма и требует ввести: имя роли ограничено
      // строчными латинскими буквами, цифрами и подчёркиванием.
      'export const C = () => <Input placeholder="my_role" />;',
      // Разбор в комментарии — объяснение, а не подпись на экране. Разбор его не
      // видит by construction: комментарий не является узлом выражения.
      '// export const C = { description: "Within-service FK → load_balancers" }; — так было до правки',
    ];
    for (const c of legal) {
      expect(internalVocabularyHits(c)).toEqual([]);
    }
  });

  // ── Полоса вычисленного текста: пять форм, каждая с законным близнецом ──────
  //
  // Значения взяты дословно из дерева (текст отказа, который гейт научился
  // видеть); синтетическая здесь только ФОРМА сборки — иначе оси было бы нечем
  // различить, живых нарушений этих форм в дереве на день правки нет.
  it("видит текст, собранный переменной, тернарником, шаблоном, склейкой и умолчанием", () => {
    const forms = [
      ['const t = "Требуется FGA-relation admin@cluster."; export const C = () => <Text>{t}</Text>;', "переменная"],
      ['export const C = () => <Alert message={bad ? "Backend не вернул ответа" : "Готово"} />;', "тернарник"],
      ["export const C = () => <Alert message={`Удалить «${name}»? AccessBinding активен.`} />;", "шаблон"],
      ['export const C = { description: "Смотри " + "KAC-246 про размер." };', "склейка"],
      ['export const C = () => <Text>{name || "Значение project_id"}</Text>;', "умолчание"],
    ] as const;
    for (const [source, form] of forms) {
      const hits = internalVocabularyHits(source);
      expect(`${form}: ${hits.length}`).toBe(`${form}: 1`);
      expect(hits[0].origin).toBe("вычислено");
    }
  });

  it("и молчит на тех же формах, собранных законным текстом", () => {
    const legal = [
      'const t = "Недостаточно прав для просмотра."; export const C = () => <Text>{t}</Text>;',
      'export const C = () => <Alert message={bad ? "Сервер не вернул ответа" : "Готово"} />;',
      "export const C = () => <Alert message={`Удалить «${name}»? Привязка активна.`} />;",
      'export const C = { description: "Смотри " + "раздел про размер." };',
      'export const C = () => <Text>{name || "Идентификатор проекта"}</Text>;',
      // Значение, вставленное В ФРАЗУ, текстом элемента не считается: там данные,
      // а не надпись. Без этой границы имя тома требовало бы перевода.
      "export const C = () => <Text>Том {volume.name} отключён</Text>;",
    ];
    for (const c of legal) {
      expect(internalVocabularyHits(c)).toEqual([]);
    }
  });

  // ── Текст элемента: полоса, в которой стояла живая находка ──────────────────
  it("видит надпись, стоящую текстом элемента, а не значением ключа", () => {
    // Дословно из дерева на день, когда гейт научился это видеть: окно выдачи
    // прав администратора облака объясняло арендатору устройство модели прав.
    const asItWas =
      "export const C = () => (\n" +
      "  <Typography.Paragraph>\n" +
      "    Cluster admin получает все права на ресурсы кластера (FGA-relation\n" +
      "  </Typography.Paragraph>\n" +
      ");\n";
    const hits = internalVocabularyHits(asItWas);
    expect(hits).toHaveLength(1);
    expect(hits[0].why).toBe("устройство модели прав");
  });

  it("и молчит на дословном значении, показанном как значение", () => {
    // Содержимое `<code>` — параметр фильтра, который оператор и копирует;
    // переводить его нельзя, а разбор видит его отдельным токеном без пробела,
    // поэтому правило «имя колонки в тексте» к нему не применяется.
    const legal = 'export const C = () => <Typography.Text code>resource_type=cluster</Typography.Text>;';
    expect(internalVocabularyHits(legal)).toEqual([]);
  });
});
