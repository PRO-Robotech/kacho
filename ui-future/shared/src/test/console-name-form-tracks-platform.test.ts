// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { GUEST_ACCESS_KEY_FIELDS } from "@shared/lib/guest-access-key-form";
import { REGISTRY } from "@shared/lib/resource-registry";
import type { FormField } from "@shared/lib/form-schema";

/**
 * Гейт: ФОРМА ИМЕНИ, ОБЪЯВЛЕННАЯ ФОРМОЙ СОЗДАНИЯ, — ЭТО ФОРМА ПЛАТФОРМЫ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ
 *
 * Подсказка под полем «Имя» — это правило, которое продукт НАЗВАЛ клиенту. Пока
 * она объявлена в консоли своей строкой, а проверяет имя сервер своей, эти две
 * строки расходятся молча: форма принимает ввод, сервер его отвергает, и клиент
 * узнаёт об этом после ожидания асинхронной операции — отказом по-английски и
 * регуляркой. Так и было (#1604): пять разных объявлений имени в одном реестре,
 * четыре из них обещали подчёркивание, одно — любой регистр, и ни одно не
 * совпадало с формой, которую держит платформа.
 *
 * Обратная сторона того же расхождения тише и потому опаснее: объявление СТРОЖЕ
 * платформы отвергает в форме ввод, который край принимает. Имя `9-web` законно
 * (форма допускает цифру первой), а прежнее объявление требовало начинать с
 * буквы — то есть консоль отбирала у клиента законное имя, и по экрану это
 * неотличимо от правила продукта.
 *
 * ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ ПРОБЫ ПОЛЯ. Проба поля утверждает, что объявленный
 * образец применяется. Здесь утверждается другое и его нечем заменить: что
 * объявленный образец — ТОТ САМЫЙ, что у сервера. Это факт о ДВУХ деревьях, и
 * получить его исполнением консоли нельзя.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ФОРМ ДВЕ, А НЕ ОДНА
 *
 * Соблазн «свести к одной» здесь неверен, и цена ошибки — та же, ради которой
 * гейт заведён, только в другую сторону. Платформа держит ОДНУ форму имени
 * (`pkg/validate/nameform`, DNS label RFC 1123) — её применяют iam, vpc,
 * compute, storage, nlb, geo. Реестр образов проверяет имя СВОЕЙ формой: она
 * допускает точку и длину до 255, потому что имя реестра живёт в OCI-пути.
 * Подставив сюда общую форму, консоль начала бы отвергать `my.registry` —
 * законное имя, которое край принимает.
 *
 * Поэтому обе формы читаются из своих исходников, и каждая привязана к тем
 * ресурсам, чей владелец её и применяет. Разойдись любая из двух — красное
 * приходит сюда, а не к клиенту.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЧТЕНИЕ `.go`, А НЕ ВЫПИСАННАЯ КОНСТАНТА
 *
 * Выписанная константа — второе место об одном предмете: она верна в день
 * записи и стареет молча, ровно как устарели пять прежних объявлений. Исходник
 * другого языка проба исполнить не может, поэтому чтение здесь — единственный
 * доступный способ связать две стороны; запрет дерева на чтение исходника как
 * текста это прямо оговаривает (`internal/repohygiene` `uisourcereadtest`,
 * условие (3): `.go` читают, `.ts`/`.tsx` загружают). Реестр консоли этот гейт
 * именно ЗАГРУЖАЕТ и судит по значениям, а не по своему тексту.
 */

const here = path.dirname(fileURLToPath(import.meta.url));
/** Корень монорепо: ui-future/shared/src/test → вверх на пять. */
const repoRoot = path.resolve(here, "../../../..");

/**
 * Форма, объявленная исходником, либо отказ с названным файлом.
 *
 * Отсутствие файла и несовпадение образца — РАЗНЫЕ беды, и обе обязаны быть
 * отказом, а не пропуском: гейт, молча пропускающий непрочитанный источник,
 * зеленеет ровно тогда, когда сравнивать не с чем.
 */
