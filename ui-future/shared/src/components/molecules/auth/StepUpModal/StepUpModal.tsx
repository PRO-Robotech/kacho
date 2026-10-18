// Повторное подтверждение личности — ОДНА реализация на продукт.
//
// Дом здесь потому, что подтверждение личности принадлежит не модулю, а
// продукту: его просит любое действие, меняющее посадку безопасности, и просить
// его двумя разными окнами значит показывать человеку два разных продукта в
// момент, когда он и так насторожён.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ОКНО ДЕЛАЕТ (#1213, приёмка F8 S2; приёмка F8-S7, Р5)
//
// Окно — единственное место консоли, где человек поднимает уровень
// уверенности или свежесть предъявления. Ведёт оно это НАШИМ глаголом —
// `POST /iam/v1/auth/step-up` с признаком формы вида `step-up`, — а не уводит на
// чужое приложение: церемония проходит, не покидая консоли, и отвергнутое
// действие после неё повторяется (его повторяет клиент API, `requestStepUp`).
//
// ЛЮБЫМ ЗАВЕДЁННЫМ СПОСОБОМ (F8-S7, Р5). Перечень способов окно берёт у службы
// — `stepUpMethods` чтения состояния второго фактора (SUA Р6), — а не выводит
// само (F8 Р2). Порядок показа — консоли: ключ доступа → код из приложения →
// запасной код. По умолчанию выбран первый доступный из ключа и кода; запасной
// код по умолчанию не выбирается никогда — он расходуется. Остальные способы —
// группой «Другой способ».
//
// Ветвь ключа: `POST …/step-up/access-key/begin` → церемония браузера над
// выданным испытанием → `POST /iam/v1/auth/step-up` `{method: "webauthn",
// credential}`. Испытание живёт в замыкании одной попытки; новая попытка —
// новое испытание. Отказ церемонии браузером — один текст консоли на все
// причины, и глагол повышения тогда не зовётся: утверждения нет.
//
// ЧТЕНИЕ БЕЗ ПЕРЕЧНЯ. Служба, ещё не отдающая `stepUpMethods` (ответ прежней
// формы Ф12 Р4), и чтение, не давшее ответа, перечня не называют. Окно тогда
// предлагает то, что церемония принимала всегда, — оба способа кода (Ф12 Р4), а
// заведён ли фактор, говорит отказ службы `SECOND_FACTOR_NOT_ENROLLED` (F8-36).
// Пустой перечень и отсутствующий — РАЗНЫЕ ответы: пустой служба назвала, и
// окно говорит «Нет способа подтверждения».
//
// «ВОЙТИ ЗАНОВО» (F8 Р12 в редакции F8-S7, §5). Путь остаётся ТОЛЬКО у окна,
// открытого отказом СВЕЖЕСТИ, когда способов нет: вход — это предъявление, и он
// даёт свежую сессию. У человека с ключом теперь есть прямой путь — ключом.
// Окно, открытое вызовом края на УРОВЕНЬ, этого пути не несёт: уровень вход
// заново не поднимает.
//
// Обещание запроса на отказе не разрешается: fail-closed — разрешить его значило
// бы пропустить действие, за которое никто не поручился.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Modal, Spin, Typography } from "antd";
import { assertionBodyOf, assertionRequestOf } from "@shared/api/access-key";
import {
  CHALLENGE_REASON_PREFIX,
  FormTokenHolder,
  LANE_REASON,
  LaneRefusal,
  NOT_BY_SUBSTANCE_TEXT,
  loginLane,
  type Ceremony,
} from "@shared/api/login-lane";
import { setStepUpRequester, type StepUpRequest } from "@shared/api/step-up";
import { SecondFactorCodeField } from "@shared/components/molecules/auth/SecondFactorCodeField";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useOptionalAuth } from "@shared/contexts/AuthContext";
import { CODE_METHODS, type SecondFactorMethod, type StepUpFactor } from "@shared/lib/step-up-methods";
import { ACCOUNT_SETTINGS_ADDRESS, loginAddress } from "@shared/pages/auth/ceremony-addresses";
import { useLogout } from "@shared/pages/auth/use-logout";

const { Paragraph } = Typography;

/** Экран, где заводят второй фактор, — раздел «Второй фактор» экрана «Безопасность». */
export const SECOND_FACTOR_ENROLLMENT_ADDRESS = `${ACCOUNT_SETTINGS_ADDRESS}#security-second-factor`;

