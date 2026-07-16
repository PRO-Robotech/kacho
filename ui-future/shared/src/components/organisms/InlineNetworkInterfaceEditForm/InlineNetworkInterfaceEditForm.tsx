// InlineNetworkInterfaceEditForm — NIC edit модалка. Visual parity с
// InlineNetworkInterfaceCreateForm.

import { useEffect, useState } from "react";
import { snakeToCamelPath } from "@shared/lib/update-mask";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, Space, Tooltip, Typography } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { ResourceRefChips } from "@shared/components/molecules/ResourceRefChips";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { LabelsEditor, labelsFromMap, labelsToMap, type LabelEntry } from "@shared/components/organisms/LabelsEditor";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { toast } from "@shared/lib/toast";

interface Props {
  projectId: string;
  nicId: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

interface NicData {
  id: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  subnet_id?: string;
  v4_address_ids?: string[];
  v6_address_ids?: string[];
  security_group_ids?: string[];
}

const labelWithInfo = (text: string, info: string) => (
  <Space size={4}>
    {text}
    <Tooltip title={info}>
      <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
    </Tooltip>
  </Space>
);

export function InlineNetworkInterfaceEditForm({ projectId, nicId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const spec = REGISTRY["network-interfaces"];

  const { data: nic, isLoading } = useQuery({
    queryKey: ["network-interfaces", "detail", nicId],
    queryFn: () => api.get<NicData>(`${spec.apiPath}/${nicId}`),
    enabled: !!nicId,
  });

  // Подсеть NIC → network_id: адреса фильтруем по подсети, SG — по её сети.
  const subnetIdOfNic = nic?.subnet_id;
  const { data: subnetOfNic } = useQuery({
    queryKey: ["subnets", "for-nic-filter", subnetIdOfNic],
    queryFn: () => api.get<{ network_id?: string }>(`${REGISTRY["subnets"].apiPath}/${subnetIdOfNic}`),
    enabled: !!subnetIdOfNic,
    staleTime: 60_000,
  });
  const sgNetworkId = subnetOfNic?.network_id;

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [v4, setV4] = useState<string[]>([]);
  const [v6, setV6] = useState<string[]>([]);
  const [sgs, setSgs] = useState<string[]>([]);
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
    if (!nic || hydrated) return;
    setName(nic.name ?? "");
    setDescription(nic.description ?? "");
    setLabels(labelsFromMap(nic.labels));
    setV4(nic.v4_address_ids ?? []);
    setV6(nic.v6_address_ids ?? []);
    setSgs(nic.security_group_ids ?? []);
    setHydrated(true);
  }, [nic, hydrated]);

  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.update(`${spec.apiPath}/${nicId}`, item),
    onSuccess: (resp) => {
      const opId = extractOperationId(resp);
      if (opId) setPendingOpId(opId);
      else {
        invalidate(spec.id, projectId);
        toast.success(`NIC ${name || nicId} сохранён`);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Сохранить NIC: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      toast.error(`Сохранить NIC: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
      return;
    }
    invalidate(spec.id, projectId);
    toast.success(`NIC ${name || nicId} сохранён`);
    setPendingOpId(null);
    onSuccess?.();
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    if (!nic) return;
    const mask: string[] = [];
    const newLabels = labelsToMap(labels);
    if ((nic.name ?? "") !== name) mask.push("name");
    if ((nic.description ?? "") !== description) mask.push("description");
    if (JSON.stringify(nic.labels ?? {}) !== JSON.stringify(newLabels)) mask.push("labels");
    const origV4 = (nic.v4_address_ids ?? []).slice().sort();
    const origV6 = (nic.v6_address_ids ?? []).slice().sort();
    const origSg = (nic.security_group_ids ?? []).slice().sort();
    if (JSON.stringify(origV4) !== JSON.stringify(v4.slice().sort())) mask.push("v4_address_ids");
    if (JSON.stringify(origV6) !== JSON.stringify(v6.slice().sort())) mask.push("v6_address_ids");
    if (JSON.stringify(origSg) !== JSON.stringify(sgs.slice().sort())) mask.push("security_group_ids");

    if (mask.length === 0) {
      onCancel();
      return;
    }
    mutation.mutate({
      name,
      description,
      labels: newLabels,
      v4_address_ids: v4,
      v6_address_ids: v6,
      security_group_ids: sgs,
      update_mask: mask.map(snakeToCamelPath).join(","),
    });
  };

  if (isLoading || !nic) {
    return (
      <div style={{ padding: 24 }}>
        <Typography.Text type="secondary">Загрузка…</Typography.Text>
      </div>
    );
  }

  return (
    <FormShell specId="network-interfaces" mode="edit" singular={spec.singular}>
      <Form
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "1 1 0" }}
        labelAlign="left"
        colon={false}
        size="middle"
      >
        <Form.Item label={labelWithInfo("Имя", "Имя интерфейса в пределах фолдера. Можно изменять.")} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Описание", "Опциональное описание для людей.")}>
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Метки", "Пары ключ=значение для группировки/фильтрации.")}>
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>

        <Form.Item label={labelWithInfo("Подсеть", "Иммутабельно после создания.")}>
          <Input value={nic.subnet_id ?? ""} disabled />
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
            refFilter={(row) =>
              (row.internal_ipv4_address as { subnet_id?: string } | undefined)?.subnet_id === nic.subnet_id
            }
            createResource="addresses"
            createPresetFields={{
              _address_kind: "internal",
              ...(nic.subnet_id ? { "internal_ipv4_address_spec.subnet_id": nic.subnet_id } : {}),
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
            refFilter={(row) =>
              (row.internal_ipv6_address as { subnet_id?: string } | undefined)?.subnet_id === nic.subnet_id
            }
            createResource="addresses"
            createEditablePresetFields={{ _address_kind: "internal_v6" }}
            createPresetFields={nic.subnet_id ? { "internal_ipv6_address_spec.subnet_id": nic.subnet_id } : undefined}
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
            refFilter={(row) => !!sgNetworkId && row.network_id === sgNetworkId}
            createResource="security-groups"
            createTitle="Создание группы безопасности"
          />
        </Form.Item>
        <FormFooter
          submitLabel="Сохранить"
          submitting={mutation.isPending || !!pendingOpId}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </Form>
    </FormShell>
  );
}
