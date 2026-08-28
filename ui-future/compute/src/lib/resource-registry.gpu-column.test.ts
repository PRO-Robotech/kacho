// Колонка «GPU-модель» — ЕДИНСТВЕННОЕ, чем реестр этого домена отличается от
// общего, и у отличия есть предикат снятия (#406).
//
// ПРЕДМЕТ. `EffectiveResources.gpu_type` — поле контракта
// (`proto/kacho/cloud/compute/v1/machine_type.proto`, «GPU silicon model»), и там
// же сказано, что число и модель ускорителя авторитетны для GPU-семейства. Общий
// реестр показывает ЧИСЛО и не показывает МОДЕЛЬ, поэтому свести реестр «в лоб»
// значило бы снять возможность у пользователя молча.
//
// САМОИСТЕЧЕНИЕ. Место колонки — в общей спеке: модель ускорителя не свойство
// раздела compute, а свойство типа машины. Как только общий реестр её заведёт,
// утверждение ниже ПОКРАСНЕЕТ и потребует снять дельту вместе с наложкой —
// послабление, пережившее свой предмет, есть тот самый класс, который мы ловим
// в коде. Пока не завёл — красным становится ПРОПАЖА колонки у домена.

import { REGISTRY as SHARED_REGISTRY } from "@shared/lib/resource-registry";

import { GPU_MODEL_COLUMN_PATH, REGISTRY } from "./resource-registry";

const paths = (specId: string, registry: typeof REGISTRY) =>
  (registry[specId]?.columns ?? []).map((c) => c.path);

describe("каталог типов машин: модель ускорителя показана", () => {
  it("объём осмотренного непуст — «ноль находок» отличимо от «ноль прочитанного»", () => {
    expect(paths("machine-types", REGISTRY).length).toBeGreaterThan(5);
    expect(paths("machine-types", SHARED_REGISTRY).length).toBeGreaterThan(5);
  });

  it("колонка есть у домена", () => {
    expect(paths("machine-types", REGISTRY)).toContain(GPU_MODEL_COLUMN_PATH);
  });

  it("колонка стоит сразу за числом ускорителей — читается парой, а не порознь", () => {
    const p = paths("machine-types", REGISTRY);
    expect(p.indexOf(GPU_MODEL_COLUMN_PATH)).toBe(p.indexOf("effective_resources.gpus") + 1);
  });

  it("дельта НЕ задвоена: колонка появляется ровно один раз", () => {
    // Наложка идемпотентна by construction, и это утверждается: заведи общий
    // реестр ту же колонку — и слепое добавление дало бы два одинаковых столбца
    // вместо сигнала снять дельту.
    expect(paths("machine-types", REGISTRY).filter((x) => x === GPU_MODEL_COLUMN_PATH)).toHaveLength(1);
  });

  it("ПРЕДИКАТ СНЯТИЯ: общий реестр колонки ещё НЕ несёт", () => {
    // Красное здесь означает не поломку, а достижение цели: колонка переехала в
    // общий реестр. Тогда снимаются — эта проба, `GPU_MODEL_COLUMN` и вся
    // наложка `withGpuModelColumn`, а реестр домена становится чистой проекцией.
    expect(paths("machine-types", SHARED_REGISTRY)).not.toContain(GPU_MODEL_COLUMN_PATH);
  });
});
