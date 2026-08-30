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
  setupFilesAfterEnv: ["<rootDir>/../shared/src/test/setup.ts"],
  // Общая суита исполняется И ЗДЕСЬ. Модуль standalone: у него собственный
  // `node_modules` и собственный замок версий, поэтому прогон общей суиты
  // участниками рабочей области (vpc/iam/system) о ЕГО посадке не утверждает
  // ничего — они разрешают зависимости из корневого дерева, а оно с этим
  // расходится. Модуль при этом отгружает пользователю почти целиком общий код
  // через тонкие прослойки, и до этой строки тот код ни разу не проверялся под
  // теми версиями, с которыми едет. Тот же порядок уже у storage (#407) и nlb
  // (#408). Держит `src/test/shared-suite-is-run-here.test.ts` — он спрашивает
  // у самого jest, что тот НАХОДИТ, а не читает эти строки.
  roots: ["<rootDir>/src", "<rootDir>/../shared/src"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}", "<rootDir>/../shared/src/**/*.test.{ts,tsx}"],
  // У `ui-future/shared` собственных node_modules нет: его исходники — часть
  // сборки КАЖДОГО remote'а, и зависимости им даёт remote (так же это делает
  // vite, для которого `@shared/*` — обычный alias внутри одного графа). Без
  // этой строки импорт shared-файла из непрямой зависимости (`clsx`,
  // `tailwind-merge`, …) роняет СУИТУ ЦЕЛИКОМ сообщением «Cannot find module …
  // from ../shared/src/…», то есть не про свой предмет. Точечные отображения
  // синглтонов ниже это НЕ покрывают — они прибивают перечисленные пакеты, а
  // здесь речь о произвольной транзитивной зависимости общего кода.
  moduleDirectories: ["node_modules", "<rootDir>/node_modules"],
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
