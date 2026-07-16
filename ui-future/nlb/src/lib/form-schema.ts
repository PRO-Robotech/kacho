// Схема формы — описывает поля для resource Create/Edit dialogue.
// Используется ResourceForm для рендеринга нативных полей вместо JSON-textarea.

import type { ReactNode } from "react";

export type FormField =
  | StringField
  | TextField
  | IntField
  | EnumField
  | RefField
  | ArrayField
  | BoolField
  | SgRulesField
  | LabelsField
  | CustomField;

interface BaseField {
  name: string; // dotted-path: "metadata.name", "spec.rules[0].direction"
  label: string;
  description?: string;
  required?: boolean;
  // Hidden — поле формы не показывается, но входит в payload (например metadata.projectId fills из контекста)
  hidden?: boolean;
  // Immutable after Create — в Edit-режиме поле рендерится disabled и
  // не попадает в update_mask. Backend всё равно бы отказал (см.
  // applySubnetMask `v4_cidr_blocks is immutable after Subnet.Create`),
  // но UI ловит это раньше + сразу подсказывает пользователю.
  immutable?: boolean;
  // Edit-only-hidden — поле есть в Create, но в Edit вообще не рендерится.
  // Используется когда поле управляется отдельным action'ом на DetailPage
  // (например, Subnet.v4_cidr_blocks → :add-cidr-blocks/:remove-cidr-blocks).
  editHidden?: boolean;
  // Create-only — поле только для Create (напр. create_default_security_group):
  // в Edit не рендерится (ресурс такого поля не имеет). KAC-239. Семантически
  // = editHidden; отдельное имя для читаемости registry.
  createOnly?: boolean;
  // Условная видимость поля по значению другого поля формы (top-level path).
  // Используется для proto oneof: discriminator-enum (`_address_kind`) скрывает
  // неактивную ветку (external_* vs internal_*). Поле всё ещё может присутствовать
  // в `obj`, sanitize стрижёт его перед отправкой.
  visibleWhen?: { field: string; equals: string | string[] };
  // Override авто-определения full-width (по умолчанию array/sg-rules/custom →
  // full-width без label-колонки). fullWidth:false рендерит поле как обычное
  // labeled (label слева 200px + контрол в wrapper-колонке) — например custom
  // static_routes (RoutesEditor), чтобы выровнять с остальными полями.
  fullWidth?: boolean;
}

export interface StringField extends BaseField {
  type: "string";
  placeholder?: string;
  default?: string;
  pattern?: string;
}

export interface TextField extends BaseField {
  type: "text";
  placeholder?: string;
  rows?: number;
}

export interface IntField extends BaseField {
  type: "int";
  min?: number;
  max?: number;
  default?: number;
}

export interface EnumField extends BaseField {
  type: "enum";
  options: { value: string; label: string }[];
  default?: string;
}

export interface BoolField extends BaseField {
  type: "bool";
  default?: boolean;
}

export interface RefField extends BaseField {
  type: "ref";
  // ID ресурса в registry откуда тянуть варианты
  refResource: string;
  // Если true — фильтруем по выбранному project (selector field=project_id op=EQ values=[currentProject])
  refProjectScoped?: boolean;
  placeholder?: string;
  // Динамический query-параметр для candidate-list, ключённый по значению
  // другого поля той же формы. Пример: { param: "subnet_id", field: "subnet_id" }
  // → GET <apiPath>?subnet_id=<form.subnet_id>. Если поле-источник пустое —
  // список не загружается (enabled: false).
  refQueryFromField?: { param: string; field: string };
  // Клиентский фильтр-предикат поверх загруженного candidate-list: остаются
  // только строки, для которых вернул true. Применяется ПОСЛЕ серверного
  // запроса (refProjectScoped / refQueryFromField). Пример (NIC-форма):
  // v4_address_ids → row => !!row.internal_ipv4_address (только внутренние IPv4).
  refFilter?: (row: Record<string, unknown>) => boolean;
  // ID ресурса в registry, который можно создать прямо из дропдауна
  // («+ Создать …» entry → открывает InlineResourceCreateForm в модалке,
  // на success подставляет id созданного ресурса в это поле).
  createResource?: string;
  // Pre-fill для inline-create-формы, вычисляемый из текущего значения формы
  // (paths → values; передаётся как presetFields в InlineResourceCreateForm).
  createPresetFields?: (form: Record<string, unknown>) => Record<string, unknown>;
  // Заголовок модалки создания (по умолчанию — "Создать <singular>").
  createTitle?: string;
}

export interface ArrayField extends BaseField {
  type: "array";
  itemFields: FormField[]; // sub-fields для одного элемента (paths внутри элемента, без префикса родителя)
  itemLabel: string; // как назвать «одну единицу»: "Rule", "Listener"
  // Минимум элементов (если 0 — массив можно опустить)
  minItems?: number;
  // Максимум элементов. Когда items.length >= maxItems — кнопка «Добавить»
  // дизейблится. Используется для NIC `v4_address_ids`/`v6_address_ids`
  // (cardinality ≤ 1 — KAC-55: один v4 + один v6 на NIC; multi-IP per VM
  // делается через несколько NIC).
  maxItems?: number;
  // Default для нового элемента
  newItem?: () => Record<string, unknown>;
}

// Editor для map<string,string> (Yandex Cloud labels). Хранится в obj как
// объект {key: value}; UI рендерит через LabelsEditor с rows-style редактором.
export interface LabelsField extends BaseField {
  type: "labels";
}

// Специализированный editor для VPC SecurityGroup rules — слишком много conditional
// (oneof target, opt-in protocol/ports), generic ArrayField это не выражает.
// Render через SgRulesEditor; sanitize вычищает `_*` дискриминаторы при submit.
export interface SgRulesField extends BaseField {
  type: "sg-rules";
}

// Bespoke-поле: рендерится произвольным React-компонентом. Используется когда
// логика поля слишком специфична для generic-механизма (multi-path write,
// cross-resource lookup, inline-create + дерево опций) — например NIC-секция
// формы создания Instance (Network→Address Cascader + Segmented external-IP).
// render() получает весь объект формы + setter + pathPrefix контейнера
// (для array-item — "network_interface_specs[0]"). sanitize ресурса должен
// вычистить введённые этим полем `_*`-служебные ключи перед submit.
export interface CustomField extends BaseField {
  type: "custom";
  render: (props: {
    pathPrefix: string;
    value: Record<string, unknown>;
    onChange: (next: Record<string, unknown>) => void;
    editMode?: boolean;
    field: CustomField;
  }) => ReactNode;
}
