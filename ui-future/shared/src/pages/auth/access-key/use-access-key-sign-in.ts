// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { assertionBodyOf, assertionRequestOf } from "@shared/api/access-key";
import { FormTokenHolder, LaneRefusal, NOT_BY_SUBSTANCE_TEXT, loginLane } from "@shared/api/login-lane";

// Попытка входа ключом доступа — от нажатия до сессии (приёмка F8-S4).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОРЯДОК ОДНОЙ ПОПЫТКИ
//
//   испытание (`access-key/begin`, признак вида `access-key-begin`) → церемония
//   браузера над ним (`navigator.credentials.get`) → предъявление утверждения
//   (`access-key/login`, признак вида `access-key-login`) → уход на адрес возврата.
//
// Испытание просится ТОЛЬКО нажатием (Р2): выдача испытания — попытка на общей
// оси источника, и экран, просящий его при открытии, тратил бы её на каждый
// показ. Признак формы заранее не добывается тоже: экран без нажатия к полосе
// ключа не обращается вовсе.
//
// Каждая попытка — свежий признак своего вида и свежее испытание (Р3): ни
// испытание, ни утверждение не переживают попытку. Они живут в замыкании одной
// попытки — не в состоянии экрана, не в хранилище браузера, не в адресе.
//
// ИСХОДЫ И ИХ ТЕКСТЫ
//
//   • отказ службы на любом глаголе — ДОСЛОВНО её текст (Р5), срок из
//     `Retry-After` у отказа по частоте закрывает кнопку ключа до истечения
//     (F8S4-13); форма пароля доступна всё время — какую ось исчерпал отказ,
//     консоль не судит (F8 Р2);
//   • ответ не той формы — текст полосы «служба не ответила по существу»;
//     церемония браузера над ним не начинается;
//   • отказ церемонии браузером — ОДИН текст консоли на все причины (Р5):
//     браузер различает их неполно, и консоль не выдаёт догадку за факт. Текст
//     и имя исключения браузера на экран не идут никогда — сообщения браузеров
//     несут адреса и сведения о доверяющей стороне (условие К2). Глагол входа
//     после такого отказа не зовётся: утверждения нет.
//
// Уровень сессии («2» или «3») консоль не выводит из флагов аутентификатора:
// его называет ответ службы (`session.assuranceLevel`).

/**
 * Исход неудачной попытки на экране. Отказ браузера — без подробностей: его
 * текст консоли называет кнопка (`AccessKeySignInButton.tsx`).
 */
export type AccessKeyFailure =
  | { kind: "service"; refusal: LaneRefusal }
  | { kind: "browser" };

/** Есть ли у браузера интерфейс ключей (Р7): нет — кнопки нет. */
export function accessKeysSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof (window as unknown as { PublicKeyCredential?: unknown }).PublicKeyCredential === "function" &&
    typeof navigator !== "undefined" &&
    typeof navigator.credentials?.get === "function"
  );
}

/** Отказ шага службы: отказ полосы — как есть; иное — «не по существу», без эха. */
function serviceFailure(e: unknown): AccessKeyFailure {
  if (e instanceof LaneRefusal) return { kind: "service", refusal: e };
  return { kind: "service", refusal: new LaneRefusal(0, null, NOT_BY_SUBSTANCE_TEXT, null, null, null) };
}

export interface AccessKeySignIn {
  /** Есть ли кнопка вовсе (Р7). */
  supported: boolean;
  /** Попытка идёт: от нажатия до исхода. */
  busy: boolean;
  /** Срок из `Retry-After`, секунды; `null` — кнопка не закрыта сроком. */
  lockedFor: number | null;
  failure: AccessKeyFailure | null;
  /** Начать попытку; без исхода (занята, закрыта, недоступна) — ничего. */
  start: () => void;
  /** Снять показ прежнего исхода (вход паролем начат). */
  clear: () => void;
}

/**
 * Попытка входа ключом. `onSignedIn` — уход экрана после выдачи сессии;
 * `blocked` — экран занят входом паролем: две выдачи сессии подряд человеку не
 * нужны, и кнопка ключа на это время закрыта.
 */
export function useAccessKeySignIn({
  onSignedIn,
  onStart,
  blocked,
}: {
  onSignedIn: () => void;
  /** Попытка начата: экран снимает показ исхода другого способа входа. */
  onStart?: () => void;
  blocked: boolean;
}): AccessKeySignIn {
  const [supported] = useState(accessKeysSupported);
  const begin = useMemo(() => new FormTokenHolder("access-key-begin"), []);
  const login = useMemo(() => new FormTokenHolder("access-key-login"), []);
  const [busy, setBusy] = useState(false);
  const [lockedFor, setLockedFor] = useState<number | null>(null);
  const [failure, setFailure] = useState<AccessKeyFailure | null>(null);
  // Занятость — и ссылкой: два нажатия в одном такте видят одно состояние
  // экрана, и второе начинало бы вторую попытку — второе испытание на общей оси.
  const inFlight = useRef(false);
  const alive = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    alive.current = controller;
    return () => {
      // Уход с экрана посреди церемонии: браузер снимает её, исход не пишется.
      controller.abort();
      alive.current = null;
    };
  }, []);

  useEffect(() => {
    if (lockedFor === null) return;
    const t = window.setTimeout(() => setLockedFor(null), lockedFor * 1000);
    return () => window.clearTimeout(t);
  }, [lockedFor]);

  const attempt = useCallback(async () => {
    const controller = alive.current;
    if (!controller) return;
    const settle = (f: AccessKeyFailure) => {
      if (controller.signal.aborted) return;
      setFailure(f);
      if (f.kind === "service" && f.refusal.code === 8 && f.refusal.retryAfterSeconds !== null) {
        setLockedFor(f.refusal.retryAfterSeconds);
      }
    };

    let publicKey: PublicKeyCredentialRequestOptions;
    try {
      publicKey = assertionRequestOf(await loginLane.accessKeyBegin(begin));
    } catch (e) {
      settle(serviceFailure(e));
      return;
    }

    let credential: Credential | null;
    try {
      credential = await navigator.credentials.get({ publicKey, signal: controller.signal });
    } catch {
      settle({ kind: "browser" });
      return;
    }
    if (controller.signal.aborted) return;

    let body: ReturnType<typeof assertionBodyOf>;
    try {
      body = assertionBodyOf(credential);
    } catch {
      // Браузер ответил не удостоверением ключа — утверждения нет, глагол не зовётся.
      settle({ kind: "browser" });
      return;
    }

    try {
      await loginLane.accessKeyLogin(login, body);
    } catch (e) {
      settle(serviceFailure(e));
      return;
    }
    if (!controller.signal.aborted) onSignedIn();
    // Экран уходит — попытка остаётся занятой, повторного нажатия нет.
    return "signed-in" as const;
  }, [begin, login, onSignedIn]);

  const start = useCallback(() => {
    if (!supported || inFlight.current || blocked || lockedFor !== null) return;
    inFlight.current = true;
    setBusy(true);
    setFailure(null);
    onStart?.();
    const release = () => {
      inFlight.current = false;
      if (alive.current) setBusy(false);
    };
    // Обработан весь путь: отклонения мимо обработчика нет (условие К4).
    void attempt().then((outcome) => {
      if (outcome !== "signed-in") release();
    }, release);
  }, [supported, blocked, lockedFor, attempt, onStart]);

  const clear = useCallback(() => setFailure(null), []);

  return { supported, busy, lockedFor, failure, start, clear };
}
