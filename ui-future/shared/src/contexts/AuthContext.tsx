// AuthContext — централизованный auth state для kacho-ui (KAC-127 Phase 2).
//
// Что внутри:
//   - user (из api-gateway /iam/v1/auth/me)
//   - access-token (in-memory только; никогда не в localStorage)
//   - login() / logout() / refresh() — высокоуровневые actions
//
// Уровня уверенности сессии здесь НЕТ, и это решение (приёмка Ф11 «уровень
// уверенности объявляет наша сессия», §1.3 Ч8). Прежде контекст читал поле
// уровня сессии поставщика и вычислял из него «свежесть подтверждения» —
// значение, которое не читал ни один прод-файл консоли: его писали и не
// читали. По уровню решает край (пол каталога прав, вызов RFC 9470), консоль
// отвечает на вызов церемонией повышения (StepUpModal) и перечитывает личность.
//
// Аутентификация data-plane запросов — ambient httpOnly печенье сессии,
// выписанное поставщиком личности; access-token держится in-memory
// (setAccessToken) для консюмеров, которым он нужен явно.
//
// Backward-compat для KAC-115 (Logout, HeaderAuth, LoginButton, UserMenu) —
// `useAuth` экспозит те же поля `user / loading / login / logout / refresh /
// hasPermission` плюс новые расширения. Старые consumers продолжают работать.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { setStepUpRequester } from "@shared/api/step-up";
import { authApi, hasPermission as checkPerm, type AuthUser, type WhoAmIResponse } from "@shared/api/auth";
import { kratos } from "@shared/lib/kratos";

/** Периодический whoami-refresh — каждые 5 минут (KAC items 1-5 Foundation). */
const WHOAMI_REFETCH_MS = 5 * 60 * 1000;

export interface AuthContextValue {
  user: AuthUser | null;
  loading: boolean;
  accessToken: string | null;
  /** Bootstrap-info из GET /iam/v1/me (KAC items 1-5): system_admin /
   *  cluster_viewer / per-account roles. null до первого успешного fetch'а
   *  или при 401/403. */
  whoami: WhoAmIResponse | null;

  /** Старт self-service login flow (Kratos browser redirect). */
  login: (returnTo?: string) => void;
  /** Выход: token-flow поставщика личности. Обратного канала выхода к службе
   *  выдачи токена здесь нет и не было — её край консоль не зовёт вовсе. */
  logout: () => Promise<void>;
  /** Перезапросить /me + whoami. */
  refresh: () => Promise<void>;
  /** Перезапросить только whoami (например, после 403 — роль могла измениться). */
  refreshWhoAmI: () => Promise<void>;
  /** Установить access-token. Производителя в дереве консоли сегодня нет:
   *  поле держит значение для консюмеров, которым токен нужен явно. */
  setAccessToken: (token: string | null) => void;
  /** Проверка permission (admin `*` wildcard). */
  hasPermission: (perm: string) => boolean;
  /** Зарегистрировать step-up handler — обычно StepUpModal. */
  setStepUpHandler: (handler: ((acr?: string) => Promise<void>) | null) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [loading, setLoading] = useState(true);
  const [accessToken, setAccessTokenState] = useState<string | null>(null);
  const [whoami, setWhoami] = useState<WhoAmIResponse | null>(null);

  // Refs для apiClient callbacks (mutable без re-render-ов).
  const tokenRef = useRef<string | null>(null);

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
      // ДВЕ ручки, обе — СВОЕГО края. Третьей была сессия у чужой службы
      // личности (`/.ory/kratos/public/sessions/whoami`), и её ответ не читал
      // НИКТО: поле `session` контекста не разбирал ни один потребитель во всём
      // дереве консоли. Запрос уходил на каждый подъём страницы и ни на что
      // видимое не влиял — снят вместе с полем (#2733). Наблюдаемое держит
      // `AuthContext.own-edge-only.test.tsx`.
      const [meResp, whoamiIamResp] = await Promise.allSettled([authApi.me(), authApi.whoami()]);
      if (meResp.status === "fulfilled") {
        setUser(meResp.value.user ?? null);
      } else {
        setUser(null);
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

  // Init: начальный refresh (сессия — по httpOnly печенью поставщика личности).
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      if (!cancelled) await refresh();
    })();
    return () => {
      cancelled = true;
    };
  }, [refresh]);

  // KAC items 1-5 Foundation: периодически refresh'им whoami каждые 5 минут,
  // чтобы поймать изменение ролей (e.g. админ grant'нул system_admin) без
  // полного `refresh` (который дополнительно дёргает /me).
  useEffect(() => {
    if (!user) return;
    // поллинг остаётся: предмет здесь не ресурс, а ЛИЧНОСТЬ вызывающего и её
    // права. Довод «журнала у iam нет» отсюда снят — журнал есть, — но вывод он
    // не менял и не меняет: ресурсный журнал несёт состояние РЕСУРСА, а не
    // решение модели прав О ВЫЗЫВАЮЩЕМ. Своей строки у «моих прав» нет ни в
    // одном из семи видов, поэтому события об их смене не существует.
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
    setAccessTokenState(null);
    tokenRef.current = null;
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

  const hasPermission = useCallback((perm: string) => checkPerm(user, perm), [user]);

  // Обработчик ОБЪЯВЛЯЕТСЯ клиенту API — он и есть его читатель (#1213).
  //
  // Прежде обработчик клали в ссылку провайдера, и читателя у неё не было НИ
  // ОДНОГО во всём дереве консоли: окно подтверждения регистрировалось и не
  // открывалось никогда, то есть уровень из консоли поднять было нечем.
  // Ссылка снята вместе с дефектом — держать её рядом с работающим объявлением
  // значило бы завести два места об одном предмете, из которых читают одно.
  const setStepUpHandler = useCallback((handler: ((acr?: string) => Promise<void>) | null) => {
    setStepUpRequester(handler);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      loading,
      accessToken,
      whoami,
      login,
      logout,
      refresh,
      refreshWhoAmI,
      setAccessToken,
      hasPermission,
      setStepUpHandler,
    }),
    [
      user,
      loading,
      accessToken,
      whoami,
      login,
      logout,
      refresh,
      refreshWhoAmI,
      setAccessToken,
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

/**
 * Идентификатор строки членства ВЫЗЫВАЮЩЕГО — либо `undefined`.
 *
 * Отдельно от `useAuth` НАМЕРЕННО, и различие не стилистическое. `useAuth`
 * отказывает вне провайдера, и это верно для страниц, которым личность
 * необходима. Здесь личность — уточнение предупреждения («это вы»), и её
 * отсутствие обязано означать «предупреждения не будет», а не «список не
 * отрисуется»: общий вид строки живёт и в поддеревьях, где провайдера нет
 * (встроенные таблицы, пробы модулей).
 *
 * `user_id` в `/iam/v1/me` и есть каноническая действующая строка вызывающего;
 * у машинного принципала его нет вовсе — тогда «своей строки» не существует, и
 * это правильный `undefined`, а не пропущенный случай.
 */
export function useSelfUserId(): string | undefined {
  return useContext(AuthContext)?.whoami?.user_id;
}
