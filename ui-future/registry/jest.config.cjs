module.exports = {
  preset: "ts-jest",
  testEnvironment: "jsdom",
  extensionsToTreatAsEsm: [".ts", ".tsx"],
  setupFilesAfterEnv: ["<rootDir>/src/test/setup.ts"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}"],
  moduleNameMapper: {
    // @ant-design/icons → статический стаб (kacho#7): Proxy-мок в setup.ts не давал
    // статических named-экспортов → ESM-линкер `import { XOutlined }` висел под vm-modules.
    "^@ant-design/icons$": "<rootDir>/src/test/antd-icons-stub.tsx",
    "\\.(css|less|scss|sass)$": "<rootDir>/src/test/style-mock.ts",
    // Те же singleton'ы, что и resolve.dedupe в vite.config.ts: файлы
    // @shared лежат вне этого пакета, поэтому без явного отображения jest
    // резолвил бы им react/react-router/react-query из ../node_modules —
    // вторая копия react роняет любой хук в общем компоненте.
    "^(react|react-dom|react-router|@tanstack/react-query|antd)$": "<rootDir>/node_modules/$1",
    "^(react|react-dom|react-router)/(.*)$": "<rootDir>/node_modules/$1/$2",
    "^@shared/(.*)$": "<rootDir>/../shared/src/$1",
    "^@/(.*)$": "<rootDir>/src/$1",
    "^(\\.{1,2}/.*)\\.js$": "$1",
  },
  transform: {
    "^.+\\.(ts|tsx)$": [
      "ts-jest",
      {
        tsconfig: "tsconfig.app.json",
        useESM: true,
      },
    ],
  },
};
