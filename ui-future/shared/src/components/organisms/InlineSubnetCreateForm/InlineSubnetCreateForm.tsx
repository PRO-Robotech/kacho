// InlineSubnetCreateForm — inline-форма создания подсети, встраиваемая в правую
// панель Network detail вместо "Общее"-Descriptions. Раскладка повторяет
// 2-column horizontal layout (label-left / input-right) с custom-
// виджетами под CIDR, метки и DHCP-collapse.
//
// Wire-format submission ровно как у public AddressService.Create:
//   { project_id, network_id, zone_id, name, description?, labels?,
//     v4_cidr_blocks: [string], v6_cidr_blocks: [string], route_table_id?, dhcp_options? }
//
// v6_cidr_blocks (KAC-68): аналогично v4, но prefix-варианты /48..../128;
// в большинстве случаев /64 (RFC-default для subnet). Оба поля optional —
// допустима v4-only / v6-only / dual-stack подсеть.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Collapse, Form, Input, Select, Space, Tooltip, Typography } from "antd";
import { QuestionCircleOutlined } from "@ant-design/icons";
import { ApiError, api } from "@shared/api/client";
import { extractOperationId } from "@shared/components/molecules/OperationDialog";
import { CidrSection } from "@shared/components/molecules/SubnetCidrChips";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { REGISTRY } from "@shared/lib/resource-registry";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";
import { LabelsEditor, labelsFromEntries, type LabelEntry } from "@shared/components/organisms/LabelsEditor";

interface Props {
  projectId: string;
  // networkId — preset (locked если задан). Если undefined — форма
  // отображает RefSelect "Сеть" как первое поле, user выбирает в форме
  // (отказались от двухшагового flow).
  networkId?: string;
  onCancel: () => void;
  onSuccess?: () => void;
}

function autoName(): string {
  return `subnetwork-${Math.floor(100000 + Math.random() * 900000)}`;
}

