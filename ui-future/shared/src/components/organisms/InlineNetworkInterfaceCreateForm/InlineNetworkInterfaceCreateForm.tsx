// InlineNetworkInterfaceCreateForm — NIC create модалка. Зеркально к
// InlineNetworkInterfaceEditForm. Subnet — RefSelect (если preset не задан).
// Все поля с info-tooltip, labelCol 140px, required-звёздочка справа.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, Select, Space, Tooltip } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { ResourceRefChips } from "@shared/components/molecules/ResourceRefChips";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { LabelsEditor, labelsToMap, type LabelEntry } from "@shared/components/organisms/LabelsEditor";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { toast } from "@shared/lib/toast";

interface Props {
  projectId: string;
  /** subnet_id preset из контекста (например, из subnet detail). Если задан —
   *  Subnet locked; иначе — RefSelect. */
  subnetId?: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

const labelWithInfo = (text: string, info: string) => (
  <Space size={4}>
    {text}
    <Tooltip title={info}>
      <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
    </Tooltip>
  </Space>
);

function autoName(): string {
  return `nic-${Math.floor(100000 + Math.random() * 900000)}`;
}

export function InlineNetworkInterfaceCreateForm({ projectId, subnetId: presetSubnetId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const spec = REGISTRY["network-interfaces"];
  const subnetSpec = REGISTRY["subnets"];

  const [name, setName] = useState(() => autoName());
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [subnetId, setSubnetId] = useState<string | undefined>(presetSubnetId);
  const subnetLocked = !!presetSubnetId;

  const [v4, setV4] = useState<string[]>([]);
  const [v6, setV6] = useState<string[]>([]);
  const [sgs, setSgs] = useState<string[]>([]);

  // Subnets для RefSelect.
  const { data: subnetList } = useQuery({
    queryKey: ["subnets", "list", projectId],
    queryFn: () =>
      api.list<{ subnets: Array<{ id: string; name?: string }> }>(subnetSpec.apiPath, {
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !subnetLocked,
    staleTime: 30_000,
  });
  const subnetOptions = useMemo(
    () =>
      (subnetList?.subnets ?? []).map((s) => ({
        value: s.id,
        label: s.name || s.id,
      })),
    [subnetList],
  );

  // Выбранная подсеть → network_id: SG фильтруем по сети подсети.
  const { data: selectedSubnet } = useQuery({
    queryKey: ["subnets", "for-nic-filter", subnetId],
    queryFn: () => api.get<{ network_id?: string }>(`${subnetSpec.apiPath}/${subnetId}`),
    enabled: !!subnetId,
    staleTime: 60_000,
  });
  const sgNetworkId = selectedSubnet?.network_id;

  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(spec.apiPath, item),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) setPendingOpId(opId);
      else {
        invalidate(spec.id, projectId);
        toast.success(`NIC ${name} создан`);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Создать NIC: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Создать NIC: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
      return;
    }
    invalidate(spec.id, projectId);
    toast.success(`NIC ${name} создан`);
    setPendingOpId(null);
    onSuccess?.();
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    if (!subnetId) {
      toast.error("Выберите подсеть для интерфейса.");
      return;
    }
    mutation.mutate({
      project_id: projectId,
      subnet_id: subnetId,
      name,
      description: description || undefined,
      labels: labelsToMap(labels),
      v4_address_ids: v4,
      v6_address_ids: v6,
      security_group_ids: sgs,
    });
  };

  return (
    <FormShell specId="network-interfaces" mode="create" singular={spec.singular}>
      <Form
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "1 1 0" }}
        labelAlign="left"
        colon={false}
        size="middle"
      >
        <Form.Item
          label={labelWithInfo("Подсеть", "Подсеть, в которой создаётся NIC. После Create иммутабельно.")}
          required
        >
          <Select
            showSearch
            value={subnetId}
            onChange={setSubnetId}
            options={subnetOptions}
            placeholder="Выберите подсеть"
            optionFilterProp="label"
            disabled={subnetLocked}
          />
        </Form.Item>

        <Form.Item label={labelWithInfo("Имя", "Имя интерфейса в пределах фолдера.")} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Описание", "Опциональное описание для людей.")}>
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Метки", "Пары ключ=значение для группировки/фильтрации.")}>
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>

        <Form.Item label={labelWithInfo("IPv4 адрес", "Один Address-ресурс с internal_ipv4. KAC-55: максимум один.")}>
          <ResourceRefChips
            title="IPv4 Address"
            refResource="addresses"
            projectId={projectId}
            tagColor="blue"
            value={v4}
            onChange={setV4}
            maxItems={1}
            disabled={!subnetId}
            disabledHint="Сначала выберите подсеть"
            refFilter={(row) =>
              (row.internal_ipv4_address as { subnet_id?: string } | undefined)?.subnet_id === subnetId
            }
            createResource="addresses"
            createPresetFields={{
              _address_kind: "internal",
              ...(subnetId ? { "internal_ipv4_address_spec.subnet_id": subnetId } : {}),
            }}
            createTitle="Создание внутреннего IPv4-адреса"
          />
        </Form.Item>

        <Form.Item
          label={labelWithInfo("IPv6 адрес", "Internal или external IPv6 Address-ресурс. KAC-55: максимум один.")}
        >
          <ResourceRefChips
            title="IPv6 Address"
            refResource="addresses"
            projectId={projectId}
            tagColor="geekblue"
            value={v6}
            onChange={setV6}
            maxItems={1}
            disabled={!subnetId}
            disabledHint="Сначала выберите подсеть"
            refFilter={(row) =>
              (row.internal_ipv6_address as { subnet_id?: string } | undefined)?.subnet_id === subnetId
            }
            createResource="addresses"
            createEditablePresetFields={{ _address_kind: "internal_v6" }}
            createPresetFields={subnetId ? { "internal_ipv6_address_spec.subnet_id": subnetId } : undefined}
            createTitle="Создание IPv6-адреса"
          />
        </Form.Item>

        <Form.Item label={labelWithInfo("Группы безопасности", "Security Groups, прилинкованные к NIC.")}>
          <ResourceRefChips
            title="Security Group"
            refResource="security-groups"
            projectId={projectId}
            tagColor="purple"
            value={sgs}
            onChange={setSgs}
            disabled={!subnetId}
            disabledHint="Сначала выберите подсеть"
            refFilter={(row) => !!sgNetworkId && row.network_id === sgNetworkId}
            createResource="security-groups"
            createTitle="Создание группы безопасности"
          />
        </Form.Item>
        <FormFooter
          submitLabel="Создать сетевой интерфейс"
          submitting={mutation.isPending || !!pendingOpId}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </Form>
    </FormShell>
  );
}
