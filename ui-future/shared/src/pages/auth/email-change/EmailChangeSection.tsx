// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useEffect, useId, useState } from "react";
import { Alert, Button, Form, Input, Typography } from "antd";
import { LANE_REASON, type LaneRefusal, laneRefusalOf, loginLane, sessionIdentity } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { ACCOUNT_SETTINGS_ADDRESS, loginAddress } from "../ceremony-addresses";

// Смена адреса почты в разделе «Учётная запись» (приёмка F8-S6, Р3–Р5).
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ СОСТОЯНИЯ (Р3)
//
//   • покой — кнопка «Сменить адрес»; адрес и отметку показывает раздел;
//   • форма — «Новый адрес», «Отправить код», «Отмена»;
//   • код   — строка о письме, «Код из письма», «Подтвердить», «Отправить снова»
//             с отсчётом по `Retry-After`, «Отмена».
//
// «Отмена» глаголов не зовёт ни из формы, ни из кода: отложенная смена истекает
// сама (EC Р5). Признаки формы добываются, когда форма ОТКРЫТА, а не при
// открытии экрана: в покое раздел ничего не просит.
//
// Строка о письме говорит «если этот адрес может быть использован»: служба не
// говорит, занят ли адрес (EC Р4), и экран тоже не говорит.
//
// Поле кода не несёт браузерной проверки обязательности, и «Подтвердить»
// включена на пустом поле: пустое предъявление доходит до службы, и её отказ
// стоит у поля кода (F8S6-12). Своего правила о коде и адресе у консоли нет (Р2).
//
// ОТКАЗЫ (Р4)
//
//   • `400` о поле — текст службы у поля, состояние сохраняется;
//   • `403 SESSION_NOT_FRESH` — повышение и один повтор делает клиент полосы;
//   • `401` с вызовом `insufficient_user_authentication` на любом из двух
//     путей — отказ по УРОВНЮ (EC Р2, §1.4в): повышение вторым фактором и
//     один повтор делает клиент полосы. До экрана он доходит, лишь когда
//     повышение не состоялось (отменено или не прошло): экран называет его
//     как есть — «Подтверждение личности не состоялось…» — и предлагает
//     «Повторить с подтверждением» тем же глаголом с тем же вводом, то есть
//     снова через повышение. Состояние и ввод сохраняются. Вопроса «кто я»
//     нет: при живой сессии он ответил бы `200` на оба `401`, и отказ по
//     уровню был бы назван неподошедшим кодом, которого никто не проверял.
//     Различает отказы машинный признак вызова, а не статус;
//   • `401` без такого вызова на предъявлении — ОДИН вопрос «кто я»: человек есть — «Код не
//     подошёл», поле кода очищено; сессии нет — экран входа. Неподошедший код и
//     снятая сессия отвечают одним `401`, различает их ответ края о сессии
//     (§1.4б, образец — экран подтверждения адреса). Второго предъявления нет;
//   • `409 EMAIL_IN_USE` — «Этот адрес уже используется», назад к форме;
//   • `429` — «Слишком много попыток», отсчёт по `Retry-After`, отправка
//     выключена до его конца;
//   • прочее (ответ края на молчание службы, `503` службы) — назван дословно;
//     сессия не гасится, носитель не трогается.
//
// После успешного подтверждения раздел возвращается в покой, а экран заново
// спрашивает «кто я» (`onChanged`) — новый адрес и отметку называет край.

/** Отсчёт по `Retry-After` — числом службы, без пересчёта. */
function waitText(action: string, seconds: number): string {
  return `${action} можно через ${seconds} с`;
}

export const CODE_REJECTED_TEXT = "Код не подошёл";
export const EMAIL_IN_USE_TEXT = "Этот адрес уже используется";
export const TOO_MANY_ATTEMPTS_TEXT = "Слишком много попыток";
export const STEP_UP_NOT_DONE_TEXT = "Подтверждение личности не состоялось: смена адреса требует второго фактора";
export const RETRY_WITH_STEP_UP_TEXT = "Повторить с подтверждением";

/** Признак вызова края о полу уровня (RFC 9470): отказ по уровню, а не о коде. */
const LEVEL_CHALLENGE = "insufficient_user_authentication";

function sentText(email: string): string {
  return `Письмо с кодом отправлено на ${email}, если этот адрес может быть использован`;
}

/** Уйти документом: новая личность обязана дойти до каждого модуля. */
function leaveDocument(to: string) {
  window.location.replace(to);
}

