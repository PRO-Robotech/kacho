// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Alert, Button, Form, Input, Modal, Spin, Typography } from "antd";
import { KeyOutlined } from "@ant-design/icons";
import { ApiError } from "@shared/api/client";
import { AccessKeyEncodingError, registrationCredentialOf, registrationRequestOf } from "@shared/api/access-key";
import { NOT_BY_SUBSTANCE_TEXT } from "@shared/api/login-lane";
import type { SecondFactorState } from "@shared/api/login-lane";
import { type Column, ResourceTable } from "@shared/components/organisms/ResourceTable";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import {
  genderOfLabel,
  mutationFailureText,
  mutationSuccessText,
  type MutationSubject,
} from "@shared/lib/mutation-signal";
import { useAnnounce } from "../security/announcer";
import { DangerDialog } from "../security/DangerDialog";
import { Moment } from "../security/moment";
import { SecuritySection } from "../security/SecuritySection";
import {
  AccessKeyOperationFailed,
  AccessKeyOperationUnknown,
  accessKeysClient,
  type AccessKeyRecord,
} from "@shared/api/access-keys";

// Раздел «Ключи доступа» экрана «Безопасность» (приёмка F8, ред. 12, S4,
// группа L; Р11 в редакции приёмки F8-S7, Р3, Р6, Р7, Р8).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО РАЗДЕЛ ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ
//
//   • перечень — глаголом Ф7, дочитанный до конца, — ОБЩЕЙ таблицей: «Имя» (с
//     описанием второй строкой), «Добавлен», «Последнее использование» либо «Ещё
//     не использовался» — пустое значение означает «не использовался» (Ф7), —
//     и «Удалить» у каждого ключа (F8-S7, Р3). Своей разметки перечня нет;
//   • заведение — окно «Добавить ключ доступа»: выдача испытания →
//     `navigator.credentials.create` над ним → приём результата → `done`
//     операции → перечень перечитан. Параметры церемонии и результат переносит
//     кодек (`api/access-key.ts`) без изменения;
//   • снятие — у КАЖДОГО ключа, и у последнего тоже: правило «последний способ
//     входа» судит служба (Ф7-26), и раздел показывает её отказ дословно — из
//     ответа снятия либо из `error` операции; ключ остаётся (F8-58, F8-69).
//     Удаление спрашивает подтверждения диалогом, называющим ключ и последствие
//     (F8-S7, Р6); отмена не выпускает ничего (F8-60);
//   • ключи ВЫКЛЮЧЕНЫ на установке (`accessKeys.available = false`, SUA Р7) —
//     состоянием раздела, а не отказом в конце церемонии (F8-S7, Р7, kacho#3229
//     п. 4): добавления нет, и выдачи испытания раздел не выпускает; заведённые
//     ключи показаны с «Удалить». У человека, у которого подтвердить снятие
//     нечем (`stepUpMethods` пуст), «Удалить» не ведёт в окно, замкнутое в круг,
//     а называет путь — сброс ключей администратором;
//   • имя и описание раздел НЕ проверяет: форму судит служба, и её нарушение
//     поля стоит у поля (`BadRequest.fieldViolations`), остальное — у раздела
//     (F8-70, F8-71); введённое при любом отказе сохранено.
//
// ИСХОД — областью `role="status"` экрана (Р9), тексты — `mutation-signal.ts`;
// отказ стоит у раздела (`role="alert"`).
//
// ОТКАЗ НАЗЫВАЕТ ШАГ И НЕ РАСКРЫВАЕТ КЛАСС. Текст службы — дословно; над ним
// раздел называет свой шаг («Ключ не добавлен»). Отказ церемонии браузером — ОДИН
// текст консоли на все причины (как F8-S4 Р5): имя исключения браузера и его
// сообщение (в них адрес и доверяющая сторона) на экран и в журнал не идут.
// Материал церемонии — испытание, удостоверение, клиентские данные — живёт в
// замыкании одной попытки и не попадает ни в состояние экрана, ни в журнал.

/** Текст консоли на отказ церемонии браузером (Р11) — один на все причины. */
export const KEY_CEREMONY_BROWSER_REFUSED = "Добавление ключа прервано в браузере — повторите";

