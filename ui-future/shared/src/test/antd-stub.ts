// Общий стенд-заменитель `antd` для проб интерфейса — ОДИН экземпляр на дерево.
//
// Зачем отдельный модуль. `src/test/setup.ts` подменяет `antd` на весь прогон,
// и его набор намеренно грубый: почти всё — простой `<div>`. Отдельным пробам
// этого не хватает: компонент, чьё решение уезжает в `menu`/`title`/`subTitle`
// (Dropdown, Result), на таком заменителе НИЧЕГО не показывает, и проба зеленеет
// при любом составе меню и при любом тексте отказа.
//
// Такая проба обязана переопределить нужную часть — но переопределять `antd`
// целиком нельзя: граф импортов тянет десятки его экспортов, и перечень,
// выписанный рядом с пробой, расходится с деревом молча (падает ссылкой на
// отсутствующий экспорт, причём в чужом файле). Поэтому набор собирается ЗДЕСЬ,
// а проба делает `{ ...antdStub(), Dropdown: <свой> }` — одно место об одном
// предмете.
//
// Заменитель обязан быть НЕ снисходительнее настоящего компонента там, где это
// дёшево: `Table` рисует свои строки по-настоящему (заменитель-`<div>` делал бы
// истинным любое утверждение о содержимом списка).

import React from "react";
import { jest } from "@jest/globals";

type AnyProps = Record<string, unknown>;

interface MockColumn {
  title?: React.ReactNode;
  dataIndex?: string;
  /** Закрепление колонки при горизонтальной прокрутке. */
  fixed?: "left" | "right";
  /** Ширина. Настоящая таблица ИГНОРИРУЕТ `fixed` без неё. */
  width?: number | string;
  render?: (value: unknown, row: unknown, index: number) => React.ReactNode;
  /** Сравнение для сортировки. Настоящая таблица рисует стрелку ровно при нём. */
  sorter?: unknown;
}

interface SelectOption {
  value: unknown;
  label?: React.ReactNode;
}

interface SelectProps {
  children?: React.ReactNode;
  options?: SelectOption[];
  onChange?: (value: string, option?: SelectOption) => void;
  onSearch?: (value: string) => void;
  value?: string;
  placeholder?: React.ReactNode;
  showSearch?: boolean;
  notFoundContent?: React.ReactNode;
  filterOption?: unknown;
  optionFilterProp?: unknown;
  [key: string]: unknown;
}

interface TagProps {
  children?: React.ReactNode;
  closable?: boolean;
  onClose?: (e: { preventDefault: () => void }) => void;
  [key: string]: unknown;
}

interface ResultProps {
  children?: React.ReactNode;
  title?: React.ReactNode;
  subTitle?: React.ReactNode;
  extra?: React.ReactNode;
  [key: string]: unknown;
}

interface AlertProps {
  children?: React.ReactNode;
  message?: React.ReactNode;
  description?: React.ReactNode;
}

interface ButtonProps {
  children?: React.ReactNode;
  loading?: boolean;
  disabled?: boolean;
  onClick?: (e: React.MouseEvent) => void;
  htmlType?: string;
  [key: string]: unknown;
}

interface CardProps {
  children?: React.ReactNode;
  title?: React.ReactNode;
  extra?: React.ReactNode;
  [key: string]: unknown;
}

interface DescriptionItem {
  key?: string | number;
  label?: React.ReactNode;
  children?: React.ReactNode;
}

interface DescriptionsProps {
  items?: DescriptionItem[];
  title?: React.ReactNode;
  children?: React.ReactNode;
  [key: string]: unknown;
}

interface MenuItemProps {
  key?: string;
  label?: React.ReactNode;
  type?: string;
  disabled?: boolean;
}

interface MenuProps {
  items?: MenuItemProps[];
  selectedKeys?: string[];
  onClick?: (info: { key: string }) => void;
  children?: React.ReactNode;
  [key: string]: unknown;
}

interface RadioOption {
  value: unknown;
  label?: React.ReactNode;
}

interface RadioGroupProps {
  options?: RadioOption[];
  value?: unknown;
  onChange?: (e: { target: { value: unknown } }) => void;
  children?: React.ReactNode;
}

/** Вариант переключателя. Настоящий принимает и объект, и голое значение. */
interface SegmentedOption {
  value: unknown;
  label?: React.ReactNode;
  disabled?: boolean;
}

