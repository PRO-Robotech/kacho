// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useId, useState, type ReactNode } from "react";
import { Alert, Button, Form, Input, Modal, QRCode, Spin, Typography } from "antd";
import {
  type Enrollment,
  type FormTokenHolder,
  type LaneRefusal,
  laneRefusalOf,
  loginLane,
  type SecondFactorState,
} from "@shared/api/login-lane";
import { BoolFact } from "@shared/components/atoms/BoolFact";
import { FACTOR_CODE_LABEL, FactorCodeChoice } from "@shared/components/molecules/auth/FactorCodeChoice";
import { LaneRefusalAlert } from "@shared/components/molecules/auth/LaneRefusalAlert";
import { type Column, ResourceTable } from "@shared/components/organisms/ResourceTable";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { CODE_METHODS, type SecondFactorMethod } from "@shared/lib/step-up-methods";
import { useAnnounce } from "./announcer";
import { DangerDialog } from "./DangerDialog";
import { Moment } from "./moment";
import { SecuritySection } from "./SecuritySection";
import { goToSection } from "./sections";
import { carriesMoments, type SecondFactorRead } from "./use-second-factor";

// Разделы «Второй фактор» и «Запасные коды» экрана «Безопасность» (приёмка
// F8-S7, Р2, Р4, Р6, Р8).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ
//
//   • таблица способов — общей таблицей, строка на способ словаря: имя,
//     состояние, «Добавлен», «Последнее использование», действие (Р2). Ячейка без
//     источника не показывается; «Ещё не использовалось» — сам факт, но только у
//     ответа формы SUA (`carriesMoments`): у прежнего ответа моментов нет вовсе;
//   • заведение кода — окно с QR-кодом адреса `otpauth`, нарисованным В БРАУЗЕРЕ
//     (секрет не уходит ни в один запрос), секретом текстом как его
//     альтернативой и полем «Код из приложения»; затем — окно «Сохраните
//     запасные коды» (Р4);
//   • «Отключить» и «Выпустить новые коды» — опасные действия: диалог называет
//     последствие, подтверждение — кодом в самом диалоге (Р6; служба снимает и
//     перевыпускает только кодом, Ф12 Р9).
//
// СВЕЖЕСТЬ И УРОВЕНЬ. Отказ свежести либо пола ведёт клиент полосы к окну
// подтверждения и ОДНОМУ повтору того же глагола (`submit`, условие C2):
// человек возвращается к своему шагу, а не на панель.

/** Тексты раздела «Второй фактор» (Р6, Р8). */
export const SECOND_FACTOR_TEXT = {
  empty: "Второй фактор не настроен. Он нужен для действий, меняющих доступ.",
  disableFirst: "Коды из приложения и запасные коды перестанут работать.",
  disableLast: "Действия, требующие второго фактора, станут недоступны, пока вы не настроите его снова.",
  regenerate: "Прежние запасные коды перестанут работать сразу.",
  codesAbsent: "Запасные коды появятся после настройки приложения-аутентификатора.",
  codesLow: "Запасные коды заканчиваются — выпустите новые",
} as const;

/** Способы кода, которыми служба примет снятие и перевыпуск сейчас: запасной — пока он есть. */
function presentableCodes(state: SecondFactorState): SecondFactorMethod[] {
  return CODE_METHODS.filter((m) => m === "totp" || (state.backupCodes?.remaining ?? 0) > 0);
}

/** Отказ шага: поле кода — у поля, остальное — у раздела. */
function splitRefusal(e: unknown): { field: string | null; refusal: LaneRefusal | null } {
  const r = laneRefusalOf(e);
  return r.field === "code" ? { field: r.message, refusal: null } : { field: null, refusal: r };
}

// ─── подтверждение кодом в диалоге ───────────────────────────────────────────

interface CodeStep {
  method: SecondFactorMethod;
  code: string;
}

function CodeDialog({
  open,
  title,
  consequences,
  confirmText,
  methods,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  consequences: readonly string[];
  confirmText: string;
  methods: readonly SecondFactorMethod[];
  /** Шаг глагола; отказ — исключением, его разбирает диалог. */
  onConfirm: (step: CodeStep) => Promise<void>;
  onClose: () => void;
}) {
  const [method, setMethod] = useState<SecondFactorMethod>("totp");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [codeError, setCodeError] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  const reset = () => {
    setMethod("totp");
    setCode("");
    setCodeError(null);
    setRefusal(null);
  };

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    setCodeError(null);
    setRefusal(null);
    try {
      await onConfirm({ method, code });
      reset();
    } catch (e) {
      const split = splitRefusal(e);
      setCodeError(split.field);
      setRefusal(split.refusal);
    } finally {
      setBusy(false);
    }
  };

  return (
    <DangerDialog
      open={open}
      title={title}
      consequences={consequences}
      confirmText={confirmText}
      busy={busy}
      onConfirm={() => void confirm()}
      onCancel={() => {
        reset();
        onClose();
      }}
    >
      <FactorCodeChoice
        groupLabel="Способ подтверждения"
        methods={methods}
        value={method}
        onChange={(m) => {
          setMethod(m);
          setCode("");
          setCodeError(null);
        }}
        code={code}
        onCodeChange={setCode}
        codeError={codeError}
        disabled={busy}
      />
      {refusal && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
    </DangerDialog>
  );
}

