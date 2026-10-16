// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { Alert, Button, Form, Input, Modal, Spin, Typography } from "antd";
import {
  LANE_REASON,
  type LaneRefusal,
  laneRefusalOf,
  loginLane,
  sessionIdentity,
  type SecondFactorState,
  type SessionAnswer,
} from "@shared/api/login-lane";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { StepUpModal } from "@shared/components/molecules/auth/StepUpModal";
import { PageHead } from "@shared/components/organisms/DetailShell/PageHead";
import { PageFrame } from "@shared/components/organisms/PageFrame";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FORM_LABEL_WIDTH, FormGrid } from "@shared/components/organisms/form/FormGrid";
import { useFormToken } from "@shared/hooks/use-form-token";
import { forgetPrincipalState } from "@shared/lib/principal-state";
import { AccessKeysSection } from "./access-key";
import { EmailChangeAction } from "./email-change";
import { OwnSessionsSection } from "./own-sessions";
import { ACCOUNT_SETTINGS_ADDRESS, loginAddress } from "./ceremony-addresses";
import { removeSelf, SelfRemovalRefused, SelfRemovalUnknown } from "@shared/api/self-removal";
import { SecurityAnnouncer, useAnnounce } from "./security/announcer";
import { SavedCodesDialog } from "./security/SavedCodesDialog";
import { BackupCodesSection, SecondFactorSection, type KeysCount } from "./security/SecondFactorSections";
import { SecuritySection } from "./security/SecuritySection";
import { NAVIGATED_SECTIONS, SECURITY_SECTION, goToSection } from "./security/sections";
import { methodsInWords, useSecondFactorState } from "./security/use-second-factor";

// Экран «Безопасность» на `/settings` — один экран личного кабинета о защите
// учётной записи (приёмка F8-S7, Р1; адрес — приёмка F8, Р1, новой ручки нет).
//
// ─────────────────────────────────────────────────────────────────────────────
// РАЗДЕЛЫ — В ПОРЯДКЕ ЗАДАЧ ЧЕЛОВЕКА (Р1)
//
//   1. Сводка — вход (адрес и его подтверждённость), чем подтверждаются
//      действия, сколько активных сессий; без второго фактора — рекомендация и
//      «Настроить»;
//   2. Способ входа — адрес почты и его смена (F8-S6), пароль (F8 Р13 без
//      изменения), вход ключом доступа;
//   3. Второй фактор — таблица способов (Р2);
//   4. Ключи доступа — таблица ключей (Р3, Р7);
//   5. Запасные коды — остаток, даты, перевыпуск (Р4);
//   6. Активные сессии — таблица своих сессий (F8-S5, вид — Р3);
//   7. Удаление учётной записи (#3186) — необратимое, ниже задач Р1 и вне
//      навигации.
//
// Навигация «Разделы страницы» в шапке — ссылки на пять разделов 2–6: сводка
// стоит первой под шапкой и видна без перехода. Прокрутка одна (`PageFrame`):
// вкладок и отдельных адресов нет — они скрыли бы сводку и не дали увидеть все
// способы разом.
//
// ОДНО ЧТЕНИЕ СОСТОЯНИЯ СПОСОБОВ на экран (`useSecondFactorState`): сводка,
// разделы и диалоги говорят об одном ответе службы и разойтись не могут.
//
// СВЕЖЕСТЬ И УРОВЕНЬ. На глаголах, требующих свежего предъявления либо пола
// «2», служба отвечает отказом, и клиент открывает окно подтверждения
// (`StepUpModal`, F8-S7 Р5) и после него ПОВТОРЯЕТ тот же глагол (одно решение
// на отказ — `refusalActionOf`, условие C2): человек возвращается туда, откуда
// его остановили.
//
// «СПРОСИТЬ НЕ УДАЛОСЬ» — НЕ «ВЫ НЕ ВОШЛИ» (условие C6). Край не ответил о сессии
// по существу — экран называет это и даёт спросить снова, а не рисует «доступны
// после входа»: человек с живой сессией иначе уходил бы входить заново.

const asRefusal = laneRefusalOf;

/** Рекомендация сводки без способов подтверждения (Р1). */
export const SUMMARY_ADVICE = "Включите второй фактор: без него действия, меняющие доступ, недоступны";

/**
 * Отметка поля, названного отказом службы: ввод ссылается на сообщение у поля
 * (`FieldError`) тем же именем, которым оно выведено из имени ввода.
 */
function markedBy(inputId: string, error: string | null) {
  return error
    ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId), status: "error" as const }
    : {};
}