function goDeclaredForm(relPath: string, pattern: RegExp, what: string): string {
  const full = path.join(repoRoot, relPath);
  let text: string;
  try {
    text = readFileSync(full, "utf8");
  } catch {
    throw new Error(
      `${what}: исходник платформы не прочитан — ${relPath}. Сравнивать не с чем, ` +
        `и молчание здесь означало бы «форма совпала», чего никто не проверял.`,
    );
  }
  const m = pattern.exec(text);
  if (!m) {
    throw new Error(
      `${what}: в ${relPath} не найдено объявление формы имени по образцу ${pattern}. ` +
        `Предпосылка гейта не выполнена: он сверяет консоль с объявлением, которого больше нет.`,
    );
  }
  return m[1];
}

/** Форма платформы — одна на всё дерево (iam · vpc · compute · storage · nlb · geo). */
const PLATFORM_FORM = goDeclaredForm(
  "pkg/validate/nameform/nameform.go",
  /^const Form = `([^`]+)`/m,
  "форма платформы",
);

/** Форма имени реестра образов — своя: допускает точку, длина до 255. */
const REGISTRY_FORM = goDeclaredForm(
  "services/registry/internal/domain/registry.go",
  /^var dnsName = regexp\.MustCompile\(`([^`]+)`\)/m,
  "форма имени реестра",
);

/** Ресурсы, чьё имя судит реестр образов своей формой. */
const REGISTRY_OWNED = new Set(["registries"]);

/** Знаки, которых нет ни в одной из двух форм, и слова, которыми их обещают. */
const FALSE_PROMISES: { needle: RegExp; says: string }[] = [
  { needle: /«_»|подчёркивани/i, says: "подчёркивание" },
  { needle: /любой регистр|заглавн|верхний регистр/i, says: "заглавные буквы" },
];

interface NameFieldSite {
  /** Где объявлено — для сообщения об отказе. */
  where: string;
  field: FormField;
  /** Какой формой судит имя владелец этого ресурса. */
  expected: string;
  expectedName: string;
}

/** Поля с именем `name`, включая подполя списков: форма имени всюду одна. */
function nameFieldsOf(fields: readonly FormField[] | undefined, where: string, sites: NameFieldSite[], spec?: string): void {
  for (const field of fields ?? []) {
    if (field.name === "name" && field.type === "string") {
      const registryOwned = spec !== undefined && REGISTRY_OWNED.has(spec);
      sites.push({
        where,
        field,
        expected: registryOwned ? REGISTRY_FORM : PLATFORM_FORM,
        expectedName: registryOwned ? "формы имени реестра образов" : "формы платформы",
      });
    }
    if (field.type === "array") nameFieldsOf(field.itemFields, `${where} → ${field.name}[]`, sites, spec);
  }
}

/**
 * Все объявления имени в консоли, КАЖДОЕ по одному разу.
 *
 * Разные спеки законно делят один объект поля (форма гостевого ключа объявлена
 * однажды и переиспользована спекой), поэтому перепись идёт по ОБЪЕКТУ, а не по
 * месту упоминания: иначе одно объявление считалось бы дважды, и число «полей
 * найдено» говорило бы о частоте ссылок, а не о составе дерева.
 */
function allNameFields(): NameFieldSite[] {
  const sites: NameFieldSite[] = [];
  for (const [id, spec] of Object.entries(REGISTRY)) {
    nameFieldsOf(spec.fields, `реестр ресурсов, спека «${id}»`, sites, id);
  }
  nameFieldsOf(GUEST_ACCESS_KEY_FIELDS, "форма гостевого ключа доступа", sites);
  const seen = new Set<FormField>();
  return sites.filter((s) => (seen.has(s.field) ? false : (seen.add(s.field), true)));
}

