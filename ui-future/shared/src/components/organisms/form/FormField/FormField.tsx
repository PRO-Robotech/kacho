import { useId } from "react";
import { Card, Input, Select, Space, Switch, Tooltip, Typography, Button as AntButton } from "antd";
import { DeleteOutlined, PlusOutlined, QuestionCircleOutlined } from "@ant-design/icons";
import { Label } from "@shared/components/atoms/ui/Input";
import { RefSelect } from "@shared/components/organisms/form/RefSelect";
import { SgRulesEditor } from "@shared/components/organisms/form/SgRulesEditor";
import { LabelsEditor } from "@shared/components/organisms/form/LabelsEditor";
import { EditableKVTable } from "@shared/components/molecules/EditableKVTable";
import { editorEmptyStyle, editorIconButtonStyle } from "@shared/components/organisms/form/editor-surface";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import type { FieldErrors } from "@shared/components/organisms/form/field-rules";
import { getByPath, setByPath, deleteByPath } from "@shared/lib/path";
import type { FormField as FF, ArrayField } from "@shared/lib/form-schema";
import { displayText } from "@shared/lib/display-text";

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
  // горизонтального label-left layout, где label слева, input справа.
  hideLabel?: boolean;
  // Идентификатор контрола, назначенный СНАРУЖИ. Нужен там, где подпись рисует
  // не сам renderer, а его вызывающий, и обязан связать её со своим вводом
  // (`<label for>`): строка составного списка (ArrayItemField). Не задан —
  // renderer чеканит свой, как и раньше.
  controlId?: string;
  // Поле не прошло правило схемы: ввод получает линию отказа и `aria-invalid`,
  // а сообщение рисует вызывающий рядом с полем. Здесь — только СОСТОЯНИЕ ввода:
  // текст отказа живёт у того, кто владеет проверкой, иначе один предмет
  // рисовался бы дважды.
  invalid?: boolean;
  // Идентификатор сообщения об отказе — ввод ссылается на него `aria-describedby`,
  // чтобы читающий с экрана услышал причину вместе с полем.
  describedBy?: string;
  // Отказы по ПОДПОЛЯМ строк списка (ключ — полный путь `<поле>[i].<подполе>`).
  // Нужны только списку: он один рисует чужие поля внутри себя.
  itemErrors?: FieldErrors;
}

function fullPath(prefix: string, name: string): string {
  if (!prefix) return name;
  return `${prefix}.${name}`;
}

// Виды полей, чей ввод — ОДИН элемент, принимающий назначенный снаружи
// идентификатор (`ScalarFieldRenderer` ставит его на сам контрол). Только их
// подпись вправе быть `<label for>`.
//
// Снаружи оставлены две группы, и обе намеренно:
//  • `custom` / `array` / `labels` / `sg-rules` — своё поддерево, одного ввода нет;
//  • `bool` — переключатель антд, то есть `<button>`: подпись не вправе именовать
//    через `for` элемент, который подписи не принимает.
const NAMED_BY_LABEL = new Set(["string", "text", "int", "enum", "ref"]);

function isNamedByLabel(field: FF): boolean {
  return NAMED_BY_LABEL.has(field.type);
}

export function FormFieldRenderer({
  field,
  pathPrefix,
  value,
  onChange,
  editMode,
  hideLabel,
  controlId,
  invalid,
  describedBy,
  itemErrors,
}: Props) {
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
        itemErrors={itemErrors}
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
      controlId={controlId}
      invalid={invalid}
      describedBy={describedBy}
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
  controlId,
  invalid,
  describedBy,
}: Props & { disabled?: boolean }) {
  const ownId = useId();
  // Идентификатор снаружи сильнее собственного: подпись рисует вызывающий, и
  // связать её со своим вводом он может только тем идентификатором, который сам
  // и назначил.
  const id = controlId ?? ownId;
  const path = fullPath(pathPrefix, field.name);
  const cur = getByPath(value, path);

  const set = (v: unknown) => onChange(setByPath(value, path, v));

  // Состояние ввода, общее для всех его видов. Обязательность объявляется
  // ЗДЕСЬ, а не только звёздочкой у подписи: звёздочку рисуют `aria-hidden`
  // (она украшение), поэтому без `aria-required` читающий с экрана не узнавал
  // об обязательности вовсе — ни до отправки, ни после.
  const state = {
    "aria-required": field.required ? true : undefined,
    "aria-invalid": invalid ? true : undefined,
    "aria-describedby": invalid ? describedBy : undefined,
  };
  // Линия отказа — свойство виджета библиотеки, а не атрибут DOM, поэтому она
  // отделена от `aria-*`: переключатель её не принимает.
  const errorLine = invalid ? ("error" as const) : undefined;

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
          status={errorLine}
          {...state}
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
          status={errorLine}
          {...state}
        />
      )}
      {field.type === "int" && (
        <Input
          id={id}
          type="number"
          value={displayText(cur)}
          onChange={(e) => set(e.target.value === "" ? undefined : Number(e.target.value))}
          min={field.min}
          max={field.max}
          disabled={disabled}
          status={errorLine}
          {...state}
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
          {...state}
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
          status={errorLine}
          {...state}
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
          required={field.required}
          invalid={invalid}
          describedBy={describedBy}
        />
      )}
    </div>
  );
}