// Геометрия формы здесь — ОБЩАЯ сетка (`FormGrid`: имя слева, ввод справа), а
// отказ у поля — общий `FieldError`. Прежде страница выписывала обе копией:
// свою колонку подписи с числом ширины и свою разметку отказа (#1274, круг 1
// ревью).

// ─── пароль ──────────────────────────────────────────────────────────────────

function ChangePasswordForm({ onSubmitted, onChanged }: { onSubmitted: () => void; onChanged: () => void }) {
  const id = useId();
  const holder = useFormToken("password");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  const onSubmit = async () => {
    if (busy) return;
    onSubmitted();
    setBusy(true);
    setRefusal(null);
    try {
      await loginLane.changePassword(holder, { currentPassword: current, newPassword: next });
      setCurrent("");
      setNext("");
      onChanged();
    } catch (err) {
      setRefusal(asRefusal(err));
    }
    setBusy(false);
  };

  const marked = refusal?.field === "currentPassword" || refusal?.field === "newPassword" ? refusal.field : null;
  const errorOf = (f: "currentPassword" | "newPassword") => (marked === f ? refusal!.message : null);
  const currentId = `${id}-current`;
  const nextId = `${id}-next`;

  return (
    <FormGrid onSubmit={() => void onSubmit()}>
      <Form.Item label="Текущий пароль" htmlFor={currentId}>
        <Input
          id={currentId}
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          {...markedBy(currentId, errorOf("currentPassword"))}
        />
        <FieldError id={fieldErrorId(currentId)} message={errorOf("currentPassword") ?? undefined} />
      </Form.Item>
      <Form.Item label="Новый пароль" htmlFor={nextId}>
        <Input
          id={nextId}
          type="password"
          autoComplete="new-password"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          {...markedBy(nextId, errorOf("newPassword"))}
        />
        <FieldError id={fieldErrorId(nextId)} message={errorOf("newPassword") ?? undefined} />
      </Form.Item>
      {refusal && marked === null && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      <Button type="primary" htmlType="submit" loading={busy}>
        Сменить пароль
      </Button>
    </FormGrid>
  );
}

/**
 * Заведение первого пароля (Р13). Признак вида `password-enroll` добывается при
 * открытии ЭТОЙ формы, а не экрана: человеку с паролем она не нужна, и экран
 * параметров не просит признак, которым не воспользуется.
 */
function EnrollPasswordForm({
  onEnrolled,
  onAlreadySet,
  onCancel,
}: {
  onEnrolled: () => void;
  onAlreadySet: (refusal: LaneRefusal) => void;
  onCancel: () => void;
}) {
  const id = useId();
  const holder = useFormToken("password-enroll");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  const onSubmit = async () => {
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    try {
      await loginLane.enrollPassword(holder, { newPassword: next });
      onEnrolled();
      return;
    } catch (err) {
      const r = asRefusal(err);
      // «Пароль уже есть» — путь к смене: раздел возвращается к форме смены и
      // называет отказ службы там дословно (F8-63).
      if (r.reason === LANE_REASON.passwordAlreadySet) {
        onAlreadySet(r);
        return;
      }
      setRefusal(r);
    }
    setBusy(false);
  };

  const nextId = `${id}-next`;
  const nextError = refusal?.field === "newPassword" ? refusal.message : null;

  return (
    <FormGrid onSubmit={() => void onSubmit()}>
      <Form.Item label="Новый пароль" htmlFor={nextId}>
        <Input
          id={nextId}
          type="password"
          autoComplete="new-password"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          {...markedBy(nextId, nextError)}
        />
        <FieldError id={fieldErrorId(nextId)} message={nextError ?? undefined} />
      </Form.Item>
      {refusal && nextError === null && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      <Button type="primary" htmlType="submit" loading={busy} style={{ marginRight: 8 }}>
        Завести пароль
      </Button>
      <Button onClick={onCancel} disabled={busy}>
        Отменить
      </Button>
    </FormGrid>
  );
}

/**
 * Пароль — действие раздела «Способ входа»: формы смены и заведения открываются
 * окном, а не стоят на экране (приёмка F8-S7, Р1, Р9: на экране — состояние и
 * действия, ввод — в окне действия). Форма «завести пароль» по-прежнему
 * выбирается человеком (F8, Р13 без изменения).
 */
function PasswordAction() {
  const announce = useAnnounce();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"смена" | "заведение">("смена");
  const [alreadySet, setAlreadySet] = useState<LaneRefusal | null>(null);
  const close = () => {
    setOpen(false);
    setMode("смена");
    setAlreadySet(null);
  };

  return (
    <>
      <Button
        onClick={() => {
          setAlreadySet(null);
          setMode("смена");
          setOpen(true);
        }}
      >
        Сменить пароль
      </Button>
      <Modal open={open} title="Пароль" onCancel={close} mask={{ closable: false }} footer={null}>
        {mode === "заведение" ? (
          <EnrollPasswordForm
            onEnrolled={() => {
              close();
              announce("Пароль заведён.");
            }}
            onAlreadySet={(r) => {
              setAlreadySet(r);
              setMode("смена");
            }}
            onCancel={() => setMode("смена")}
          />
        ) : (
          <>
            {alreadySet && (
              <div style={{ marginBottom: 12 }}>
                <LaneRefusalAlert refusal={alreadySet} />
              </div>
            )}
            <ChangePasswordForm
              onSubmitted={() => setAlreadySet(null)}
              onChanged={() => {
                close();
                announce("Пароль сменён.");
              }}
            />
            <Button
              type="link"
              style={{ paddingLeft: 0, marginTop: 8 }}
              onClick={() => {
                setAlreadySet(null);
                setMode("заведение");
              }}
            >
              У меня нет пароля — завести
            </Button>
          </>
        )}
      </Modal>
    </>
  );
}

// ─── сводка и способ входа ───────────────────────────────────────────────────

function FactLine({ children }: { children: ReactNode }) {
  return <div style={{ marginBottom: 6 }}>{children}</div>;
}

function SummarySection({
  email,
  emailVerified,
  state,
  sessions,
}: {
  email: string;
  emailVerified: boolean | undefined;
  state: SecondFactorState | null;
  sessions: number | null;
}) {
  const methods = state?.stepUpMethods;
  return (
    <SecuritySection section="summary">
      <FactLine>
        {`Вход: ${email}`}{" "}
        {typeof emailVerified === "boolean" && (
          <BoolFact
            value={emailVerified}
            yes="Адрес подтверждён"
            no="Адрес не подтверждён"
            yesTone="good"
            noTone="attention"
          />
        )}
      </FactLine>
      {methods !== undefined && <FactLine>{`Подтверждение действий: ${methodsInWords(methods)}`}</FactLine>}
      {state?.accessKeys?.available === false && <FactLine>Вход ключом доступа: Выключены на установке</FactLine>}
      {sessions !== null && <FactLine>{`Активные сессии: ${sessions}`}</FactLine>}
      {methods !== undefined && methods.length === 0 && (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 12,
            flexWrap: "wrap",
            marginTop: 8,
            padding: "8px 12px",
            border: "1px solid var(--status-warn-fg)",
            borderRadius: 6,
          }}
        >
          <span>{SUMMARY_ADVICE}</span>
          <Button size="small" type="primary" onClick={() => goToSection("secondFactor")}>
            Настроить
          </Button>
        </div>
      )}
    </SecuritySection>
  );
}

