// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId, useState } from "react";
import { Link } from "react-router";
import { Button, Form, Input } from "antd";
import { LaneRefusal, laneRefusalOf, loginLane } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { CeremonyScreen } from "./CeremonyScreen";
import { loginAddress } from "./ceremony-addresses";
import { useReturnTo } from "./use-return-to";

// Экран регистрации — человек заводит себя сам и сразу получает сессию
// (приёмка F8, S1, группа C).
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

export function RegistrationPage({ leave = leaveDocument }: { leave?: (to: string) => void }) {
  const id = useId();
  const returnTo = useReturnTo();
  const holder = useFormToken("register");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  const onSubmit = async () => {
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    try {
      await loginLane.register(holder, { email, password });
      leave(returnTo);
      return;
    } catch (err) {
      setRefusal(laneRefusalOf(err));
    }
    setBusy(false);
  };

  const marked = fieldOf(refusal);
  const inputId = (f: RegistrationField) => `${id}-${f}`;
  const fieldError = (f: RegistrationField) => (marked === f ? refusal!.message : undefined);
  const described = (f: RegistrationField) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId(f)), status: "error" as const }
      : {};

  return (
    <CeremonyScreen
      title="Новая учётная запись"
      footer={<Link to={loginAddress(returnTo)}>Уже есть учётная запись — войти</Link>}
    >
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
            <LaneRefusalAlert refusal={refusal} />
          </div>
        )}
        <Button type="primary" htmlType="submit" block loading={busy}>
          Завести учётную запись
        </Button>
      </FormGrid>
    </CeremonyScreen>
  );
}

export default RegistrationPage;
