// NlbVipSourceField — выбор источника VIP балансировщика пофамильно (v4/v6).
// Балансировщик несёт один VIP на семейство; источник каждого семейства —
// oneof:
//   • INTERNAL:
//       — «Из подсети (авто)»  → subnet_id: VIP выделяется из подсети (placement
//          подсети обязан совпадать с placement балансировщика);
//       — «Линк адреса»        → address_id: линк заранее созданного internal Address.
//   • EXTERNAL:
//       — «Публичный (авто)»   → public {}: платформенный public IP;
//       — «Линк адреса»        → address_id: линк заранее созданного public Address.
//
// Раскладка — одна строка на семейство: слева единый label («IPv4 Адрес» /
// «IPv6 Адрес»), справа — переключатель режима (segmented) и соответствующий
// селектор (без собственных под-лейблов). Отдельного enable-тоглера семейства
// НЕТ: семейство считается заданным, если у активного режима есть значение
// (subnet_id / address_id непусты, либо режим public). Пустое семейство целиком
// опускается в wire — так пустой addressId/subnetId никогда не уходит на бэкенд.
//
// Кандидаты фильтруются по placement балансировщика:
//   • подсеть-источник — только подсети совпадающего placement_type;
//   • Address-линк (INTERNAL) — только адреса, чья internal-подсеть того же
//     placement (family-совпадение + subnet_id ∈ множества подсетей placement).
//
// UI-представление хранится в obj.vip_source (с дискриминатором режима `_*_mode`);
// sanitize ресурса load-balancers собирает wire-форму v4_source/v6_source через
// buildVipSourceOrNull (ровно один кейс oneof на непустое семейство).
//
// NlbDisabledZonesField — deny-list зон REGIONAL-балансировщика (drain): зоны,
// из которых anycast-VIP не анонсируется. Multi-select зон региона балансировщика.

import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Form, Segmented, Select, Typography } from "antd";
import { api } from "@/api/client";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { AddressVpcCascader } from "@/components/organisms/form/AddressVpcCascader";
import { ImmutableField } from "@/components/organisms/form/ImmutableField";
import { useProjectStore } from "@/lib/context-store";
import { getByPath, setByPath } from "@/lib/path";

type Family = "v4" | "v6";
// VipMode — режим источника VIP семейства:
//   subnet  — авто-аллокация из подсети (INTERNAL);
//   address — линк существующего Address (INTERNAL internal / EXTERNAL public);
//   public  — платформенный public IP (EXTERNAL).
export type VipMode = "subnet" | "address" | "public";

interface Props {
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  editMode?: boolean;
}

const FAMILY_LABEL: Record<Family, string> = { v4: "IPv4 Адрес", v6: "IPv6 Адрес" };

// Единый layout горизонтальных строк секции «Источник VIP»: label слева 200px,
// контрол справа — паритет с ResourceFormBody.
const ROW_FORM_PROPS = {
  component: false as const,
  layout: "horizontal" as const,
  labelCol: { flex: "200px" },
  wrapperCol: { flex: "1 1 0" },
  labelAlign: "left" as const,
  colon: false,
  size: "middle" as const,
};

// familyIpVersion — UI-дискриминатор семейства → enum Address.IpVersion на проводе.
export function familyIpVersion(family: Family): "IPV4" | "IPV6" {
  return family === "v4" ? "IPV4" : "IPV6";
}

// effectiveVipMode — нормализует режим под схему балансировщика: INTERNAL
// допускает {subnet, address} (default subnet), EXTERNAL — {public, address}
// (default public). Устаревший режим (после смены type) схлопывается в валидный.
export function effectiveVipMode(type: string, mode: string | undefined): VipMode {
  const valid: VipMode[] = type === "EXTERNAL" ? ["public", "address"] : ["subnet", "address"];
  const def: VipMode = type === "EXTERNAL" ? "public" : "subnet";
  return valid.includes(mode as VipMode) ? (mode as VipMode) : def;
}

// buildVipSource — собирает wire-oneof одного семейства из UI-представления:
// ровно один из subnet_id / address_id / public {}. Режим нормализуется под type.
// Не проверяет непустоту значения (см. buildVipSourceOrNull для guard).
export function buildVipSource(
  type: string,
  mode: string | undefined,
  fam: Record<string, unknown> | undefined,
): Record<string, unknown> {
  const em = effectiveVipMode(type, mode);
  if (em === "public") return { public: {} };
  if (em === "address") return { address_id: (fam?.address_id as string) || "" };
  return { subnet_id: (fam?.subnet_id as string) || "" };
}

