// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useState } from "react";
import { Link } from "react-router";
import { Alert, Button, Checkbox, Form, Input, Space, Spin, Typography } from "antd";
import {
  type LaneRefusal,
  laneRefusalOf,
  loginLane,
  sessionIdentity,
  type RefusalInput,
  type SecondFactorPresentation,
} from "@shared/api/login-lane";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { EMPTY_PRESENTATION, SecondFactorCodeField } from "@shared/components/molecules/auth/SecondFactorCodeField";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { AccessKeySignInButton, useAccessKeySignIn } from "./access-key";
import { CeremonyScreen } from "./CeremonyScreen";
import { recoveryAddress, registrationAddress } from "./ceremony-addresses";
import { useReturnTo } from "./use-return-to";

// Экран входа — церемонию ведёт консоль своими глаголами (приёмка F8, S1).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭКРАН ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ
//
//   • сначала спрашивает край, есть ли сессия: у человека с живой сессией формы
//     нет — он уходит на адрес возврата (F8-11); негодный носитель и его
//     отсутствие для экрана одно состояние (F8-12); край не ответил по
//     существу — экран этого не скрывает и никуда не уводит: форма есть, а
//     рядом названо, что узнать о сессии не удалось (условие C6);
//   • отправляет форму глаголом входа с признаком своего вида; отказ показывает
//     ДОСЛОВНО, поле, названное службой, отмечает (Р2, F8-06);
//   • на отказе по частоте называет срок из `Retry-After` и до его истечения
//     отправку не производит — кнопка закрыта, а закрытая кнопка по умолчанию
//     закрывает и отправку клавишей ввода (F8-09);
//   • после входа уводит документ на адрес возврата, только своего
//     происхождения (F8-13): перезагрузка документа нужна, чтобы каждый модуль
//     прочёл новую личность, а не держал прежнюю;
//   • предлагает путь на восстановление доступа — ссылкой «Не получается
//     войти?» с тем же адресом возврата и без адреса почты (приёмка F8-S3, Р4,
//     F8S3-03): путь обещается ровно тогда, когда экран восстановления есть;
//   • к отказу `16` называет следующий шаг — проверить ввод или восстановить
//     доступ, — одним текстом на все причины (#2953): причин отказа экран не
//     различает (Р4);
//   • у флажка второго фактора говорит, когда его отмечать: код без заведённого
//     фактора служба отвергает тем же отказом, что неверный пароль (#2953);
//   • рядом с формой пароля — кнопка входа ключом доступа (приёмка F8-S4, Р1),
//     если у браузера есть интерфейс ключей (Р7). Испытание она просит только
//     нажатием (Р2); пока идёт одна попытка входа, вторая — другим способом —
//     закрыта: две выдачи сессии подряд человеку не нужны. Попытку ведёт
//     `access-key/use-access-key-sign-in.ts`.
//
// Своего правила пароля, формы кода или адреса здесь нет (Р2): незаполненное
// поле называет служба.

/**
 * Когда отмечать флажок второго фактора (#2953): код без заведённого фактора
 * служба отвергает тем же отказом, что неверный пароль, и попытка засчитывается.
 * Различить «фактор есть» до входа экран не вправе — он говорит человеку, когда
 * флажок нужен.
 */
export const SECOND_FACTOR_TOGGLE_HINT =
  "Отмечайте, только если заводили второй фактор в параметрах учётной записи.";

/** Вводы формы входа, которые служба может назвать отказом (таблица полей — в клиенте полосы). */
type LoginField = Extract<RefusalInput, "email" | "password" | "code" | "method">;

