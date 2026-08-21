// src/components/form/ResourceFormBody.tsx
// ResourceFormBody — ЕДИНЫЙ рендер тела Create/Edit формы ресурса. Рендерится
// и modal-шеллом (Inline*Form), и page-шеллом (ResourceCreate/EditPage), что
// даёт паритет create==edit==modal==page. Шеллы владеют state + mutation +
// Operation-flow и передают obj/onChange/lockedPaths/submit сюда.
import { useId, useState } from "react";
import { Alert, Form } from "antd";
import { FormFieldRenderer } from "@shared/components/organisms/form/FormField";
import { FormGrid } from "@shared/components/organisms/form/FormGrid";
import { FormShell } from "@shared/components/organisms/form/FormShell";
import { FieldLabel } from "@shared/components/organisms/form/FieldLabel";
import { FieldError, fieldErrorId } from "@shared/components/organisms/form/FieldError";
import { FormFooter } from "@shared/components/organisms/form/FormFooter";
import { ImmutableField } from "@shared/components/organisms/form/ImmutableField";
import { checkFields, hasErrors } from "@shared/components/organisms/form/field-rules";
import { FORM_DIVIDER_STYLE } from "@shared/components/organisms/form/editor-surface";
import { getByPath } from "@shared/lib/path";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import type { FormField } from "@shared/lib/form-schema";
import { displayText } from "@shared/lib/display-text";

export interface ResourceFormBodyProps {
  spec: ResourceSpec;
  mode: "create" | "edit";
  obj: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  /** preset/immutable paths → read-only ImmutableField. */
  lockedPaths?: Set<string>;
  /** per-field enum option narrowing (create-context). */
  fieldOptionsFilter?: Record<string, string[]>;
  /** Заголовок формы. Не задан — шапка называет действие с предметом одной
   *  строкой («Создать подсеть»); надзаголовка-действия над ней нет. */
  title?: string;
  submitLabel: string;
  submitting: boolean;
  onSubmit: () => void;
  onCancel: () => void;
  /** sticky footer for tall forms. */
  stickyFooter?: boolean;
}

// Канон формы — имя поля в левой колонке, ввод в правой. Во всю ширину выходит
// только то, что в правую колонку не помещается: редактор правил SG и custom-
// виджеты (их размер знает автор поля — он и ставит `fullWidth` явно).
//
// Список сам по себе поводом не является: список из ОДНОГО подполя (CIDR
// супернета, ссылка на адрес, ссылка на группу) — обычное поле с несколькими
// значениями, и его вывод из канона делал форму разноголосой. Полная ширина
// нужна списку СОСТАВНОМУ, чьи подполя стоят колонками (спецификации
// интерфейсов: custom-секция NIC плюс вложенный список групп).
const ALWAYS_FULL_WIDTH = new Set(["sg-rules", "custom"]);

export function isFullWidthField(f: FormField): boolean {
  if (f.fullWidth !== undefined) return f.fullWidth;
  if (f.type === "array") return f.itemFields.length > 1;
  return ALWAYS_FULL_WIDTH.has(f.type);
}

// Экспортирована для тестов.
// Сравнение — по СТРОКОВОМУ виду текущего значения: `equals` объявлен строкой, а
// читаемое поле может быть любым значением формы. Bool-тумблер читается как
// `false`, что строке "false" не равно — гейт на нём молча прятал поле навсегда.
export function matchesVisibleWhen(
  obj: Record<string, unknown>,
  vw: { field: string; equals: string | string[] } | undefined,
): boolean {
  if (!vw) return true;
  const raw = getByPath(obj, vw.field);
  if (raw === undefined || raw === null) return false;
  const cur = displayText(raw);
  return Array.isArray(vw.equals) ? vw.equals.includes(cur) : cur === vw.equals;
}


function displayValue(obj: Record<string, unknown>, field: FormField): React.ReactNode {
  const raw = getByPath(obj, field.name);
  if (field.type === "enum") {
    const opt = field.options.find((o) => o.value === raw);
    if (opt) return opt.label;
  }
  return displayText(raw);
}

