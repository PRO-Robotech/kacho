import { useEffect, useState } from "react";
import { sessionIdentity, type SessionIdentity } from "@shared/api/login-lane";

/**
 * Кто за сессией браузера — для каркаса консоли (рейл: «Войти» либо
 * «Учётная запись»).
 *
 * `undefined` — край ещё не ответил, и каркас не обещает ни того, ни другого:
 * кнопка «Войти», мигнувшая у вошедшего человека, выглядела бы как потеря сессии.
 */
export function useSessionIdentity(): SessionIdentity | null | undefined {
  const [who, setWho] = useState<SessionIdentity | null | undefined>(undefined);
  useEffect(() => {
    let cancelled = false;
    void sessionIdentity().then((w) => {
      if (!cancelled) setWho(w);
    });
    return () => {
      cancelled = true;
    };
  }, []);
  return who;
}