function fieldOf(refusal: LaneRefusal | null): LoginField | null {
  const f = refusal?.field;
  return f === "email" || f === "password" || f === "code" || f === "method" ? f : null;
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
  const [sessionUnknown, setSessionUnknown] = useState<LaneRefusal | null>(null);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [withFactor, setWithFactor] = useState(false);
  const [factor, setFactor] = useState<SecondFactorPresentation>(EMPTY_PRESENTATION);
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const [lockedFor, setLockedFor] = useState<number | null>(null);
  const signedIn = useCallback(() => leave(returnTo), [leave, returnTo]);
  const keySignIn = useAccessKeySignIn({ onSignedIn: signedIn, onStart: () => setRefusal(null), blocked: busy });

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

  // Срок из `Retry-After`: до его истечения отправка закрыта. Таймер снимает
  // закрытие, а не повторяет отправку — повторит её человек.
  useEffect(() => {
    if (lockedFor === null) return;
    const t = window.setTimeout(() => setLockedFor(null), lockedFor * 1000);
    return () => window.clearTimeout(t);
  }, [lockedFor]);

  const onSubmit = async () => {
    if (busy || lockedFor !== null || keySignIn.busy) return;
    setBusy(true);
    setRefusal(null);
    keySignIn.clear();
    try {
      await loginLane.login(holder, { email, password, secondFactor: withFactor ? factor : undefined });
      leave(returnTo);
      return; // экран уходит — кнопка остаётся занятой, повторной отправки нет
    } catch (err) {
      const r = laneRefusalOf(err);
      setRefusal(r);
      // Срок — только названный заголовком; нет срока — нет и закрытия: срок не
      // выдумывается (условие C11). Закрытие держит ОБРАБОТЧИК отправки выше, а
      // не только кнопка: клавиша ввода идёт мимо кнопки.
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
  const inputId = (f: Exclude<LoginField, "code" | "method">) => `${id}-${f}`;
  const fieldError = (f: LoginField) => (marked === f ? refusal!.message : null);
  const described = (f: Exclude<LoginField, "code" | "method">) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId(f)), status: "error" as const }
      : {};

  return (
    <CeremonyScreen
      title="Вход в консоль"
      footer={
        <Space direction="vertical" size={8}>
          <Link to={recoveryAddress(returnTo)}>Не получается войти?</Link>
          <Link to={registrationAddress(returnTo)}>Завести учётную запись</Link>
        </Space>
      }
    >
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
      <FormGrid label="Вход в консоль" onSubmit={() => void onSubmit()}>
        <Form.Item label="Адрес электронной почты" htmlFor={inputId("email")}>
          <Input
            id={inputId("email")}
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            {...described("email")}
          />
          <FieldError id={fieldErrorId(inputId("email"))} message={fieldError("email") ?? undefined} />
        </Form.Item>
        <Form.Item label="Пароль" htmlFor={inputId("password")}>
          <Input
            id={inputId("password")}
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            {...described("password")}
          />
          <FieldError id={fieldErrorId(inputId("password"))} message={fieldError("password") ?? undefined} />
        </Form.Item>
        <Form.Item label="Второй фактор">
          <Checkbox
            checked={withFactor}
            onChange={(e) => setWithFactor(e.target.checked)}
            aria-describedby={`${id}-factor-hint`}
          >
            Подтвердить вторым фактором
          </Checkbox>
          <Typography.Paragraph id={`${id}-factor-hint`} type="secondary" style={{ margin: "4px 0 0" }}>
            {SECOND_FACTOR_TOGGLE_HINT}
          </Typography.Paragraph>
        </Form.Item>
        {withFactor && (
          <SecondFactorCodeField
            value={factor}
            onChange={setFactor}
            codeError={fieldError("code")}
            methodError={fieldError("method")}
          />
        )}
        {refusal && (marked === null || (!withFactor && (marked === "code" || marked === "method"))) && (
          <div style={{ marginBottom: 16 }}>
            <LaneRefusalAlert refusal={refusal} context="sign-in" />
          </div>
        )}
        <Button type="primary" htmlType="submit" block loading={busy} disabled={lockedFor !== null || keySignIn.busy}>
          Войти
        </Button>
      </FormGrid>
      <AccessKeySignInButton signIn={keySignIn} blocked={busy} />
    </CeremonyScreen>
  );
}

export default LoginPage;
