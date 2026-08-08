// Дублёр antd для проб iam, которые МОНТИРУЮТ таблицы и окна.
//
// Зачем свой, если общий стаб уже есть. Общий (shared/src/test/setup.ts) считает
// `rowKey` функцией и бросает `rowKey is not a function` на `rowKey="id"` —
// форме, которую настоящий antd принимает и которой пользуются панели токенов.
// То есть дублёр СТРОЖЕ настоящего, и проба падает на самом монтировании, а не
// на поведении. Кроме того общий стаб не подставляет значение ячейки по
// `dataIndex` и рисует окно независимо от `open` — на таком дублёре утверждения
// про содержимое строки и про «окно закрыто» ничего не значат.
//
// Направление отклонений здесь одно: БЛИЖЕ к настоящему antd, никогда не мягче.
// Строка получает своё значение, закрытое окно не рисуется, подтверждение
// действия требует нажатия кнопки с тем же текстом, что у настоящего Popconfirm.
//
// Живёт в пакете iam намеренно: общий набор проб переписывается параллельно, и
// править его отсюда значило бы менять чужой предмет ради своего.
//
// Перечень имён устареть МОЛЧА не может: недостающее имя ESM-линкер объявляет
// ошибкой «does not provide an export named …» и суита краснеет целиком. Свежий
// перечень выводится, а не вспоминается:
//   git grep -zPo 'import\s*\{[^}]*\}\s*from\s*"antd"' -- ui-future/iam/src ui-future/shared/src

import React from "react";

type Props = React.PropsWithChildren<Record<string, unknown>>;

const Div = ({ children, ...p }: Props) => React.createElement("div", p, children);

const Button = ({ children, onClick, disabled, ...p }: Props) =>
  React.createElement(
    "button",
    { type: "button", onClick: onClick as React.MouseEventHandler, disabled: disabled as boolean, ...p },
    children,
  );

const Input = ({ onChange, value, ...p }: Props) =>
  React.createElement("input", { onChange: onChange as React.ChangeEventHandler, value: value as string, ...p });

const TextArea = ({ onChange, value, ...p }: Props) =>
  React.createElement("textarea", { onChange: onChange as React.ChangeEventHandler, value: value as string, ...p });

interface Column {
  title?: React.ReactNode;
  dataIndex?: string;
  key?: string;
  render?: (value: unknown, row: unknown, index: number) => React.ReactNode;
}

interface TableProps {
  columns?: Column[];
  dataSource?: Record<string, unknown>[];
  rowKey?: string | ((row: unknown) => string);
  onRow?: (row: unknown) => Record<string, unknown>;
  locale?: { emptyText?: React.ReactNode };
}

/**
 * Таблица рисует строки по-настоящему и ПОДСТАВЛЯЕТ значение ячейки по
 * `dataIndex` — иначе любое утверждение о содержимом строки было бы утверждением
 * о дублёре. `rowKey` принимается и строкой, и функцией, как у настоящего antd.
 */
const Table = ({ columns = [], dataSource = [], rowKey, onRow, locale }: TableProps) => {
  const keyOf = (row: Record<string, unknown>, i: number): string => {
    if (typeof rowKey === "function") return rowKey(row);
    if (typeof rowKey === "string") return String(row[rowKey] ?? i);
    return String(i);
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
              { key: keyOf(row, ri), ...(onRow ? onRow(row) : {}) },
              columns.map((c, ci) =>
                React.createElement(
                  "td",
                  { key: ci },
                  c.render
                    ? c.render(c.dataIndex ? row[c.dataIndex] : undefined, row, ri)
                    : c.dataIndex
                      ? String(row[c.dataIndex] ?? "")
                      : null,
                ),
              ),
            ),
          ),
    ),
  );
};

/** Закрытое окно не рисуется вовсе — как у настоящего antd (destroyOnClose off тут неважен). */
const Modal = ({ open, title, footer, children, ...p }: Props) =>
  open
    ? React.createElement(
        "div",
        { role: "dialog", ...p },
        React.createElement("div", { "data-part": "title" }, title as React.ReactNode),
        React.createElement("div", { "data-part": "body" }, children),
        React.createElement("div", { "data-part": "footer" }, footer as React.ReactNode),
      )
    : null;

/**
 * Подтверждение действия: триггер + всплывающая часть (заголовок, пояснение и
 * кнопка с тем же текстом, что у настоящего `okText`). Действие НЕ выполняется
 * само — нужно нажать, как и в жизни.
 *
 * Отличие от настоящего названо прямо: всплывающая часть рисуется СРАЗУ, а не по
 * нажатию. Это делает читаемым предупреждение, ради которого её и пишут; скрыть
 * отсутствующий текст такая поблажка не может — отсутствующего не видно в обоих
 * случаях. Что она НЕ проверяет — что предупреждение показывается вовремя.
 */