/** Строка раздела «Способ входа»: имя слева колонкой формы, факт и действие справа (канон формы). */
function SignInRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div style={{ display: "flex", gap: 16, alignItems: "baseline", flexWrap: "wrap", marginBottom: 12 }}>
      <div style={{ flex: `0 0 ${FORM_LABEL_WIDTH}px`, color: "var(--kc-text-secondary)" }}>{label}</div>
      <div style={{ flex: "1 1 280px", minWidth: 0 }}>{children}</div>
    </div>
  );
}

function SignInSection({
  email,
  emailVerified,
  state,
  keysCount,
  onChanged,
  leave,
}: {
  email: string;
  emailVerified: boolean | undefined;
  state: SecondFactorState | null;
  keysCount: KeysCount;
  onChanged: () => Promise<void>;
  leave?: (to: string) => void;
}) {
  const off = state?.accessKeys?.available === false;
  return (
    <SecuritySection section="signIn">
      <SignInRow label="Адрес почты">
        <div>{email}</div>
        {typeof emailVerified === "boolean" && (
          <BoolFact value={emailVerified} yes="Адрес подтверждён" no="Адрес не подтверждён" />
        )}
        <div>
          <EmailChangeAction onChanged={onChanged} leave={leave} />
        </div>
      </SignInRow>
      <SignInRow label="Пароль">
        <PasswordAction />
      </SignInRow>
      {(off || typeof keysCount === "number") && (
        <SignInRow label="Вход ключом доступа">
          {off ? (
            "Выключены на установке"
          ) : (
            <BoolFact
              value={(typeof keysCount === "number" ? keysCount : 0) > 0}
              yes={`Доступен · ключей: ${typeof keysCount === "number" ? keysCount : 0}`}
              no="Не настроен"
              yesTone="good"
            />
          )}
        </SignInRow>
      )}
    </SecuritySection>
  );
}

