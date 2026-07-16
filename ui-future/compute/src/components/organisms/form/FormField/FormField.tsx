import { useId } from "react";
import { Card, Input, Select, Space, Switch, Tooltip, Typography, Button as AntButton } from "antd";
import { DeleteOutlined, PlusOutlined, QuestionCircleOutlined } from "@ant-design/icons";
import { Label } from "@/components/atoms/ui/Input";
import { RefSelect } from "@/components/organisms/form/RefSelect";
import { SgRulesEditor } from "@/components/organisms/form/SgRulesEditor";
import { LabelsEditor } from "@/components/organisms/form/LabelsEditor";
import { getByPath, setByPath, deleteByPath } from "@/lib/path";
import type { FormField as FF, ArrayField } from "@/lib/form-schema";

interface Props {
  field: FF;
  // pathPrefix — родительский путь, например "spec.rules[0]"; пустая строка для top-level
  pathPrefix: string;
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  // В Edit-режиме поля с `immutable: true` рендерятся disabled.
  // В Create — игнорируется.
  editMode?: boolean;
  // Если true — встроенный <Label> внутри renderer'а не рисуется (label
  // рендерится снаружи, например в AntD Form.Item). Используется для
  // горизонтального YC-style layout, где label слева, input справа.
  hideLabel?: boolean;
}

function fullPath(prefix: string, name: string): string {
  if (!prefix) return name;
  return `${prefix}.${name}`;
}

export function FormFieldRenderer({ field, pathPrefix, value, onChange, editMode, hideLabel }: Props) {
  if (field.hidden) return null;
  if (editMode && field.editHidden) return null;
  if (field.visibleWhen) {
    // visibleWhen.field — относительный путь (резолвится через pathPrefix),
    // чтобы дискриминатор oneof внутри array-item тоже работал. Если поле
    // начинается с "/" или совпадает с top-level именем — приоритетно
    // пробуем pathPrefix-resolution, fallback на top-level.
    const rel = field.visibleWhen.field;
    const relPath = pathPrefix ? `${pathPrefix}.${rel}` : rel;
    const cur = (getByPath(value, relPath) as string | undefined) ?? (getByPath(value, rel) as string | undefined);
    const want = field.visibleWhen.equals;
    const matched = Array.isArray(want) ? want.includes(cur ?? "") : cur === want;
    if (!matched) return null;
  }
  const disabled = !!(field.immutable && editMode);
  if (field.type === "custom") {
    return <>{field.render({ pathPrefix, value, onChange, editMode, field })}</>;
  }
  if (field.type === "array")
    return (
      <ArrayFieldRenderer
        field={field}
        pathPrefix={pathPrefix}
        value={value}
        onChange={onChange}
        editMode={editMode}
        disabled={disabled}
        hideLabel={hideLabel}
      />
    );
  if (field.type === "sg-rules") {
    const path = pathPrefix ? `${pathPrefix}.${field.name}` : field.name;
    // KAC-243 (scenario 18): создаваемая SG принадлежит сети из поля network_id
    // той же формы — SG-target rule может ссылаться только на SG этой сети.
    const editingNetworkId = getByPath(value, "network_id") as string | undefined;
    return (
      <SgRulesEditor
        pathPrefix={pathPrefix}
        value={value}
        onChange={onChange}
        path={path}
        description={field.description}
        editingNetworkId={editingNetworkId || undefined}
      />
    );
  }
  if (field.type === "labels") {
    const path = pathPrefix ? `${pathPrefix}.${field.name}` : field.name;
    return (
      <LabelsEditor
        pathPrefix={pathPrefix}
        value={value}
        onChange={onChange}
        path={path}
        label={hideLabel ? "" : field.label}
        description={hideLabel ? undefined : field.description}
        disabled={disabled}
      />
    );
  }
  return (
    <ScalarFieldRenderer
      field={field}
      pathPrefix={pathPrefix}
      value={value}
      onChange={onChange}
      disabled={disabled}
      hideLabel={hideLabel}
    />
  );
}

function ScalarFieldRenderer({
  field,
  pathPrefix,
  value,
  onChange,
  disabled,
  hideLabel,
}: Props & { disabled?: boolean }) {
  const id = useId();
  const path = fullPath(pathPrefix, field.name);
  const cur = getByPath(value, path);

  const set = (v: unknown) => onChange(setByPath(value, path, v));

  return (
    <div className={hideLabel ? "" : "space-y-1.5"}>
      {!hideLabel && (
        <Label
          htmlFor={id}
          required={field.required}
          description={
            disabled ? `${field.description ? field.description + " " : ""}(immutable после Create)` : field.description
          }
        >
          {field.label}
        </Label>
      )}
      {field.type === "string" && (
        <Input
          id={id}
          value={(cur as string | undefined) ?? ""}
          onChange={(e) => set(e.target.value)}
          placeholder={field.placeholder}
          pattern={field.pattern}
          disabled={disabled}
        />
      )}
      {field.type === "text" && (
        <Input.TextArea
          id={id}
          value={(cur as string | undefined) ?? ""}
          onChange={(e) => set(e.target.value)}
          placeholder={field.placeholder}
          rows={field.rows ?? 3}
          disabled={disabled}
        />
      )}
      {field.type === "int" && (
        <Input
          id={id}
          type="number"
          value={cur === undefined || cur === null ? "" : String(cur)}
          onChange={(e) => set(e.target.value === "" ? undefined : Number(e.target.value))}
          min={field.min}
          max={field.max}
          disabled={disabled}
        />
      )}
      {field.type === "bool" && (
        // Переключатель (Switch), а не сырой checkbox. Label — слева в Form.Item,
        // здесь не дублируем.
        <Switch
          id={id}
          checked={Boolean(cur ?? field.default)}
          onChange={(checked) => set(checked)}
          disabled={disabled}
        />
      )}
      {field.type === "enum" && (
        <Select
          id={id}
          showSearch
          allowClear
          value={(cur as string | undefined) || undefined}
          onChange={(v) => set(v || undefined)}
          placeholder="— Не выбрано —"
          disabled={disabled}
          style={{ width: "100%" }}
          optionFilterProp="label"
          options={field.options.map((o) => ({ value: o.value, label: o.label }))}
        />
      )}
      {field.type === "ref" && (
        <RefSelect
          id={id}
          refResource={field.refResource}
          refProjectScoped={field.refProjectScoped}
          value={cur as string | undefined}
          onChange={(uid) => set(uid || undefined)}
          placeholder={field.placeholder}
          disabled={disabled}
          refQueryFromField={field.refQueryFromField}
          refFilter={field.refFilter}
          formValue={value}
          createResource={field.createResource}
          createPresetFields={field.createPresetFields}
          createTitle={field.createTitle}
        />
      )}
    </div>
  );
}