interface SegmentedProps {
  options?: Array<SegmentedOption | string | number>;
  value?: unknown;
  onChange?: (value: unknown) => void;
  [key: string]: unknown;
}

interface SwitchProps {
  checked?: boolean;
  onChange?: (checked: boolean) => void;
  [key: string]: unknown;
}

interface ModalRootProps {
  children?: React.ReactNode;
  open?: boolean;
  className?: string;
  style?: React.CSSProperties;
  [key: string]: unknown;
}

interface MockTableProps {
  columns?: MockColumn[];
  dataSource?: unknown[];
  /** Настоящая таблица принимает и имя поля, и функцию — заменитель обязан тоже. */
  rowKey?: string | ((row: unknown) => string);
  onRow?: (row: unknown) => Record<string, unknown>;
  locale?: { emptyText?: React.ReactNode };
  /** Настоящая таблица рисует столбец флажков ровно при этом свойстве. */
  rowSelection?: {
    selectedRowKeys?: (string | number)[];
    onChange?: (keys: (string | number)[], rows: unknown[]) => void;
  };
}

/** Значение клетки: настоящая таблица достаёт его по `dataIndex` и отдаёт в `render`. */
function cellValue(row: unknown, dataIndex?: string): unknown {
  if (!dataIndex) return undefined;
  return (row as Record<string, unknown> | null)?.[dataIndex];
}

function keyOf(row: unknown, rowKey: MockTableProps["rowKey"], index: number): string | number {
  if (typeof rowKey === "function") return rowKey(row);
  if (typeof rowKey === "string") {
    const v = cellValue(row, rowKey);
    // Ключом годится только скаляр: объект дал бы всем строкам одно и то же
    // «[object Object]», то есть один ключ на весь список.
    if (typeof v === "string" || typeof v === "number") return v;
    return index;
  }
  return index;
}

/**
 * Только те свойства, которые настоящий компонент доносит до DOM: `data-*`,
 * `aria-*`, `id`, `title`. Остальные (`width`, `destroyOnClose`, `maskClosable`,
 * …) — параметры виджета: React ругается на них как на неизвестные атрибуты, а
 * проба, которая начнёт их читать, будет утверждать форму дублёра.
 */
function domAttrs(props: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(props)) {
    if (k.startsWith("data-") || k.startsWith("aria-") || k === "id" || k === "title") out[k] = v;
  }
  return out;
}

