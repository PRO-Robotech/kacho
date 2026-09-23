// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { Button, Input } from "antd";
import { LaneRefusal, loginLane } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { useFormToken } from "@shared/hooks/use-form-token";
import { CeremonyField, CeremonyScreen } from "./CeremonyScreen";
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

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    try {
      await loginLane.register(holder, { email, password });
      leave(returnTo);
      return;
    } catch (err) {
      setRefusal(err instanceof LaneRefusal ? err : new LaneRefusal(0, null, String(err), null, null, null));
    }
    setBusy(false);
  };

  const marked = fieldOf(refusal);
  const fieldError = (f: RegistrationField) => (marked === f ? refusal!.message : null);
  const described = (f: RegistrationField) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": `${id}-${f}-error`, status: "error" as const }
      : {};

  return (
    <CeremonyScreen
      title="Новая учётная запись"
      footer={<Link to={loginAddress(returnTo !== "/" ? returnTo : undefined)}>Уже есть учётная запись — войти</Link>}
    >
      <form aria-label="Новая учётная запись" onSubmit={(e) => void onSubmit(e)} noValidate>
        <CeremonyField id={`${id}-email`} label="Адрес электронной почты" error={fieldError("email")}>
          <Input
            id={`${id}-email`}
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            {...described("email")}
          />
        </CeremonyField>
        <CeremonyField id={`${id}-password`} label="Пароль" error={fieldError("password")}>
          <Input
            id={`${id}-password`}
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            {...described("password")}
          />
        </CeremonyField>
        {refusal && marked === null && (
          <div style={{ marginBottom: 16 }}>
            <LaneRefusalAlert refusal={refusal} />
          </div>
        )}
        <Button type="primary" htmlType="submit" block loading={busy}>
          Завести учётную запись
        </Button>
      </form>
    </CeremonyScreen>
  );
}

export default RegistrationPage;
