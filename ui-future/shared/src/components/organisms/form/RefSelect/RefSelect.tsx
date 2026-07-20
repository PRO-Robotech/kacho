// RefSelect — выбор ресурса по ID из выпадающего списка.
// Загружает список через GET <apiPath>?project_id=<id> (+ опц. динамический
// query-параметр от другого поля формы, напр. ?subnet_id=<form.subnet_id>).
// apiPath уже содержит полный путь (e.g. "/iam/v1/projects"),
// никакого "/v1/" префикса не добавляем.
// Flat API: ресурсы имеют поля id и name.
//
// Если задан createResource — в списке появляется «+ Создать …» entry,
// открывающая InlineResourceCreateForm в модалке (паттерн inline-create
// related-resource, как на NetworkDetailPage / SubnetDetailPage). На success
// id созданного ресурса подставляется в это поле.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal, Select } from "antd";
import { api } from "@shared/api/client";
import { getResource } from "@shared/lib/resource-registry";
import { useProjectStore } from "@shared/lib/context-store";
import { ErrorResult } from "@shared/components/molecules/ErrorResult";
import { InlineResourceCreateForm } from "@shared/components/organisms/InlineResourceCreateForm";
import { FormBareProvider } from "@shared/components/organisms/form/FormShell";

interface Props {
  refResource: string;
  refProjectScoped?: boolean;
  value?: string;
  onChange: (uid: string) => void;
  placeholder?: string;
  id?: string;
  disabled?: boolean;
  // Динамический query-параметр от другого поля формы.
  refQueryFromField?: { param: string; field: string };
  // Клиентский фильтр-предикат поверх загруженного candidate-list.
  refFilter?: (row: Record<string, unknown>) => boolean;
  // Текущее значение всей формы (для refQueryFromField / createPresetFields).
  formValue?: Record<string, unknown>;
  // Inline-create related-resource.
  createResource?: string;
  createPresetFields?: (form: Record<string, unknown>) => Record<string, unknown>;
  createTitle?: string;
}

export function RefSelect({
  refResource,
  refProjectScoped,
  value,
  onChange,
  placeholder,
  id,
  disabled,
  refQueryFromField,
  refFilter,
  formValue,
  createResource,
  createPresetFields,
  createTitle,
}: Props) {
  const project = useProjectStore((s) => s.project);
  const spec = getResource(refResource);
  const createSpec = createResource ? getResource(createResource) : undefined;

  const [creating, setCreating] = useState(false);

  // Динамический query-параметр (e.g. subnet_id) — берём из текущего значения формы.
  const dynParamValue =
    refQueryFromField && formValue ? (formValue[refQueryFromField.field] as string | undefined) : undefined;
  const needsDynParam = !!refQueryFromField;

  const enabled = !!spec && (!refProjectScoped || !!project) && (!needsDynParam || !!dynParamValue);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: [
      "ref",
      refResource,
      refProjectScoped ? project?.id : null,
      needsDynParam ? (dynParamValue ?? null) : null,
    ],
    queryFn: () => {
      const q: Record<string, string> = {};
      if (refProjectScoped && project) q["project_id"] = project.id;
      if (refQueryFromField && dynParamValue) q[refQueryFromField.param] = dynParamValue;
      return api.list<Record<string, Array<{ id: string; name: string } & Record<string, unknown>>>>(spec!.apiPath, q);
    },
    enabled,
    staleTime: 30_000,
  });

  if (!spec) return <div className="text-xs text-rose-600">Unknown ref: {refResource}</div>;

  const candidates = (data?.[spec.payloadKey] ?? []).filter((it) =>
    refFilter ? refFilter(it as Record<string, unknown>) : true,
  );
  const options = candidates.map((it) => ({
    uid: it.id,
    name: headLabelFor(refResource, it as Record<string, unknown>),
    extra: extraInfoFor(refResource, it as Record<string, unknown>),
  }));

  const CREATE_SENTINEL = "__create__";

  return (
    <div className="space-y-1">
      <Select
        id={id}
        showSearch
        allowClear
        value={value || undefined}
        placeholder={placeholder ?? `Выбрать ${spec.singular}…`}
        disabled={disabled || !enabled}
        style={{ width: "100%" }}
        optionFilterProp="label"
        // CREATE_SENTINEL — действие (открыть inline-create modal), не значение:
        // не зовём onChange (value не меняется → controlled Select откатывается).
        onChange={(v) => {
          if (v === CREATE_SENTINEL) {
            setCreating(true);
            return;
          }
          onChange(v || "");
        }}
        options={[
          ...options.map((o) => ({
            value: o.uid as string,
            label: `${o.name || o.uid}${o.extra ? ` · ${o.extra}` : ""}`,
          })),
          ...(createSpec ? [{ value: CREATE_SENTINEL, label: `+ Создать ${createSpec.singular.toLowerCase()}…` }] : []),
        ]}
      />
      {refProjectScoped && !project && <p className="text-xs text-amber-600">Выберите проект в шапке для загрузки.</p>}
      {needsDynParam && !dynParamValue && (
        <p className="text-xs text-amber-600">Сначала выберите «{refQueryFromField!.field}» выше.</p>
      )}
      {isLoading && <p className="text-xs text-muted-foreground">Загрузка списка {spec.plural}…</p>}
      {error && <ErrorResult error={error} />}
      {value && options.length > 0 && !options.find((o) => o.uid === value) && (
        <p className="text-xs text-amber-600">ID не найден в списке (возможно ресурс удалён или вне фильтра).</p>
      )}

      {/* helper closure доступен ниже — функция объявлена в file scope */}
      {creating && createSpec && (
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
              spec={createSpec}
              ctx={{
                projectId: project?.id,
                accountId: project?.accountId,
              }}
              presetFields={createPresetFields && formValue ? createPresetFields(formValue) : undefined}
              projectId={project?.id ?? null}
              title={createTitle}
              onCancel={() => setCreating(false)}
              onSuccess={() => {
                // refetch candidate-list — новый ресурс должен появиться;
                // затем подхватываем последний созданный по имени-эвристике
                // (InlineResourceCreateForm не отдаёт id наверх — после refetch
                // diff'им список). Простой best-effort: перезапрашиваем и
                // оставляем выбор пользователю, если не смогли определить.
                void refetch().then((r) => {
                  const after = (r.data?.[spec.payloadKey] ?? []) as Array<{ id: string }>;
                  const before = new Set(options.map((o) => o.uid));
                  const fresh = after.find((it) => !before.has(it.id));
                  if (fresh) onChange(fresh.id);
                });
              }}
            />
          </FormBareProvider>
        </Modal>
      )}
    </div>
  );
}

