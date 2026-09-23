// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useState, type FormEvent, type ReactNode } from "react";
import { Link } from "react-router";
import { Alert, Button, Input, Spin, Typography } from "antd";
import {
  LANE_REASON,
  LaneRefusal,
  loginLane,
  sessionIdentity,
  type Enrollment,
  type SecondFactorPresentation,
  type SecondFactorState,
  type SessionIdentity,
} from "@shared/api/login-lane";
import { requestStepUp } from "@shared/api/step-up";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { EMPTY_PRESENTATION, SecondFactorCodeField } from "@shared/components/molecules/auth/SecondFactorCodeField";
import { StepUpModal } from "@shared/components/molecules/auth/StepUpModal";
import { PAGE_PADDING, PageHead } from "@shared/components/organisms/DetailShell/PageHead";
import { useFormToken } from "@shared/hooks/use-form-token";
import { loginAddress } from "./ceremony-addresses";

// Параметры учётной записи на `/settings` — смена пароля и второй фактор, не
// покидая консоли (приёмка F8, S2).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭКРАН ДЕЛАЕТ
//
//   • учётная запись: адрес и признак его подтверждённости — из ответа края о
//     сессии; действия «подтвердить адрес» нет: производителя письма на
//     посадке нет (F8-17);
//   • смена пароля глаголом службы; отказ — дословно, носитель консоль не
//     трогает ни на успехе (его перевыпускает служба), ни на отказе края
//     (F8-23…F8-25);
//   • второй фактор: состояние — чтением глагола; заведение → материал →
//     подтверждение первым кодом → запасные коды, показанные один раз; снятие и
//     перечеканка — с подтверждением кодом (F8-26…F8-32).
//
// СВЕЖЕСТЬ. На глаголах, требующих свежего предъявления, служба отвечает
// `SESSION_NOT_FRESH`. Экран открывает церемонию повышения и после неё
// ПОВТОРЯЕТ тот же шаг: человек возвращается туда, откуда его остановили, а не
// на панель (F8-29). Повтор один — второй отказ показывается как есть.

/** Выполнить шаг; на `SESSION_NOT_FRESH` — повышение и ОДИН повтор того же шага. */
async function withFreshness<T>(step: () => Promise<T>): Promise<T> {
  try {
    return await step();
  } catch (e) {
    if (e instanceof LaneRefusal && e.reason === LANE_REASON.sessionNotFresh && (await requestStepUp())) {
      return step();
    }
    throw e;
  }
}

function asRefusal(e: unknown): LaneRefusal {
  return e instanceof LaneRefusal ? e : new LaneRefusal(0, null, String(e), null, null, null);
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} style={{ maxWidth: 720, marginBottom: 32 }}>
      <Typography.Title level={4} id={id} style={{ marginTop: 0 }}>
        {title}
      </Typography.Title>
      {children}
    </section>
  );
}

/** Строка формы: подпись слева, ввод справа (канон формы консоли). */
function FieldRow({ id, label, error, children }: { id: string; label: string; error: string | null; children: ReactNode }) {
  return (
    <div style={{ display: "flex", gap: 16, alignItems: "flex-start", marginBottom: 12 }}>
      <label htmlFor={id} style={{ flex: "0 0 200px", paddingTop: 5 }}>
        {label}
      </label>
      <div style={{ flex: "1 1 auto", minWidth: 0 }}>
        {children}
        {error && (
          <div id={`${id}-error`} style={{ color: "var(--kc-error)", marginTop: 4 }}>
            {error}
          </div>
        )}
      </div>
    </div>
  );
}

/** Строка ФАКТА: подпись слева, значение справа; поля ввода нет — подпись не `<label>`. */
function FactRow({ caption, children }: { caption: string; children: ReactNode }) {
  return (
    <div style={{ display: "flex", gap: 16, alignItems: "flex-start", marginBottom: 12 }}>
      <div style={{ flex: "0 0 200px" }}>{caption}</div>
      <div style={{ flex: "1 1 auto", minWidth: 0 }}>{children}</div>
    </div>
  );
}

// ─── пароль ──────────────────────────────────────────────────────────────────

