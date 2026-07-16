// RolesListShell — тонкая обёртка над generic ResourceListPage для Role.
//
// Backend-контракт: GET /iam/v1/roles → только system-роли (cluster-каталог);
// GET /iam/v1/roles?accountId=<acc> → system + custom-роли этого account'а.
// Роли ≠ Project/Group: system-каталог виден ВСЕГДА, поэтому это НЕ голый
// IamScopedListShell (тот прячет всё за «Выберите Account»). Scope-account —
// из шапочной пилюли (context-store); если не выбрана, auto-default на первый
// account из listAccounts, чтобы single-account-пользователь сразу видел свои
// custom-роли. pageSize=1000: Segmented «Кастомные» фильтрует client-side
// загруженную страницу (system-роли сортируются первыми → при дефолтном
// page-size custom-роли выпали бы на 2-ю страницу).

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ResourceListPage } from "@/components/organisms/ResourceListPage";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useContext } from "@shared/lib/context-store";
import { iamApi } from "@shared/api/iam";

export function RolesListShell() {
  const account = useContext((s) => s.account);

  const accountsQuery = useQuery({
    queryKey: ["iam", "accounts", "list"],
    queryFn: () => iamApi.listAccounts({ pageSize: "1000" }),
    staleTime: 30_000,
  });
  const accountList = useMemo(() => accountsQuery.data?.accounts ?? [], [accountsQuery.data]);
  const effectiveAccountId = account?.id ?? accountList[0]?.id ?? null;

  return effectiveAccountId ? (
    <ResourceListPage spec={REGISTRY.roles} parentField="accountId" parentValue={effectiveAccountId} pageSize="1000" />
  ) : (
    <ResourceListPage spec={REGISTRY.roles} pageSize="1000" />
  );
}
