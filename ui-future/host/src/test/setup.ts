import "@testing-library/jest-dom";
import React from "react";
import { jest } from "@jest/globals";
import { TextDecoder, TextEncoder } from "node:util";

// @ant-design/icons ships as ESM; under jest's experimental-vm-modules antd's
// internal require() of it races the concurrent import() ("Cannot require() ES
// Module … Context.js synchronously"). Mock it to a plain <span> like every
// other package's test setup does. antd itself stays real so Layout/footer
// assertions keep exercising the shell's real markup.
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

Object.defineProperty(global, "TextEncoder", {
  writable: true,
  value: TextEncoder,
});

Object.defineProperty(global, "TextDecoder", {
  writable: true,
  value: TextDecoder,
});

Object.defineProperty(global, "fetch", {
  writable: true,
  value: () => Promise.reject(new Error("fetch mock not implemented")),
});

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
});

class ResizeObserverMock {
  observe = () => undefined;
  unobserve = () => undefined;
  disconnect = () => undefined;
}

Object.defineProperty(window, "ResizeObserver", {
  writable: true,
  value: ResizeObserverMock,
});