function PasswordSection() {
  const id = useId();
  const holder = useFormToken("password");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const [changed, setChanged] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    setChanged(false);
    try {
      await withFreshness(() => loginLane.changePassword(holder, { currentPassword: current, newPassword: next }));
      setCurrent("");
      setNext("");
      setChanged(true);
    } catch (err) {
      setRefusal(asRefusal(err));
    }
    setBusy(false);
  };

  const marked = refusal?.field === "currentPassword" || refusal?.field === "newPassword" ? refusal.field : null;
  const describe = (f: "currentPassword" | "newPassword", inputId: string) =>
    marked === f
      ? { "aria-invalid": true as const, "aria-describedby": `${inputId}-error`, status: "error" as const }
      : {};

  return (
    <Section title="Пароль">
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <FieldRow id={`${id}-current`} label="Текущий пароль" error={marked === "currentPassword" ? refusal!.message : null}>
          <Input
            id={`${id}-current`}
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            {...describe("currentPassword", `${id}-current`)}
          />
        </FieldRow>
        <FieldRow id={`${id}-next`} label="Новый пароль" error={marked === "newPassword" ? refusal!.message : null}>
          <Input
            id={`${id}-next`}
            type="password"
            autoComplete="new-password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
            {...describe("newPassword", `${id}-next`)}
          />
        </FieldRow>
        {refusal && marked === null && (
          <div style={{ marginBottom: 12 }}>
            <LaneRefusalAlert refusal={refusal} />
          </div>
        )}
        {changed && (
          <p role="status" style={{ margin: "0 0 12px" }}>
            Пароль сменён.
          </p>
        )}
        <Button type="primary" htmlType="submit" loading={busy}>
          Сменить пароль
        </Button>
      </form>
    </Section>
  );
}

// ─── второй фактор ───────────────────────────────────────────────────────────

type FactorView =
  | { kind: "состояние" }
  | { kind: "заведение"; enrollment: Enrollment; code: string }
  | { kind: "коды"; codes: string[]; after: "подтверждение" | "перечеканка" }
  | { kind: "снятие" | "перечеканка"; factor: SecondFactorPresentation };

function stateLine(state: SecondFactorState): string {
  if (state.totp.enrolled) {
    const codes = state.backupCodes;
    return codes
      ? `Второй фактор настроен · запасных кодов осталось ${codes.remaining} из ${codes.total}`
      : "Второй фактор настроен";
  }
  return state.totp.pendingUntil ? "Настройка второго фактора не завершена" : "Второй фактор не настроен";
}

