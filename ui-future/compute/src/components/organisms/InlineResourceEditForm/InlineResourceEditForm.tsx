// InlineResourceEditForm — generic inline-форма редактирования ресурса,
// встраиваемая в правую панель ResourceDetailPage вместо "Общее"-Descriptions.
// Делегирует рендер ResourceFormBody (editMode → immutable-поля через ImmutableField),
// PATCH с computeUpdateMask и Operation-banner на onSuccess.
//
// Применяется по умолчанию ко всем ресурсам, у которых есть spec.fields. Для
// resource-specific layout (например, YC-style для subnet) детальная страница
// может передать свой `renderInlineEdit` в ResourceDetailPage и переопределить
// эту форму.

import { useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Alert } from "antd";
import { extractOperationId } from "@/components/molecules/OperationDialog";
import { ResourceFormBody } from "@/components/organisms/form/ResourceFormBody";
import { computeUpdateMask, snakeToCamelPath } from "@/components/organisms/ResourceFormDialog";
import { ApiError, api } from "@/api/client";
import { applyFieldDefaults, type ResourceSpec } from "@/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@/lib/use-operation";
import { toast } from "@/lib/toast";

interface Props {
  spec: ResourceSpec;
  /** Текущий объект ресурса (уже загружен ResourceDetailPage). */
  data: Record<string, unknown>;
  /** project_id для invalidate + OperationBanner. */
  projectId: string | null;
  onCancel: () => void;
  onSuccess?: () => void;
}

export function InlineResourceEditForm({ spec, data, projectId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const fields = spec.fields;
  const originalRef = useRef<Record<string, unknown> | null>(null);
  const [obj, setObj] = useState<Record<string, unknown>>({});
  const [hydrated, setHydrated] = useState(false);

  const id = (data.id as string | undefined) ?? "";

  useEffect(() => {
    if (hydrated || !fields) return;
    // wire → form: если spec определил hydrate (см. resource-registry для
    // NIC v4/v6_address_ids/security_group_ids и Subnet v4/v6_cidr_blocks),
    // оборачиваем array-of-string поля в {value:"..."}-объекты, чтобы
    // RefSelect/array-form их корректно отображал в edit-режиме. Иначе
    // RefSelect получает массив строк и не показывает имена.
    const wireData: Record<string, unknown> = { ...data };
    const baseObj = spec.hydrate ? spec.hydrate(wireData) : wireData;
    const merged = applyFieldDefaults(fields, baseObj);
    originalRef.current = baseObj;
    setObj(merged);
    setHydrated(true);
  }, [data, fields, hydrated, spec]);

  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.update(`${spec.apiPath}/${id}`, item),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) {
        setPendingOpId(opId);
      } else {
        invalidate(spec.id, projectId);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Сохранить ${spec.singular}: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Сохранить ${spec.singular}: ${op.error.message ?? "ошибка"}`);
    } else {
      invalidate(spec.id, projectId);
      toast.success(`${spec.singular} сохранён`);
      onSuccess?.();
    }
    setPendingOpId(null);
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    if (!fields || !originalRef.current) return;
    let parsed: Record<string, unknown> = obj;
    if (spec.sanitize) parsed = spec.sanitize(parsed);
    const mask = computeUpdateMask(originalRef.current, parsed, fields);
    if (mask.length === 0) {
      onCancel();
      return;
    }
    mutation.mutate({
      ...parsed,
      update_mask: mask.map(snakeToCamelPath).join(","),
    });
  };

  if (!fields) {
    return <Alert type="warning" message={`У ресурса ${spec.singular} нет form-schema; используйте API напрямую.`} />;
  }

  return (
    <ResourceFormBody
      spec={spec}
      mode="edit"
      obj={obj}
      onChange={setObj}
      submitLabel="Сохранить"
      submitting={mutation.isPending || pendingOpId !== null}
      onSubmit={submit}
      onCancel={onCancel}
    />
  );
}
