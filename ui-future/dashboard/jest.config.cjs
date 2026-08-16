const { singletonMappings } = require("../shared/jest-singletons.cjs");

module.exports = {
  // Порядок суит — свойство дерева, а не кэша машины: см. шапку
  // ../shared/jest-sequencer-by-path.cjs (и #461, где плавающий порядок давал
  // разное число падений на одном дереве). `require.resolve` — потому что
  // `testSequencer` принимает ПУТЬ, а файл лежит вне пакета.
  testSequencer: require.resolve("../shared/jest-sequencer-by-path.cjs"),
  preset: "ts-jest",
  testEnvironment: "jsdom",
  extensionsToTreatAsEsm: [".ts", ".tsx"],
  setupFilesAfterEnv: ["<rootDir>/src/test/setup.ts"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}"],
  // У `ui-future/shared` собственных node_modules нет: зависимости общему коду
  // даёт потребитель — так же, как это делает vite через alias.
  moduleDirectories: ["node_modules", "<rootDir>/node_modules"],
  moduleNameMapper: {
    // Те же singleton'ы, что resolve.dedupe в vite.config.ts: без них файлам
    // @shared досталась бы ВТОРАЯ копия react.
    ...singletonMappings(__dirname),
    "^(react|react-dom)/(.*)$": "<rootDir>/node_modules/$1/$2",
    "^@shared/(.*)$": "<rootDir>/../shared/src/$1",
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