function SecondFactorSection() {
  const id = useId();
  const holder = useFormToken("second-factor");
  const [state, setState] = useState<SecondFactorState | null>(null);
  const [unreadable, setUnreadable] = useState<LaneRefusal | null>(null);
  const [stage, setStage] = useState<FactorView>({ kind: "состояние" });
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  const read = useCallback(async () => {
    try {
      setState(await loginLane.secondFactorState());
      setUnreadable(null);
    } catch (e) {
      setUnreadable(asRefusal(e));
    }
  }, []);

  // Первое чтение — при открытии экрана; дальше состояние перечитывает каждый
  // шаг глагола (`run`), потому что каждый шаг его меняет.
  useEffect(() => {
    let cancelled = false;
    loginLane.secondFactorState().then(
      (s) => {
        if (!cancelled) setState(s);
      },
      (e: unknown) => {
        if (!cancelled) setUnreadable(asRefusal(e));
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  /** Шаг глагола: отказ показывается дословно, состояние перечитывается всегда. */
  const run = async (step: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    try {
      await withFreshness(step);
    } catch (e) {
      setRefusal(asRefusal(e));
    }
    await read();
    setBusy(false);
  };

  const enroll = () =>
    run(async () => {
      const enrollment = await loginLane.enroll(holder);
      setStage({ kind: "заведение", enrollment, code: "" });
    });

  const confirm = (code: string) =>
    run(async () => {
      const confirmed = await loginLane.confirm(holder, code);
      setStage({ kind: "коды", codes: confirmed.backupCodes, after: "подтверждение" });
    });

  const remove = (factor: SecondFactorPresentation) =>
    run(async () => {
      await loginLane.remove(holder, factor);
      setStage({ kind: "состояние" });
    });

  const regenerate = (factor: SecondFactorPresentation) =>
    run(async () => {
      const issued = await loginLane.regenerateBackupCodes(holder, factor);
      setStage({ kind: "коды", codes: issued.backupCodes, after: "перечеканка" });
    });

  const codeError = refusal?.field === "code" ? refusal.message : null;

  let body: ReactNode = null;
  if (stage.kind === "заведение") {
    const firstCodeId = `${id}-first-code`;
    body = (
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void confirm(stage.code);
        }}
        noValidate
      >
        <Typography.Paragraph>
          Добавьте учётную запись в приложение-аутентификатор — секретом или адресом ниже, — и введите код, который оно
          покажет.
        </Typography.Paragraph>
        <FactRow caption="Секрет">
          <Typography.Text code copyable>
            {stage.enrollment.secret}
          </Typography.Text>
        </FactRow>
        <FactRow caption="Адрес для приложения">
          <Typography.Text code copyable style={{ wordBreak: "break-all" }}>
            {stage.enrollment.otpauthUri}
          </Typography.Text>
        </FactRow>
        <FieldRow id={firstCodeId} label="Первый код из приложения" error={codeError}>
          <Input
            id={firstCodeId}
            value={stage.code}
            onChange={(e) => setStage({ ...stage, code: e.target.value })}
            autoComplete="one-time-code"
            inputMode="numeric"
            {...(codeError
              ? { "aria-invalid": true as const, "aria-describedby": `${firstCodeId}-error`, status: "error" as const }
              : {})}
          />
        </FieldRow>
        <Button type="primary" htmlType="submit" loading={busy} style={{ marginRight: 8 }}>
          Подтвердить
        </Button>
        <Button onClick={() => setStage({ kind: "состояние" })} disabled={busy}>
          Отменить
        </Button>
      </form>
    );
  } else if (stage.kind === "коды") {
    body = (
      <>
        <Alert
          type="warning"
          showIcon
          message={
            stage.after === "перечеканка"
              ? "Прежние запасные коды больше недействительны. Новые показываются один раз — сохраните их сейчас."
              : "Запасные коды показываются один раз — сохраните их сейчас."
          }
          style={{ marginBottom: 12 }}
        />
        <ul aria-label="Запасные коды" style={{ fontFamily: "monospace", columns: 2 }}>
          {stage.codes.map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        <Button onClick={() => setStage({ kind: "состояние" })}>Готово</Button>
      </>
    );
  } else if (stage.kind === "снятие" || stage.kind === "перечеканка") {
    const removing = stage.kind === "снятие";
    body = (
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void (removing ? remove(stage.factor) : regenerate(stage.factor));
        }}
        noValidate
      >
        <Typography.Paragraph>
          {removing
            ? "Подтвердите снятие вторым фактором."
            : "Подтвердите выпуск новых запасных кодов вторым фактором — прежние перестанут действовать."}
        </Typography.Paragraph>
        <div style={{ marginBottom: 12 }}>
          <SecondFactorCodeField
            value={stage.factor}
            onChange={(factor) => setStage({ kind: stage.kind, factor })}
            codeError={codeError}
          />
        </div>
        <Button type="primary" danger={removing} htmlType="submit" loading={busy} style={{ marginRight: 8 }}>
          {removing ? "Снять" : "Выпустить коды"}
        </Button>
        <Button onClick={() => setStage({ kind: "состояние" })} disabled={busy}>
          Отменить
        </Button>
      </form>
    );
  } else if (state) {
    body = state.totp.enrolled ? (
      <>
        <Button
          onClick={() => {
            setRefusal(null);
            setStage({ kind: "снятие", factor: EMPTY_PRESENTATION });
          }}
          style={{ marginRight: 8 }}
        >
          Снять второй фактор
        </Button>
        <Button
          onClick={() => {
            setRefusal(null);
            setStage({ kind: "перечеканка", factor: EMPTY_PRESENTATION });
          }}
        >
          Выпустить новые запасные коды
        </Button>
      </>
    ) : (
      <Button type="primary" loading={busy} onClick={() => void enroll()}>
        Настроить второй фактор
      </Button>
    );
  }

  return (
    <Section title="Второй фактор">
      {state === null && unreadable === null && <Spin />}
      {unreadable && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={unreadable} />
          <Button onClick={() => void read()} style={{ marginTop: 8 }}>
            Прочитать снова
          </Button>
        </div>
      )}
      {state && <Typography.Paragraph>{stateLine(state)}</Typography.Paragraph>}
      {refusal && codeError === null && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      {body}
    </Section>
  );
}

// ─── страница ────────────────────────────────────────────────────────────────

export function AccountSettingsPage() {
  const [who, setWho] = useState<SessionIdentity | null | undefined>(undefined);
  useEffect(() => {
    let cancelled = false;
    void sessionIdentity().then((w) => {
      if (!cancelled) setWho(w);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="workbench" style={{ padding: PAGE_PADDING }}>
      {/* Окно повышения живёт рядом с экраном, который его спрашивает: шаги
          свежести зовут его отсюда (`requestStepUp`). */}
      <StepUpModal />
      <PageHead title="Параметры учётной записи" />
      {who === undefined && <Spin />}
      {who === null && (
        <Typography.Paragraph>
          Параметры доступны после входа. <Link to={loginAddress("/settings")}>Войти</Link>
        </Typography.Paragraph>
      )}
      {who && (
        <>
          <Section title="Учётная запись">
            <Typography.Paragraph style={{ marginBottom: 4 }}>{who.user.email}</Typography.Paragraph>
            {who.session && (
              <BoolFact value={who.session.emailVerified} yes="Адрес подтверждён" no="Адрес не подтверждён" />
            )}
          </Section>
          <PasswordSection />
          <SecondFactorSection />
        </>
      )}
    </section>
  );
}

export default AccountSettingsPage;