/** Тексты окна на исходы службы (F8-S7, Р5) — дословно. */
export const STEP_UP_TEXT = {
  failed: "Подтверждение не прошло. Повторите или выберите другой способ.",
  keyExpired: "Время подтверждения ключом истекло — повторите",
  keyBrowserRefused: "Подтверждение ключом прервано в браузере — повторите или выберите другой способ",
  noMethod: "Нет способа подтверждения",
  backupLow: "Запасные коды заканчиваются — выпустите новые",
} as const;

/** «Слишком много попыток. Повторите через N мин.» — N из `Retry-After`, вверх до минуты. */
export function tooManyAttemptsText(retryAfterSeconds: number | null): string {
  if (retryAfterSeconds === null) return "Слишком много попыток.";
  return `Слишком много попыток. Повторите через ${Math.max(1, Math.ceil(retryAfterSeconds / 60))} мин.`;
}

/** Порядок показа способов — консоли (Р5). */
const SHOW_ORDER: readonly StepUpFactor[] = ["webauthn", "totp", "lookup_secret"];

/** Способ по умолчанию — первый доступный из ключа и кода; запасной код — никогда. */
export function defaultStepUpFactor(methods: readonly StepUpFactor[]): StepUpFactor | null {
  if (methods.includes("webauthn")) return "webauthn";
  if (methods.includes("totp")) return "totp";
  return null;
}

/** Есть ли у браузера интерфейс предъявления ключа. */
function keyCeremonySupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof (window as unknown as { PublicKeyCredential?: unknown }).PublicKeyCredential === "function" &&
    typeof navigator !== "undefined" &&
    typeof navigator.credentials?.get === "function"
  );
}

/** Перечень способов: названный службой, либо прежний перечень церемонии (шапка). */
async function readMethods(): Promise<StepUpFactor[]> {
  try {
    const state = await loginLane.secondFactorState();
    return state.stepUpMethods ?? [...CODE_METHODS];
  } catch {
    return [...CODE_METHODS];
  }
}

interface PendingRequest {
  request: StepUpRequest;
  resolve: () => void;
  reject: (e: Error) => void;
}

/** Что окно говорит под выбором: текст консоли на исход либо отказ службы дословно. */
type Notice = { kind: "text"; text: string } | { kind: "refusal"; refusal: LaneRefusal };

/** Адрес, на который человек вернётся после входа заново, — текущий. */
function currentAddress(): string {
  return `${window.location.pathname}${window.location.search}`;
}

/**
 * «Войти заново» — выход ТЕМ ЖЕ действием, что у консоли один (`useLogout`,
 * условие C13), и вход с возвратом сюда. Монтируется только в окне свежести без
 * способов: признак формы выхода добывается, когда путь предложен.
 */