// buildVipSourceOrNull — как buildVipSource, но возвращает null, если активный
// режим не заполнен (пустой subnet_id / address_id). Так семейство без выбора
// целиком опускается в wire, а не уходит как {address_id:""} / {subnet_id:""},
// который бэкенд отвергает («Illegal argument addressId»). Режим public всегда
// валиден (VIP выделяется платформой).
export function buildVipSourceOrNull(
  type: string,
  mode: string | undefined,
  fam: Record<string, unknown> | undefined,
): Record<string, unknown> | null {
  const em = effectiveVipMode(type, mode);
  if (em === "public") return { public: {} };
  if (em === "address") return (fam?.address_id as string) ? { address_id: fam!.address_id } : null;
  return (fam?.subnet_id as string) ? { subnet_id: fam!.subnet_id } : null;
}

// subnetPlacementMatches — кандидат-подсеть подходит для источника VIP, только
// если её placement совпадает с placement балансировщика. Legacy-подсети без
// placement_type трактуются как ZONAL.
export function subnetPlacementMatches(placement: string) {
  return (row: Record<string, unknown>): boolean => {
    const pt = (row.placement_type as string | undefined) || "ZONAL";
    return pt === placement;
  };
}

// linkAddressFilter — кандидат-Address для линка подходит, только если его сфера
// совпадает со схемой балансировщика (internal ⟺ INTERNAL, external ⟺ EXTERNAL)
// и семейство — с целевым слотом (v4_source → IPv4).
export function linkAddressFilter(type: string, family: Family) {
  const wantExternal = type === "EXTERNAL";
  return (row: Record<string, unknown>): boolean => {
    if (family === "v4") {
      return wantExternal ? row.external_ipv4_address != null : row.internal_ipv4_address != null;
    }
    return wantExternal ? row.external_ipv6_address != null : row.internal_ipv6_address != null;
  };
}

// addressInternalSubnetId — subnet_id внутреннего адреса выбранного семейства.
// Нужен, чтобы отсеять INTERNAL-адреса, чья подсеть иного placement, чем у
// балансировщика. Публичные (external) адреса subnet_id не несут → undefined.
export function addressInternalSubnetId(family: Family, row: Record<string, unknown>): string | undefined {
  const key = family === "v4" ? "internal_ipv4_address" : "internal_ipv6_address";
  const a = row[key] as { subnet_id?: string } | undefined;
  return a?.subnet_id || undefined;
}

// Section — лёгкая секция «Источник VIP» (заголовок + разделитель). В этом
// remote нет общего FormSection, поэтому обёртка локальная и минимальная.
function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div style={{ marginTop: 8, marginBottom: 8 }}>
      <div
        style={{
          fontSize: 12,
          fontWeight: 600,
          textTransform: "uppercase",
          letterSpacing: "0.04em",
          color: "var(--kc-text-secondary)",
          paddingBottom: 8,
          marginBottom: 12,
          borderBottom: "1px solid var(--kc-border-secondary)",
        }}
      >
        {title}
      </div>
      {children}
    </div>
  );
}

// FamilyRow — одна строка семейства (v4/v6): единый label слева, справа — режим
// источника (segmented) и соответствующий селектор без своих под-лейблов.
function FamilyRow({ value, onChange, family }: Props & { family: Family }) {
  const project = useProjectStore((s) => s.project);
  const base = `vip_source.${family}`;
  const type = (getByPath(value, "type") as string) || "INTERNAL";
  const placement = (getByPath(value, "placement_type") as string) || "ZONAL";
  const rawMode = getByPath(value, `vip_source._${family}_mode`) as string | undefined;
  const mode = effectiveVipMode(type, rawMode);

  const set = (path: string, v: unknown) => onChange(setByPath(value, path, v));

  const modeOptions =
    type === "EXTERNAL"
      ? [
          { label: "Публичный (авто)", value: "public" },
          { label: "Линк адреса", value: "address" },
        ]
      : [
          { label: "Из подсети (авто)", value: "subnet" },
          { label: "Линк адреса", value: "address" },
        ];

  // Server-side фильтр подсетей по placement (whitelist vpc — {name,
  // placement_type}); клиентский subnetPlacementMatches остаётся как guard.
  const placementFilter = `placement_type="${placement}"`;

  // Для линка INTERNAL-адреса нужен набор подсетей совпадающего placement —
  // адрес допустим, только если его internal-подсеть входит в этот набор.
  const needSubnetSet = mode === "address" && type === "INTERNAL";
  const { data: subnetData } = useQuery({
    queryKey: ["ref", "subnets", "placement-set", project?.id ?? null, placement],
    queryFn: () =>
      api.list<{ subnets: Array<Record<string, unknown>> }>("/vpc/v1/subnets", {
        project_id: project!.id,
        pageSize: "500",
        filter: placementFilter,
      }),
    enabled: needSubnetSet && !!project,
    staleTime: 30_000,
  });
  const allowedSubnetIds = new Set(
    (subnetData?.subnets ?? []).filter(subnetPlacementMatches(placement)).map((s) => s.id as string),
  );

  // Address-линк: family/сфера (linkAddressFilter) + для INTERNAL — подсеть
  // адреса того же placement, что и балансировщик.
  const addressFilter = (row: Record<string, unknown>): boolean => {
    if (!linkAddressFilter(type, family)(row)) return false;
    if (type === "EXTERNAL") return true;
    const sid = addressInternalSubnetId(family, row);
    return sid ? allowedSubnetIds.has(sid) : false;
  };

  return (
    <Form.Item label={FAMILY_LABEL[family]} style={{ marginBottom: family === "v4" ? 12 : 0 }}>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <Segmented
          value={mode}
          onChange={(m) => set(`vip_source._${family}_mode`, String(m))}
          options={modeOptions}
        />

        {mode === "subnet" && (
          <RefSelect
            refResource="subnets"
            refProjectScoped
            refFilter={subnetPlacementMatches(placement)}
            value={getByPath(value, `${base}.subnet_id`) as string | undefined}
            onChange={(uid) => set(`${base}.subnet_id`, uid || undefined)}
            placeholder={`Подсеть (${placement}) для авто-аллокации VIP — оставьте пустым, чтобы не задавать ${FAMILY_LABEL[family]}`}
          />
        )}

        {mode === "address" && (
          <AddressVpcCascader
            family={family}
            type={type}
            addressFilter={addressFilter}
            value={getByPath(value, `${base}.address_id`) as string | undefined}
            onChange={(uid) => set(`${base}.address_id`, uid || undefined)}
            placeholder={`Сеть (VPC) → адрес — оставьте пустым, чтобы не задавать ${FAMILY_LABEL[family]}`}
          />
        )}

        {mode === "public" && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            Публичный VIP выделяется платформой автоматически.
          </Typography.Text>
        )}
      </div>
    </Form.Item>
  );
}

