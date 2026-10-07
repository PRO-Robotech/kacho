// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Alert, Button, Form, Input, Modal, Spin, Typography } from "antd";
import { ApiError } from "@shared/api/client";
import { AccessKeyEncodingError, registrationCredentialOf, registrationRequestOf } from "@shared/api/access-key";
import { NOT_BY_SUBSTANCE_TEXT } from "@shared/api/login-lane";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { formatDateTime } from "@shared/lib/datetime";
import {
  genderOfLabel,
  mutationFailureText,
  mutationSuccessText,
  type MutationSubject,
} from "@shared/lib/mutation-signal";
import { toast } from "@shared/lib/toast";
import {
  AccessKeyOperationFailed,
  AccessKeyOperationUnknown,
  accessKeysClient,
  type AccessKeyRecord,
} from "@shared/api/access-keys";

// Раздел «Ключи доступа» экрана параметров учётной записи (приёмка F8, ред. 12,
// S4, группа L; Р11).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО РАЗДЕЛ ДЕЛАЕТ И ЧЕГО НЕ ДЕЛАЕТ
//
//   • перечень — глаголом Ф7, дочитанный до конца; ключ показан именем,
//     описанием, моментом заведения и моментом последнего использования либо
//     «Ещё не использовался» — пустое значение означает «не использовался» (Ф7);
//   • заведение — выдача испытания → `navigator.credentials.create` над ним →
//     приём результата → `done` операции → перечень перечитан. Параметры
//     церемонии и результат переносит кодек (`api/access-key.ts`) без изменения;
//   • снятие — у КАЖДОГО ключа, и у последнего тоже: правило «последний способ
//     входа» судит служба (Ф7-26), и раздел показывает её отказ дословно — из
//     ответа снятия либо из `error` операции; ключ остаётся (F8-58, F8-69).
//     Удаление спрашивает подтверждения диалогом, называющим ключ; отмена не
//     выпускает ничего (F8-60);
//   • имя и описание раздел НЕ проверяет: форму судит служба, и её нарушение
//     поля стоит у поля (`BadRequest.fieldViolations`), остальное — у раздела
//     (F8-70, F8-71); введённое при любом отказе сохранено.
//
// ИСХОД — ЕДИНЫМ МЕХАНИЗМОМ СИГНАЛА (решение владельца 2026-08-15: каждое
// действие сообщает о выполнении или отказе). Тексты — `mutation-signal.ts`,
// показ — уведомлением (`toast`; показ примонтирован экраном параметров) и,
// поскольку отказ обязан стоять у поля либо у раздела (Р11), ещё и на месте.
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

