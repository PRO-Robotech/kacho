// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useState } from "react";
import { Link } from "react-router";
import { Alert, Button, Form, Input, Spin, Typography } from "antd";
import { type LaneRefusal, laneRefusalOf, loginLane, sessionIdentity, type RefusalInput } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { CeremonyScreen } from "./CeremonyScreen";
import { loginAddress } from "./ceremony-addresses";
import { useReturnTo } from "./use-return-to";

// Экран восстановления доступа — `/recovery`, вне каркаса, за стражем
// подтверждённости (приёмка F8-S3; глаголы — приёмка Ф5 службы).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭКРАН ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ
//
//   • сначала спрашивает, есть ли сессия, — тем же решением, что экран входа:
//     у человека с живой сессией формы нет, документ уходит на адрес возврата
//     (F8S3-02); узнать не удалось — форма есть, и рядом это названо;
//   • ступень 1 — адрес почты и «Отправить код». Ответ службы — `200 {}` на ЛЮБОЙ
//     исход (адрес заведён или нет, подтверждён или нет, окно писем полно или
//     нет), и экран показывает ОДИН текст, зависящий только от того, что ответ
//     получен (Р2, F8S3-05): различимый текст отвечал бы на вопрос «заведён ли
//     адрес» тому, кто пароля не знает;
//   • ступень 2 — введённый адрес только для чтения, код из письма и новый
//     пароль. Отправляются РОВНО объявленные поля, код и пароль — как введены:
//     своего правила у экрана нет, отказ о поле называет служба (Р3);
//   • отказ показывает ДОСЛОВНО и не обогащает (Р6): отказ кода — один текст на
//     все причины, включая блокировку; рядом — следующий шаг, один на код
//     (`LaneRefusalAlert`);
//   • на отказе по частоте отсчитывает срок из `Retry-After` и до его истечения
//     отправку не производит — ни кнопкой, ни клавишей ввода. Нет заголовка —
//     нет и отсчёта: своего числа у консоли нет (Р2, F8S3-11);
//   • успех уводит ДОКУМЕНТОМ на адрес возврата: новая личность обязана дойти до
//     каждого модуля; адрес возврата читает ТОЛЬКО `useReturnTo` (Р5, F8S3-15);
//   • способов входа человека не различает: с паролем, без, со вторым фактором —
//     тело и экран те же, исход — службы (Р6).
//
// Адреса почты в адресе страницы нет нигде: ни ссылка с экрана входа, ни ссылка
// отсюда его не несут (Р4).

/** Уйти ДОКУМЕНТОМ: новая личность обязана дойти до каждого модуля. */
function leaveDocument(to: string) {
  window.location.replace(to);
}

const TITLE = "Восстановление доступа";
const CODE_FORM = "Смена пароля по коду";

/** Текст ступени 2 — один на любой ответ запроса кода (приёмка F8-S3, Р2, Т-код). */
export const RECOVERY_CODE_SENT_TEXT =
  "Если адрес заведён и подтверждён, на него отправлено письмо с кодом восстановления. Код действует " +
  "ограниченное время и применяется один раз. Введите код из последнего письма и новый пароль.";

/** Вводы экрана, которые служба может назвать отказом (таблица полей — в клиенте полосы). */
type RecoveryField = Extract<RefusalInput, "email" | "code" | "newPassword">;

function fieldOf(refusal: LaneRefusal | null): RecoveryField | null {
  const f = refusal?.field;
  return f === "email" || f === "code" || f === "newPassword" ? f : null;
}

/** Действие экрана, чей отказ показан: отказ одного действия не отмечает поля другого. */
type Action = "request" | "complete";

/** Срок до повтора действия — число службы, отсчитываемое экраном; `null` — срока нет. */
interface Waiting {
  action: Action;
  seconds: number;
}

