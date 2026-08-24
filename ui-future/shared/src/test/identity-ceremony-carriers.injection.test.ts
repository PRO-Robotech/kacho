import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { walkCeremonyCarriers, PROVIDER_PROTOCOL } from "./identity-ceremony-carriers";

/**
 * Способность гейта полосы личности УПАСТЬ — и промолчать там, где падать не на чем.
 *
 * Гейт над настоящим деревом зелёный, поэтому его работоспособность из его же
 * зелени не следует НИКАК. Здесь она доказывается инъекцией на синтетическом
 * дереве: настоящий носитель протокола, настоящий узел JSX — и законный близнец
 * рядом, иначе гейт ловил бы форму (каталог, имя файла), а не существо (рендер).
 *
 * Оси проверяются по одной, потому что дефект и его близнец отличаются РОВНО
 * одним: тем, поднимает ли носителя приложение.
 */

let root: string;

function put(rel: string, body: string): void {
  const full = path.join(root, rel);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, body, "utf8");
}

/** Носитель церемонии: импортирует протокол поставщика и отдаёт компонент. */
function seedCarrier(name: string): void {
  put(
    `shared/src/pages/${name}.tsx`,
    [
      `import { kratos } from "${PROVIDER_PROTOCOL}";`,
      `export function ${name}() {`,
      "  return <a href={kratos.loginUrl()}>вход</a>;",
      "}",
      "",
    ].join("\n"),
  );
}

/**
 * Приложение, поднимающее названные компоненты узлами JSX.
 *
 * Всегда кладёт и пустышку в `shared/src`: обходчик читает библиотеку без
 * снисхождения и на отсутствующем каталоге ОТКАЗЫВАЕТ, а не возвращает ноль.
 * Так и задумано — молчаливый ноль прочитанного неотличим от нуля находок, — но
 * синтетическое дерево обязано быть таким же полным, как настоящее.
 */
function seedApp(...mounted: string[]): void {
  put("shared/src/lib/placeholder.ts", "export const PLACEHOLDER = 1;\n");
  put(
    "demo/src/main.tsx",
    [
      ...mounted.map((n) => `import { ${n} } from "@shared/pages/${n}";`),
      "export function Shell() {",
      `  return <div>${mounted.map((n) => `<${n} />`).join("")}</div>;`,
      "}",
      "",
    ].join("\n"),
  );
}

beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), "kacho-ceremony-"));
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

describe("гейт полосы личности способен упасть", () => {
  it("носитель церемонии, которого никто не рендерит, — НАХОДКА, и он назван", () => {
    seedCarrier("LonelyCeremonyPage");
    seedApp();

    const census = walkCeremonyCarriers(root);

    expect(census.carriers.map((c) => c.rel)).toEqual(["shared/src/pages/LonelyCeremonyPage.tsx"]);
    expect(census.orphaned.map((c) => c.rel)).toEqual(["shared/src/pages/LonelyCeremonyPage.tsx"]);
    // Координата обязана быть названа: находка без неё нечинима.
    expect(census.orphaned[0].components).toEqual(["LonelyCeremonyPage"]);
  });

  it("законный близнец — тот же носитель, поднятый приложением, — МОЛЧИТ", () => {
    seedCarrier("MountedCeremonyPage");
    seedApp("MountedCeremonyPage");

    const census = walkCeremonyCarriers(root);

    // Носитель распознан (значит предикат не «просто ничего не видит»)…
    expect(census.carriers.map((c) => c.rel)).toEqual(["shared/src/pages/MountedCeremonyPage.tsx"]);
    // …и находкой не объявлен.
    expect(census.orphaned).toEqual([]);
    expect(census.rendered.map((c) => c.rel)).toEqual(["shared/src/pages/MountedCeremonyPage.tsx"]);
  });

  it("мёртвая гроздь не выдаёт себя за живую: взаимный рендер живостью НЕ считается", () => {
    // Ровно тот случай, на котором предикат сначала промахнулся: одна мёртвая
    // страница рендерит другую, и обе выглядели бы живыми.
    seedCarrier("FirstDeadPage");
    put(
      "shared/src/pages/SecondDeadPage.tsx",
      [
        `import { kratos } from "${PROVIDER_PROTOCOL}";`,
        'import { FirstDeadPage } from "./FirstDeadPage";',
        "export function SecondDeadPage() {",
        "  return <FirstDeadPage href={kratos.loginUrl()} />;",
        "}",
        "",
      ].join("\n"),
    );
    seedApp();

    const census = walkCeremonyCarriers(root);

    expect(census.orphaned.map((c) => c.rel).sort()).toEqual([
      "shared/src/pages/FirstDeadPage.tsx",
      "shared/src/pages/SecondDeadPage.tsx",
    ]);
  });

  it("файл БЕЗ протокола поставщика носителем не считается — даже лёжа рядом", () => {
    // Иначе гейт судил бы по каталогу, а не по существу, и всякая страница
    // рядом становилась бы его предметом.
    put(
      "shared/src/pages/PlainPage.tsx",
      ["export function PlainPage() {", "  return <div>обычная страница</div>;", "}", ""].join("\n"),
    );
    seedApp();

    const census = walkCeremonyCarriers(root);

    expect(census.carriers).toEqual([]);
    expect(census.orphaned).toEqual([]);
  });

  it("на дереве без носителей перепись пуста, а не ложно-зелена", () => {
    // Ноль находок обязано быть отличимо от нуля прочитанного: настоящий гейт
    // требует непустой переписи отдельным утверждением.
    seedApp();

    const census = walkCeremonyCarriers(root);

    expect(census.carriers).toEqual([]);
    expect(census.productFiles.length).toBeGreaterThan(0);
  });
});
