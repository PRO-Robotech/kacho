import { useEffect, useState } from "react";
import { sessionIdentity, type SessionAnswer } from "@shared/api/login-lane";

/**
 * Кто за сессией браузера — для каркаса консоли (рейл: «Войти» либо
 * «Учётная запись»).
 *
 * `undefined` — край ещё не ответил, и каркас не обещает ни того, ни другого:
 * кнопка «Войти», мигнувшая у вошедшего человека, выглядела бы как потеря сессии.
 * Исход «спросить не удалось» (`unknown`) каркас читает так же, а не как «сессии
 * нет» (условие C6): предложить войти человеку, чей край на миг не ответил,
 * значило бы сказать ему, что он вышел.
 */
export function useSessionIdentity(): SessionAnswer | undefined {
  const [who, setWho] = useState<SessionAnswer | undefined>(undefined);
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