function SectionsNav() {
  return (
    <nav aria-label="Разделы страницы" style={{ marginBottom: 12 }}>
      <ul style={{ display: "flex", flexWrap: "wrap", gap: 16, listStyle: "none", margin: 0, padding: 0 }}>
        {NAVIGATED_SECTIONS.map((key) => (
          <li key={key}>
            <a
              href={`#${SECURITY_SECTION[key].id}`}
              onClick={(e) => {
                e.preventDefault();
                goToSection(key);
              }}
            >
              {SECURITY_SECTION[key].title}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

function SecurityScreen({
  who,
  reread,
  leave,
}: {
  who: Extract<SessionAnswer, { kind: "present" }>;
  reread: () => Promise<void>;
  leave?: (to: string) => void;
}) {
  const factor = useSecondFactorState();
  const factorHolder = useFormToken("second-factor");
  const [keysCount, setKeysCount] = useState<KeysCount>(null);
  const [sessions, setSessions] = useState<number | null>(null);
  const [addKey, setAddKey] = useState(0);
  const [codes, setCodes] = useState<{ codes: string[]; regenerated: boolean } | null>(null);
  const showCodes = useCallback((c: string[], regenerated: boolean) => setCodes({ codes: c, regenerated }), []);
  const emailVerified = who.session?.emailVerified;
  const { reread: rereadFactor } = factor;
  const keysChanged = useCallback(() => void rereadFactor(), [rereadFactor]);

  return (
    <>
      <SummarySection email={who.user.email} emailVerified={emailVerified} state={factor.state} sessions={sessions} />
      <SignInSection
        email={who.user.email}
        emailVerified={emailVerified}
        state={factor.state}
        keysCount={keysCount}
        onChanged={reread}
        leave={leave}
      />
      <SecondFactorSection
        read={factor}
        holder={factorHolder}
        keysCount={keysCount}
        onAddKey={() => {
          goToSection("accessKeys");
          setAddKey((n) => n + 1);
        }}
        showCodes={showCodes}
      />
      <AccessKeysSection
        userId={who.user.id}
        factor={factor.state}
        onCount={setKeysCount}
        addRequested={addKey}
        onChanged={keysChanged}
      />
      <BackupCodesSection read={factor} holder={factorHolder} showCodes={showCodes} />
      <OwnSessionsSection onCount={setSessions} />
      <DeleteSelfSection user={who.user} leave={leave ?? ((to) => window.location.replace(to))} />
      <SavedCodesDialog
        codes={codes?.codes ?? null}
        regenerated={codes?.regenerated ?? false}
        onDone={() => setCodes(null)}
      />
    </>
  );
}

// ─── удаление своей учётной записи (#3186) ────────────────────────────────────
//
// Служба объявляет самоудаление: личность стирается во всех аккаунтах сразу,
// необратимо, после удаления аккаунтов, которыми человек владеет. Экран ведёт к
// нему: необратимое подтверждается вводом своего адреса (не одним щелчком), id
// личности — из ответа «кто я», уровень сессии поднимает окно повышения по
// отказу края, отказ «сперва аккаунты» назван шагом, успех уводит ДОКУМЕНТОМ на
// вход — каркас прежней сессии не остаётся во вкладке.

/** Шаг к отказу «человек ещё владеет аккаунтом» — порядок службы: аккаунт, затем личность. */
export const SELF_REMOVAL_OWNS_ACCOUNTS_STEP =
  "Сначала удалите аккаунты, которыми вы владеете (раздел «Аккаунты»), — затем удалите учётную запись.";

function DeleteSelfSection({
  user,
  leave,
}: {
  user: { id: string; email: string };
  leave: (to: string) => void;
}) {
  const inputId = useId();
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<{ text: string; step: string | null } | null>(null);
  const matches = typed.trim().toLowerCase() === user.email.toLowerCase();

  const remove = async () => {
    if (busy || !matches) return;
    setBusy(true);
    setFailure(null);
    try {
      await removeSelf(user.id);
      forgetPrincipalState();
      leave(loginAddress());
      return; // экран уходит — кнопка остаётся занятой, повтора нет
    } catch (e) {
      setOpen(false);
      if (e instanceof SelfRemovalRefused) {
        setFailure({ text: e.message, step: e.ownsAccounts ? SELF_REMOVAL_OWNS_ACCOUNTS_STEP : null });
      } else if (e instanceof SelfRemovalUnknown) {
        setFailure({ text: e.message, step: null });
      } else {
        setFailure({ text: "Запрос не дошёл до службы. Повторите удаление позже.", step: null });
      }
    }
    setBusy(false);
  };

  return (
    <SecuritySection section="deleteSelf">
      <Typography.Paragraph>
        Учётная запись удаляется во всех аккаунтах сразу и навсегда: вместе с ней уходят сессии, способы входа, ключи
        доступа и членства. Удалить её можно, когда не осталось аккаунтов, которыми вы владеете.
      </Typography.Paragraph>
      {failure && (
        <div style={{ marginBottom: 12 }}>
          <Alert type="error" showIcon message={failure.text} />
          {failure.step !== null && (
            <Typography.Paragraph data-testid="self-removal-next-step" style={{ margin: "8px 0 0" }}>
              {failure.step}
            </Typography.Paragraph>
          )}
        </div>
      )}
      <Button
        danger
        onClick={() => {
          setTyped("");
          setOpen(true);
        }}
      >
        Удалить учётную запись
      </Button>
      <Modal
        open={open}
        title="Удалить учётную запись навсегда?"
        onCancel={() => setOpen(false)}
        footer={[
          <Button key="cancel" onClick={() => setOpen(false)}>
            Отмена
          </Button>,
          <Button key="remove" danger type="primary" loading={busy} disabled={!matches} onClick={() => void remove()}>
            Удалить навсегда
          </Button>,
        ]}
      >
        <Typography.Paragraph>Действие необратимо. Чтобы подтвердить, введите адрес почты учётной записи.</Typography.Paragraph>
        <Form layout="vertical">
          <Form.Item label="Адрес почты учётной записи" htmlFor={inputId}>
            <Input id={inputId} autoComplete="off" value={typed} onChange={(e) => setTyped(e.target.value)} />
          </Form.Item>
        </Form>
      </Modal>
    </SecuritySection>
  );
}

// ─── страница ────────────────────────────────────────────────────────────────

export function AccountSettingsPage({ leave }: { leave?: (to: string) => void } = {}) {
  const [who, setWho] = useState<SessionAnswer | undefined>(undefined);
  // Каждый вопрос «кто вошёл» — свой номер: эффект задаёт вопрос и принимает
  // ответ только на него, а «Проверить снова» сбрасывает показанное и заводит
  // следующий номер — в обработчике нажатия, а не в эффекте.
  const [asked, setAsked] = useState(0);
  const askAgain = () => {
    setWho(undefined);
    setAsked((n) => n + 1);
  };
  useEffect(() => {
    let cancelled = false;
    void sessionIdentity().then((w) => {
      if (!cancelled) setWho(w);
    });
    return () => {
      cancelled = true;
    };
  }, [asked]);
  // Переспросить «кто я», НЕ сбрасывая показанного: разделы экрана остаются на
  // месте, меняется только ответ края (смена адреса, F8-S6, Р3).
  const reread = useCallback(async () => {
    setWho(await sessionIdentity());
  }, []);

  return (
    // Шапка стоит, содержимое прокручивается под ней — одной областью
    // (`PageFrame`): рабочая область каркаса сама не прокручивается.
    <PageFrame
      head={
        <>
          <PageHead title="Безопасность" level={1} divider={who?.kind !== "present"} />
          {who?.kind === "present" && <SectionsNav />}
        </>
      }
    >
      <SecurityAnnouncer>
        {/* Окно подтверждения живёт рядом с экраном, который его спрашивает:
            шаги свежести и уровня зовут его отсюда (`requestStepUp`). */}
        <StepUpModal leave={leave} />
        {who === undefined && <Spin />}
        {who?.kind === "absent" && (
          <Typography.Paragraph>
            Параметры безопасности доступны после входа. <Link to={loginAddress(ACCOUNT_SETTINGS_ADDRESS)}>Войти</Link>
          </Typography.Paragraph>
        )}
        {who?.kind === "unknown" && (
          <div style={{ maxWidth: 720, marginBottom: 12 }}>
            <LaneRefusalAlert refusal={who.refusal} />
            <Button onClick={askAgain} style={{ marginTop: 8 }}>
              Проверить снова
            </Button>
          </div>
        )}
        {who?.kind === "present" && <SecurityScreen who={who} reread={reread} leave={leave} />}
      </SecurityAnnouncer>
    </PageFrame>
  );
}

export default AccountSettingsPage;