/**
 * Те же поля БЕЗ склейки по объекту — сколько раз правило имени применяется.
 *
 * Печатаются обе величины, а не одна: «объявлений 5» без «мест применения»
 * скрывает охват (одно расхождение стоит двадцати форм), а «мест 25» без
 * «объявлений» скрывает, сколько на самом деле источников правила.
 */
function allNameSites(): NameFieldSite[] {
  const sites: NameFieldSite[] = [];
  for (const [id, spec] of Object.entries(REGISTRY)) {
    nameFieldsOf(spec.fields, `реестр ресурсов, спека «${id}»`, sites, id);
  }
  return sites;
}

const census = allNameFields();
process.stdout.write(
  `\n  форма имени: спек осмотрено ${Object.keys(REGISTRY).length}, ` +
    `мест применения ${allNameSites().length}, объявлений «Имя» ${census.length}, ` +
    `формой платформы судятся ${census.filter((s) => s.expected === PLATFORM_FORM).length}, ` +
    `формой реестра образов ${census.filter((s) => s.expected === REGISTRY_FORM).length}\n\n`,
);

describe("форма имени в консоли — та же, что у платформы (#1604)", () => {
  const sites = census;

  it("перепись: осмотренное названо числом, пустой обход — отказ", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: гейт,
    // не нашедший ни одного поля имени, ничего не проверил и обязан сказать это
    // отказом, а не зелёным.
    expect(Object.keys(REGISTRY).length).toBeGreaterThan(0);
    expect(sites.length).toBeGreaterThan(0);
  });

  it("две формы прочитаны из исходников платформы и различаются", () => {
    // Предпосылка гейта: формы РАЗНЫЕ. Совпади они — привязка ресурсов к
    // владельцу перестала бы что-либо проверять, и гейт остался бы зелёным,
    // проверяя воздух.
    expect(PLATFORM_FORM).toBe("^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$");
    expect(REGISTRY_FORM).not.toBe(PLATFORM_FORM);
  });

  it("каждое поле «Имя» объявляет образец СВОЕГО владельца", () => {
    // Находки собираются перечнем, а не по одной: сообщение отказа обязано
    // называть ВСЕ расхождения сразу — иначе починка идёт кругами по одному
    // полю за прогон, а перепись остаётся невидимой.
    const wrong = sites
      .filter((s) => (s.field.type === "string" ? s.field.pattern : undefined) !== s.expected)
      .map((s) => {
        const declared = s.field.type === "string" ? s.field.pattern : undefined;
        return declared === undefined
          ? `${s.where}: образца нет вовсе — форма примет ввод, который край отвергнет, и отказ придёт после ожидания операции`
          : `${s.where}: объявлен «${declared}», у владельца «${s.expected}» (${s.expectedName})`;
      });
    expect(wrong).toEqual([]);
  });

  it("подсказка «Имя» не обещает знаков, которых форма не принимает", () => {
    const lying = sites.flatMap((s) => {
      const text = s.field.description ?? "";
      return FALSE_PROMISES.filter(({ needle }) => needle.test(text)).map(
        ({ says }) => `${s.where}: подсказка обещает ${says} — «${text}»`,
      );
    });
    expect(lying).toEqual([]);
  });

  it("образец платформы принимает цифру первой и один символ — законный ввод не отбирается", () => {
    // Контроль в обратную сторону. Отрицания выше зеленели бы на образце,
    // отвергающем всё, — поэтому здесь утверждается, что образец ПРИНИМАЕТ то,
    // что принимает край.
    const re = new RegExp(PLATFORM_FORM);
    expect(re.test("9-web")).toBe(true);
    expect(re.test("a")).toBe(true);
    expect(re.test("web-net-1")).toBe(true);
    expect(re.test("Web_Net")).toBe(false);
    expect(re.test("-web")).toBe(false);
  });

  it("образец реестра образов принимает точку — своя форма не сужена до общей", () => {
    const re = new RegExp(REGISTRY_FORM);
    expect(re.test("my.registry")).toBe(true);
    expect(re.test("9reg")).toBe(true);
    expect(re.test("My_Registry")).toBe(false);
  });
});
