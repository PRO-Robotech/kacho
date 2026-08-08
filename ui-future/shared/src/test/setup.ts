import "@testing-library/jest-dom";
import React from "react";
import { jest } from "@jest/globals";
import { antdStub } from "./antd-stub";
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

jest.unstable_mockModule("antd", () => antdStub());