/** Подлежащее сигнала: ключ доступа по имени (пустое имя — по идентификатору). */
const KEY_LABEL = "Ключ доступа";
function keySubject(name: string): MutationSubject {
  return { label: KEY_LABEL, gender: genderOfLabel(KEY_LABEL) ?? "m", name: name.trim() || null };
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

function KeyItem({ record, busy, onRemove }: { record: AccessKeyRecord; busy: boolean; onRemove: () => void }) {
  return (
    <li style={{ display: "flex", alignItems: "flex-start", gap: 12, padding: "8px 0" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <Typography.Text strong>{record.name || record.id}</Typography.Text>
        {record.description ? <div>{record.description}</div> : null}
        <Typography.Text type="secondary" style={{ display: "block" }}>
          Заведён {formatDateTime(record.created_at)}
        </Typography.Text>
        <Typography.Text type="secondary" style={{ display: "block" }}>
          {record.last_used_at
            ? `Последнее использование ${formatDateTime(record.last_used_at)}`
            : "Ещё не использовался"}
        </Typography.Text>
      </div>
      <Button danger loading={busy} onClick={onRemove}>
        Удалить
      </Button>
    </li>
  );
}

export function AccessKeysSection({ userId }: { userId: string }) {
  const id = useId();
  const [supported] = useState(accessKeyCreationSupported);
  const [keys, setKeys] = useState<AccessKeyRecord[] | null>(null);
  const [listRefusal, setListRefusal] = useState<KeysRefusal | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [adding, setAdding] = useState(false);
  const [addRefusal, setAddRefusal] = useState<KeysRefusal | null>(null);
  const [browserRefused, setBrowserRefused] = useState(false);
  const [asked, setAsked] = useState<AccessKeyRecord | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const [removeRefusal, setRemoveRefusal] = useState<KeysRefusal | null>(null);
  // Уход с экрана посреди шага: исход не пишется, церемония браузера снята.
  const alive = useRef<AbortController | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    alive.current = controller;
    return () => {
      controller.abort();
      alive.current = null;
    };
  }, []);

  // Исход чтения перечня ложится в состояние только у живого раздела.
  const applyRead = useCallback((read: KeysRead) => {
    if (alive.current?.signal.aborted) return;
    if ("keys" in read) {
      setKeys(read.keys);
      setListRefusal(null);
    } else {
      setListRefusal(read.refusal);
    }
  }, []);

  const reload = useCallback(async () => {
    applyRead(await readKeys(userId));
  }, [userId, applyRead]);

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

  const add = async () => {
    const controller = alive.current;
    if (!controller || adding || !supported) return;
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
      const failed = (refusal: KeysRefusal) => {
        toast.error(mutationFailureText("create", subject, refusal.text));
        settle(() => setAddRefusal(refusal));
      };
      const refusedByBrowser = () => {
        toast.error(mutationFailureText("create", subject, KEY_CEREMONY_BROWSER_REFUSED));
        settle(() => setBrowserRefused(true));
      };
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
      });
      await reload();
      toast.success(mutationSuccessText("create", subject));
    } finally {
      settle(() => setAdding(false));
    }
  };

  const remove = async (record: AccessKeyRecord) => {
    const controller = alive.current;
    if (!controller || removing !== null) return;
    setRemoving(record.id);
    setRemoveRefusal(null);
    const subject = keySubject(record.name || record.id);
    try {
      await accessKeysClient.revoke(userId, record.id);
      await reload();
      toast.success(mutationSuccessText("delete", subject));
    } catch (e) {
      const refusal = refusalOf(e, `Ключ «${record.name || record.id}» не удалён.`);
      toast.error(mutationFailureText("delete", subject, refusal.text));
      if (!controller.signal.aborted) setRemoveRefusal(refusal);
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
  const sectionId = `${id}-title`;

  return (
    <section aria-labelledby={sectionId} style={{ maxWidth: 720, marginBottom: 32 }}>
      <Typography.Title level={4} id={sectionId} style={{ marginTop: 0 }}>
        Ключи доступа
      </Typography.Title>
      {keys === null && listRefusal === null && <Spin />}
      {listRefusal && (
        <div style={{ marginBottom: 12 }}>
          <RefusalNotice refusal={listRefusal} />
          <Button onClick={() => void reload()}>Прочитать снова</Button>
        </div>
      )}
      {keys !== null && keys.length === 0 && <Typography.Paragraph>Ключей доступа нет</Typography.Paragraph>}
      {keys !== null && keys.length > 0 && (
        <ul aria-label="Заведённые ключи" style={{ listStyle: "none", padding: 0, margin: "0 0 16px" }}>
          {keys.map((k) => (
            <KeyItem
              key={k.id}
              record={k}
              busy={removing === k.id}
              onRemove={() => {
                setRemoveRefusal(null);
                setAsked(k);
              }}
            />
          ))}
        </ul>
      )}
      {removeRefusal && <RefusalNotice refusal={removeRefusal} />}
      {supported ? (
        <FormGrid onSubmit={() => void add()}>
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
          <Button type="primary" htmlType="submit" loading={adding}>
            Добавить ключ доступа
          </Button>
        </FormGrid>
      ) : (
        <Typography.Paragraph>{KEY_CEREMONY_UNSUPPORTED}</Typography.Paragraph>
      )}
      <Modal
        open={asked !== null}
        title={asked ? `Удалить ключ доступа «${asked.name || asked.id}»?` : undefined}
        okText="Удалить"
        okButtonProps={{ danger: true }}
        cancelText="Отмена"
        onCancel={() => setAsked(null)}
        onOk={() => {
          const record = asked;
          setAsked(null);
          if (record) void remove(record);
        }}
      >
        <Typography.Paragraph>После удаления войти им больше будет нельзя.</Typography.Paragraph>
      </Modal>
    </section>
  );
}
