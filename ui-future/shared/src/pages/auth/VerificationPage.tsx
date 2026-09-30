// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useEffect, useId, useState } from "react";
import { Alert, Button, Form, Input, Space, Typography } from "antd";
import { type LaneRefusal, laneRefusalOf, loginLane, sessionIdentity, UNKNOWN_SESSION_TEXT } from "@shared/api/login-lane";
import { refusalActionOf } from "@shared/api/refusal-action";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { ADDRESS_STATE_NOT_NAMED_TEXT, AddressStateUnknownPage, SessionPending } from "./AddressStateUnknownPage";
import { CeremonyScreen } from "./CeremonyScreen";
import { loginAddress } from "./ceremony-addresses";
import { useLogout } from "./use-logout";
import { useAddressState } from "./use-address-state";
import { useReturnTo } from "./use-return-to";

// Экран подтверждения адреса почты — `/verification`, вне каркаса (приёмка F6b,
// Р8, Р9).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭКРАН ДЕЛАЕТ
//
//   • решает по ответу края о сессии тем же решением, что страж каркаса
//     (`useAddressState`): сессии нет — уходит на вход без адреса возврата;
//     адрес подтверждён — уходит на адрес возврата, подтверждать нечего;
//     не названо и неизвестно — страница стража (Р7);
//   • при открытии не шлёт ничего, кроме вопроса о сессии и признаков формы:
//     письмо — действие человека либо регистрации, а не следствие открытия
//     адреса (F6b-18). Первое письмо ставит служба при регистрации (Р15);
//   • код из письма отправляет как введён: своего суждения о содержимом кода
//     нет, приведение делает служба (Р7 службы);
//   • отсчёт до следующего письма берёт ТОЛЬКО у службы — из `Retry-After` и на
//     успехе, и на отказе по частоте; нет заголовка — нет и отсчёта (Р8). Пока
//     отсчёт идёт, отправка закрыта — и кнопкой, и в обработчике;
//   • попыток не считает и своей блокировки не заводит: предел — на код, у
//     службы (F6b-39);
//   • решает по `ErrorInfo.reason` тем же решением на отказ, что всюду
//     (`refusalActionOf`): два исхода `400` / `9` различает только причина;
//   • на `401` любого глагола один раз спрашивает «кто я»: неподошедший код и
//     снятая сессия отвечают одним `401`, различает их ответ края о сессии;
//   • адрес возврата читает только через `useReturnTo` — на каждом выходе.
//
// Кнопки «Продолжить» нет: подтверждение в другом месте снимает эту сессию
// (Р10 службы), и следующее обращение экрана это узнаёт (F6b-30).

/** Уйти ДОКУМЕНТОМ: новая личность и новый носитель обязаны дойти до каждого модуля. */
function leaveDocument(to: string) {
  window.location.replace(to);
}

const TITLE = "Подтвердите адрес почты";
const CHECK_CODE_TEXT = "Проверьте код или отправьте новое письмо.";

function sentText(email: string): string {
  return `Письмо с новым кодом отправлено на ${email}. Прежний код больше не действует.`;
}

function waitText(seconds: number): string {
  return `Отправить новое письмо можно через ${seconds} с`;
}

export function VerificationPage({ leave = leaveDocument }: { leave?: (to: string) => void }) {
  const returnTo = useReturnTo();
  const { state, retry } = useAddressState();

  useEffect(() => {
    if (state.kind === "absent") leave(loginAddress());
    else if (state.kind === "confirmed") leave(returnTo);
  }, [state, leave, returnTo]);

  switch (state.kind) {
    case "asking":
    case "absent":
    case "confirmed":
      return <SessionPending />;
    case "not-named":
      return <AddressStateUnknownPage text={ADDRESS_STATE_NOT_NAMED_TEXT} onRetry={retry} leave={leave} />;
    case "unknown":
      return <AddressStateUnknownPage text={UNKNOWN_SESSION_TEXT} onRetry={retry} leave={leave} />;
    case "unconfirmed":
      return <ConfirmationScreen email={state.email} returnTo={returnTo} leave={leave} />;
    default: {
      const unhandled: never = state;
      throw new Error(`положение подтверждения «${JSON.stringify(unhandled)}» не решено экраном`);
    }
  }
}

/** Исход последнего действия на экране — один: новый исход замещает прежний. */
type Outcome =
  | { kind: "refused"; refusal: LaneRefusal; checkCode: boolean }
  | { kind: "sent" };

function refusalTextOf(refusal: LaneRefusal): string {
  // Ответа не было вовсе — служба ничего не сказала, и консоль причины не
  // выдумывает (Р9: при отсутствии ответа — `UNKNOWN_SESSION_TEXT`).
  return refusal.status === 0 ? UNKNOWN_SESSION_TEXT : refusal.message;
}

