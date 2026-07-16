// AuthContext — централизованный auth state для kacho-ui (KAC-127 Phase 2).
//
// Что внутри:
//   - user / session (из api-gateway /iam/v1/auth/me + Kratos /sessions/whoami)
//   - access-token (in-memory только; никогда не в localStorage)
//   - mfaFreshUntil (timestamp) — для step-up RequireMFAFresh-guard
//   - login() / logout() / refresh() — высокоуровневые actions
//
// Аутентификация data-plane запросов — ambient httpOnly session cookie
// (Kratos/Hydra), выписанная api-gateway middleware; access-token держится
// in-memory (setAccessToken) для консюмеров, которым он нужен явно.
//
// Backward-compat для KAC-115 (Logout, HeaderAuth, LoginButton, UserMenu) —
// `useAuth` экспозит те же поля `user / loading / login / logout / refresh /
// hasPermission` плюс новые расширения. Старые consumers продолжают работать.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { authApi, hasPermission as checkPerm, type AuthUser, type WhoAmIResponse } from "@shared/api/auth";
import { kratos, type KratosSession } from "@shared/lib/kratos";
import { config } from "@shared/lib/config";

/** Периодический whoami-refresh — каждые 5 минут (KAC items 1-5 Foundation). */
const WHOAMI_REFETCH_MS = 5 * 60 * 1000;

export interface AuthContextValue {
  user: AuthUser | null;
  session: KratosSession | null;
  loading: boolean;
  accessToken: string | null;
  /** Unix-seconds timestamp, до которого MFA «свежий». */
  mfaFreshUntil: number;
  /** Bootstrap-info из GET /iam/v1/me (KAC items 1-5): system_admin /
   *  cluster_viewer / per-account roles. null до первого успешного fetch'а
   *  или при 401/403. */
  whoami: WhoAmIResponse | null;

  /** Старт self-service login flow (Kratos browser redirect). */
  login: (returnTo?: string) => void;
  /** Logout: Kratos token-flow + Hydra BCL. */
  logout: () => Promise<void>;
  /** Перезапросить /me + whoami. */
  refresh: () => Promise<void>;
  /** Перезапросить только whoami (например, после 403 — роль могла измениться). */
  refreshWhoAmI: () => Promise<void>;
  /** Установить access-token (после Hydra token-exchange). */
  setAccessToken: (token: string | null) => void;
  /** Установить mfa-fresh timestamp (после успешного step-up). */
  markMfaFresh: (ttlSec?: number) => void;
  /** Проверка permission (admin `*` wildcard). */
  hasPermission: (perm: string) => boolean;
  /** Зарегистрировать step-up handler — обычно StepUpModal. */
  setStepUpHandler: (handler: ((acr?: string) => Promise<void>) | null) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [session, setSession] = useState<KratosSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [accessToken, setAccessTokenState] = useState<string | null>(null);
  const [mfaFreshUntil, setMfaFreshUntil] = useState<number>(0);
  const [whoami, setWhoami] = useState<WhoAmIResponse | null>(null);

  // Refs для apiClient callbacks (mutable без re-render-ов).
  const tokenRef = useRef<string | null>(null);
  const stepUpHandlerRef = useRef<((acr?: string) => Promise<void>) | null>(null);

  tokenRef.current = accessToken;

  const refreshWhoAmI = useCallback(async () => {
    try {
      const w = await authApi.whoami();
      setWhoami(w);
    } catch {
      // 401/403 — нормально для незалогиненных / без cluster доступа.
      setWhoami(null);
    }
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [meResp, whoamiKratosResp, whoamiIamResp] = await Promise.allSettled([
        authApi.me(),
        kratos.whoami(),
        authApi.whoami(),
      ]);
      if (meResp.status === "fulfilled") {
        setUser(meResp.value.user ?? null);
      } else {
        setUser(null);
      }
      if (whoamiKratosResp.status === "fulfilled") {
        setSession(whoamiKratosResp.value);
        // Kratos AAL2 → considered MFA-fresh; user_verification флаг — на бэке.
        if (whoamiKratosResp.value?.authenticator_assurance_level === "aal2") {
          const lastAuth = new Date(whoamiKratosResp.value.authenticated_at).getTime() / 1000;
          setMfaFreshUntil(lastAuth + config.mfaFreshTtlMin * 60);
        }
      } else {
        setSession(null);
      }
      if (whoamiIamResp.status === "fulfilled") {
        setWhoami(whoamiIamResp.value);
      } else {
        setWhoami(null);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // Init: начальный refresh (сессия — по httpOnly cookie Kratos/Hydra).
  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!cancelled) await refresh();
    })();
    return () => {
      cancelled = true;
    };
  }, [refresh]);

  // KAC items 1-5 Foundation: периодически refresh'им whoami каждые 5 минут,
  // чтобы поймать изменение ролей (e.g. админ grant'нул system_admin) без
  // полного `refresh` (который дополнительно дёргает /me и kratos/whoami).
  useEffect(() => {
    if (!user) return;
    const t = setInterval(() => {
      void refreshWhoAmI();
    }, WHOAMI_REFETCH_MS);
    return () => clearInterval(t);
  }, [user, refreshWhoAmI]);

  const login = useCallback((returnTo?: string) => {
    window.location.assign(kratos.loginUrl(returnTo));
  }, []);

  const logout = useCallback(async () => {
    try {
      const { logout_token } = await kratos.initLogout();
      await kratos.submitLogout(logout_token);
    } catch {
      // Session уже истекла — игнорируем.
    }
    setUser(null);
    setSession(null);
    setAccessTokenState(null);
    tokenRef.current = null;
    setMfaFreshUntil(0);
    setWhoami(null);
    try {
      authApi.logout();
    } catch {
      window.location.assign("/");
    }
  }, []);

  const setAccessToken = useCallback((token: string | null) => {
    setAccessTokenState(token);
    tokenRef.current = token;
  }, []);

  const markMfaFresh = useCallback((ttlSec?: number) => {
    const ttl = ttlSec ?? config.mfaFreshTtlMin * 60;
    setMfaFreshUntil(Math.floor(Date.now() / 1000) + ttl);
  }, []);

  const hasPermission = useCallback((perm: string) => checkPerm(user, perm), [user]);

  const setStepUpHandler = useCallback((handler: ((acr?: string) => Promise<void>) | null) => {
    stepUpHandlerRef.current = handler;
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      session,
      loading,
      accessToken,
      mfaFreshUntil,
      whoami,
      login,
      logout,
      refresh,
      refreshWhoAmI,
      setAccessToken,
      markMfaFresh,
      hasPermission,
      setStepUpHandler,
    }),
    [
      user,
      session,
      loading,
      accessToken,
      mfaFreshUntil,
      whoami,
      login,
      logout,
      refresh,
      refreshWhoAmI,
      setAccessToken,
      markMfaFresh,
      hasPermission,
      setStepUpHandler,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

/** Hook для доступа к auth state. Throws вне AuthProvider. */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within <AuthProvider>");
  }
  return ctx;
}

/** True если MFA свежий (для RequireMFAFresh guard). */
export function isMfaFresh(value: { mfaFreshUntil: number }): boolean {
  return value.mfaFreshUntil > Math.floor(Date.now() / 1000);
}