export function InlineSubnetCreateForm({ projectId, networkId: presetNetworkId, onCancel, onSuccess }: Props) {
  const invalidate = useInvalidateResourceList();
  const subnetSpec = REGISTRY["subnets"];
  const zoneSpec = REGISTRY["zones"];
  const regionSpec = REGISTRY["regions"];
  const rtSpec = REGISTRY["route-tables"];
  const networkSpec = REGISTRY["networks"];

  // Если networkId preset (передан из контекста — например, "Создать подсеть"
  // из NetworkDetailPage), сеть locked. Иначе — selectable в форме.
  const [networkId, setNetworkId] = useState<string | undefined>(presetNetworkId);
  const networkLocked = !!presetNetworkId;

  // Список Networks для RefSelect (когда preset не задан).
  const { data: netData } = useQuery({
    queryKey: ["networks", "list", projectId],
    queryFn: () =>
      api.list<{ networks: Array<{ id: string; name?: string }> }>(networkSpec.apiPath, {
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !networkLocked,
    staleTime: 30_000,
  });
  const networkOptions = useMemo(
    () =>
      (netData?.networks ?? []).map((n) => ({
        value: n.id,
        label: n.name || n.id,
      })),
    [netData],
  );

  const [name, setName] = useState(() => autoName());
  const [description, setDescription] = useState("");
  const [labels, setLabels] = useState<LabelEntry[]>([]);
  const [zoneId, setZoneId] = useState<string | undefined>(undefined);
  // Размещение подсети: ZONAL (одна зона) либо REGIONAL (весь регион).
  const [placementType, setPlacementType] = useState<"ZONAL" | "REGIONAL">("ZONAL");
  const [regionId, setRegionId] = useState<string | undefined>(undefined);
  const [routeTableId, setRouteTableId] = useState<string | undefined>(undefined);
  // CIDR-блоки храним как массив готовых строк "10.0.0.0/24" (как в edit-вью);
  // визуально — chip-list через SubnetCidrChips (visual parity с SubnetCidrManager).
  const [v4Blocks, setV4Blocks] = useState<string[]>([]);
  const [v6Blocks, setV6Blocks] = useState<string[]>([]);
  const [dhcpDomainName, setDhcpDomainName] = useState("");
  const [dhcpDns, setDhcpDns] = useState<string[]>([]);
  const [dhcpNtp, setDhcpNtp] = useState<string[]>([]);

  // Зоны: глобальный admin-ресурс, без project_id.
  const { data: zoneData } = useQuery({
    queryKey: ["zones", "list"],
    queryFn: () =>
      api.list<{ zones: Array<{ id: string; name?: string }> }>(zoneSpec.apiPath, {
        pageSize: "500",
      }),
    staleTime: 60_000,
  });
  const zoneOptions = useMemo(
    () =>
      (zoneData?.zones ?? []).map((z) => ({
        value: z.id,
        label: z.name || z.id,
      })),
    [zoneData],
  );
  // Default-zone — первая по списку (обычно ru-central1-a).
  useEffect(() => {
    if (!zoneId && zoneOptions.length > 0) {
      setZoneId(zoneOptions[0].value);
    }
  }, [zoneId, zoneOptions]);

  // Регионы (для REGIONAL-размещения) — geo admin-ресурс, без project_id.
  const { data: regionData } = useQuery({
    queryKey: ["regions", "list"],
    queryFn: () =>
      api.list<{ regions: Array<{ id: string; name?: string }> }>(regionSpec.apiPath, {
        pageSize: "500",
      }),
    staleTime: 60_000,
  });
  const regionOptions = useMemo(
    () => (regionData?.regions ?? []).map((r) => ({ value: r.id, label: r.name || r.id })),
    [regionData],
  );
  useEffect(() => {
    if (placementType === "REGIONAL" && !regionId && regionOptions.length > 0) {
      setRegionId(regionOptions[0].value);
    }
  }, [placementType, regionId, regionOptions]);

  // RouteTables: project-scoped, ещё фильтруем по network.
  const { data: rtData } = useQuery({
    queryKey: ["route-tables", "list", projectId, networkId],
    queryFn: () =>
      api.list<{ route_tables: Array<Record<string, unknown>> }>(rtSpec.apiPath, {
        project_id: projectId,
        pageSize: "500",
      }),
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

  // Doppler-flow: ждём op.done через polling вместо banner.
  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  const { data: op } = useOperation(pendingOpId);

  const mutation = useMutation({
    mutationFn: (item: unknown) => api.create(subnetSpec.apiPath, item),
    onSuccess: (resp) => {
      const id = extractOperationId(resp);
      if (id) {
        setPendingOpId(id);
      } else {
        invalidate(subnetSpec.id, projectId);
        onSuccess?.();
        onCancel();
      }
    },
    onError: (err) => {
      const m = err instanceof ApiError ? `${err.code}: ${err.message}` : (err as Error).message;
      toast.error(`Создать подсеть: ${m}`);
    },
  });

  useEffect(() => {
    if (!pendingOpId || !op?.done) return;
    if (op.error) {
      // На ошибку — НЕ вызываем onCancel/onSuccess: остаёмся на форме,
      // user видит toast с причиной (например CIDR overlap) и может
      // поправить ввод. Раньше любой результат закрывал форму — баг.
      toast.error(`Создать подсеть: ${op.error.message ?? "ошибка"}`);
      setPendingOpId(null);
      return;
    }
    invalidate(subnetSpec.id, projectId);
    toast.success(`Подсеть ${name} создана`);
    setPendingOpId(null);
    onSuccess?.();
    onCancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code]);

  const submit = () => {
    if (!networkId) {
      toast.error("Выберите сеть для подсети.");
      return;
    }
    if (placementType === "ZONAL" && !zoneId) {
      toast.error("Выберите зону доступности.");
      return;
    }
    if (placementType === "REGIONAL" && !regionId) {
      toast.error("Выберите регион.");
      return;
    }
    // CIDR-строки уже валидированы и добавлены через SubnetCidrChips —
    // используем как есть. Хотя бы одно семейство (v4 / v6 / оба) обязательно.
    if (v4Blocks.length === 0 && v6Blocks.length === 0) {
      toast.error("Добавьте хотя бы один CIDR (IPv4 или IPv6).");
      return;
    }
    const labelMap = labelsFromEntries(labels);
    const dhcp =
      dhcpDomainName || dhcpDns.length > 0 || dhcpNtp.length > 0
        ? {
            domain_name: dhcpDomainName || undefined,
            domain_name_servers: dhcpDns.length > 0 ? dhcpDns : undefined,
            ntp_servers: dhcpNtp.length > 0 ? dhcpNtp : undefined,
          }
        : undefined;

    const payload: Record<string, unknown> = {
      project_id: projectId,
      network_id: networkId,
      placement_type: placementType,
      zone_id: placementType === "ZONAL" ? zoneId : undefined,
      region_id: placementType === "REGIONAL" ? regionId : undefined,
      name,
      description: description || undefined,
      labels: Object.keys(labelMap).length > 0 ? labelMap : undefined,
      v4_cidr_blocks: v4Blocks.length > 0 ? v4Blocks : undefined,
      v6_cidr_blocks: v6Blocks.length > 0 ? v6Blocks : undefined,
      route_table_id: routeTableId || undefined,
      dhcp_options: dhcp,
    };

    mutation.mutate(payload);
  };

  return (
    <FormShell specId="subnets" mode="create" singular={subnetSpec.singular}>
      <Form
        layout="horizontal"
        labelCol={{ flex: "200px" }}
        wrapperCol={{ flex: "1 1 0" }}
        labelAlign="left"
        colon={false}
        size="middle"
      >
        <Form.Item label="Сеть" required>
          <Select
            showSearch
            value={networkId}
            onChange={(v) => setNetworkId(v)}
            options={networkOptions}
            placeholder="Выберите сеть"
            optionFilterProp="label"
            disabled={networkLocked}
          />
        </Form.Item>

        <Form.Item label="Имя" required>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="subnetwork-..." />
        </Form.Item>

        <Form.Item label="Описание">
          <Input.TextArea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
        </Form.Item>

        <Form.Item label="Метки">
          <LabelsEditor value={labels} onChange={setLabels} />
        </Form.Item>

        <Form.Item label="Размещение" required>
          <Select
            value={placementType}
            onChange={(v) => setPlacementType(v)}
            options={[
              { value: "ZONAL", label: "ZONAL — в одной зоне доступности" },
              { value: "REGIONAL", label: "REGIONAL — во всём регионе" },
            ]}
          />
        </Form.Item>

        {placementType === "ZONAL" ? (
          <Form.Item label="Зона доступности" required>
            <Select value={zoneId} onChange={setZoneId} options={zoneOptions} placeholder="Выберите зону" />
          </Form.Item>
        ) : (
          <Form.Item label="Регион" required>
            <Select value={regionId} onChange={setRegionId} options={regionOptions} placeholder="Выберите регион" />
          </Form.Item>
        )}

        <Form.Item label="Таблица маршрутизации">
          <Select
            value={routeTableId}
            onChange={(v) => setRouteTableId(v)}
            options={rtOptions}
            allowClear
            placeholder="Выберите таблицу маршрутизации (опц.)"
          />
        </Form.Item>

        {/* IPv4 и IPv6 CIDR — ДВА отдельных поля (как в edit). Хотя бы одно
            семейство обязательно (v4-only / v6-only / dual-stack). */}
        <Form.Item
          label={
            <Space size={4}>
              IPv4 CIDR
              <Tooltip title="IPv4 CIDR-блоки подсети. Введите CIDR с префиксом, например 10.0.0.0/24, и нажмите Add. Можно оставить пустым для IPv6-only подсети.">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
          required
        >
          <CidrSection kind="v4" blocks={v4Blocks} onChange={setV4Blocks} hideTitle />
        </Form.Item>

        <Form.Item
          label={
            <Space size={4}>
              IPv6 CIDR
              <Tooltip title="IPv6 CIDR-блоки подсети. Введите CIDR с префиксом, например 2001:db8::/64, и нажмите Add. Можно оставить пустым для IPv4-only подсети.">
                <QuestionCircleOutlined style={{ color: "rgba(255,255,255,0.45)" }} />
              </Tooltip>
            </Space>
          }
        >
          <CidrSection kind="v6" blocks={v6Blocks} onChange={setV6Blocks} hideTitle />
        </Form.Item>

        <div style={{ margin: "16px 0" }}>
          <Collapse
            ghost
            items={[
              {
                key: "dhcp",
                label: <Typography.Text strong>Настройки DHCP</Typography.Text>,
                children: (
                  <Space direction="vertical" size={12} style={{ width: "100%" }}>
                    <Form.Item label="Domain name" style={{ marginBottom: 0 }}>
                      <Input
                        value={dhcpDomainName}
                        onChange={(e) => setDhcpDomainName(e.target.value)}
                        placeholder="<domain>"
                      />
                    </Form.Item>
                    <Form.Item label="DNS servers" style={{ marginBottom: 0 }}>
                      <Select
                        mode="tags"
                        value={dhcpDns}
                        onChange={setDhcpDns}
                        tokenSeparators={[",", " "]}
                        placeholder="<ip-адреса DNS>"
                      />
                    </Form.Item>
                    <Form.Item label="NTP servers" style={{ marginBottom: 0 }}>
                      <Select
                        mode="tags"
                        value={dhcpNtp}
                        onChange={setDhcpNtp}
                        tokenSeparators={[",", " "]}
                        placeholder="<NTP-серверы>"
                      />
                    </Form.Item>
                  </Space>
                ),
              },
            ]}
          />
        </div>
        <FormFooter
          submitLabel="Создать подсеть"
          submitting={mutation.isPending || pendingOpId !== null}
          onSubmit={submit}
          onCancel={onCancel}
        />
      </Form>
    </FormShell>
  );
}