const Popconfirm = ({ children, onConfirm, okText, title, description, ...p }: Props) =>
  React.createElement(
    "div",
    p,
    children,
    React.createElement(
      "div",
      { role: "tooltip" },
      React.createElement("div", null, title as React.ReactNode),
      React.createElement("div", null, description as React.ReactNode),
      React.createElement(
        "button",
        { type: "button", onClick: onConfirm as React.MouseEventHandler },
        (okText as React.ReactNode) ?? "OK",
      ),
    ),
  );

const Segmented = ({ options = [], value, onChange }: Props) =>
  React.createElement(
    "div",
    { role: "radiogroup" },
    (options as { label: string; value: string }[]).map((o) =>
      React.createElement(
        "button",
        {
          key: o.value,
          type: "button",
          role: "radio",
          "aria-checked": o.value === value,
          onClick: () => (onChange as (v: string) => void)?.(o.value),
        },
        o.label,
      ),
    ),
  );

const Descriptions = Object.assign(Div, {
  Item: ({ label, children }: Props) =>
    React.createElement("div", null, React.createElement("span", null, label as React.ReactNode), children),
});

export const antdDouble = {
  __esModule: true,
  Alert: ({ message, description, ...p }: Props) =>
    React.createElement("div", { role: "note", ...p }, message as React.ReactNode, description as React.ReactNode),
  App: Div,
  AutoComplete: Input,
  Avatar: Div,
  Badge: Div,
  Button,
  Card: Div,
  Cascader: Div,
  Checkbox: Input,
  Col: Div,
  Collapse: Div,
  ConfigProvider: Div,
  Descriptions,
  Divider: Div,
  Dropdown: Div,
  Empty: Object.assign(Div, { PRESENTED_IMAGE_SIMPLE: null }),
  Form: Object.assign(
    ({ children, onFinish }: Props) =>
      React.createElement(
        "form",
        {
          onSubmit: (e: React.FormEvent) => {
            e.preventDefault();
            (onFinish as ((v: Record<string, unknown>) => void) | undefined)?.({});
          },
        },
        children,
      ),
    {
      Item: ({ label, help, children }: Props) =>
        React.createElement(
          "div",
          null,
          React.createElement("label", null, label as React.ReactNode),
          children,
          React.createElement("small", null, help as React.ReactNode),
        ),
      List: Div,
      // Дескриптор формы несёт те же методы, что настоящий: без них вызывающий
      // падает на `form.resetFields is not a function` — то есть дублёр был бы
      // строже продукта и прятал бы поведение, ради которого его и монтируют.
      useForm: () => [
        {
          resetFields: () => undefined,
          submit: () => undefined,
          setFieldsValue: () => undefined,
          getFieldsValue: () => ({}),
          validateFields: () => Promise.resolve({}),
        },
      ],
      useWatch: () => undefined,
    },
  ),
  Image: Div,
  Input: Object.assign(Input, { TextArea, Search: Input }),
  InputNumber: ({ onChange, value, ...p }: Props) =>
    React.createElement("input", {
      type: "number",
      value: (value as number) ?? "",
      onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
        (onChange as (v: number | null) => void)?.(e.target.value === "" ? null : Number(e.target.value)),
      ...p,
    }),
  Layout: Object.assign(Div, { Content: Div, Header: Div, Sider: Div }),
  List: Div,
  Menu: Div,
  Modal: Object.assign(Modal, { confirm: () => undefined, destroyAll: () => undefined }),
  Popconfirm,
  Radio: Object.assign(Input, { Group: Div, Button: Div }),
  Result: Div,
  Row: Div,
  Segmented,
  Select: Div,
  Space: Div,
  Spin: Div,
  Statistic: Div,
  Switch: Input,
  Table,
  Tabs: Div,
  Tag: ({ children }: Props) => React.createElement("span", null, children),
  // Подпись подсказки достаётся нативным `title`: у настоящего antd она видна
  // при наведении, здесь — читается как атрибут. Пропасть она не может.
  Tooltip: ({ children, title }: Props) => React.createElement("span", title ? { title: String(title) } : {}, children),
  Tree: Div,
  Typography: Object.assign(({ children }: Props) => React.createElement("span", null, children), {
    Text: ({ children }: Props) => React.createElement("span", null, children),
    Title: ({ children }: Props) => React.createElement("h2", null, children),
    Paragraph: ({ children }: Props) => React.createElement("p", null, children),
  }),
  theme: {
    useToken: () => ({
      token: {
        colorBgContainer: "#fff",
        colorBorderSecondary: "#e5e7eb",
        colorError: "#ef4444",
        colorFillSecondary: "#f3f4f6",
        colorPrimary: "#1677ff",
        colorText: "#111827",
        colorTextSecondary: "#6b7280",
        colorTextTertiary: "#9ca3af",
      },
    }),
  },
};
