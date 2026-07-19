import "@testing-library/jest-dom";
import { TextDecoder, TextEncoder } from "node:util";

// @ant-design/icons мокается через jest.config moduleNameMapper (стаб
// src/test/antd-icons-stub.tsx с реальными статическими named-экспортами), НЕ через
// jest.unstable_mockModule: Proxy-мок не давал статических named-экспортов и ESM-линкер
// `import { XOutlined }` висел вечно под --experimental-vm-modules (kacho#7, DIAG6).

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
