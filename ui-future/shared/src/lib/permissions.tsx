// Permission gate utilities (KAC items 1-5 Foundation).
//
// Источник данных — `whoami: WhoAmIResponse` из `AuthContext` (GET /iam/v1/me).
//
// Использование:
//
//   const { isSystemAdmin, hasAccountRole } = usePermissions();
//   if (!isSystemAdmin) return <Hidden/>;
//
//   <RequirePermission check={(p) => p.isSystemAdmin}>
//     <DeleteButton/>
//   </RequirePermission>
//
//   <DisabledIfNot check={(p) => p.isSystemAdmin} reason="Требуется system_admin">
//     <Button>Создать Region</Button>
//   </DisabledIfNot>
//
// Также — `mapApiErrorTo403Message` для toast-mapping rich `deny_reasons`
// в человеческие сообщения (KAC item #4).

import { cloneElement, useMemo, type ReactElement, type ReactNode } from "react";
import { Tooltip } from "antd";
import { useAuth } from "@shared/contexts/AuthContext";
import { extractDenyReasons, type AccountMembership, type WhoAmIResponse } from "@shared/api/auth";
import { ApiError } from "@shared/api/client";

/** Аггрегированный permission-snapshot для текущего user'а. */
export interface PermissionSnapshot {
  /** True если /iam/v1/me вернул system_admin=true (cluster-wide). */
  isSystemAdmin: boolean;
  /** True если cluster_viewer (read-only cluster level). */
  isClusterViewer: boolean;
  /** Загружен ли whoami (false до первого fetch'а — UI обычно показывает Spin). */
  loaded: boolean;
  /** Список account-membership-ов. */
  accounts: AccountMembership[];
  /**
   * Есть ли у user'а указанная role в указанном account'е.
   * Сравнение по equality с role.id ИЛИ role.name (backend может вернуть либо).
   */
  hasAccountRole: (accountId: string, role: string) => boolean;
  /** Список всех ролей user'а в указанном account'е (или [] если не member). */
  rolesInAccount: (accountId: string) => string[];
  /** True если user — member указанного account'а (с любой ролью). */
  isMemberOfAccount: (accountId: string) => boolean;
}

const EMPTY_SNAPSHOT: PermissionSnapshot = {
  isSystemAdmin: false,
  isClusterViewer: false,
  loaded: false,
  accounts: [],
  hasAccountRole: () => false,
  rolesInAccount: () => [],
  isMemberOfAccount: () => false,
};

/**
 * Hook возвращает PermissionSnapshot для текущего user'а. Безопасно вызывать
 * до того как whoami загрузился (вернёт `loaded:false`).
 */
export function usePermissions(): PermissionSnapshot {
  const { whoami, loading } = useAuth();
  return useMemo(() => buildSnapshot(whoami, loading), [whoami, loading]);
}

export function buildSnapshot(whoami: WhoAmIResponse | null, loading: boolean): PermissionSnapshot {
  if (!whoami) {
    // Если ещё loading — НЕ говорим isSystemAdmin=false уверенно;
    // вызывающий код через `loaded` сам решит, скрывать или ждать.
    return { ...EMPTY_SNAPSHOT, loaded: !loading };
  }
  const byAccount = new Map<string, AccountMembership>();
  for (const a of whoami.accounts ?? []) {
    if (a?.account_id) byAccount.set(a.account_id, a);
  }
  return {
    isSystemAdmin: !!whoami.system_admin,
    isClusterViewer: !!whoami.cluster_viewer,
    loaded: true,
    accounts: whoami.accounts ?? [],
    hasAccountRole: (accountId, role) => {
      const a = byAccount.get(accountId);
      if (!a) return false;
      return (a.roles ?? []).some((r) => r === role || r.endsWith(`/${role}`));
    },
    rolesInAccount: (accountId) => byAccount.get(accountId)?.roles ?? [],
    isMemberOfAccount: (accountId) => byAccount.has(accountId),
  };
}

// ====== <RequirePermission> ======

interface RequirePermissionProps {
  /** Predicate над snapshot'ом. true → render `children`. */
  check: (p: PermissionSnapshot) => boolean;
  /** Альтернатива для отрицательной ветки. По умолчанию — `null` (скрыть). */
  fallback?: ReactNode;
  /** Пока whoami грузится — render `loadingFallback` (или ничего). */
  loadingFallback?: ReactNode;
  children: ReactNode;
}

/**
 * Conditional render по permission'у. Если check(snap)=false → возвращает
 * `fallback` (default null = hidden).
 */
export function RequirePermission({
  check,
  fallback = null,
  loadingFallback = null,
  children,
}: RequirePermissionProps) {
  const p = usePermissions();
  if (!p.loaded) return <>{loadingFallback}</>;
  if (!check(p)) return <>{fallback}</>;
  return <>{children}</>;
}

// ====== <DisabledIfNot> ======

interface DisabledIfNotProps {
  /** Predicate. true → child render как обычно; false → wrap в Tooltip+disabled. */
  check: (p: PermissionSnapshot) => boolean;
  /** Текст tooltip'а, когда disabled. */
  reason: string;
  /** Single child должен поддерживать `disabled` prop (Button / Menu.Item / …). */
  children: ReactElement;
}

/**
 * Оборачивает кнопку (или другой interactive component) и делает её disabled
 * + добавляет Tooltip с reason, если у user'а нет permission'а. Для UX лучше
 * чем полностью скрывать destructive action — user понимает почему недоступно.
 *
 * NB: child обязан поддерживать `disabled` prop. Если нет — используйте
 * `<RequirePermission/>` с fallback'ом.
 */
export function DisabledIfNot({ check, reason, children }: DisabledIfNotProps) {
  const p = usePermissions();
  if (!p.loaded || check(p)) {
    return children;
  }
  // Tooltip оборачивает disabled button → AntD требует extra span для events.
  return (
    <Tooltip title={reason}>
      <span style={{ cursor: "not-allowed" }}>{withDisabled(children)}</span>
    </Tooltip>
  );
}

function withDisabled(el: ReactElement): ReactElement {
  // Безопасно type-cast'им — caller гарантирует, что child принимает `disabled`.
  const existing = (el.props ?? {}) as Record<string, unknown>;
  return cloneElement(el as ReactElement<{ disabled?: boolean }>, { ...existing, disabled: true });
}

// ====== 403 mapper ======

/**
 * Извлекает человеческое сообщение из ApiError для 403-ответов:
 *   - если есть `details[].metadata.deny_reasons` (KAC item #4) — join'ит их messages;
 *   - иначе — fallback на `error.message` или generic "Permission denied".
 *
 * Используется в toast-error / inline Alert.
 */
export function mapApiErrorToMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const reasons = extractDenyReasons(err.details);
    if (reasons.length > 0) {
      return reasons.map((r) => r.message).join("; ");
    }
    return err.message || (err.status === 403 ? "Permission denied" : "Ошибка");
  }
  if (err instanceof Error) return err.message;
  return String(err ?? "Ошибка");
}

/** True если err — ApiError со status=403/code=PERMISSION_DENIED. */
export function isPermissionDeniedError(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 403 || err.code === "7");
}

/** True если err — ApiError со status=409 / code=ALREADY_EXISTS (gRPC code 6). */
export function isAlreadyExistsError(err: unknown): boolean {
  if (!(err instanceof ApiError)) return false;
  return err.status === 409 || err.code === "6" || err.code === "ALREADY_EXISTS";
}
