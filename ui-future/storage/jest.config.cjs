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
  // Суита общего модуля исполняется И ЗДЕСЬ, а не только у vpc/iam/system.
  //
  // Модуль больше не держит своих копий: всё, что он показывает, приезжает из
  // `shared/`. Значит его вердикт о собственных пробах говорит о его РАЗРЕШЕНИИ
  // ровно столько же, сколько о его коде, — а разрешение здесь СВОЁ: у пакета
  // отдельный `package-lock`, отдельный `node_modules` и отображение
  // singleton'ов (`singletonMappings`), которого у workspace-модулей нет.
  // Прогон общей суиты под ЭТИМ отображением — единственное, что отличает
  // «общий код исправен» от «общий код исправен там, где его уже гоняли».
  roots: ["<rootDir>/src", "<rootDir>/../shared/src"],
  setupFilesAfterEnv: ["<rootDir>/../shared/src/test/setup.ts"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}", "<rootDir>/../shared/src/**/*.test.{ts,tsx}"],
  moduleNameMapper: {
    // @ant-design/icons → статический стаб (kacho#7): Proxy-мок в setup.ts не давал
    // статических named-экспортов → ESM-линкер `import { XOutlined }` висел под vm-modules.
    "^@ant-design/icons$": "<rootDir>/../shared/src/test/antd-icons-stub.tsx",
    "\\.(css|less|scss|sass)$": "<rootDir>/src/test/style-mock.ts",
    // Те же singleton'ы, что и resolve.dedupe в vite.config.ts: файлы @shared лежат
    // вне этого пакета, поэтому без явного отображения jest резолвил бы им ВТОРУЮ
    // копию react из ../node_modules. Почему отображение на файл входа, а не на
    // каталог пакета (react-router@8 — exports-only, каталог не резолвится и уносит
    // суиту целиком) — в ../shared/jest-singletons.cjs.
    ...singletonMappings(__dirname),
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