/** Браузер без интерфейса ключей (F8-53): добавить нельзя, перечень и снятие — можно. */
export const KEY_CEREMONY_UNSUPPORTED = "Этот браузер не работает с ключами доступа — добавьте ключ в другом браузере";

/** Запрос не получил ответа вовсе — причины консоль не знает и не выдумывает. */
const NO_CONNECTION = "Запрос не дошёл до службы: нет соединения";

/** Операция не дала исхода в срок либо ответ без операции: исход неизвестен. */
const OUTCOME_UNKNOWN = "Служба не назвала исход операции — перечень перечитан";

/** Есть ли у браузера интерфейс СОЗДАНИЯ ключа. */
export function accessKeyCreationSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof (window as unknown as { PublicKeyCredential?: unknown }).PublicKeyCredential === "function" &&
    typeof navigator !== "undefined" &&
    typeof navigator.credentials?.create === "function"
  );
}

type KeyField = "name" | "description";

/** Подлежащее сигнала: ключ доступа по имени в кавычках (пустое имя — по идентификатору). */
const KEY_LABEL = "Ключ доступа";
function keySubject(name: string): MutationSubject {
  const n = name.trim();
  return { label: KEY_LABEL, gender: genderOfLabel(KEY_LABEL) ?? "m", name: n ? `«${n}»` : null };
}

/** Тексты раздела (F8-S7, Р3, Р6, Р7, Р8) — дословно. */
export const ACCESS_KEYS_TEXT = {
  empty: "Ключей доступа нет. Входите и подтверждайте действия без пароля и кода.",
  turnedOff: "Ключи доступа на этой установке выключены администратором",
  removeConsequence: "Войти этим ключом будет нельзя.",
  removeLastLevelTwo: "Действия, требующие второго фактора, станут недоступны.",
  removeImpossible:
    "Удалить ключ сейчас нельзя: нужен второй фактор, а ключи на этой установке выключены. Попросите администратора установки сбросить ваши ключи доступа.",
} as const;

/** Имя ключа на экране: имя, а без него — идентификатор. */
function keyName(record: AccessKeyRecord): string {
  return record.name || record.id;
}

/** Отказ шага раздела: текст для человека и — если служба назвала — поле. */
interface KeysRefusal {
  step: string;
  text: string;
  field: KeyField | null;
}

/** Нарушение поля из `details` отказа (`google.rpc.BadRequest`, N36). */
function fieldViolationOf(details: unknown): { field: KeyField; description: string } | null {
  if (!Array.isArray(details)) return null;
  for (const d of details) {
    if (!d || typeof d !== "object") continue;
    const violations = (d as { fieldViolations?: unknown }).fieldViolations;
    if (!Array.isArray(violations)) continue;
    for (const v of violations) {
      const field = (v as { field?: unknown })?.field;
      const description = (v as { description?: unknown })?.description;
      if ((field === "name" || field === "description") && typeof description === "string" && description !== "") {
        return { field, description };
      }
    }
  }
  return null;
}

function refusalOf(e: unknown, step: string): KeysRefusal {
  if (e instanceof ApiError) {
    const violation = fieldViolationOf(e.details);
    if (violation) return { step, text: violation.description, field: violation.field };
    return { step, text: e.message || NOT_BY_SUBSTANCE_TEXT, field: null };
  }
  if (e instanceof AccessKeyOperationFailed) return { step, text: e.message, field: null };
  if (e instanceof AccessKeyOperationUnknown) return { step, text: OUTCOME_UNKNOWN, field: null };
  if (e instanceof AccessKeyEncodingError) return { step, text: NOT_BY_SUBSTANCE_TEXT, field: null };
  // Иное — обращение без ответа (`fetch` отклонён): текст исключения не показывается.
  return { step, text: NO_CONNECTION, field: null };
}

/** Исход чтения перечня: ключи либо отказ — третьего не бывает. */
type KeysRead = { keys: AccessKeyRecord[] } | { refusal: KeysRefusal };

async function readKeys(userId: string): Promise<KeysRead> {
  try {
    return { keys: await accessKeysClient.list(userId) };
  } catch (e) {
    return { refusal: refusalOf(e, "Перечень ключей не прочитан.") };
  }
}

