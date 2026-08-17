import {
  UNRESOLVED,
  collectVerbRouteUses,
  type ConsoleSource,
} from "./console-verb-literals";

/**
 * Инъекция для разбора выражений пути консоли — в ОБЕ стороны.
 *
 * (а) верни дефект — комментарий с нечётной обратной кавычкой перед настоящим
 *     обращением к глаголу: разбор обязан продолжать это обращение ВИДЕТЬ;
 * (б) поставь рядом законную конструкцию той же формы — путь-глагол, стоящий
 *     ВНУТРИ комментария: находкой он становиться не должен, иначе гейт
 *     запрещал бы объяснять код.
 *
 * Без (б) проверка ловила бы форму, а не существо: «строка, похожая на путь» —
 * нормальное содержимое любой документирующей шапки в этом дереве.
 *
 * Разбор здесь ТОТ ЖЕ, что судит настоящее дерево (`collectVerbRouteUses`), а не
 * его упрощённая копия: фикстура, снисходительнее продукта, делает невидимым
 * ровно тот дефект, ради которого её подставляют.
 */

function src(source: string, over: Partial<ConsoleSource> = {}): ConsoleSource {
  return {
    file: "probe/src/probe.ts",
    app: "probe",
    isResourceRegistry: false,
    source,
    ...over,
  };
}

const resolvedOf = (sources: ConsoleSource[]) =>
  collectVerbRouteUses(sources).uses.map((u) =>
    u.resolved.split(UNRESOLVED).join("…"),
  );

describe("разбор выражений пути консоли читает исполняемую часть", () => {
  it("нечётная обратная кавычка в комментарии больше не ослепляет разбор", () => {
    // Ровно форма дефекта #559: одна обратная кавычка в прозе выше по файлу.
    // Прежний разбор считал парность по сырому тексту, поэтому КАЖДЫЙ литерал
    // ниже читался как содержимое, а не как выражение пути.
    const withOddTick = src(
      [
        "// Ниже одна обратная кавычка ` и больше ни одной — парность сдвинута.",
        'const p = "/vpc/v1/networks/{id}:internal";',
      ].join("\n"),
    );
    expect(resolvedOf([withOddTick])).toEqual([
      "/vpc/v1/networks/{id}:internal",
    ]);

    // Контроль: тот же файл БЕЗ кавычки в прозе. Если бы разбор ломался по
    // другой причине, обе стороны молчали бы одинаково и проба ничего бы не
    // доказывала.
    const withoutOddTick = src(
      'const p = "/vpc/v1/networks/{id}:internal";',
    );
    expect(resolvedOf([withoutOddTick])).toEqual([
      "/vpc/v1/networks/{id}:internal",
    ]);
  });

  it("путь-глагол внутри комментария находкой не становится", () => {
    const onlyInProse = src(
      [
        "/**",
        ' *  Пример: "/vpc/v1/networks/{id}:internal" — форма ГЛАГОЛЬНАЯ.',
        " *  Здесь же `одиночная` кавычка в прозе — она ничего не значит.",
        " */",
        "export const nothing = 1;",
      ].join("\n"),
    );
    expect(resolvedOf([onlyInProse])).toEqual([]);
  });

  it("шаблонный литерал восстанавливается вместе с подстановкой", () => {
    const templated = src(
      [
        'const BASE = "/nlb/v1/loadBalancers";',
        "export const startPath = (id: string) => `${BASE}/${id}:start`;",
      ].join("\n"),
    );
    // BASE подставлен из константы файла, `${id}` остался неразрешённым.
    expect(resolvedOf([templated])).toEqual(["/nlb/v1/loadBalancers/…:start"]);
  });

  it("константа из другого модуля резолвится объектной картой путей", () => {
    const declaring = src(
      'export const IAM = { users: "/iam/v1/users" };',
      { file: "shared/src/api/iam.ts", app: "shared" },
    );
    const using = src("const p = `${IAM.users}/${id}:block`;", {
      file: "iam/src/pages/UsersPage.tsx",
      app: "iam",
    });
    expect(resolvedOf([declaring, using])).toEqual(["/iam/v1/users/…:block"]);
  });

  it("спека реестра резолвится реестром СВОЕГО приложения", () => {
    const registry = src(
      [
        "export const REGISTRY = {",
        '  "nlb-load-balancers": { id: "nlb-load-balancers", route: "/nlb", apiPath: "/nlb/v1/loadBalancers" },',
        "};",
      ].join("\n"),
      {
        file: "nlb/src/lib/resource-registry.tsx",
        app: "nlb",
        isResourceRegistry: true,
      },
    );
    const page = src('const SPEC = REGISTRY["nlb-load-balancers"];\n' + "const p = `${SPEC.apiPath}/${id}:stop`;", {
      file: "nlb/src/pages/Page.tsx",
      app: "nlb",
    });
    expect(resolvedOf([registry, page])).toEqual(["/nlb/v1/loadBalancers/…:stop"]);

    // Отрицание в паре с положительным: тот же файл, но приложение ДРУГОЕ —
    // реестр чужого приложения к нему не применяется, и путь остаётся
    // неразрешённым. Копии реестра намеренно расходятся, и склеивать их нельзя.
    const foreignPage = src('const SPEC = REGISTRY["nlb-load-balancers"];\n' + "const p = `${SPEC.apiPath}/${id}:stop`;", {
      file: "vpc/src/pages/Page.tsx",
      app: "vpc",
    });
    expect(resolvedOf([registry, foreignPage])).toEqual([]);
  });

  it("параметр маршрута браузера действием-глаголом не считается", () => {
    // `/projects/:projectId` выглядит так же и REST-путём не является вовсе.
    // Без этой стороны разбор объявил бы находкой каждый маршрут консоли.
    const routes = src('const r = "/vpc/networks/:networkId";');
    expect(resolvedOf([routes])).toEqual([]);
  });

  it("перепись литералов растёт вместе с прочитанным", () => {
    // «Ноль находок» обязано быть отличимо от «ноль разобранного»: число файлов
    // этого не показывает — прежний разбор читал ВСЕ файлы и терял литералы
    // внутри них.
    const scan = collectVerbRouteUses([
      src('const a = "x";\nconst b = `y`;\nconst c = `${a}/z`;'),
    ]);
    expect(scan.filesParsed).toBe(1);
    expect(scan.literalsParsed).toBe(3);
  });
});
