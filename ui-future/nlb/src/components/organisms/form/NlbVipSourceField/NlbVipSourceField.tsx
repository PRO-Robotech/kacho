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
//   • обе схемы:
//       — «Не задавать»        → семейство опускается в wire целиком.
//
// Раскладка — одна строка на семейство: слева единый label («IPv4 Адрес» /
// «IPv6 Адрес»), справа — переключатель режима (segmented) и соответствующий
// селектор (без собственных под-лейблов).
//
// ОТКАЗ ОТ СЕМЕЙСТВА — ОТДЕЛЬНЫЙ РЕЖИМ, а не пустое значение источника.
// Сервис требует источник хотя бы для ОДНОГО семейства, поэтому балансировщик
// только на IPv4 законен. Прежде отказ выражался лишь косвенно — выбрать ветвь
// ссылки и оставить её пустой, — и для EXTERNAL это был единственный путь:
// режим «публичный» источник даёт БЕЗУСЛОВНО и стоял умолчанием у обоих
// семейств, так что на провод уезжали оба. Пустая ссылка семейство по-прежнему
// опускает (пустой addressId/subnetId на бэкенд не уходит), но НАЗЫВАЕТСЯ отказ
// теперь своим именем.
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
import { pickerScope } from "@shared/lib/picker-search";

type Family = "v4" | "v6";
// VipMode — режим источника VIP семейства:
//   subnet  — авто-аллокация из подсети (INTERNAL);
//   address — линк существующего Address (INTERNAL internal / EXTERNAL public);
//   public  — платформенный public IP (EXTERNAL);
//   off     — семейство НЕ задаётся (источник в тело не уезжает).
export type VipMode = "subnet" | "address" | "public" | "off";

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

// Область поиска в списке зон (#528).
//
// Зоны отдаёт geo, и белого списка выражения у него НЕТ: уйти вводу отсюда
// некуда, а выдумать поле запроса нельзя — незнакомое поле это не «фильтр без
// эффекта», а отказ на всю страницу. Значит законный исход один: сказать правду
// об области. Список читается одной страницей и сужается в браузере, поэтому
// пустой ответ означает «нет среди загруженных», а не «такой зоны нет» — а
// именно последнее и утверждало прежнее «нет совпадений», при том что у
// выпадающего списка нет продолжения, которым это можно было бы опровергнуть.
const ZONE_SCOPE = pickerScope(undefined);

// familyIpVersion — UI-дискриминатор семейства → enum Address.IpVersion на проводе.
export function familyIpVersion(family: Family): "IPV4" | "IPV6" {
  return family === "v4" ? "IPV4" : "IPV6";
}

// `placement` — единственный ввод режима, который форма несёт (NLB CONTRACT / F2).
// `type` и `placement_type` — производные output-only проекции: запрос их
// принимает лишь затем, чтобы выставивший их клиент получил явный
// InvalidArgument, поэтому форма их не шлёт и, следовательно, не хранит. Всё,
// что раньше читало их из объекта формы, обязано выводить их отсюда — иначе
// читается undefined, молча подставляется INTERNAL/ZONAL, и выбор оператора не
// доходит ни до предлагаемых режимов VIP, ни до ветви oneof в теле запроса.
export function lbTypeFromPlacement(placement: string | undefined): "EXTERNAL" | "INTERNAL" {
  return placement === "EXTERNAL_REGIONAL" ? "EXTERNAL" : "INTERNAL";
}

export function lbPlacementTypeFromPlacement(placement: string | undefined): "REGIONAL" | "ZONAL" {
  return placement === "INTERNAL_ZONAL" ? "ZONAL" : "REGIONAL";
}

// effectiveVipMode — нормализует режим под схему балансировщика: INTERNAL
// допускает {subnet, address, off} (default subnet), EXTERNAL — {public,
// address, off} (default public). Устаревший режим (после смены type)
// схлопывается в валидный.
//
// `off` законен при ЛЮБОЙ схеме: это не источник, а отказ от семейства, и
// зависеть от схемы ему не от чего.
export function effectiveVipMode(type: string, mode: string | undefined): VipMode {
  const valid: VipMode[] = type === "EXTERNAL" ? ["public", "address", "off"] : ["subnet", "address", "off"];
  const def: VipMode = type === "EXTERNAL" ? "public" : "subnet";
  return valid.includes(mode as VipMode) ? (mode as VipMode) : def;
}

// buildVipSourceOrNull — wire-ветвь oneof одного семейства, либо null, если
// семейство не задано. Режим нормализуется под type.
//
// Не задано — это ТРИ случая, и все они дают null:
//   • `off` — арендатор ЯВНО отказался от семейства (балансировщик только на
//     IPv4 или только на IPv6 — законный ресурс: сервис требует источник хотя
//     бы для одного семейства, не для обоих);
//   • пустой subnet_id / address_id — ветвь ссылки без ссылки; уйди она телом
//     как {address_id:""}, сервис ответил бы жалобой на поле, которого
//     оператор не называл;
//   • ничего не выбрано вовсе.
// Режим `public` источник даёт ВСЕГДА — VIP выделяет платформа, называть
// нечего. Именно поэтому явный `off` необходим: без него внешний
// балансировщик по умолчанию слал ОБА семейства, и отказаться было нечем.
export function buildVipSourceOrNull(
  type: string,
  mode: string | undefined,
  fam: Record<string, unknown> | undefined,
): Record<string, unknown> | null {
  const em = effectiveVipMode(type, mode);
  if (em === "off") return null;
  if (em === "public") return { public: {} };
  if (em === "address") return (fam?.address_id as string) ? { address_id: fam!.address_id } : null;
  return (fam?.subnet_id as string) ? { subnet_id: fam!.subnet_id } : null;
}

