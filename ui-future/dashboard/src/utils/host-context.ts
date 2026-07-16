const STORAGE_KEY = "kacho.context.v2";

export interface AccountRef {
  id: string;
  name: string;
}

export interface ProjectRef {
  id: string;
  name: string;
  accountId: string;
}

export interface HostContext {
  account: AccountRef | null;
  project: ProjectRef | null;
}

export function loadHostContext(): HostContext {
  const ids = parsePathIds(window.location.pathname);
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<HostContext>;
      return mergeUrlContext(
        {
          account: parsed.account ?? null,
          project: parsed.project ?? null,
        },
        ids,
      );
    }
  } catch {
    // ignore invalid shell state
  }
  return mergeUrlContext({ account: null, project: null }, ids);
}

function parsePathIds(pathname: string): {
  accountId: string | null;
  projectId: string | null;
} {
  const accountMatch = pathname.match(/^\/accounts\/([^/]+)/);
  const projectMatch = pathname.match(/^\/projects\/([^/]+)/);
  return {
    accountId: accountMatch?.[1] ?? null,
    projectId: projectMatch?.[1] ?? null,
  };
}

function mergeUrlContext(context: HostContext, ids: ReturnType<typeof parsePathIds>): HostContext {
  const next = { ...context };
  if (ids.accountId && ids.accountId !== context.account?.id) {
    next.account = { id: ids.accountId, name: "" };
    next.project = null;
  }
  if (ids.projectId && ids.projectId !== context.project?.id) {
    next.project = {
      id: ids.projectId,
      name: "",
      accountId: ids.accountId ?? next.account?.id ?? "",
    };
  }
  return next;
}
