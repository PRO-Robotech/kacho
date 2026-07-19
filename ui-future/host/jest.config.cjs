module.exports = {
  preset: "ts-jest",
  testEnvironment: "jsdom",
  extensionsToTreatAsEsm: [".ts", ".tsx"],
  setupFilesAfterEnv: ["<rootDir>/src/test/setup.ts"],
  testMatch: ["<rootDir>/src/**/*.test.{ts,tsx}"],
  moduleNameMapper: {
    "^(\\.{1,2}/.*)\\.js$": "$1",
    // @ant-design/icons → стаб с реальными статическими named-экспортами. КОРЕНЬ
    // host-hang (kacho#7, DIAG6): прежний jest.unstable_mockModule Proxy-мок в setup.ts
    // не давал статических named-экспортов → под --experimental-vm-modules ESM-линкер
    // `import { XOutlined }` (HostRail, 20 иконок) висел вечно, ожидая binding. Мап на
    // реальный стаб → линкер резолвит. Заодно снимает исходную ESM/CJS-гонку antd↔icons
    // (antd тоже получает стаб, реальный @ant-design/icons не грузится).
    "^@ant-design/icons$": "<rootDir>/src/test/antd-icons-stub.tsx",
    "^dashboard/DashboardPage$": "<rootDir>/src/test/dashboard-remote.tsx",
    "^dashboard/navigation$": "<rootDir>/src/test/dashboard-navigation.ts",
    "^vpc/VpcPage$": "<rootDir>/src/test/vpc-remote.tsx",
    "^vpc/navigation$": "<rootDir>/src/test/vpc-navigation.ts",
    // compute/storage/registry navigation — застаблены (пустые), иначе HostRail
    // useEffect Promise.allSettled(import("<r>/navigation")) виснет на CI-ранере
    // (unstubbed bare-specifier под --experimental-vm-modules never settles). kacho#7.
    "^compute/navigation$": "<rootDir>/src/test/compute-navigation.ts",
    "^storage/navigation$": "<rootDir>/src/test/storage-navigation.ts",
    "^registry/navigation$": "<rootDir>/src/test/registry-navigation.ts",
    "^nlb/NlbPage$": "<rootDir>/src/test/nlb-remote.tsx",
    "^nlb/navigation$": "<rootDir>/src/test/nlb-navigation.ts",
    "^iam/IamPage$": "<rootDir>/src/test/iam-remote.tsx",
    "^iam/navigation$": "<rootDir>/src/test/iam-navigation.ts",
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
