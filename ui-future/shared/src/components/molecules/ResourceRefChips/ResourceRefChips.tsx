// ResourceRefChips — controlled chip-list для array-of-ref полей (например
// NIC.v4_address_ids / v6_address_ids / security_group_ids). Визуально как
// SubnetCidrChips, но содержимое — id чужих ресурсов; чипы показывают
// resolved name (загрузка через api.list для project-scoped ресурсов).
// Внизу — Select для добавления, Tag-close для удаления.

import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, Modal, Select, Space, Tag, Typography } from "antd";
import { CloseOutlined } from "@ant-design/icons";
import { api } from "@shared/api/client";
import { getResource } from "@shared/lib/resource-registry";
import { useContext } from "@shared/lib/context-store";
import { InlineResourceCreateForm } from "@shared/components/organisms/InlineResourceCreateForm";

interface Props {
  title: string;
  /** ID ресурса в REGISTRY (например, "addresses", "security-groups"). */
  refResource: string;
  /** project_id для ListXxxRequest. */
  projectId: string;
  /** Опц. client-side filter (например, только internal IPv4 Address'ы). */
  refFilter?: (row: Record<string, unknown>) => boolean;
  /** Цвет chip'ов (для visual diff между IPv4/IPv6/SG). */
  tagColor?: string;
  value: string[];
  onChange: (next: string[]) => void;
  /** Максимум элементов (KAC-55: ≤1 v4/v6 Address на NIC). */
  maxItems?: number;
  /** KAC-101: ID ресурса в REGISTRY для inline-create в dropdown.
   *  Если задан — в списке появляется «+ Создать …» entry, открывающая
   *  InlineResourceCreateForm в модалке; на success id созданного ресурса
   *  автоматически добавляется в текущий chip-list. */
  createResource?: string;
  /** Опц. preset для form'ы (например, internal_ipv4_address_spec.subnet_id
   *  из контекста NIC формы). Поля locked. */
  createPresetFields?: Record<string, unknown>;
  /** Опц. редактируемые preset-поля (например, _address_kind). Дефолт, но
   *  пользователь может изменить. */
  createEditablePresetFields?: Record<string, unknown>;
  /** Опц. title модалки. */
  createTitle?: string;
  /** Заблокировать селектор (например, пока не выбрана подсеть). */
  disabled?: boolean;
  /** Подсказка под селектором, когда disabled (например, «Сначала выберите подсеть»). */
  disabledHint?: string;
}

