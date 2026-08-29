import { isValidElement, type ReactElement, type ReactNode } from "react";

import { REGISTRY } from "./resource-registry";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";
import { PlacementAnchor } from "@shared/components/molecules/PlacementAnchor";

/**
 * Раздел `/registry/*` читает ОБЩИЙ реестр ресурсов (issue #409).
 *
 * ПРЕДМЕТ. Реестр ресурсов у консоли один, и общая оболочка карточки
 * (`ResourceShell`), общий подборщик ссылок (`RefSelect`) и общая ссылка на
 * чужой ресурс (`RefNameLink`) резолвят спеку ПО ИДЕНТИФИКАТОРУ именно в нём.
 * Пока три записи домена жили только в модуле, ссылка на реестр из соседнего
 * раздела вырождалась в плоский идентификатор — то есть одно поле читалось
 * двумя разными способами, смотря откуда на него смотрят.
 *
 * ПОЧЕМУ ПРОБА ЖИВЁТ В ОБЩЕМ МОДУЛЕ, А НЕ В `registry/`. Утверждается свойство
 * ОБЩЕГО реестра, и падать оно обязано у КАЖДОГО домена, который его исполняет,
 * — иначе снятие записи заметил бы только тот, кто гоняет пробы registry.
 *
 * ЧТО УТВЕРЖДАЕТСЯ, А ЧТО НЕТ. Здесь — состав и те свойства записей, которые
 * ломаются молча: маршрут, путь края, ключ полезной нагрузки, набор глаголов и
 * вид якоря размещения. Доменные строки карточки (адрес, видимость, класс
 * исчезаемости) утверждает проба расширений в самом модуле: их рисует
 * регистрация, а не реестр.
 */

/** Записи, которые раздел `/registry/*` показывает как СВОИ ресурсы. */
const DOMAIN = ["registries", "repositories", "tags"] as const;

/** Обходит дерево элементов и возвращает все узлы указанного типа. */
function nodesOfType(node: unknown, type: unknown): ReactElement[] {
  const out: ReactElement[] = [];
  const seen = new Set<unknown>();
  const walk = (n: unknown): void => {
    if (n === null || n === undefined || typeof n !== "object") return;
    if (Array.isArray(n)) {
      n.forEach(walk);
      return;
    }
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

const REGISTRY_ROW = {
  id: "reg-1",
  name: "my-registry",
  region_id: "ru-central1",
  placement_type: "REGIONAL",
  status: "ACTIVE",
  repository_count: 3,
  endpoint: "registry.kacho.local/reg-1",
};

describe("общий реестр несёт ресурсы раздела registry", () => {
  it(`перепись: записей в общем реестре ${Object.keys(REGISTRY).length}, из них домена registry ${
    DOMAIN.filter((id) => REGISTRY[id]).length
  } из ${DOMAIN.length}`, () => {
    // Пустой обход сделал бы всё нижеследующее вакуумно истинным.
    expect(Object.keys(REGISTRY).length).toBeGreaterThan(20);
    // Ссылочная цель домена — регион каталога geo; без неё `RefSelect` не
    // резолвит поле «Регион» формы создания реестра.
    expect(REGISTRY.regions).toBeDefined();
  });

  it.each(DOMAIN)("запись «%s» объявлена в общем реестре", (id) => {
    // Координата, а не счёт: красная проба обязана назвать, какой записи нет.
    expect(REGISTRY[id]).toBeDefined();
  });

  it("пути края и ключи полезной нагрузки — те, что объявляет контракт registry", () => {
    expect(REGISTRY.registries).toMatchObject({
      route: "registries",
      apiPath: "/registry/v1/registries",
      payloadKey: "registries",
      scope: "project",
    });
    expect(REGISTRY.repositories).toMatchObject({
      route: "repositories",
      apiPath: "/registry/v1/registries/{registryId}/repositories",
      payloadKey: "repositories",
    });
    expect(REGISTRY.tags).toMatchObject({
      route: "tags",
      apiPath: "/registry/v1/registries/{registryId}/repositories/{repository}/tags",
      payloadKey: "tags",
    });
  });

  it("глаголы объявлены по контракту: репозиторий и тег не создаются консолью", () => {
    // Репозиторий материализуется первым `docker push`, тег пишется тем же
    // push'ем — единственная мутация домена сверх реестра это удаление тега.
    expect(REGISTRY.registries.ops).toEqual({ create: true, update: true, delete: true });
    expect(REGISTRY.repositories.ops).toEqual({ create: false, update: false, delete: false });
    expect(REGISTRY.tags.ops).toEqual({ create: false, update: false, delete: true });
  });

  it("дочерние списки объявлены: репозитории под реестром, теги под репозиторием", () => {
    expect(REGISTRY.registries.related).toEqual([
      { childId: "repositories", filterField: "registry_id", label: "Репозитории" },
    ]);
    expect(REGISTRY.repositories.related).toEqual([
      { childId: "tags", filterField: ["registry_id", "repository"], label: "Теги" },
    ]);
  });

  it("правило 2: размещение реестра — ССЫЛКА на каталог geo, а не плоский идентификатор", () => {
    // Утверждается дерево элементов, а не текст: проба по тексту осталась бы
    // зелёной на плоском идентификаторе, из которого некуда пойти, — ровно на
    // том дефекте, который правило 2 запрещает.
    const col = REGISTRY.registries.columns.find((c) => c.path === "region_id");
    expect(col).toBeDefined();
    const anchors = nodesOfType(col?.render?.(REGISTRY_ROW), PlacementAnchor);
    expect(anchors).toHaveLength(1);

    const props = anchors[0].props as { row: Record<string, unknown>; maxChars?: number };
    const link = nodesOfType(PlacementAnchor(props), RefNameLink)[0];
    expect(link.props).toMatchObject({ specId: "regions", refId: "ru-central1" });
    // Глобальный каталог спрашивается БЕЗ project_id: область видимости берётся
    // из спеки, а переданный вручную проп её перекрыл бы.
    expect(link.props).not.toHaveProperty("projectId", "prj-1");
  });

  it("положительный контроль: соседняя колонка идентичности осталась колонкой", () => {
    // Отрицание выше («не плоский текст») зеленело бы на реестре без колонок
    // вовсе — поэтому рядом стоит утверждение, что колонки на месте.
    const headers = REGISTRY.registries.columns.map((c) => c.header);
    expect(headers).toContain("Имя");
    expect(headers).toContain("Адрес");
    expect(headers.length).toBeGreaterThan(4);
  });
});
