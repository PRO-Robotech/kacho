// Перепись областей списка: предикаты, которыми судит гейт `#373`.
//
// Вынесены из самого гейта по той же причине, что и `shared-symbol-sweep`:
// у них ДВА потребителя — гейт над деревом и проба инъекции над синтетикой, —
// и оба обязаны судить ОДНИМ предикатом. Иначе инъекция доказывает способность
// упасть у одной копии, а дерево судится другой, и расхождение проявляется
// ровно там, где его не видно.
//
// Файл лежит в `src/test/`, поэтому под собственную перепись не подпадает.

/**
 * Места рендера таблицы ресурса — по открывающему тегу, включая generic-форму
 * (`<ResourceTable<SgRule>`).
 */
export function renderSiteCount(text: string): number {
  const re = /<ResourceTable[<\s>]/g;
  let n = 0;
  while (re.exec(text) !== null) n++;
  return n;
}

/** Файл объявил полноту набора у таблицы. */
export function declaresCompleteness(text: string): boolean {
  return /\bcomplete[=\s}]/.test(text);
}

/**
 * Подбор значения в выпадающем поле формы — НЕ предмет этого гейта.
 *
 * Формально класс тот же: список опций тоже читается страницей, и «нет такого»
 * в нём тоже может означать «нет среди прочитанных». Но починка там другая —
 * поле подбора обязано спрашивать сервер по мере ввода, а не подписываться, —
 * и смешивать её сюда значило бы объявить закрытым то, чего гейт не проверяет.
 *
 * Владелец предиката распознаётся по имени ручки antd.
 */
const PICKER_OWNER = /filterOption|showSearch|filterSort|filterTreeNode/;

const NARROWING = /toLowerCase\(\)\.(includes|indexOf)|\.sort\(\(/;

/**
 * Номера строк (с единицы), где сужение принадлежит НЕ подбору значения.
 *
 * Контекст берётся на три строки вверх: ручка antd объявляется на своей строке,
 * а предикат переносится на следующую после прогона форматтера.
 */
export function narrowingLines(text: string): number[] {
  const lines = text.split("\n");
  const out: number[] = [];
  lines.forEach((line, i) => {
    if (!NARROWING.test(line)) return;
    if (PICKER_OWNER.test(lines.slice(Math.max(0, i - 3), i + 1).join("\n"))) return;
    out.push(i + 1);
  });
  return out;
}

/** Файл читает список у края. */
export function readsList(text: string): boolean {
  return /pageSize:|api\.list[<(]|useResourceList\(|\.list[A-Z][a-zA-Z]*\(/.test(text);
}

/** У пользователя есть ручка, которой он этим сужением управляет. */
export function hasControl(text: string): boolean {
  return /<Input|<Select|<Segmented|<Checkbox|Input\.Search/.test(text);
}

/** Поверхность, сужающая прочитанный список в браузере ручкой пользователя. */
export function narrowsLoadedList(text: string): boolean {
  return narrowingLines(text).length > 0 && readsList(text) && hasControl(text);
}

/** Файл сослался на общий словарь области. */
export function declaresScope(text: string): boolean {
  return /@shared\/lib\/list-scope/.test(text);
}

/** Объявление реализации таблицы ресурса (а не её ре-экспорт). */
export function declaresResourceTable(text: string): boolean {
  return /^export function ResourceTable\b/m.test(text);
}
