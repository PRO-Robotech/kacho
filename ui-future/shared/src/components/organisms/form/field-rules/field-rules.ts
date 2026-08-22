// Правила поля, объявленные схемой, — читаемые ДО отправки, а не после отказа.
//
// ПОЧЕМУ ЭТО ЗАВЕДЕНО. Схема формы объявляет `required`, `pattern`, `min`/`max`,
// `minItems`/`maxItems` — и до сих пор их не читал НИКТО. `pattern` уезжал в
// одноимённый атрибут ввода, где без нативной отправки формы он не делает
// ничего; `required` доезжал только до звёздочки у подписи; предельные величины
// доезжали до `min`/`max` ввода, который браузер не запрещает обойти вставкой.
// То есть объявление было, а потребителя не было — тот самый класс
// «принято-и-проигнорировано», только на стороне консоли. Наблюдаемое следствие:
// сеть с пустым блоком CIDR (шаблон кладёт ровно одну пустую строку) уходила на
// сервер и возвращалась отказом, который называл поле запроса, а не поле формы.
//
// ПОЧЕМУ СООБЩЕНИЕ НАЗЫВАЕТ ПОЛЕ, хотя подпись стоит рядом слева. Сообщение
// живёт не только у поля: оно же читается программой чтения с экрана, попадает
// в перечень «что мешает отправить» и переживает прокрутку. Без имени поля оно
// означало бы «что-то не так здесь» — то есть требовало бы от читателя
// восстанавливать предмет по расположению.
//
// ЧЕГО ЗДЕСЬ НЕТ НАМЕРЕННО. Междуполевые инварианты ресурса (`spec.validate`)
// сюда не переезжают: у них нет ОДНОГО поля, к которому их можно приписать, и
// владеют ими оболочки формы. Здесь — только то, что объявлено у самого поля.

import type { ArrayField, FormField } from "@shared/lib/form-schema";
import { getByPath } from "@shared/lib/path";

/** Сообщения по полям: ключ — путь поля в объекте формы, значение — текст отказа. */
export type FieldErrors = Record<string, string>;

/**
 * Пусто ли значение поля.
 *
 * Переключатель исключён намеренно: `false` — законный ответ «нет», а не
 * отсутствие ответа. Считать его пустотой значило бы требовать от арендатора
 * включить то, что он осознанно выключил.
 */
function isEmpty(value: unknown, field: FormField): boolean {
  if (field.type === "bool") return false;
  if (value === undefined || value === null) return true;
  if (typeof value === "string") return value.trim() === "";
  if (Array.isArray(value)) return value.length === 0;
  if (field.type === "labels" && typeof value === "object") return Object.keys(value).length === 0;
  return false;
}

/** Имя поля в сообщении — в кавычках-ёлочках, как везде в консоли. */
function labelOf(field: FormField): string {
  return `«${field.label}»`;
}

/**
 * Правило ОДНОГО поля. Возвращает первое нарушение либо `null`.
 *
 * Порядок проверок — от «значения нет» к «значение не то»: сообщение про
 * образец на пустой строке сбивало бы с толку, ведь заполнять надо, а не
 * исправлять.
 */
