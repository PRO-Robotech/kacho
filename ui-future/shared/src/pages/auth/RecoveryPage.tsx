// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId, useState } from "react";
import { Link } from "react-router";
import { Button, Form, Input, Typography } from "antd";
import { type LaneRefusal, laneRefusalOf, loginLane, SubmissionSuperseded } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { CeremonyScreen } from "./CeremonyScreen";
import { loginAddress } from "./ceremony-addresses";
import { useReturnTo } from "./use-return-to";

// Экран восстановления доступа (приёмка NTF-2, NTF2-43, NTF2-72, NTF2-58;
// замысел `issue-2917` З10).
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВА ГЛАГОЛА НА ОДНОМ ЭКРАНЕ
//
//   • запрос письма (`POST /iam/v1/auth/recovery`) — ответ `200 {}` одинаков для
//     заведённого и незаведённого адреса, и экран говорит ОДНО: «Если адрес
//     зарегистрирован, мы отправили письмо». Экран, различающий эти случаи,
//     стал бы оракулом заведённых адресов. Форма запроса остаётся на экране:
//     письмо можно запросить снова тем же адресом либо другим;
//   • предъявление кода из письма с новым паролем
//     (`POST /iam/v1/auth/recovery/complete`) — успех выдаёт сессию, и экран
//     уводит документ на адрес возврата, как вход.
//
// ОГРАНИЧИТЕЛЬ КРАЯ (Р5). Запрос письма — анонимный почтовый глагол: край может
// ответить вызовом доказательства работы, и клиент полосы решает его сам,
// прозрачно для экрана (форма в положении отправки, пока решатель ищет). Отказ
// ограничителя — `503` хранилища, вызов, оставшийся без доказательства, отказ
// по частоте — экран показывает `message` ответа ДОСЛОВНО, без своего текста;
// форма остаётся редактируемой. Уход экрана завершает решатель
// (`useFormToken`).
//
// Своего правила пароля и формы кода здесь нет (Р2): их судит служба и
// называет поле.

/** Уйти ДОКУМЕНТОМ: новая личность обязана дойти до каждого модуля. */
function leaveDocument(to: string) {
  window.location.replace(to);
}

/** Единый текст после запроса письма — один на любой ответ `200` (NTF2-72). */
export const RECOVERY_REQUESTED_TEXT = "Если адрес зарегистрирован, мы отправили письмо с кодом.";

type CompletionField = "code" | "newPassword";

function completionFieldOf(refusal: LaneRefusal | null): CompletionField | null {
  return refusal?.field === "code" || refusal?.field === "newPassword" ? refusal.field : null;
}

export function RecoveryPage({ leave = leaveDocument }: { leave?: (to: string) => void }) {
  const id = useId();
  const returnTo = useReturnTo();
  const request = useFormToken("recovery");
  const completion = useFormToken("recovery-complete");
  const [email, setEmail] = useState("");
  const [requesting, setRequesting] = useState(false);
  const [requestRefusal, setRequestRefusal] = useState<LaneRefusal | null>(null);
  /** Адрес, на который письмо запрошено ответом `200`; `null` — ещё не запрошено. */
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [completing, setCompleting] = useState(false);
  const [completionRefusal, setCompletionRefusal] = useState<LaneRefusal | null>(null);

  const onRequest = async () => {
    if (requesting) return;
    setRequesting(true);
    setRequestRefusal(null);
    const address = email;
    try {
      await loginLane.requestRecovery(request, { email: address });
      setSentTo(address);
      setCompletionRefusal(null);
    } catch (err) {
      // Отправку сменила новая либо экран ушёл — показывать нечего.
      if (err instanceof SubmissionSuperseded) return;
      setRequestRefusal(laneRefusalOf(err));
    }
    setRequesting(false);
  };

  const onComplete = async () => {
    if (completing || sentTo === null) return;
    setCompleting(true);
    setCompletionRefusal(null);
    try {
      await loginLane.completeRecovery(completion, { email: sentTo, code, newPassword });
      leave(returnTo);
      return;
    } catch (err) {
      if (err instanceof SubmissionSuperseded) return;
      setCompletionRefusal(laneRefusalOf(err));
    }
    setCompleting(false);
  };

  const inputId = (f: string) => `${id}-${f}`;
  const marked = completionFieldOf(completionRefusal);
  const described = (f: CompletionField) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId(f)), status: "error" as const }
      : {};

  return (
    <CeremonyScreen
      title="Восстановление доступа"
      footer={<Link to={loginAddress(returnTo)}>Вспомнили пароль — войти</Link>}
    >
      <FormGrid label="Запрос письма для восстановления доступа" onSubmit={() => void onRequest()}>
        <Form.Item label="Адрес электронной почты" htmlFor={inputId("email")}>
          <Input
            id={inputId("email")}
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Form.Item>
        {requestRefusal && (
          <div style={{ marginBottom: 16 }}>
            <LaneRefusalAlert refusal={requestRefusal} verbatim />
          </div>
        )}
        <Button type="primary" htmlType="submit" block loading={requesting} disabled={requesting}>
          Отправить письмо с кодом
        </Button>
      </FormGrid>
      {sentTo !== null && (
        <>
          <Typography.Paragraph style={{ marginTop: 16 }}>{RECOVERY_REQUESTED_TEXT}</Typography.Paragraph>
          <FormGrid label="Новый пароль по коду из письма" onSubmit={() => void onComplete()}>
            <Form.Item label="Код из письма" htmlFor={inputId("code")}>
              <Input
                id={inputId("code")}
                autoComplete="one-time-code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                {...described("code")}
              />
              <FieldError
                id={fieldErrorId(inputId("code"))}
                message={marked === "code" ? completionRefusal!.message : undefined}
              />
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
              <FieldError
                id={fieldErrorId(inputId("newPassword"))}
                message={marked === "newPassword" ? completionRefusal!.message : undefined}
              />
            </Form.Item>
            {completionRefusal && marked === null && (
              <div style={{ marginBottom: 16 }}>
                <LaneRefusalAlert refusal={completionRefusal} />
              </div>
            )}
            <Button type="primary" htmlType="submit" block loading={completing} disabled={completing}>
              Сменить пароль и войти
            </Button>
          </FormGrid>
        </>
      )}
    </CeremonyScreen>
  );
}

export default RecoveryPage;
