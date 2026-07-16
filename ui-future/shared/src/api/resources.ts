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

export const networksApi = {
  list: (q?: Record<string, string>) => api.list<NetworkList>("/vpc/v1/networks", q),
};

export const subnetsApi = {
  list: (q?: Record<string, string>) => api.list<SubnetList>("/vpc/v1/subnets", q),
};

export const addressesApi = {
  list: (q?: Record<string, string>) => api.list<AddressList>("/vpc/v1/addresses", q),
};

export const routeTablesApi = {
  list: (q?: Record<string, string>) => api.list<RouteTableList>("/vpc/v1/route-tables", q),
};