export function RecoveryPage({ leave = leaveDocument }: { leave?: (to: string) => void }) {
  const id = useId();
  const returnTo = useReturnTo();
  const requestHolder = useFormToken("recovery");
  const completeHolder = useFormToken("recovery-complete");
  const [phase, setPhase] = useState<"проверка сессии" | "форма">("проверка сессии");
  const [sessionUnknown, setSessionUnknown] = useState<LaneRefusal | null>(null);
  const [stage, setStage] = useState<"адрес" | "код">("адрес");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [busy, setBusy] = useState<Action | null>(null);
  const [refused, setRefused] = useState<{ action: Action; refusal: LaneRefusal } | null>(null);
  const [waiting, setWaiting] = useState<Waiting | null>(null);

  const askSession = useCallback(
    (isCancelled: () => boolean = () => false) =>
      sessionIdentity().then((who) => {
        if (isCancelled()) return;
        if (who.kind === "present") {
          leave(returnTo);
          return;
        }
        setSessionUnknown(who.kind === "unknown" ? who.refusal : null);
        setPhase("форма");
      }),
    [leave, returnTo],
  );

  useEffect(() => {
    let cancelled = false;
    void askSession(() => cancelled);
    return () => {
      cancelled = true;
    };
  }, [askSession]);

  // Отсчёт снимает закрытие, а не повторяет отправку: повторит её человек.
  useEffect(() => {
    if (waiting === null) return;
    const t = window.setTimeout(
      () => setWaiting(waiting.seconds > 1 ? { action: waiting.action, seconds: waiting.seconds - 1 } : null),
      1000,
    );
    return () => window.clearTimeout(t);
  }, [waiting]);

  /** Отказ действия: показать и, если служба назвала срок, закрыть действие до него. */
  const onRefused = (action: Action, err: unknown) => {
    const refusal = laneRefusalOf(err);
    setRefused({ action, refusal });
    if (refusal.code === 8 && refusal.retryAfterSeconds !== null) {
      setWaiting({ action, seconds: refusal.retryAfterSeconds });
    }
  };

  const locked = (action: Action) => waiting?.action === action;

  const onRequest = async () => {
    // Закрытие держит ОБРАБОТЧИК, а не только кнопка: клавиша ввода идёт мимо кнопки.
    if (busy !== null || locked("request")) return;
    setBusy("request");
    setRefused(null);
    try {
      await loginLane.requestRecovery(requestHolder, { email });
      setStage("код");
    } catch (err) {
      onRefused("request", err);
    }
    setBusy(null);
  };

  const onComplete = async () => {
    if (busy !== null || locked("complete")) return;
    setBusy("complete");
    setRefused(null);
    try {
      await loginLane.completeRecovery(completeHolder, { email, code, newPassword });
      leave(returnTo);
      return; // экран уходит — кнопка остаётся занятой, повторной отправки нет
    } catch (err) {
      onRefused("complete", err);
    }
    setBusy(null);
  };

  if (phase === "проверка сессии") {
    return (
      <CeremonyScreen title={TITLE}>
        <Spin />
      </CeremonyScreen>
    );
  }

  const refusal = refused?.refusal ?? null;
  const marked = fieldOf(refusal);
  const inputId = (f: RecoveryField) => `${id}-${f}`;
  const fieldError = (f: RecoveryField) => (marked === f ? refusal!.message : undefined);
  const described = (f: RecoveryField) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId(f)), status: "error" as const }
      : {};
  const refusalAlert = (action: Action) =>
    refused !== null &&
    refused.action === action &&
    (marked === null || (stage === "адрес" && marked !== "email")) && (
      <div style={{ marginBottom: 16 }}>
        <LaneRefusalAlert
          refusal={refused.refusal}
          context="recovery"
          secondsLeft={waiting?.action === action ? waiting.seconds : null}
        />
      </div>
    );

  const emailItem = (
    <Form.Item label="Адрес почты" htmlFor={inputId("email")}>
      <Input
        id={inputId("email")}
        type="email"
        autoComplete="username"
        value={email}
        readOnly={stage === "код"}
        onChange={(e) => setEmail(e.target.value)}
        {...described("email")}
      />
      <FieldError id={fieldErrorId(inputId("email"))} message={fieldError("email")} />
    </Form.Item>
  );

  return (
    <CeremonyScreen title={TITLE} footer={<Link to={loginAddress(returnTo)}>Вернуться ко входу</Link>}>
      {sessionUnknown && (
        <div style={{ marginBottom: 16 }}>
          <Alert
            type="warning"
            showIcon
            message={sessionUnknown.message}
            action={
              <Button size="small" onClick={() => void askSession()}>
                Проверить снова
              </Button>
            }
          />
        </div>
      )}
      {stage === "адрес" ? (
        <FormGrid label={TITLE} onSubmit={() => void onRequest()}>
          <Typography.Paragraph>
            Введите адрес почты учётной записи: на него придёт письмо с кодом восстановления.
          </Typography.Paragraph>
          {emailItem}
          {refusalAlert("request")}
          <Button type="primary" htmlType="submit" block loading={busy === "request"} disabled={locked("request")}>
            Отправить код
          </Button>
        </FormGrid>
      ) : (
        <>
          <Typography.Paragraph>{RECOVERY_CODE_SENT_TEXT}</Typography.Paragraph>
          <FormGrid label={CODE_FORM} onSubmit={() => void onComplete()}>
            {emailItem}
            <Form.Item label="Код из письма" htmlFor={inputId("code")}>
              <Input
                id={inputId("code")}
                autoComplete="one-time-code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                {...described("code")}
              />
              <FieldError id={fieldErrorId(inputId("code"))} message={fieldError("code")} />
            </Form.Item>
            <Form.Item label="Новый пароль" htmlFor={inputId("newPassword")}>
              <Input
                id={inputId("newPassword")}
                type="password"
                autoComplete="new-password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                {...described("newPassword")}
              />
              <FieldError id={fieldErrorId(inputId("newPassword"))} message={fieldError("newPassword")} />
            </Form.Item>
            {refusalAlert("complete")}
            <Button type="primary" htmlType="submit" block loading={busy === "complete"} disabled={locked("complete")}>
              Сменить пароль и войти
            </Button>
          </FormGrid>
          <div style={{ marginTop: 16 }}>
            {refusalAlert("request")}
            <Button loading={busy === "request"} disabled={locked("request")} onClick={() => void onRequest()}>
              Отправить код ещё раз
            </Button>
          </div>
        </>
      )}
    </CeremonyScreen>
  );
}

export default RecoveryPage;
