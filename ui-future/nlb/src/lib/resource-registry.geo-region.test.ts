// Справочник регионов в реестре nlb — сверка с публичной проекцией geo.
//
// Ground truth: proto/kacho/cloud/geo/v1/region.proto. Публичный `Region` несёт
// ровно {id, name, created_at, country_code, open_for_placement,
// open_zone_count_hint}. Поля `status` у него НЕТ — и не было ни в одной
// ревизии продукта: состояние региона живёт только в `InternalRegion`
// (`GeoStatus status = 5`), а тот выставлен на cluster-internal листенере и
// публичной поверхности не касается (two-projection, `security.md`
// §«Инфра-чувствительные данные»).
//
// Поэтому колонка «Статус» в списке регионов была не устаревшей, а адресующей
// поле, которого в этом продукте никогда не существовало: она рендерила пустое
// значение при любом ответе сервера. Это тот же класс, что адрес без
// производителя на крае, — не «отстало от контракта», а «не существовало».
//
// Зона — другой случай: у неё `status` БЫЛ и снят намеренно
// (zone.proto:24-25 `reserved 3; reserved "status";`), а признаком доступности
// стало `open_for_placement`.

import { REGISTRY } from "./resource-registry";

/** Поля публичного geo.Region (region.proto, message Region). */
const PUBLIC_REGION_FIELDS = [
  "id",
  "name",
  "created_at",
  "country_code",
  "open_for_placement",
  "open_zone_count_hint",
];

/** Поля публичного geo.Zone (zone.proto, message Zone). */
const PUBLIC_ZONE_FIELDS = ["id", "region_id", "name", "created_at", "open_for_placement", "placement_blocked_reason"];

describe("справочники geo в реестре nlb", () => {
  it("колонки регионов адресуют только поля публичного Region", () => {
    const spec = REGISTRY["compute-regions"];
    expect(spec).toBeDefined();
    const paths = (spec.columns ?? []).map((c) => c.path.split(".")[0]);
    expect(paths.length).toBeGreaterThan(0);
    for (const p of paths) {
      expect(PUBLIC_REGION_FIELDS).toContain(p);
    }
  });

  it("колонки зон адресуют только поля публичной Zone", () => {
    // Положительный близнец: та же форма проверки на соседнем справочнике,
    // который сегодня верен, — иначе «красный» выше нельзя отличить от гейта,
    // который краснеет на чём угодно.
    const spec = REGISTRY["zones"];
    expect(spec).toBeDefined();
    const paths = (spec.columns ?? []).map((c) => c.path.split(".")[0]);
    expect(paths.length).toBeGreaterThan(0);
    for (const p of paths) {
      expect(PUBLIC_ZONE_FIELDS).toContain(p);
    }
  });

  it("оба справочника читают geo, а не чужой домен", () => {
    expect(REGISTRY["compute-regions"].apiPath).toBe("/geo/v1/regions");
    expect(REGISTRY["zones"].apiPath).toBe("/geo/v1/zones");
  });
});