// headLabelFor — основная подпись option. У большинства ресурсов это `name`,
// но у User поля name нет — берём display_name / email.
function headLabelFor(refResource: string, row: Record<string, unknown>): string {
  if (refResource === "users") {
    return (row.display_name as string) || (row.email as string) || (row.id as string) || "";
  }
  return (row.name as string) ?? "";
}

// extraInfoFor — формирует короткую полезную подпись для option в дропдауне.
// Для каждого ресурса показываем «адресную» информацию: CIDR / IP / ID-пула /
// статус — чтобы пользователь различал «безымянные» ресурсы.
function extraInfoFor(refResource: string, row: Record<string, unknown>): string {
  switch (refResource) {
    case "subnets": {
      // VPC-1: prefer the immutable primary anchor; fall back to additional
      // ranges. v4_cidr_blocks[] retired in favour of ipv4_cidr_primary.
      const primary4 = (row.ipv4_cidr_primary as string | undefined) ?? "";
      const primary6 = (row.ipv6_cidr_primary as string | undefined) ?? "";
      const extra4 = (row.ipv4_cidr_blocks as string[] | undefined) ?? [];
      const extra6 = (row.ipv6_cidr_blocks as string[] | undefined) ?? [];
      const cidrs = [primary4, primary6, ...extra4, ...extra6].filter(Boolean);
      return cidrs.length > 0 ? cidrs.join(", ") : "";
    }
    case "addresses": {
      const ext4 = (row.external_ipv4_address as { address?: string } | undefined)?.address;
      const int4 = (row.internal_ipv4_address as { address?: string } | undefined)?.address;
      const ext6 = (row.external_ipv6_address as { address?: string } | undefined)?.address;
      const int6 = (row.internal_ipv6_address as { address?: string } | undefined)?.address;
      return ext4 || int4 || ext6 || int6 || "";
    }
    case "gateways": {
      // Gateway proto: shared_egress_gateway oneof + ip / used_by; показываем
      // тип шлюза если name пустое.
      const sg = row.shared_egress_gateway as Record<string, unknown> | undefined;
      if (sg) return "shared-egress";
      return "";
    }
    case "networks": {
      const ipv4 = (row.ipv4_cidr_blocks as string[] | undefined) ?? [];
      return ipv4.length > 0 ? ipv4.join(", ") : "";
    }
    case "address-pools": {
      const cidrs = (row.cidr_blocks as string[] | undefined) ?? [];
      const isDefault = row.is_default === true ? " · default" : "";
      return (cidrs.length > 0 ? cidrs.join(", ") : "") + isDefault;
    }
    case "zones": {
      const r = (row.region_id as string | undefined) ?? "";
      return r;
    }
    case "route-tables":
    case "security-groups": {
      const net = (row.network_id as string | undefined) ?? "";
      return net ? `net:${net.slice(0, 8)}` : "";
    }
    // NLB-типы: показываем регион (+ схему) — как «адресную» инфу vpc-ресурсов.
    case "load-balancers":
    case "network-load-balancers": {
      const region = (row.region_id as string | undefined) ?? "";
      const scheme = (row.type as string | undefined) ?? "";
      return [region, scheme].filter(Boolean).join(" · ");
    }
    case "target-groups": {
      return (row.region_id as string | undefined) ?? "";
    }
    // Geo Region: name — head-label, id (ru-central1) — полезный extra.
    case "regions":
    case "compute-regions": {
      return (row.id as string | undefined) ?? "";
    }
    default:
      return "";
  }
}
