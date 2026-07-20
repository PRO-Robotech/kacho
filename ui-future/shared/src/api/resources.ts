// Per-resource API helpers. Обёртки над api/client.api.list/get.
// Используются ProjectSelector, DashboardPage и другими компонентами,
// которые не могут пользоваться generic registry.
// URL-ы verbatim из proto google.api.http annotations.
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
  list: (q?: Record<string, string>) => api.list<RouteTableList>("/vpc/v1/route-tables", q),
};
