// Реестр ресурсов compute-remote — ПРОЕКЦИЯ общего реестра, а не его копия.
//
// ЧТО ЗДЕСЬ БЫЛО. Восемь спек на 683 строки, объявленных заново: маршруты,
// колонки, поля формы, `sanitize`, `validate`, пустые состояния. Все восемь
// объявлены и в `@shared/lib/resource-registry`, и общий там был БОГАЧЕ по
// каждой оси — перепись по путям полей дала 23 поля, которых у копии не было
// (сетевой интерфейс 8, зона 3, том 2, группа размещения 6, образ 1, машина 3),
// и НИ ОДНОГО свойства спеки, которое было бы только у копии.
//
// Отставание было содержательным, а не косметическим. Четыре спеки из восьми
// (`zones`, `volumes`, `images`, `network-interfaces`) держались здесь как
// ref-цели форм — и не резолвили НИЧЕГО: `RefSelect` и `RefNameLink` ходят
// прямо в `@shared/lib/resource-registry`, поэтому копия на резолв не влияла
// никогда. То есть 4 спеки из 8 были мёртвым кодом, который выглядел живым.
//
// ЧТО ОСТАЛОСЬ СВОИМ — ровно одна колонка, и она названа ниже поимённо.
// Модульность самого реестра при этом сохранена намеренно: в нём ровно те
// ресурсы, которые приложение показывает (`scoped-resources.ts`), поэтому
// исчезновение спеки из общего остаётся громким, а чужие разделы не
// притворяются здешними.

import type { ResourceColumn, ResourceSpec } from "@shared/lib/resource-spec";
import { REGISTRY as SHARED_REGISTRY } from "@shared/lib/resource-registry";

import { ALL_SCOPED_IDS } from "./scoped-resources";

// Форма ресурса объявлена ОДИН раз — в `@shared/lib/resource-spec`. Реэкспорт
// оставлен, чтобы потребители не меняли импорты: у него нет тела, поэтому
// разойтись с источником он не может. Собственное ОБЪЯВЛЕНИЕ формы здесь
// запрещено (KAC #132) — его ловит scripts/check-resource-spec-single-source.mjs.
export type { ResourceColumn, ResourceSpec };

// Помощники читаются из общего — своих реализаций у них здесь нет и не будет.
export {
  applyFieldDefaults,
  editReadPath,
  getByPath,
  mutationBasePath,
  resourceProjectPath,
} from "@shared/lib/resource-registry";
export {
  isSystemScopedResource,
  resourceListPath,
  resourceServicePrefix,
  type ServicePrefix,
} from "@shared/lib/service-prefix";

/**
 * ЕДИНСТВЕННОЕ, чем реестр этого домена отличается от общего: колонка «GPU-модель».
 *
 * `EffectiveResources.gpu_type` — поле контракта
 * (`proto/kacho/cloud/compute/v1/machine_type.proto:90`, «GPU silicon model»),
 * и там же сказано, что `gpus`/`gpuType` авторитетны для GPU-семейства. Общий
 * реестр показывает ЧИСЛО ускорителей и не показывает их МОДЕЛЬ — то есть по
 * этой колонке беднее домена, и свести реестр «в лоб» значило бы снять
 * возможность у пользователя.
 *
 * ПОЧЕМУ ДЕЛЬТА ЖИВЁТ ЗДЕСЬ, А НЕ В ОБЩЕМ. Место этой колонки — в общей спеке
 * `machine-types`: модель ускорителя не свойство раздела compute, а свойство
 * типа машины, и всякий, кто покажет каталог типов, обязан показывать её тоже.
 * Перенос требует правки `shared/src/lib/resource-registry.tsx` — файла, в
 * который в эти же сутки пишут ещё три линии де-форка (storage, nlb, registry),
 * поэтому он вынесен отдельным изменением, а не сделан здесь молча.
 *
 * ПРЕДИКАТ СНЯТИЯ — машинный и самоистекающий: как только общая спека заведёт
 * `effective_resources.gpu_type`, проба `resource-registry.gpu-column.test.ts`
 * ПОКРАСНЕЕТ и потребует снять эту дельту вместе с наложкой. Послабление,
 * пережившее свой предмет, — тот же класс, который мы ловим в коде.
 */
const GPU_MODEL_COLUMN: ResourceColumn = {
  header: "GPU-модель",
  path: "effective_resources.gpu_type",
  format: "code",
};

/** Идентификатор поля, по которому дельта опознаётся, — один на объявление и пробу. */
export const GPU_MODEL_COLUMN_PATH = GPU_MODEL_COLUMN.path;

/** Спека общего реестра плюс колонка модели ускорителя сразу за числом ускорителей. */
function withGpuModelColumn(spec: ResourceSpec): ResourceSpec {
  const columns = spec.columns ?? [];
  if (columns.some((c) => c.path === GPU_MODEL_COLUMN.path)) return spec;
  const afterGpus = columns.findIndex((c) => c.path === "effective_resources.gpus");
  // Не найдено — колонка встаёт в конец, а не теряется: перечень колонок общей
  // спеки может измениться, и молчаливая потеря дельты была бы ровно тем, ради
  // чего дельта заведена.
  const at = afterGpus === -1 ? columns.length : afterGpus + 1;
  return { ...spec, columns: [...columns.slice(0, at), GPU_MODEL_COLUMN, ...columns.slice(at)] };
}

/**
 * Реестр приложения: спеки общего реестра по перечню `scoped-resources`.
 *
 * `undefined` не отбрасывается `filter(Boolean)` и не подменяется заглушкой:
 * спека, исчезнувшая из общего реестра, обязана быть ГРОМКОЙ, и её ловит
 * `scoped-resources.test.ts`. Здесь она просто не попадёт в объект, а проба
 * назовёт её по имени.
 */
export const REGISTRY: Record<string, ResourceSpec> = Object.fromEntries(
  ALL_SCOPED_IDS.flatMap((id) => {
    const spec = SHARED_REGISTRY[id];
    if (!spec) return [];
    return [[id, id === "machine-types" ? withGpuModelColumn(spec) : spec]] as const;
  }),
);

export function getResource(id: string): ResourceSpec | undefined {
  return REGISTRY[id];
}
