// InlineSecurityGroupEditForm — inline edit метаданных Security Group.
//
// KAC-239: форма правит ТОЛЬКО name / description / labels (PATCH
// /vpc/v1/securityGroups/<id>, update_mask). Правила управляются отдельно —
// в табе «Правила» через SgRulesPanel (per-rule add/edit/delete), а не правкой
// всего ресурса. Поэтому здесь rules-секции нет.

import { useEffect, useState } from "react";
import { snakeToCamelPath } from "@shared/lib/update-mask";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, Typography } from "antd";
import { api } from "@shared/api/client";
import { resolveMutationResponse } from "@shared/lib/operation-outcome";
import { LabelsFieldRenderer } from "@shared/components/organisms/form/LabelsEditor";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";
import { errorText } from "@shared/lib/error-presentation";

interface Props {
  projectId: string;
  sgId: string;
  onCancel: () => void;
}

export function InlineSecurityGroupEditForm({ projectId, sgId, onCancel }: Props) {
  const sgSpec = REGISTRY["security-groups"];
  const invalidate = useInvalidateResourceList();

  const { data, isLoading } = useQuery({
    queryKey: [sgSpec.id, "detail", sgId],
    queryFn: () => api.get<Record<string, unknown>>(`${sgSpec.apiPath}/${sgId}`),
    enabled: !!sgId,
    staleTime: 0,
  });

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [obj, setObj] = useState<Record<string, unknown>>({});
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    if (!data || hydrated) return;
    setName((data.name as string) ?? "");
    setDescription((data.description as string) ?? "");
    setObj({ labels: data.labels ?? {} });
    setHydrated(true);
  }, [data, hydrated]);

  const updateMain = useMutation({
    mutationFn: (payload: unknown) => api.update(`${sgSpec.apiPath}/${sgId}`, payload),
  });

  const submit = async () => {
    if (!data) return;
    const mask: string[] = [];
    if ((data.name as string) !== name) mask.push("name");
    if (((data.description as string) ?? "") !== description) mask.push("description");
    if (JSON.stringify(data.labels ?? {}) !== JSON.stringify(obj.labels ?? {})) mask.push("labels");

    if (mask.length === 0) {
      onCancel();
      return;
    }
    try {
      const resp = await updateMain.mutateAsync({
        name,
        description,
        labels: obj.labels ?? {},
        update_mask: mask.map(snakeToCamelPath).join(","),
      });
      // Отказ уходит вызывающему через `throw`: он ловится ниже тем же
      // `catch`, что и отказ края, и показывается тем же сообщением. Прежний
      // ключ ветки «операции нет» не имел вовсе — форма закрывалась молча.
      const resolved = resolveMutationResponse(resp, sgSpec.mutationsReturnOperation !== false);
      if (resolved.kind === "violation") throw new Error(resolved.message);
      if (resolved.kind === "operation") {
        operationStore.start({
          id: resolved.opId,
          title: `Сохранение группы безопасности ${name}`,
          resourceId: sgSpec.id,
          projectId,
        });
      }
      invalidate(sgSpec.id, projectId);
      onCancel();
    } catch (err) {
      const m = errorText(err);
      toast.error(`Сохранить группу безопасности: ${m}`);
    }
  };

  if (isLoading || !data) {
    return (
      <div style={{ padding: 24 }}>
        <Typography.Text type="secondary">Загрузка…</Typography.Text>
      </div>
    );
  }

  return (
    <FormShell specId="security-groups" mode="edit" singular={sgSpec.singular}>
      <FormGrid>
        <Form.Item label="Имя">
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Form.Item>

        <Form.Item label="Описание">
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
        </Form.Item>

        <Form.Item label="Метки">
          <LabelsFieldRenderer pathPrefix="" path="labels" label="" value={obj} onChange={setObj} />
        </Form.Item>
        <FormFooter submitLabel="Сохранить" submitting={updateMain.isPending} onSubmit={submit} onCancel={onCancel} />
      </FormGrid>
    </FormShell>
  );
}