function ConfirmationScreen({
  email,
  returnTo,
  leave,
}: {
  email: string;
  returnTo: string;
  leave: (to: string) => void;
}) {
  const id = useId();
  const confirmHolder = useFormToken("verify-email-confirm");
  const requestHolder = useFormToken("verify-email");
  const exit = useLogout(leave);
  const [code, setCode] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [requesting, setRequesting] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  // Секунды до следующего письма — число службы; `null` — срока нет.
  const [waiting, setWaiting] = useState<number | null>(null);

  // Отсчёт снимает закрытие, а не отправляет письмо: отправит человек.
  useEffect(() => {
    if (waiting === null) return;
    const t = window.setTimeout(() => setWaiting(waiting > 1 ? waiting - 1 : null), 1000);
    return () => window.clearTimeout(t);
  }, [waiting]);

  /**
   * Исходы, которые уводят с экрана: «адрес уже подтверждён» (по причине) и
   * `401`, за которым край ответил «сессии нет». Вопрос «кто я» — ОДИН и только
   * на `401`: второго обращения к глаголу он не выпускает.
   */
  const leftOn = async (refusal: LaneRefusal): Promise<boolean> => {
    if (refusalActionOf(refusal, "ceremony") === "address-confirmed") {
      leave(returnTo);
      return true;
    }
    if (refusal.status === 401) {
      const who = await sessionIdentity();
      if (who.kind === "absent") {
        leave(loginAddress());
        return true;
      }
    }
    return false;
  };

  const onConfirm = async () => {
    if (confirming) return;
    setConfirming(true);
    setOutcome(null);
    try {
      await loginLane.confirmAddress(confirmHolder, code);
      leave(returnTo);
      return; // экран уходит — кнопка остаётся занятой, второго предъявления нет
    } catch (err) {
      const refusal = laneRefusalOf(err);
      if (await leftOn(refusal)) return;
      setOutcome({ kind: "refused", refusal, checkCode: refusal.status === 401 });
    }
    setConfirming(false);
  };

  const onRequest = async () => {
    if (requesting || waiting !== null) return;
    setRequesting(true);
    setOutcome(null);
    try {
      const { retryAfterSeconds } = await loginLane.requestAddressConfirmation(requestHolder);
      setOutcome({ kind: "sent" });
      setWaiting(retryAfterSeconds);
    } catch (err) {
      const refusal = laneRefusalOf(err);
      if (await leftOn(refusal)) return;
      setOutcome({ kind: "refused", refusal, checkCode: false });
      if (refusal.code === 8 && refusal.retryAfterSeconds !== null) setWaiting(refusal.retryAfterSeconds);
    }
    setRequesting(false);
  };

  const codeId = `${id}-code`;

  return (
    <CeremonyScreen title={TITLE}>
      <Typography.Paragraph>
        {`Чтобы продолжить работу в консоли, подтвердите адрес ${email}: введите код из письма, отправленного на этот адрес.`}
      </Typography.Paragraph>
      <Typography.Paragraph>
        Письмо с кодом приходит после регистрации. Если письма нет или код не подходит, отправьте новое.
      </Typography.Paragraph>
      {outcome?.kind === "refused" && (
        <div style={{ marginBottom: 16 }}>
          <Alert
            type="error"
            showIcon
            message={refusalTextOf(outcome.refusal)}
            description={outcome.checkCode ? CHECK_CODE_TEXT : undefined}
          />
        </div>
      )}
      {outcome?.kind === "sent" && (
        <Typography.Paragraph>{sentText(email)}</Typography.Paragraph>
      )}
      <FormGrid label="Подтверждение адреса почты" onSubmit={() => void onConfirm()}>
        <Form.Item label="Код из письма" htmlFor={codeId}>
          <Input id={codeId} autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} />
        </Form.Item>
        <Button type="primary" htmlType="submit" block loading={confirming}>
          Подтвердить
        </Button>
      </FormGrid>
      <div style={{ marginTop: 16 }}>
        {waiting !== null && <Typography.Paragraph>{waitText(waiting)}</Typography.Paragraph>}
        {exit.refusal && (
          <div style={{ marginBottom: 16 }}>
            <LaneRefusalAlert refusal={exit.refusal} />
          </div>
        )}
        <Space wrap>
          <Button loading={requesting} disabled={waiting !== null} onClick={() => void onRequest()}>
            Отправить новое письмо
          </Button>
          <Button loading={exit.busy} onClick={() => void exit.logout()}>
            Выйти
          </Button>
        </Space>
      </div>
    </CeremonyScreen>
  );
}

export default VerificationPage;
