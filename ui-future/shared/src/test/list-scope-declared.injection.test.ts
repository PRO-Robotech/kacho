// Судья #373 доказывает способность упасть — и способность смолчать.
//
// Гейт `list-scope-declared` над зелёным деревом печатает «находок нет», и это
// утверждение неотличимо от «предикат ничего не ловит». Здесь тот же предикат
// (импортируется из `list-scope-census`, а не переписывается) прогоняется на
// синтетике В ОБЕ СТОРОНЫ: нарушение обязано быть найдено, законная
// конструкция той же формы — пропущена.
//
// Синтетика намеренно НЕ похожа на настоящий файл: правдоподобная фикстура
// прячет дефект, который сама и кормит.

import {
  declaresCompleteness,
  declaresResourceTable,
  declaresScope,
  hasControl,
  narrowingLines,
  narrowsLoadedList,
  readsList,
  renderSiteCount,
} from "./list-scope-census";

describe("места рендера таблицы", () => {
  it("считается каждый тег, включая generic-форму", () => {
    const src = `
      <ResourceTable rows={a} complete />
      <ResourceTable<Строка> rows={b} complete />
      <ResourceTableSomethingElse rows={c} />
    `;
    // Третий — другой компонент, чьё имя лишь начинается так же. Без разделителя
    // после имени предикат считал бы его своим и требовал бы от него ответа.
    expect(renderSiteCount(src)).toBe(2);
  });

  it("объявление полноты распознаётся, его отсутствие — тоже", () => {
    expect(declaresCompleteness("<ResourceTable rows={a} complete />")).toBe(true);
    expect(declaresCompleteness("<ResourceTable rows={a} complete={!hasMore} />")).toBe(true);
    expect(declaresCompleteness("<ResourceTable rows={a} />")).toBe(false);
  });

  it("СЛАБОСТЬ предиката названа: он судит о файле, а не о теге", () => {
    // Предикат ищет объявление в тексте ФАЙЛА. Значит слово в прозе способно
    // его удовлетворить — и это записано здесь, а не подразумевается: следующий
    // читатель должен знать цену, а не открывать её на разборе ложного зелёного.
    //
    // Почему не усиливаем сейчас: разбор JSX регулярным выражением разошёлся бы
    // с настоящим синтаксисом на первом же переносе строки, а полноценный разбор
    // — отдельная работа. Сегодня радиус слабости нулевой: файлов, где слово
    // `complete` стоит в прозе рядом с нерешённой таблицей, в дереве нет, и
    // перепись гейта печатает и число мест, и число файлов, поэтому расхождение
    // между ними видно.
    expect(declaresCompleteness("// набор complete и полон\n<ResourceTable rows={a} />")).toBe(true);
    // А запятая после слова его уже не удовлетворяет — то есть форма прозы
    // решает исход. Это и есть признак того, что предикат синтаксический.
    expect(declaresCompleteness("// набор complete, потому что курсора нет\n<ResourceTable rows={a} />")).toBe(false);
  });
});

describe("клиентское сужение: нарушение находится", () => {
  const VIOLATION = `
    const ответ = await api.list<{ штуки: Штука[] }>("/путь", { pageSize: "500" });
    const видимые = ответ.штуки.filter((ш) => ш.имя.toLowerCase().includes(запрос));
    return <Input value={запрос} onChange={сменить} />;
  `;

  it("сужает, читает список, ручка на экране — находка", () => {
    expect(narrowsLoadedList(VIOLATION)).toBe(true);
    expect(declaresScope(VIOLATION)).toBe(false);
  });

  it("та же поверхность, объявившая область, — пропускается", () => {
    // Законный близнец. Без него «находок нет» означало бы и «дерево честно», и
    // «предикат ловит всё подряд, а исключения его гасят».
    const LEGITIMATE = `import { clientScope } from "@shared/lib/list-scope";\n` + VIOLATION;
    expect(narrowsLoadedList(LEGITIMATE)).toBe(true);
    expect(declaresScope(LEGITIMATE)).toBe(true);
  });
});

describe("клиентское сужение: чужие формы НЕ считаются находкой", () => {
  it("подбор значения в поле формы — не предмет этого гейта", () => {
    // Тот же вызов `toLowerCase().includes`, но владелец предиката — ручка
    // выпадающего поля. Класс подбора чинится иначе и заведён отдельно.
    const pickerSample = `
      const ответ = await api.list<{ роли: Роль[] }>("/роли", { pageSize: "500" });
      return (
        <Select
          options={варианты}
          filterOption={(ввод, вариант) =>
            String(вариант?.label).toLowerCase().includes(ввод.toLowerCase())
          }
          showSearch
        />
      );
    `;
    expect(narrowingLines(pickerSample)).toEqual([]);
    expect(narrowsLoadedList(pickerSample)).toBe(false);
  });

  it("сужение без чтения списка — не предмет (это не список)", () => {
    const withoutList = `
      const видимые = свои.filter((ш) => ш.имя.toLowerCase().includes(запрос));
      return <Input value={запрос} />;
    `;
    expect(narrowingLines(withoutList).length).toBeGreaterThan(0);
    expect(readsList(withoutList)).toBe(false);
    expect(narrowsLoadedList(withoutList)).toBe(false);
  });

  it("сужение без ручки — не предмет (пользователю нечем этим управлять)", () => {
    const withoutControl = `
      const ответ = await api.list<{ штуки: Штука[] }>("/путь", { pageSize: "500" });
      const свои = ответ.штуки.filter((ш) => ш.родитель.toLowerCase().includes(родительId));
    `;
    expect(readsList(withoutControl)).toBe(true);
    expect(hasControl(withoutControl)).toBe(false);
    expect(narrowsLoadedList(withoutControl)).toBe(false);
  });
});

describe("вторая реализация таблицы", () => {
  it("объявление находится, ре-экспорт — нет", () => {
    expect(declaresResourceTable("export function ResourceTable<T>({ rows }: Пропсы<T>) {")).toBe(true);
    // Тонкая прослойка ничего не объявляет и является разрешённой формой.
    expect(declaresResourceTable('export * from "@shared/components/organisms/ResourceTable";')).toBe(false);
    expect(declaresResourceTable('export { ResourceTable } from "@shared/components/organisms/ResourceTable";')).toBe(
      false,
    );
  });
});
