// InlineSubnetEditForm — inline-форма редактирования подсети, встраиваемая в
// правую панель Subnet detail вместо "Общее"-Descriptions. Раскладка повторяет
// InlineSubnetCreateForm; иммутабельные поля (Зона доступности, CIDR-блоки,
// Network) показываются read-only с подсказкой.
//
// Wire: PATCH /vpc/v1/subnets/<id> с update_mask, перечисляющим только
// действительно изменённые mutable-поля.

import { useEffect, useMemo, useState } from "react";
import { snakeToCamelPath } from "@shared/lib/update-mask";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, Select, Space, Tooltip, Typography } from "antd";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { LockOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { REGISTRY, getByPath } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import {
  LabelsEditor,
  labelsToEntries,
  labelsFromEntries,
  type LabelEntry,
} from "@shared/components/organisms/LabelsEditor";

interface Props {
  projectId: string;
  subnetId: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

// VPC-1: DhcpOptions retired; CIDR/placement/network immutable (managed via
// verbs / fixed after Create). Only these fields are mutable through Update.
const MUTABLE_FIELDS = ["name", "description", "labels", "route_table_id"] as const;

export function InlineSubnetEditForm({ projectId, subnetId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const subnetSpec = REGISTRY["subnets"];
  const rtSpec = REGISTRY["route-tables"];

  const { data: subnet, isLoading } = useQuery({
    queryKey: [subnetSpec.id, "detail", subnetId],
    queryFn: () => api.get<Record<string, unknown>>(`${subnetSpec.apiPath}/${subnetId}`),
    enabled: !!subnetId,
    staleTime: 0,
  });

  const networkId = (subnet?.network_id as string | undefined) ?? "";
  const zoneId = (subnet?.zone_id as string | undefined) ?? "";
  const regionId = (subnet?.region_id as string | undefined) ?? "";
  const placementType = (subnet?.placement_type as string | undefined) ?? "";
  const isRegional = placementType === "REGIONAL" || (!zoneId && !!regionId);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [routeTableId, setRouteTableId] = useState<string | undefined>(undefined);
  const [hydrated, setHydrated] = useState(false);

  // Hydrate state из subnet один раз после первого fetch.
  useEffect(() => {
    if (!subnet || hydrated) return;
    setName((subnet.name as string) ?? "");
    setDescription((subnet.description as string) ?? "");
    setLabels(labelsToEntries(subnet.labels as Record<string, string> | undefined));
    setRouteTableId((subnet.route_table_id as string | undefined) || undefined);
    setHydrated(true);
  }, [subnet, hydrated]);

  const { data: rtData } = useQuery({
    queryKey: ["route-tables", "list", projectId, networkId],
    queryFn: () =>
      api.list<{ route_tables: Array<Record<string, unknown>> }>(rtSpec.apiPath, {
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !!projectId && !!networkId,
    staleTime: 30_000,
  });
  const rtOptions = useMemo(
    () =>
      (rtData?.route_tables ?? [])
        .filter((r) => r.network_id === networkId)
        .map((r) => ({
          value: r.id as string,
          label: ((r.name as string) || (r.id as string)) ?? "",
        })),
    [rtData, networkId],
  );

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.update(`${subnetSpec.apiPath}/${subnetId}`, item),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) {
        setPendingOpId(opId);
      } else {
        invalidate(subnetSpec.id, projectId);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Сохранить подсеть: ${m}`);
    },
  });

  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);
  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Сохранить подсеть: ${op.error.message ?? "ошибка"}`);
    } else {
      invalidate(subnetSpec.id, projectId);
      toast.success(`Подсеть ${name} сохранена`);
      onSuccess?.();
    }
    setPendingOpId(null);
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    if (!subnet) return;

    const labelMap = labelsFromEntries(labels);

    const next = {
      name,
      description: description || "",
      labels: labelMap,
      route_table_id: routeTableId || "",
    };

    // Diff против текущего объекта — определяем актуальные изменения.
    const mask: string[] = [];
    if ((subnet.name as string) !== name) mask.push("name");
    if (((subnet.description as string) ?? "") !== description) mask.push("description");
    const origLabels = JSON.stringify(subnet.labels ?? {});
    const newLabels = JSON.stringify(labelMap);
    if (origLabels !== newLabels) mask.push("labels");
    const origRt = (subnet.route_table_id as string) ?? "";
    if (origRt !== (routeTableId ?? "")) mask.push("route_table_id");

    if (mask.length === 0) {
      onCancel();
      return;
    }

    mutation.mutate({
      ...next,
      update_mask: mask.map(snakeToCamelPath).join(","),
    });
  };

  if (isLoading || !subnet) {
    return (
      <div style={{ padding: 24 }}>
        <Typography.Text type="secondary">Загрузка…</Typography.Text>
      </div>
    );
  }

  return (
    <FormShell specId="subnets" mode="edit" singular={subnetSpec.singular}>
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
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              {isRegional ? "Регион" : "Зона доступности"}
              <Tooltip title="Размещение (placementType°) неизменяемо после Subnet.Create">
                <LockOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <Input value={isRegional ? regionId : zoneId} disabled />
        </Form.Item>

        <Form.Item label="Таблица маршрутизации">
          <Select
            value={routeTableId}
            onChange={(v) => setRouteTableId(v)}
            options={rtOptions}
            allowClear
            placeholder="Выберите таблицу маршрутизации (опц.)"
          />
        </Form.Item>

        {/* VPC-1: CIDR (основной + доп.) и placement НЕ редактируются здесь —
            основной иммутабелен, доп. диапазоны мутируются отдельными RPC
            (:add/:remove-cidr-blocks) в панели SubnetCidrPanel блока «Обзор».
            DhcpOptions сняты by design. */}

        <FormFooter
          submitLabel="Сохранить"
          submitting={mutation.isPending || pendingOpId !== null}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </Form>
    </FormShell>
  );

  // Suppress unused
  void getByPath;
  void MUTABLE_FIELDS;
}