// ─── заведение кода ──────────────────────────────────────────────────────────

function EnrollDialog({
  enrollment,
  onConfirm,
  onCancel,
}: {
  enrollment: Enrollment | null;
  onConfirm: (code: string) => Promise<void>;
  onCancel: () => void;
}) {
  const id = useId();
  const codeId = `${id}-code`;
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [codeError, setCodeError] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const [copied, setCopied] = useState<"done" | "failed" | null>(null);

  const reset = () => {
    setCode("");
    setCodeError(null);
    setRefusal(null);
    setCopied(null);
  };

  const confirm = async () => {
    if (busy) return;
    setBusy(true);
    setCodeError(null);
    setRefusal(null);
    try {
      await onConfirm(code);
      reset();
    } catch (e) {
      const split = splitRefusal(e);
      setCodeError(split.field);
      setRefusal(split.refusal);
    } finally {
      setBusy(false);
    }
  };

  const copySecret = async () => {
    if (!enrollment) return;
    try {
      await navigator.clipboard.writeText(enrollment.secret);
      setCopied("done");
    } catch {
      setCopied("failed");
    }
  };

  return (
    <Modal
      open={enrollment !== null}
      title="Настроить приложение-аутентификатор"
      onCancel={() => {
        reset();
        onCancel();
      }}
      mask={{ closable: false }}
      footer={[
        <Button
          key="cancel"
          onClick={() => {
            reset();
            onCancel();
          }}
          disabled={busy}
        >
          Отмена
        </Button>,
        <Button key="ok" type="primary" loading={busy} onClick={() => void confirm()}>
          Подтвердить
        </Button>,
      ]}
    >
      {enrollment && (
        <>
          <Typography.Paragraph>
            Отсканируйте QR-код приложением-аутентификатором либо введите в нём секрет, затем введите код, который
            покажет приложение.
          </Typography.Paragraph>
          {/* QR-код рисуется В БРАУЗЕРЕ кодом консоли: адрес `otpauth` с секретом
              не уходит ни в один запрос (Р2). Изображение названо; его доступная
              альтернатива — секрет текстом рядом (Р9). */}
          <div
            role="img"
            aria-label="QR-код для приложения-аутентификатора"
            style={{ display: "inline-block", marginBottom: 12 }}
          >
            <QRCode value={enrollment.otpauthUri} bordered={false} size={176} />
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", marginBottom: 12 }}>
            <span>Секрет:</span>
            <Typography.Text code style={{ wordBreak: "break-all" }}>
              {enrollment.secret}
            </Typography.Text>
            <Button size="small" onClick={() => void copySecret()}>
              Копировать секрет
            </Button>
          </div>
          {copied && (
            <p role="status" style={{ margin: "0 0 12px" }}>
              {copied === "done" ? "Секрет скопирован" : "Скопировать не удалось — введите секрет вручную"}
            </p>
          )}
          <FormGrid label="Подтверждение приложения-аутентификатора" onSubmit={() => void confirm()}>
            <Form.Item label={FACTOR_CODE_LABEL.totp} htmlFor={codeId}>
              <Input
                id={codeId}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                autoComplete="one-time-code"
                inputMode="numeric"
                aria-invalid={codeError ? true : undefined}
                aria-describedby={codeError ? fieldErrorId(codeId) : undefined}
                status={codeError ? "error" : undefined}
              />
              <FieldError id={fieldErrorId(codeId)} message={codeError ?? undefined} />
            </Form.Item>
            {refusal && (
              <div style={{ marginBottom: 12 }}>
                <LaneRefusalAlert refusal={refusal} />
              </div>
            )}
            <button type="submit" hidden aria-hidden tabIndex={-1} />
          </FormGrid>
        </>
      )}
    </Modal>
  );
}

// ─── раздел «Второй фактор» ──────────────────────────────────────────────────

interface MethodRow {
  key: "totp" | "keys" | "codes";
  name: string;
  state: ReactNode;
  added: string | null;
  lastUsed: ReactNode;
  action: ReactNode;
}