function ReauthenticateAction({ leave, disabled }: { leave: (to: string) => void; disabled: boolean }) {
  const { logout, busy, refusal } = useLogout(() => leave(loginAddress(currentAddress())));
  return (
    <div style={{ marginTop: 12 }}>
      <Paragraph style={{ marginBottom: 8 }}>Либо войдите заново любым способом входа.</Paragraph>
      {refusal && (
        <div style={{ marginBottom: 8 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      <Button loading={busy} disabled={disabled} onClick={() => void logout()}>
        Войти заново
      </Button>
    </div>
  );
}

export function StepUpModal({ leave = (to) => window.location.replace(to) }: { leave?: (to: string) => void } = {}) {
  const auth = useOptionalAuth();
  // Признаки добываются при отправке, а не при монтировании: окно смонтировано
  // на каждой странице модуля, а открывается редко.
  const holder = useMemo(() => new FormTokenHolder("step-up"), []);
  const beginHolder = useMemo(() => new FormTokenHolder("step-up-access-key-begin"), []);
  const [keySupported] = useState(keyCeremonySupported);
  const [pending, setPending] = useState<PendingRequest | null>(null);
  const [methods, setMethods] = useState<StepUpFactor[] | null>(null);
  const [chosen, setChosen] = useState<StepUpFactor | null>(null);
  const [code, setCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [codeError, setCodeError] = useState<string | null>(null);
  const [lockedFor, setLockedFor] = useState<number | null>(null);
  const [announce, setAnnounce] = useState<{ text: string; low: boolean } | null>(null);
  // Номер открытия: ответ чтения перечня ложится только в своё открытие.
  const opened = useRef(0);
  const ceremony = useRef<AbortController | null>(null);

  const adopt = useCallback(
    (next: StepUpFactor[], keepChoice: boolean) => {
      const offered = keySupported ? next : next.filter((m) => m !== "webauthn");
      setMethods(offered);
      setChosen((was) => (keepChoice && was !== null && offered.includes(was) ? was : defaultStepUpFactor(offered)));
    },
    [keySupported],
  );

  // Обработчик ОБЪЯВЛЯЕТСЯ клиенту API — он и есть его читатель (#1213).
  useEffect(() => {
    const handler = (request: StepUpRequest) =>
      new Promise<void>((resolve, reject) => {
        const n = ++opened.current;
        setMethods(null);
        setChosen(null);
        setCode("");
        setNotice(null);
        setCodeError(null);
        setAnnounce(null);
        setPending({ request, resolve, reject });
        void readMethods().then((m) => {
          if (opened.current === n) adopt(m, false);
        });
      });
    setStepUpRequester(handler);
    return () => setStepUpRequester(null);
  }, [adopt]);

  useEffect(() => {
    if (lockedFor === null) return;
    const t = window.setTimeout(() => setLockedFor(null), lockedFor * 1000);
    return () => window.clearTimeout(t);
  }, [lockedFor]);

  useEffect(
    () => () => {
      ceremony.current?.abort();
    },
    [],
  );

  const close = () => {
    ceremony.current?.abort();
    ceremony.current = null;
    opened.current++;
    setPending(null);
    setCode("");
    setNotice(null);
    setCodeError(null);
  };

  const cancel = () => {
    pending?.reject(new Error("повышение отменено человеком"));
    close();
  };

  /** Отказ шага — словом консоли на известный исход, иначе текст службы дословно. */
  const refused = (e: unknown) => {
    const r = e instanceof LaneRefusal ? e : new LaneRefusal(0, null, NOT_BY_SUBSTANCE_TEXT, null, null, null);
    if (r.reason === LANE_REASON.tooManyAttempts || r.code === 8) {
      if (r.retryAfterSeconds !== null) setLockedFor(r.retryAfterSeconds);
      setNotice({ kind: "text", text: tooManyAttemptsText(r.retryAfterSeconds) });
      return;
    }
    if (r.reason?.startsWith(CHALLENGE_REASON_PREFIX)) {
      setNotice({ kind: "text", text: STEP_UP_TEXT.keyExpired });
      return;
    }
    if (
      r.reason === LANE_REASON.secondFactorNotEnrolled ||
      r.reason === LANE_REASON.accessKeyNotRegistered ||
      r.reason === LANE_REASON.accessKeysDisabled
    ) {
      // Способа больше нет — перечень перечитывается, способ уходит из выбора.
      setNotice({ kind: "refusal", refusal: r });
      const n = opened.current;
      void readMethods().then((m) => {
        if (opened.current === n) adopt(m, true);
      });
      return;
    }
    if (r.status === 401 && r.code === 16 && r.challenge === null) {
      setNotice({ kind: "text", text: STEP_UP_TEXT.failed });
      return;
    }
    if (r.field === "code") {
      setCodeError(r.message);
      return;
    }
    setNotice({ kind: "refusal", refusal: r });
  };

  const granted = async (answer: Ceremony, by: StepUpFactor) => {
    // Уровень после церемонии знает край по нашей сессии; консоли достаточно
    // перечитать личность и отпустить отвергнутый запрос на повтор.
    await auth?.refresh();
    if (by === "lookup_secret" && typeof answer.backupCodesRemaining === "number") {
      setAnnounce({
        text: `Осталось запасных кодов: ${answer.backupCodesRemaining}`,
        low: answer.backupCodesRemaining <= 2,
      });
    }
    pending?.resolve();
    close();
  };

  const confirmCode = async () => {
    if (!pending || submitting || lockedFor !== null || chosen === null || chosen === "webauthn") return;
    const method: SecondFactorMethod = chosen;
    setSubmitting(true);
    setNotice(null);
    setCodeError(null);
    try {
      await granted(await loginLane.stepUp(holder, { method, code }), method);
    } catch (err) {
      refused(err);
    } finally {
      setSubmitting(false);
    }
  };

  const confirmKey = async () => {
    if (!pending || submitting || lockedFor !== null) return;
    setChosen("webauthn");
    setSubmitting(true);
    setNotice(null);
    setCodeError(null);
    const controller = new AbortController();
    ceremony.current = controller;
    try {
      let publicKey: PublicKeyCredentialRequestOptions;
      try {
        publicKey = assertionRequestOf(await loginLane.stepUpAccessKeyBegin(beginHolder));
      } catch (e) {
        refused(e);
        return;
      }
      let answer: Credential | null;
      try {
        answer = await navigator.credentials.get({ publicKey, signal: controller.signal });
      } catch {
        if (!controller.signal.aborted) setNotice({ kind: "text", text: STEP_UP_TEXT.keyBrowserRefused });
        return;
      }
      if (controller.signal.aborted) return;
      let credential: Record<string, unknown>;
      try {
        credential = assertionBodyOf(answer);
      } catch {
        setNotice({ kind: "text", text: STEP_UP_TEXT.keyBrowserRefused });
        return;
      }
      try {
        await granted(await loginLane.stepUp(holder, { method: "webauthn", credential }), "webauthn");
      } catch (e) {
        refused(e);
      }
    } finally {
      if (ceremony.current === controller) ceremony.current = null;
      setSubmitting(false);
    }
  };

  const freshness = pending?.request.cause === "freshness";
  const locked = lockedFor !== null;
  const codeMethods = (methods ?? []).filter((m): m is SecondFactorMethod => m !== "webauthn");
  const ordered = SHOW_ORDER.filter((m): m is SecondFactorMethod => m !== "webauthn" && codeMethods.includes(m));
  const keyOffered = methods?.includes("webauthn") ?? false;
  const none = methods !== null && methods.length === 0;
  const notEnrolled = notice?.kind === "refusal" && notice.refusal.reason === LANE_REASON.secondFactorNotEnrolled;
  const codeChosen = chosen !== null && chosen !== "webauthn";
  // Значок ожидания, оставшийся после быстрого отказа (F8-S7-33, kacho#3285),
  // снимается правилом листа каркаса на всех кнопках консоли
  // (`host/src/styles.css`), а не перемонтированием отдельных кнопок.

  return (
    <>
      {announce && (
        <div role="status" style={{ marginBottom: 12 }}>
          <div>{announce.text}</div>
          {announce.low && <div>{STEP_UP_TEXT.backupLow}</div>}
        </div>
      )}
      <Modal
        open={pending !== null}
        title="Подтверждение действия"
        onCancel={cancel}
        mask={{ closable: false }}
        footer={[
          <Button key="cancel" onClick={cancel}>
            Отмена
          </Button>,
          ...(codeChosen && !none
            ? [
                <Button
                  key="ok"
                  type="primary"
                  loading={submitting}
                  disabled={locked}
                  onClick={() => void confirmCode()}
                >
                  Подтвердить
                </Button>,
              ]
            : []),
        ]}
      >
        <FormGrid label="Подтверждение действия" onSubmit={() => void confirmCode()}>
          <Paragraph>
            {freshness
              ? "Это действие требует подтвердить личность ещё раз."
              : "Это действие подтверждается вторым фактором."}
          </Paragraph>
          {methods === null && <Spin />}
          {none && (
            <>
              <Paragraph>{STEP_UP_TEXT.noMethod}</Paragraph>
              <Paragraph>
                <a
                  href={SECOND_FACTOR_ENROLLMENT_ADDRESS}
                  onClick={() => pending?.reject(new Error("способа подтверждения нет"))}
                >
                  Настроить второй фактор
                </a>
              </Paragraph>
            </>
          )}
          {keyOffered && (
            <div style={{ marginBottom: 12 }}>
              <Button
                type={chosen === "webauthn" ? "primary" : "default"}
                loading={submitting && chosen === "webauthn"}
                disabled={locked || (submitting && chosen !== "webauthn")}
                onClick={() => void confirmKey()}
              >
                Подтвердить ключом доступа
              </Button>
            </div>
          )}
          {ordered.length > 0 && (
            <SecondFactorCodeField
              variant="confirm"
              groupLabel="Другой способ"
              methods={ordered}
              method={codeChosen ? chosen : null}
              onMethodChange={(m) => {
                setChosen(m);
                setCode("");
                setCodeError(null);
              }}
              code={code}
              onCodeChange={setCode}
              codeError={codeError}
              disabled={locked || submitting}
            />
          )}
          {notice?.kind === "text" && (
            <div role="alert" style={{ marginBottom: 12 }}>
              {notice.text}
            </div>
          )}
          {notice?.kind === "refusal" && (
            <div style={{ marginBottom: 12 }}>
              <LaneRefusalAlert refusal={notice.refusal} />
              {notEnrolled && !none && (
                <Paragraph style={{ marginTop: 8 }}>
                  <a
                    href={SECOND_FACTOR_ENROLLMENT_ADDRESS}
                    onClick={() => pending?.reject(new Error("второй фактор не настроен"))}
                  >
                    Настроить второй фактор
                  </a>
                </Paragraph>
              )}
            </div>
          )}
          {/* Отправка клавишей ввода из поля — та же, что кнопкой. */}
          <button type="submit" hidden aria-hidden tabIndex={-1} />
        </FormGrid>
        {freshness && none && <ReauthenticateAction leave={leave} disabled={submitting} />}
      </Modal>
    </>
  );
}