// ArrayItemField — компактная обёртка для поля внутри array-item:
// mini-label сверху (11px, серый), * для required справа, ⓘ-tooltip если есть
// description. Input снизу через children (hideLabel=true в FormFieldRenderer).
//
// ПОДПИСЬ — НАСТОЯЩИЙ `<label for>` ТАМ, ГДЕ ЕЙ ЕСТЬ ЧТО ИМЕНОВАТЬ.
//
// Прежде это был `<span>`: у ввода внутри строки не было доступного имени
// ВООБЩЕ — ни `label for`, ни `aria-label`. Читающий с экрана слышал «поле
// ввода» без единого слова о том, что вводить, а адресовать такое подполе можно
// было только через соседнюю разметку («первый такой-то», «внутри обёртки»).
// Обёртки формы (`.ant-form-item`) у подполя нет by construction — строка
// рисуется обычными `div`, — поэтому сквозная проба целилась во ВНЕШНЕЕ поле
// составного виджета и не находила ничего (#600).
//
// `htmlFor` НЕОБЯЗАТЕЛЕН, и это не послабление. Подполе, рисующее собственное
// поддерево (`custom`, вложенный список, редактор меток), одного ввода не имеет
// — `for` указывал бы в пустоту, то есть подпись утверждала бы, что именует
// контрол, которого по этому адресу нет. Там остаётся текст, как и было.
//
// Звёздочка обязательности и ⓘ стоят ВНЕ `<label>` намеренно: иконка антд несёт
// своё `aria-label`, и внутри подписи она стала бы частью доступного имени
// («Внешний адрес question-circle»).
function ArrayItemField({
  htmlFor,
  label,
  required,
  description,
  children,
}: {
  htmlFor?: string;
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
          fontSize: 11,
          fontWeight: 600,
          letterSpacing: "0.02em",
          color: "var(--kc-text-secondary)",
          lineHeight: 1.2,
          whiteSpace: "nowrap",
        }}
      >
        {htmlFor ? (
          <label htmlFor={htmlFor} style={{ overflow: "hidden", textOverflow: "ellipsis" }}>
            {label}
          </label>
        ) : (
          <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{label}</span>
        )}
        {required && (
          <span style={{ color: "var(--kc-danger)" }} aria-hidden>
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
  hideLabel,
  itemErrors,
}: { field: ArrayField; disabled?: boolean } & Omit<Props, "field">) {
  // Основа идентификаторов подполей строки. Каждое подполе получает свой
  // (`<основа>-<строка>-<имя подполя>`), иначе одна подпись связалась бы со
  // ВСЕМИ вводами того же имени — и правка «второй» строки молча уходила бы в
  // первую.
  const uid = useId();
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

  // Список из ОДНОГО подполя, стоящий в правой колонке: имя поля уже названо
  // левой колонкой, а подпись подполя ("CIDR" под меткой "CIDR IPv4")
  // повторяет её же и ничего не добавляет.
  const soleItemField = hideLabel && field.itemFields.length === 1;

  const rows = (
    <>
      {items.length === 0 && (
        <div style={{ ...editorEmptyStyle, display: "grid", placeItems: "center" }}>— пусто —</div>
      )}
      {items.map((_, idx) => {
        // Колонки считаются по ПОКАЗЫВАЕМЫМ подполям, а не по объявленным.
        // Строка со взаимоисключающей группой объявляет поля ВСЕХ ветвей, а
        // показывает поля одной: счёт по объявленным нарезал бы строку на
        // столько колонок, сколько ветвей, и каждое видимое поле получало бы
        // долю ширины, которой ему не хватает (цели группы — восемь объявленных
        // подполей против трёх-четырёх видимых, #375).
        const visible = field.itemFields.filter((sub) => {
          // visibleWhen — резолвится FormFieldRenderer'ом; здесь фильтруем,
          // чтобы не оставить пустую mini-label-обёртку.
          if (!sub.visibleWhen) return true;
          const rel = sub.visibleWhen.field;
          const relPath = `${path}[${idx}].${rel}`;
          const cur =
            (getByPath(value, relPath) as string | undefined) ?? (getByPath(value, rel) as string | undefined);
          const want = sub.visibleWhen.equals;
          return Array.isArray(want) ? want.includes(cur ?? "") : cur === want;
        });
        return (
        <div
          key={idx}
          style={{
            display: "flex",
            alignItems: "flex-start",
            gap: 8,
            padding: 10,
            borderRadius: 8,
            border: "1px solid var(--kc-border)",
            background: "var(--kc-field)",
          }}
        >
          <div
            style={{
              display: "grid",
              gridTemplateColumns: visible.length > 1 ? `repeat(${visible.length}, minmax(0, 1fr))` : "1fr",
              gap: 8,
              flex: 1,
            }}
          >
            {visible.map((sub) => {
              const subId = `${uid}-${idx}-${sub.name}`;
              // Отказ подполя адресуется ПУТЁМ, а не порядковым номером колонки:
              // строка со взаимоисключающей группой показывает поля одной ветви,
              // и номер колонки означал бы разное в разных строках.
              const subProblem = itemErrors?.[`${path}[${idx}].${sub.name}`];
              const input = (
                <FormFieldRenderer
                  field={sub}
                  pathPrefix={`${path}[${idx}]`}
                  value={value}
                  onChange={onChange}
                  editMode={editMode}
                  hideLabel
                  controlId={subId}
                  invalid={!!subProblem}
                  describedBy={fieldErrorId(subId)}
                />
              );
              if (soleItemField)
                return (
                  <div key={sub.name}>
                    {input}
                    <FieldError id={fieldErrorId(subId)} message={subProblem} />
                  </div>
                );
              return (
                <ArrayItemField
                  key={sub.name}
                  htmlFor={isNamedByLabel(sub) ? subId : undefined}
                  label={sub.label}
                  required={!!sub.required}
                  description={sub.description}
                >
                  {input}
                  <FieldError id={fieldErrorId(subId)} message={subProblem} />
                </ArrayItemField>
              );
            })}
          </div>
          <AntButton
            type="text"
            icon={<DeleteOutlined />}
            onClick={() => removeAt(idx)}
            disabled={disabled}
            danger
            style={{ ...editorIconButtonStyle, flexShrink: 0, marginTop: 2 }}
          />
        </div>
        );
      })}
    </>
  );

  // Список из ОДНОГО текстового подполя — таблицей значений: заголовок
  // колонки, строки со значением, действие на строке и «Добавить» снизу. Тот же
  // вид, что у меток и статических маршрутов, поэтому набор CIDR читается как
  // набор, а не как россыпь отдельных полей (решение владельца 2026-08-12).
  const plainListField =
    hideLabel && field.itemFields.length === 1 && (field.itemFields[0]?.type ?? "string") === "string";

  if (plainListField) {
    const sub = field.itemFields[0];
    const values = items.map((it) => ({ a: typeof it?.value === "string" ? it.value : "", b: "" }));
    return (
      <EditableKVTable
        rows={values}
        // Отказы строк доезжают до самой таблицы. Пока их здесь не было, отказ
        // подполя не показывался НИГДЕ: эта ветвь возвращается раньше `rows`,
        // где сообщение ставится, поэтому форма отказывалась отправляться и
        // молчала о причине — нажатие на «Создать» не давало ни ответа, ни
        // объяснения. Свойство «незаполненное обязательное подполе не уезжает
        // на сервер» при этом держалось, а «форма называет поле» — нет.
        rowErrors={items.map((_, idx) => {
          const message = itemErrors?.[`${path}[${idx}].${sub.name}`];
          return message ? { id: fieldErrorId(`${uid}-${idx}-${sub.name}`), message } : undefined;
        })}
        onChange={(next) => onChange(setByPath(value, path, next.map((r) => ({ value: r.a }))))}
        colA={{
          header: sub.label || field.itemLabel,
          // `placeholder` есть не у каждого вида подполя — читаем только у строкового.
          placeholder: sub.type === "string" ? (sub.placeholder ?? "") : "",
        }}
        // Имя поля стоит СЛЕВА (левая колонка формы), поэтому шапка единственной
        // колонки повторяла бы его вторым словом: «IPv4-адрес» слева и «Address»
        // в шапке. В форме шапка не нужна — смысл колонки задан меткой поля.
        hideHeader
        addLabel={`Добавить ${field.itemLabel}`}
        disabled={disabled}
      />
    );
  }

  if (hideLabel) {
    // Имя и пояснение поля несёт левая колонка формы (`Form.Item label`).
    // Своя рамка с заголовком повторила бы имя второй раз и вывела бы поле из
    // общей сетки — ровно то, из-за чего список читался как чужеродный блок.
    return (
      <div style={disabled ? { opacity: 0.6, pointerEvents: "none" } : undefined}>
        <Space direction="vertical" size={8} style={{ width: "100%" }}>
          {rows}
        </Space>
        <AntButton
          type="dashed"
          block
          icon={<PlusOutlined />}
          onClick={add}
          disabled={disabled || atCap}
          style={{ marginTop: 8 }}
        >
          Добавить {field.itemLabel}
        </AntButton>
      </div>
    );
  }

  return (
    <Card
      size="small"
      title={
        <Space size={8}>
          <Typography.Text strong>{field.label}</Typography.Text>
          {field.required && <span style={{ color: "var(--kc-danger)", fontSize: 12 }}>*</span>}
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
        {rows}
      </Space>
      {field.description && (
        <Typography.Text type="secondary" style={{ fontSize: 11, display: "block", marginTop: 8 }}>
          {field.description}
        </Typography.Text>
      )}
    </Card>
  );
}