const METHOD_COLUMNS: Column<MethodRow>[] = [
  { header: "Способ", cell: (r) => r.name },
  { header: "Состояние", cell: (r) => r.state, width: 220 },
  { header: "Добавлен", cell: (r) => (r.added ? <Moment at={r.added} /> : null), width: 200 },
  { header: "Последнее использование", cell: (r) => r.lastUsed },
  { header: "", cell: (r) => r.action },
];

function keysStateText(state: SecondFactorState, keysCount: number): string {
  if (state.accessKeys?.available === false) return "Выключены на установке";
  return keysCount > 0 ? `Есть: ${keysCount}` : "Нет";
}

export function SecondFactorSection({
  read,
  holder,
  keysCount,
  onAddKey,
  showCodes,
}: {
  read: SecondFactorRead;
  /** Признак формы вида `second-factor` — один на экран: его делят оба раздела. */
  holder: FormTokenHolder;
  /** Число заведённых ключей; `null` — перечень ещё не прочитан. */
  keysCount: number | null;
  /** Открыть добавление ключа (раздел «Ключи доступа»). */
  onAddKey: () => void;
  showCodes: (codes: string[], regenerated: boolean) => void;
}) {
  const announce = useAnnounce();
  const { state, unreadable, reread } = read;
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null);
  const [enrolling, setEnrolling] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);

  const enroll = async () => {
    if (enrolling) return;
    setEnrolling(true);
    setRefusal(null);
    try {
      setEnrollment(await loginLane.enroll(holder));
    } catch (e) {
      setRefusal(laneRefusalOf(e));
    }
    await reread();
    setEnrolling(false);
  };

  const confirm = async (code: string) => {
    try {
      const confirmed = await loginLane.confirm(holder, code);
      setEnrollment(null);
      showCodes(confirmed.backupCodes, false);
      announce("Приложение-аутентификатор настроено");
    } finally {
      await reread();
    }
  };

  const disable = async (step: CodeStep) => {
    try {
      await loginLane.remove(holder, step);
      setDisabling(false);
      announce("Приложение-аутентификатор отключено");
    } finally {
      await reread();
    }
  };

  let body: ReactNode = null;
  if (state && keysCount !== null) {
    const nothing = !state.totp.enrolled && state.totp.pendingUntil === undefined && keysCount === 0;
    if (nothing) {
      // Пустое состояние — на пустом ответе: ни кода, ни ключей (Р8).
      body = (
        <>
          <Typography.Paragraph>{SECOND_FACTOR_TEXT.empty}</Typography.Paragraph>
          <Button type="primary" loading={enrolling} onClick={() => void enroll()} style={{ marginRight: 8 }}>
            Настроить приложение-аутентификатор
          </Button>
          {state.accessKeys?.available !== false && <Button onClick={onAddKey}>Добавить ключ доступа</Button>}
        </>
      );
    } else {
      const moments = carriesMoments(state);
      const rows: MethodRow[] = [
        {
          key: "totp",
          name: "Приложение-аутентификатор",
          state: (
            <BoolFact
              value={state.totp.enrolled}
              yes="Настроено"
              no={state.totp.pendingUntil !== undefined ? "Настройка не завершена" : "Не настроено"}
              yesTone="good"
              noTone="attention"
            />
          ),
          added: state.totp.confirmedAt ?? null,
          lastUsed: state.totp.lastUsedAt ? (
            <Moment at={state.totp.lastUsedAt} />
          ) : state.totp.enrolled && moments ? (
            "Ещё не использовалось"
          ) : null,
          action: state.totp.enrolled ? (
            <Button size="small" onClick={() => setDisabling(true)}>
              Отключить
            </Button>
          ) : (
            <Button
              size="small"
              type="primary"
              loading={enrolling}
              aria-label="Настроить приложение-аутентификатор"
              onClick={() => void enroll()}
            >
              Настроить
            </Button>
          ),
        },
        {
          key: "keys",
          name: "Ключи доступа",
          state: keysStateText(state, keysCount),
          added: null,
          lastUsed: null,
          action: (
            <Button size="small" type="link" onClick={() => goToSection("accessKeys")}>
              Перейти к ключам
            </Button>
          ),
        },
        {
          key: "codes",
          name: "Запасные коды",
          state: state.backupCodes ? `Осталось ${state.backupCodes.remaining} из ${state.backupCodes.total}` : "Нет",
          added: state.backupCodes?.issuedAt ?? null,
          lastUsed: state.backupCodes?.lastUsedAt ? (
            <Moment at={state.backupCodes.lastUsedAt} />
          ) : state.backupCodes && moments ? (
            "Ещё не использовались"
          ) : null,
          action: (
            <Button size="small" type="link" onClick={() => goToSection("backupCodes")}>
              Перейти к кодам
            </Button>
          ),
        },
      ];
      body = <ResourceTable rows={rows} columns={METHOD_COLUMNS} rowKey={(r) => r.key} complete fill={false} />;
    }
  }

  // Вторая фраза — когда после снятия кода способов «2» не останется (Р6): ключ
  // остаётся способом «2» — и тогда её нет. Перечня служба не назвала — фразы
  // нет: утверждать «станут недоступны» консоль не вправе.
  const lastLevelTwo = state?.stepUpMethods !== undefined && !state.stepUpMethods.includes("webauthn");

  return (
    <SecuritySection section="secondFactor">
      {(state === null || keysCount === null) && unreadable === null && <Spin />}
      {unreadable && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={unreadable} />
          <Button onClick={() => void reread()} style={{ marginTop: 8 }}>
            Прочитать снова
          </Button>
        </div>
      )}
      {refusal && (
        <div style={{ marginBottom: 12 }}>
          <LaneRefusalAlert refusal={refusal} />
        </div>
      )}
      {body}
      <EnrollDialog enrollment={enrollment} onConfirm={confirm} onCancel={() => setEnrollment(null)} />
      {state && (
        <CodeDialog
          open={disabling}
          title="Отключить приложение-аутентификатор"
          consequences={
            lastLevelTwo
              ? [SECOND_FACTOR_TEXT.disableFirst, SECOND_FACTOR_TEXT.disableLast]
              : [SECOND_FACTOR_TEXT.disableFirst]
          }
          confirmText="Отключить"
          methods={presentableCodes(state)}
          onConfirm={disable}
          onClose={() => setDisabling(false)}
        />
      )}
    </SecuritySection>
  );
}