// EditReadOnlyBlock — источник VIP неизменяем после Create: в edit-режиме
// показываем резолвнутый связанный Address (v*_address_id) read-only с замком,
// в тех же горизонтальных строках label-слева.
function EditReadOnlyBlock({ value }: Props) {
  const v4 = (getByPath(value, "v4_address_id") as string) || "";
  const v6 = (getByPath(value, "v6_address_id") as string) || "";
  return (
    <Section title="Источник VIP">
      <Form {...ROW_FORM_PROPS}>
        <Form.Item label={FAMILY_LABEL.v4} style={{ marginBottom: 8 }}>
          <ImmutableField value={v4} reason="Неизменяемо после создания" />
        </Form.Item>
        <Form.Item label={FAMILY_LABEL.v6} style={{ marginBottom: 0 }}>
          <ImmutableField value={v6} reason="Неизменяемо после создания" />
        </Form.Item>
      </Form>
    </Section>
  );
}

export function NlbVipSourceField({ value, onChange, editMode }: Props) {
  if (editMode) return <EditReadOnlyBlock value={value} onChange={onChange} />;
  return (
    <Section title="Источник VIP">
      <Form {...ROW_FORM_PROPS}>
        <FamilyRow value={value} onChange={onChange} family="v4" />
        <FamilyRow value={value} onChange={onChange} family="v6" />
      </Form>
      <Typography.Text type="secondary" style={{ fontSize: 11 }}>
        <span style={{ color: "#ff4d4f" }}>*</span> Задайте источник хотя бы для одного семейства (IPv4 или IPv6). Сам
        VIP-адрес назначается после создания (резолвится в связанный Address) — здесь задаётся только источник.
      </Typography.Text>
    </Section>
  );
}

// NlbDisabledZonesField — deny-list зон REGIONAL-балансировщика (drain).
// Multi-select зон региона (зоны, из которых VIP не анонсируется).
export function NlbDisabledZonesField({ value, onChange }: Props) {
  const regionId = (getByPath(value, "region_id") as string) || "";
  const selected = (getByPath(value, "disabled_announce_zones") as string[] | undefined) ?? [];

  const { data, isLoading } = useQuery({
    queryKey: ["ref", "geo-zones", "by-region", regionId],
    queryFn: () => api.list<{ zones: Array<{ id: string; region_id?: string }> }>("/geo/v1/zones", {}),
    enabled: !!regionId,
    staleTime: 30_000,
  });

  const zones = (data?.zones ?? []).filter((z) => (z.region_id ?? "") === regionId);
  const options = zones.map((z) => ({ value: z.id, label: z.id }));

  // Без vertical-Space обёртки: контрол `fullWidth:false` живёт в горизонтальном
  // Form.Item (label слева / Select справа). Select сам width:100%.
  return (
    <Select
      mode="multiple"
      allowClear
      showSearch
      optionFilterProp="label"
      value={selected}
      options={options}
      loading={isLoading}
      disabled={!regionId}
      placeholder={regionId ? "Зоны без анонса (drain)" : "Сначала выберите регион"}
      onChange={(vals) => onChange(setByPath(value, "disabled_announce_zones", vals))}
      style={{ width: "100%" }}
    />
  );
}
