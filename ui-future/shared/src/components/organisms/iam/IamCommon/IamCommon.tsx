// Общие хелперы для IAM-страниц: формат timestamp + copyable id + Operation
// poll-обёртка для мутаций.

import { useState, useCallback, useEffect, useRef } from "react";
import { Button, Typography, Tag } from "antd";
import { useQueryClient } from "@tanstack/react-query";
import { CopyOutlined } from "@ant-design/icons";
import { api, ApiError } from "@shared/api/client";
import { formatDateTime } from "@shared/lib/datetime";
import { useOperation } from "@shared/lib/use-operation";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { toast } from "@shared/lib/toast";
import type { Operation } from "@shared/api/types";
import type { Role } from "@shared/api/iam";
import { copyText } from "@shared/lib/clipboard";

/**
 * groupedRoleOptions — опции для role-picker `<Select>`, сгруппированные на
 * "Системные" / "Кастомные" (AntD OptGroup-style nested options).
 * KAC-127: единый формат во всех role-pickers (AccessBindings, invite-user).
 */
export function groupedRoleOptions(roles: Role[]) {
  // Backend gRPC-gateway emit'ит camelCase `isSystem`; старый snake_case `is_system`
  // оставлен для совместимости. KAC-171 follow-up — раньше преднастроенные 58
  // system-roles падали в "Кастомные" группу потому что is_system всегда undefined.
  const isSystemRole = (r: Role) => r.is_system === true || r.isSystem === true;
  const system = roles.filter(isSystemRole);
  const custom = roles.filter((r) => !isSystemRole(r));
  const toOpt = (r: Role) => ({
    value: r.id,
    label: `${r.name} · ${r.id}`,
  });
  const groups: { label: string; options: { value: string; label: string }[] }[] = [];
  if (system.length > 0) groups.push({ label: "Системные", options: system.map(toOpt) });
  if (custom.length > 0) groups.push({ label: "Кастомные", options: custom.map(toOpt) });
  return groups;
}

export function fmtTs(ts?: string): string {
  if (!ts) return "—";
  try {
    return formatDateTime(ts);
  } catch {
    return ts;
  }
}

export function CopyableMonoId({ id }: { id: string | undefined }) {
  if (!id) return <Typography.Text type="secondary">—</Typography.Text>;
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
      <code style={{ fontSize: 12, fontFamily: "monospace" }}>{id}</code>
      <Button
        size="small"
        type="text"
        icon={<CopyOutlined style={{ fontSize: 11 }} />}
        onClick={(e) => {
          e.stopPropagation();
          if (id) {
            // См. `@shared/lib/clipboard`.
            void copyText(id);
            toast.success("Скопировано");
          }
        }}
      />
    </span>
  );
}

export function SystemTag({ isSystem }: { isSystem?: boolean }) {
  return isSystem ? <Tag color="purple">Системная</Tag> : <Tag color="default">Пользовательская</Tag>;
}

/**
 * useIamMutation — обёртка над POST/PATCH/DELETE мутирующего RPC + Operation polling.
 * Возвращает { run, pending, op } — run(body|path) запускает мутацию, после
 * получения operationId — peek-poll до done; на done success — invalidate
 * указанные query-keys; на done error — toast.
 *
 * opts.onSuccess — вызывается после успешной операции (после done=true && !error).
 */
export function useIamMutation(opts: {
  method: "POST" | "PATCH" | "DELETE" | "ACTION";
  path: string | ((body: unknown) => string);
  invalidateKeys: string[][];
  successText?: string;
  onSuccess?: (op: Operation) => void;
}) {
  const qc = useQueryClient();
  const [opId, setOpId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { data: op } = useOperation(opId);
  const onSuccessRef = useRef(opts.onSuccess);
  const invalidateRef = useRef(opts.invalidateKeys);
  const successTextRef = useRef(opts.successText);
  useEffect(() => {
    onSuccessRef.current = opts.onSuccess;
    invalidateRef.current = opts.invalidateKeys;
    successTextRef.current = opts.successText;
  });

  useEffect(() => {
    if (op?.done && opId) {
      if (op.error) {
        toast.error(op.error.message ?? "Operation failed");
      } else {
        if (successTextRef.current) toast.success(successTextRef.current);
        invalidateRef.current.forEach((k) => void qc.invalidateQueries({ queryKey: k }));
        if (onSuccessRef.current) onSuccessRef.current(op);
      }
      setOpId(null);
      setSubmitting(false);
    }
  }, [op?.done, op?.error, opId, qc, op]);

  const run = useCallback(
    async (body?: unknown) => {
      setSubmitting(true);
      try {
        const path = typeof opts.path === "function" ? opts.path(body) : opts.path;
        // Форма ответа — то, что предстоит выяснить, а не то, что можно
        // объявить. Тип `{ operation: Operation }` здесь и был корнем: он
        // утверждал вложенный конверт, которого край не отдаёт, и заставлял
        // читать пустой ключ.
        let resp: unknown;
        switch (opts.method) {
          case "POST":
            resp = await api.create(path, body ?? {});
            break;
          case "PATCH":
            resp = await api.update(path, body ?? {});
            break;
          case "DELETE":
            resp = await api.delete(path);
            break;
          case "ACTION":
            resp = await api.action(path, body ?? {});
            break;
        }
        // ИСХОД ЧИТАЕТСЯ ОБЩИМ РАЗБОРОМ, а не своим ключом.
        //
        // Здесь стояло `resp?.operation?.id ?? null`. Край отдаёт Operation
        // ВЕРХНИМ уровнем — это записано в `operation-outcome.ts`, который знает
        // обе формы конверта, — поэтому вложенный ключ был пуст ВСЕГДА, ветка
        // «операции нет, считаем успех» срабатывала на каждой мутации, а опрос
        // ниже (он написан верно и читает `error`) не запускался ни разу.
        //
        // Через этот путь идут выдача ролей, приглашение пользователя и членство
        // в группах — действия, где «применилось или нет» важнее всего. Отказ
        // операции показывался пользователю зелёным тостом.
        //
        // `expectOperation` здесь безусловно true, и это не допущение: все
        // мутации iam объявлены `returns (operation.Operation)` — предикат
        // (разбор контрактов `proto/kaname/cloud/iam/v1/*`) не нашёл ни одного
        // синхронного мутирующего RPC этого домена.
        const resolved = resolveMutationResponse(resp, true);
        if (resolved.kind === "operation") {
          setOpId(resolved.opId);
        } else if (resolved.kind === "violation") {
          // Не успех и не отказ сервера — ответ не той формы, какую обещает
          // контракт. Подтвердить выполнение нечем, поэтому говорим об этом, а
          // не рапортуем «готово».
          toast.error(resolved.message);
          setSubmitting(false);
        } else {
          invalidateRef.current.forEach((k) => void qc.invalidateQueries({ queryKey: k }));
          if (successTextRef.current) toast.success(successTextRef.current);
          setSubmitting(false);
        }
        // Ответ отдаётся как есть: его ФОРМА — как раз то, что выясняет разбор
        // выше, и сузить её здесь значило бы повторить исходную ошибку в типе.
        // Читателей у этого значения в дереве нет (`run(...)` зовут ради
        // побочного действия), поэтому оно ничего не обещает вызывающему.
        return resp;
      } catch (e) {
        const msg = e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Ошибка";
        toast.error(msg);
        setSubmitting(false);
        throw e;
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [opts.method, opts.path, qc],
  );

  return { run, op, submitting, opId };
}