// ─── раздел «Запасные коды» ──────────────────────────────────────────────────

interface CodesProperty {
  name: string;
  value: ReactNode;
}

const PROPERTY_COLUMNS: Column<CodesProperty>[] = [
  { header: "Свойство", cell: (r) => r.name },
  { header: "Значение", cell: (r) => r.value },
];

export function BackupCodesSection({
  read,
  holder,
  showCodes,
}: {
  read: SecondFactorRead;
  holder: FormTokenHolder;
  showCodes: (codes: string[], regenerated: boolean) => void;
}) {
  const announce = useAnnounce();
  const { state, reread } = read;
  const [regenerating, setRegenerating] = useState(false);

  const regenerate = async (step: CodeStep) => {
    try {
      const issued = await loginLane.regenerateBackupCodes(holder, step);
      setRegenerating(false);
      showCodes(issued.backupCodes, true);
      announce("Запасные коды выпущены");
    } finally {
      await reread();
    }
  };

  let body: ReactNode = null;
  if (state && !state.totp.enrolled) {
    body = <Typography.Paragraph>{SECOND_FACTOR_TEXT.codesAbsent}</Typography.Paragraph>;
  } else if (state?.backupCodes) {
    const codes = state.backupCodes;
    const properties: CodesProperty[] = [];
    if (codes.issuedAt) properties.push({ name: "Выпущены", value: <Moment at={codes.issuedAt} /> });
    if (codes.lastUsedAt) {
      properties.push({ name: "Последнее использование", value: <Moment at={codes.lastUsedAt} /> });
    } else if (carriesMoments(state)) {
      properties.push({ name: "Последнее использование", value: "Ещё не использовались" });
    }
    body = (
      <>
        <Typography.Paragraph>{`Осталось ${codes.remaining} из ${codes.total}`}</Typography.Paragraph>
        {codes.remaining <= 2 && (
          <Alert type="warning" showIcon message={SECOND_FACTOR_TEXT.codesLow} style={{ marginBottom: 12 }} />
        )}
        {properties.length > 0 && (
          <div style={{ marginBottom: 12 }}>
            <ResourceTable rows={properties} columns={PROPERTY_COLUMNS} rowKey={(r) => r.name} complete fill={false} />
          </div>
        )}
        <Button onClick={() => setRegenerating(true)}>Выпустить новые коды</Button>
        <CodeDialog
          open={regenerating}
          title="Выпустить новые коды"
          consequences={[SECOND_FACTOR_TEXT.regenerate]}
          confirmText="Выпустить"
          methods={presentableCodes(state)}
          onConfirm={regenerate}
          onClose={() => setRegenerating(false)}
        />
      </>
    );
  }

  return (
    <SecuritySection section="backupCodes">
      {state === null && <Spin />}
      {body}
    </SecuritySection>
  );
}
