// AuthContext — централизованный auth state для kacho-ui (KAC-127 Phase 2).
//
// Что внутри:
//   - user / session — из ответа края о сессии (`GET /iam/v1/auth/me`): кто за
//     браузерной сессией и её срок, уровень и подтверждённость адреса;
//   - whoami — bootstrap прав из `GET /iam/v1/me`;
//   - login() / logout() / refresh() — высокоуровневые действия.
//
// Церемонии входа и выхода ведёт КОНСОЛЬ своими экранами и глаголами нашей
// службы (приёмка F8): `login()` уводит на экран входа консоли, `logout()` зовёт
// глагол выхода. Чужой поставщик личности отсюда не зовётся ни одним путём — ни
// переходом, ни запросом к его потоку, ни чтением его сессии.
//
// Уровня уверенности как РЕШЕНИЯ здесь нет (приёмка Ф11 §1.3 Ч8): по уровню
// решает край (пол каталога прав, вызов RFC 9470), консоль отвечает на вызов
// церемонией повышения (StepUpModal) и перечитывает личность. Поле
// `session.assuranceLevel` — факт ответа края, а не суждение консоли.
//
// Аутентификация data-plane запросов — ambient httpOnly носитель сессии, его
// держит браузер; консоль носитель не читает и не пишет.

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { setStepUpRequester } from "@shared/api/step-up";
import { authApi, hasPermission as checkPerm, type AuthUser, type WhoAmIResponse } from "@shared/api/auth";
import { FormTokenHolder, loginLane, type LaneSession } from "@shared/api/login-lane";
import { loginAddress } from "@shared/pages/auth/ceremony-addresses";

/** Периодический whoami-refresh — каждые 5 минут (KAC items 1-5 Foundation). */
const WHOAMI_REFETCH_MS = 5 * 60 * 1000;

export interface AuthContextValue {
  user: AuthUser | null;
  /** Сессия по ответу края; `null` — сессии нет. */
  session: LaneSession | null;
  loading: boolean;
  accessToken: string | null;
  /** Bootstrap-info из GET /iam/v1/me (KAC items 1-5): system_admin /
   *  cluster_viewer / per-account roles. null до первого успешного fetch'а
   *  или при 401/403. */
  whoami: WhoAmIResponse | null;

  /** Увести на экран входа консоли с адресом возврата. */
  login: (returnTo?: string) => void;
  /**
   * Выход глаголом службы. Отказ ПРОБРАСЫВАЕТСЯ: экран не вправе показать
   * «вышли», пока служба выхода не подтвердила (приёмка F8, F8-19).
   */
  logout: () => Promise<void>;
  /** Перезапросить /me + whoami. */
  refresh: () => Promise<void>;
  /** Перезапросить только whoami (например, после 403 — роль могла измениться). */
  refreshWhoAmI: () => Promise<void>;
  /** Установить access-token (после Hydra token-exchange). */
  setAccessToken: (token: string | null) => void;
  /** Проверка permission (admin `*` wildcard). */
  hasPermission: (perm: string) => boolean;
  /** Зарегистрировать step-up handler — обычно StepUpModal. */
  setStepUpHandler: (handler: ((acr?: string) => Promise<void>) | null) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [session, setSession] = useState<LaneSession | null>(null);
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
      const [meResp, whoamiIamResp] = await Promise.allSettled([authApi.me(), authApi.whoami()]);
      if (meResp.status === "fulfilled") {
        setUser(meResp.value.user ?? null);
        setSession(meResp.value.session ?? null);
      } else {
        setUser(null);
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

  // Init: начальный refresh (сессия — по httpOnly носителю, его держит браузер).
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
  // полного `refresh` (который дополнительно перечитывает личность и сессию).
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
    window.location.assign(loginAddress(returnTo));
  }, []);

  const logoutHolder = useMemo(() => new FormTokenHolder("logout"), []);
  const logout = useCallback(async () => {
    await loginLane.logout(logoutHolder);
    setUser(null);
    setSession(null);
    setAccessTokenState(null);
    tokenRef.current = null;
    setWhoami(null);
    window.location.replace(loginAddress());
  }, [logoutHolder]);

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
      session,
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
      session,
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

/**
 * Контекст личности, если провайдер смонтирован, иначе `null`.
 *
 * Для тех, кому личность — уточнение, а не условие: окно повышения живёт и на
 * странице каркаса, где провайдера нет, и перечитывать личность там некому.
 */
export function useOptionalAuth(): AuthContextValue | null {
  return useContext(AuthContext);
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
