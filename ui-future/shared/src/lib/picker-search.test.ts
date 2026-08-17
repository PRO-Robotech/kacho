// Область поиска поля подбора: что уезжает запросом и что стоит на месте
// пустого ответа.
//
// Предмет — не форма выражения, а ЧТО ПОЛЕ УТВЕРЖДАЕТ. Пустой ответ поля,
// сузившего список в браузере, читается пользователем как «такого ресурса нет»,
// хотя означает «нет среди прочитанных»: у выпадающего списка нет ни счётчика,
// ни «показать ещё», то есть узнать разницу неоткуда.

import {
  PICKER_EMPTY_LOADED,
  PICKER_EMPTY_SERVER,
  PICKER_NOTICE_LOADED,
  PICKER_NOTICE_SERVER,
  pickerScope,
  pickerScopeOfSpec,
} from "./picker-search";

describe("область поиска поля подбора", () => {
  it("настоящее поле ресурса спрашивается ПОДСТРОКОЙ", () => {
    // Точное равенство для строки поиска бесполезно: набирающий имя по частям
    // не совпадёт никогда. Оператор `CONTAINS` заведён владельцем ради неё.
    const scope = pickerScope({ serverSearchField: "name" });

    expect(scope.asksServer).toBe(true);
    expect(scope.query("web")).toEqual({ filter: 'name CONTAINS "web"' });
    expect(scope.emptyText).toBe(PICKER_EMPTY_SERVER);
    expect(scope.notice).toBe(PICKER_NOTICE_SERVER);
  });

  it("выделенное слово запроса спрашивается РАВЕНСТВОМ, а не подстрокой", () => {
    // iam отвергает CONTAINS у пользователя явно (`InvalidArgument` на всю
    // страницу): подстроку он ищет сам, по своему слову. Подставить сюда общий
    // механизм списков значило бы ронять страницу на каждом вводе.
    const scope = pickerScope({ serverTerm: "search" });

    expect(scope.query("min.ops")).toEqual({ filter: 'search="min.ops"' });
  });

  it("пустой ввод не превращается в выражение", () => {
    // `field CONTAINS ""` означает «любая строка» — то есть весь список был бы
    // показан как РЕЗУЛЬТАТ ПОИСКА. Пустой ввод обязан не спрашивать ничего.
    const byField = pickerScope({ serverSearchField: "name" });
    const byTerm = pickerScope({ serverTerm: "search" });

    expect(byField.query("")).toEqual({});
    expect(byField.query("   ")).toEqual({});
    expect(byTerm.query("")).toEqual({});
    // Строка из одних кавычек после очистки тоже пуста — и тоже не выражение.
    expect(byField.query('"""')).toEqual({});
  });

  it("кавычка из значения убирается, а не экранируется", () => {
    // Грамматика владельца обратной косой не понимает: кавычка ЗАКРЫВАЕТ
    // значение, и один символ в строке поиска ронял бы разбор всей страницы.
    expect(pickerScope({ serverSearchField: "name" }).query('we"b')).toEqual({
      filter: 'name CONTAINS "web"',
    });
  });

  it("без объявления владельца поле не выдумывает запрос, а называет свою область", () => {
    // Выдумать поле нельзя: белый список выражения ведёт владелец, и незнакомое
    // имя — не «фильтр без эффекта», а отказ на всю страницу. Значит остаётся
    // единственный законный исход — сказать правду о том, что сузилось.
    const scope = pickerScope(undefined);

    expect(scope.asksServer).toBe(false);
    expect(scope.query("что угодно")).toEqual({});
    expect(scope.emptyText).toBe(PICKER_EMPTY_LOADED);
    expect(scope.notice).toBe(PICKER_NOTICE_LOADED);
  });

  it("пустой ответ называет ОБЛАСТЬ, а не отсутствие", () => {
    // Ровно та строка, которую читает человек. Отрицание «ничего не найдено»
    // законно только там, где спрошен весь список.
    expect(PICKER_EMPTY_SERVER).toMatch(/по всему списку/);
    expect(PICKER_EMPTY_LOADED).toMatch(/загруженных/);
    expect(PICKER_EMPTY_LOADED).not.toMatch(/^Ничего не найдено$/);
  });

  it("объявление ресурса читается одним местом", () => {
    // Два места, читающие одно объявление, разошлись бы молча — и разошлись бы
    // там, где расхождение не видно: оба возвращают «область» на любом входе.
    expect(pickerScopeOfSpec({ serverSearchField: "name" }).query("a")).toEqual({
      filter: 'name CONTAINS "a"',
    });
    expect(pickerScopeOfSpec({ search: { serverTerm: "search" } }).query("a")).toEqual({
      filter: 'search="a"',
    });
    expect(pickerScopeOfSpec(undefined).asksServer).toBe(false);
    expect(pickerScopeOfSpec({}).asksServer).toBe(false);
  });

  it("при обоих ключах выбор детерминирован", () => {
    // Объявлять оба запрещено, но исход обязан быть один и тот же независимо от
    // порядка ветвей: иначе одно поле искало бы по-разному от сборки к сборке.
    expect(pickerScope({ serverSearchField: "name", serverTerm: "search" }).query("a")).toEqual({
      filter: 'search="a"',
    });
  });
});
