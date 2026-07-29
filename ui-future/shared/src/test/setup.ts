import "@testing-library/jest-dom";
import React from "react";
import { jest } from "@jest/globals";
import { TextDecoder, TextEncoder } from "node:util";

Object.assign(globalThis, {
  TextDecoder,
  TextEncoder,
});

// jsdom ships no ResizeObserver, and ResourceTable measures its own body with
// one to size the scroll area. Without a stub every render of a list throws
// before a single row exists, so nothing above the table can be tested.
if (!("ResizeObserver" in globalThis)) {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  Object.assign(globalThis, { ResizeObserver: ResizeObserverStub });
}

// @ant-design/icons мокается через moduleNameMapper (стаб ./antd-icons-stub.tsx
// с реальными статическими named-экспортами), а НЕ Proxy-моком: Proxy их не
// даёт, и под --experimental-vm-modules ESM-линкер не находит binding для
// `import { XOutlined }` — процесс jest уходит молча, с кодом 0 и без отчёта,
// то есть весь прогон читается как зелёный, ни разу не выполнившись. Тот же
// корень и то же лечение, что в host.

jest.unstable_mockModule("@monaco-editor/react", () => ({
  __esModule: true,
  default: (props: React.HTMLAttributes<HTMLDivElement>) => React.createElement("div", props),
}));

jest.unstable_mockModule("antd", () => {
  const Component = ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) =>
    React.createElement("div", props, children);
  const Button = ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) =>
    React.createElement("button", { type: "button", ...props }, children);
  const Input = (props: Record<string, unknown>) => React.createElement("input", props);
  const Search = (props: Record<string, unknown>) => React.createElement("input", { type: "search", ...props });
  const Textarea = (props: Record<string, unknown>) => React.createElement("textarea", props);

  // Table renders its rows for real. A `<div>` stand-in makes every assertion
  // about a list's contents — a cell, a row action, an empty state — pass no
  // matter what the component did, which is worse than having no test.
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
  const Select = ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) =>
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
});
