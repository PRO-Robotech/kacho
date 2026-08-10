import "@testing-library/jest-dom";
import React from "react";
import { jest } from "@jest/globals";
import { TextDecoder, TextEncoder } from "node:util";

Object.assign(globalThis, {
  TextDecoder,
  TextEncoder,
});

// Три пробела в API браузера, которых у jsdom нет, а у antd-организмов (Table,
// Dropdown, Tabs) — есть на пути монтирования. Без них render страницы падает
// AggregateError'ом, в котором ИМЯ причины не печатается вовсе: список
// вложенных ошибок React 19 в отчёт jest не выводит, и падение читается как
// «страница не монтируется», хотя не хватает ровно этих трёх заглушек.
//
// Заглушки НЕ подменяют поведение продукта — они дают средам ответ той же формы,
// какой даёт браузер: наблюдатель, который ничего не наблюдает; медиазапрос,
// который не совпал; и вычисленный стиль без псевдоэлемента (jsdom умеет только
// его). Если проба начнёт зависеть от настоящего замера — она обязана это
// назвать, а не молча получить нули.
if (!("ResizeObserver" in globalThis)) {
  Object.assign(globalThis, {
    ResizeObserver: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  });
}
if (typeof window !== "undefined" && typeof window.matchMedia !== "function") {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
}
if (typeof window !== "undefined") {
  const computed = window.getComputedStyle.bind(window);
  window.getComputedStyle = ((elt: Element) => computed(elt)) as typeof window.getComputedStyle;
}

jest.unstable_mockModule("@monaco-editor/react", () => ({
  __esModule: true,
  default: (props: React.HTMLAttributes<HTMLDivElement>) => React.createElement("div", props),
}));

interface ModalProps {
  open?: boolean;
  title?: React.ReactNode;
  footer?: React.ReactNode;
  children?: React.ReactNode;
}

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
  // Настоящее окно antd СКРЫВАЕТ содержимое при `open=false` и показывает свои
  // заголовок и подвал. Прежний заменитель рисовал детей всегда и терял и то и
  // другое: утверждение «после клика окно показало X» проходило до клика, а
  // текст заголовка/кнопок подвала был ненаблюдаем вовсе. Заменитель обязан
  // выполнять контракт настоящего, иначе проба закрепляет его форму.
  const ModalRoot = ({ open, title, footer, children, ...props }: ModalProps) =>
    open === false
      ? null
      : React.createElement(
          "div",
          { role: "dialog", ...props },
          React.createElement("div", null, title as React.ReactNode),
          React.createElement("div", null, children),
          React.createElement("div", null, footer as React.ReactNode),
        );
  const Modal = Object.assign(ModalRoot, {
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
    // ConfigProvider несёт `ThemeProvider` раздела, то есть стоит НА ПУТИ
    // монтирования любой страницы. Стаб без него не «даёт компонент попроще» —
    // ESM-линкер валит СУИТУ ЦЕЛИКОМ («does not provide an export named
    // ConfigProvider») до первого утверждения, и падение читается как «страницу
    // смонтировать нельзя». Дублёр обязан выполнять контракт настоящего.
    ConfigProvider: Component,
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
    // Skeleton стоит на пути монтирования страниц реестра (заглушка загрузки).
    // Без него ESM-линкер валит суиту целиком до первого утверждения.
    Skeleton: Component,
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