// subnetPlacementMatches — кандидат-подсеть подходит для источника VIP, только
// если её placement совпадает с placement балансировщика. Legacy-подсети без
// placement_type трактуются как ZONAL.
export function subnetPlacementMatches(placement: string, regionId?: string) {
  return (row: Record<string, unknown>): boolean => {
    const pt = (row.placement_type as string | undefined) || "ZONAL";
    if (pt !== placement) return false;
    // Регион подсети обязан совпадать с регионом балансировщика.
    //
    // Без этой проверки в список попадали подсети ЛЮБОГО региона, и выбор
    // заканчивался отказом сервера уже после отправки формы: связать ресурсы
    // разных регионов нельзя (согласованность размещения — правило продукта, а
    // не вкус). Отвергнуть заведомо негодный выбор до отправки дешевле для всех.
    //
    // Регион берётся из АВТОРИТЕТНОГО поля строки подсети, а не выводится из
    // имени её зоны: имена региона и зоны — произвольные строки, и вывод по
    // ним запрещён прямо (правило продукта; строковая деривация к тому же молча
    // даёт пустоту у подсети без зоны, и проверка превращается в no-op).
    if (!regionId) return false;
    return (row.region_id as string | undefined) === regionId;
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
  // Режим берётся из `placement` — единственного, что форма несёт. Прежнее чтение
  // `type` / `placement_type` из объекта формы всегда давало undefined (форма их
  // не шлёт — это write-reject проекции), поэтому пикер застревал в
  // INTERNAL/ZONAL: «Публичный (авто)» не предлагался вовсе, а REGIONAL-подсети
  // не попадали в список кандидатов.
  const placementMode = getByPath(value, "placement") as string | undefined;
  // Регион балансировщика — из того же объекта формы: подсеть обязана быть его
  // региона, и пока он не выбран, выбор подсети не имеет смысла.
  const regionId = getByPath(value, "region_id") as string | undefined;
  const type = lbTypeFromPlacement(placementMode);
  const placement = lbPlacementTypeFromPlacement(placementMode);
  const rawMode = getByPath(value, `vip_source._${family}_mode`) as string | undefined;
  const mode = effectiveVipMode(type, rawMode);

  const set = (path: string, v: unknown) => onChange(setByPath(value, path, v));

  // «Не задавать» стоит рядом с источниками, потому что это такой же исход
  // выбора, как они: балансировщик только на одном семействе — законный ресурс.
  // Пока варианта не было, отказ от семейства выражался лишь косвенно (выбрать
  // «линк адреса» и оставить пусто), а умолчание внешней схемы слало оба.
  const OFF_OPTION = { label: "Не задавать", value: "off" };
  const modeOptions =
    type === "EXTERNAL"
      ? [{ label: "Публичный (авто)", value: "public" }, { label: "Линк адреса", value: "address" }, OFF_OPTION]
      : [{ label: "Из подсети (авто)", value: "subnet" }, { label: "Линк адреса", value: "address" }, OFF_OPTION];

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
        <Segmented value={mode} onChange={(m) => set(`vip_source._${family}_mode`, String(m))} options={modeOptions} />

        {mode === "subnet" && (
          <RefSelect
            refResource="subnets"
            refProjectScoped
            refFilter={subnetPlacementMatches(placement, regionId)}
            value={getByPath(value, `${base}.subnet_id`) as string | undefined}
            onChange={(uid) => set(`${base}.subnet_id`, uid || undefined)}
            // Пока регион не выбран, выбирать не из чего: любая подсеть окажется
            // либо верной, либо из чужого региона, а какая именно — решает
            // регион. Поле закрыто и САМО ГОВОРИТ, чего ждёт: закрытое поле без
            // объяснения читается как неисправное.
            disabled={!regionId}
            placeholder={regionId ? `Подсеть (${placement}) для авто-аллокации VIP` : "Сначала выберите регион"}
          />
        )}

        {mode === "address" && (
          <AddressVpcCascader
            family={family}
            type={type}
            addressFilter={addressFilter}
            value={getByPath(value, `${base}.address_id`) as string | undefined}
            onChange={(uid) => set(`${base}.address_id`, uid || undefined)}
            placeholder="Сеть (VPC) → адрес"
          />
        )}

        {mode === "public" && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            Публичный VIP выделяется платформой автоматически.
          </Typography.Text>
        )}

        {mode === "off" && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {FAMILY_LABEL[family]} не задаётся — балансировщик будет работать без этого семейства.
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
        {/* Цвет — роль палитры, а не литерал: литерал берётся из одной темы и
            в другой остаётся собой. Здесь стоял красный набора antd, который
            со сменой темы не менялся вовсе. */}
        <span style={{ color: "var(--kc-danger)" }}>*</span> Задайте источник хотя бы для одного семейства (IPv4 или
        IPv6). Сам VIP-адрес назначается после создания (резолвится в связанный Address) — здесь задаётся только
        источник.
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
      // Сервер этот список не сужает, поэтому сеево по метке остаётся — оно
      // здесь и есть вся область поиска, и подпись поля говорит об этом.
      optionFilterProp="label"
      title={ZONE_SCOPE.notice}
      // Пустой ответ обязан называть свою ОБЛАСТЬ, а не утверждать отсутствие
      // зоны, которую поле не спрашивало.
      notFoundContent={isLoading ? undefined : ZONE_SCOPE.emptyText}
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
