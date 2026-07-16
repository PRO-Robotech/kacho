// InlineAddressPoolCreateForm — форма создания AddressPool с тем же
// layout, что у InlineSubnetCreateForm: horizontal Form, IPv4/IPv6 CIDR через
// chip-list виджет (SubnetCidrChips). KAC-60 разрешает v4/v6/dual-stack пулы.
//
// Wire-format submission:
//   POST /vpc/v1/addressPools
//   { name, description?, kind, zone_id?, v4_cidr_blocks: [string],
//     v6_cidr_blocks: [string], is_default?, selector_priority?, selector_labels? }
//
// KAC-71: cidr_blocks разделён на v4_cidr_blocks + v6_cidr_blocks (parity с
// Subnet, явная family-семантика). Backend требует хотя бы одно семейство
// непустым (REQ-IPL-CR-04).

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, InputNumber, Select, Space, Switch, Tooltip } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { SubnetCidrChips } from "@shared/components/molecules/SubnetCidrChips";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

interface Props {
  onCancel: () => void;
  onSuccess?: () => void;
}

// KAC-70: AddressPoolKind — единственный валидный вариант EXTERNAL_PUBLIC.
// EXTERNAL_TEST = 2 / RESERVED_INTERNAL = 100 удалены из proto enum
// (`reserved 2, 100` в kacho.cloud.vpc.v1.AddressPoolKind).
const KIND_OPTIONS = [{ value: "EXTERNAL_PUBLIC", label: "External public" }];

export function InlineAddressPoolCreateForm({ onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const spec = REGISTRY["address-pools"];
  const zoneSpec = REGISTRY["zones"];

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<string>("EXTERNAL_PUBLIC");
  const [zoneId, setZoneId] = useState<string | undefined>(undefined);
  const [v4Blocks, setV4Blocks] = useState<string[]>([]);
  const [v6Blocks, setV6Blocks] = useState<string[]>([]);
  const [isDefault, setIsDefault] = useState(false);
  const [selectorPriority, setSelectorPriority] = useState<number>(0);

  // Zones — глобальный admin-ресурс.
  const { data: zoneData } = useQuery({
    queryKey: ["zones", "list"],
    queryFn: () =>
      api.list<{ zones: Array<{ id: string; name?: string }> }>(zoneSpec.apiPath, {
        pageSize: "500",
      }),
    staleTime: 60_000,
  });
  const zoneOptions = useMemo(
    () => [
      { value: "", label: "(глобальный — без зоны)" },
      ...(zoneData?.zones ?? []).map((z) => ({
        value: z.id,
        label: z.name || z.id,
      })),
    ],
    [zoneData],
  );
  useEffect(() => {
    if (zoneId === undefined && zoneOptions.length > 0) {
      setZoneId("");
    }
  }, [zoneId, zoneOptions]);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(spec.apiPath, item),
    onSuccess: () => {
      invalidate(spec.id, null);
      toast.success(`Пул адресов ${name || "(без имени)"} создан`);
      onSuccess?.();
      onCancel();
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Создать пул адресов: ${m}`);
    },
  });

  const submit = () => {
    if (v4Blocks.length === 0 && v6Blocks.length === 0) {
      toast.error("Добавьте хотя бы один CIDR (IPv4 или IPv6).");
      return;
    }
    const payload: Record<string, unknown> = {
      name: name || undefined,
      description: description || undefined,
      kind,
      zone_id: zoneId || undefined,
      v4_cidr_blocks: v4Blocks,
      v6_cidr_blocks: v6Blocks,
      is_default: isDefault,
      selector_priority: selectorPriority,
    };
    mutation.mutate(payload);
  };

  return (
    <FormShell specId="address-pools" mode="create" singular={spec.singular}>
      <Form
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "1 1 0" }}
        labelAlign="left"
        colon={false}
        size="middle"
      >
        <Form.Item label="Имя">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="pool-public-zone-a" />
        </Form.Item>

        <Form.Item label="Описание">
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} />
        </Form.Item>

        <Form.Item label="Тип" required>
          <Select value={kind} onChange={setKind} options={KIND_OPTIONS} placeholder="Выберите тип пула" />
        </Form.Item>

        <Form.Item label="Зона">
          <Select value={zoneId} onChange={setZoneId} options={zoneOptions} placeholder="Выберите зону" />
        </Form.Item>

        <Form.Item
          required
          label={
            <Space size={4}>
              IPv4 и IPv6 CIDR
              <Tooltip title="Блоки IPv4 (например, 198.51.100.0/24) и/или IPv6 (например, 2001:db8::/64), из которых аллоцируются адреса. Хотя бы одно семейство обязательно.">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <SubnetCidrChips v4Blocks={v4Blocks} onV4Change={setV4Blocks} v6Blocks={v6Blocks} onV6Change={setV6Blocks} />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              Default
              <Tooltip title="Один is_default=true на (zone, kind). Default-пул используется когда явный pool не задан.">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <Switch checked={isDefault} onChange={setIsDefault} />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              Selector priority
              <Tooltip title="Tie-break при равенстве specificity. Higher wins.">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <InputNumber
            value={selectorPriority}
            onChange={(v) => setSelectorPriority((v as number) ?? 0)}
            style={{ width: "100%" }}
          />
        </Form.Item>
        <FormFooter
          submitLabel="Создать пул адресов"
          submitting={mutation.isPending}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </Form>
    </FormShell>
  );
}
