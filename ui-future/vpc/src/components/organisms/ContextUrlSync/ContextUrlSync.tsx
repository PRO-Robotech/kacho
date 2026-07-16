// ContextUrlSync — синхронизирует context-store с path-based URL.
// KAC-117: модель Account/Project (раньше Org/Cloud/Folder). URL-формат:
//   /accounts                                  — список аккаунтов
//   /accounts/:accountId/projects              — projects текущего account
//   /projects/:projectId                        — project dashboard
//   /projects/:projectId/vpc/networks (...)     — project selected, vpc list
//   /projects/:projectId/vpc/networks/:uid      — detail
//
// При смене URL → парсинг → обновление context-store. Имена ресурсов в context
// заполняются позже когда BreadcrumbSelector загрузит соответствующий list.
//
// Hydration: если URL содержит projectId/accountId но context из localStorage
// пустой (прямая ссылка / инкогнито), мы догружаем родителя через
// GET /<resource>/{id} цепочкой, чтобы pills заполнились name'ами.

import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "@shared/api/client";
import { contextApi, useContext } from "@shared/lib/context-store";

interface ProjectApi {
  id: string;
  name: string;
  accountId: string;
}
interface AccountApi {
  id: string;
  name: string;
}

export function ContextUrlSync() {
  const { pathname } = useLocation();
  const ctx = useContext((s) => s);

  // Hydration: GET для project/account когда у нас в context есть id но нет name.
  const projectHydrate = useQuery({
    queryKey: ["hydrate-project", ctx.project?.id],
    queryFn: () => api.get<ProjectApi>(`/iam/v1/projects/${ctx.project!.id}`),
    enabled: !!ctx.project?.id && (!ctx.project.name || !ctx.project.accountId),
    staleTime: 60_000,
  });
  const accountHydrate = useQuery({
    queryKey: ["hydrate-account", ctx.account?.id],
    queryFn: () => api.get<AccountApi>(`/iam/v1/accounts/${ctx.account!.id}`),
    enabled: !!ctx.account?.id && !ctx.account.name,
    staleTime: 60_000,
  });

  // Применяем результаты hydration в context.
  useEffect(() => {
    if (projectHydrate.data && ctx.project?.id === projectHydrate.data.id) {
      const p = projectHydrate.data;
      const needName = !ctx.project.name && !!p.name;
      const needAcc = !ctx.project.accountId && !!p.accountId;
      if (needName || needAcc) {
        contextApi.hydrate({
          project: { name: p.name, accountId: p.accountId },
          account:
            needAcc && (!ctx.account || ctx.account.id !== p.accountId)
              ? { id: p.accountId, name: ctx.account?.name ?? "" }
              : undefined,
        });
      }
    }
  }, [projectHydrate.data, ctx.project, ctx.account]);

  useEffect(() => {
    if (accountHydrate.data && ctx.account?.id === accountHydrate.data.id) {
      const a = accountHydrate.data;
      if (!ctx.account.name && a.name) {
        contextApi.hydrate({ account: { name: a.name } });
      }
    }
  }, [accountHydrate.data, ctx.account]);

  // Парсинг URL → обновление context.
  useEffect(() => {
    const ids = parsePathIds(pathname);
    const cur = contextApi.get();
    const curAcc = cur.account?.id ?? null;
    const curProj = cur.project?.id ?? null;

    if (ids.accountId && ids.accountId !== curAcc) {
      contextApi.setAccount({ id: ids.accountId, name: cur.account?.name ?? "" });
    }
    if (ids.projectId && ids.projectId !== curProj) {
      contextApi.setProject({
        id: ids.projectId,
        name: cur.project?.name ?? "",
        accountId: cur.account?.id ?? ids.accountId ?? "",
      });
    }

    // Explicit reset когда пользователь вышел в корень.
    if (pathname === "/" || pathname === "/accounts") {
      if (curAcc || curProj) {
        contextApi.setAccount(null);
      }
    }
  }, [pathname]);

  return null;
}

function parsePathIds(pathname: string): {
  accountId: string | null;
  projectId: string | null;
} {
  const accMatch = pathname.match(/^\/accounts\/([^/]+)/);
  const projMatch = pathname.match(/^\/projects\/([^/]+)/);
  return {
    accountId: accMatch?.[1] ?? null,
    projectId: projMatch?.[1] ?? null,
  };
}