/** Шаг — словом консоли; под ним текст отказа ДОСЛОВНО и ни словом больше. */
function RefusalNotice({ refusal }: { refusal: KeysRefusal }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <Typography.Paragraph style={{ marginBottom: 4 }}>{refusal.step}</Typography.Paragraph>
      <Alert type="error" showIcon message={refusal.text} />
    </div>
  );
}

const KEY_COLUMNS = (onRemove: (r: AccessKeyRecord) => void, busy: string | null): Column<AccessKeyRecord>[] => [
  {
    header: "Имя",
    cell: (r) => (
      <>
        <Typography.Text strong>{keyName(r)}</Typography.Text>
        {r.description ? <div>{r.description}</div> : null}
      </>
    ),
  },
  { header: "Добавлен", cell: (r) => (r.created_at ? <Moment at={r.created_at} /> : null), width: 200 },
  {
    header: "Последнее использование",
    cell: (r) => (r.last_used_at ? <Moment at={r.last_used_at} /> : "Ещё не использовался"),
  },
  {
    header: "",
    cell: (r) => (
      <Button
        size="small"
        danger
        loading={busy === r.id}
        aria-label={`Удалить ключ доступа «${keyName(r)}»`}
        onClick={() => onRemove(r)}
      >
        Удалить
      </Button>
    ),
  },
];

