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
  render?: (value: unknown, row: unknown, index: number) => React.ReactNode;
}

interface SelectOption {
  value: unknown;
  label?: React.ReactNode;
}

interface SelectProps {
  children?: React.ReactNode;
  options?: SelectOption[];
  onChange?: (value: string, option?: SelectOption) => void;
  value?: string;
  placeholder?: React.ReactNode;
  [key: string]: unknown;
}

interface TagProps {
  children?: React.ReactNode;
  closable?: boolean;
  onClose?: (e: { preventDefault: () => void }) => void;
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
      ? React.createElement(
          "span",
          null,
          prefix as React.ReactNode,
          React.createElement("input", props),
          suffix as React.ReactNode,
        )
      : React.createElement("input", props);
  const Search = (props: AnyProps) => React.createElement("input", { type: "search", ...props });
  const Textarea = (props: AnyProps) => React.createElement("textarea", props);

  const Table = ({ columns = [], dataSource = [], rowKey, onRow, locale }: MockTableProps) =>
    React.createElement(
      "table",
      null,
      React.createElement(
        "thead",
        null,
        React.createElement(
          "tr",
          null,
          columns.map((c, i) => React.createElement("th", { key: i }, c.title)),
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
  // Настоящий `Select` рисует ВАРИАНТЫ и отдаёт в `onChange` выбранное
  // ЗНАЧЕНИЕ (не DOM-событие). Заменитель-`<select>` с `options` в атрибуте не
  // показывал ни одного варианта и передавал событие: состав списка был
  // ненаблюдаем, а выбор — невоспроизводим, поэтому проба поневоле утверждала
  // бы форму дублёра.
  const Select = ({ children, options, onChange, value, placeholder, ...props }: SelectProps) =>
    React.createElement(
      "select",
      {
        ...props,
        value: value ?? "",
        onChange: (e: { target: { value: string } }) => {
          const picked = (options ?? []).find((o) => String(o.value) === e.target.value);
          onChange?.(e.target.value, picked);
        },
      },
      React.createElement("option", { key: "__placeholder__", value: "" }, placeholder as React.ReactNode),
      ...(options ?? []).map((o) =>
        React.createElement("option", { key: String(o.value), value: String(o.value) }, o.label as React.ReactNode),
      ),
      children,
    );
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
    React.createElement("div", null, React.createElement("label", null, label as React.ReactNode), children);
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
  const ModalRoot = ({ children, open, className, style, ...rest }: ModalRootProps) =>
    open === false
      ? null
      : React.createElement("div", { className, style, role: "dialog", ...domAttrs(rest) }, children);
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
      React.createElement("div", { role: "alert" }, message as React.ReactNode, description as React.ReactNode, children),
    App: Component,
    AutoComplete: Input,
    Avatar: Component,
    Badge: Component,
    Button,
    // Настоящая карточка показывает свои заголовок и дополнение; заменитель
    // ронял их в атрибуты, поэтому счётчик/подпись в шапке карточки были
    // ненаблюдаемы вовсе.
    Card: ({ children, title, extra, ...rest }: CardProps) =>
      React.createElement(
        "div",
        domAttrs(rest),
        React.createElement("div", null, title as React.ReactNode),
        React.createElement("div", null, extra as React.ReactNode),
        children,
      ),
    Cascader: Select,
    Checkbox,
    Col: Component,
    Collapse: Component,
    Descriptions: Component,
    Divider: Component,
    Dropdown: Component,
    // Настоящий `Empty` рисует своё пояснение; заменитель-`<div>` прятал его в
    // атрибут, и утверждение о тексте пустого списка было недостижимо.
    Empty: ({ children, description }: React.PropsWithChildren<{ description?: React.ReactNode }>) =>
      React.createElement("div", null, description, children),
    Form,
    Image: Component,
    Input: Object.assign(Input, { TextArea: Textarea, Search }),
    InputNumber: Input,
    Layout,
    List: Component,
    Menu: Component,
    Modal,
    Popconfirm: Component,
    Result: Component,
    Row: Component,
    Segmented: Component,
    Select,
    Space,
    Spin: Component,
    Statistic: Component,
    Switch: Input,
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
    Tooltip: ({ children, title }: { children?: React.ReactNode; title?: React.ReactNode }) =>
      React.createElement("span", title ? { title: String(title) } : {}, children),
    Tree: Component,
    Typography,
    theme,
  };
}
