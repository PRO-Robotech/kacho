const { singletonMappings } = require("../shared/jest-singletons.cjs");

module.exports = {
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
