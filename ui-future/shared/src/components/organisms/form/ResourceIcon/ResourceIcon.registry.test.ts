// Ключ карты иконок обязан быть идентификатором спеки, которая есть в дереве.
//
// Класс. `ResourceIcon` берёт иконку по `specId` и молча падает на заглушку,
// если ключа нет. Поэтому ключ, переживший свой ресурс (`disks` — публичного
// `/compute/v1/disks` в стволе нет вовсе), выглядит рабочим, а ключ, названный
// не тем именем (`instances` при идентификаторе спеки `compute-instances`),
// оставляет живой ресурс без иконки. Оба состояния неотличимы от нормы на глаз
// и не ловятся ни typecheck'ом, ни ревью диффа.
//
// Источник истины — идентификаторы спек ВСЕХ реестров консоли, а не список
// рядом с тестом: карта общая, и `images`/`snapshots` она обслуживает для
// реестра storage, а не своего пакета. Сверять её только с shared значило бы
// объявить находкой законный ключ.
//
// Вердикт — по содержимому; объём осмотренного утверждается отдельно.

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
// shared/src/components/organisms/form/ResourceIcon → ui-future
const CONSOLE_ROOT = path.resolve(HERE, "../../../../../..");

// Приложения консоли — поимённо, как в console-verb-routes-exist: новое
// приложение обязано быть внесено осознанно, иначе выпадет из-под сверки молча.
const CONSOLE_APPS = ["host", "dashboard", "shared", "vpc", "compute", "storage", "nlb", "registry", "iam", "system"];

function registryFiles(): string[] {
  const out: string[] = [];
  for (const app of CONSOLE_APPS) {
    const dir = path.join(CONSOLE_ROOT, app, "src", "lib");
    let entries: string[];
    try {
      entries = readdirSync(dir);
    } catch {
      continue;
    }
    for (const e of entries) {
      const abs = path.join(dir, e);
      if (statSync(abs).isFile() && /^resource-registry.*\.tsx?$/.test(e) && !e.includes(".test.")) out.push(abs);
    }
  }
  return out;
}

const specIds = new Set(
  registryFiles().flatMap((f) =>
    [...readFileSync(f, "utf8").matchAll(/\bid:\s*"([a-z][a-z0-9-]*)"/g)].map((m) => m[1]),
  ),
);

const iconSource = readFileSync(path.join(HERE, "ResourceIcon.tsx"), "utf8");
const iconBlock = iconSource.slice(iconSource.indexOf("const ICONS"), iconSource.indexOf("interface Props"));
const iconKeys = [...iconBlock.matchAll(/^\s*"?([a-z][a-z0-9-]*)"?:\s*</gm)].map((m) => m[1]);

describe("карта иконок ресурсов", () => {
  it("прочитала реестры консоли и саму карту (молчаливый ноль — не пропуск)", () => {
    // Пять приложений из десяти держат собственный реестр (shared, compute,
    // storage, nlb, registry); у остальных его нет — они ходят в общий.
    expect(registryFiles().length).toBeGreaterThanOrEqual(5);
    expect(specIds.size).toBeGreaterThan(30);
    expect(iconKeys.length).toBeGreaterThan(20);
  });

  it("не держит ключа, за которым нет спеки ни в одном реестре", () => {
    const orphans = iconKeys.filter((k) => !specIds.has(k));
    expect(orphans).toEqual([]);
  });

  it("сопоставитель краснеет на выдуманном ключе", () => {
    // Контроль в обратную сторону: без него «сирот нет» означало бы лишь, что
    // ключи вообще не разобраны.
    expect(specIds.has("teleporters")).toBe(false);
    expect(specIds.has("networks")).toBe(true);
  });

  // Ресурсы registry держались в форке ИМЕННО из-за этих трёх ключей: общая
  // карта их не знала, и реестр, репозиторий и тег получали умолчание —
  // `AppstoreOutlined`, тот же глиф, что у «иконки нет» и у пула адресов. Свести
  // модуль на общую карту, не перенеся ключи, значило бы снять различимость трёх
  // ресурсов сразу и не заметить: заглушка выглядит как иконка.
  it("ресурсы registry имеют собственный глиф, а не умолчание", () => {
    for (const id of ["registries", "repositories", "tags"]) {
      expect({ id, hasIcon: iconKeys.includes(id) }).toEqual({ id, hasIcon: true });
    }
  });

  it("глифы трёх ресурсов registry различны между собой и не заняты соседями", () => {
    // Иначе «иконка есть» выполнялось бы тремя одинаковыми картинками — то есть
    // тем же неразличением, только объявленным.
    const glyphOf = new Map(
      [...iconBlock.matchAll(/^\s*"?([a-z][a-z0-9-]*)"?:\s*<(\w+)/gm)].map((m) => [m[1], m[2]] as const),
    );
    const mine = ["registries", "repositories", "tags"].map((id) => glyphOf.get(id));
    expect(new Set(mine).size).toBe(3);
    // Соседи по карте, чьи глифы форк предлагал занять повторно.
    expect(mine).not.toContain(glyphOf.get("cidr-groups"));
    expect(mine).not.toContain(glyphOf.get("placement-groups"));
    // Умолчание — не иконка: ключ с ним неотличим от отсутствующего.
    expect(mine).not.toContain("AppstoreOutlined");
  });
});