export function ResourceRefChips({
  title,
  refResource,
  projectId,
  refFilter,
  tagColor = "blue",
  value,
  onChange,
  maxItems,
  createResource,
  createPresetFields,
  createEditablePresetFields,
  createTitle,
  disabled,
  disabledHint,
}: Props) {
  const spec = getResource(refResource);
  const createSpec = createResource ? getResource(createResource) : undefined;
  const account = useContext((s) => s.account);
  const [draft, setDraft] = useState<string | undefined>(undefined);
  const [creating, setCreating] = useState(false);
  // Ремоунт Select при выборе sentinel «Создать»: AntD держит sentinel
  // «выбранным» (controlled value не меняется) → повторный выбор того же пункта
  // не триггерит onChange → модалка не открывалась снова после отмены.
  const [selKey, setSelKey] = useState(0);

  // Загружаем список ресурсов проекта для resolve id→name + dropdown options.
  const { data: listData, refetch } = useQuery({
    queryKey: [refResource, "list", projectId],
    queryFn: () =>
      api.list<Record<string, unknown>>(spec!.apiPath, {
        project_id: projectId,
        pageSize: "500",
      }),
    enabled: !!spec,
    staleTime: 30_000,
  });

  const rows = useMemo(() => {
    if (!listData || !spec) return [];
    const arr = (listData[spec.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
    return refFilter ? arr.filter(refFilter) : arr;
  }, [listData, spec, refFilter]);

  const byId = useMemo(() => new Map(rows.map((r) => [(r.id as string) ?? "", r])), [rows]);

  const CREATE_SENTINEL = "__create__";

  // Лейбл ресурса: для addresses — «name: ip» (доп-алиас IP), иначе name.
  const labelFor = useCallback(
    (r: Record<string, unknown> | undefined, id: string): string => {
      const name = ((r?.name as string) || id) ?? id;
      if (refResource !== "addresses" || !r) return name;
      const pick = (k: string) => (r[k] as { address?: string } | undefined)?.address;
      const ip =
        pick("external_ipv4_address") ||
        pick("internal_ipv4_address") ||
        pick("external_ipv6_address") ||
        pick("internal_ipv6_address") ||
        "";
      return ip ? `${name}: ${ip}` : name;
    },
    [refResource],
  );

  // Options для dropdown — только те, что ещё не добавлены. KAC-101: при
  // createResource добавляем sentinel-опцию «+ Создать <singular>…».
  const options = useMemo(() => {
    const base = rows
      .filter((r) => !value.includes((r.id as string) ?? ""))
      .map((r) => ({
        value: (r.id as string) ?? "",
        label: labelFor(r, (r.id as string) ?? ""),
      }));
    if (createSpec) {
      base.push({
        value: CREATE_SENTINEL,
        label: `+ Создать ${createSpec.singular.toLowerCase()}…`,
      });
    }
    return base;
  }, [rows, value, createSpec, labelFor]);

  const atCap = maxItems !== undefined && value.length >= maxItems;

  // KAC-101: на выбор sentinel-опции открываем inline-create modal
  // (alt-flow: пользователь не жмёт Add, а сразу выбирает «+ Создать…»).
  const onDraftChange = (next: string | undefined) => {
    if (next === CREATE_SENTINEL) {
      setCreating(true);
      setDraft(undefined);
      setSelKey((k) => k + 1); // remount Select → нет залипшего sentinel
      return;
    }
    // Авто-добавление при выборе — раньше требовался отдельный клик «Add», и
    // пользователь, выбрав значение в селекторе, забывал его нажать → изменение
    // не попадало в value → форма не детектила diff → запрос не отправлялся.
    if (next && !value.includes(next) && !atCap) {
      onChange([...value, next]);
    }
    setDraft(undefined);
    setSelKey((k) => k + 1); // remount → Select сбрасывается на placeholder
  };

  const onRemove = (id: string) => {
    onChange(value.filter((v) => v !== id));
  };

  return (
    <Card
      size="small"
      title={
        <Space size={8}>
          <Typography.Text strong>{title}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {value.length}
            {maxItems !== undefined ? ` / ${maxItems}` : ""}
          </Typography.Text>
        </Space>
      }
    >
      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        <div style={{ minHeight: 24 }}>
          {value.length === 0 ? (
            <Typography.Text type="secondary" italic style={{ fontSize: 12 }}>
              — пусто —
            </Typography.Text>
          ) : (
            <Space size={[6, 6]} wrap>
              {value.map((id) => {
                const row = byId.get(id);
                const name = labelFor(row, id);
                return (
                  <Tag
                    key={id}
                    color={tagColor}
                    closable
                    closeIcon={<CloseOutlined style={{ fontSize: 10 }} />}
                    onClose={(e) => {
                      e.preventDefault();
                      onRemove(id);
                    }}
                    style={{ fontFamily: "monospace", fontSize: 12, margin: 0 }}
                  >
                    {name}
                  </Tag>
                );
              })}
            </Space>
          )}
        </div>
        <Space.Compact style={{ width: "100%" }}>
          <Select
            key={selKey}
            showSearch
            value={draft}
            onChange={onDraftChange}
            options={options}
            placeholder={
              disabled ? (disabledHint ?? "Недоступно") : atCap ? `Максимум ${maxItems}` : `Выбрать ${title}`
            }
            optionFilterProp="label"
            disabled={disabled || atCap}
            style={{ flex: 1 }}
          />
        </Space.Compact>
      </Space>
      {creating && createSpec && (
        <Modal
          open
          footer={null}
          onCancel={() => setCreating(false)}
          width={860}
          destroyOnClose
          maskClosable
          title={null}
        >
          <InlineResourceCreateForm
            spec={createSpec}
            ctx={{ projectId, accountId: account?.id }}
            presetFields={createPresetFields}
            editablePresetFields={createEditablePresetFields}
            projectId={projectId}
            title={createTitle}
            onCancel={() => setCreating(false)}
            onSuccess={() => {
              // KAC-101: refetch candidate-list — новый ресурс должен появиться;
              // diff'им до/после и подхватываем fresh id в текущий chip-list.
              const beforeIds = new Set(rows.map((r) => (r.id as string) ?? "").filter(Boolean));
              void refetch().then((r) => {
                const after = (r.data?.[spec!.payloadKey] as Record<string, unknown>[] | undefined) ?? [];
                const filtered = refFilter ? after.filter(refFilter) : after;
                const fresh = filtered.find((it) => !beforeIds.has((it.id as string) ?? ""));
                if (fresh) {
                  const id = (fresh.id as string) ?? "";
                  if (id && !value.includes(id)) {
                    if (!(maxItems !== undefined && value.length >= maxItems)) {
                      onChange([...value, id]);
                    }
                  }
                }
                setCreating(false);
              });
            }}
          />
        </Modal>
      )}
    </Card>
  );
}