// ArrayItemField — компактная обёртка для поля внутри array-item:
// mini-label сверху (11px, серый), * для required справа, ⓘ-tooltip если есть
// description. Input снизу через children (hideLabel=true в FormFieldRenderer).
function ArrayItemField({
  label,
  required,
  description,
  children,
}: {
  label: string;
  required?: boolean;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4, minWidth: 0 }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 4,
          fontSize: 12,
          color: "var(--kc-text-secondary)",
          lineHeight: 1.2,
          whiteSpace: "nowrap",
        }}
      >
        <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{label}</span>
        {required && (
          <span style={{ color: "#ff4d4f" }} aria-hidden>
            *
          </span>
        )}
        {description && (
          <Tooltip title={description}>
            <QuestionCircleOutlined style={{ fontSize: 12, color: "var(--kc-text-tertiary)" }} />
          </Tooltip>
        )}
      </div>
      {children}
    </div>
  );
}

function ArrayFieldRenderer({
  field,
  pathPrefix,
  value,
  onChange,
  editMode,
  disabled,
}: { field: ArrayField; disabled?: boolean } & Omit<Props, "field">) {
  const path = fullPath(pathPrefix, field.name);
  const items = (getByPath(value, path) as Record<string, unknown>[] | undefined) ?? [];

  const atCap = field.maxItems !== undefined && items.length >= field.maxItems;

  const add = () => {
    if (atCap) return;
    const next = [...items, field.newItem ? field.newItem() : {}];
    onChange(setByPath(value, path, next));
  };

  const removeAt = (idx: number) => {
    onChange(deleteByPath(value, `${path}[${idx}]`));
  };

  return (
    <Card
      size="small"
      title={
        <Space size={8}>
          <Typography.Text strong>{field.label}</Typography.Text>
          {field.required && <span style={{ color: "#ff4d4f", fontSize: 12 }}>*</span>}
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {items.length}
            {field.maxItems !== undefined ? `/${field.maxItems}` : ""}
          </Typography.Text>
        </Space>
      }
      extra={
        <AntButton type="primary" ghost size="small" icon={<PlusOutlined />} onClick={add} disabled={disabled || atCap}>
          Добавить
        </AntButton>
      }
      style={disabled ? { opacity: 0.6, pointerEvents: "none" } : undefined}
    >
      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        {items.length === 0 && (
          <Typography.Text type="secondary" italic style={{ fontSize: 12 }}>
            — пусто —
          </Typography.Text>
        )}
        {items.map((_, idx) => (
          <div
            key={idx}
            style={{
              display: "flex",
              alignItems: "flex-start",
              gap: 8,
              padding: 8,
              borderRadius: 6,
              background: "var(--kc-hover-fill)",
            }}
          >
            <div
              style={{
                display: "grid",
                gridTemplateColumns:
                  field.itemFields.length > 1 ? `repeat(${field.itemFields.length}, minmax(0, 1fr))` : "1fr",
                gap: 8,
                flex: 1,
              }}
            >
              {field.itemFields.map((sub) => {
                // visibleWhen — резолвится FormFieldRenderer'ом; здесь
                // фильтруем чтобы не оставить пустую mini-label-обёртку.
                if (sub.visibleWhen) {
                  const rel = sub.visibleWhen.field;
                  const relPath = `${path}[${idx}].${rel}`;
                  const cur =
                    (getByPath(value, relPath) as string | undefined) ?? (getByPath(value, rel) as string | undefined);
                  const want = sub.visibleWhen.equals;
                  const matched = Array.isArray(want) ? want.includes(cur ?? "") : cur === want;
                  if (!matched) return null;
                }
                return (
                  <ArrayItemField
                    key={sub.name}
                    label={sub.label}
                    required={!!sub.required}
                    description={sub.description}
                  >
                    <FormFieldRenderer
                      field={sub}
                      pathPrefix={`${path}[${idx}]`}
                      value={value}
                      onChange={onChange}
                      editMode={editMode}
                      hideLabel
                    />
                  </ArrayItemField>
                );
              })}
            </div>
            <AntButton
              type="text"
              size="small"
              icon={<DeleteOutlined />}
              onClick={() => removeAt(idx)}
              disabled={disabled}
              danger
              style={{ flexShrink: 0, marginTop: 2 }}
            />
          </div>
        ))}
      </Space>
      {field.description && (
        <Typography.Text type="secondary" style={{ fontSize: 11, display: "block", marginTop: 8 }}>
          {field.description}
        </Typography.Text>
      )}
    </Card>
  );
}