export function checkField(field: FormField, value: unknown): string | null {
  if (field.required && isEmpty(value, field)) {
    return `${labelOf(field)}: поле обязательное — без него ресурс не создать.`;
  }
  if (isEmpty(value, field)) return null;

  if (field.type === "string" && field.pattern) {
    const text = typeof value === "string" ? value : String(value);
    let re: RegExp;
    try {
      re = new RegExp(field.pattern);
    } catch {
      // Негодный образец — это дефект объявления, а не ввода арендатора.
      // Молча пропустить вернее, чем обвинить того, кто ни при чём.
      return null;
    }
    if (!re.test(text)) {
      return `${labelOf(field)}: значение «${text}» не подходит под правило поля.`;
    }
  }

  if (field.type === "int" && typeof value === "number" && Number.isFinite(value)) {
    const { min, max } = field;
    if (min !== undefined && max !== undefined && (value < min || value > max)) {
      return `${labelOf(field)}: допустимо от ${min} до ${max}, введено ${value}.`;
    }
    if (min !== undefined && value < min) {
      return `${labelOf(field)}: не меньше ${min}, введено ${value}.`;
    }
    if (max !== undefined && value > max) {
      return `${labelOf(field)}: не больше ${max}, введено ${value}.`;
    }
  }

  if (field.type === "array" && Array.isArray(value)) {
    const { minItems, maxItems } = field;
    if (minItems !== undefined && value.length < minItems) {
      return `${labelOf(field)}: нужно не меньше ${minItems} — сейчас ${value.length}.`;
    }
    if (maxItems !== undefined && value.length > maxItems) {
      return `${labelOf(field)}: не больше ${maxItems} — сейчас ${value.length}.`;
    }
  }

  return null;
}

/**
 * Подполя строки списка проверяются ТАК ЖЕ, как поля верхнего уровня.
 *
 * Это не педантизм: обязательное подполе — единственное, чем в консоли выражен
 * обязательный блок CIDR у сети и у группы префиксов. Сам список обязательным
 * не объявлен (пустой список законен), а вот заведённая строка обязана нести
 * значение — иначе на сервер уходит пустая строка, которую он и отвергает.
 *
 * Сообщение несёт НОМЕР строки: без него два одинаковых имени подполя в разных
 * строках дали бы одинаковый текст, и читатель не знал бы, какую строку править.
 *
 * Вглубь — на список ВНУТРИ строки списка — разбор не идёт, и это сказано вслух,
 * чтобы следующий читатель не принял умолчание за обещание: показывать такое
 * сообщение всё равно негде (у вложенной строки своего места под текст нет), а
 * посчитанный и никому не показанный отказ запер бы форму без объяснения.
 * Появится вложенный список с обязательным подполем — сначала заводится место
 * под сообщение, потом разбор.
 */
function checkItems(field: ArrayField, path: string, obj: Record<string, unknown>, out: FieldErrors): void {
  const items = getByPath(obj, path);
  if (!Array.isArray(items)) return;
  items.forEach((_, idx) => {
    for (const sub of field.itemFields) {
      if (sub.hidden) return;
      const subPath = `${path}[${idx}].${sub.name}`;
      const problem = checkField(sub, getByPath(obj, subPath));
      if (problem) out[subPath] = `Строка ${idx + 1}. ${problem}`;
    }
  });
}

export interface CheckOptions {
  /** Правка: неизменяемые поля показаны как заблокированные и не отправляются. */
  editMode?: boolean;
  /** Пути, заданные из контекста, — арендатор их не правит, требовать с него нечего. */
  lockedPaths?: Set<string>;
}

/**
 * Проверка НАБОРА полей, уже отобранного оболочкой как видимый.
 *
 * Видимость решает вызывающий, и это принципиально: поле, скрытое ветвлением
 * (неактивная ветвь `oneof`), обязательным для арендатора не является — он его
 * не видит и заполнить не может. Проверять по объявлению, а не по показанному,
 * значило бы запереть форму сообщением о поле, которого на экране нет.
 */
export function checkFields(
  visible: FormField[],
  obj: Record<string, unknown>,
  opts: CheckOptions = {},
): FieldErrors {
  const out: FieldErrors = {};
  const locked = opts.lockedPaths ?? new Set<string>();
  for (const field of visible) {
    if (field.hidden) continue;
    if (locked.has(field.name)) continue;
    if (opts.editMode && field.immutable) continue;
    const problem = checkField(field, getByPath(obj, field.name));
    if (problem) out[field.name] = problem;
    if (field.type === "array") checkItems(field, field.name, obj, out);
  }
  return out;
}

/** Есть ли о чём говорить. Отдельным именем — вызывающие читают это трижды. */
export function hasErrors(errors: FieldErrors): boolean {
  return Object.keys(errors).length > 0;
}