export function ResourceFormBody({
  spec,
  mode,
  obj,
  onChange,
  lockedPaths,
  fieldOptionsFilter,
  title,
  submitLabel,
  submitting,
  onSubmit,
  onCancel,
  stickyFooter,
}: ResourceFormBodyProps) {
  // Хуки стоят ДО раннего возврата: ресурс без схемы формы уходит из функции
  // ниже, и хук, оказавшийся за этим возвратом, вызывался бы не на каждом
  // рендере — это нарушение правил хуков, а не стилистика.
  //
  // `attempted` — «отправку уже пробовали». Отдельным состоянием, а не выводом
  // из `submitting`: отправка не начинается, пока форма не прошла проверку, и
  // вывод по `submitting` означал бы «отказы показываются только тогда, когда
  // отказов нет».
  const [attempted, setAttempted] = useState(false);
  const uid = useId();
  const fields = spec.fields;
  if (!fields) {
    return <Alert type="warning" message={`У ресурса ${spec.singular} нет form-schema; используйте API напрямую.`} />;
  }
  const editMode = mode === "edit";
  const locked = lockedPaths ?? new Set<string>();

  const shown = fields.filter((f) => {
    if (f.hidden) return false;
    if (editMode && (f.editHidden || f.createOnly)) return false;
    if (!editMode && f.updateOnly) return false;
    return matchesVisibleWhen(obj, f.visibleWhen);
  });

  // ОБЩИЕ ПОЛЯ ИДУТ ПЕРВЫМИ И ВСЕГДА В ОДНОМ ПОРЯДКЕ (решение владельца):
  // имя → описание → метки, затем черта, затем поля самого ресурса.
  //
  // Порядок объявления в реестре у ресурсов разный — у подсети форма начиналась
  // с выбора сети, у прочих с имени. Из-за этого рука шла к разным местам на
  // соседних формах, а метки находились то вторыми, то последними. Между тем
  // первые три поля есть у КАЖДОГО ресурса и означают везде одно, так что их
  // место — свойство продукта, а не отдельной формы.
  //
  // Порядок задаётся ЗДЕСЬ, а не переписыванием реестров: реестров восемь, и
  // договорённость, живущая в восьми местах, разойдётся при первом же новом
  // ресурсе. Поля, которых у ресурса нет, просто отсутствуют — черта тогда
  // встаёт сразу после тех, что есть, а если общих полей нет вовсе, её нет.
  const COMMON_ORDER = ["name", "description", "labels"];
  const common = COMMON_ORDER.map((n) => shown.find((f) => f.name === n)).filter(
    (f): f is (typeof shown)[number] => f !== undefined,
  );
  const rest = shown.filter((f) => !COMMON_ORDER.includes(f.name));
  const visible = [...common, ...rest];
  // Черта отделяет «как назвать» от «чем это будет»: индекс, ПЕРЕД которым она
  // рисуется. Ноль означает, что общих полей у ресурса нет и отделять нечего.
  const dividerBefore = common.length > 0 && rest.length > 0 ? common.length : -1;

  // Отказы показываются ПОСЛЕ попытки отправки, а не с первого рендера: форма,
  // краснеющая ещё до того, как её тронули, обвиняет за незаполненное поле того,
  // кто только что её открыл. Обязательность до отправки сообщает звёздочка у
  // подписи и `aria-required` у самого ввода.
  //
  // Считаются они на КАЖДОМ рендере по текущему `obj`, а не запоминаются снимком:
  // поправленное поле выходит из отказа сразу, как только его тронули, а
  // соседнее остаётся названным. Снимок, сделанный в момент отправки, держал бы
  // красным поле, которое уже исправлено, — и подталкивал бы жать «создать»
  // второй раз, чтобы узнать, помогло ли.
  const errors = attempted ? checkFields(visible, obj, { editMode, lockedPaths: locked }) : {};

  const handleSubmit = () => {
    setAttempted(true);
    if (hasErrors(checkFields(visible, obj, { editMode, lockedPaths: locked }))) return;
    onSubmit();
  };

  const renderField = (f: (typeof shown)[number]) => {
          const isLocked = locked.has(f.name) || (editMode && !!f.immutable);
          const fullWidth = isFullWidthField(f);

          // Locked scalar/ref → read-only affordance (not hidden, not silent-disabled).
          if (isLocked && !fullWidth && f.type !== "labels") {
            return (
              <Form.Item key={f.name} label={<FieldLabel text={f.label} info={f.description} />}>
                <ImmutableField
                  value={displayValue(obj, f)}
                  reason={editMode ? "Неизменяемо после создания" : "Задано из контекста"}
                />
              </Form.Item>
            );
          }

          const allowed = fieldOptionsFilter?.[f.name];
          const field =
            allowed && f.type === "enum"
              ? {
                  ...f,
                  options: allowed
                    .map((v) => f.options.find((o) => o.value === v))
                    .filter((o): o is { value: string; label: string } => !!o),
                }
              : f;

          // Здесь показывается отказ САМОГО поля — и только он. Отказ подполя
          // строки списка наверх НЕ всплывает: его показывает сама строка, у
          // которой не заполнили ввод (`FormField` ставит `FieldError` в строке
          // с одним подполем, в строке с несколькими и в строке таблицы
          // значений). Всплытие давало то же сообщение вторым местом — у поля
          // целиком, — и читатель искал незаполненный ввод глазами по всему
          // списку, хотя претензия относилась к одной его строке.
          //
          // Прежняя редакция этого примечания утверждала обратное («отказ поля и
          // отказ строки — ОДНО сообщение под полем») и обосновывала это тем,
          // что у строки таблицы значений места под сообщение нет by
          // construction. Оба утверждения пережили свой предмет: всплытие снято
          // решением владельца, а место у строки заведено (`EditableKVTable`,
          // проп `rowErrors`). Два места об одном предмете, из которых верно
          // одно, — поэтому неверное снято, а не оставлено рядом.
          const problem = errors[f.name];
          const errId = fieldErrorId(`${uid}-${f.name}`);

          const inner = (
            <>
              <FormFieldRenderer
                field={field}
                pathPrefix=""
                value={obj}
                onChange={onChange}
                editMode={editMode}
                hideLabel={!fullWidth}
                invalid={!!errors[f.name]}
                describedBy={errId}
                itemErrors={errors}
              />
              <FieldError id={errId} message={problem} />
            </>
          );

          if (fullWidth) {
            return (
              <Form.Item key={f.name} wrapperCol={{ offset: 0, flex: "auto" }} colon={false}>
                {inner}
              </Form.Item>
            );
          }
          return (
            <Form.Item key={f.name} label={<FieldLabel text={f.label} info={f.description} />} required={!!f.required}>
              {inner}
            </Form.Item>
          );
  };

  return (
    <FormShell specId={spec.id} mode={mode} singular={spec.singular} accusative={spec.accusative} title={title}>
      <FormGrid>
        {common.map(renderField)}
        {/* ЧЕРТА отделяет «как назвать» от «чем это будет». Она часть сетки
            (`gridColumn: 1 / -1`), а не обёртка вокруг поля: обёртка сломала бы
            раскладку «имя слева, ввод справа», которую задаёт сама сетка.

            Стиль берётся ОБЪЯВЛЕННЫЙ (`FORM_DIVIDER_STYLE`), а не выписанный
            здесь. Выписанный был дословной копией объявления — и это ровно тот
            случай, ради которого объявление и заводили: две черты разной
            толщины на соседних формах читаются как разные места продукта, а
            пока копии совпадают побайтово, расхождения не видно вовсе. */}
        {dividerBefore > 0 && <div style={FORM_DIVIDER_STYLE} aria-hidden />}
        {rest.map(renderField)}

        {/* Футер — вне грид-Form.Item (иначе наследует пустую 200px label-колонку
            и кнопки/разделитель уезжают вправо). Рендерим на всю ширину формы. */}
        <FormFooter
          submitLabel={submitLabel}
          submitting={submitting}
          onSubmit={handleSubmit}
          onCancel={onCancel}
          sticky={stickyFooter}
        />
      </FormGrid>
    </FormShell>
  );
}
