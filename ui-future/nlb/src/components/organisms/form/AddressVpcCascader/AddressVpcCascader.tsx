// AddressVpcCascader — выбор Address для линка VIP через каскадер [VPC → Address].
// Даёт пользователю контекст «в какой сети (VPC) живёт адрес»: internal-адреса
// сгруппированы по network своей подсети (address → subnet → network), external
// (публичные) — в отдельной группе «Публичные адреса». Рядом — кнопка «Создать
// адрес», открывающая inline-форму создания Address (тот же паттерн, что RefSelect).
//
// Кандидаты фильтруются переданным addressFilter (family/сфера/placement из
// NlbVipSourceField). Значение — плоский address_id; каскадер резолвит его в путь
// [networkKey, addressId] для controlled-режима.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Cascader, Modal } from "antd";
import { Plus } from "lucide-react";
import { api } from "@/api/client";
import { useProjectStore } from "@/lib/context-store";
import { getResource } from "@/lib/resource-registry";
import { InlineResourceCreateForm } from "@/components/organisms/InlineResourceCreateForm";
import { FormBareProvider } from "@/components/organisms/form/FormShell";
import { addressInternalSubnetId } from "@/components/organisms/form/NlbVipSourceField/NlbVipSourceField";

type Family = "v4" | "v6";
const PUBLIC_KEY = "__public__";

interface Props {
  family: Family;
  type: string;
  addressFilter: (row: Record<string, unknown>) => boolean;
  value?: string;
  onChange: (id: string | undefined) => void;
  placeholder?: string;
}

// addressIp — tenant-адрес выбранного семейства для подписи опции (internal/external).
function addressIp(family: Family, row: Record<string, unknown>): string {
  const keys =
    family === "v4"
      ? ["internal_ipv4_address", "external_ipv4_address"]
      : ["internal_ipv6_address", "external_ipv6_address"];
  for (const k of keys) {
    const a = row[k] as { address?: string } | undefined;
    if (a?.address) return a.address;
  }
  return "";
}

export function AddressVpcCascader({ family, type, addressFilter, value, onChange, placeholder }: Props) {
  const project = useProjectStore((s) => s.project);
  const [creating, setCreating] = useState(false);
  const addressSpec = getResource("addresses");

  const listOpts = (key: string, path: string) => ({
    queryKey: ["ref", key, project?.id ?? null] as const,
    queryFn: () =>
      api.list<Record<string, Array<Record<string, unknown>>>>(path, { project_id: project!.id, pageSize: "500" }),
    enabled: !!project,
    staleTime: 30_000,
  });

  const { data: addrData, refetch } = useQuery(listOpts("addresses", "/vpc/v1/addresses"));
  const { data: subnetData } = useQuery(listOpts("subnets-all", "/vpc/v1/subnets"));
  const { data: netData } = useQuery(listOpts("networks", "/vpc/v1/networks"));

  const subnetToNet = useMemo(() => {
    const m = new Map<string, string>();
    (subnetData?.subnets ?? []).forEach((s) => m.set(s.id as string, (s.network_id as string) || ""));
    return m;
  }, [subnetData]);

  const netName = useMemo(() => {
    const m = new Map<string, string>();
    (netData?.networks ?? []).forEach((n) => m.set(n.id as string, (n.name as string) || (n.id as string)));
    return m;
  }, [netData]);

  // Группировка кандидатов: internal → по network подсети; external → «Публичные».
  const { options, pathOf } = useMemo(() => {
    const groups = new Map<string, { value: string; label: string }[]>();
    const path = new Map<string, [string, string]>();
    (addrData?.addresses ?? []).filter(addressFilter).forEach((row) => {
      const id = row.id as string;
      let key: string;
      if (type === "EXTERNAL") {
        key = PUBLIC_KEY;
      } else {
        const sid = addressInternalSubnetId(family, row);
        const netId = sid ? subnetToNet.get(sid) : undefined;
        if (!netId) return; // internal-адрес без резолва сети — не показываем
        key = netId;
      }
      const ip = addressIp(family, row);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push({ value: id, label: `${(row.name as string) || id}${ip ? ` · ${ip}` : ""}` });
      path.set(id, [key, id]);
    });
    const opts = [...groups.entries()].map(([key, children]) => ({
      value: key,
      label: key === PUBLIC_KEY ? "Публичные адреса" : `Сеть · ${netName.get(key) || key}`,
      children,
    }));
    return { options: opts, pathOf: path };
  }, [addrData, subnetToNet, netName, addressFilter, family, type]);

  const cascaderValue = value ? pathOf.get(value) : undefined;

  return (
    <div style={{ display: "flex", gap: 8 }}>
      <Cascader
        options={options}
        value={cascaderValue}
        onChange={(val) => onChange((val?.[1] as string) || undefined)}
        placeholder={placeholder ?? "Выберите сеть (VPC) → адрес"}
        showSearch
        allowClear
        expandTrigger="hover"
        style={{ flex: 1 }}
        displayRender={(labels) => labels[labels.length - 1]}
      />
      {addressSpec && (
        <Button icon={<Plus size={16} />} onClick={() => setCreating(true)}>
          Создать адрес
        </Button>
      )}
      {creating && addressSpec && (
        <Modal
          open
          footer={null}
          onCancel={() => setCreating(false)}
          width={720}
          destroyOnClose
          title={null}
          styles={{ body: { padding: "12px 24px 20px" } }}
        >
          <FormBareProvider>
            <InlineResourceCreateForm
              spec={addressSpec}
              ctx={{ projectId: project?.id, accountId: project?.accountId }}
              projectId={project?.id ?? null}
              title="Создать адрес"
              onCancel={() => setCreating(false)}
              onSuccess={() => {
                setCreating(false);
                // после create — refetch и авто-выбор нового адреса (diff по id).
                const before = new Set((addrData?.addresses ?? []).map((a) => a.id as string));
                void refetch().then((r) => {
                  const fresh = (r.data?.addresses ?? []).find((a) => !before.has(a.id as string));
                  if (fresh) onChange(fresh.id as string);
                });
              }}
            />
          </FormBareProvider>
        </Modal>
      )}
    </div>
  );
}
