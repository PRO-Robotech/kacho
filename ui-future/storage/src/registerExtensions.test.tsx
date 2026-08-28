import { isValidElement, type ReactElement, type ReactNode } from "react";

// Регистрация — side-effect: расширения подключаются к ОБЩЕМУ реестру ровно так
// же, как на старте бандла.
import "./registerExtensions";
import { detailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { DetailSurface, PropertyRows } from "@/components/organisms/DetailShell";
import { ConsumersFact } from "@shared/components/molecules/ConsumersFact";
import { PlacementAnchor } from "@shared/components/molecules/PlacementAnchor";

/**
 * Строки карточек storage: правила 2, 6 и 9 канона консоли.
 *
 * Утверждается ДЕРЕВО ЭЛЕМЕНТОВ, а не отрисованный текст. Причина: правило 2
 * требует именно ССЫЛКУ («иконка типа + имя + переход»), и проба, ищущая текст,
 * осталась бы зелёной на плоском идентификаторе, из которого некуда пойти, —
 * ровно тот дефект, который правило и запрещает. Монтировать при этом нечего:
 * `overviewExtra` возвращает данные, а не разметку, поэтому ни маршрутизатор, ни
 * клиент запросов здесь не нужны.
 */

/**
 * Обходит дерево элементов и возвращает все узлы указанного типа.
 *
 * Спускается по ЛЮБОМУ значению props, а не только по `children`. Первая
 * редакция читала один `children` и не находила ничего: секции карточки
 * получают строки таблицей ЧЕРЕЗ ПРОП (`rows`), поэтому обход по детям
 * возвращал пустой список — и утверждение «способности названы следствием»
 * прошло бы на карточке, где их нет вовсе. Ловушка типична: отрицание зеленеет
 * на сломанном обходе, поэтому у каждого отрицания ниже стоит положительный
 * контроль.
 */
function nodesOfType(node: unknown, type: unknown): ReactElement[] {
  const out: ReactElement[] = [];
  const seen = new Set<unknown>();
  const walk = (n: unknown): void => {
    if (n === null || n === undefined || typeof n === "string" || typeof n === "number" || typeof n === "boolean") {
      return;
    }
    if (Array.isArray(n)) {
      n.forEach(walk);
      return;
    }
    if (typeof n !== "object") return;
    if (seen.has(n)) return;
    seen.add(n);
    if (isValidElement(n)) {
      const el = n as ReactElement<Record<string, unknown>>;
      if (el.type === type) out.push(el);
      for (const value of Object.values(el.props ?? {})) walk(value);
      return;
    }
    for (const value of Object.values(n as Record<string, unknown>)) walk(value);
  };
  walk(node as ReactNode);
  return out;
}

const ctx = (data: Record<string, unknown>) => ({
  data,
  projectId: "prj-1",
  detailBase: "/projects/prj-1/storage/volumes/vol-1",
  navigate: () => undefined,
});

/**
 * Расширение резолвится ТЕМ ЖЕ вызовом, каким его берёт `ResourceShell`.
 *
 * Прежде проба читала модульную карту расширений — то есть предмет, который сам
 * был форком общего реестра. Через общий резолвер проба заодно утверждает и саму
 * РЕГИСТРАЦИЮ: незарегистрированное расширение падает здесь, с именем ресурса,
 * а не приходит `undefined` в конце цепочки.
 */
function ext(specId: string) {
  const found = detailExtension(specId);
  if (!found) throw new Error(`расширение detail-страницы не зарегистрировано: ${specId}`);
  return found;
}

function rows(specId: string, data: Record<string, unknown>) {
  return ext(specId).overviewExtra!(ctx(data));
}

// Подпись строки — ReactNode (в неё кладут ⓘ-подсказку). Здесь все подписи
// текстовые, и проба на этом НАСТАИВАЕТ: узел вместо текста — находка, а не
// повод к `String(<узел>)`, который дал бы «[object Object]» для любой подписи
// и сделал бы сравнения ниже истинными при любом содержании.
function labelText(r: { label: ReactNode }): string {
  if (typeof r.label !== "string") throw new Error(`подпись строки не текст: ${String(r.label)}`);
  return r.label;
}

function labels(specId: string, data: Record<string, unknown>): string[] {
  return rows(specId, data).map(labelText);
}

function valueOf(specId: string, data: Record<string, unknown>, label: string): ReactNode {
  const row = rows(specId, data).find((r) => labelText(r) === label);
  return row?.value ?? null;
}

const IMAGE = {
  id: "img-1",
  project_id: "prj-1",
  region_id: "ru-central1",
  placement_type: "REGIONAL",
};

const VOLUME = {
  id: "vol-1",
  project_id: "prj-1",
  zone_id: "ru-central1-a",
  disk_type_id: "network-ssd",
  size_bytes: String(10 * 1024 ** 3),
  status: "AVAILABLE",
};

describe("правило 2 — зона, регион и тип диска показываются ссылкой", () => {
  it("том: зона и тип диска — RefNameLink, а не плоский идентификатор", () => {
    const zone = nodesOfType(valueOf("volumes", VOLUME, "Зона доступности"), RefNameLink);
    expect(zone).toHaveLength(1);
    expect(zone[0].props).toMatchObject({ specId: "zones", refId: "ru-central1-a" });

    const dt = nodesOfType(valueOf("volumes", VOLUME, "Тип диска"), RefNameLink);
    expect(dt).toHaveLength(1);
    expect(dt[0].props).toMatchObject({ specId: "disk-types", refId: "network-ssd" });
  });

  it("зона и регион спрашиваются БЕЗ project_id — это глобальный каталог geo", () => {
    // `RefNameLink` берёт область видимости из спеки, но `projectId`-проп
    // перекрывает контекст. Передать его сюда значило бы вернуть тот же чужой
    // параметр вручную, в обход общего предиката.
    const zone = nodesOfType(valueOf("volumes", VOLUME, "Зона доступности"), RefNameLink)[0];
    expect(zone.props).not.toHaveProperty("projectId", "prj-1");

    // Якорь размещения образа рисует `PlacementAnchor` — ветка ZONAL/REGIONAL
    // живёт в ЕДИНСТВЕННОМ месте на всё дерево. Утверждаемое свойство то же,
    // что и прежде («регион показан ссылкой на каталог geo и спрашивается без
    // project_id»), и утверждается оно ТАМ, ГДЕ ТЕПЕРЬ ИСПОЛНЯЕТСЯ: компонент
    // раскрывается вызовом, и смотрим, чем он оказался. Утверждать вместо
    // этого сам факт «в строке стоит PlacementAnchor» значило бы закрепить имя
    // компонента, а не свойство, — и проба осталась бы зелёной, если бы он
    // однажды перестал вести на каталог.
    const anchors = nodesOfType(valueOf("images", IMAGE, "Размещение"), PlacementAnchor);
    expect(anchors).toHaveLength(1);
    const anchorProps = anchors[0].props as { row: Record<string, unknown>; maxChars?: number };
    const region = nodesOfType(PlacementAnchor(anchorProps), RefNameLink)[0];
    expect(region.props).toMatchObject({ specId: "regions", refId: "ru-central1" });
    expect(region.props).not.toHaveProperty("projectId", "prj-1");
  });

  it("вид размещения НЕ печатается токеном рядом с тем же фактом", () => {
    // Прежде строк было две: «Регион» со ссылкой и «Размещение» с сырым
    // `REGIONAL` — машинное слово рядом с фактом, уже названным ссылкой строкой
    // выше. Токен наружу не выходит вовсе: вид размещения — это тип ресурса, на
    // который ведёт ссылка, и отдельного слова ему не нужно.
    const ls = labels("images", IMAGE);
    expect(ls).toContain("Размещение");
    expect(ls).not.toContain("Регион");
    expect(rows("images", IMAGE).map((r) => r.value)).not.toContain("REGIONAL");
  });

  it("потребители тома — ссылки, а не имена через запятую", () => {
    // `used_by` — тот же источник, что у колонки «Используется» в списке томов:
    // один предмет, один вид. Прежде список рисовал ссылки, а карточка того же
    // тома — плоскую строку имён, из которой на машину не перейти.
    const used = valueOf(
      "volumes",
      { ...VOLUME, used_by: [{ referrer: { type: "compute.instance", id: "ins-1", name: "web-1" } }] },
      "Используется",
    );
    const facts = nodesOfType(used, ConsumersFact);
    expect(facts).toHaveLength(1);
    expect((facts[0].props as { usedBy: unknown[] }).usedBy).toHaveLength(1);
    // Положительный контроль: значение — узел, а не строка. Без него отрицание
    // выше зеленело бы и на пустой ячейке.
    expect(typeof used).not.toBe("string");
  });

  it("снимок: собственный якорь зоны показан ссылкой", () => {
    // Якорь СНИМКА, а не «зона исходного тома»: ссылка на источник обнуляется
    // при его удалении, и зона, добираемая через источник, однажды стала бы
    // пустой строкой.
    const zone = nodesOfType(
      valueOf("snapshots", { id: "snp-1", project_id: "prj-1", zone_id: "ru-central1-b" }, "Зона доступности"),
      RefNameLink,
    );
    expect(zone).toHaveLength(1);
    expect(zone[0].props).toMatchObject({ specId: "zones", refId: "ru-central1-b" });
  });

  it("пустая ссылка не рисуется ссылкой в никуда", () => {
    const zone = nodesOfType(valueOf("volumes", { ...VOLUME, zone_id: "" }, "Зона доступности"), RefNameLink);
    expect(zone).toHaveLength(0);
  });
});

describe("правило 9 — поле без источника не показывается", () => {
  it("причины состояния нет ⇒ строки нет вовсе, а не прочерк", () => {
    // Прочерк на месте причины читается как «причина есть, но мы её не знаем».
    expect(labels("volumes", VOLUME)).not.toContain("Причина состояния");
    expect(labels("volumes", { ...VOLUME, status_reason: "STATUS_REASON_UNSPECIFIED" })).not.toContain(
      "Причина состояния",
    );
    // Положительный контроль: иначе отрицание зеленело бы на сломанном рендере.
    expect(labels("volumes", { ...VOLUME, status: "ERROR", status_reason: "BACKEND_REJECTED" })).toContain(
      "Причина состояния",
    );
  });

  it("формат образа не заявлен ⇒ строки нет; заявлен ⇒ назван словом", () => {
    // Прочерк на месте формата читается как «формат есть, но мы его не знаем»,
    // а токен `STANDARD` отвечает, как значение называется внутри, и не
    // говорит читателю ничего.
    expect(labels("images", IMAGE)).not.toContain("Формат");
    expect(labels("images", { ...IMAGE, format: "FORMAT_UNSPECIFIED" })).not.toContain("Формат");
    const shown = JSON.stringify(valueOf("images", { ...IMAGE, format: "STANDARD" }, "Формат"));
    expect(shown).toContain("Стандартный");
    expect(shown).not.toContain("STANDARD");
    // Незнакомое значение показывается СОБОЙ: словарь контракта мог
    // пополниться раньше консоли, и промолчать о формате хуже, чем показать
    // токен.
    expect(JSON.stringify(valueOf("images", { ...IMAGE, format: "QCOW2" }, "Формат"))).toContain("QCOW2");
  });

  it("причина есть у всех трёх ресурсов, а не только у тома", () => {
    expect(labels("snapshots", { id: "snp-1", status_reason: "SOURCE_NOT_READY" })).toContain("Причина состояния");
    expect(labels("images", { id: "img-1", status_reason: "BACKEND_UNAVAILABLE" })).toContain("Причина состояния");
  });

  it("занятые байты: не сообщены ⇒ строки нет; сообщён ноль ⇒ строка есть", () => {
    // Отсутствие значения и ноль — РАЗНЫЕ утверждения, и различие несёт само
    // значение. Ноль означает «том пуст»; показать на нём «—» значило бы сказать
    // «не знаю» там, где сервер ответил точно.
    expect(labels("volumes", VOLUME)).not.toContain("Занято");
    const zero = rows("volumes", { ...VOLUME, used_bytes: "0" }).find((r) => r.label === "Занято");
    expect(zero).toBeDefined();
    expect(JSON.stringify(zero!.value)).toContain("0 B");
  });

  it("промежуточное состояние объясняется, конечное — нет", () => {
    expect(labels("volumes", VOLUME)).not.toContain("Что происходит");
    expect(labels("volumes", { ...VOLUME, status: "CREATING" })).toContain("Что происходит");
    expect(labels("volumes", { ...VOLUME, status: "MIGRATING" })).toContain("Что происходит");
  });
});

describe("правило 6 — способности типа диска названы следствием", () => {
  const CAPS = {
    id: "network-ssd",
    tier: "FAST",
    lifecycle: "ACTIVE",
    capabilities: { snapshots: true, clone_from_snapshot: false },
  };

  it("каждая способность рисуется BoolFact с парой фраз, а не «Да»/«Нет»", () => {
    const below = ext("disk-types").overviewBelow!(ctx(CAPS));
    const facts = nodesOfType(below, BoolFact);
    expect(facts.length).toBeGreaterThan(0);
    for (const f of facts) {
      const props = f.props as { yes: string; no: string };
      expect(props.yes).not.toMatch(/^(Да|Нет)$/);
      expect(props.no).not.toMatch(/^(Да|Нет)$/);
      expect(props.yes).not.toBe(props.no);
    }
    const snapshots = facts.find((f) => (f.props as { yes: string }).yes === "Снимки поддерживаются");
    expect(snapshots).toBeDefined();
    expect((snapshots!.props as { value: unknown }).value).toBe(true);
  });

  it("тон объявлен КАЖДОЙ стороне, и стороны звучат по-разному (правило 5)", () => {
    // Цвет следует за СМЫСЛОМ, а не за истинностью. Без объявленного тона обе
    // стороны уходят в нейтральный, и «Снимки недоступны» выглядит таким же
    // штатным положением, как «Снимки поддерживаются», — при том что закрытая
    // возможность это ровно та сторона, о которой стоит знать до создания тома.
    const below = ext("disk-types").overviewBelow!(ctx(CAPS));
    const facts = nodesOfType(below, BoolFact);
    expect(facts.length).toBeGreaterThan(0);
    for (const f of facts) {
      const props = f.props as { yesTone?: string; noTone?: string };
      expect(props.yesTone).toBeDefined();
      expect(props.noTone).toBeDefined();
      expect(props.yesTone).not.toBe(props.noTone);
    }
  });

  it("секции карточки — ОБЩАЯ поверхность и общие строки, а не своя таблица", () => {
    // Здесь стояла собственная `<table>` с колонкой подписи в 240 точек: на
    // одной странице жили два вида одного предмета — строки свойств сверху и
    // другие строки свойств снизу (правила 4 и 9 канона).
    const below = ext("disk-types").overviewBelow!(ctx({ ...CAPS, limits: { min_size_bytes: "1073741824" } }));
    expect(nodesOfType(below, DetailSurface).length).toBeGreaterThan(0);
    expect(nodesOfType(below, PropertyRows).length).toBeGreaterThan(0);
    // Отрицание — рядом с положительным выше: обход по сломанному дереву дал бы
    // пустой список и здесь, и там.
    expect(nodesOfType(below, "table")).toHaveLength(0);
  });

  it("способностей нет в ответе ⇒ секции нет вовсе (правило 9)", () => {
    // Иначе карточка утверждала бы «ничего не умеет» о классе, про который
    // сервер просто промолчал.
    const below = ext("disk-types").overviewBelow!(ctx({ id: "x" }));
    expect(nodesOfType(below, BoolFact)).toHaveLength(0);
  });

  it("ярус и состояние обращения названы словами, а не токеном", () => {
    const tier = JSON.stringify(valueOf("disk-types", CAPS, "Ярус"));
    expect(tier).toContain("Быстрый отклик");
    expect(tier).not.toMatch(/"FAST"/);
    const lifecycle = JSON.stringify(valueOf("disk-types", CAPS, "Обращение"));
    expect(lifecycle).toContain("Принимает новые тома");
  });
});

describe("действия-глаголы стоят на карточках, которым они принадлежат", () => {
  it("смена типа диска — у тома; копия — у снимка и образа", () => {
    // Отсутствие действия там, где контракт его завёл, — такая же находка, как
    // лишнее: глагол без кнопки недоступен из консоли вовсе.
    expect(ext("volumes").headerActions).toBeDefined();
    expect(ext("snapshots").headerActions).toBeDefined();
    expect(ext("images").headerActions).toBeDefined();
    // У каталога типов дисков действий нет: он read-only на публичной поверхности.
    expect(ext("disk-types").headerActions).toBeUndefined();
  });
});
