// Per-resource API helpers. Обёртки над api/client.api.list/get.
// URL-ы verbatim из proto google.api.http annotations — это утверждение держит
// проба `lib/api-path-surface.test.ts`, которая сверяет каждый путь shared с
// http-аннотациями дерева proto (прежде тут стояло `/vpc/v1/route-tables`, чего
// на поверхности нет ни в одной ревизии; маршрут таблиц маршрутов — camelCase).
//
// Здесь стояло «используются ProjectSelector, DashboardPage и другими
// компонентами»: на 2026-08-04 у всех четырёх наборов ноль импортёров во всех
// девяти приложениях (предикат — `grep -rw <имя>` по ui-future без
// node_modules), потребители ходят через generic registry. Оставлены как
// поверхность пакета; названо честно, чтобы «используются» не читалось как факт.
//
// KAC-124: organization-manager + resource-manager упразднены, заменены на
// kaname.cloud.iam.v1 (Account / Project). Helpers под IAM лежат в api/iam.ts
// (iamApi.listAccounts / listProjects).

import { api } from "./client";
import type { NetworkList, SubnetList, AddressList, RouteTableList, Operation } from "./types";

// ====== vpc ======

// VPC-1: CIDR mutation is verb-only (:add-cidr-blocks / :remove-cidr-blocks),
// never PATCH — the declared Network supernet and the Subnet additional ranges
// are immutable through Update. Each verb returns an Operation. Family is chosen
// by which key is sent (ipv4_cidr_blocks / ipv6_cidr_blocks).
export const networksApi = {
  list: (q?: Record<string, string>) => api.list<NetworkList>("/vpc/v1/networks", q),
  addCidrBlocks: (id: string, blocks: { ipv4_cidr_blocks?: string[]; ipv6_cidr_blocks?: string[] }) =>
    api.action(`/vpc/v1/networks/${id}:add-cidr-blocks`, blocks),
  removeCidrBlocks: (id: string, blocks: { ipv4_cidr_blocks?: string[]; ipv6_cidr_blocks?: string[] }) =>
    api.action(`/vpc/v1/networks/${id}:remove-cidr-blocks`, blocks),
};

export const subnetsApi = {
  list: (q?: Record<string, string>) => api.list<SubnetList>("/vpc/v1/subnets", q),
  addCidrBlocks: (id: string, blocks: { ipv4_cidr_blocks?: string[]; ipv6_cidr_blocks?: string[] }) =>
    api.action(`/vpc/v1/subnets/${id}:add-cidr-blocks`, blocks),
  removeCidrBlocks: (id: string, blocks: { ipv4_cidr_blocks?: string[]; ipv6_cidr_blocks?: string[] }) =>
    api.action(`/vpc/v1/subnets/${id}:remove-cidr-blocks`, blocks),
};

export const addressesApi = {
  list: (q?: Record<string, string>) => api.list<AddressList>("/vpc/v1/addresses", q),
};

export const routeTablesApi = {
  list: (q?: Record<string, string>) => api.list<RouteTableList>("/vpc/v1/routeTables", q),
};

// ─────────────────────────────────────────────────────────────────────────────
// Конверт ресурса: пять глаголов платформы над одним базовым путём
// ─────────────────────────────────────────────────────────────────────────────
//
// `list`/`get`/`create`/`update`/`delete` — форма ПЛАТФОРМЫ, а не домена. Домен
// даёт ровно две вещи: базовый путь и тип страницы списка; всё остальное — как
// адресуется единичный ресурс, что уходит в тело, что возвращает мутация —
// одинаково для каждого ресурса каждого сервиса и вытекает из конвенций Kachō
// (чтение sync, мутация async через `Operation`).
//
// Пока эти пять строк пишет каждый модуль сам, всякое изменение формы правится
// в стольких местах, сколько ресурсов у консоли, и доезжает не всюду. Класс
// тихий: копия компилируется и структурно совпадает, расхождение начинается в
// день первой правки. На день заведения фабрики пятёрка была выписана ВОСЕМЬ
// раз — у compute, storage (трижды), nlb (трижды) и registry — и все восемь
// были дословно одинаковы.
//
// ЧТО ФАБРИКА НЕ ЗАБИРАЕТ У ДОМЕНА и забирать не должна: суффикс-действия
// (`:start`, `:attachDisk`, `:copy`, `:changeDiskType`, `:add-cidr-blocks`) и
// адресацию вложенных ресурсов (репозитории и теги живут ПОД реестром). У них
// разные тела, разные пути и разное число координат — общим они не выражаются.
// Домен добавляет их к фабрике распаковкой:
//
//     export const volumesApi = {
//       ...resourceApi<VolumeList>(VOLUMES),
//       changeDiskType: (id: string, diskTypeId: string) => …,
//     };
//
// Держится гейтом `shared/src/test/mutation-envelope.test.ts`: объект приложения,
// объявляющий все пять имён СВОИМИ свойствами, — находка.

/** Пять глаголов платформы над базовым путём ресурса. */
export interface ResourceApi<TList> {
  list: (query?: Record<string, string>) => Promise<TList>;
  get: (id: string) => Promise<Record<string, unknown>>;
  create: (body: unknown) => Promise<{ operation: Operation }>;
  update: (id: string, body: unknown) => Promise<{ operation: Operation }>;
  delete: (id: string) => Promise<{ operation: Operation }>;
}

/**
 * Конверт ресурса над `basePath`.
 *
 * Идентификатор подставляется в путь БЕЗ экранирования — ровно так, как это
 * делали все восемь сведённых сюда копий. Это не недосмотр, а сохранение
 * поведения: `id` платформы — crockford-base32 с дефисом (`api-conventions.md`
 * §«id-prefix — hyphen-канон»), в нём нет ни одного знака, который экранирование
 * изменило бы. Ввести экранирование значило бы сменить поведение восьми
 * ресурсов разом, и такой смене место в своём изменении со своим предметом, а
 * не в сведении копий. Вложенные ресурсы, где в путь попадает НЕ id (имя
 * репозитория, тег), адресуются доменом и экранируют сами.
 */
export function resourceApi<TList>(basePath: string): ResourceApi<TList> {
  const one = (id: string) => `${basePath}/${id}`;
  return {
    list: (query?: Record<string, string>) => api.list<TList>(basePath, query),
    get: (id: string) => api.get<Record<string, unknown>>(one(id)),
    create: (body: unknown) => api.create(basePath, body),
    update: (id: string, body: unknown) => api.update(one(id), body),
    delete: (id: string) => api.delete(one(id)),
  };
}

/**
 * Читающая половина конверта — для каталогов платформы (типы дисков, типы
 * машин): чтение публично, а вся правка живёт во внутреннем API на :9091
 * (ban #6), поэтому мутирующих глаголов у них нет и быть не должно.
 */
export function catalogApi<TList>(basePath: string): Pick<ResourceApi<TList>, "list" | "get"> {
  const { list, get } = resourceApi<TList>(basePath);
  return { list, get };
}