export function AccessKeysSection({
  userId,
  factor,
  onCount,
  addRequested = 0,
  onChanged,
}: {
  userId: string;
  /** Состояние способов (доступность ключей, способы подтверждения); `null` — ещё не прочитано. */
  factor: SecondFactorState | null;
  /**
   * Число ключей в прочитанном перечне — для сводки и таблицы способов;
   * `"unread"` — чтение перечня отказано, а прочитанного прежде нет.
   */
  onCount?: (n: number | "unread") => void;
  /** Растёт — открыть добавление ключа (действие пустого состояния второго фактора). */
  addRequested?: number;
  /** Перечень изменился — способы подтверждения у службы тоже. */
  onChanged?: () => void;
}) {
  const id = useId();
  const announce = useAnnounce();
  const [supported] = useState(accessKeyCreationSupported);
  const [keys, setKeys] = useState<AccessKeyRecord[] | null>(null);
  const [listRefusal, setListRefusal] = useState<KeysRefusal | null>(null);
  const [adding, setAdding] = useState(false);
  const [addOpenHere, setAddOpenHere] = useState(false);
  // Просьба открыть добавление извне — числом: просьба открыта, пока её номер
  // больше последнего закрытого.
  const [addDismissed, setAddDismissed] = useState(0);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [addRefusal, setAddRefusal] = useState<KeysRefusal | null>(null);
  const [browserRefused, setBrowserRefused] = useState(false);
  const [asked, setAsked] = useState<AccessKeyRecord | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const [removeRefusal, setRemoveRefusal] = useState<KeysRefusal | null>(null);
  // Уход с экрана посреди шага: исход не пишется, церемония браузера снята.
  const alive = useRef<AbortController | null>(null);
  // Перечень хоть раз прочитан — отказ перечитывания число у соседей не снимает.
  const readOnce = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    alive.current = controller;
    return () => {
      controller.abort();
      alive.current = null;
    };
  }, []);

  // Исход чтения перечня ложится в состояние только у живого раздела.
  const applyRead = useCallback(
    (read: KeysRead) => {
      if (alive.current?.signal.aborted) return;
      if ("keys" in read) {
        readOnce.current = true;
        setKeys(read.keys);
        setListRefusal(null);
        onCount?.(read.keys.length);
      } else {
        setListRefusal(read.refusal);
        // Прочитанное прежде остаётся в таблице раздела — и его число у соседей;
        // перечня не было — соседи узнают отказ, а не ждут ответа, которого не будет.
        if (!readOnce.current) onCount?.("unread");
      }
    },
    [onCount],
  );

  const reload = useCallback(async () => {
    applyRead(await readKeys(userId));
    onChanged?.();
  }, [userId, applyRead, onChanged]);

  // Первое чтение — подпиской на ответ, а не вызовом, пишущим состояние из
  // эффекта: исход ложится, когда ответ пришёл, и не ложится вовсе, если
  // раздел за это время сменил пользователя либо ушёл с экрана.
  useEffect(() => {
    let cancelled = false;
    void readKeys(userId).then((read) => {
      if (!cancelled) applyRead(read);
    });
    return () => {
      cancelled = true;
    };
  }, [userId, applyRead]);

  const turnedOff = factor?.accessKeys?.available === false;
  const canAdd = supported && !turnedOff;

  const addOpen = canAdd && (addOpenHere || addRequested > addDismissed);
  const setAddOpen = (open: boolean) => {
    setAddOpenHere(open);
    if (!open) setAddDismissed(addRequested);
  };

  const add = async () => {
    const controller = alive.current;
    if (!controller || adding || !canAdd) return;
    setAdding(true);
    setAddRefusal(null);
    setBrowserRefused(false);
    setRemoveRefusal(null);
    const step = "Ключ не добавлен.";
    const settle = (f: () => void) => {
      if (!controller.signal.aborted) f();
    };
    try {
      let publicKey: PublicKeyCredentialCreationOptions;
      const subject = keySubject(name);
      const failed = (refusal: KeysRefusal) => settle(() => setAddRefusal(refusal));
      const refusedByBrowser = () => settle(() => setBrowserRefused(true));
      try {
        publicKey = registrationRequestOf(await accessKeysClient.beginRegistration(userId));
      } catch (e) {
        failed(refusalOf(e, step));
        return;
      }
      let created: Credential | null;
      try {
        created = await navigator.credentials.create({ publicKey, signal: controller.signal });
      } catch {
        // Отказ церемонии браузером: приём результата не зовётся — результата нет.
        refusedByBrowser();
        return;
      }
      if (controller.signal.aborted) return;
      let credential: Record<string, unknown>;
      try {
        credential = registrationCredentialOf(created);
      } catch {
        refusedByBrowser();
        return;
      }
      try {
        await accessKeysClient.finishRegistration(userId, { name, description, credential });
      } catch (e) {
        failed(refusalOf(e, step));
        // Операция кончилась отказом либо без исхода — перечень мог измениться.
        if (e instanceof AccessKeyOperationFailed || e instanceof AccessKeyOperationUnknown) await reload();
        return;
      }
      settle(() => {
        setName("");
        setDescription("");
        setAddOpen(false);
      });
      await reload();
      announce(mutationSuccessText("create", subject));
    } finally {
      settle(() => setAdding(false));
    }
  };

  /**
   * «Удалить» у ключа. Ключи выключены и подтвердить снятие нечем — сообщение о
   * пути вместо окна, замкнутого в круг (Р7): снятие держит пол «2», а «2» здесь
   * даёт только код. Иначе — диалог с последствием (Р6).
   */
  const ask = (record: AccessKeyRecord) => {
    setRemoveRefusal(null);
    if (turnedOff && factor?.stepUpMethods !== undefined && factor.stepUpMethods.length === 0) {
      announce(ACCESS_KEYS_TEXT.removeImpossible);
      return;
    }
    setAsked(record);
  };

  const remove = async (record: AccessKeyRecord) => {
    const controller = alive.current;
    if (!controller || removing !== null) return;
    setRemoving(record.id);
    setRemoveRefusal(null);
    const subject = keySubject(keyName(record));
    try {
      await accessKeysClient.revoke(userId, record.id);
      await reload();
      announce(mutationSuccessText("delete", subject));
    } catch (e) {
      const refusal = refusalOf(e, `Ключ «${keyName(record)}» не удалён.`);
      if (!controller.signal.aborted) setRemoveRefusal(refusal);
      announce(mutationFailureText("delete", subject, refusal.text));
      // Отказ операции (гонка двух снятий) — перечень перечитан; синхронный отказ
      // снятия — операции не было, и перечень прежний.
      if (e instanceof AccessKeyOperationFailed || e instanceof AccessKeyOperationUnknown) await reload();
    } finally {
      if (!controller.signal.aborted) setRemoving(null);
    }
  };

  const fieldError = (f: KeyField) => (addRefusal?.field === f ? addRefusal.text : null);
  const marked = (inputId: string, error: string | null) =>
    error ? { "aria-invalid": true as const, "aria-describedby": fieldErrorId(inputId), status: "error" as const } : {};
  const nameId = `${id}-name`;
  const descriptionId = `${id}-description`;
  // Единственный способ «2» — этот ключ: после снятия подтверждать действия нечем.
  const lastLevelTwo =
    factor?.stepUpMethods !== undefined &&
    factor.stepUpMethods.length === 1 &&
    factor.stepUpMethods[0] === "webauthn" &&
    (keys?.length ?? 0) === 1;

  return (
    <SecuritySection section="accessKeys">
      {keys === null && listRefusal === null && <Spin />}
      {listRefusal && (
        <div style={{ marginBottom: 12 }}>
          <RefusalNotice refusal={listRefusal} />
          <Button onClick={() => void reload()}>Прочитать снова</Button>
        </div>
      )}
      {turnedOff && (
        <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 12 }}>
          <KeyOutlined aria-hidden style={{ fontSize: 28, color: "var(--kc-text-tertiary)" }} />
          <Typography.Paragraph style={{ margin: 0 }}>{ACCESS_KEYS_TEXT.turnedOff}</Typography.Paragraph>
        </div>
      )}
      {keys !== null && keys.length === 0 && !turnedOff && (
        <Typography.Paragraph>{ACCESS_KEYS_TEXT.empty}</Typography.Paragraph>
      )}
      {keys !== null && keys.length > 0 && (
        <div style={{ marginBottom: 12 }}>
          <ResourceTable rows={keys} columns={KEY_COLUMNS(ask, removing)} rowKey={(k) => k.id} complete fill={false} />
        </div>
      )}
      {removeRefusal && <RefusalNotice refusal={removeRefusal} />}
      {keys !== null &&
        !turnedOff &&
        (supported ? (
          <Button type={keys.length === 0 ? "primary" : "default"} onClick={() => setAddOpen(true)}>
            Добавить ключ доступа
          </Button>
        ) : (
          <Typography.Paragraph>{KEY_CEREMONY_UNSUPPORTED}</Typography.Paragraph>
        ))}
      <Modal
        open={addOpen}
        title="Добавить ключ доступа"
        onCancel={() => setAddOpen(false)}
        mask={{ closable: false }}
        footer={[
          <Button key="cancel" onClick={() => setAddOpen(false)} disabled={adding}>
            Отмена
          </Button>,
          <Button key="ok" type="primary" loading={adding} onClick={() => void add()}>
            Добавить
          </Button>,
        ]}
      >
        <FormGrid label="Добавить ключ доступа" onSubmit={() => void add()}>
          <Form.Item label="Имя" htmlFor={nameId}>
            <Input
              id={nameId}
              value={name}
              onChange={(e) => setName(e.target.value)}
              {...marked(nameId, fieldError("name"))}
            />
            <FieldError id={fieldErrorId(nameId)} message={fieldError("name") ?? undefined} />
          </Form.Item>
          <Form.Item label="Описание" htmlFor={descriptionId}>
            <Input
              id={descriptionId}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              {...marked(descriptionId, fieldError("description"))}
            />
            <FieldError id={fieldErrorId(descriptionId)} message={fieldError("description") ?? undefined} />
          </Form.Item>
          {addRefusal && addRefusal.field === null && <RefusalNotice refusal={addRefusal} />}
          {browserRefused && (
            <div style={{ marginBottom: 12 }}>
              <Alert type="warning" showIcon message={KEY_CEREMONY_BROWSER_REFUSED} />
            </div>
          )}
          <button type="submit" hidden aria-hidden tabIndex={-1} />
        </FormGrid>
      </Modal>
      <DangerDialog
        open={asked !== null}
        title={asked ? `Удалить ключ доступа «${keyName(asked)}»?` : ""}
        consequences={
          lastLevelTwo
            ? [ACCESS_KEYS_TEXT.removeConsequence, ACCESS_KEYS_TEXT.removeLastLevelTwo]
            : [ACCESS_KEYS_TEXT.removeConsequence]
        }
        confirmText="Удалить"
        onCancel={() => setAsked(null)}
        onConfirm={() => {
          const record = asked;
          // Диалог закрывается ДО глагола: окно подтверждения, если служба его
          // спросит, встаёт одно, а не поверх этого диалога.
          setAsked(null);
          if (record) void remove(record);
        }}
      />
    </SecuritySection>
  );
}
