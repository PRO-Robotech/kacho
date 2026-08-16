module.exports = {
  // Порядок суит — свойство дерева, а не кэша машины: см. шапку
  // ../shared/jest-sequencer-by-path.cjs (и #461, где плавающий порядок давал
  // разное число падений на одном дереве). `require.resolve` — потому что
  // `testSequencer` принимает ПУТЬ, а файл лежит вне пакета.
  testSequencer: require.resolve("../shared/jest-sequencer-by-path.cjs"),
  preset: "ts-jest",
  testEnvironment: "jsdom",
  extensionsToTreatAsEsm: [".ts", ".tsx"],
  roots: ["<rootDir>/src", "<rootDir>/../shared/src"],
  setupFilesAfterEnv: ["<rootDir>/../shared/src/test/setup.ts"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}", "<rootDir>/../shared/src/**/*.test.{ts,tsx}"],
  moduleNameMapper: {
    // @ant-design/icons → стаб с реальными статическими named-экспортами.
    // Прежний Proxy-мок в shared/src/test/setup.ts их не давал, и под
    // --experimental-vm-modules ESM-линкер уводил процесс jest молча (exit 0, без
    // отчёта) на КАЖДОЙ суите, которая транзитивно тянет иконки — то есть на всех
    // shared-суитах. Тот же фикс, что уже стоит в host.
    "^@ant-design/icons$": "<rootDir>/../shared/src/test/antd-icons-stub.tsx",
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
