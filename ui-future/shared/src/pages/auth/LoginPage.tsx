// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useEffect, useId, useState, type FormEvent } from "react";
import { Link } from "react-router";
import { Button, Checkbox, Input, Spin } from "antd";
import { LaneRefusal, loginLane, sessionIdentity, type SecondFactorPresentation } from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { EMPTY_PRESENTATION, SecondFactorCodeField } from "@shared/components/molecules/auth/SecondFactorCodeField";
import { useFormToken } from "@shared/hooks/use-form-token";
import { CeremonyField, CeremonyScreen } from "./CeremonyScreen";
import { useReturnTo } from "./use-return-to";

// Экран входа — церемонию ведёт консоль своими глаголами (приёмка F8, S1).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭКРАН ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ
//
//   • сначала спрашивает край, есть ли сессия: у человека с живой сессией формы
//     нет — он уходит на адрес возврата (F8-11); негодный носитель и его
//     отсутствие для экрана одно состояние (F8-12);
//   • отправляет форму глаголом входа с признаком своего вида; отказ показывает
//     ДОСЛОВНО, поле, названное службой, отмечает (Р2, F8-06);
//   • на отказе по частоте называет срок из `Retry-After` и до его истечения
//     отправку не производит — кнопка закрыта, а закрытая кнопка по умолчанию
//     закрывает и отправку клавишей ввода (F8-09);
//   • после входа уводит документ на адрес возврата, только своего
//     происхождения (F8-13): перезагрузка документа нужна, чтобы каждый модуль
//     прочёл новую личность, а не держал прежнюю;
//   • пути на восстановление доступа НЕ предлагает: его на посадке нет до S3, и
//     обещание пути, которого нет, хуже его отсутствия (Р4, F8-39).
//
// Своего правила пароля, формы кода или адреса здесь нет (Р2): незаполненное
// поле называет служба.

/** Поле формы входа, которое служба может назвать отказом, — и где его отметить. */
type LoginField = "email" | "password" | "code";

function fieldOf(refusal: LaneRefusal | null): LoginField | null {
  switch (refusal?.field) {
    case "email":
      return "email";
    case "password":
      return "password";
    case "secondFactor.code":
    case "secondFactor.method":
    case "secondFactor":
      return "code";
    default:
      return null;
  }
}

/** Уйти ДОКУМЕНТОМ: новая личность обязана дойти до каждого модуля. */
function leaveDocument(to: string) {
  window.location.replace(to);
}

export function LoginPage({ leave = leaveDocument }: { leave?: (to: string) => void }) {
  const id = useId();
  const returnTo = useReturnTo();
  const holder = useFormToken("login");
  const [phase, setPhase] = useState<"проверка сессии" | "форма">("проверка сессии");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [withFactor, setWithFactor] = useState(false);
  const [factor, setFactor] = useState<SecondFactorPresentation>(EMPTY_PRESENTATION);
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const [lockedFor, setLockedFor] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    void sessionIdentity().then((who) => {
      if (cancelled) return;
      if (who) leave(returnTo);
      else setPhase("форма");
    });
    return () => {
      cancelled = true;
    };
  }, [leave, returnTo]);

  // Срок из `Retry-After`: до его истечения отправка закрыта. Таймер снимает
  // закрытие, а не повторяет отправку — повторит её человек.
  useEffect(() => {
    if (lockedFor === null) return;
    const t = window.setTimeout(() => setLockedFor(null), lockedFor * 1000);
    return () => window.clearTimeout(t);
  }, [lockedFor]);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || lockedFor !== null) return;
    setBusy(true);
    setRefusal(null);
    try {
      await loginLane.login(holder, { email, password, secondFactor: withFactor ? factor : undefined });
      leave(returnTo);
      return; // экран уходит — кнопка остаётся занятой, повторной отправки нет
    } catch (err) {
      const r = err instanceof LaneRefusal ? err : new LaneRefusal(0, null, String(err), null, null, null);
      setRefusal(r);
      if (r.code === 8 && r.retryAfterSeconds !== null) setLockedFor(r.retryAfterSeconds);
    }
    setBusy(false);
  };

  if (phase === "проверка сессии") {
    return (
      <CeremonyScreen title="Вход в консоль">
        <Spin />
      </CeremonyScreen>
    );
  }

  const marked = fieldOf(refusal);
  const fieldError = (f: LoginField) => (marked === f ? refusal!.message : null);
  const described = (f: LoginField) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": `${id}-${f}-error`, status: "error" as const }
      : {};

  return (
    <CeremonyScreen
      title="Вход в консоль"
      footer={
        <Link to={`/registration${returnTo !== "/" ? `?returnTo=${encodeURIComponent(returnTo)}` : ""}`}>
          Завести учётную запись
        </Link>
      }
    >
      <form aria-label="Вход в консоль" onSubmit={(e) => void onSubmit(e)} noValidate>
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
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            {...described("password")}
          />
        </CeremonyField>
        <div style={{ marginBottom: 16 }}>
          <Checkbox checked={withFactor} onChange={(e) => setWithFactor(e.target.checked)}>
            Подтвердить вторым фактором
          </Checkbox>
        </div>
        {withFactor && (
          <div style={{ marginBottom: 16 }}>
            <SecondFactorCodeField value={factor} onChange={setFactor} codeError={fieldError("code")} />
          </div>
        )}
        {refusal && marked === null && (
          <div style={{ marginBottom: 16 }}>
            <LaneRefusalAlert refusal={refusal} />
          </div>
        )}
        <Button type="primary" htmlType="submit" block loading={busy} disabled={lockedFor !== null}>
          Войти
        </Button>
      </form>
    </CeremonyScreen>
  );
}

export default LoginPage;
