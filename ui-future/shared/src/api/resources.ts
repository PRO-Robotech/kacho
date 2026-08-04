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
// kacho.cloud.iam.v1 (Account / Project). Helpers под IAM лежат в api/iam.ts
// (iamApi.listAccounts / listProjects).

import { api } from "./client";
import type { NetworkList, SubnetList, AddressList, RouteTableList } from "./types";

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
