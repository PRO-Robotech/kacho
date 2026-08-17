// AddressVpcCascader — выбор Address для линка VIP через каскадер [VPC → Address].
// Даёт пользователю контекст «в какой сети (VPC) живёт адрес»: internal-адреса
// сгруппированы по network своей подсети (address → subnet → network), external
// (публичные) — в отдельной группе «Публичные адреса». Рядом — кнопка «Создать
// адрес», открывающая inline-форму создания Address (тот же паттерн, что RefSelect).
//
// Кандидаты фильтруются переданным addressFilter (family/сфера/placement из
// NlbVipSourceField). Значение — плоский address_id; каскадер резолвит его в путь
// [networkKey, addressId] для controlled-режима.

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Cascader, Modal } from "antd";
import { Plus } from "lucide-react";
import { api } from "@/api/client";
import { useProjectStore } from "@/lib/context-store";
import { getResource } from "@/lib/resource-registry";
import { useDebouncedValue } from "@shared/lib/list-search";
import { pickerScopeOfSpec } from "@shared/lib/picker-search";
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

// groupLabelOf — подпись группы каскадера. Отдельной функцией, потому что её
// зовут двое: сборка вариантов и восстановление ветки выбранного адреса, когда
// сужение унесло его из ответа. Две копии этой строки разошлись бы молча.
function groupLabelOf(key: string, netName: Map<string, string>): string {
  return key === PUBLIC_KEY ? "Публичные адреса" : `Сеть · ${netName.get(key) || key}`;
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

  // Введённое в каскадере. Область поиска решает, что с ним делать: спросить
  // владельца адресов либо честно сказать, что сужаются только загруженные
  // варианты (#528). Раньше ввод не покидал вкладку: адреса читались одной
  // страницей в 500 строк, а поле отвечало «нет совпадений» — утверждение об
  // отсутствии адреса, которого никто не проверял, и опровергнуть его
  // пользователю нечем: «показать ещё» у выпадающего списка нет.
  //
  // Сужается ТОЛЬКО список адресов, и это здесь законно: группы-сети выводятся
  // из самих найденных адресов (адрес → подсеть → сеть), поэтому суженный ответ
  // остаётся связным деревом — предки не теряются. Списки подсетей и сетей ввод
  // не трогает: они тут не кандидаты, а справочник имён.
  const scope = pickerScopeOfSpec(addressSpec);
  const [term, setTerm] = useState("");
  const debouncedTerm = useDebouncedValue(term, scope.asksServer ? 250 : 0);
  const serverQuery = scope.asksServer ? scope.query(debouncedTerm) : {};
  // Ключ запроса несёт ввод ТОЛЬКО когда сужает сервер: иначе каждое нажатие
  // клавиши сбрасывало бы кэш и перечитывало один и тот же список.
  const queryTermKey = scope.asksServer ? (serverQuery.filter ?? "") : "";

  const listOpts = (key: string, path: string) => ({
    queryKey: ["ref", key, project?.id ?? null] as const,
    queryFn: () =>
      api.list<Record<string, Array<Record<string, unknown>>>>(path, { project_id: project!.id, pageSize: "500" }),
    enabled: !!project,
    staleTime: 30_000,
  });

  const {
    data: addrData,
    isLoading: addrLoading,
    refetch,
  } = useQuery({
    queryKey: ["ref", "addresses", project?.id ?? null, queryTermKey],
    queryFn: () =>
      api.list<Record<string, Array<Record<string, unknown>>>>("/vpc/v1/addresses", {
        ...serverQuery,
        project_id: project!.id,
        pageSize: "500",
      }),
    enabled: !!project,
    staleTime: 30_000,
  });
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
  const { options, pathOf, metaOf } = useMemo(() => {
    const groups = new Map<string, { value: string; label: string }[]>();
    const path = new Map<string, [string, string]>();
    const meta = new Map<string, { groupKey: string; label: string }>();
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
      const label = `${(row.name as string) || id}${ip ? ` · ${ip}` : ""}`;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push({ value: id, label });
      path.set(id, [key, id]);
      meta.set(id, { groupKey: key, label });
    });
    const opts = [...groups.entries()].map(([key, children]) => ({
      value: key,
      label: groupLabelOf(key, netName),
      children,
    }));
    return { options: opts, pathOf: path, metaOf: meta };
  }, [addrData, subnetToNet, netName, addressFilter, family, type]);

  // Выбранный адрес обязан пережить сужение: край отвечает по ВВОДУ, и уже
  // сделанный выбор в этот ответ попадать не обязан. Без запоминания каскадер
  // потерял бы и ветку, и подпись — и показал бы сырой `adr-…` вместо имени,
  // ровно то, что канон консоли (правило 2) запрещает. Значение формы при этом
  // не меняется: от сужения СПИСКА выбор не зависит.
  const chosenRef = useRef<{ id: string; groupKey: string; label: string } | null>(null);
  const chosenMeta = value ? metaOf.get(value) : undefined;
  if (value && chosenMeta) chosenRef.current = { id: value, ...chosenMeta };
  const kept = value && !chosenMeta && chosenRef.current?.id === value ? chosenRef.current : null;

  const shownOptions = (() => {
    if (!kept) return options;
    const child = { value: kept.id, label: kept.label };
    const group = options.find((g) => g.value === kept.groupKey);
    if (group) return options.map((g) => (g === group ? { ...g, children: [child, ...g.children] } : g));
    return [{ value: kept.groupKey, label: groupLabelOf(kept.groupKey, netName), children: [child] }, ...options];
  })();

  const cascaderValue = value ? (pathOf.get(value) ?? (kept ? [kept.groupKey, kept.id] : undefined)) : undefined;

  return (
    <div style={{ display: "flex", gap: 8 }}>
      <Cascader
        options={shownOptions}
        value={cascaderValue}
        onChange={(val) => onChange((val?.[1] as string) || undefined)}
        placeholder={placeholder ?? "Выберите сеть (VPC) → адрес"}
        // Сузил сервер — клиент НЕ пересеивает: подпись варианта склеена из
        // имени и IP, и повторное сужение по ней вычло бы из ответа края
        // строки, которые он прислал именно по этому вводу. Не сужает —
        // остаётся сеево по умолчанию, то есть прежнее поведение.
        showSearch={scope.asksServer ? { onSearch: setTerm, filter: () => true } : { onSearch: setTerm }}
        title={scope.notice}
        // Пустой ответ обязан называть свою ОБЛАСТЬ. Здесь и жила ложь: «нет
        // совпадений» на месте «нет среди загруженных».
        notFoundContent={addrLoading ? undefined : scope.emptyText}
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
