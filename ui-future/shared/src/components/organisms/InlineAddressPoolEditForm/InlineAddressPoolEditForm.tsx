// InlineAddressPoolEditForm — форма редактирования AddressPool,
// визуально парная к InlineAddressPoolCreateForm: тот же horizontal Form
// layout. Wire-format:
//   PATCH /vpc/v1/addressPools/{id}  { name, description, ..., update_mask }
//
// kind / zone_id — immutable (disabled в форме).
//
// KAC-269: AddressPool.Update БОЛЬШЕ НЕ меняет CIDR (proto убрал
// v4/v6_cidr_blocks + replace_* из UpdateAddressPoolRequest). CIDR-блоки
// управляются отдельными RPC (:addCidrBlocks / :removeCidrBlocks) через
// AddressPoolCidrManager (sync, мутирует сразу, не через эту Update-форму).

import { useEffect, useState } from "react";
import { snakeToCamelPath } from "@shared/lib/update-mask";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Form, Input, InputNumber, Select, Space, Switch, Tooltip, Typography } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { AddressPoolCidrManager } from "@shared/components/organisms/AddressPoolCidrManager";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

interface Props {
  poolId: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

interface PoolData {
  id: string;
  name?: string;
  description?: string;
  kind?: string;
  zone_id?: string;
  v4_cidr_blocks?: string[];
  v6_cidr_blocks?: string[];
  is_default?: boolean;
  selector_priority?: number;
}

// KAC-70: AddressPoolKind — единственный валидный вариант EXTERNAL_PUBLIC.
// EXTERNAL_TEST = 2 / RESERVED_INTERNAL = 100 удалены из proto enum
// (`reserved 2, 100` в kacho.cloud.vpc.v1.AddressPoolKind).
const KIND_OPTIONS = [{ value: "EXTERNAL_PUBLIC", label: "External public" }];

export function InlineAddressPoolEditForm({ poolId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const spec = REGISTRY["address-pools"];

  const { data: pool, isLoading } = useQuery({
    queryKey: ["address-pools", "detail", poolId],
    queryFn: () => api.get<PoolData>(`${spec.apiPath}/${poolId}`),
    enabled: !!poolId,
  });

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isDefault, setIsDefault] = useState(false);
  const [selectorPriority, setSelectorPriority] = useState<number>(0);
  const [hydrated, setHydrated] = useState(false);

  // Hydrate из загруженных данных. CIDR-блоки НЕ в state формы (KAC-269: их не
  // меняет Update) — они читаются напрямую из pool для AddressPoolCidrManager.
  useEffect(() => {
    if (!pool || hydrated) return;
    setName(pool.name ?? "");
    setDescription(pool.description ?? "");
    setIsDefault(!!pool.is_default);
    setSelectorPriority(pool.selector_priority ?? 0);
    setHydrated(true);
  }, [pool, hydrated]);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.update(`${spec.apiPath}/${poolId}`, item),
    onSuccess: () => {
      invalidate(spec.id, null);
      toast.success(`Пул адресов ${name || poolId} обновлён`);
      onSuccess?.();
      onCancel();
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Сохранить пул адресов: ${m}`);
    },
  });

  const submit = () => {
    if (!pool) return;

    // KAC-269: CIDR-блоки больше НЕ в Update — только name/description/is_default/
    // selector_priority. CIDR управляется AddressPoolCidrManager (:addCidrBlocks /
    // :removeCidrBlocks). proto убрал v4/v6_cidr_blocks + replace_* из
    // UpdateAddressPoolRequest, так что включать их в mask нельзя.
    const mask: string[] = [];
    if ((pool.name ?? "") !== name) mask.push("name");
    if ((pool.description ?? "") !== description) mask.push("description");
    if ((pool.is_default ?? false) !== isDefault) mask.push("is_default");
    if ((pool.selector_priority ?? 0) !== selectorPriority) mask.push("selector_priority");

    if (mask.length === 0) {
      onCancel();
      return;
    }
    const payload: Record<string, unknown> = {
      name,
      description: description || "",
      is_default: isDefault,
      selector_priority: selectorPriority,
      update_mask: mask.map(snakeToCamelPath).join(","),
    };
    mutation.mutate(payload);
  };

  if (isLoading || !pool) {
    return (
      <div style={{ padding: 24 }}>
        <Typography.Text type="secondary">Загрузка…</Typography.Text>
      </div>
    );
  }

  return (
    <FormShell specId="address-pools" mode="edit" singular={spec.singular}>
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

        <Form.Item label="Тип">
          <Select value={pool.kind ?? "EXTERNAL_PUBLIC"} options={KIND_OPTIONS} disabled />
        </Form.Item>

        <Form.Item label="Зона">
          <Input value={pool.zone_id || "(глобальный)"} disabled />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              IPv4 и IPv6 CIDR
              <Tooltip title="Блоки IPv4 и/или IPv6, из которых аллоцируются адреса. Добавление/удаление применяется сразу (отдельный RPC), не через «Сохранить». Удалить блок с уже выделенными адресами нельзя.">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <AddressPoolCidrManager
            poolId={poolId}
            v4Blocks={pool.v4_cidr_blocks ?? []}
            v6Blocks={pool.v6_cidr_blocks ?? []}
          />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              Default
              <Tooltip title="Один is_default=true на (zone, kind).">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <Switch checked={isDefault} onChange={setIsDefault} />
        </Form.Item>

        <Form.Item label="Selector priority">
          <InputNumber
            value={selectorPriority}
            onChange={(v) => setSelectorPriority((v as number) ?? 0)}
            style={{ width: "100%" }}
          />
        </Form.Item>
        <FormFooter submitLabel="Сохранить" submitting={mutation.isPending} onSubmit={submit} onCancel={onCancel} />
      </Form>
    </FormShell>
  );
}
