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

// Экран регистрации — человек заводит себя сам (приёмка F8, S1, группа C) —
// «сначала письмо, потом сессия» (приёмка NTF-2, Р9, NTF2-80, NTF2-82):
//
//   • шаг 1 — адрес и пароль (`POST /iam/v1/auth/register`). Ответ `200 {}`
//     ОДИНАКОВ для свободного и занятого адреса и сессии не несёт; на адрес
//     уходит письмо — с кодом либо «учётная запись уже есть». Экран говорит одно
//     и то же в обоих случаях и спрашивает код. Край может ответить вызовом
//     доказательства работы — его решает клиент полосы прозрачно (NTF2-72);
//     отказ ограничителя экран показывает дословно, без своего текста (Р5);
//   • шаг 2 — код из письма с ТЕМ ЖЕ адресом и паролем
//     (`POST /iam/v1/auth/register/confirm`). Только он заводит учётную запись
//     и сессию, и только он уводит в консоль. Неверный код — отказ службы
//     дословно, код можно ввести снова (NTF2-82).
//
// Отказ показывается ДОСЛОВНО и ни словом больше (F8-15): служба отвечает ОДНИМ
// отказом на занятый адрес и на потолок темпа, и экран, добавивший «такой адрес
// уже есть», стал бы оракулом заведённых адресов. Правило пароля судит служба
// и называет поле (F8-16) — консоль своего правила не применяет (Р2).
//
// Отображаемого имени форма не спрашивает: служба его у заводящего себя не
// принимает (выводит из адреса), а поле без читателя принимать нельзя.

type RegistrationField = "email" | "password";

function fieldOf(refusal: LaneRefusal | null): RegistrationField | null {
  return refusal?.field === "email" || refusal?.field === "password" ? refusal.field : null;
}

function leaveDocument(to: string) {
  window.location.replace(to);
}

/** Что экран говорит после шага 1 — одно на свободный и занятый адрес. */
export const REGISTRATION_REQUESTED_TEXT =
  "Мы отправили письмо на этот адрес. Если в нём код — введите его ниже; если учётная запись на этот адрес уже есть, письмо подскажет, как восстановить доступ.";

export function RegistrationPage({ leave = leaveDocument }: { leave?: (to: string) => void }) {
  const id = useId();
  const returnTo = useReturnTo();
  const holder = useFormToken("register");
  const confirmation = useFormToken("register-confirm");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  /** Адрес и пароль, принятые шагом 1; `null` — шаг 1 ещё не пройден. */
  const [requested, setRequested] = useState<{ email: string; password: string } | null>(null);
  const [code, setCode] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [confirmRefusal, setConfirmRefusal] = useState<LaneRefusal | null>(null);

  const onSubmit = async () => {
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    const form = { email, password };
    try {
      await loginLane.register(holder, form);
      setRequested(form);
      setCode("");
      setConfirmRefusal(null);
    } catch (err) {
      if (err instanceof SubmissionSuperseded) return;
      setRefusal(laneRefusalOf(err));
    }
    setBusy(false);
  };

  const onConfirm = async () => {
    if (confirming || requested === null) return;
    setConfirming(true);
    setConfirmRefusal(null);
    try {
      await loginLane.confirmRegistration(confirmation, { ...requested, code });
      leave(returnTo);
      return;
    } catch (err) {
      if (err instanceof SubmissionSuperseded) return;
      setConfirmRefusal(laneRefusalOf(err));
    }
    setConfirming(false);
  };

  const marked = fieldOf(refusal);
  const inputId = (f: RegistrationField | "code") => `${id}-${f}`;
  const fieldError = (f: RegistrationField) => (marked === f ? refusal!.message : undefined);
  const described = (f: RegistrationField) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId(f)), status: "error" as const }
      : {};

  const footer = <Link to={loginAddress(returnTo)}>Уже есть учётная запись — войти</Link>;

  if (requested !== null) {
    const codeMarked = confirmRefusal?.field === "code";
    return (
      <CeremonyScreen title="Новая учётная запись" footer={footer}>
        <Typography.Paragraph>{REGISTRATION_REQUESTED_TEXT}</Typography.Paragraph>
        <Typography.Paragraph>
          Адрес: <Typography.Text strong>{requested.email}</Typography.Text>
        </Typography.Paragraph>
        <FormGrid label="Подтверждение регистрации" onSubmit={() => void onConfirm()}>
          <Form.Item label="Код из письма" htmlFor={inputId("code")}>
            <Input
              id={inputId("code")}
              autoComplete="one-time-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              {...(codeMarked
                ? {
                    "aria-invalid": true as const,
                    "aria-describedby": fieldErrorId(inputId("code")),
                    status: "error" as const,
                  }
                : {})}
            />
            <FieldError id={fieldErrorId(inputId("code"))} message={codeMarked ? confirmRefusal.message : undefined} />
          </Form.Item>
          {confirmRefusal && !codeMarked && (
            <div style={{ marginBottom: 16 }}>
              <LaneRefusalAlert refusal={confirmRefusal} />
            </div>
          )}
          <Button type="primary" htmlType="submit" block loading={confirming} disabled={confirming}>
            Подтвердить и войти
          </Button>
        </FormGrid>
        <Button type="link" onClick={() => setRequested(null)} disabled={confirming}>
          Изменить адрес или пароль
        </Button>
      </CeremonyScreen>
    );
  }

  return (
    <CeremonyScreen title="Новая учётная запись" footer={footer}>
      <FormGrid label="Новая учётная запись" onSubmit={() => void onSubmit()}>
        <Form.Item label="Адрес электронной почты" htmlFor={inputId("email")}>
          <Input
            id={inputId("email")}
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            {...described("email")}
          />
          <FieldError id={fieldErrorId(inputId("email"))} message={fieldError("email")} />
        </Form.Item>
        <Form.Item label="Пароль" htmlFor={inputId("password")}>
          <Input
            id={inputId("password")}
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            {...described("password")}
          />
          <FieldError id={fieldErrorId(inputId("password"))} message={fieldError("password")} />
        </Form.Item>
        {refusal && marked === null && (
          <div style={{ marginBottom: 16 }}>
            <LaneRefusalAlert refusal={refusal} verbatim />
          </div>
        )}
        <Button type="primary" htmlType="submit" block loading={busy} disabled={busy}>
          Завести учётную запись
        </Button>
      </FormGrid>
    </CeremonyScreen>
  );
}

export default RegistrationPage;