export function antdStub(): Record<string, unknown> {
  const Component = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("div", props, children);
  // Настоящая кнопка antd в состоянии `loading` НЕ принимает нажатий (защита от
  // повторной отправки), а её вид задаётся `type`/`size`/`danger` — это
  // параметры виджета, а не атрибуты DOM. Заменитель, отдававший всё в DOM,
  // делал повторную отправку возможной и ронял `type="primary"` в атрибут
  // нативной кнопки.
  const Button = ({ children, loading, disabled, onClick, htmlType, ...props }: ButtonProps) =>
    React.createElement(
      "button",
      {
        type: (htmlType as string) ?? "button",
        disabled: Boolean(disabled) || Boolean(loading),
        onClick,
        className: props.className as string | undefined,
        style: props.style as React.CSSProperties | undefined,
        ...domAttrs(props),
      },
      children,
    );
  // Настоящее поле ввода показывает свои `prefix`/`suffix` (замок с причиной,
  // единицы измерения). Заменитель ронял их, и объяснение «почему нельзя
  // править» было ненаблюдаемо, хотя пользователь его видит.
  const Input = ({ prefix, suffix, ...props }: AnyProps & { prefix?: React.ReactNode; suffix?: React.ReactNode }) =>
    prefix || suffix
      ? React.createElement("span", null, prefix, React.createElement("input", props), suffix)
      : React.createElement("input", props);
  const Search = (props: AnyProps) => React.createElement("input", { type: "search", ...props });
  // Настоящее ЧИСЛОВОЕ поле зовёт `onChange` с ЧИСЛОМ (или `null` на пустоте), а
  // не с событием. Заменитель, отдававший событие, делал недостижимым весь путь
  // «ввёл число → отправили»: вызывающий, разбирающий число, получал объект,
  // сохранял пустоту и молча не отправлял ничего. Дублёр обязан выполнять
  // контракт настоящего, иначе он прячет ровно то, ради чего его подставляют.
  const InputNumber = ({ onChange, value, ...props }: AnyProps & { onChange?: (v: number | null) => void }) =>
    React.createElement("input", {
      type: "number",
      value: value === undefined || value === null ? "" : (value as number),
      onChange: (e: { target: { value: string } }) => {
        const raw = e.target.value;
        onChange?.(raw.trim() === "" || Number.isNaN(Number(raw)) ? null : Number(raw));
      },
      ...props,
    });
  const Textarea = (props: AnyProps) => React.createElement("textarea", props);

  const Table = ({ columns = [], dataSource = [], rowKey, onRow, locale, rowSelection }: MockTableProps) => {
    // Столбец флажков — наблюдаемое следствие `rowSelection`, и он ОБЯЗАН быть
    // настоящим флажком: проба о групповом действии иначе утверждала бы о
    // разметке, которой у заменителя нет, и зеленела бы при любом поведении.
    const selected = new Set((rowSelection?.selectedRowKeys ?? []).map(String));
    const toggle = (key: string) => {
      const next = new Set(selected);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      const keys = [...next];
      rowSelection?.onChange?.(keys, dataSource.filter((r, i) => next.has(String(keyOf(r, rowKey, i)))));
    };
    return React.createElement(
      "table",
      null,
      React.createElement(
        "thead",
        null,
        React.createElement(
          "tr",
          null,
          rowSelection ? React.createElement("th", { key: "__sel__" }) : null,
          columns.map((c, i) =>
            React.createElement(
              "th",
              {
                key: i,
                "data-fixed": c.fixed,
                "data-width": c.width,
                // Стрелка сортировки — наблюдаемое следствие `sorter`. Без неё
                // проба о сортировке утверждала бы о разметке, которой у
                // заменителя нет вовсе, и зеленела бы при любом поведении.
                "data-sortable": c.sorter ? "yes" : undefined,
              },
              c.title,
            ),
          ),
        ),
      ),
      React.createElement(
        "tbody",
        null,
        dataSource.length === 0
          ? React.createElement(
              "tr",
              null,
              React.createElement("td", { colSpan: Math.max(columns.length, 1) }, locale?.emptyText),
            )
          : dataSource.map((row, ri) =>
              React.createElement(
                "tr",
                { key: keyOf(row, rowKey, ri), ...(onRow ? onRow(row) : {}) },
                rowSelection
                  ? React.createElement(
                      "td",
                      { key: "__sel__" },
                      React.createElement("input", {
                        type: "checkbox",
                        checked: selected.has(String(keyOf(row, rowKey, ri))),
                        onChange: () => toggle(String(keyOf(row, rowKey, ri))),
                      }),
                    )
                  : null,
                columns.map((c, ci) => {
                  const value = cellValue(row, c.dataIndex);
                  return React.createElement(
                    "td",
                    { key: ci },
                    c.render ? c.render(value, row, ri) : (value as React.ReactNode),
                  );
                }),
              ),
            ),
      ),
    );
  };
  // Настоящий `Select` рисует ВАРИАНТЫ и отдаёт в `onChange` выбранное
  // ЗНАЧЕНИЕ (не DOM-событие). Заменитель-`<select>` с `options` в атрибуте не
  // показывал ни одного варианта и передавал событие: состав списка был
  // ненаблюдаем, а выбор — невоспроизводим, поэтому проба поневоле утверждала
  // бы форму дублёра.
  //
  // `showSearch` у настоящего `Select` — это ПОЛЕ ВВОДА внутри списка, и ввод в
  // него уезжает в `onSearch`. Дублёр обязан выполнять контракт настоящего в
  // той части, которая и есть предмет пробы: без поля ввода нельзя утверждать
  // ни что ввод уходит запросом на сервер, ни что он не уходит (#528). Поле
  // добавляется РЯДОМ с `<select>` (внутрь нельзя — это невалидная разметка), а
  // роль `combobox` остаётся у `<select>`, поэтому прежние пробы не меняются.
  //
  // `notFoundContent` рисуется, когда вариантов нет: именно там и живёт
  // утверждение об области поиска, ради которого дублёр расширен.
  const Select = ({
    children,
    options,
    onChange,
    onSearch,
    value,
    placeholder,
    showSearch,
    notFoundContent,
    filterOption,
    optionFilterProp,
    ...props
  }: SelectProps) => {
    void filterOption;
    void optionFilterProp;
    const select = React.createElement(
      "select",
      {
        ...props,
        value: value ?? "",
        onChange: (e: { target: { value: string } }) => {
          const picked = (options ?? []).find((o) => String(o.value) === e.target.value);
          onChange?.(e.target.value, picked);
        },
      },
      React.createElement("option", { key: "__placeholder__", value: "" }, placeholder),
      ...(options ?? []).map((o) =>
        React.createElement("option", { key: String(o.value), value: String(o.value) }, o.label),
      ),
      children,
    );
    if (!showSearch && notFoundContent === undefined) return select;
    return React.createElement(
      "div",
      null,
      showSearch
        ? React.createElement("input", {
            key: "__search__",
            type: "search",
            "aria-label": "select-search",
            onChange: (e: { target: { value: string } }) => onSearch?.(e.target.value),
          })
        : null,
      select,
      (options ?? []).length === 0 && notFoundContent !== undefined
        ? React.createElement("div", { key: "__empty__" }, notFoundContent as React.ReactNode)
        : null,
    );
  };
  // Настоящий `Checkbox` — это флажок с подписью. Прежде здесь стоял тот же
  // заменитель, что у текстового поля: у него нет ни роли флажка, ни `checked`
  // у цели события, поэтому настройка видимости колонок была ненаблюдаема
  // целиком.
  const Checkbox = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("label", null, React.createElement("input", { type: "checkbox", ...props }), children);

  // Ссылка настоящей типографики — якорь. Её отсутствие в наборе не «обедняло
  // проверку», а роняло рендер целиком: узел разворачивался в `undefined`.
  const Anchor = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("a", props, children);
  const Typography = Object.assign(Component, {
    Text: Component,
    Title: Component,
    Paragraph: Component,
    Link: Anchor,
  });
  const Layout = Object.assign(Component, {
    Content: Component,
    Header: Component,
    Sider: Component,
  });
  // Настоящий `Form.Item` ПОКАЗЫВАЕТ подпись поля; заменитель ронял её в
  // атрибут, поэтому «какие поля видит пользователь» было ненаблюдаемо, а
  // проба о составе формы утверждала бы форму дублёра.
  const FormItem = ({ children, label }: { children?: React.ReactNode; label?: React.ReactNode }) =>
    React.createElement("div", null, React.createElement("label", null, label), children);
  const Form = Object.assign(Component, {
    Item: FormItem,
    List: Component,
    useForm: () => [{}],
    useWatch: () => undefined,
  });
  // Настоящее модальное окно СКРЫВАЕТ своё содержимое, пока `open` ложно.
  // Заменитель-`<div>` рисовал его всегда, и утверждение «после клика окно
  // показало X» проходило ещё ДО клика — то есть проба закрепляла форму
  // дублёра, а не наблюдаемое. Пропуск `open` (неуправляемое окно) сохраняет
  // прежнее поведение: скрывать нечего.
  //
  // `title` и `footer` РИСУЮТСЯ: настоящее окно показывает и то, и другое, а
  // заменитель ронял их в атрибуты. В `footer` живут кнопки действия — то есть
  // единственное, ради чего окно открывают; пока их не было, ни одна проба не
  // могла нажать «Выдать»/«Сохранить», и всё поведение за этой кнопкой
  // оставалось непроверяемым. `footer={null}` (окно без действий) уважается.
  const ModalRoot = ({
    children,
    open,
    title,
    footer,
    okText,
    cancelText,
    onOk,
    onCancel,
    okButtonProps,
    className,
    style,
    ...rest
  }: ModalRootProps) => {
    if (open === false) return null;
    // `footer={null}` — окно без действий, уважается. Отсутствие `footer` у
    // настоящего окна означает НЕ «нет кнопок», а «кнопки по умолчанию»
    // (`okText`/`cancelText`): именно ими подтверждают подключение тома,
    // удаление и всякое другое необратимое действие. Пока их не было, эти
    // пути были недостижимы для проб целиком.
    const ok = okButtonProps as { disabled?: boolean; loading?: boolean } | undefined;
    const defaultFooter =
      footer === undefined
        ? [
            React.createElement(
              "button",
              {
                key: "cancel",
                type: "button",
                onClick: onCancel as () => void,
              },
              (cancelText as React.ReactNode) ?? "Cancel",
            ),
            React.createElement(
              "button",
              {
                key: "ok",
                type: "button",
                disabled: Boolean(ok?.disabled) || Boolean(ok?.loading),
                onClick: onOk as () => void,
              },
              (okText as React.ReactNode) ?? "OK",
            ),
          ]
        : footer === null
          ? null
          : [footer as React.ReactNode];
    return React.createElement(
      "div",
      { className, style, role: "dialog", ...domAttrs(rest) },
      React.createElement("div", null, title as React.ReactNode),
      children,
      defaultFooter === null ? null : React.createElement("div", null, ...defaultFooter),
    );
  };
  const Modal = Object.assign(ModalRoot, {
    confirm: jest.fn(),
    destroyAll: jest.fn(),
  });
  // `Space.Compact` — не украшение: без него узел `<Space.Compact>` в реальном
  // компоненте разворачивается в `undefined` и падает весь рендер, то есть
  // проба не доходит до утверждений вовсе.
  const Space = Object.assign(Component, { Compact: Component });
  const theme = {
    useToken: () => ({
      token: {
        colorBgContainer: "#ffffff",
        colorBorderSecondary: "#e5e7eb",
        colorError: "#ef4444",
        colorFillSecondary: "#f3f4f6",
        colorPrimary: "#1677ff",
        colorText: "#111827",
        colorTextSecondary: "#6b7280",
      },
    }),
  };

  return {
    __esModule: true,
    // Настоящее уведомление показывает свои `message` и `description`;
    // заменитель ронял их в атрибуты, и текст предупреждения был ненаблюдаем.
    Alert: ({ children, message, description }: AlertProps) =>
      React.createElement("div", { role: "alert" }, message, description, children),
    App: Component,
    AutoComplete: Input,
    Avatar: Component,
    Badge: Component,
    // Поставщик темы обёртывает КАЖДЫЙ remote (`shared/src/lib/theme-context`).
    // Пока его в наборе не было, любая проба, чей граф импортов доходит до темы,
    // падала ссылкой на отсутствующий экспорт — и падала СУИТОЙ ЦЕЛИКОМ, ещё до
    // первого утверждения, то есть сообщением не про свой предмет. Тема на
    // наблюдаемое не влияет, поэтому заменитель — прозрачная обёртка.
    ConfigProvider: ({ children }: React.PropsWithChildren) => React.createElement(React.Fragment, null, children),
    Button,
    // Настоящая карточка показывает свои заголовок и дополнение; заменитель
    // ронял их в атрибуты, поэтому счётчик/подпись в шапке карточки были
    // ненаблюдаемы вовсе.
    Card: ({ children, title, extra, ...rest }: CardProps) =>
      React.createElement(
        "div",
        domAttrs(rest),
        React.createElement("div", null, title),
        React.createElement("div", null, extra),
        children,
      ),
    Cascader: Select,
    Checkbox,
    Col: Component,
    Collapse: Component,
    // Настоящий список свойств рисует ПАРЫ подпись→значение, и на карточке
    // ресурса это весь обзор целиком. Заменитель-`<div>` ронял `items` в
    // атрибут: ни одна строка обзора не была наблюдаема, поэтому «карточка
    // показывает описание» проверить было нечем.
    Descriptions: ({ items, title, children }: DescriptionsProps) =>
      React.createElement(
        "dl",
        null,
        title === undefined ? null : React.createElement("div", null, title as React.ReactNode),
        (items ?? []).map((it, i) =>
          React.createElement(
            "div",
            { key: it.key ?? i },
            React.createElement("dt", null, it.label),
            React.createElement("dd", null, it.children),
          ),
        ),
        children,
      ),
    Divider: Component,
    Dropdown: Component,
    // Настоящий `Empty` рисует своё пояснение; заменитель-`<div>` прятал его в
    // атрибут, и утверждение о тексте пустого списка было недостижимо.
    Empty: ({ children, description }: React.PropsWithChildren<{ description?: React.ReactNode }>) =>
      React.createElement("div", null, description, children),
    Form,
    Image: Component,
    Input: Object.assign(Input, { TextArea: Textarea, Search }),
    InputNumber,
    Layout,
    List: Component,
    // Настоящее меню рисует свои пункты. На карточке ресурса это рейл вкладок:
    // пока пункты уезжали в атрибут, «какие вкладки обещаны» было ненаблюдаемо,
    // и утверждение «вкладки, которой быть не должно, нет» зеленело на пустом
    // заменителе — то есть на всём сразу.
    //
    // ФОРМА ВЗЯТА У НАСТОЯЩЕГО, а не изобретена (#588). Прежде заменитель рисовал
    // пункты `<button>` с `aria-current="page"` — ни того, ни другого меню antd
    // не производит НИКОГДА, и это ровно та ловушка, которую этот файл называет
    // абзацем про `Segmented` (#418): утверждение оказывается прибито к форме
    // дублёра и переживает продукт. Переход на настоящий рендер оставил бы такие
    // пробы зелёными на разметке, которой в консоли нет.
    //
    // Сверено с собранным `antd` в дереве: корень — `<ul role="menu">`; пункт —
    // `<li role="menuitem">` с `aria-disabled` и классами
    // `ant-menu-item{,-selected,-disabled}`; разделитель — `<li role="separator"
    // class="ant-menu-item-divider">`. Выбранность помечается КЛАССОМ, а не
    // `aria-selected`: его настоящий пункт ставит только при `role="option"`.
    //
    // Отклонение допускается одно и только в сторону строгости: запрещённый пункт
    // не зовёт обработчик — как и у настоящего.
    Menu: ({ items, selectedKeys, onClick, children }: MenuProps) =>
      React.createElement(
        "ul",
        { role: "menu" },
        (items ?? []).map((item, i) =>
          item.type === "divider"
            ? React.createElement("li", {
                key: item.key ?? `d${i}`,
                role: "separator",
                className: "ant-menu-item-divider",
              })
            : React.createElement(
                "li",
                {
                  key: item.key ?? i,
                  role: "menuitem",
                  "aria-disabled": Boolean(item.disabled),
                  className: [
                    "ant-menu-item",
                    (selectedKeys ?? []).includes(item.key ?? "") ? "ant-menu-item-selected" : "",
                    item.disabled ? "ant-menu-item-disabled" : "",
                  ]
                    .filter(Boolean)
                    .join(" "),
                  onClick: () => {
                    if (!item.disabled) onClick?.({ key: item.key ?? "" });
                  },
                },
                item.label,
              ),
        ),
        children,
      ),
    Modal,
    Popconfirm: Component,
    // Настоящий `Result` показывает заголовок и пояснение; заменитель ронял их
    // в атрибуты, и текст отказа (в т.ч. сообщение края) был ненаблюдаем.
    Result: ({ children, title, subTitle, extra, ...rest }: ResultProps) =>
      React.createElement(
        "div",
        domAttrs(rest),
        React.createElement("div", null, title),
        React.createElement("div", null, subTitle),
        React.createElement("div", null, extra),
        children,
      ),
    // Настоящая радиогруппа — набор переключателей с общим именем. Её
    // отсутствие в наборе не «обедняло проверку», а роняло суиту целиком:
    // ESM-линкер не находит binding и уводит прогон ещё до первого утверждения.
    Radio: Object.assign(
      ({ children, value, ...rest }: React.PropsWithChildren<AnyProps>) =>
        React.createElement(
          "label",
          null,
          React.createElement("input", { type: "radio", value: value as string, ...domAttrs(rest) }),
          children,
        ),
      {
        Group: ({ children, options, value, onChange }: RadioGroupProps) =>
          React.createElement(
            "div",
            { role: "radiogroup" },
            (options ?? []).map((o) =>
              React.createElement(
                "label",
                { key: String(o.value) },
                React.createElement("input", {
                  type: "radio",
                  value: String(o.value),
                  checked: value === o.value,
                  onChange: () => onChange?.({ target: { value: o.value } }),
                }),
                o.label,
              ),
            ),
            children,
          ),
        Button: ({ children, ...rest }: React.PropsWithChildren<AnyProps>) =>
          React.createElement("button", { type: "button", ...domAttrs(rest) }, children),
      },
    ),
    Row: Component,
    // Настоящий переключатель рисует варианты ВИДИМЫМ ТЕКСТОМ — по ним оператор
    // и выбирает. Варианты приходят пропом `options`, детьми он их не получает,
    // поэтому заменитель-`<div>` не показывал НИ ОДНОГО: свойство «какой выбор
    // предложен» было ненаблюдаемо во всех девяти местах, где переключатель
    // стоит (реестры ресурсов, ветвь спецификации интерфейса, панели ключей и
    // токенов, страница ролей, источник VIP балансировщика, вид списка). Проба
    // на таком дублёре зелена при любом составе вариантов — то есть хуже
    // отсутствующей: слот занят, уверенность создана, предмета нет.
    //
    // ФОРМА ВЗЯТА У НАСТОЯЩЕГО, а не изобретена: `rc-segmented` рисует на
    // вариант `<label>` с радио-полем внутри, и подпись — текст этого label'а.
    // Отсюда доступное имя варианта, по которому проба его и находит. Роль или
    // атрибут, которых настоящий компонент не производит, добавлять НЕЛЬЗЯ:
    // утверждение окажется прибито к форме дублёра и переживёт продукт — так
    // уже вышло с подписью-подсказкой у `Select` (#418).
    //
    // Отклонение допускается одно и только в сторону строгости: `disabled`
    // варианта попадает на поле ввода, поэтому выбрать запрещённое нельзя — как
    // и у настоящего. Дублёр, принимающий больше настоящего, прячет ровно тот
    // дефект, ради которого его подставляют.
    Segmented: ({ options, value, onChange, ...rest }: SegmentedProps) =>
      React.createElement(
        "div",
        domAttrs(rest),
        (options ?? []).map((raw) => {
          const o: SegmentedOption =
            typeof raw === "object" && raw !== null ? raw : { value: raw, label: String(raw) };
          return React.createElement(
            "label",
            { key: String(o.value) },
            React.createElement("input", {
              type: "radio",
              value: String(o.value),
              checked: value === o.value,
              disabled: Boolean(o.disabled),
              onChange: () => onChange?.(o.value),
            }),
            o.label ?? String(o.value),
          );
        }),
      ),
    Select,
    // `Skeleton` стоит на пути монтирования страниц реестра (заглушка загрузки
    // в `RepositoryTagsPanel`). Приехал сюда вместе со сведением окружения проб
    // (#418): у registry он был в СВОЁМ наборе, у общего — нет, и без него
    // ESM-линкер валил бы суиту целиком до первого утверждения.
    Skeleton: Component,
    Space,
    Spin: Component,
    Statistic: Component,
    // Настоящий переключатель отдаёт в `onChange` НОВОЕ СОСТОЯНИЕ (`boolean`),
    // а не событие DOM. Заменитель-поле ввода отдавал событие: оно истинно
    // всегда, поэтому «выключить» не срабатывало НИ РАЗУ, а проба, написанная
    // на таком дублёре, закрепляла бы это как норму. Роль — `switch`, как у
    // настоящего: иначе переключатель недостижим по доступному имени.
    Switch: ({ checked, onChange, ...rest }: SwitchProps) =>
      React.createElement("input", {
        type: "checkbox",
        role: "switch",
        checked: Boolean(checked),
        onChange: (e: React.ChangeEvent<HTMLInputElement>) => onChange?.(e.target.checked),
        ...domAttrs(rest),
      }),
    Table,
    Tabs: Component,
    // Настоящий закрываемый `Tag` рисует крестик с доступным именем `close`
    // (aria-label самого antd). Заменитель-`<div>` его не рисовал вовсе, и
    // снятие элемента из набора было ненаблюдаемо — то есть проверялась
    // только та половина виджета, где ничего не меняется.
    Tag: ({ children, closable, onClose, color: _color, ...rest }: TagProps) =>
      React.createElement(
        "span",
        rest,
        children,
        closable
          ? React.createElement(
              "button",
              {
                type: "button",
                "aria-label": "close",
                onClick: () => onClose?.({ preventDefault: () => {} }),
              },
              "\u00d7",
            )
          : null,
      ),
    // Подпись подсказки достаётся нативным `title`: у настоящего antd она видна
    // при наведении, здесь — читается атрибутом. Пропасть она не может.
    //
    // В атрибут уезжает ТОЛЬКО текстовая подпись. Атрибут `title` по своей природе
    // строка, а `React.ReactNode` — это и узел: `String(<узел>)` дал бы
    // `[object Object]`, то есть один и тот же текст для любой подсказки. Тогда
    // проба, утверждающая «подсказка объясняет, почему нельзя править», проходила
    // бы при ЛЮБОМ содержании подсказки — и при пустом тоже. Узловая подпись
    // поэтому РИСУЕТСЯ: её содержимое остаётся наблюдаемым по тексту.
    Tooltip: ({ children, title }: { children?: React.ReactNode; title?: React.ReactNode }) => {
      const text = typeof title === "string" || typeof title === "number" ? String(title) : undefined;
      return React.createElement("span", text ? { title: text } : {}, children, text === undefined ? title : null);
    },
    Tree: Component,
    Typography,
    theme,
  };
}
