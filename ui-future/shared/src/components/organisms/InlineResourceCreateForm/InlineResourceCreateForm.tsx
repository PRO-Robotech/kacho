// InlineResourceCreateForm — встраиваемая форма создания ресурса.
// Используется когда форма должна жить внутри detail-страницы родителя
// (Network detail → Создать подсеть), а не как отдельный route.
//
// Mirror'ит submit-логику ResourceCreatePage, но без navigate/breadcrumb-side-effects.

import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert } from "antd";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { ResourceFormBody } from "@shared/components/organisms/form/ResourceFormBody";
import { ApiError, api } from "@shared/api/client";
import {
  applyFieldDefaults,
  type ResourceSpec,
} from "@shared/lib/resource-registry";
import { setByPath } from "@shared/lib/path";
import {
  useInvalidateResourceList,
  useOperation,
} from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

interface Props {
  spec: ResourceSpec;
  /** Контекст для ResourceSpec.template — передаётся в applyFieldDefaults. */
  ctx: { projectId?: string; accountId?: string };
  /** Поля, которые должны быть pre-filled и locked (immutable в форме).
   *  Ключи — paths (например "network_id" или "internal_ipv4_address_spec.subnet_id"). */
  presetFields?: Record<string, unknown>;
  /** Поля, которые должны быть pre-filled, но остаются editable (это начальное
   *  значение, не lock). Пример: `_address_kind` при создании адреса из контекста
   *  подсети — дефолт "internal", но пользователь может переключить на "internal_v6". */
  editablePresetFields?: Record<string, unknown>;
  /** Per-field ограничение набора опций enum-полей. Ключ — имя поля, значение —
   *  список допустимых `value` (в нужном порядке). Опции, не вошедшие в список,
   *  не показываются. Пример: `{ _address_kind: ["internal", "internal_v6"] }` —
   *  в контексте подсети internal IPv4/IPv6, без `external`. */
  fieldOptionsFilter?: Record<string, string[]>;
  /** projectId для invalidate + OperationBanner. */
  projectId: string | null;
  /** Title формы. По умолчанию — "Создать <singular>". */
  title?: string;
  onCancel: () => void;
  /** Вызывается после успешного submit (Operation pushed в banner или sync-create). */
  onSuccess?: () => void;
}

export function InlineResourceCreateForm({
  spec,
  ctx,
  presetFields,
  editablePresetFields,
  fieldOptionsFilter,
  projectId,
  title,
  onCancel,
  onSuccess,
}: Props) {
  const invalidate = useInvalidateResourceList();
  const presets = useMemo(() => presetFields ?? {}, [presetFields]);
  const editablePresets = useMemo(
    () => editablePresetFields ?? {},
    [editablePresetFields],
  );

  const initialObj = useMemo(() => {
    const tpl = spec.template(ctx);
    const baseObj =
      typeof tpl === "object" && tpl !== null
        ? { ...(tpl as Record<string, unknown>) }
        : {};
    let merged: Record<string, unknown> = applyFieldDefaults(
      spec.fields,
      baseObj,
    );
    for (const [path, val] of Object.entries(editablePresets)) {
      merged = setByPath(merged, path, val);
    }
    for (const [path, val] of Object.entries(presets)) {
      merged = setByPath(merged, path, val);
    }
    // Auto-name: если у ресурса есть поле name и оно пустое, генерируем
    // <route>-NNNNNN — иначе backend (UNIQUE по project_id+name) отвечает
    // ALREADY_EXISTS на повторный nameless ресурс.
    if (
      spec.fields?.some((f) => f.name === "name") &&
      (!merged.name || merged.name === "")
    ) {
      const stem = spec.route.replace(/-/g, "");
      merged.name = `${stem}-${Math.floor(100000 + Math.random() * 900000)}`;
    }
    return merged;
    // initial — фиксируем при mount; presets/ctx стабильны через жизненный цикл формы
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [obj, setObj] = useState<Record<string, unknown>>(initialObj);
  const lockedPathsRef = useRef(new Set(Object.keys(presets)));

  // Doppler-flow: после POST дожидаемся op.done через polling, кнопка
  // пульсирует, форма не закрывается. По завершении → success/error toast.
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(spec.apiPath, item),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) {
        setPendingOpId(id);
      } else {
        // Sync-ответ (admin RPC без Operation envelope) — закрываем сразу.
        invalidate(spec.id, projectId);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m =
        err instanceof ApiError
          ? `${err.code}: ${err.message}`
          : (err as Error).message;
      toast.error(`Создать ${spec.singular}: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      const msg = op.error.message ?? "ошибка";
      toast.error(`Создать ${spec.singular}: ${msg}`);
    } else {
      invalidate(spec.id, projectId);
      toast.success(`${spec.singular} создан`);
      onSuccess?.();
    }
    setPendingOpId(null);
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    let parsed: Record<string, unknown> = obj;
    // Клиент-валидация ДО sanitize (инварианты читают UI-дискриминаторы формы) —
    // additive: срабатывает только если spec.validate определён (compute-parity).
    if (spec.validate) {
      const err = spec.validate(parsed);
      if (err) {
        toast.error(err);
        return;
      }
    }
    if (spec.sanitize) parsed = spec.sanitize(parsed);
    mutation.mutate(parsed);
  };

  const fields = spec.fields;
  if (!fields) {
    return (
      <Alert
        type="warning"
        message={`У ресурса ${spec.singular} нет form-schema; используйте API напрямую.`}
      />
    );
  }

  return (
    <ResourceFormBody
      spec={spec}
      mode="create"
      obj={obj}
      onChange={setObj}
      lockedPaths={lockedPathsRef.current}
      fieldOptionsFilter={fieldOptionsFilter}
      title={title}
      submitLabel={`Создать ${spec.singular.toLowerCase()}`}
      submitting={mutation.isPending || pendingOpId !== null}
      onSubmit={submit}
      onCancel={onCancel}
    />
  );
}
