import "@testing-library/jest-dom";
import React from "react";
import { jest } from "@jest/globals";
import { TextDecoder, TextEncoder } from "node:util";

Object.assign(globalThis, {
  TextDecoder,
  TextEncoder,
});

jest.unstable_mockModule("@ant-design/icons", () => {
  const Icon = (props: React.HTMLAttributes<HTMLSpanElement>) => React.createElement("span", props);

  return new Proxy(
    { __esModule: true },
    {
      get(target, prop) {
        if (prop in target) return target[prop as keyof typeof target];
        return Icon;
      },
    },
  );
});

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
  const Textarea = (props: Record<string, unknown>) => React.createElement("textarea", props);
  const Select = ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) =>
    React.createElement("select", props, children);

  const Typography = Object.assign(Component, {
    Text: Component,
    Title: Component,
    Paragraph: Component,
    Link: Component,
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
    Input: Object.assign(Input, { TextArea: Textarea, Search: Input }),
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
    Space: Object.assign(Component, { Compact: Component }),
    Spin: Component,
    Statistic: Component,
    Switch: Input,
    Table: Component,
    Tabs: Component,
    Tag: Component,
    Tooltip: Component,
    Tree: Component,
    Typography,
    theme,
  };
});
