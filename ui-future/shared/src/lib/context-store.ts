// Context store: Account → Project breadcrumb-state.
//
// Account = top-level tenant, Project = child of Account (см. workspace
// CLAUDE.md «Что это за проект»). VPC / Compute ресурсы scoped по Project.
//
// Persist в localStorage (`kacho.context.v2`).

import { useSyncExternalStore } from "react";

export interface AccountRef {
  id: string;
  name: string;
}

export interface ProjectRef {
  id: string;
  name: string;
  accountId: string;
}

interface State {
  account: AccountRef | null;
  project: ProjectRef | null;
}

const KEY = "kacho.context.v2";

function emptyState(): State {
  return { account: null, project: null };
}

function load(): State {
  if (typeof window === "undefined") return emptyState();
  try {
    const raw = window.localStorage.getItem(KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      return { account: parsed.account ?? null, project: parsed.project ?? null };
    }
  } catch {
    // ignore
  }
  return emptyState();
}

let state: State = load();
const listeners = new Set<() => void>();

function persist() {
  try {
    window.localStorage.setItem(KEY, JSON.stringify({ account: state.account, project: state.project }));
  } catch {
    // ignore
  }
}

function setState(next: State) {
  state = next;
  persist();
  listeners.forEach((l) => l());
}

export const contextApi = {
  get: () => state,

  setAccount(account: AccountRef | null) {
    // Сменили Account — сбрасываем Project.
    setState({ account, project: null });
  },

  setProject(project: ProjectRef | null) {
    if (!project) {
      setState({ account: state.account, project: null });
      return;
    }
    // Если проект из другого Account — переключаем account-ref на новый id
    // (name догрузится через ContextUrlSync hydration).
    const account =
      state.account && state.account.id === project.accountId ? state.account : { id: project.accountId, name: "" };
    setState({ account, project });
  },

  /** Patch — обновить отдельные поля без сброса потомков. */
  hydrate(patch: { account?: Partial<AccountRef>; project?: Partial<ProjectRef> }) {
    const next: State = { account: state.account, project: state.project };
    if (patch.account) {
      if (state.account) {
        next.account = { ...state.account, ...patch.account };
      } else if (patch.account.id) {
        next.account = { id: patch.account.id, name: patch.account.name ?? "" };
      }
    }
    if (patch.project && state.project) {
      next.project = { ...state.project, ...patch.project };
    } else if (patch.project?.id) {
      next.project = {
        id: patch.project.id,
        name: patch.project.name ?? "",
        accountId: patch.project.accountId ?? state.account?.id ?? "",
      };
    }
    setState(next);
  },

  subscribe(l: () => void) {
    listeners.add(l);
    return () => listeners.delete(l);
  },
};

export function useContext<T>(selector: (s: State) => T): T {
  return useSyncExternalStore(
    contextApi.subscribe,
    () => selector(state),
    () => selector(state),
  );
}

export function useProjectStore<T>(selector: (s: { project: ProjectRef | null }) => T): T {
  return useSyncExternalStore(
    contextApi.subscribe,
    () => selector({ project: state.project }),
    () => selector({ project: state.project }),
  );
}
