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
  const Select = ({ children, ...props }: React.PropsWithChildren<AnyProps>) =>
    React.createElement("select", props, children);
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
  const Form = Object.assign(Component, {
    Item: Component,
    List: Component,
    useForm: () => [{}],
    useWatch: () => undefined,
  });
  // Настоящее модальное окно СКРЫВАЕТ своё содержимое, пока `open` ложно.
  // Заменитель-`<div>` рисовал его всегда, и утверждение «после клика окно
  // показало X» проходило ещё ДО клика — то есть проба закрепляла форму
  // дублёра, а не наблюдаемое. Пропуск `open` (неуправляемое окно) сохраняет
  // прежнее поведение: скрывать нечего.
  const ModalRoot = ({ children, open, ...props }: React.PropsWithChildren<{ open?: boolean }>) =>
    open === false ? null : React.createElement("div", props, children);
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
    Alert: Component,
    App: Component,
    AutoComplete: Input,
    Avatar: Component,
    Badge: Component,
    Button,
    Card: Component,
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
    Tag: Component,
    Tooltip: Component,
    Tree: Component,
    Typography,
    theme,
  };
}
