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
import { ApiError, api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { LabelsEditor } from "@shared/components/organisms/form/LabelsEditor";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { operationStore } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";

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
    setObj({ labels: (data.labels as Record<string, string>) ?? {} });
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
      const opId = extractOperationId(resp as Parameters<typeof extractOperationId>[0]);
      if (opId) {
        operationStore.start({
          id: opId,
          title: `Сохранение группы безопасности ${name}`,
          resourceId: sgSpec.id,
          projectId,
        });
      }
      invalidate(sgSpec.id, projectId);
      onCancel();
    } catch (err) {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
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
      <Form
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "1 1 0" }}
        labelAlign="left"
        colon={false}
        size="middle"
      >
        <Form.Item label="Имя" required>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Form.Item>

        <Form.Item label="Описание">
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
        </Form.Item>

        <Form.Item label="Метки">
          <LabelsEditor pathPrefix="" path="labels" label="" value={obj} onChange={setObj} />
        </Form.Item>
        <FormFooter submitLabel="Сохранить" submitting={updateMain.isPending} onSubmit={submit} onCancel={onCancel} />
      </Form>
    </FormShell>
  );
}