/** Секунды отсчёта; `null` — отсчёта нет. Тикает раз в секунду до нуля. */
function useCountdown(): [number | null, (s: number | null) => void] {
  const [left, setLeft] = useState<number | null>(null);
  useEffect(() => {
    if (left === null) return;
    const t = window.setTimeout(() => setLeft(left > 1 ? left - 1 : null), 1000);
    return () => window.clearTimeout(t);
  }, [left]);
  return [left, setLeft];
}

/**
 * Повтор после несостоявшегося повышения — тот же глагол с тем же вводом:
 * запрос — с адресом, с которым он ушёл («Отправить снова» в состоянии «код» —
 * тоже запрос), предъявление — с кодом из поля.
 */
type Retry = { verb: "request"; address: string } | { verb: "confirm" };

/** Что показано под формой: исход последнего действия — один, новый замещает прежний. */
type Notice =
  | { kind: "refused"; refusal: LaneRefusal }
  | { kind: "text"; text: string }
  // Повышение не состоялось; кнопка — `Retry`.
  | { kind: "step-up-not-done"; retry: Retry };

function markedBy(inputId: string, error: string | null) {
  return error
    ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId), status: "error" as const }
    : {};
}

function ChangeForm({
  onClose,
  onChanged,
  leave,
}: {
  onClose: () => void;
  onChanged: () => Promise<void> | void;
  leave: (to: string) => void;
}) {
  const id = useId();
  const requestHolder = useFormToken("email-change");
  const confirmHolder = useFormToken("email-change-confirm");
  const [stage, setStage] = useState<"form" | "code">("form");
  const [newEmail, setNewEmail] = useState("");
  // Адрес, на который ушёл принятый запрос: «Отправить снова» зовёт его же.
  const [sentTo, setSentTo] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState<"request" | "confirm" | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [fieldError, setFieldError] = useState<{ field: "newEmail" | "code"; text: string } | null>(null);
  const [requestWait, setRequestWait] = useCountdown();
  const [confirmWait, setConfirmWait] = useCountdown();

  /** Отказ, общий обоим глаголам: уровень, темп и поле — по Р4; прочее — дословно. */
  const refused = (refusal: LaneRefusal, onWait: (s: number | null) => void, retry: Retry) => {
    if (refusal.challenge === LEVEL_CHALLENGE) {
      setNotice({ kind: "step-up-not-done", retry });
      return;
    }
    if (refusal.reason === LANE_REASON.tooManyAttempts) {
      setNotice({ kind: "text", text: TOO_MANY_ATTEMPTS_TEXT });
      onWait(refusal.retryAfterSeconds);
      return;
    }
    if (refusal.field === "newEmail" || refusal.field === "code") {
      setFieldError({ field: refusal.field, text: refusal.message });
      return;
    }
    setNotice({ kind: "refused", refusal });
  };

  const request = async (address: string) => {
    if (busy !== null || requestWait !== null) return;
    setBusy("request");
    setNotice(null);
    setFieldError(null);
    try {
      const { retryAfterSeconds } = await loginLane.requestEmailChange(requestHolder, address);
      setSentTo(address);
      setCode("");
      setStage("code");
      setRequestWait(retryAfterSeconds);
    } catch (err) {
      refused(laneRefusalOf(err), setRequestWait, { verb: "request", address });
    }
    setBusy(null);
  };

  const confirm = async () => {
    if (busy !== null || confirmWait !== null) return;
    setBusy("confirm");
    setNotice(null);
    setFieldError(null);
    try {
      await loginLane.confirmEmailChange(confirmHolder, code);
      // Раздел — в покой, затем экран переспрашивает «кто я»: новый адрес и
      // отметку называет край, а не эта форма.
      onClose();
      void onChanged();
      return;
    } catch (err) {
      const refusal = laneRefusalOf(err);
      if (refusal.challenge === LEVEL_CHALLENGE) {
        // Код не проверялся: он остаётся в поле, «кто я» не спрашивается.
        refused(refusal, setConfirmWait, { verb: "confirm" });
      } else if (refusal.status === 401) {
        // Один вопрос «кто я»: он различает неподошедший код и снятую сессию.
        const who = await sessionIdentity();
        if (who.kind === "absent") {
          leave(loginAddress(ACCOUNT_SETTINGS_ADDRESS));
          return; // экран уходит — кнопка остаётся занятой, второго предъявления нет
        }
        if (who.kind === "present") {
          setCode("");
          setNotice({ kind: "text", text: CODE_REJECTED_TEXT });
        } else {
          setNotice({ kind: "refused", refusal: who.refusal });
        }
      } else if (refusal.reason === LANE_REASON.emailInUse) {
        setNewEmail(sentTo);
        setStage("form");
        setNotice({ kind: "text", text: EMAIL_IN_USE_TEXT });
      } else {
        refused(refusal, setConfirmWait, { verb: "confirm" });
      }
    }
    setBusy(null);
  };

  const emailId = `${id}-new-email`;
  const codeId = `${id}-code`;
  const emailError = fieldError?.field === "newEmail" ? fieldError.text : null;
  const codeError = fieldError?.field === "code" ? fieldError.text : null;

  /** Повтор того же глагола с тем же вводом — снова через клиент полосы и его повышение. */
  const retryAfterStepUp = (retry: Retry) => void (retry.verb === "request" ? request(retry.address) : confirm());

  const noticeView =
    notice === null ? null : (
      <div style={{ marginBottom: 12 }}>
        {notice.kind === "refused" ? (
          <LaneRefusalAlert refusal={notice.refusal} />
        ) : notice.kind === "step-up-not-done" ? (
          <Alert
            type="error"
            showIcon
            message={STEP_UP_NOT_DONE_TEXT}
            action={
              <Button size="small" onClick={() => retryAfterStepUp(notice.retry)} disabled={busy !== null}>
                {RETRY_WITH_STEP_UP_TEXT}
              </Button>
            }
          />
        ) : (
          <Alert type="error" showIcon message={notice.text} />
        )}
      </div>
    );

  if (stage === "form") {
    return (
      <FormGrid label="Смена адреса почты" onSubmit={() => void request(newEmail)}>
        <Form.Item label="Новый адрес" htmlFor={emailId}>
          <Input
            id={emailId}
            type="email"
            autoComplete="email"
            value={newEmail}
            onChange={(e) => setNewEmail(e.target.value)}
            {...markedBy(emailId, emailError)}
          />
          <FieldError id={fieldErrorId(emailId)} message={emailError ?? undefined} />
        </Form.Item>
        {noticeView}
        {requestWait !== null && <Typography.Paragraph>{waitText("Отправить код", requestWait)}</Typography.Paragraph>}
        <Button
          type="primary"
          htmlType="submit"
          loading={busy === "request"}
          disabled={requestWait !== null}
          style={{ marginRight: 8 }}
        >
          Отправить код
        </Button>
        <Button onClick={onClose} disabled={busy !== null}>
          Отмена
        </Button>
      </FormGrid>
    );
  }

  return (
    <FormGrid label="Подтверждение нового адреса" onSubmit={() => void confirm()}>
      <Typography.Paragraph>{sentText(sentTo)}</Typography.Paragraph>
      <Form.Item label="Код из письма" htmlFor={codeId}>
        <Input
          id={codeId}
          autoComplete="one-time-code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          {...markedBy(codeId, codeError)}
        />
        <FieldError id={fieldErrorId(codeId)} message={codeError ?? undefined} />
      </Form.Item>
      {noticeView}
      {confirmWait !== null && <Typography.Paragraph>{waitText("Подтвердить", confirmWait)}</Typography.Paragraph>}
      {requestWait !== null && <Typography.Paragraph>{waitText("Отправить снова", requestWait)}</Typography.Paragraph>}
      <Button
        type="primary"
        htmlType="submit"
        loading={busy === "confirm"}
        disabled={confirmWait !== null}
        style={{ marginRight: 8 }}
      >
        Подтвердить
      </Button>
      <Button
        loading={busy === "request"}
        disabled={requestWait !== null || busy === "confirm"}
        onClick={() => void request(sentTo)}
        style={{ marginRight: 8 }}
      >
        Отправить снова
      </Button>
      <Button onClick={onClose} disabled={busy !== null}>
        Отмена
      </Button>
    </FormGrid>
  );
}

/** Действие раздела «Учётная запись»: покой — кнопка; форма и код — `ChangeForm`. */
export function EmailChangeAction({
  onChanged,
  leave = leaveDocument,
}: {
  /** Спросить «кто я» заново: новый адрес и отметку называет край. */
  onChanged: () => Promise<void> | void;
  leave?: (to: string) => void;
}) {
  const [open, setOpen] = useState(false);
  if (!open) {
    return (
      <Button onClick={() => setOpen(true)} style={{ marginTop: 8 }}>
        Сменить адрес
      </Button>
    );
  }
  return (
    <div style={{ marginTop: 12 }}>
      <ChangeForm onClose={() => setOpen(false)} onChanged={onChanged} leave={leave} />
    </div>
  );
}
