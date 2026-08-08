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
  render?: (value: unknown, row: unknown, index: number) => React.ReactNode;
}

interface MockTableProps {
  columns?: MockColumn[];
  dataSource?: unknown[];
  rowKey?: (row: unknown) => string;
  onRow?: (row: unknown) => Record<string, unknown>;
  locale?: { emptyText?: React.ReactNode };
}

export function antdStub(): Record<string, unknown> {
  const Component = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("div", props, children);
  const Button = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("button", { type: "button", ...props }, children);
  const Input = (props: AnyProps) => React.createElement("input", props);
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
                { key: rowKey ? rowKey(row) : ri, ...(onRow ? onRow(row) : {}) },
                columns.map((c, ci) => React.createElement("td", { key: ci }, c.render?.(undefined, row, ri))),
              ),
            ),
      ),
    );
  const Select = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("select", props, children);

  const Typography = Object.assign(Component, {
    Text: Component,
    Title: Component,
    Paragraph: Component,
  });
  const Layout = Object.assign(Component, {
    Content: Component,
    Header: Component,
    Sider: Component,
  });
  const Form = Object.assign(Component, {
    Item: Component,
    List: Component,
    useForm: () => [{}],
    useWatch: () => undefined,
  });
  const Modal = Object.assign(Component, {
    confirm: jest.fn(),
    destroyAll: jest.fn(),
  });
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
    Alert: Component,
    App: Component,
    AutoComplete: Input,
    Avatar: Component,
    Badge: Component,
    Button,
    Card: Component,
    Cascader: Select,
    Checkbox: Input,
    Col: Component,
    Collapse: Component,
    Descriptions: Component,
    Divider: Component,
    Dropdown: Component,
    Empty: Component,
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
    Space: Component,
    Spin: Component,
    Statistic: Component,
    Switch: Input,
    Table,
    Tabs: Component,
    Tag: Component,
    Tooltip: Component,
    Tree: Component,
    Typography,
    theme,
  };
}
